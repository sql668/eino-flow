package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	enginemodel "einoflow-backend/internal/engine/model"
)

// calculatorToolInfo 是测试用的工具声明（结构与真实计算工具一致）。
func calculatorToolInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "calculator",
		Desc: "计算两个数的加、减、乘、除",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"operand_a": {Type: schema.Number, Desc: "左操作数", Required: true},
			"operand_b": {Type: schema.Number, Desc: "右操作数", Required: true},
			"operator":  {Type: schema.String, Desc: "运算符", Required: true},
		}),
	}
}

func TestChatModel_GenerateParsesToolCalls(t *testing.T) {
	captured := make(chan chatRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("解析请求失败: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		captured <- request

		_, _ = w.Write([]byte(`{
			"choices":[{
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[{
						"id":"call_1",
						"type":"function",
						"function":{"name":"calculator","arguments":"{\"operand_a\":2,\"operand_b\":3,\"operator\":\"+\"}"}
					}]
				}
			}]
		}`))
	}))
	defer server.Close()

	model := NewChatModel(ChatOptions{BaseURL: server.URL, Model: "test-model"})
	toolModel, err := model.WithTools([]*schema.ToolInfo{calculatorToolInfo()})
	if err != nil {
		t.Fatalf("绑定工具失败: %v", err)
	}

	message, err := toolModel.Generate(context.Background(), []*schema.Message{schema.UserMessage("2+3 等于多少")})
	if err != nil {
		t.Fatalf("非流式调用失败: %v", err)
	}
	if len(message.ToolCalls) != 1 {
		t.Fatalf("应解析出 1 个工具调用，实际 %d 个", len(message.ToolCalls))
	}
	call := message.ToolCalls[0]
	if call.ID != "call_1" || call.Function.Name != "calculator" {
		t.Fatalf("工具调用解析错误: %+v", call)
	}
	wantArgs := `{"operand_a":2,"operand_b":3,"operator":"+"}`
	if call.Function.Arguments != wantArgs {
		t.Fatalf("工具参数应为 %s，实际 %s", wantArgs, call.Function.Arguments)
	}

	request := <-captured
	if request.Stream {
		t.Fatal("Generate 必须使用非流式请求")
	}
	if len(request.Tools) != 1 || request.Tools[0].Function.Name != "calculator" {
		t.Fatalf("请求应带上工具声明，实际: %+v", request.Tools)
	}
	if request.Tools[0].Function.Parameters == nil {
		t.Fatal("工具声明必须带参数结构，否则模型不知道要传什么")
	}
}

func TestChatModel_StreamAccumulatesToolCallFragments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("测试服务器需要支持 Flush")
			return
		}
		lines := []string{
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"calculator","arguments":"{\"operand_a\":2,"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"operand_b\":3,\"operator\":\"+\"}"}}]}}]}`,
			`data: [DONE]`,
		}
		for _, line := range lines {
			if _, err := w.Write([]byte(line + "\n\n")); err != nil {
				t.Errorf("写流失败: %v", err)
				return
			}
			flusher.Flush()
		}
	}))
	defer server.Close()

	model := NewChatModel(ChatOptions{BaseURL: server.URL, Model: "test-model"})
	toolModel, err := model.WithTools([]*schema.ToolInfo{calculatorToolInfo()})
	if err != nil {
		t.Fatalf("绑定工具失败: %v", err)
	}

	stream, err := toolModel.Stream(context.Background(), []*schema.Message{schema.UserMessage("2+3 等于多少")})
	if err != nil {
		t.Fatalf("流式调用失败: %v", err)
	}
	defer stream.Close()

	var chunks []*schema.Message
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("读取流失败: %v", err)
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("应收到 2 个工具调用分片，实际 %d 个", len(chunks))
	}
	// ReAct 的默认检查器只看第一段是否带 ToolCalls，这里必须满足。
	if len(chunks[0].ToolCalls) != 1 {
		t.Fatal("第一个分片必须带工具调用，否则 ReAct 会以为模型在直接回答")
	}

	merged, err := schema.ConcatMessages(chunks)
	if err != nil {
		t.Fatalf("Eino 合并分片失败: %v", err)
	}
	if len(merged.ToolCalls) != 1 {
		t.Fatalf("合并后应有 1 个工具调用，实际 %d 个", len(merged.ToolCalls))
	}
	wantArgs := `{"operand_a":2,"operand_b":3,"operator":"+"}`
	if merged.ToolCalls[0].Function.Arguments != wantArgs {
		t.Fatalf("分片参数应拼成完整 JSON，期望 %s，实际 %s",
			wantArgs, merged.ToolCalls[0].Function.Arguments)
	}
}

func TestChatModel_SerializesToolHistory(t *testing.T) {
	captured := make(chan chatRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("解析请求失败: %v", err)
		}
		captured <- request
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"结果是 5"}}]}`))
	}))
	defer server.Close()

	model := NewChatModel(ChatOptions{BaseURL: server.URL, Model: "test-model"})
	_, err := model.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("2+3 等于多少"),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "calculator",
				Arguments: `{"operand_a":2,"operand_b":3,"operator":"+"}`,
			},
		}}),
		schema.ToolMessage("5", "call_1", schema.WithToolName("calculator")),
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}

	request := <-captured
	if len(request.Messages) != 3 {
		t.Fatalf("应带 3 条历史消息，实际 %d 条", len(request.Messages))
	}
	assistant := request.Messages[1]
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Function.Name != "calculator" {
		t.Fatalf("assistant 的工具调用没有原样带上: %+v", assistant)
	}
	toolMessage := request.Messages[2]
	if toolMessage.Role != "tool" || toolMessage.ToolCallID != "call_1" || toolMessage.Content != "5" {
		t.Fatalf("tool 结果消息序列化错误: %+v", toolMessage)
	}
}

func TestChatModel_ToolUnsupportedErrorIsDeterministic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"this model does not support tools"}}`))
	}))
	defer server.Close()

	model := NewChatModel(ChatOptions{BaseURL: server.URL, Model: "plain-model"})
	toolModel, err := model.WithTools([]*schema.ToolInfo{calculatorToolInfo()})
	if err != nil {
		t.Fatalf("绑定工具失败: %v", err)
	}

	message, err := toolModel.Generate(context.Background(), []*schema.Message{schema.UserMessage("2+3")})
	if err == nil {
		t.Fatalf("模型不支持工具时必须报错，不能返回退化回答: %+v", message)
	}
	if !errors.Is(err, enginemodel.ErrToolCallingUnsupported) {
		t.Fatalf("错误应可判定为 ErrToolCallingUnsupported，实际: %v", err)
	}
	if message != nil {
		t.Fatal("失败时不应返回任何回答")
	}
}

func TestChatModel_WithToolsDoesNotMutateBase(t *testing.T) {
	base := NewChatModel(ChatOptions{Model: "test-model"})
	withTools, err := base.WithTools([]*schema.ToolInfo{calculatorToolInfo()})
	if err != nil {
		t.Fatalf("绑定工具失败: %v", err)
	}
	if withTools == base {
		t.Fatal("WithTools 必须返回新实例，原地修改会让并发请求互相覆盖工具集")
	}
	if len(base.tools) != 0 {
		t.Fatalf("基础实例不应被绑定工具，实际 %d 个", len(base.tools))
	}
	if !strings.Contains(withTools.(*ChatModel).tools[0].Function.Name, "calculator") {
		t.Fatal("派生实例应带上工具")
	}
}
