package adguard

import (
	"fmt"
	"net"
	"testing"
)

func TestUDPPortInUse_FreeHighPort(t *testing.T) {
	// 先占一个临时口再释放，得到一个确定空闲的端口
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法绑定临时 UDP: %v", err)
	}
	addr := pc.LocalAddr().(*net.UDPAddr)
	port := addr.Port
	_ = pc.Close()

	if UDPPortInUse(port) {
		t.Fatalf("刚释放的 UDP :%d 应判定为空闲", port)
	}
}

func TestUDPPortInUse_BusyPort(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法绑定临时 UDP: %v", err)
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port

	// 在部分平台上 :port 与 127.0.0.1:port 可能不互斥；
	// 再以 :port 方式占用一次验证。
	pc2, err2 := net.ListenPacket("udp", fmt.Sprintf(":%d", port))
	if err2 != nil {
		// 已被 127.0.0.1 占用则 UDPPortInUse 应返回 true
		if !UDPPortInUse(port) {
			t.Fatalf("已被占用的 UDP :%d 应判定为占用 (err2=%v)", port, err2)
		}
		return
	}
	defer pc2.Close()
	if !UDPPortInUse(port) {
		t.Fatalf("ListenPacket 成功占用后 UDP :%d 应判定为占用", port)
	}
}

func TestUDPPortInUse_InvalidPort(t *testing.T) {
	if !UDPPortInUse(0) || !UDPPortInUse(-1) || !UDPPortInUse(70000) {
		t.Fatal("非法端口应视为占用/不可用")
	}
}
