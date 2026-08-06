package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// MonitorStats 一次宿主资源采样的结果。
//
// 语义与 .api 规格中的 SystemStats 对齐，但这里是领域层自己的结构：
// service 不依赖 api/internal/types（那属于生成物，且是 api 层向内依赖，
// 反向会形成循环）。字段含义见 .api 文件中的注释。
type MonitorStats struct {
	CPUPercent    float64
	MemTotal      uint64
	MemUsed       uint64
	MemPercent    float64
	NetUpRate     uint64 // 字节/秒
	NetDownRate   uint64 // 字节/秒
	NetUpTotal    uint64 // 开机以来累计，排除 loopback
	NetDownTotal  uint64
	DiskTotal     uint64
	DiskUsed      uint64
	DiskPercent   float64
	DiskPath      string
	UptimeSeconds uint64 // 主机开机时长
}

// cpuSampleInterval 是 CPU 使用率的采样窗口。
//
// gopsutil 的 Percent 需要前后两次读数做差分，窗口为 0 时两次读数
// 几乎同时完成，/proc/stat 的 tick 粒度下分母趋近于零，结果会在
// 0 与一个随机值之间跳动。取 200ms 既有代表性，又不至于让每次
// 查询明显变慢（控制台 60s 才轮询一次）。
const cpuSampleInterval = 200 * time.Millisecond

// MonitorService 采集宿主服务器资源（CPU/内存/磁盘/运行时长）并计算网络速率。
//
// 网络速率无法一次性测得：它需要「前后两次累计字节数的差 ÷ 间隔」。
// 因此本服务是单例并持有上次采样（累计字节 + 时间戳），每次查询返回
// 「自上次查询以来的平均速率」——轮询越频繁，数值越接近瞬时速率。
// 查询与采样写入共用一把锁串行化，多个并发请求也不会互相污染基线。
type MonitorService struct {
	mu       sync.Mutex
	diskPath string // 磁盘探测目标：面板数据目录所在分区

	// 上次采样基线，用于网络速率差分。hasPrev 区分「从未采样」与
	// 「上次恰好采样到 0」——首次调用没有基线，速率只能给 0。
	prevUp   uint64
	prevDown uint64
	prevTime time.Time
	hasPrev  bool
}

// NewMonitorService 创建资源监控服务。
//
// diskPath 是磁盘使用率的探测目标，应为面板数据目录（其所在分区
// 与面板/内核的运行最相关——磁盘满最先影响的就是这两个进程）。
// 传入前应转成绝对路径：Windows 上相对路径无法定位分区。
func NewMonitorService(diskPath string) *MonitorService {
	return &MonitorService{diskPath: diskPath}
}

// Stats 采集一次资源快照。
//
// 任一单项采集失败都整体返回错误（调用方按 500 处理、前端降级显示）：
// 监控数据是「尽力而为」的展示，但把失败静默成零值会误导用户
// 以为 CPU 真的是 0%。gopsutil 在常见部署形态（Linux 容器 / Windows /
// macOS）下各单项都很稳定，聚合错误不会频繁触发。
func (s *MonitorService) Stats(ctx context.Context) (*MonitorStats, error) {
	cpuPct, err := cpu.PercentWithContext(ctx, cpuSampleInterval, false)
	if err != nil {
		return nil, fmt.Errorf("采集 CPU 使用率失败: %w", err)
	}

	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("采集内存使用率失败: %w", err)
	}

	du, err := disk.UsageWithContext(ctx, s.diskPath)
	if err != nil {
		return nil, fmt.Errorf("采集磁盘使用率失败: %w", err)
	}

	uptimeSec, err := host.UptimeWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("采集主机运行时长失败: %w", err)
	}

	upTotal, downTotal, err := s.netTotals(ctx)
	if err != nil {
		return nil, fmt.Errorf("采集网卡流量失败: %w", err)
	}

	// 差分与基线更新必须在同一临界区内：两请求并发时，后进者
	// 拿到的基线必须是自己「之前」的采样，否则速率会算到对方头上。
	now := time.Now()
	s.mu.Lock()
	upRate, downRate := networkRates(s.prevUp, s.prevDown, s.prevTime, s.hasPrev, upTotal, downTotal, now)
	s.prevUp, s.prevDown, s.prevTime, s.hasPrev = upTotal, downTotal, now, true
	s.mu.Unlock()

	return &MonitorStats{
		CPUPercent:    cpuPct[0],
		MemTotal:      vm.Total,
		MemUsed:       vm.Used,
		MemPercent:    vm.UsedPercent,
		NetUpRate:     upRate,
		NetDownRate:   downRate,
		NetUpTotal:    upTotal,
		NetDownTotal:  downTotal,
		DiskTotal:     du.Total,
		DiskUsed:      du.Used,
		DiskPercent:   du.UsedPercent,
		DiskPath:      du.Path,
		UptimeSeconds: uptimeSec,
	}, nil
}

// netTotals 汇总全部物理网卡的累计收发字节，排除 loopback。
//
// 回环接口的流量是进程自己发给自己的，包含它会严重高估"上下行"：
// 内核 API、Web 面板这些本机访问都会算进去，且数值随本机流量
// 无意义地跳动。判定用名字前缀/关键词，覆盖三平台常见命名
// （lo / lo0 / "Loopback Pseudo-Interface 1"）。
func (s *MonitorService) netTotals(ctx context.Context) (up, down uint64, err error) {
	counters, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return 0, 0, err
	}
	for _, c := range counters {
		name := strings.ToLower(c.Name)
		if strings.HasPrefix(name, "lo") || strings.Contains(name, "loopback") {
			continue
		}
		up += c.BytesSent
		down += c.BytesRecv
	}
	return up, down, nil
}

// networkRates 由前后两次累计字节差分出平均速率（字节/秒）。
//
// 纯函数、无副作用，便于单测。首次采样（hasPrev=false）没有基线，
// 速率给 0；计数器重置（网卡重启、系统重启）或时钟倒退时按 0 处理，
// 而不是产生一个离谱的大数——宁可丢一次数据也不误导。
func networkRates(prevUp, prevDown uint64, prevTime time.Time, hasPrev bool, curUp, curDown uint64, curTime time.Time) (upRate, downRate uint64) {
	if !hasPrev || curTime.Before(prevTime) {
		return 0, 0
	}
	elapsed := curTime.Sub(prevTime).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}
	if curUp >= prevUp {
		upRate = uint64(float64(curUp-prevUp) / elapsed)
	}
	if curDown >= prevDown {
		downRate = uint64(float64(curDown-prevDown) / elapsed)
	}
	return upRate, downRate
}
