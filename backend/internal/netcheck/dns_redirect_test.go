package netcheck

import (
	"strings"
	"testing"
)

func TestBuildAGHDNSRedirectNFT(t *testing.T) {
	script, err := BuildAGHDNSRedirectNFT(DNSRedirectParams{DNSPort: 1053, EnableIPv6: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"table inet aurora_agh_dns",
		"th dport 53 redirect to :1053",
		"ip daddr != 127.0.0.0/8",
		"ip6 daddr != ::1/128",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestBuildAGHDNSRedirectNFT_RejectPort53(t *testing.T) {
	_, err := BuildAGHDNSRedirectNFT(DNSRedirectParams{DNSPort: 53})
	if err == nil {
		t.Fatal("expected error for port 53")
	}
}

func TestBuildAGHDNSRedirectNFT_RejectBadPort(t *testing.T) {
	_, err := BuildAGHDNSRedirectNFT(DNSRedirectParams{DNSPort: 0})
	if err == nil {
		t.Fatal("expected error")
	}
}
