package protected

import (
	"testing"

	"auroramihomo/backend/internal/version"
)

// TestSystemStatusLogicAppVersion 验证 version.Get() 能正确返回设置的 AppVersion
func TestSystemStatusLogicAppVersion(t *testing.T) {
	orig := version.AppVersion
	defer func() { version.AppVersion = orig }()

	version.AppVersion = "v0.2.0-test"
	if got := version.Get(); got != "v0.2.0-test" {
		t.Errorf("AppVersion mismatch: got %q, want %q", got, "v0.2.0-test")
	}
}
