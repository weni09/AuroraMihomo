package protected

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/applog"
	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/netcheck"
	"auroramihomo/backend/internal/service"
	"auroramihomo/backend/internal/substore"
	"auroramihomo/backend/internal/updater"
)

func errInvalid(msg string) error { return errors.New(msg) }

func nowTime() time.Time { return time.Now() }

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// randomToken 生成 n 字节的随机十六进制凭据，用于订阅/组合/文件的分享链接。
//
// 必须返回 error：此前实现丢弃了 rand.Read 的错误，随机源异常时
// b 保持全零，于是产出固定的 "0000..." token。分享凭据一旦可预测，
// 任何人都能直接拉取到订阅内容（含机场账号信息）。
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成随机凭据失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func toSubscriptionType(s model.Subscription) types.Subscription {
	last := ""
	if !s.LastUpdate.IsZero() {
		last = s.LastUpdate.Format(time.RFC3339)
	}
	// 设计 §5：展示该订阅缓存的节点数量
	nodeCount := 0
	if s.CachedNodes != "" {
		var nodes []map[string]interface{}
		if json.Unmarshal([]byte(s.CachedNodes), &nodes) == nil {
			nodeCount = len(nodes)
		}
	}

	var ops []types.PipelineOperator
	if s.Operators != "" {
		_ = jsonUnmarshal([]byte(s.Operators), &ops)
	}
	if ops == nil {
		ops = []types.PipelineOperator{}
	}

	return types.Subscription{
		Id:           s.ID,
		Name:         s.Name,
		Url:          s.URL,
		Content:      s.Content,
		Enabled:      s.Enabled == 1,
		Interval:     s.Interval,
		Status:       s.Status,
		ErrorMessage: s.ErrorMessage,
		LastUpdate:   last,
		NodeCount:    nodeCount,
		UserAgent:    s.UserAgent,
		Operators:    ops,
		ShareToken:   s.ShareToken,
		Traffic:      toTrafficInfo(s),
	}
}

// toTrafficInfo 组装机场流量信息，没有任何数据时返回 nil 以便前端隐藏该列
func toTrafficInfo(s model.Subscription) *types.TrafficInfo {
	if s.Upload == 0 && s.Download == 0 && s.Total == 0 && s.Expire == 0 {
		return nil
	}
	expireAt := ""
	if s.Expire > 0 {
		expireAt = time.Unix(s.Expire, 0).Format(time.RFC3339)
	}
	return &types.TrafficInfo{
		Upload:   s.Upload,
		Download: s.Download,
		Used:     s.Upload + s.Download,
		Total:    s.Total,
		Expire:   s.Expire,
		ExpireAt: expireAt,
	}
}

func toFileType(f model.SubFile) types.SubFile {
	expiresAt := ""
	if !f.ShareExpiresAt.IsZero() {
		expiresAt = f.ShareExpiresAt.Format(time.RFC3339)
	}
	// 存量数据 ConfigType 为空，按原样输出解释，保持升级前行为
	configType := f.ConfigType
	if configType == "" {
		configType = model.FileConfigTypeFile
	}
	// 存量数据 SourceMode 为空，其正文就是编辑器里的内容，按 local 解释
	sourceMode := f.SourceMode
	if sourceMode == "" {
		sourceMode = model.FileSourceLocal
	}
	// 存量数据 TemplateLang 为空，按原有的 Go 模板语法解释，保持升级前行为
	templateLang := firstNonEmpty(f.TemplateLang, model.TemplateLangGo)
	return types.SubFile{
		Id:                 f.ID,
		Name:               f.Name,
		Content:            f.Content,
		Type:               f.Type,
		SyncUrl:            f.SyncURL,
		SourceMode:         sourceMode,
		MergeSources:       f.MergeSources,
		IgnoreFailedRemote: f.IgnoreFailedRemote,
		UserAgent:          f.UserAgent,
		ShareToken:         f.ShareToken,
		ConfigType:         configType,
		SourceType:         f.SourceType,
		SourceId:           f.SourceID,
		TemplateLang:       templateLang,
		TrafficUrl:         f.TrafficURL,
		ShareName:          f.ShareName,
		ExpiresAt:          expiresAt,
	}
}

