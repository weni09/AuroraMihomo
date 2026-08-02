package adguard

import "testing"

func TestParseSSUsersForPort(t *testing.T) {
	ss := `Netid State  Recv-Q Send-Q  Local Address:Port  Peer Address:PortProcess
udp   UNCONN 0      0               *:5353            *:*    users:(("AdGuardHome",pid=10117,fd=6))
tcp   LISTEN 0      4096      127.0.0.1:3000      0.0.0.0:*    users:(("AdGuardHome",pid=10117,fd=10))
tcp   LISTEN 0      4096            *:1053            *:*    users:(("mihomo",pid=9619,fd=8))
`
	if g := parseSSUsersForPort(ss, 5353); g != "AdGuardHome" {
		t.Fatalf("5353 want AdGuardHome got %q", g)
	}
	if g := parseSSUsersForPort(ss, 1053); g != "mihomo" {
		t.Fatalf("1053 want mihomo got %q", g)
	}
	if g := parseSSUsersForPort(ss, 53); g != "" {
		t.Fatalf("53 want empty got %q", g)
	}
}

func TestIsAdGuardProcessName(t *testing.T) {
	if !isAdGuardProcessName("AdGuardHome") {
		t.Fatal("AdGuardHome")
	}
	if isAdGuardProcessName("mihomo") {
		t.Fatal("mihomo should be false")
	}
}

func TestCheckDNSPortAvailability_Free(t *testing.T) {
	// 选一个高位端口，极大概率空闲
	port := 58432
	av, _, err := CheckDNSPortAvailability(port, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if av != PortFree {
		t.Fatalf("want free got %d", av)
	}
}
