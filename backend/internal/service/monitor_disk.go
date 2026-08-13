package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

// diskExcludedFSTypes 非数据盘 / 虚拟文件系统，不计入容量合计。
// 键一律小写；比对时 ToLower。
//
// overlay 必须排除：Docker Desktop（含 Mac）容器根在 Linux VM 的 overlay 上，
// 对其做 Usage 会得到 VM 虚拟盘（常见约 60GB），不是宿主磁盘。
// fuse / fuseblk 是通用 FUSE 壳，排除以免把 gvfs 等桌面挂载算进去；
// Mac 绑定盘的 fstype 是 virtiofs / osxfs / grpcfuse，不在此表。
var diskExcludedFSTypes = map[string]struct{}{
	"tmpfs": {}, "devtmpfs": {}, "devfs": {}, "overlay": {}, "overlay2": {},
	"aufs": {}, "squashfs": {}, "proc": {}, "sysfs": {}, "cgroup": {}, "cgroup2": {},
	"pstore": {}, "bpf": {}, "tracefs": {}, "debugfs": {}, "securityfs": {},
	"hugetlbfs": {}, "mqueue": {}, "rpc_pipefs": {}, "fusectl": {}, "configfs": {},
	"fuse": {}, "fuseblk": {}, "fuse.portal": {}, "fuse.gvfsd-fuse": {},
	"nsfs": {}, "ramfs": {}, "rootfs": {}, "iso9660": {}, "udf": {},
	"autofs": {}, "binfmt_misc": {}, "efivarfs": {},
}

// DiskVolume 一块计入合计的常规文件系统。
type DiskVolume struct {
	Path    string
	Total   uint64
	Used    uint64
	Free    uint64
	Percent float64
	Fstype  string
}

type diskPartition struct {
	Device     string
	Mountpoint string
	Fstype     string
}

type diskUsage struct {
	Path        string
	Total       uint64
	Used        uint64
	Free        uint64
	UsedPercent float64
}

func diskFSTypeExcluded(fstype string) bool {
	_, ok := diskExcludedFSTypes[strings.ToLower(strings.TrimSpace(fstype))]
	return ok
}

// listHostPartitions / usageOfPath 可在测试里替换。
var listHostPartitions = func(ctx context.Context) ([]diskPartition, error) {
	ps, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]diskPartition, 0, len(ps))
	for _, p := range ps {
		out = append(out, diskPartition{
			Device:     p.Device,
			Mountpoint: p.Mountpoint,
			Fstype:     p.Fstype,
		})
	}
	return out, nil
}

var usageOfPath = func(ctx context.Context, path string) (diskUsage, error) {
	u, err := disk.UsageWithContext(ctx, path)
	if err != nil {
		return diskUsage{}, err
	}
	return diskUsage{
		Path:        u.Path,
		Total:       u.Total,
		Used:        u.Used,
		Free:        u.Free,
		UsedPercent: u.UsedPercent,
	}, nil
}

func collectDiskVolumes(ctx context.Context, parts []diskPartition, usage map[string]diskUsage) ([]DiskVolume, error) {
	seen := map[string]struct{}{}
	var out []DiskVolume
	for _, p := range parts {
		if diskFSTypeExcluded(p.Fstype) {
			continue
		}
		mp := strings.TrimSpace(p.Mountpoint)
		if mp == "" {
			continue
		}
		u, ok := usage[mp]
		if !ok {
			var err error
			u, err = usageOfPath(ctx, mp)
			if err != nil {
				// 单块盘读失败不拖垮整次采集（卸载中的盘、权限不足）
				continue
			}
		}
		if u.Total == 0 {
			continue
		}
		id := diskVolumeIdentity(p, u)
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, DiskVolume{
			Path:    firstNonEmptyPath(u.Path, mp),
			Total:   u.Total,
			Used:    u.Used,
			Free:    u.Free,
			Percent: u.UsedPercent,
			Fstype:  p.Fstype,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func firstNonEmptyPath(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// diskVolumeIdentity 去重键。
//
// 同一块设备 bind 到多处只计一次。Docker Desktop / Mac 上多个 virtiofs、
// osxfs、grpcfuse 挂载往往 Device 不同，但 Statfs 返回同一套 Total/Used
// （宿主盘或 VM 盘被映射了多次）——按容量指纹合并，避免「共」放大 N 倍。
func diskVolumeIdentity(p diskPartition, u diskUsage) string {
	ft := strings.ToLower(strings.TrimSpace(p.Fstype))
	if ft == "virtiofs" || ft == "osxfs" || ft == "grpcfuse" || strings.HasPrefix(ft, "fuse.grpc") {
		return fmt.Sprintf("macfs:%d:%d", u.Total, u.Used)
	}
	dev := strings.TrimSpace(p.Device)
	if dev != "" && !strings.EqualFold(dev, "none") {
		return "dev:" + dev
	}
	return "mnt:" + p.Mountpoint
}

func aggregateDiskStats(vols []DiskVolume) (total, used uint64, percent float64, label string) {
	if len(vols) == 0 {
		return 0, 0, 0, ""
	}
	for _, v := range vols {
		total += v.Total
		used += v.Used
	}
	// 合计百分比与「已用/共」同一分母（sum Total），避免卡片上两个数字对不上。
	if total > 0 {
		percent = float64(used) / float64(total) * 100
	}
	if len(vols) == 1 {
		return total, used, percent, vols[0].Path
	}
	return total, used, percent, fmt.Sprintf("%d 个文件系统", len(vols))
}

// collectHostDisk 列出宿主常规文件系统并合计。
// 分区表读失败时回落到 fallbackPath（数据目录）单点 Usage。
func collectHostDisk(ctx context.Context, fallbackPath string) (vols []DiskVolume, total, used uint64, percent float64, label string, err error) {
	parts, perr := listHostPartitions(ctx)
	if perr == nil {
		vols, _ = collectDiskVolumes(ctx, parts, nil)
	}
	if len(vols) == 0 && strings.TrimSpace(fallbackPath) != "" {
		u, uerr := usageOfPath(ctx, fallbackPath)
		if uerr != nil {
			if perr != nil {
				return nil, 0, 0, 0, "", fmt.Errorf("采集磁盘使用率失败: %w", perr)
			}
			return nil, 0, 0, 0, "", fmt.Errorf("采集磁盘使用率失败: %w", uerr)
		}
		vols = []DiskVolume{{
			Path:    firstNonEmptyPath(u.Path, fallbackPath),
			Total:   u.Total,
			Used:    u.Used,
			Free:    u.Free,
			Percent: u.UsedPercent,
		}}
	}
	if len(vols) == 0 {
		return nil, 0, 0, 0, "", fmt.Errorf("采集磁盘使用率失败: 没有可统计的常规文件系统")
	}
	total, used, percent, label = aggregateDiskStats(vols)
	return vols, total, used, percent, label, nil
}
