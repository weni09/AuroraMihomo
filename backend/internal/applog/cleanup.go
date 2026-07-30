package applog

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// 保留时长的取值范围。
//
// 下限 1 天：再短就等于关掉落盘，那应该用 ToFile: false 表达。
// 上限 365 天：本项目的日志量不大，但没有上限时误填一个巨大值
// 会让轮转归档无限堆积，磁盘占用失控。
const (
	MinRetentionDays     = 1
	MaxRetentionDays     = 365
	DefaultRetentionDays = 7
)

// retentionDays 用原子变量存：清理在后台 goroutine 里跑，
// 而设置可由界面在任意时刻改动，两者并发访问。
// 放在包级而非 fileSink 上，是因为清理任务与 Writer 生命周期解耦——
// 即使 Writer 因落盘失败降级成仅内存，已有的历史文件仍需被清理。
var retentionDays atomic.Int32

func init() { retentionDays.Store(DefaultRetentionDays) }

// SetRetentionDays 设置日志文件保留天数，超出范围时夹到边界。
// 返回实际生效的值，便于调用方回显给用户。
func SetRetentionDays(days int) int {
	if days < MinRetentionDays {
		days = MinRetentionDays
	}
	if days > MaxRetentionDays {
		days = MaxRetentionDays
	}
	retentionDays.Store(int32(days))
	return days
}

// RetentionDays 返回当前保留天数。
func RetentionDays() int { return int(retentionDays.Load()) }

// CleanupResult 汇总一次清理的结果，供日志与接口展示。
type CleanupResult struct {
	Removed int   `json:"removed"`
	Bytes   int64 `json:"bytes"`
}

// CleanupArchives 删除超过保留期的轮转归档。
//
// 只删归档（aurora.log.20260730-141000），不动当前正在写的 aurora.log——
// 当前文件由大小轮转机制管理，按时间删它会把刚写的日志也删掉。
//
// 判据用文件的修改时间而非文件名里的时间戳：时间戳是归档那一刻的时间，
// 而修改时间同样是那一刻（归档后不再写入），两者等价；用 mtime 则不必
// 解析文件名，也不会因为将来改了命名格式而失效。
//
// 返回删除数量与释放的字节数。单个文件删除失败不中断整轮清理：
// 某个文件被占用不该导致其余过期文件全部留下。
func CleanupArchives(logPath string, days int) (CleanupResult, error) {
	var res CleanupResult
	if logPath == "" {
		return res, nil
	}
	if days < MinRetentionDays {
		days = MinRetentionDays
	}

	matches, err := filepath.Glob(logPath + ".*")
	if err != nil {
		return res, err
	}
	cutoff := time.Now().AddDate(0, 0, -days)

	for _, path := range matches {
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		if !st.ModTime().Before(cutoff) {
			continue
		}
		size := st.Size()
		if err := os.Remove(path); err != nil {
			continue
		}
		res.Removed++
		res.Bytes += size
	}
	return res, nil
}
