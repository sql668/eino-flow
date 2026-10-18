package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/trace"

	enginemodel "einoflow-backend/internal/engine/model"
)

// DefaultMaxToolRounds 是未配置时的工具调用次数上限（用户拍板默认 3）。
//
// 上限按「工具调用次数」而不是「模型轮次」计算：一次模型回复可能并行调用多个工具，
// 用户看到的成本与风险都来自实际执行了几次工具。
const DefaultMaxToolRounds = 3

// ErrToolRoundLimit 表示本次问答因工具调用次数达到上限而终止。
//
// 单独一个错误而不是把 Eino 的 "exceeds max steps" 直接抛给上层：
// service 需要据它给用户一个明确说明，而不是一句图执行失败。
var ErrToolRoundLimit = errors.New("工具调用次数已达到上限")

// ToolEventKind 是工具事件的类型。
type ToolEventKind string

const (
	// ToolEventCall 在模型决定调用工具、即将执行（或被上限拦截）时发出。
	ToolEventCall ToolEventKind = "tool_call"
	// ToolEventResult 在工具执行结束（成功或失败）后发出。
	ToolEventResult ToolEventKind = "tool_result"
)

// ToolEvent 是一次工具调用的可观测记录。
//
// 运行时不直接写 SSE：事件形态属于服务层与前端契约，
// runtime 只负责"发生了什么"，由注入的 Observer 决定怎么呈现与落库。
type ToolEvent struct {
	Kind ToolEventKind
	// Round 是本次问答内的工具调用序号，从 1 开始（含被上限拦截的调用）。
	Round int
	// CallID 是模型给出的调用 ID，用于把结果与调用对应起来。
	CallID string
	Name   string
	// Arguments 是模型给出的原始参数 JSON（摘要由展示层按需截断）。
	Arguments string
	// Result 在 Kind=result 时有值；失败时是给模型看的错误说明。
	Result string
	// Failed 表示工具执行返回了错误。
	Failed bool
	// Skipped 表示这次调用因达到轮次上限而没有真正执行。
	Skipped bool
	// Duration 是工具执行耗时；被跳过时为 0。
	Duration time.Duration
}

// ToolObserver 接收工具调用事件。
//
// 返回错误会终止本次问答：典型场景是 SSE 写失败（客户端已断开），
// 继续执行只会白烧模型与检索算力。
type ToolObserver func(ctx context.Context, event ToolEvent) error

// AgentConfig 是一次智能体运行的配置。
type AgentConfig struct {
	// Model 必须是具备工具调用能力的模型；不支持时 NewAgent 直接报错。
	Model enginemodel.ToolCallingChatModel
	// Tools 是本次问答应启用的工具集（见 tools.ToolNamesForChat + Registry.Select）。
	Tools []tool.BaseTool
	// MaxToolRounds <= 0 时使用 DefaultMaxToolRounds。
	MaxToolRounds int
	// SystemPrompt 可选：为空时使用内置的智能体提示词。
	SystemPrompt string
	// Observer 可选：用于流式事件与落库留痕。
	Observer ToolObserver
}

// Agent 是编译好的 ReAct 运行时。
type Agent struct {
	runnable *react.Agent
	// maxToolRounds 只用于日志与错误说明，真正的计数在 guard 中间件里。
	maxToolRounds int
	observer      ToolObserver
	traceID       string

	mu sync.Mutex
	// used 是已经"发起"的工具调用次数；被上限拦截的调用也算一次，
	// 这样第 4 次调用不会再次真正执行工具。
	used int
}

// defaultAgentPrompt 是智能体的系统提示词。
//
// 不写死具体工具名：可用工具随本次问答变化（未指定知识库时没有检索工具），
// 工具名与用途由工具声明本身告诉模型，提示词只约束行为。
const defaultAgentPrompt = `你是 EinoFlow 的智能体助手。请根据问题需要调用可用工具获取信息，再基于工具结果作答。
要求：
1. 优先使用工具而不是猜测；工具失败时如实说明，并基于已有信息继续作答。
2. 引用知识库内容时标注来源（文档名、页码或标题路径）。
3. 用简洁的书面中文回答，先给结论，再给必要说明。`

