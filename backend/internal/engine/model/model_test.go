package model

import (
	"errors"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
)

// baseOnly 只实现 BaseChatModel（嵌入接口即可满足方法集），用于模拟"不支持工具"的模型。
type baseOnly struct{ einomodel.BaseChatModel }

// toolCapable 实现 ToolCallingChatModel，用于模拟支持工具的模型。
type toolCapable struct{ einomodel.ToolCallingChatModel }

func TestRequireToolCalling_RejectsPlainModel(t *testing.T) {
	got, err := RequireToolCalling("plain-model", baseOnly{})
	if err == nil {
		t.Fatal("普通对话模型必须被拒绝，不能静默退化为纯聊天")
	}
	if !errors.Is(err, ErrToolCallingUnsupported) {
		t.Fatalf("错误应可判定为 ErrToolCallingUnsupported，实际: %v", err)
	}
	if got != nil {
		t.Fatal("失败时不应返回任何模型实例")
	}
}

func TestRequireToolCalling_ReturnsCapableModel(t *testing.T) {
	capable := toolCapable{}
	got, err := RequireToolCalling("tool-model", capable)
	if err != nil {
		t.Fatalf("支持工具的模型不应报错: %v", err)
	}
	if got == nil {
		t.Fatal("应返回可用的工具调用模型")
	}
}

func TestRequireToolCalling_RejectsNilModel(t *testing.T) {
	if _, err := RequireToolCalling("missing", nil); !errors.Is(err, ErrToolCallingUnsupported) {
		t.Fatalf("空模型应返回可判定的错误，实际: %v", err)
	}
}
