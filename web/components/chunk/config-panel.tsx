"use client";

import { Button } from "@/components/ui/button";
import { Field, Input, Select } from "@/components/ui/field";
import { ruleEnabled, rulePattern, useChunkWorkbenchStore } from "@/stores/chunk-workbench-store";
import type { RetrievalConfig } from "@/types/api";

// 分段工作台左侧：配置面板。
//
// 与 Dify 那套的差异：检索参数在这里**只读展示**（它是知识库级设置，
// 每次上传都重配一遍是重复劳动），真正的修改入口在知识库设置页。

export function ChunkConfigPanel({ retrieval }: { retrieval: RetrievalConfig | null }) {
  const options = useChunkWorkbenchStore((state) => state.options);
  const config = useChunkWorkbenchStore((state) => state.config);
  const setConfig = useChunkWorkbenchStore((state) => state.setConfig);
  const toggleRule = useChunkWorkbenchStore((state) => state.toggleRule);
  const setRulePattern = useChunkWorkbenchStore((state) => state.setRulePattern);
  const applyPreset = useChunkWorkbenchStore((state) => state.applyPreset);
  const resetToDatasetDefault = useChunkWorkbenchStore((state) => state.resetToDatasetDefault);

  if (!options) {
    return <p className="text-sm text-ink-muted">加载配置项…</p>;
  }

  const currentMode = options.modes.find((item) => item.mode === config.mode);
  const maxOverlap = Math.floor(config.maxRunes / 2);

  return (
    <div className="flex flex-col gap-5">
      <section className="flex flex-col gap-3">
        <header className="flex items-center justify-between border-b border-grid pb-2">
          <span className="eyebrow">分段</span>
          <Button onClick={() => void resetToDatasetDefault()}>恢复默认</Button>
        </header>

        <div className="flex flex-wrap gap-2">
          {[
            { key: "manual", label: "手册 / 说明书" },
            { key: "paper", label: "论文 / 报告" },
            { key: "log", label: "聊天 / 日志" },
            { key: "record", label: "逐条记录" },
          ].map((preset) => (
            <Button key={preset.key} onClick={() => applyPreset(preset.key)}>
              {preset.label}
            </Button>
          ))}
          <span className="self-center text-[11px] text-ink-soft">预设只改切分参数</span>
        </div>

        <Field label="分段模式" hint="按标题适合有章节结构的文档；固定长度适合没有可靠层级的内容。">
          <Select
            value={config.mode}
            onChange={(event) => setConfig({ mode: event.target.value as typeof config.mode })}
          >
            {options.modes.map((mode) => (
              <option key={mode.mode} value={mode.mode} disabled={!mode.implemented}>
                {mode.label}
                {mode.implemented ? "" : "（尚未完成）"}
              </option>
            ))}
          </Select>
        </Field>

        {currentMode?.implemented === false ? (
          <p className="rounded border border-warn/30 bg-warn-soft px-3 py-2 text-xs leading-5 text-warn">
            {currentMode.label}功能尚未完成，敬请期待。当前请先选择其它模式。
          </p>
        ) : (
          <p className="text-xs leading-5 text-ink-soft">{currentMode?.description}</p>
        )}

        {config.mode === "separator" ? (
          <Field label="分隔符" required hint={String.raw`例如空行写作 \n\n（按回车换行用 \n）`}>
            <Input
              value={config.separator}
              onChange={(event) => setConfig({ separator: event.target.value })}
              placeholder={String.raw`\n\n`}
            />
          </Field>
        ) : null}

        <div className="grid grid-cols-2 gap-3">
          <Field
            label="分段长度上限"
            hint={`字符数，可填 ${options.limits.minMaxRunes}~${options.limits.maxMaxRunes}。中文一个字算一个字符。`}
          >
            <Input
              type="number"
              min={options.limits.minMaxRunes}
              max={options.limits.maxMaxRunes}
              value={config.maxRunes}
              onChange={(event) => {
                const next = Number(event.target.value);
                // 上限变化时同步收敛重叠：后端也会校验，这里顺手避免提交注定失败的配置。
                const overlapRunes = Math.min(config.overlapRunes, Math.floor(next / 2));
                setConfig({ maxRunes: next, overlapRunes });
              }}
            />
          </Field>
          <Field label="最小长度" hint="短于此长度的片段会与相邻内容合并，避免「半句话」。">
            <Input
              type="number"
              min={0}
              max={options.limits.maxMinRunes}
              value={config.minRunes}
              onChange={(event) => setConfig({ minRunes: Number(event.target.value) })}
            />
          </Field>
        </div>

        <Field
          label="窗口重叠"
          hint="相邻片段之间重复的字符数，默认取上限的 10%。只在同一章节内被长度切开的片段之间生效，标题与表格处不重叠。"
        >
          <Input
            type="number"
            min={0}
            max={maxOverlap}
            value={config.overlapRunes}
            onChange={(event) => setConfig({ overlapRunes: Number(event.target.value) })}
          />
        </Field>
        <span className="reading -mt-2 text-[11px] text-ink-soft">
          当前 {config.overlapRunes} 字 · 上限 {maxOverlap} 字（分段长度的一半）
        </span>

        <label className="flex items-center gap-2 text-xs text-ink-muted">
          <input
            type="checkbox"
            checked={config.keepNoise}
            onChange={(event) => setConfig({ keepNoise: event.target.checked })}
          />
          保留页眉 / 页脚 / 页码（默认丢弃）
        </label>

        {/* 关键词：入库时自动抽，抽不到也可以入库后再手工补（分段浏览页）。 */}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-t border-grid-soft pt-3">
          <label className="flex items-center gap-2 text-xs text-ink-muted">
            <input
              type="checkbox"
              checked={config.keywords?.enabled ?? true}
              onChange={(event) =>
                setConfig({
                  keywords: {
                    enabled: event.target.checked,
                    maxCount: config.keywords?.maxCount ?? 5,
                  },
                })
              }
            />
            自动抽取分段关键词
          </label>
          <div className="flex items-center gap-1.5">
            <span className="text-[11px] text-ink-soft">每段最多</span>
            <Input
              type="number"
              min={1}
              max={10}
              className="w-16"
              value={config.keywords?.maxCount ?? 5}
              onChange={(event) =>
                setConfig({
                  keywords: {
                    enabled: config.keywords?.enabled ?? true,
                    maxCount: Number(event.target.value),
                  },
                })
              }
            />
          </div>
        </div>
      </section>

      <section className="flex flex-col gap-3 border-t border-grid-soft pt-4">
        <header className="flex items-center justify-between">
          <span className="eyebrow">文本清洗</span>
          <span className="text-[11px] text-ink-soft">默认全部关闭</span>
        </header>

        <div className="flex flex-col gap-2">
          {options.cleaningRules.map((rule) => {
            const enabled = ruleEnabled(config.cleaning, rule.key);
            return (
              <div key={rule.key} className="rounded border border-grid px-3 py-2">
                <label className="flex items-start gap-2">
                  <input
                    type="checkbox"
                    className="mt-0.5"
                    checked={enabled}
                    onChange={(event) => toggleRule(rule.key, event.target.checked)}
                  />
                  <span className="flex flex-col gap-0.5">
                    <span className="flex items-center gap-1.5 text-xs font-medium text-ink">
                      {rule.label}
                      {rule.risk === "high" ? (
                        <span className="rounded-sm bg-warn-soft px-1 py-0.5 text-[10px] text-warn">
                          会改动内容
                        </span>
                      ) : null}
                    </span>
                    <span className="text-[11px] leading-5 text-ink-soft">
                      {rule.description}
                    </span>
                  </span>
                </label>
                {rule.needsPattern && enabled ? (
                  <div className="mt-2 pl-6">
                    <Input
                      value={rulePattern(config.cleaning, rule.key)}
                      onChange={(event) => setRulePattern(rule.key, event.target.value)}
                      placeholder={String.raw`正则，例如 第 \d+ 页 共 \d+ 页`}
                    />
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      </section>

      <section className="flex flex-col gap-2 border-t border-grid-soft pt-4">
        <header className="flex items-center justify-between">
          <span className="eyebrow">检索设置</span>
          <a
            href={`/datasets/${useChunkWorkbenchStore.getState().datasetId}/settings`}
            className="text-[11px] text-pri hover:underline"
          >
            去知识库设置修改
          </a>
        </header>
        {retrieval ? (
          <dl className="grid grid-cols-2 gap-x-3 gap-y-1.5 text-[11px] text-ink-muted">
            <ReadOnlyItem label="返回条数" value={String(retrieval.topK)} />
            <ReadOnlyItem label="召回条数" value={String(retrieval.candidateTopK)} />
            <ReadOnlyItem
              label="相似度阈值"
              value={retrieval.scoreThreshold > 0 ? String(retrieval.scoreThreshold) : "未启用"}
            />
            <ReadOnlyItem label="重排" value={retrieval.rerank ? "开启" : "关闭"} />
            <ReadOnlyItem
              label="语义 / 关键词"
              value={`${retrieval.vectorWeight} / ${retrieval.keywordWeight}`}
            />
            <ReadOnlyItem label="检索方式" value={searchModeLabel(retrieval.searchMode)} />
          </dl>
        ) : (
          <p className="text-[11px] text-ink-soft">加载中…</p>
        )}
        <p className="text-[11px] leading-5 text-ink-soft">
          检索参数属于知识库级设置，这里只读展示；每次上传文档都重配一遍是重复劳动。
        </p>
      </section>
    </div>
  );
}

function ReadOnlyItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-2">
      <dt className="text-ink-soft">{label}</dt>
      <dd className="reading text-ink">{value}</dd>
    </div>
  );
}

/** 检索方式的中文名。混合检索时把融合方式一起写出来，
 * 因为这个只读区要回答的问题就是「这个库现在到底怎么找」。 */
function searchModeLabel(mode: RetrievalConfig["searchMode"]): string {
  if (mode === "fulltext") {
    return "全文";
  }
  if (mode === "hybrid") {
    return "混合";
  }
  return "向量";
}
