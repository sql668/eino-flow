package mineru_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"einoflow-backend/internal/engine/rag/loader"
	"einoflow-backend/internal/engine/rag/loader/mineru"
)

// 集成测试：需要真实的 MinerU 服务在跑，因此用环境变量开启，避免 go test 默认依赖外部服务。
//
//	MINERU_TEST_BASE_URL=http://127.0.0.1:8000 go test ./internal/engine/rag/loader/mineru/ -run Integration -v
//
// 可用 MINERU_TEST_FILE 指定样本文件，默认用仓库里的产品手册（含表格）。
const (
	envBaseURL = "MINERU_TEST_BASE_URL"
	envFile    = "MINERU_TEST_FILE"
)

// defaultTestFile 从本测试文件所在目录回溯到仓库根目录下的样本文件。
func defaultTestFile(t *testing.T) string {
	t.Helper()
	if custom := os.Getenv(envFile); custom != "" {
		return custom
	}
	// mineru -> loader -> rag -> engine -> internal -> backend -> 仓库根
	return filepath.Join("..", "..", "..", "..", "..", "..", "docs", "rag-test", "万用表RS-12的使用.pdf")
}

func TestLoader_Integration(t *testing.T) {
	baseURL := os.Getenv(envBaseURL)
	if baseURL == "" {
		t.Skipf("未设置 %s，跳过集成测试", envBaseURL)
	}

	filePath := defaultTestFile(t)
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取样本文件 %s 失败: %v", filePath, err)
	}

	client := mineru.NewClient(mineru.ClientOptions{
		BaseURL:      baseURL,
		PollInterval: time.Second,
		PollTimeout:  5 * time.Minute,
	})
	instance := mineru.New(client, "basic")

	start := time.Now()
	doc, err := instance.Load(context.Background(), loader.LoadRequest{
		Filename: filepath.Base(filePath),
		Content:  content,
		MIMEType: "application/pdf",
	})
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	elapsed := time.Since(start)

	if len(doc.Blocks) == 0 {
		t.Fatal("解析结果没有任何内容块")
	}

	// 元信息必须完整：这几个字段要落库用于判断是否需要重建索引
	if doc.Meta.Parser != "mineru" {
		t.Errorf("Meta.Parser = %q", doc.Meta.Parser)
	}
	if doc.Meta.ParserVersion == "" {
		t.Error("Meta.ParserVersion 为空，无法判断解析器是否升级")
	}
	if doc.Meta.Tier != "basic" {
		t.Errorf("Meta.Tier = %q, 期望 basic", doc.Meta.Tier)
	}
	if mode := doc.Meta.ParseMode; mode != "txt" && mode != "ocr" {
		t.Errorf("Meta.ParseMode = %q, 期望 txt 或 ocr", mode)
	}
	if doc.Meta.PageCount <= 0 {
		t.Errorf("Meta.PageCount = %d", doc.Meta.PageCount)
	}

	// 页码必须从 1 开始（MinerU 内部是 0 基，映射时已转换）
	minPage := doc.Blocks[0].Page
	for _, block := range doc.Blocks {
		if block.Page < 1 {
			t.Fatalf("出现小于 1 的页码: %d", block.Page)
		}
		if block.Page < minPage {
			minPage = block.Page
		}
	}
	if minPage != 1 {
		t.Errorf("最小页码 = %d, 期望 1", minPage)
	}

	// 样本是产品手册，应当解析出表格与标题；表格必须以 HTML 形式保留行列结构
	var tables, titles int
	for _, block := range doc.Blocks {
		switch block.Type {
		case loader.BlockTable:
			tables++
			if !containsHTMLTable(block.Text()) {
				t.Errorf("表格块内容不是 HTML，切片时会丢失行列结构: %q", truncate(block.Text(), 80))
			}
		case loader.BlockParagraphTitle:
			titles++
		}
	}
	if tables == 0 {
		t.Error("样本含表格，但未解析出任何表格块")
	}
	if titles == 0 {
		t.Error("样本含标题，但未解析出任何标题块")
	}

	t.Logf("解析完成：%d 块，%d 页，%d 表格，%d 标题，耗时 %s（服务端与总耗时均含模型加载）",
		len(doc.Blocks), doc.Meta.PageCount, tables, titles, elapsed.Round(time.Millisecond))
	if len(doc.Warnings) > 0 {
		t.Logf("警告: %v", doc.Warnings)
	}
}

func containsHTMLTable(text string) bool {
	return strings.Contains(text, "<table") || strings.Contains(text, "<td")
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
