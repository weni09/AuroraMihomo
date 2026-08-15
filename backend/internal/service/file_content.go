package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"auroramihomo/backend/internal/fetcher"
	"auroramihomo/backend/internal/model"
)

// 单个远程地址的拉取超时。模板文件常引用规则集等较大文本，
// 但也不能让一个卡死的上游把整次渲染拖到请求超时。
const fileFetchTimeout = 30 * time.Second

// FileContentResult 是一次正文解析的产物。
type FileContentResult struct {
	// Content 为最终正文（本地与各远程段按配置顺序拼接）
	Content string
	// Warnings 记录被跳过的远程地址，供界面提示。
	// 静默策略（quiet）下为空。
	Warnings []string
}

// FileContentResolver 负责把一个文件配置解析成对外提供的正文。
//
// 抽出独立类型的原因：文件正文的取用有三条入口——「立即同步」、
// 直链输出、以及「文件作为远程配置来源」。此前三处各自读 f.Content，
// 一旦引入远程正文与合并顺序就必然出现「同步看到的」与「分享输出的」
// 不一致。现在三条路径共用本解析器。
type FileContentResolver struct {
	client *fetcher.Client
}

func NewFileContentResolver() *FileContentResolver {
	return &FileContentResolver{client: fetcher.New(fileFetchTimeout)}
}

// SetRawCDNProviders 把 raw 加速源列表推给内部 fetcher。
// 文件远程地址（模板转换等）可能是 raw.githubusercontent.com 直链。
func (r *FileContentResolver) SetRawCDNProviders(providers []string) {
	r.client.SetRawCDNProviders(providers)
}

// SetProxyURLFunc 把本地 mihomo 代理查询回调转发给内部 fetcher。
func (r *FileContentResolver) SetProxyURLFunc(fn func() string) {
	r.client.SetProxyURLFunc(fn)
}

// Resolve 按文件配置解析正文。
//
// 取用规则（对齐官方 Sub-Store 的 source / mergeSources 语义）：
//   - 不合并时只取 SourceMode 指定的一侧；
//   - localFirst / remoteFirst 时两侧都取，仅先后顺序不同。
//
// 远程地址支持多行，按行拆分后并发拉取，但结果严格按配置顺序拼接——
// 规则片段与配置模板都对顺序敏感，不能按返回快慢排列。
func (r *FileContentResolver) Resolve(ctx context.Context, f *model.SubFile) (FileContentResult, error) {
	if f == nil {
		return FileContentResult{}, fmt.Errorf("文件不存在")
	}

	mode := normalizeSourceMode(f.SourceMode)
	merge := f.MergeSources
	useLocal := mode == model.FileSourceLocal || merge == model.FileMergeLocalFirst || merge == model.FileMergeRemoteFirst
	useRemote := mode == model.FileSourceRemote || merge == model.FileMergeLocalFirst || merge == model.FileMergeRemoteFirst

	var remoteParts []string
	var warnings []string
	if useRemote {
		urls := SplitFileURLs(f.SyncURL)
		if len(urls) == 0 {
			return FileContentResult{}, fmt.Errorf("文件「%s」未配置远程地址", f.Name)
		}
		var err error
		remoteParts, warnings, err = r.fetchAll(ctx, f, urls)
		if err != nil {
			return FileContentResult{}, err
		}
	}

	// 顺序由 mergeSources 决定；不合并时只有一侧有内容，顺序无影响
	parts := make([]string, 0, len(remoteParts)+1)
	if merge == model.FileMergeRemoteFirst {
		parts = append(parts, remoteParts...)
		if useLocal {
			parts = append(parts, f.Content)
		}
	} else {
		if useLocal {
			parts = append(parts, f.Content)
		}
		parts = append(parts, remoteParts...)
	}

	return FileContentResult{Content: joinFileParts(parts), Warnings: warnings}, nil
}

// fetchAll 并发拉取全部远程地址，返回与入参同序的正文片段。
//
// 并发是必要的：多引用几个规则集时串行拉取会把渲染耗时叠加成好几秒，
// 而分享链接是面向客户端的实时请求。
func (r *FileContentResolver) fetchAll(ctx context.Context, f *model.SubFile, urls []string) ([]string, []string, error) {
	bodies := make([]string, len(urls))
	errs := make([]error, len(urls))

	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			// 每个地址各自限时，避免一个慢上游拖垮整批
			reqCtx, cancel := context.WithTimeout(ctx, fileFetchTimeout)
			defer cancel()
			data, err := r.client.FetchWithUA(reqCtx, u, f.UserAgent)
			if err != nil {
				errs[i] = err
				return
			}
			bodies[i] = string(data)
		}(i, u)
	}
	wg.Wait()

	strategy := f.IgnoreFailedRemote
	parts := make([]string, 0, len(urls))
	warnings := make([]string, 0)
	for i, err := range errs {
		if err == nil {
			parts = append(parts, bodies[i])
			continue
		}
		switch strategy {
		case model.FileFailQuiet:
			// 静默跳过：用户已明确表示不关心个别地址的可用性
		case model.FileFailSkip:
			warnings = append(warnings, fmt.Sprintf("已跳过 %s：%v", urls[i], err))
		default:
			// 默认从严：静默产出缺内容的文件，客户端会拿到不完整配置而不自知
			return nil, nil, fmt.Errorf("拉取 %s 失败: %w", urls[i], err)
		}
	}

	// 全部地址都失败时即便配了跳过策略也不能算成功：
	// 那只会产出一个空文件，覆盖掉原有内容
	if len(parts) == 0 {
		return nil, nil, fmt.Errorf("文件「%s」的全部远程地址均拉取失败", f.Name)
	}
	return parts, warnings, nil
}

// SplitFileURLs 按行拆分远程地址列表，去空白并丢弃空行与注释行。
// 支持 # 开头的注释，便于用户临时停用某个地址而不必删掉它。
func SplitFileURLs(raw string) []string {
	lines := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' })
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// normalizeSourceMode 把空值按 local 解释。
// 存量文件没有该字段，其正文就是编辑器里的内容。
func normalizeSourceMode(mode string) string {
	if mode == model.FileSourceRemote {
		return model.FileSourceRemote
	}
	return model.FileSourceLocal
}

// joinFileParts 用换行拼接各段，跳过空白段。
//
// 段间补换行是必要的：上游文件末尾常无换行，直接相接会把前一段的
// 末行与后一段的首行粘成一行。但只规整段与段之间的接缝，
// 最后一段原样保留——单段（最常见的纯本地文件）必须逐字输出，
// 末尾换行属于文件内容的一部分，擅自去掉就不是「原样输出」了。
func joinFileParts(parts []string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		return ""
	}
	var b strings.Builder
	for i, p := range kept {
		if i == len(kept)-1 {
			b.WriteString(p)
			break
		}
		// 去掉原有的尾部换行再补一个，避免段间出现多个空行
		b.WriteString(strings.TrimRight(p, "\r\n"))
		b.WriteString("\n")
	}
	return b.String()
}