// loadRewriteRules 恒返回 nil：「全局改写规则」已移除，
// 改写改由各订阅/组合自身的处理管道承担（见 service.RenderService.RewriteRules）。
// 保留该函数以维持「测试构建 / 转换预览」与真实分享路径的调用形态一致。
func loadRewriteRules(_ *svc.ServiceContext) []substore.RewriteRule {
	return nil
}

func toCollectionType(svcCtx *svc.ServiceContext, c model.SubCollection) types.Collection {
	items, _ := svcCtx.Database.ListCollectionItems(c.ID)
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.SubscriptionID)
	}
	var ops []types.PipelineOperator
	if c.Operators != "" {
		_ = jsonUnmarshal([]byte(c.Operators), &ops)
	}
	if ops == nil {
		ops = []types.PipelineOperator{}
	}
	return types.Collection{
		Id:         c.ID,
		Name:       c.Name,
		Enabled:    c.Enabled == 1,
		Operators:  ops,
		ShareToken: c.ShareToken,
		SubIds:     ids,
	}
}

// validateFileSources 校验文件的正文来源配置是否自洽。
//
// 放在字段全部落定之后统一校验：来源方式、合并方式与远程地址可能在
// 同一次请求里一起改动，逐字段校验会误判中间状态。
func validateFileSources(f *model.SubFile) error {
	switch f.SourceMode {
	case "", model.FileSourceLocal, model.FileSourceRemote:
	default:
		return errInvalid("来源方式只能是 local 或 remote")
	}
	switch f.MergeSources {
	case model.FileMergeNone, model.FileMergeLocalFirst, model.FileMergeRemoteFirst:
	default:
		return errInvalid("合并方式只能为空、localFirst 或 remoteFirst")
	}
	switch f.IgnoreFailedRemote {
	case model.FileFailStrict, model.FileFailSkip, model.FileFailQuiet:
	default:
		return errInvalid("远程失败处理只能为空、enabled 或 quiet")
	}

	needsRemote := f.SourceMode == model.FileSourceRemote ||
		f.MergeSources == model.FileMergeLocalFirst ||
		f.MergeSources == model.FileMergeRemoteFirst
	if needsRemote && len(service.SplitFileURLs(f.SyncURL)) == 0 {
		return errInvalid("使用远程内容时必须填写至少一个远程地址")
	}
	// 纯本地且正文为空的文件对外只会输出空内容，多半是漏填而非本意
	if !needsRemote && strings.TrimSpace(f.Content) == "" {
		return errInvalid("本地来源的文件内容不能为空")
	}
	return nil
}

// toUpdateSettings 组装更新设置响应。
// 读、写两个接口返回同一形态，集中在此避免字段增减时漏改一处。
func toUpdateSettings(svcCtx *svc.ServiceContext, st updater.RuntimeSettings) *types.UpdateSettings {
	return &types.UpdateSettings{
		AutoUpdateEnabled: st.AutoUpdateEnabled,
		AutoUpdateCron:    st.AutoUpdateCron,
		CDNProviders:      st.CDNProviders,
		UseMihomoProxy:    st.UseMihomoProxy,
		MihomoProxyUrl:    st.MihomoProxyURL,
		MihomoPath:        st.MihomoPath,
		ZashboardDir:      st.ZashboardDir,
		MihomoPresent:     st.MihomoPresent,
		ZashboardPresent:  st.ZashboardPresent,
		MihomoVersion:     st.MihomoVersion,
		ZashboardVersion:  st.ZashboardVersion,
		DefaultCDN:        svcCtx.Updater.DefaultCDNProviders(),
		// 日志保留天数不属于 updater 的运行期设置（那管的是组件更新），
		// 直接从 applog 读当前生效值
		LogRetentionDays:  applog.RetentionDays(),
		LogCleanupCron:    svcCtx.SettingsService.LogCleanupCron(),
		LogCleanupEnabled: svcCtx.SettingsService.LogCleanupEnabled(),
	}
}

