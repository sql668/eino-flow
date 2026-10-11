import type { ReactNode } from "react";

// 仪表读数：本设计的记忆点之一（见 web/AGENTS.md 第 4 节）。
//
// 形态固定为「眉标 + 大号等宽数字 + 单位/说明」，抽出来是因为它必须在全站一致——
// 各页面各写一份，数字的字号、对齐、单位写法很快就会漂移，那就不像仪表了。

export type ReadingTone = "default" | "warn" | "muted";

export interface ReadingProps {
  label: string;
  value: string;
  /** 单位（段 / 字 / ms / 维…），紧贴数字显示 */
  unit?: string;
  /** 数字下方的一行说明 */
  note?: ReactNode;
  tone?: ReadingTone;
  /** 数字字号：sm 用于密集列表，lg 用于页面级读数条 */
  size?: "sm" | "lg";
}

const valueTone: Record<ReadingTone, string> = {
  default: "text-ink",
  warn: "text-warn",
  muted: "text-ink-muted",
};

export function Reading({
  label,
  value,
  unit,
  note,
  tone = "default",
  size = "lg",
}: ReadingProps) {
  const valueClass = size === "lg" ? "text-[22px] leading-7" : "text-[13px] leading-5";
  return (
    <div className="flex min-w-0 flex-col gap-0.5 px-3 py-2 first:pl-0">
      <span className="eyebrow !text-ink-soft">{label}</span>
      <span className="flex items-baseline gap-1">
        {/* 不截断读数：模型名、指纹这类值本身就是信息，截断等于把信息藏起来。
            长值允许换行（break-all 对不含空格的 id 更合适）。 */}
        <span className={`reading break-all font-semibold ${valueClass} ${valueTone[tone]}`}>
          {value}
        </span>
        {unit ? <span className="text-[11px] text-ink-soft">{unit}</span> : null}
      </span>
      {note ? <span className="text-[10px] leading-4 text-ink-soft">{note}</span> : null}
    </div>
  );
}

/** ReadingRow 是读数条的容器：等分列 + 竖直分隔线（仪表盘的一行表头）。 */
export function ReadingRow({
  columns = 3,
  children,
}: {
  columns?: 2 | 3 | 4;
  children: ReactNode;
}) {
  const cols: Record<number, string> = {
    2: "sm:grid-cols-2",
    3: "sm:grid-cols-3",
    4: "sm:grid-cols-4",
  };
  return (
    <dl className={`grid grid-cols-2 divide-grid sm:divide-x ${cols[columns]}`}>{children}</dl>
  );
}
