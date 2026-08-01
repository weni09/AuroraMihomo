package version

// AppVersion 在构建时通过 -ldflags "-X auroramihomo/backend/internal/version.AppVersion=vX.Y.Z" 注入。
// 未注入时默认值为 "dev"。
var AppVersion = "dev"

// Get 返回当前系统版本号。
func Get() string {
	if AppVersion == "" {
		return "dev"
	}
	return AppVersion
}
