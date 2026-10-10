import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";

// 表单原语：带标签与提示的输入控件。
// 统一在这里，是为了保证「标签 + 控件 + 提示」的排版在所有表单里一致，
// 也为后续接入设计体系留出唯一改动点。

const controlClass =
  "w-full rounded-md border border-neutral-300 px-3 py-1.5 text-sm outline-none transition-colors focus:border-neutral-500 disabled:bg-neutral-100 disabled:text-neutral-400";

export interface FieldProps {
  label: string;
  /** 输入框下方的补充说明 */
  hint?: ReactNode;
  /** 该字段是否必填（仅用于展示提示，真正的校验在后端） */
  required?: boolean;
  children: ReactNode;
}

export function Field({ label, hint, required = false, children }: FieldProps) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-xs font-medium text-neutral-600">
        {label}
        {required ? <span className="ml-0.5 text-red-500">*</span> : null}
      </span>
      {children}
      {hint ? <span className="text-xs leading-5 text-neutral-500">{hint}</span> : null}
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
