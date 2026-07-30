package netcheck

// 本文件放平台无关的纯解析函数：入参是已读出的文本或注入的探针，
// 不直接触碰真实文件系统。
//
// 单独成文件（不带 //go:build linux）的原因是可测性：解析 /proc/self/status
// 的 capability 位、/etc/os-release、/proc/modules 这些逻辑本身与运行平台
// 无关，放在带 linux tag 的文件里就只能在 Linux 上跑测试，而开发与 CI
// 未必在 Linux 上进行。

import (
	"strconv"
	"strings"
)

// Linux capability 的位序号（include/uapi/linux/capability.h）。
// 只列本包用到的三个。
const (
	capNetBindService = 10
	capNetAdmin       = 12
	capNetRaw         = 13
)

// readCapabilities 解析 /proc/self/status 里的 CapEff 与 CapBnd。
//
// 两者的区别是容器场景的关键：docker 的 cap_add 只填充 bounding 与
// permitted 集，非 root 进程且二进制无 file capability 时 effective 集为空，
// 于是 CapBnd 里看得到 NET_ADMIN 但实际不持有。区分开才能给出正确提示。
func readCapabilities(statusPath string) (eff, bnd uint64) {
	content := readFileTrim(statusPath)
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 16, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "CapEff:":
			eff = v
		case "CapBnd:":
			bnd = v
		}
	}
	return eff, bnd
}

func hasCap(mask uint64, bit uint) bool { return mask&(1<<bit) != 0 }

// parseOSRelease 取 /etc/os-release 的 ID 与 ID_LIKE。
func parseOSRelease(content string) (id string, idLike string) {
	for _, line := range strings.Split(content, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			id = v
		case "ID_LIKE":
			idLike = v
		}
	}
	return id, idLike
}

func cgroupLooksContainerized(content string) bool {
	for _, kw := range []string{"docker", "kubepods", "containerd", "lxc"} {
		if strings.Contains(content, kw) {
			return true
		}
	}
	return false
}

// modulePresent 在 /proc/modules 内容里查模块名（首列精确匹配）。
func modulePresent(modulesContent, name string) bool {
	for _, line := range strings.Split(modulesContent, "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == name {
			return true
		}
	}
	return false
}
