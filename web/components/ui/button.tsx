import type { ButtonHTMLAttributes } from "react";

import { CONTROL_HEIGHT } from "@/components/ui/field";

// 极简按钮原语。
// 项目尚未引入 shadcn/ui，但「样式散落在每个按钮上」是明确要避免的：
// 后续任何页面都要用按钮，抽在这里之后替换设计体系只需改一个文件。

type Variant = "primary" | "secondary" | "danger" | "ghost";

const variants: Record<Variant, string> = {
  primary:
    "bg-neutral-900 text-white hover:bg-neutral-700 disabled:bg-neutral-400",
  secondary:
    "border border-neutral-300 bg-white text-neutral-800 hover:bg-neutral-100 disabled:text-neutral-400",
  danger:
    "border border-red-200 bg-white text-red-600 hover:bg-red-50 disabled:text-red-300",
  ghost: "text-neutral-600 hover:bg-neutral-100 disabled:text-neutral-300",
};

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  /** 操作进行中：禁用并显示忙碌文案，避免重复提交 */
  busy?: boolean;
}

export function Button({
  variant = "secondary",
  busy = false,
  disabled,
  className = "",
  children,
  ...rest
}: ButtonProps) {
  return (
    <button
      {...rest}
      disabled={disabled || busy}
      className={`inline-flex ${CONTROL_HEIGHT} items-center justify-center gap-1 rounded-md px-3 text-sm transition-colors disabled:cursor-not-allowed ${variants[variant]} ${className}`}
    >
      {busy ? "处理中…" : children}
    </button>
  );
}
