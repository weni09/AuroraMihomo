package version

import "testing"

func TestGetVersion(t *testing.T) {
	if Get() == "" {
		t.Errorf("expected non-empty version string, got empty")
	}
}
