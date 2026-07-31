package public

import (
	"context"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type ServeFileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewServeFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ServeFileLogic {
	return &ServeFileLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// ServeFileRaw 按直链 token 返回文件内容及其对应的 Content-Type。
//
// 这里刻意用不可枚举的随机 token 而非文件名寻址：该端点是公开的（供 mihomo
// 内核与外部客户端直接拉取，无法携带 JWT），若以用户自定义的文件名寻址，
// 任何未认证者都能猜名遍历出全部文件内容。
//
// 内容一律经 RenderService 产出，而非直接返回 f.Content：
// mihomo 类型的文件是模板，需要套用其订阅来源的节点渲染。
// 两条分享路径（/file/:token 与 /share/:token）共用同一渲染入口，
// 避免直链看到的内容与参与配置合并的内容不一致。
func (l *ServeFileLogic) ServeFileRaw(token string) (string, string, error) {
	f, err := l.svcCtx.Database.GetFileByToken(token)
	if err != nil {
		return "", "", err
	}
	// 零值表示永不过期
	if !f.ShareExpiresAt.IsZero() && time.Now().After(f.ShareExpiresAt) {
		return "", "", service.ErrShareExpired
	}
	body, err := l.svcCtx.RenderService.RenderFile(l.ctx, f)
	if err != nil {
		return "", "", err
	}
	// 模板渲染出的是 mihomo 配置，Content-Type 应为 YAML 而非按文件名猜测
	if f.ConfigType == model.FileConfigTypeMihomo {
		return body, "text/yaml; charset=utf-8", nil
	}
	return body, fileContentType(f.Type, f.Name), nil
}

// fileContentType 依据文件类型/扩展名给出合适的 Content-Type，
// 此前一律返回 text/plain，导致 yaml、json 文件被客户端按纯文本处理
func fileContentType(typ, name string) string {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "yaml", "yml":
		return "text/yaml; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	case "script", "js", "javascript":
		return "application/javascript; charset=utf-8"
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "text/yaml; charset=utf-8"
	case strings.HasSuffix(lower, ".json"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(lower, ".js"):
		return "application/javascript; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
