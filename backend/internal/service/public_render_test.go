package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/substore"
)

// 公开分享路径必须跳过 script：死循环脚本若仍执行会卡满 scriptTimeout。
func TestPublicShareStripsScriptOperators(t *testing.T) {
	svc, db := newTestRenderService(t)

	ops, err := json.Marshal([]substore.PipelineOperator{{
		Type:    substore.OpScript,
		Enabled: true,
		Payload: map[string]interface{}{"script": "while(true){}"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	sub := &model.Subscription{
		Name:       "s",
		ShareToken: "pubtok1",
		Enabled:    1,
		Content:    "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#N1\n",
		Operators:  string(ops),
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	res, err := svc.RenderByToken(context.Background(), "pubtok1", "mihomo-yaml", "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("公开渲染应成功（剥离 script 后）: %v", err)
	}
	if res == nil || res.Body == "" {
		t.Fatal("应有输出 body")
	}
	// 若仍执行 while(true)，会接近 5s 超时；剥离后应远小于该值
	if elapsed > 2*time.Second {
		t.Fatalf("公开路径疑似仍执行了 script，耗时 %v", elapsed)
	}
	if !strings.Contains(res.Body, "N1") && !strings.Contains(res.Body, "1.1.1.1") {
		// mihomo yaml 至少应含节点信息之一
		t.Logf("body=%q", res.Body)
	}
}

func TestPublicFileRejectsJSTemplate(t *testing.T) {
	svc, db := newTestRenderService(t)
	sub := &model.Subscription{
		Name: "src", Enabled: 1,
		Content: "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#N1\n",
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	f := &model.SubFile{
		Name:         "jsfile",
		ShareToken:   "filetokjs",
		ConfigType:   model.FileConfigTypeMihomo,
		TemplateLang: model.TemplateLangJS,
		Content:      "function main(config){ return config }",
		SourceType:   "subscription",
		SourceID:     sub.ID,
	}
	if err := db.SaveFile(f); err != nil {
		t.Fatal(err)
	}

	_, _, err := svc.RenderPublicFileByToken(context.Background(), "filetokjs")
	if err == nil {
		t.Fatal("公开 JS 模板应被拒绝")
	}
	if !errors.Is(err, ErrPublicJSTemplate) {
		t.Fatalf("期望 ErrPublicJSTemplate，实际 %v", err)
	}

	// 鉴权路径仍允许（语法/运行由 RenderMihomoOverride 处理）
	if _, err := svc.RenderFile(context.Background(), f); err != nil {
		t.Fatalf("非公开 RenderFile 应允许 JS 模板: %v", err)
	}
}

func TestPublicFileUsesCachedRender(t *testing.T) {
	svc, db := newTestRenderService(t)
	f := &model.SubFile{
		Name:       "plain",
		ShareToken: "filetokplain",
		ConfigType: model.FileConfigTypeFile,
		Content:    "rule-providers: {}\n",
		Type:       "yaml",
	}
	if err := db.SaveFile(f); err != nil {
		t.Fatal(err)
	}
	body1, _, err := svc.RenderPublicFileByToken(context.Background(), "filetokplain")
	if err != nil {
		t.Fatal(err)
	}
	body2, _, err := svc.RenderPublicFileByToken(context.Background(), "filetokplain")
	if err != nil {
		t.Fatal(err)
	}
	if body1 != body2 {
		t.Fatalf("缓存应返回相同内容")
	}
}