// NewAgent 用 Eino 的 ReAct 组件组装运行时。
//
// 每次问答编译一次：工具集与模型都随请求变化（应用 / 知识库级覆盖），
// 这与现有问答链路"按请求编译"的做法一致；Compile 只做类型与拓扑校验，开销可控。
func NewAgent(ctx context.Context, config AgentConfig) (*Agent, error) {
	if config.Model == nil {
		return nil, fmt.Errorf("缺少支持工具调用的对话模型")
	}

	maxToolRounds := config.MaxToolRounds
	if maxToolRounds <= 0 {
		maxToolRounds = DefaultMaxToolRounds
	}
	systemPrompt := strings.TrimSpace(config.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = defaultAgentPrompt
	}

	instance := &Agent{
		maxToolRounds: maxToolRounds,
		observer:      config.Observer,
		traceID:       traceIDFromContext(ctx),
	}
	// MaxStep 是 Eino 图的执行步数，不是工具调用次数：
	// 一轮工具调用占「模型 + 工具」两步，最后还需要一次模型作答。
	// 额外留 2 步给"超过上限的调用被拦截后，模型再作答"。
	maxStep := 2*maxToolRounds + 3

	runnable, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: config.Model,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: config.Tools,
			ToolCallMiddlewares: []compose.ToolMiddleware{
				instance.guardMiddleware(),
			},
		},
		MaxStep: maxStep,
		MessageModifier: func(_ context.Context, input []*schema.Message) []*schema.Message {
			// 每次模型调用都在副本前面补系统提示词：
			// ReAct 的 state 会累积历史，不能把提示词重复塞进 state。
			out := make([]*schema.Message, 0, len(input)+1)
			out = append(out, schema.SystemMessage(systemPrompt))
			out = append(out, input...)
			return out
		},
	})
	if err != nil {
		return nil, fmt.Errorf("编译智能体运行时失败: %w", err)
	}
	instance.runnable = runnable
	return instance, nil
}

// Generate 执行一次智能体问答并一次性返回最终回答。
func (a *Agent) Generate(
	ctx context.Context, input []*schema.Message, opts ...agent.AgentOption,
) (*schema.Message, error) {
	started := time.Now()
	log.Printf("[agent] trace_id=%s event=start messages=%d max_tool_rounds=%d",
		traceIDFromContext(ctx), len(input), a.maxToolRounds)

	message, err := a.runnable.Generate(ctx, input, opts...)
	if err != nil {
		return nil, a.wrapRunError(err)
	}
	log.Printf("[agent] trace_id=%s event=finish duration_ms=%d answer_chars=%d tokens_estimated=%d",
		traceIDFromContext(ctx), time.Since(started).Milliseconds(),
		len([]rune(message.Content)), len([]rune(message.Content)))
	return message, nil
}

// Stream 执行一次智能体问答并返回流式输出。
func (a *Agent) Stream(
	ctx context.Context, input []*schema.Message, opts ...agent.AgentOption,
) (*schema.StreamReader[*schema.Message], error) {
	started := time.Now()
	log.Printf("[agent] trace_id=%s event=start stream=true messages=%d max_tool_rounds=%d",
		traceIDFromContext(ctx), len(input), a.maxToolRounds)

	stream, err := a.runnable.Stream(ctx, input, opts...)
	if err != nil {
		return nil, a.wrapRunError(err)
	}
	// 流式结束时间只能在读取端得知；这里记录启动耗时，结束耗时由 service 在收流后记录。
	log.Printf("[agent] trace_id=%s event=stream_started duration_ms=%d",
		traceIDFromContext(ctx), time.Since(started).Milliseconds())
	return stream, nil
}

// wrapRunError 把 Eino 的步数超限转成可判定的领域错误。
func (a *Agent) wrapRunError(err error) error {
	if errors.Is(err, compose.ErrExceedMaxSteps) {
		return fmt.Errorf("%w（最多 %d 次工具调用）: %w", ErrToolRoundLimit, a.maxToolRounds, err)
	}
	return err
}

