import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";

// 表单原语与面板。
//
// 视觉方向（web/AGENTS.md「视觉方向」）：1px 网格线 + 纸底，无阴影；
// 面板头部是一条细规则线，右端可以挂**等宽读数**（像工程图上的尺寸标注）。

// 控件高度固定：同一行里输入框、下拉、按钮必须等高。
// 否则按底边对齐时矮一点的控件顶部会低几像素，看起来就是「没对齐」。
export const CONTROL_HEIGHT = "h-[34px]";

const controlClass = `w-full ${CONTROL_HEIGHT} rounded border border-grid bg-white px-3 text-sm text-ink outline-none transition-colors placeholder:text-ink-soft hover:border-ink-soft focus:border-pri disabled:bg-grid-soft disabled:text-ink-soft`;

export interface FieldProps {
  label: string;
  /**
   * 补充说明：渲染成标签旁的小图标，鼠标悬浮（或键盘聚焦）时显示。
   *
   * 之所以不做成输入框下方的常驻文字：那会让同一行里各字段高度不一致，
   * 「标签 + 控件」被迫错位。说明文字多数人只在需要时才看，悬浮足矣。
   */
  hint?: ReactNode;
  /**
   * 需要常驻显示的辅助文字（少数场景，例如「已保存：sk-***0001」这类状态）。
   * 只在竖向排列的表单里用；横排表单里请用 hint，否则又会破坏对齐。
   */
  help?: ReactNode;
  /** 该字段是否必填（仅用于展示提示，真正的校验在后端） */
  required?: boolean;
  children: ReactNode;
}

export function Field({ label, hint, help, required = false, children }: FieldProps) {
  return (
    // gap-1.5 与标签行高度固定：横排时各字段的标签与控件才会在同一基线上。
    <label className="flex flex-col gap-1.5">
      <span className="flex h-4 items-center gap-1 text-xs font-medium text-ink-muted">
        {label}
        {required ? <span className="ml-0.5 text-fault">*</span> : null}
        {hint ? (
          // 纯 CSS 悬浮（group-hover / group-focus），不引入弹层组件也不占布局空间。
          <span
            className="group relative inline-flex cursor-help items-center"
            tabIndex={0}
            role="note"
          >
            <svg
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              strokeWidth={1.4}
              className="h-3.5 w-3.5 text-ink-soft group-hover:text-ink-muted"
              aria-hidden="true"
            >
              <circle cx="8" cy="8" r="6.2" />
              <path d="M6.4 6.2a1.7 1.7 0 1 1 2.2 1.9c-.4.2-.6.5-.6.9v.3M8 11.6v.1" strokeLinecap="round" />
            </svg>
            <span className="pointer-events-none absolute left-0 top-full z-30 mt-1 hidden max-h-40 w-64 translate-y-0.5 overflow-y-auto overscroll-contain rounded bg-ink px-2.5 py-2 text-[11px] font-normal leading-5 text-white shadow-lg group-hover:block group-focus:block">
              {hint}
            </span>
          </span>
        ) : null}
      </span>
      {children}
      {help ? <span className="reading text-xs text-ink-muted">{help}</span> : null}
    </label>
  );
}

export function Input({ className = "", ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...rest} className={`${controlClass} ${className}`} />;
}

export function Select({ className = "", children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select {...rest} className={`${controlClass} ${className}`}>
      {children}
    </select>
  );
}

export interface PanelProps {
  /** 区块眉标（大写标注字）：说明「这一块是什么」 */
  eyebrow?: string;
  title?: string;
  description?: ReactNode;
  /**
   * 右端读数：放这一块的真实数字（段数、字数、耗时…）。
   * 这是本设计的记忆点——面板头部像工程图上的尺寸标注，而不是装饰性图标。
   */
  reading?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  className?: string;
}

export function Panel({
  eyebrow,
  title,
  description,
  reading,
  actions,
  children,
  className = "",
}: PanelProps) {
  const hasHeader = eyebrow || title || description || reading || actions;
  return (
    <section className={`rounded border border-grid bg-white ${className}`}>
      {hasHeader ? (
        <header className="flex flex-wrap items-start justify-between gap-3 border-b border-grid-soft px-4 py-3">
          <div className="flex min-w-0 flex-col gap-0.5">
            {eyebrow ? <span className="eyebrow">{eyebrow}</span> : null}
            {title ? <h2 className="text-sm font-semibold text-ink">{title}</h2> : null}
            {description ? (
              <p className="text-xs leading-5 text-ink-muted">{description}</p>
            ) : null}
          </div>
          <div className="flex shrink-0 items-center gap-3">
            {reading ? <span className="reading text-xs text-ink-muted">{reading}</span> : null}
            {actions}
          </div>
        </header>
      ) : null}
      {children ? <div className="px-4 py-3">{children}</div> : null}
    </section>
  );
}
