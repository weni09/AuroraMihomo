package netcheck

// 本文件放平台无关的纯解析函数：入参是已读出的文本或注入的探针，
// 不直接触碰真实文件系统。
//
// 单独成文件（不带 //go:build linux）的原因是可测性：解析 /proc/self/status
// 的 capability 位、/etc/os-release、/proc/modules 这些逻辑本身与运行平台
// 无关，放在带 linux tag 的文件里就只能在 Linux 上跑测试，而开发与 CI
// 未必在 Linux 上进行。

import (
	"net"
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

// loopbackDNSStub 从 resolv.conf 内容里找出指向回环的 nameserver。
//
// 为什么要专门找它：本机 DNS 劫持规则刻意排除回环目标（mihomo 自己的 DNS
// 就监听在回环上，不排除会自环）。于是 systemd-resolved 这类把 nameserver
// 指向 127.0.0.53 的机器上，本机的 DNS 查询压根不会被劫持，域名类分流规则
// 对本机流量全部失效。这种"看着开了其实没生效"的状态必须被如实报出来。
//
// 返回第一个命中的地址而非全部：告警文案只需要举出一个具体地址让用户能对上，
// 列一串反而更难读。
func loopbackDNSStub(resolvConf string) string {
	for _, line := range strings.Split(resolvConf, "\n") {
		line = strings.TrimSpace(line)
		// resolv.conf 的注释是 # 或 ;，两者都要跳过，否则被注释掉的
		// nameserver 会被当成生效配置
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		// 地址可能带 %zone 后缀（v6 链路本地），net.ParseIP 不认，先切掉
		addr, _, _ := strings.Cut(fields[1], "%")
		if ip := net.ParseIP(addr); ip != nil && ip.IsLoopback() {
			return fields[1]
		}
	}
	return ""
}

// hasIPv6DefaultRoute 判断 /proc/net/ipv6_route 里有没有默认路由。
//
// 该文件每行的前两列是"目的地址(32 位十六进制) 前缀长度(十六进制)"，
// 默认路由即目的地址全 0、前缀长度 0。直接按文本比对而不解析成 net.IP：
// 只需要判断"是不是 ::/0"，转换一趟没有额外收益。
//
// 刻意跳过 lo 上的路由：内核会为回环装一条 ::/0 的 unreachable 路由，
// 把它算进来会让没有 v6 出网的机器被误判成有。
func hasIPv6DefaultRoute(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		// 目的地址、前缀长度、…、设备名（末列）。列数不足说明不是数据行
		if len(fields) < 10 {
			continue
		}
		if fields[0] != strings.Repeat("0", 32) || fields[1] != "00" {
			continue
		}
		if dev := fields[len(fields)-1]; dev == "lo" {
			continue
		}
		return true
	}
	return false
}

// hasGlobalIPv6Addr 判断 /proc/net/if_inet6 里有没有全局单播 v6 地址。
//
// 每行格式：地址(32 位十六进制) 接口序号 前缀长度 scope 标志 设备名。
// scope 为 "00" 表示全局；回环是 "10"、链路本地是 "20"。只有全局地址
// 才意味着这台机器真能往外走 v6——只有链路本地地址的机器下发 v6 规则
// 等于制造黑洞。
func hasGlobalIPv6Addr(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		if fields[3] != "00" {
			continue
		}
		if dev := fields[len(fields)-1]; dev == "lo" {
			continue
		}
		return true
	}
	return false
}
