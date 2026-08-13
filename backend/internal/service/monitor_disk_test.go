package service

import (
	"context"
	"testing"
)

func TestCollectDiskVolumesExcludesVirtualFS(t *testing.T) {
	parts := []diskPartition{
		{Device: "/dev/sda1", Mountpoint: "/", Fstype: "ext4"},
		{Device: "tmpfs", Mountpoint: "/tmp", Fstype: "tmpfs"},
		{Device: "overlay", Mountpoint: "/overlay", Fstype: "overlay"},
		{Device: "proc", Mountpoint: "/proc", Fstype: "proc"},
		{Device: "/dev/sdb1", Mountpoint: "/data", Fstype: "xfs"},
	}
	usage := map[string]diskUsage{
		"/":     {Path: "/", Total: 100, Used: 40, Free: 55, UsedPercent: 42.1},
		"/data": {Path: "/data", Total: 200, Used: 50, Free: 150, UsedPercent: 25},
	}
	got, err := collectDiskVolumes(context.Background(), parts, usage)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("应保留 2 个常规文件系统，实际 %d: %+v", len(got), got)
	}
	if got[0].Path != "/" || got[1].Path != "/data" {
		t.Fatalf("挂载点顺序或内容不对: %+v", got)
	}
}

func TestCollectDiskVolumesDedupesSameDevice(t *testing.T) {
	parts := []diskPartition{
		{Device: "/dev/sda1", Mountpoint: "/", Fstype: "ext4"},
		{Device: "/dev/sda1", Mountpoint: "/bind", Fstype: "ext4"},
	}
	usage := map[string]diskUsage{
		"/":     {Path: "/", Total: 100, Used: 40, Free: 55},
		"/bind": {Path: "/bind", Total: 100, Used: 40, Free: 55},
	}
	got, err := collectDiskVolumes(context.Background(), parts, usage)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("同一设备两次挂载应去重，实际 %d", len(got))
	}
}

// Docker Desktop / Mac：多个 virtiofs 挂载常报同一块盘的容量，必须按容量指纹去重，
// 否则「共」会是宿主盘的 N 倍。
func TestCollectDiskVolumesDedupesVirtiofsSameSize(t *testing.T) {
	parts := []diskPartition{
		{Device: "data", Mountpoint: "/data", Fstype: "virtiofs"},
		{Device: "Users", Mountpoint: "/Users", Fstype: "virtiofs"},
		{Device: "host_mnt", Mountpoint: "/host_mnt/Users", Fstype: "grpcfuse"},
	}
	same := diskUsage{Total: 500_000, Used: 200_000, Free: 300_000, UsedPercent: 40}
	usage := map[string]diskUsage{
		"/data":           {Path: "/data", Total: same.Total, Used: same.Used, Free: same.Free, UsedPercent: same.UsedPercent},
		"/Users":          {Path: "/Users", Total: same.Total, Used: same.Used, Free: same.Free, UsedPercent: same.UsedPercent},
		"/host_mnt/Users": {Path: "/host_mnt/Users", Total: same.Total, Used: same.Used, Free: same.Free, UsedPercent: same.UsedPercent},
	}
	got, err := collectDiskVolumes(context.Background(), parts, usage)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("同容量 virtiofs/grpcfuse 应去重为 1，实际 %d: %+v", len(got), got)
	}
}

func TestAggregateDiskStatsSumsAndPercent(t *testing.T) {
	vols := []DiskVolume{
		{Path: "/", Total: 100, Used: 40, Free: 55, Percent: 42},
		{Path: "/data", Total: 200, Used: 50, Free: 150, Percent: 25},
	}
	total, used, pct, label := aggregateDiskStats(vols)
	if total != 300 || used != 90 {
		t.Fatalf("合计 total=%d used=%d", total, used)
	}
	want := 90.0 / 300.0 * 100
	if pct < want-0.01 || pct > want+0.01 {
		t.Fatalf("百分比 %v 期望约 %v", pct, want)
	}
	if label != "2 个文件系统" {
		t.Fatalf("标签 %q", label)
	}
}

func TestAggregateDiskStatsEmpty(t *testing.T) {
	total, used, pct, label := aggregateDiskStats(nil)
	if total != 0 || used != 0 || pct != 0 || label != "" {
		t.Fatalf("空列表应全零，got %d %d %v %q", total, used, pct, label)
	}
}

func TestDiskFSTypeExcludedCaseInsensitive(t *testing.T) {
	if !diskFSTypeExcluded("TMPFS") || !diskFSTypeExcluded("Overlay2") {
		t.Fatal("排除表应按小写比对")
	}
	if diskFSTypeExcluded("ext4") || diskFSTypeExcluded("apfs") || diskFSTypeExcluded("virtiofs") {
		t.Fatal("常规/ Mac 绑定盘不应排除")
	}
}
