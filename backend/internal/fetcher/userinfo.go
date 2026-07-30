package fetcher

import (
	"strconv"
	"strings"
)

// UserInfo 是机场通过 subscription-userinfo 响应头返回的流量信息。
// 头部形如：upload=1234; download=5678; total=107374182400; expire=1740000000
type UserInfo struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"` // Unix 秒，0 表示不限期
}

// Used 返回已用流量（上传 + 下载）
func (u UserInfo) Used() int64 {
	return u.Upload + u.Download
}

// IsZero 判断是否没有拿到任何有效流量信息
func (u UserInfo) IsZero() bool {
	return u.Upload == 0 && u.Download == 0 && u.Total == 0 && u.Expire == 0
}

// ParseUserInfo 解析 subscription-userinfo 头部内容，
// 无法识别的字段被忽略，整体解析永不报错。
func ParseUserInfo(raw string) UserInfo {
	var info UserInfo
	for _, seg := range strings.Split(raw, ";") {
		kv := strings.SplitN(seg, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "upload":
			info.Upload = val
		case "download":
			info.Download = val
		case "total":
			info.Total = val
		case "expire":
			info.Expire = val
		}
	}
	return info
}
