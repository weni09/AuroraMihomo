package protected

import (
	"fmt"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
)

// 分享的三种载体。
const (
	shareKindSubscription = "subscription"
	shareKindCollection   = "collection"
	shareKindFile         = "file"
)

// 订阅 / 组合 / 文件三类分享的共用实现。
//
// 这三者此前各自散落在对应的管理页面里，用户无法一眼看清
// 「我一共对外开放了哪些链接」。聚合到一处后才能做集中的
// 改名、设有效期、重置凭据与撤销。
//
// 四个端点（列表 / 改名 / 重置 / 撤销）的写操作都以返回最新列表收尾，
// 因此列表构建与解析辅助集中在此，各 logic 只做参数转换与分派。

// listShares 汇总三类分享的当前状态。
func listShares(svcCtx *svc.ServiceContext) (*types.ShareListResp, error) {
	items := make([]types.ShareItem, 0, 16)
	now := time.Now()

	subs, err := svcCtx.Database.GetSubscriptions()
	if err != nil {
		return nil, err
	}
	for _, s := range subs {
		items = append(items, types.ShareItem{
			Kind:       shareKindSubscription,
			Id:         s.ID,
			SourceName: s.Name,
			ShareName:  s.ShareName,
			ShareToken: s.ShareToken,
			Url:        shareURL(s.ShareToken),
			ExpiresAt:  formatExpiry(s.ShareExpiresAt),
			Expired:    expired(s.ShareExpiresAt, now),
			Enabled:    s.Enabled == 1,
			Revoked:    strings.TrimSpace(s.ShareToken) == "",
		})
	}

	cols, err := svcCtx.Database.ListCollections()
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		items = append(items, types.ShareItem{
			Kind:       shareKindCollection,
			Id:         c.ID,
			SourceName: c.Name,
			ShareName:  c.ShareName,
			ShareToken: c.ShareToken,
			Url:        shareURL(c.ShareToken),
			ExpiresAt:  formatExpiry(c.ShareExpiresAt),
			Expired:    expired(c.ShareExpiresAt, now),
			Enabled:    c.Enabled == 1,
			Revoked:    strings.TrimSpace(c.ShareToken) == "",
		})
	}

	files, err := svcCtx.Database.ListFiles()
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		items = append(items, types.ShareItem{
			Kind:       shareKindFile,
			Id:         f.ID,
			SourceName: f.Name,
			ShareName:  f.ShareName,
			ShareToken: f.ShareToken,
			// 文件走独立的直链端点，路径与订阅/组合不同
			Url:       fileURL(f.ShareToken),
			ExpiresAt: formatExpiry(f.ShareExpiresAt),
			Expired:   expired(f.ShareExpiresAt, now),
			// 文件没有启用开关，恒为可用
			Enabled: true,
			Revoked: strings.TrimSpace(f.ShareToken) == "",
		})
	}

	return &types.ShareListResp{Items: items}, nil
}

// updateShare 修改分享的展示名与有效期
func updateShare(svcCtx *svc.ServiceContext, req *types.ShareUpdateReq) (*types.ShareListResp, error) {
	expiresAt, err := parseExpiry(req.ExpiresAt)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.ShareName)

	switch req.Kind {
	case shareKindSubscription:
		err = svcCtx.Database.UpdateSubscriptionShare(req.Id, name, expiresAt)
	case shareKindCollection:
		err = svcCtx.Database.UpdateCollectionShare(req.Id, name, expiresAt)
	case shareKindFile:
		err = svcCtx.Database.UpdateFileShare(req.Id, name, expiresAt)
	default:
		return nil, fmt.Errorf("未知的分享类型: %s", req.Kind)
	}
	if err != nil {
		return nil, err
	}
	return listShares(svcCtx)
}

// resetShare 重置分享凭据。旧链接立即失效，用于凭据外泄后的补救。
func resetShare(svcCtx *svc.ServiceContext, req *types.ShareActionReq) (*types.ShareListResp, error) {
	// 三类分享统一 16 字节（128 bit）。旧实现订阅/组合仅 8 字节，
	// 对免登录公开端点偏短；重置时一并抬到与文件相同的强度。
	token, err := randomToken(shareTokenBytes)
	if err != nil {
		// 随机源异常时必须失败，不能退化为可预测凭据
		return nil, err
	}

	switch req.Kind {
	case shareKindSubscription:
		err = svcCtx.Database.ResetSubscriptionShareToken(req.Id, token)
	case shareKindCollection:
		err = svcCtx.Database.ResetCollectionShareToken(req.Id, token)
	case shareKindFile:
		err = svcCtx.Database.ResetFileShareToken(req.Id, token)
	default:
		return nil, fmt.Errorf("未知的分享类型: %s", req.Kind)
	}
	if err != nil {
		return nil, err
	}
	return listShares(svcCtx)
}

// revokeShare 撤销分享：清空凭据但保留实体本身。
func revokeShare(svcCtx *svc.ServiceContext, req *types.ShareActionReq) (*types.ShareListResp, error) {
	var err error
	switch req.Kind {
	case shareKindSubscription:
		err = svcCtx.Database.ClearSubscriptionShareToken(req.Id)
	case shareKindCollection:
		err = svcCtx.Database.ClearCollectionShareToken(req.Id)
	case shareKindFile:
		err = svcCtx.Database.ClearFileShareToken(req.Id)
	default:
		return nil, fmt.Errorf("未知的分享类型: %s", req.Kind)
	}
	if err != nil {
		return nil, err
	}
	return listShares(svcCtx)
}

// shareURL 返回订阅/组合的分享地址；无凭据时返回空串表示不可访问
func shareURL(token string) string {
	if strings.TrimSpace(token) == "" {
		return ""
	}
	return "/api/v1/share/" + token
}

func fileURL(token string) string {
	if strings.TrimSpace(token) == "" {
		return ""
	}
	return "/api/v1/file/" + token
}

func formatExpiry(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func expired(t time.Time, now time.Time) bool {
	return !t.IsZero() && now.After(t)
}

// parseExpiry 解析有效期入参。空串表示永不过期。
// 同时接受 RFC3339 与 datetime-local 控件产出的 "2006-01-02T15:04" 格式。
func parseExpiry(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// 浏览器 datetime-local 不带时区，按本地时区解释才符合用户预期
	if t, err := time.ParseInLocation("2006-01-02T15:04", s, time.Local); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("无法解析有效期时间: %s", s)
}
