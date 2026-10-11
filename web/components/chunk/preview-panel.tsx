"use client";

import { useState } from "react";

import { Reading, ReadingRow } from "@/components/ui/reading";
import { Button } from "@/components/ui/button";
import { useChunkWorkbenchStore } from "@/stores/chunk-workbench-store";
import type { PreviewChunk } from "@/types/api";

// 分段工作台右侧：预览面板。
//
// 这里承担三件事（都是 Dify 那版缺的）：
//   1. 预览结果可见且**可控**：进入页面自动算一次，之后改参数要点「预览」重算
//      （大文件上自动重算纯属浪费），并明确标出「结果已过期」；
//   2. 清洗「删了什么」可见：每条规则给命中数、删除字数与被删样本，片段可切换原文/清洗后；
//   3. 健康度提示：哪些片段贴着上限被切开、哪些过短、重叠带来多少重复内容。

export function ChunkPreviewPanel() {
  const preview = useChunkWorkbenchStore((state) => state.preview);
  const previewing = useChunkWorkbenchStore((state) => state.previewing);
  const previewDirty = useChunkWorkbenchStore((state) => state.previewDirty);
  const blockedReason = useChunkWorkbenchStore((state) => state.blockedReason);
  const refreshPreview = useChunkWorkbenchStore((state) => state.refreshPreview);

  // 预览是手动触发的：无论有没有结果，这个按钮都要在，
  // 否则「改了配置却没有可点的入口」会让人以为功能坏了。
  const toolbar = (
    <div className="flex flex-wrap items-center gap-3">
      {previewDirty && !previewing ? (
        <span className="rounded-sm border border-warn/30 bg-warn-soft px-2 py-1 text-[11px] text-warn">
          配置已改动，下面的结果还是上一次的
        </span>
      ) : null}
      {blockedReason ? (
        <span className="text-[11px] text-ink-muted">{blockedReason}</span>
      ) : null}
      <Button variant="primary" busy={previewing} onClick={() => void refreshPreview()}>
        预览
      </Button>
    </div>
  );

  if (!preview) {
    return (
      <div className="flex flex-col items-start gap-3">
        {toolbar}
        <p className="text-sm text-ink-muted">
          {previewing ? "正在按当前配置预览…" : "还没有预览结果"}
        </p>
      </div>
    );
  }

  const { stats, cleaningStats } = preview;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-grid pb-2 text-[11px] text-ink-soft">
        <span className="reading">
          {preview.meta.sourceName || "文档"} · {preview.meta.pageCount} 页 ·{" "}
          {preview.meta.parser}/{preview.meta.parseMode}
        </span>
        <span className="reading">指纹 {preview.configVersion}</span>
        <div className="ml-auto">{toolbar}</div>
      </div>

      {/* 读数条：像仪表一样把关键数字并排摆出来，数字用等宽 + 单位标签 */}
      <ReadingRow columns={4}>
        <Reading label="片段总数" value={String(stats.total)} unit="段" note={`预览前 ${preview.returned}`} />
        <Reading label="平均长度" value={String(stats.avgRunes)} unit="字" note={`最长 ${stats.maxChunkRunes}`} />
        <Reading
          label="贴着上限"
          value={String(stats.atLimit)}
          unit="段"
          note={stats.atLimit > 0 ? "被强制切开" : "无"}
          tone={stats.atLimit > 0 ? "warn" : "muted"}
        />
        <Reading
          label="重叠引入"
          value={String(stats.overlapRunes)}
          unit="字"
          note={`表格 ${stats.tables} · 噪声 ${stats.droppedNoise}`}
        />
      </ReadingRow>

      {preview.hints.length > 0 ? (
        <ul className="flex flex-col gap-1 rounded border border-pri/20 bg-pri-soft px-3 py-2">
          {preview.hints.map((hint) => (
            <li key={hint} className="text-[11px] leading-5 text-pri-ink">
              → {hint}
            </li>
          ))}
        </ul>
      ) : null}

      {cleaningStats.removedRunes > 0 ? (
        <section className="flex flex-col gap-2 rounded border border-grid bg-white px-3 py-2">
          <header className="flex items-center justify-between">
            <span className="text-xs font-medium text-ink">清洗结果</span>
            <span className="reading text-[11px] text-ink-muted">
              {cleaningStats.beforeRunes} 字 → {cleaningStats.afterRunes} 字（删除{" "}
              {cleaningStats.removedRunes} 字）
            </span>
          </header>
          <div className="flex flex-col gap-1.5">
            {preview.cleaning
              .filter((rule) => rule.removedRunes > 0 || rule.note)
              .map((rule) => (
                <div key={rule.key} className="text-[11px] leading-5">
                  <span className="text-neutral-700">
                    {rule.label}：
                    {/* 命中数只对「按模式删除」的规则有意义；像空白规范化这类
                        还可能来自首尾空白裁剪，此时只报删除字数，避免「命中 0 次却删了 9 字」的自相矛盾。 */}
                    {rule.hits > 0 ? `命中 ${rule.hits} 次，` : ""}
                    删除 {rule.removedRunes} 字
                  </span>
                  {rule.note ? <span className="text-ink-soft">（{rule.note}）</span> : null}
                  {rule.samples && rule.samples.length > 0 ? (
                    <div className="mt-0.5 flex flex-wrap gap-1">
                      {rule.samples.map((sample) => (
                        <span
                          key={sample}
                          className="reading rounded-sm bg-fault-soft px-1.5 py-0.5 text-[10px] text-fault line-through"
                          title="被这条规则删除的内容"
                        >
                          {sample}
                        </span>
                      ))}
                    </div>
                  ) : null}
                </div>
              ))}
          </div>
        </section>
      ) : null}

      <div className="flex flex-col gap-2">
        {preview.chunks.map((chunk) => (
          <ChunkCard key={chunk.index} chunk={chunk} />
        ))}
      </div>

      {preview.total > preview.returned ? (
        <p className="text-[11px] text-ink-soft">
          共 {preview.total} 段，此处只预览前 {preview.returned} 段
        </p>
      ) : null}
    </div>
  );
}

