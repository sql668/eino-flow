"use client";

import { useEffect, useRef, useState } from "react";

// 关键词编辑：只读时渲染成一排小标签，点「编辑」切成可增删的编辑态。
//
// 抽成组件的原因：关键词出现在分段浏览页，后续引用详情、召回测试页也会用到；
// 各写一份的话「回车添加」「去重」「上限提示」这些交互细节很快就会不一致。
//
// 交互约定：
//   * 回车或逗号提交一个词；退格清空时删掉最后一个；
//   * 词上限与后端一致（10 个），到上限后输入框禁用并给出提示；
//   * 进入编辑态时把当前值复制到本地草稿，取消即丢弃（避免半成品污染列表）。

export interface KeywordEditorProps {
  keywords: string[];
  /** 词数量上限，与后端 model.MaxKeywordMaxCount 对齐 */
  maxCount?: number;
  busy?: boolean;
  onSave: (keywords: string[]) => void | Promise<void>;
}

export function KeywordEditor({ keywords, maxCount = 10, busy = false, onSave }: KeywordEditorProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<string[]>(keywords);
  const [input, setInput] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);

  // 外部值变化（保存成功回填）时同步草稿，编辑态下不动，避免把用户正在输入的内容冲掉。
  useEffect(() => {
    if (!editing) {
      setDraft(keywords);
    }
  }, [keywords, editing]);

  const atLimit = draft.length >= maxCount;

  const addKeyword = (raw: string) => {
    const value = raw.trim();
    if (!value || atLimit || draft.includes(value)) {
      setInput("");
      return;
    }
    setDraft([...draft, value]);
    setInput("");
  };

  const commit = async () => {
    await onSave(draft);
    setEditing(false);
  };

  if (!editing) {
    return (
      <div className="flex flex-wrap items-center gap-1">
        {keywords.length === 0 ? (
          <span className="text-[11px] text-ink-soft">无关键词</span>
        ) : (
          keywords.map((word) => (
            <span
              key={word}
              className="rounded-sm border border-grid bg-paper px-1.5 py-0.5 text-[11px] text-ink-muted"
            >
              {word}
            </span>
          ))
        )}
        <button
          type="button"
          className="ml-1 text-[11px] text-pri hover:text-pri-ink"
          onClick={() => {
            setDraft(keywords);
            setEditing(true);
          }}
        >
          编辑
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-1">
        {draft.map((word) => (
          <span
            key={word}
            className="flex items-center gap-1 rounded-sm border border-pri/40 bg-pri-soft px-1.5 py-0.5 text-[11px] text-pri-ink"
          >
            {word}
            <button
              type="button"
              aria-label={`删除关键词 ${word}`}
              className="text-pri-ink/70 hover:text-fault"
              onClick={() => setDraft(draft.filter((item) => item !== word))}
            >
              ×
            </button>
          </span>
        ))}
        {draft.length === 0 ? (
          <span className="text-[11px] text-ink-soft">还没有关键词</span>
        ) : null}
      </div>
      <div className="flex items-center gap-1.5">
        <input
          ref={inputRef}
          value={input}
          disabled={atLimit}
          placeholder={atLimit ? `最多 ${maxCount} 个` : "输入后回车添加"}
          className="h-[26px] w-40 rounded-sm border border-grid bg-white px-2 text-[11px] text-ink outline-none placeholder:text-ink-soft focus:border-pri disabled:bg-grid-soft"
          onChange={(event) => setInput(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === ",") {
              event.preventDefault();
              addKeyword(input);
              return;
            }
            // 空输入时退格删最后一个：标签编辑的通用习惯，少点一次鼠标。
            if (event.key === "Backspace" && input === "" && draft.length > 0) {
              setDraft(draft.slice(0, -1));
            }
          }}
        />
        <button
          type="button"
          disabled={busy}
          className="whitespace-nowrap rounded-sm bg-pri px-2 py-1 text-[11px] text-white hover:bg-pri-ink disabled:opacity-60"
          onClick={() => void commit()}
        >
          {busy ? "保存中…" : "保存"}
        </button>
        <button
          type="button"
          className="whitespace-nowrap rounded-sm border border-grid px-2 py-1 text-[11px] text-ink-muted hover:border-ink-soft"
          onClick={() => {
            setEditing(false);
            setInput("");
          }}
        >
          取消
        </button>
      </div>
      <span className="text-[10px] leading-4 text-ink-soft">
        关键词会进入全文检索的输入文本，改动立即影响后续检索
      </span>
    </div>
  );
}