// guardMiddleware 负责三件事：轮次上限、失败回灌、事件与日志留痕。
//
// 用工具中间件而不是在运行时外面再包一层循环：ReAct 的多轮调度由 Eino 负责，
// 在工具边界拦截是唯一既能看清每次调用、又不需要重写 ReAct 的位置。
func (a *Agent) guardMiddleware() compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				a.mu.Lock()
				a.used++
				round := a.used
				a.mu.Unlock()

				if round > a.maxToolRounds {
					result := fmt.Sprintf(
						"已达到工具调用上限（%d 次），本次不再执行工具。请基于已有信息直接作答，并说明信息可能不完整。",
						a.maxToolRounds)
					if err := a.notify(ctx, ToolEvent{
						Kind:      ToolEventCall,
						Round:     round,
						CallID:    input.CallID,
						Name:      input.Name,
						Arguments: input.Arguments,
						Skipped:   true,
					}); err != nil {
						return nil, err
					}
					if err := a.notify(ctx, ToolEvent{
						Kind:      ToolEventResult,
						Round:     round,
						CallID:    input.CallID,
						Name:      input.Name,
						Arguments: input.Arguments,
						Result:    result,
						Skipped:   true,
					}); err != nil {
						return nil, err
					}
					log.Printf("[agent] trace_id=%s event=tool_skipped name=%s call_id=%s round=%d limit=%d",
						traceIDFromContext(ctx), input.Name, input.CallID, round, a.maxToolRounds)
					return &compose.ToolOutput{Result: result}, nil
				}

				if err := a.notify(ctx, ToolEvent{
					Kind:      ToolEventCall,
					Round:     round,
					CallID:    input.CallID,
					Name:      input.Name,
					Arguments: input.Arguments,
				}); err != nil {
					return nil, err
				}
				log.Printf("[agent] trace_id=%s event=tool_call name=%s call_id=%s round=%d args=%s",
					traceIDFromContext(ctx), input.Name, input.CallID, round, truncateForLog(input.Arguments))

				started := time.Now()
				output, callErr := next(ctx, input)
				duration := time.Since(started)

				if callErr != nil {
					// 工具失败不中断问答：把失败原因作为工具结果回灌，
					// 模型可以换关键词重试或如实说明——这正是 ReAct 的纠错空间。
					result := fmt.Sprintf("工具执行失败：%v。请根据失败原因调整参数或换一种方式，必要时如实说明无法获取该信息。", callErr)
					if err := a.notify(ctx, ToolEvent{
						Kind:      ToolEventResult,
						Round:     round,
						CallID:    input.CallID,
						Name:      input.Name,
						Arguments: input.Arguments,
						Result:    result,
						Failed:    true,
						Duration:  duration,
					}); err != nil {
						return nil, err
					}
					log.Printf("[agent] trace_id=%s event=tool_error name=%s call_id=%s round=%d duration_ms=%d error=%s",
						traceIDFromContext(ctx), input.Name, input.CallID, round,
						duration.Milliseconds(), truncateForLog(callErr.Error()))
					return &compose.ToolOutput{Result: result}, nil
				}

				result := ""
				if output != nil {
					result = output.Result
				}
				if err := a.notify(ctx, ToolEvent{
					Kind:      ToolEventResult,
					Round:     round,
					CallID:    input.CallID,
					Name:      input.Name,
					Arguments: input.Arguments,
					Result:    result,
					Duration:  duration,
				}); err != nil {
					return nil, err
				}
				log.Printf("[agent] trace_id=%s event=tool_result name=%s call_id=%s round=%d duration_ms=%d result=%s",
					traceIDFromContext(ctx), input.Name, input.CallID, round,
					duration.Milliseconds(), truncateForLog(result))
				return output, nil
			}
		},
	}
}

// notify 把事件交给观察者。
func (a *Agent) notify(ctx context.Context, event ToolEvent) error {
	if a.observer == nil {
		return nil
	}
	if err := a.observer(ctx, event); err != nil {
		return fmt.Errorf("发送工具事件失败: %w", err)
	}
	return nil
}

// traceIDFromContext 取出 OTel 的 trace_id；没有链路时返回 "-"。
//
// 用 "-" 而不是空串：日志里能明确看出"这条没有链路"，
// 空值容易被误认为日志字段缺失。
func traceIDFromContext(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return "-"
	}
	return spanContext.TraceID().String()
}

// truncateForLog 截断日志里的长参数 / 长结果，避免一次检索把日志刷屏。
func truncateForLog(text string) string {
	const limit = 300
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}
	return string(runes[:limit]) + "…"
}
