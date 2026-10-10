import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";

// 表单原语：带标签与提示的输入控件。
// 统一在这里，是为了保证「标签 + 控件 + 提示」的排版在所有表单里一致，
// 也为后续接入设计体系留出唯一改动点。

// 控件高度固定：同一行里输入框、下拉、按钮必须等高。
// 否则按底边对齐时，矮一点的控件顶部会低几像素，看起来就是「没对齐」。
export const CONTROL_HEIGHT = "h-[34px]";

const controlClass = `w-full ${CONTROL_HEIGHT} rounded-md border border-neutral-300 px-3 text-sm outline-none transition-colors focus:border-neutral-500 disabled:bg-neutral-100 disabled:text-neutral-400`;

export interface FieldProps {
  label: string;
  /**
   * 补充说明：渲染成标签旁的小图标，鼠标悬浮（或键盘聚焦）时显示。
   *
   * 之所以不做成输入框下方的常驻文字：那会让同一行里各字段高度不一致，
   * 「标签 + 控件」被迫错位（这次就踩到了）。说明文字多数人只在需要时才看，悬浮足矣。
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
      <span className="flex h-4 items-center gap-1 text-xs font-medium text-neutral-600">
        {label}
        {required ? <span className="ml-0.5 text-red-500">*</span> : null}
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
              className="h-3.5 w-3.5 text-neutral-400 group-hover:text-neutral-600"
              aria-hidden="true"
            >
              <circle cx="8" cy="8" r="6.2" />
              <path d="M6.4 6.2a1.7 1.7 0 1 1 2.2 1.9c-.4.2-.6.5-.6.9v.3M8 11.6v.1" strokeLinecap="round" />
            </svg>
            <span className="pointer-events-none absolute left-0 top-full z-30 hidden w-56 translate-y-1 rounded-md bg-neutral-900 px-2 py-1.5 text-[11px] font-normal leading-5 text-white shadow-lg group-hover:block group-focus:block">
              {hint}
            </span>
          </span>
        ) : null}
      </span>
      {children}
      {help ? <span className="text-xs leading-5 text-neutral-500">{help}</span> : null}
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

export interface CardProps {
  title?: string;
  description?: ReactNode;
  /** 右上角操作区 */
  actions?: ReactNode;
  children: ReactNode;
}

export function Card({ title, description, actions, children }: CardProps) {
  return (
    <section className="rounded-lg border border-neutral-200 bg-white p-4 shadow-sm">
      {title ? (
        <header className="mb-3 flex items-start justify-between gap-3">
          <div className="flex flex-col gap-1">
            <h3 className="text-sm font-semibold text-neutral-900">{title}</h3>
            {description ? (
              <p className="text-xs leading-5 text-neutral-500">{description}</p>
            ) : null}
          </div>
          {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
        </header>
      ) : null}
      {children}
    </section>
  );
}
