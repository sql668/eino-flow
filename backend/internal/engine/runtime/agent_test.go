package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	enginemodel "einoflow-backend/internal/engine/model"
)

// scriptedChatModel 按调用次数产出工具调用，超过设定次数后给出最终回答。
// 用它来验证 ReAct 的循环、上限与失败回灌，不依赖任何真实模型服务。
type scriptedChatModel struct {
	mu sync.Mutex
	// toolCalls 是前多少次模型调用返回工具调用。
	toolCalls int
	calls     int
	// onCall 可选：每次调用时检查输入消息。
	onCall func(call int, input []*schema.Message)
}

func (m *scriptedChatModel) WithTools([]*schema.ToolInfo) (enginemodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *scriptedChatModel) Generate(
	_ context.Context, input []*schema.Message, _ ...einomodel.Option,
) (*schema.Message, error) {
	m.mu.Lock()
	m.calls++
	call := m.calls
	m.mu.Unlock()

	if m.onCall != nil {
		m.onCall(call, input)
	}
	if call <= m.toolCalls {
		return schema.AssistantMessage("", []schema.ToolCall{{
			ID:   fmt.Sprintf("call_%d", call),
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "probe",
				Arguments: `{"q":"test"}`,
			},
		}}), nil
	}
	return schema.AssistantMessage("最终回答", nil), nil
}

func (m *scriptedChatModel) Stream(
	context.Context, []*schema.Message, ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("测试脚本只实现 Generate")
}

// probeTool 记录实际执行次数与参数。
type probeTool struct {
	mu      sync.Mutex
	calls   int
	inputs  []string
	result  string
	callErr error
}

func (t *probeTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "probe",
		Desc: "测试用工具",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"q": {Type: schema.String, Desc: "查询", Required: true},
		}),
	}, nil
}

func (t *probeTool) InvokableRun(
	_ context.Context, argumentsInJSON string, _ ...tool.Option,
) (string, error) {
	t.mu.Lock()
	t.calls++
	t.inputs = append(t.inputs, argumentsInJSON)
	t.mu.Unlock()
	if t.callErr != nil {
		return "", t.callErr
	}
	if t.result == "" {
		return "工具结果", nil
	}
	return t.result, nil
}

func (t *probeTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func newTestAgent(t *testing.T, model *scriptedChatModel, probe *probeTool, maxRounds int) (*Agent, *[]ToolEvent) {
	t.Helper()
	var events []ToolEvent
	agentInstance, err := NewAgent(context.Background(), AgentConfig{
		Model:         model,
		Tools:         []tool.BaseTool{probe},
		MaxToolRounds: maxRounds,
		Observer: func(_ context.Context, event ToolEvent) error {
			events = append(events, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("构造智能体失败: %v", err)
	}
	return agentInstance, &events
}

func TestAgent_DefaultMaxToolRoundsStopsAtThree(t *testing.T) {
	model := &scriptedChatModel{toolCalls: 4} // 第 4 次工具调用应被上限拦截
	probe := &probeTool{}
	agentInstance, events := newTestAgent(t, model, probe, 0)

	message, err := agentInstance.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("请反复调用工具"),
	})
	if err != nil {
		t.Fatalf("问答失败: %v", err)
	}
	if message.Content != "最终回答" {
		t.Fatalf("应返回最终回答，实际: %q", message.Content)
	}
	if probe.callCount() != DefaultMaxToolRounds {
		t.Fatalf("默认上限应只执行 %d 次工具，实际 %d 次", DefaultMaxToolRounds, probe.callCount())
	}

	var skipped int
	var limitMessage string
	for _, event := range *events {
		if event.Skipped {
			skipped++
			if event.Kind == ToolEventResult {
				limitMessage = event.Result
			}
		}
	}
	if skipped != 2 { // 被拦截的调用同样会有 call + result 两个事件
		t.Fatalf("应记录一次被拦截的调用（2 个事件），实际 %d 个事件", skipped)
	}
	if !strings.Contains(limitMessage, "上限") {
		t.Fatalf("被拦截时应给模型明确说明，实际: %q", limitMessage)
	}
}

func TestAgent_ConfigurableMaxToolRounds(t *testing.T) {
	model := &scriptedChatModel{toolCalls: 6}
	probe := &probeTool{}
	agentInstance, _ := newTestAgent(t, model, probe, 5)

	if _, err := agentInstance.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("请反复调用工具"),
	}); err != nil {
		t.Fatalf("问答失败: %v", err)
	}
	if probe.callCount() != 5 {
		t.Fatalf("配置为 5 时应执行 5 次工具，实际 %d 次", probe.callCount())
	}
}

func TestAgent_ToolFailureIsFedBackAndDoesNotAbort(t *testing.T) {
	sentinel := errors.New("向量库不可用")
	probe := &probeTool{callErr: sentinel}
	var sawFailure bool
	model := &scriptedChatModel{toolCalls: 1}
	model.onCall = func(call int, input []*schema.Message) {
		if call != 2 {
			return
		}
		for _, message := range input {
			if message.Role == schema.Tool && strings.Contains(message.Content, "失败") {
				sawFailure = true
			}
		}
	}
	agentInstance, events := newTestAgent(t, model, probe, 0)

	message, err := agentInstance.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("查一下资料"),
	})
	if err != nil {
		t.Fatalf("工具失败不应中断问答: %v", err)
	}
	if message.Content != "最终回答" {
		t.Fatalf("应继续产出最终回答，实际: %q", message.Content)
	}
	if !sawFailure {
		t.Fatal("失败原因应作为 tool 消息回灌给模型")
	}

	var failedEvent *ToolEvent
	for i := range *events {
		if (*events)[i].Kind == ToolEventResult && (*events)[i].Failed {
			failedEvent = &(*events)[i]
		}
	}
	if failedEvent == nil {
		t.Fatal("失败必须在事件留痕中可见")
	}
	if !strings.Contains(failedEvent.Result, sentinel.Error()) {
		t.Fatalf("失败事件应包含原始原因，实际: %q", failedEvent.Result)
	}
}

func TestAgent_LogsToolCallWithTraceIDAndDuration(t *testing.T) {
	var buffer bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buffer)
	defer log.SetOutput(previous)

	model := &scriptedChatModel{toolCalls: 1}
	probe := &probeTool{}
	agentInstance, _ := newTestAgent(t, model, probe, 0)

	if _, err := agentInstance.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("查一下资料"),
	}); err != nil {
		t.Fatalf("问答失败: %v", err)
	}

	logs := buffer.String()
	for _, want := range []string{"trace_id=", "event=tool_call", "name=probe", "duration_ms="} {
		if !strings.Contains(logs, want) {
			t.Fatalf("日志缺少 %q:\n%s", want, logs)
		}
	}
}
