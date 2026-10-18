"use client";

import { useState } from "react";

import type { MessageToolCallInfo } from "@/types/api";

const toolLabels: Record<string, string> = {
  knowledge_retrieval: "知识库检索",
  calculator: "计算",
};

const statusStyles = {
  ok: { label: "完成", text: "text-ok", rail: "border-l-ok" },
  warn: { label: "已跳过", text: "text-warn", rail: "border-l-warn" },
  fault: { label: "失败", text: "text-fault", rail: "border-l-fault" },
} as const;

type StatusStyle = (typeof statusStyles)[keyof typeof statusStyles];

function statusOf(call: MessageToolCallInfo): StatusStyle {
  if (call.skipped) {
    return statusStyles.warn;
  }
  if (call.failed) {
    return statusStyles.fault;
  }
  return statusStyles.ok;
}

function firstLine(text: string): string {
  const line = text
    .split("\n")
    .map((item) => item.trim())
    .find((item) => item !== "");
  return line ?? "";
}

/**
 * 工具调用记录块。
 *
 * 视觉上是一条「调用账本」：左侧细轨表示状态，行首两位序号表示真实调用顺序。
 * 默认只露出工具名、状态、耗时与第一行摘要，点开才看完整入参与结果——
 * 智能体一次问答可能调用多次工具，全部展开会把正文挤到屏幕外。
 */
export function ToolCallsBlock({ calls }: { calls: MessageToolCallInfo[] }) {
  if (calls.length === 0) {
    return null;
  }
  const totalMS = calls.reduce((sum, call) => sum + call.durationMs, 0);
  return (
    <section className="max-w-[90%] self-start rounded border border-grid bg-white">
      <header className="flex flex-wrap items-baseline gap-2 border-b border-grid-soft px-3 py-2">
        <span className="eyebrow !text-ink-soft">工具调用</span>
        <span className="reading text-[10px] text-ink-soft">
          {calls.length} 次 · {totalMS} ms
        </span>
      </header>
      <ol className="divide-y divide-grid-soft">
        {calls.map((call) => (
          <ToolCallRow key={`${call.round}-${call.callId}`} call={call} />
        ))}
      </ol>
    </section>
  );
}

function ToolCallRow({ call }: { call: MessageToolCallInfo }) {
  const [open, setOpen] = useState(false);
  const status = statusOf(call);
  const summary = firstLine(call.result) || firstLine(call.arguments);

  return (
    <li className={`border-l-2 ${status.rail}`}>
      <button
        type="button"
        className="flex w-full items-baseline gap-2 px-2.5 py-1.5 text-left"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
      >
        <span className="reading w-5 shrink-0 text-[10px] text-ink-soft">
          {String(call.round).padStart(2, "0")}
        </span>
        <span className="shrink-0 text-[12px] font-medium text-ink">
          {toolLabels[call.name] ?? call.name}
        </span>
        <span className={`shrink-0 text-[10px] ${status.text}`}>{status.label}</span>
        <span className="reading shrink-0 text-[10px] text-ink-soft">{call.durationMs} ms</span>
        <span className="min-w-0 flex-1 truncate text-[11px] text-ink-muted">{summary}</span>
        <span className="shrink-0 text-[10px] text-ink-soft">{open ? "收起" : "展开"}</span>
      </button>
      {open ? (
        <div className="grid gap-2 px-2.5 pb-2.5 pl-9">
          <Readout label="入参" value={call.arguments} />
          <Readout
            label="结果"
            value={call.result}
            tone={status === statusStyles.fault ? "text-fault" : ""}
          />
        </div>
      ) : null}
    </li>
  );
}

function Readout({ label, value, tone = "" }: { label: string; value: string; tone?: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="eyebrow !text-ink-soft">{label}</span>
      <pre
        className={`max-h-40 overflow-y-auto whitespace-pre-wrap break-words rounded bg-paper px-2 py-1.5 text-[11px] leading-5 text-ink-muted ${tone}`}
      >
        {value.trim() === "" ? "—" : value}
        {/* 摘要可能被截断，这是刻意的：完整片段通过引用来源回查。 */}
      </pre>
    </div>
  );
}
