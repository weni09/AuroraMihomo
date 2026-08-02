package service

import (
	"strings"
	"testing"
)

func TestEnsureBaseTUNGatewayDefaults(t *testing.T) {
	type tc struct {
		name     string
		src      string
		changed  bool
		wantAR   bool
		wantNoTP bool
	}
	cases := []tc{
		{
			name:     "tun off",
			src:      "tun:\n  enable: false\n  auto-redirect: false\n",
			changed:  false,
			wantAR:   false,
			wantNoTP: false,
		},
		{
			name:     "tun on undeclared ar",
			src:      "tun:\n  enable: true\n  stack: mixed\n",
			changed:  true,
			wantAR:   true,
			wantNoTP: false,
		},
		{
			name:     "tun on explicit false ar",
			src:      "tun:\n  enable: true\n  auto-route: true\n  auto-redirect: false\n",
			changed:  false,
			wantAR:   false,
			wantNoTP: false,
		},
		{
			name:     "tun on explicit true ar",
			src:      "tun:\n  enable: true\n  auto-route: true\n  auto-redirect: true\n",
			changed:  false,
			wantAR:   true,
			wantNoTP: false,
		},
		{
			name:     "tun on drop tproxy-port",
			src:      "tproxy-port: 7893\ntun:\n  enable: true\n  auto-route: true\n  auto-redirect: true\n",
			changed:  true,
			wantAR:   true,
			wantNoTP: true,
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			out, changed, err := ensureBaseTUNGatewayDefaults(c.src)
			if err != nil {
				t.Fatal(err)
			}
			if changed != c.changed {
				t.Fatalf("changed=%v want %v out=%q", changed, c.changed, out)
			}
			set, en, err := readTUNAutoRedirect(out)
			if err != nil {
				t.Fatal(err)
			}
			if c.wantAR && (!set || !en) {
				t.Fatalf("want auto-redirect true set=%v en=%v out=%q", set, en, out)
			}
			if c.name == "tun on explicit false ar" && (!set || en) {
				t.Fatalf("want explicit false set=%v en=%v out=%q", set, en, out)
			}
			if c.wantNoTP && strings.Contains(out, "tproxy-port:") {
				t.Fatalf("should drop tproxy-port out=%q", out)
			}
		})
	}
}
