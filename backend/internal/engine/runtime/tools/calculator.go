// Package tools 放置内置工具实现。
//
// 与运行时（runtime 包）分开：runtime 只认 Eino 的工具接口，
// 具体工具是什么、怎么算，是各自实现的事——加工具不用动运行时。
package tools

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// calculatorName 是工具名。模型按名字选工具，名字要一眼看懂用途。
const calculatorName = "calculator"

// calcArgs 是计算工具的入参。
//
// 刻意只做「两个操作数 + 一个运算符」：它是**演示工具**，
// 用途是验证"工具机制本身可用"（模型会不会调、结果会不会回灌、多步能不能串起来），
// 不是要给用户一个计算器产品。要算复杂表达式，模型可以多调几次——
// 那反而正好演示了多步工具调用。
type calcArgs struct {
	// OperandA / OperandB 是左右操作数。
	OperandA float64 `json:"operand_a"`
	OperandB float64 `json:"operand_b"`
	// Operator 是运算符，取值 + - * / 。
	Operator string `json:"operator"`
}

// NewCalculator 构造计算工具。
//
// 这个工具不依赖任何外部服务、不写任何业务数据：知识库检索工具依赖向量库与向量化服务，
// 一旦出问题无法判断"是工具机制坏了还是依赖坏了"；一个纯本地工具能把这两件事分开。
func NewCalculator() (tool.InvokableTool, error) {
	calculator, err := utils.InferTool(
		calculatorName,
		"计算两个数的加、减、乘、除。需要做算术时使用它，不要自己心算。",
		func(_ context.Context, args calcArgs) (string, error) {
			return calculate(args)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("构造计算工具失败: %w", err)
	}
	return calculator, nil
}

// calculate 执行四则运算。
//
// 错误信息要写给**模型**看：它拿到的是工具返回的文本，措辞清楚它才能自己纠正
// （例如把 "除" 改成 "/"），而不是把错误原样抛给用户。
func calculate(args calcArgs) (string, error) {
	operator := strings.TrimSpace(args.Operator)
	switch operator {
	case "+":
		return formatNumber(args.OperandA + args.OperandB), nil
	case "-":
		return formatNumber(args.OperandA - args.OperandB), nil
	case "*":
		return formatNumber(args.OperandA * args.OperandB), nil
	case "/":
		if args.OperandB == 0 {
			return "", fmt.Errorf("除数不能为 0")
		}
		return formatNumber(args.OperandA / args.OperandB), nil
	default:
		return "", fmt.Errorf("不支持的运算符 %q，只能使用 + - * /", operator)
	}
}

// formatNumber 去掉浮点误差带来的长尾。
//
// 直接用 %v 输出 0.1+0.2 会得到 0.30000000000000004，
// 模型看到这种数字会倾向于"再解释一遍"，反而让回答变啰嗦。
func formatNumber(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "结果超出可表示范围"
	}
	rounded := math.Round(value*1e10) / 1e10
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.10f", rounded), "0"), ".")
}
