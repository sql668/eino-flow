// Package runtime 承载智能体（ReAct）运行时与工具注册。
//
// 放在 engine 层：编排与工具执行都属于「AI 跑起来」这件事，
// handler / service 只负责请求解析与业务编排（见 backend/AGENTS.md 的分层约定）。
package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// registeredTool 是注册表里的一条记录。
//
// 注册时就把 ToolInfo 取出来缓存：工具名与描述在注册后不会变，
// 每次组装工具集都去问一遍（Info 可能触发反射或网络）没有意义。
type registeredTool struct {
	info *schema.ToolInfo
	tool tool.BaseTool
}

// Registry 是工具注册表。
//
// 为什么用注册表而不是在运行时里写 if/else：工具是会持续增加的东西
// （V2.0 的插件市场、MCP 适配都会往这里加），新增一个工具应当是"新增一个实现"，
// 而不是回头改运行时的分支（开闭原则，见根 AGENTS.md 第 10 节）。
type Registry struct {
	byName map[string]registeredTool
	order  []string
}

// NewRegistry 构造空注册表。
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]registeredTool)}
}

// Register 注册一个工具。
//
// 名称为空、重名、或取不到工具信息时**直接报错**：静默覆盖会让
// 「模型刚才调用的到底是哪个工具」变得不可预测，而这类问题在日志里几乎看不出来。
func (r *Registry) Register(ctx context.Context, candidate tool.BaseTool) error {
	if candidate == nil {
		return fmt.Errorf("注册工具失败: 工具为空")
	}
	info, err := candidate.Info(ctx)
	if err != nil {
		return fmt.Errorf("注册工具失败: 无法获取工具信息: %w", err)
	}
	if info == nil || strings.TrimSpace(info.Name) == "" {
		return fmt.Errorf("注册工具失败: 工具名不能为空")
	}
	name := strings.TrimSpace(info.Name)
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("注册工具失败: 工具名 %q 重复", name)
	}
	r.byName[name] = registeredTool{info: info, tool: candidate}
	r.order = append(r.order, name)
	return nil
}

// Tools 返回按注册顺序排列的工具，可直接交给 Eino 的工具节点 / ReAct 运行时。
//
// 返回切片而不是 map：Eino 的 ToolsNode 会用它拼工具列表，
// 顺序稳定能让「同一问题的工具调用顺序」在测试与日志里可比对。
func (r *Registry) Tools() []tool.BaseTool {
	out := make([]tool.BaseTool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.byName[name].tool)
	}
	return out
}

// ToolInfos 返回工具信息（拼提示词、做能力声明时用）。
func (r *Registry) ToolInfos() []*schema.ToolInfo {
	out := make([]*schema.ToolInfo, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.byName[name].info)
	}
	return out
}

// Names 返回已注册的工具名，用于日志与错误信息。
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Len 返回已注册的工具数量。
func (r *Registry) Len() int { return len(r.order) }
