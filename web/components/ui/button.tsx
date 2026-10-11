import type { ButtonHTMLAttributes } from "react";

import { CONTROL_HEIGHT } from "@/components/ui/field";

// 按钮原语。
//
// 视觉方向见 web/AGENTS.md「视觉方向」：主操作是**蓝图蓝**（唯一的饱和色），
// 其余按钮一律「墨线 + 纸」的克制样式；危险操作用读数红。
// 不用阴影、圆角 3px：这是控制台，按钮该像开关而不是糖果。

type Variant = "primary" | "secondary" | "danger" | "ghost";

const variants: Record<Variant, string> = {
  primary: "bg-pri text-white hover:bg-pri-ink disabled:bg-pri/40",
  secondary: "border border-grid bg-white text-ink hover:border-ink-soft hover:bg-paper",
  danger: "border border-fault/30 bg-white text-fault hover:bg-fault-soft disabled:text-fault/40",
  ghost: "text-ink-muted hover:bg-grid-soft hover:text-ink",
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
      className={`inline-flex ${CONTROL_HEIGHT} items-center justify-center gap-1 rounded px-3 text-sm transition-colors disabled:cursor-not-allowed ${variants[variant]} ${className}`}
    >
      {busy ? "处理中…" : children}
    </button>
  );
}
