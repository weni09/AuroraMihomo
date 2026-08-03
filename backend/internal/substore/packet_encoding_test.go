package substore

import (
	"strings"
	"testing"
)

func TestApplyPacketEncoding_VLESSDefaultXUDP(t *testing.T) {
	n := Node{Name: "v", Type: "vless", Server: "a.com", Port: 443, Extra: map[string]interface{}{"uuid": "u"}}
	item := map[string]interface{}{"name": "v", "type": "vless"}
	applyPacketEncoding(n, item)
	if item["packet-encoding"] != "xudp" {
		t.Fatalf("vless 缺省应为 xudp，实际 %#v", item["packet-encoding"])
	}
}

func TestApplyPacketEncoding_PreserveExisting(t *testing.T) {
	n := Node{Name: "v", Type: "vless", Server: "a.com", Port: 443,
		Extra: map[string]interface{}{"packet-encoding": "packetaddr"}}
	item := map[string]interface{}{"packet-encoding": "packetaddr"}
	applyPacketEncoding(n, item)
	if item["packet-encoding"] != "packetaddr" {
		t.Fatalf("已有值不应被覆盖: %#v", item["packet-encoding"])
	}
}

func TestApplyPacketEncoding_FromXUDPFlag(t *testing.T) {
	n := Node{Name: "m", Type: "vmess", Server: "a.com", Port: 443,
		Extra: map[string]interface{}{"xudp": true}}
	item := map[string]interface{}{"type": "vmess"}
	// 先模拟 nodeToProxyMap 拷贝 Extra
	for k, v := range n.Extra {
		item[k] = v
	}
	// 拷贝后若仍无 packet-encoding，由 xudp 映射
	delete(item, "packet-encoding")
	applyPacketEncoding(n, item)
	if item["packet-encoding"] != "xudp" {
		t.Fatalf("xudp 标志应映射为 packet-encoding=xudp，实际 %#v", item["packet-encoding"])
	}
}

func TestNodesToMihomoYAML_IncludesPacketEncoding(t *testing.T) {
	out, err := NodesToMihomoYAML([]Node{{
		Name: "n1", Type: "vless", Server: "a.com", Port: 443, UDP: true,
		Extra: map[string]interface{}{"uuid": "u1", "tls": true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "packet-encoding: xudp") {
		t.Fatalf("mihomo 输出应含 packet-encoding: xudp\n%s", out)
	}
}
