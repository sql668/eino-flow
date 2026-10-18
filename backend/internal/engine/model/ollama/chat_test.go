package ollama

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

func testCalculatorTool() *schema.ToolInfo {
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
	captured := make(chan ollamaChatRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("请求路径错误: %s", r.URL.Path)
		}
		var request ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("解析请求失败: %v", err)
		}
		captured <- request
		_, _ = w.Write([]byte(`{
			"message":{
				"role":"assistant",
				"content":"",
				"tool_calls":[{
					"function":{"name":"calculator","arguments":{"operand_a":2,"operand_b":3,"operator":"+"}}
				}]
			},
			"done":true
		}`))
	}))
	defer server.Close()

	model := NewChatModel(ChatOptions{BaseURL: server.URL, Model: "qwen3:8b"})
	toolModel, err := model.WithTools([]*schema.ToolInfo{testCalculatorTool()})
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
	if call.Function.Name != "calculator" {
		t.Fatalf("工具名错误: %s", call.Function.Name)
	}
	if call.ID == "" {
		t.Fatal("Ollama 不返回调用 ID 时应补一个，否则工具结果无法关联")
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
}

func TestChatModel_StreamParsesToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := []string{
			`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"calculator","arguments":{"operand_a":2,"operand_b":3,"operator":"+"}}}]},"done":false}`,
			`{"message":{"role":"assistant","content":""},"done":true}`,
		}
		for _, line := range lines {
			if _, err := w.Write([]byte(line + "\n")); err != nil {
				t.Errorf("写流失败: %v", err)
				return
			}
		}
	}))
	defer server.Close()

	model := NewChatModel(ChatOptions{BaseURL: server.URL, Model: "qwen3:8b"})
	toolModel, err := model.WithTools([]*schema.ToolInfo{testCalculatorTool()})
	if err != nil {
		t.Fatalf("绑定工具失败: %v", err)
	}

	stream, err := toolModel.Stream(context.Background(), []*schema.Message{schema.UserMessage("2+3 等于多少")})
	if err != nil {
		t.Fatalf("流式调用失败: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("读取流失败: %v", err)
	}
	if len(chunk.ToolCalls) != 1 || chunk.ToolCalls[0].Function.Name != "calculator" {
		t.Fatalf("流式响应应解析出工具调用，实际: %+v", chunk.ToolCalls)
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("工具调用之后应结束，实际: %v", err)
	}
}

func TestChatModel_SerializesToolHistoryWithObjectArguments(t *testing.T) {
	captured := make(chan ollamaChatRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("解析请求失败: %v", err)
		}
		captured <- request
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"结果是 5"},"done":true}`))
	}))
	defer server.Close()

	model := NewChatModel(ChatOptions{BaseURL: server.URL, Model: "qwen3:8b"})
	_, err := model.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("2+3 等于多少"),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID:   "call_0",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "calculator",
				Arguments: `{"operand_a":2,"operand_b":3,"operator":"+"}`,
			},
		}}),
		schema.ToolMessage("5", "call_0", schema.WithToolName("calculator")),
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}

	request := <-captured
	if len(request.Messages) != 3 {
		t.Fatalf("应带 3 条历史消息，实际 %d 条", len(request.Messages))
	}
	assistant := request.Messages[1]
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("assistant 的工具调用没有带上: %+v", assistant)
	}
	// Ollama 要求 arguments 是 JSON 对象；如果是被引号包住的字符串，这里会看到首字符是 "。
	arguments := strings.TrimSpace(string(assistant.ToolCalls[0].Function.Arguments))
	if !strings.HasPrefix(arguments, "{") {
		t.Fatalf("arguments 必须是 JSON 对象而不是字符串，实际: %s", arguments)
	}
	toolMessage := request.Messages[2]
	if toolMessage.Role != "tool" || toolMessage.ToolName != "calculator" || toolMessage.Content != "5" {
		t.Fatalf("tool 结果消息序列化错误: %+v", toolMessage)
	}
}

func TestChatModel_ToolUnsupportedErrorIsDeterministic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"model does not support tools"}`))
	}))
	defer server.Close()

	model := NewChatModel(ChatOptions{BaseURL: server.URL, Model: "plain-model"})
	toolModel, err := model.WithTools([]*schema.ToolInfo{testCalculatorTool()})
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
