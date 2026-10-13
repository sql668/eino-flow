package tools

import (
	"context"
	"strings"
	"testing"
)

func TestCalculator_Arithmetic(t *testing.T) {
	calculator, err := NewCalculator()
	if err != nil {
		t.Fatalf("构造计算工具失败: %v", err)
	}

	// 用 InvokableRun 而不是直接调 calculate：这条路径包含 Eino 的参数解码，
	// 模型给的就是 JSON 字符串，测试要覆盖同一入口。
	cases := []struct {
		name string
		args string
		want string
	}{
		{"加法", `{"operand_a":2,"operand_b":3,"operator":"+"}`, "5"},
		{"减法", `{"operand_a":2,"operand_b":3,"operator":"-"}`, "-1"},
		{"乘法", `{"operand_a":23,"operand_b":5,"operator":"*"}`, "115"},
		{"除法", `{"operand_a":10,"operand_b":4,"operator":"/"}`, "2.5"},
		{"浮点误差被收敛", `{"operand_a":0.1,"operand_b":0.2,"operator":"+"}`, "0.3"},
		{"大数相除", `{"operand_a":100,"operand_b":3,"operator":"/"}`, "33.3333333333"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := calculator.InvokableRun(context.Background(), testCase.args)
			if err != nil {
				t.Fatalf("执行失败: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("期望 %q，实际 %q", testCase.want, got)
			}
		})
	}
}

func TestCalculator_ErrorsAreReadable(t *testing.T) {
	calculator, err := NewCalculator()
	if err != nil {
		t.Fatalf("构造计算工具失败: %v", err)
	}

	t.Run("除数为零", func(t *testing.T) {
		_, err := calculator.InvokableRun(context.Background(), `{"operand_a":1,"operand_b":0,"operator":"/"}`)
		if err == nil {
			t.Fatal("除数为零必须报错")
		}
		if !strings.Contains(err.Error(), "除数") {
			t.Fatalf("错误信息要写给模型看，应说明是除数问题，实际: %v", err)
		}
	})

	t.Run("未知运算符", func(t *testing.T) {
		_, err := calculator.InvokableRun(context.Background(), `{"operand_a":1,"operand_b":2,"operator":"%"}`)
		if err == nil {
			t.Fatal("未知运算符必须报错")
		}
		if !strings.Contains(err.Error(), "运算符") {
			t.Fatalf("错误信息应指出允许的运算符，实际: %v", err)
		}
	})

	t.Run("参数不是合法 JSON", func(t *testing.T) {
		if _, err := calculator.InvokableRun(context.Background(), `not-json`); err == nil {
			t.Fatal("参数解析失败必须报错，不能当成零值继续算")
		}
	})
}

func TestCalculator_InfoForModel(t *testing.T) {
	calculator, err := NewCalculator()
	if err != nil {
		t.Fatalf("构造计算工具失败: %v", err)
	}
	info, err := calculator.Info(context.Background())
	if err != nil {
		t.Fatalf("取工具信息失败: %v", err)
	}
	if info.Name != calculatorName {
		t.Fatalf("工具名应为 %q，实际 %q", calculatorName, info.Name)
	}
	if strings.TrimSpace(info.Desc) == "" {
		t.Fatal("工具说明不能为空——模型靠它决定什么时候调用")
	}
	if info.ParamsOneOf == nil {
		t.Fatal("应带参数结构，否则模型无从知道该传什么")
	}
}
