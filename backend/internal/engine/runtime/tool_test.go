package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// staticTool 是只提供工具信息的替身，用于验证注册表的校验与顺序。
type staticTool struct {
	info *schema.ToolInfo
	err  error
}

func (s staticTool) Info(context.Context) (*schema.ToolInfo, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.info, nil
}

func namedTool(name string) staticTool {
	return staticTool{info: &schema.ToolInfo{Name: name, Desc: name + " 的说明"}}
}

func TestRegistry_KeepsRegistrationOrder(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"search", "calculator"} {
		if err := registry.Register(context.Background(), namedTool(name)); err != nil {
			t.Fatalf("注册 %s 失败: %v", name, err)
		}
	}

	// 顺序稳定才能让日志与测试里的「工具调用顺序」可比对。
	want := []string{"search", "calculator"}
	got := registry.Names()
	if len(got) != len(want) {
		t.Fatalf("期望 %d 个工具，实际 %d 个", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 个工具应为 %s，实际 %s", i+1, want[i], got[i])
		}
	}
	if registry.Len() != 2 || len(registry.Tools()) != 2 || len(registry.ToolInfos()) != 2 {
		t.Fatalf("工具数量不一致：Len=%d Tools=%d ToolInfos=%d",
			registry.Len(), len(registry.Tools()), len(registry.ToolInfos()))
	}
}

func TestRegistry_RejectsDuplicateName(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(context.Background(), namedTool("search")); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}

	err := registry.Register(context.Background(), namedTool("search"))
	if err == nil {
		t.Fatal("重名工具必须被拒绝（静默覆盖会让模型调用哪个工具变得不可预测）")
	}
	if !strings.Contains(err.Error(), "重复") {
		t.Fatalf("错误信息应说明是重名，实际: %v", err)
	}
	if registry.Len() != 1 {
		t.Fatalf("被拒绝的注册不应改变注册表，实际 %d 个工具", registry.Len())
	}
}

func TestRegistry_RejectsEmptyName(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(context.Background(), staticTool{info: &schema.ToolInfo{Name: "   "}})
	if err == nil {
		t.Fatal("空名称工具必须被拒绝")
	}
	if registry.Len() != 0 {
		t.Fatal("被拒绝的注册不应进入注册表")
	}
}

func TestRegistry_PropagatesInfoError(t *testing.T) {
	sentinel := errors.New("工具信息不可用")
	registry := NewRegistry()

	err := registry.Register(context.Background(), staticTool{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Fatalf("取工具信息失败应原样上抛，实际: %v", err)
	}
}

func TestRegistry_RejectsNilTool(t *testing.T) {
	if err := NewRegistry().Register(context.Background(), nil); err == nil {
		t.Fatal("nil 工具必须被拒绝")
	}
}