// encodeOperators 把请求中的处理管道序列化为可持久化的 JSON 字符串
func encodeOperators(ops []types.PipelineOperator) string {
	if len(ops) == 0 {
		return ""
	}
	b, err := jsonMarshal(ops)
	if err != nil {
		return ""
	}
	return string(b)
}

// toTransparentStatus 组装透明代理状态响应。
//
// 三个端点（查询 / 修改 / 确认）返回同一形态，集中在此避免字段增减时漏改。
// 每次都重新跑一遍环境检测而不缓存：用户可能刚按提示装完依赖就回来点开关，
// 缓存会让他看到过期的"不可用"结论。探测本身只是读几个文件与 LookPath，
// 开销可以忽略。
func toTransparentStatus(st *service.TransparentState, env *netcheck.Report) *types.TransparentStatusResp {
	resp := &types.TransparentStatusResp{
		Enabled:            st.Enabled,
		Mode:               st.Mode,
		PendingConfirm:     st.PendingConfirm,
		SecondsLeft:        st.SecondsLeft,
		TProxyPort:         st.TProxyPort,
		TUNStack:           st.TUNStack,
		PortConfiguredOnly: st.PortConfiguredOnly,
		RulesOutOfSync:     st.RulesOutOfSync,
		Env: types.TransparentEnvReport{
			OS:                  env.OS,
			Arch:                env.Arch,
			Kernel:              env.Kernel,
			Distro:              env.Distro,
			PackageManager:      env.PackageManager,
			Root:                env.Root,
			CapNetAdmin:         env.CapNetAdmin,
			CapNetRaw:           env.CapNetRaw,
			CapNetAdminBounding: env.CapNetAdminBounding,
			InContainer:         env.InContainer,
			HostNetwork:         env.HostNetwork,
			TunDevice:           env.TunDevice,
			// 切片显式初始化为空而非 nil：前端拿到 null 需要额外判空，
			// 而 JSON 里 [] 可以直接遍历
			Modes:    make([]types.TransparentModeStatus, 0, len(env.Modes)),
			Warnings: env.Warnings,
		},
	}
	if resp.Env.Warnings == nil {
		resp.Env.Warnings = []string{}
	}
	for _, m := range env.Modes {
		missing := m.Missing
		if missing == nil {
			missing = []string{}
		}
		resp.Env.Modes = append(resp.Env.Modes, types.TransparentModeStatus{
			Mode:        string(m.Mode),
			Available:   m.Available,
			Reason:      m.Reason,
			Missing:     missing,
			InstallHint: m.InstallHint,
		})
	}
	return resp
}

// toTransparentProvision 组装环境准备响应。
//
// env 用的是执行之后重新探测的报告：装完包后 nft/iproute2 才出现在 PATH 上，
// 沿用旧报告会让界面停在"提示成功但模式仍显示不可用"的割裂状态。
func toTransparentProvision(res *netcheck.ProvisionResult,
	env *netcheck.Report) *types.TransparentProvisionResp {
	resp := &types.TransparentProvisionResp{
		Success:       res.Success,
		Message:       res.Message,
		NotPersistent: res.NotPersistent,
		// 切片显式初始化为空而非 nil，理由同 toTransparentStatus：
		// 前端拿到 null 要额外判空，而 [] 可以直接遍历
		Steps:          make([]types.TransparentProvisionStep, 0, len(res.Steps)),
		ManualCommands: res.ManualCommands,
	}
	if resp.ManualCommands == nil {
		resp.ManualCommands = []string{}
	}
	for _, s := range res.Steps {
		resp.Steps = append(resp.Steps, types.TransparentProvisionStep{
			Name:    s.Name,
			Command: s.Command,
			Success: s.Success,
			Detail:  s.Detail,
			Skipped: s.Skipped,
		})
	}
	if env != nil {
		resp.Env = toTransparentStatus(&service.TransparentState{}, env).Env
	}
	return resp
}