/** ChunkCard 渲染单个片段，支持「清洗后 / 原文」切换。 */
function ChunkCard({ chunk }: { chunk: PreviewChunk }) {
  // 默认显示清洗后的内容（那是真正入库的东西）；想看被删了什么就切到原文。
  const [showOriginal, setShowOriginal] = useState(false);
  const changed = chunk.originalText !== "" && chunk.originalText !== chunk.content;
  const text = showOriginal ? chunk.originalText : chunk.content;

  return (
    <article className="rounded border border-grid bg-white px-3 py-2">
      <header className="flex flex-wrap items-center gap-2 text-[11px] text-ink-soft">
        <span className="reading font-medium text-ink-muted">
          #{String(chunk.index).padStart(2, "0")}
        </span>
        <span className="reading">{chunk.charCount} 字</span>
        {chunk.page > 0 ? <span className="reading">第 {chunk.page} 页</span> : null}
        {chunk.titlePath.length > 0 ? <span>{chunk.titlePath.join(" / ")}</span> : null}
        {chunk.kind === "table" ? (
          <span className="rounded-sm bg-grid-soft px-1 py-0.5 text-ink-muted">表格</span>
        ) : null}
        {chunk.overlapRunes > 0 ? (
          <span className="rounded-sm bg-pri-soft px-1 py-0.5 text-pri-ink">
            开头 {chunk.overlapRunes} 字与上一段重叠
          </span>
        ) : null}
        {changed ? (
          <button
            type="button"
            className="ml-auto text-[11px] text-pri hover:underline"
            onClick={() => setShowOriginal((prev) => !prev)}
          >
            {showOriginal ? "看清洗后" : "看原文"}
          </button>
        ) : null}
      </header>
      <p
        className={`mt-1 whitespace-pre-wrap text-xs leading-5 ${
          showOriginal ? "text-ink-soft line-through decoration-fault/40" : "text-ink-muted"
        }`}
      >
        {text.length > 600 ? `${text.slice(0, 600)}…` : text}
      </p>
    </article>
  );
}
