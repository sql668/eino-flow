"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import { PageContainer } from "@/components/layout/page-container";
import { Button } from "@/components/ui/button";
import { Field, Input, Select } from "@/components/ui/field";
import { chunkApi, datasetsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { ChunkOptions, DatasetInfo, RetrievalConfig } from "@/types/api";

// 知识库设置页：属性 + 分段模式 + 检索设置。
//
// 为什么把「分段模式」也放这里：它是知识库级默认值，
// 上传文档时预填、也可以在工作台里为单个文档临时改（工作台确认后可选写回）。
// 「检索设置」只有这里有写入口——工作台上只读展示，避免每次上传都重配一遍。
export default function DatasetSettingsPage() {
  const params = useParams<{ id: string }>();
  const datasetId = Number(params.id);

  const [dataset, setDataset] = useState<DatasetInfo | null>(null);
  const [options, setOptions] = useState<ChunkOptions | null>(null);
  const [retrieval, setRetrieval] = useState<RetrievalConfig | null>(null);
  const [saving, setSaving] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    if (!Number.isFinite(datasetId) || datasetId <= 0) {
      setError("知识库 ID 不合法");
      return;
    }
    Promise.all([
      datasetsApi.get(datasetId),
      chunkApi.options(datasetId),
      chunkApi.getRetrievalConfig(datasetId),
    ])
      .then(([detail, chunkOptions, retrievalConfig]) => {
        setDataset(detail);
        setOptions(chunkOptions);
        setRetrieval(retrievalConfig);
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : "加载知识库设置失败"));
  }, [datasetId]);

  const handleSave = async (kind: "chunk" | "retrieval") => {
    setSaving(kind);
    setError("");
    setNotice("");
    try {
      if (kind === "chunk" && options) {
        const saved = await chunkApi.saveChunkConfig(datasetId, options.defaults);
        setOptions({ ...options, defaults: saved });
        setNotice("分段模式已保存，新建文档时会预填这套参数");
      }
      if (kind === "retrieval" && retrieval) {
        const saved = await chunkApi.saveRetrievalConfig(datasetId, retrieval);
        setRetrieval(saved);
        setNotice("检索设置已保存，立即对新的检索生效");
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "保存失败");
    } finally {
      setSaving("");
    }
  };

  return (
    <PageContainer className="flex flex-col gap-5 px-8 py-7">
      <nav className="reading text-xs text-ink-soft">
        <Link href="/datasets" className="hover:text-pri">知识库</Link>
        <span className="mx-1.5">/</span>
        <Link href={`/datasets/${datasetId}`} className="hover:text-pri">
          {dataset?.name ?? `#${datasetId}`}
        </Link>
        <span className="mx-1.5">/</span>
        <span className="text-ink-muted">设置</span>
      </nav>

      <header className="flex flex-col gap-1">
        <span className="eyebrow">知识库</span>
        <h1 className="text-[22px] font-semibold tracking-tight text-ink">设置</h1>
        <p className="text-sm leading-6 text-ink-muted">
          分段模式是新建文档时的默认值；检索设置决定「召回多少、要不要重排、低于多少分不要」。
        </p>
      </header>

      {error ? <p className="rounded border border-fault/30 bg-fault-soft px-3 py-2 text-sm text-fault">{error}</p> : null}
      {notice ? (
        <p className="rounded border border-ok/30 bg-ok-soft px-3 py-2 text-sm text-ok">{notice}</p>
      ) : null}

      <section className="flex flex-col gap-3 rounded border border-grid bg-white px-4 py-4">
        <h2 className="text-sm font-semibold text-ink">基本信息</h2>
        <dl className="grid gap-3 sm:grid-cols-2">
          <ReadOnlyRow label="名称" value={dataset?.name ?? "—"} />
          <ReadOnlyRow label="状态" value={dataset?.status ?? "—"} />
          <ReadOnlyRow
            label="向量模型"
            value={dataset?.embeddingModel || "跟随全局默认"}
            hint="更换向量模型会改变向量空间，需要重建索引（在工作台里对每个文档重新处理）"
          />
        </dl>
        <p className="text-[11px] leading-5 text-neutral-400">
          名称与描述的编辑入口将在后续版本补上（当前用接口修改）。
        </p>
      </section>

      <section className="flex flex-col gap-3 rounded border border-grid bg-white px-4 py-4">
        <h2 className="text-sm font-semibold text-ink">分段模式（新建文档的默认值）</h2>
        {options ? (
          <>
            <Field label="模式" hint="按标题适合有章节结构的文档；固定长度适合没有可靠层级的内容。">
              <Select
                value={options.defaults.mode}
                onChange={(event) =>
                  setOptions({
                    ...options,
                    defaults: {
                      ...options.defaults,
                      mode: event.target.value as ChunkOptions["defaults"]["mode"],
                    },
                  })
                }
              >
                {options.modes.map((mode) => (
                  <option key={mode.mode} value={mode.mode} disabled={!mode.implemented}>
                    {mode.label}
                    {mode.implemented ? "" : "（尚未完成）"}
                  </option>
                ))}
              </Select>
            </Field>
            {options.defaults.mode === "parent_child" ? (
              <p className="rounded border border-warn/30 bg-warn-soft px-3 py-2 text-xs leading-5 text-warn">
                父子分段功能尚未完成，敬请期待：子块用于检索、父块作为上下文。
                当前请先选择其它模式。
              </p>
            ) : null}
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="分段长度上限" hint="字符数">
                <Input
                  type="number"
                  value={options.defaults.maxRunes}
                  onChange={(event) =>
                    setOptions({
                      ...options,
                      defaults: { ...options.defaults, maxRunes: Number(event.target.value) },
                    })
                  }
                />
              </Field>
              <Field label="最小长度" hint="短于此长度会与相邻内容合并">
                <Input
                  type="number"
                  value={options.defaults.minRunes}
                  onChange={(event) =>
                    setOptions({
                      ...options,
                      defaults: { ...options.defaults, minRunes: Number(event.target.value) },
                    })
                  }
                />
              </Field>
              <Field label="窗口重叠" hint="默认取上限的 10%">
                <Input
                  type="number"
                  value={options.defaults.overlapRunes}
                  onChange={(event) =>
                    setOptions({
                      ...options,
                      defaults: { ...options.defaults, overlapRunes: Number(event.target.value) },
                    })
                  }
                />
              </Field>
            </div>
            <label className="flex items-center gap-2 text-xs text-ink-muted">
              <input
                type="checkbox"
                checked={options.defaults.keepNoise}
                onChange={(event) =>
                  setOptions({
                    ...options,
                    defaults: { ...options.defaults, keepNoise: event.target.checked },
                  })
                }
              />
              保留页眉 / 页脚 / 页码（默认丢弃）
            </label>
            <div className="flex items-center gap-2">
              <Button
                variant="primary"
                busy={saving === "chunk"}
                onClick={() => void handleSave("chunk")}
              >
                保存分段默认值
              </Button>
              <span className="text-[11px] text-ink-soft">
                清洗规则与更细的参数在「分段与清洗」工作台里调（每个文档可单独确认）
              </span>
            </div>
          </>
        ) : (
          <p className="text-xs text-neutral-400">加载中…</p>
        )}
      </section>

      <section className="flex flex-col gap-3 rounded border border-grid bg-white px-4 py-4">
        <h2 className="text-sm font-semibold text-ink">检索设置</h2>
        {retrieval ? (
          <>
            <div className="grid gap-3 sm:grid-cols-4">
              <Field label="返回条数" hint="最终给模型看几段">
                <Input
                  type="number"
                  value={retrieval.topK}
                  onChange={(event) =>
                    setRetrieval({ ...retrieval, topK: Number(event.target.value) })
                  }
                />
              </Field>
              <Field label="召回条数" hint="重排前的候选数，要 ≥ 返回条数">
                <Input
                  type="number"
                  value={retrieval.candidateTopK}
                  onChange={(event) =>
                    setRetrieval({ ...retrieval, candidateTopK: Number(event.target.value) })
                  }
                />
              </Field>
              <Field
                label="相似度阈值"
                hint="0 表示不启用。低于它直接丢弃，用于挡住「问什么都给一段」"
              >
                <Input
                  type="number"
                  step="0.05"
                  min={0}
                  max={1}
                  value={retrieval.scoreThreshold}
                  onChange={(event) =>
                    setRetrieval({ ...retrieval, scoreThreshold: Number(event.target.value) })
                  }
                />
              </Field>
              <Field label="语义 / 关键词权重" hint="关键词一侧待稀疏检索落地后生效">
                <div className="flex items-center gap-2">
                  <Input
                    type="number"
                    step="0.1"
                    min={0}
                    max={1}
                    value={retrieval.vectorWeight}
                    onChange={(event) => {
                      const vectorWeight = Number(event.target.value);
                      setRetrieval({
                        ...retrieval,
                        vectorWeight,
                        keywordWeight: Number((1 - vectorWeight).toFixed(2)),
                      });
                    }}
                  />
                  <span className="reading text-xs text-ink-soft">
                    / {retrieval.keywordWeight}
                  </span>
                </div>
              </Field>
            </div>
            <label className="flex items-center gap-2 text-xs text-ink-muted">
              <input
                type="checkbox"
                checked={retrieval.rerank}
                onChange={(event) => setRetrieval({ ...retrieval, rerank: event.target.checked })}
              />
              启用重排（需要重排服务可用；未配置或不可用时会降级为向量顺序并在响应里说明）
            </label>
            <div>
              <Button
                variant="primary"
                busy={saving === "retrieval"}
                onClick={() => void handleSave("retrieval")}
              >
                保存检索设置
              </Button>
            </div>
          </>
        ) : (
          <p className="text-xs text-neutral-400">加载中…</p>
        )}
      </section>
    </PageContainer>
  );
}

function ReadOnlyRow({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-[11px] text-ink-soft">{label}</dt>
      <dd className="reading text-sm text-ink">{value}</dd>
      {hint ? <span className="text-[11px] text-ink-soft">{hint}</span> : null}
    </div>
  );
}
