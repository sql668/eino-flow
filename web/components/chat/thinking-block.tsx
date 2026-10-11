"use client";

import { useEffect, useState } from "react";

// 「思考过程」折叠块。

// 存在的理由：qwen3 这类推理模型会先写一大段思考（实测 8~10 秒）才产出回答，
// 这段时间用户只看到「正在组织回答」，会以为卡住了。把思考内容流式显示出来，
// 等待就有了进度感；回答开始后自动折起来，避免抢正文的注意力。
//
// 视觉上刻意压制：小字号、弱色、纸底 + 细边框，明确它是过程信息而不是答案。

export interface ThinkingBlockProps {
  /** 已收到的思考文本。 */
  text: string;
  /** 是否还在思考（回答尚未开始）。 */
  active: boolean;
}

export function ThinkingBlock({ text, active }: ThinkingBlockProps) {
  const [open, setOpen] = useState(active);

  // 思考中默认展开（此时它是唯一的进展），回答开始后自动折起。
  useEffect(() => {
    setOpen(active);
  }, [active]);

  if (text.trim() === "") {
    return null;
  }

  return (
    <div className="max-w-[90%] self-start rounded border border-grid-soft bg-paper px-3 py-2">
      <button
        type="button"
        className="flex w-full items-center gap-2 text-left"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
      >
        <span className="eyebrow !text-ink-soft">思考过程</span>
        <span className="reading text-[10px] text-ink-soft">
          {active ? "进行中…" : `${Array.from(text).length} 字`}
        </span>
        <span className="ml-auto text-[10px] text-ink-soft">{open ? "收起" : "展开"}</span>
      </button>
      {open ? (
        <p className="mt-1.5 max-h-48 overflow-y-auto whitespace-pre-wrap text-[11px] leading-5 text-ink-muted">
          {text}
        </p>
      ) : null}
    </div>
  );
}
