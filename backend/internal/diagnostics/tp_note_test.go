package diagnostics

import (
	"context"
	"testing"
	"time"
)

// 验证透明代理开启时 direct 结果标注（TUN 模式模拟）
func TestTransparentNoteIntegrationTUN(t *testing.T) {
	svc := New(Config{
		MaxConcurrent: 3,
		ResultTTL:     time.Minute,
		ProbeTimeout:  time.Second,
		Probes: map[string]Probe{
			TypeTCP: ProbeFunc(func(ctx context.Context, tgt DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
				return ProbeResult{Target: tgt.Target, Type: TypeTCP, Path: path, Status: StatusSuccess}
			}),
		},
		CapNetAdminFn:       func() bool { return true }, // TUN 下有 CAP 也标注
		TransparentStatusFn: func() (bool, string) { return true, "tun" },
	})
	defer svc.Close()
	id, err := svc.Run(context.Background(), DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: TypeTCP, Target: "x", Port: 443}},
		Path:    "both",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		snap, ok := svc.GetResult(id)
		if ok && snap.Done {
			for _, r := range snap.Results {
				if r.Path == "direct" {
					note, _ := r.Detail.(map[string]interface{})["transparentNote"].(string)
					if note != transparentNoteTUN {
						t.Fatalf("TUN direct 应标 %q, got %q", transparentNoteTUN, note)
					}
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("超时")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
