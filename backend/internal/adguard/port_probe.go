package adguard

import (
	"fmt"
	"net"
)

// UDPPortInUse 探测 UDP :port 是否已被占用。
// 尝试 ListenPacket；失败视为占用（含权限不足导致无法绑定的情况）。
// 成功绑定后立即关闭，返回 false。
func UDPPortInUse(port int) bool {
	if port <= 0 || port > 65535 {
		return true
	}
	pc, err := net.ListenPacket("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	_ = pc.Close()
	return false
}
