"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

import { PageContainer } from "@/components/layout/page-container";
import { Button } from "@/components/ui/button";
import { Field, Input, Panel, Select } from "@/components/ui/field";
import { chunkApi, datasetsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type {
  DatasetInfo,
  HybridRank,
  RecallTestPayload,
  RetrievalConfig,
  RetrievalRunInfo,
  SearchHit,
  SearchMode,
  SearchResult,
} from "@/types/api";

// 检索追踪 / 召回测试。
//
// 两块职责：看"当时发生了什么"（只读历史），以及"换组参数会怎样"（临时测试）。
// 历史与测试结果都是台账式读数：分数用等宽字体，参数直接标在每组结果头上。

interface RecallGroup {
  id: number;
  payload: RecallTestPayload;
  result: SearchResult;
}

const modeLabels: Record<SearchMode, string> = {
  vector: "向量",
  fulltext: "全文",
  hybrid: "混合",
};

const rankLabels: Record<HybridRank, string> = {
  weighted: "加权",
  rrf: "RRF",
};

export default function RetrievalTracePage() {
  const params = useParams<{ id: string }>();
  const datasetId = Number(params.id);

  const [dataset, setDataset] = useState<DatasetInfo | null>(null);
  const [form, setForm] = useState<RetrievalConfig | null>(null);
  const [query, setQuery] = useState("");
  const [groups, setGroups] = useState<RecallGroup[]>([]);
  const [history, setHistory] = useState<RetrievalRunInfo[]>([]);
  const [historyTotal, setHistoryTotal] = useState(0);
  const [loadingHistory, setLoadingHistory] = useState(true);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const nextGroupID = useRef(1);

  const loadHistory = useCallback(async () => {
    if (!Number.isFinite(datasetId) || datasetId <= 0) {
      return;
    }
    setLoadingHistory(true);
    try {
      const result = await chunkApi.listRetrievalRuns(datasetId, { pageSize: 20 });
      setHistory(result.items);
      setHistoryTotal(result.total);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "加载检索历史失败");
    } finally {
      setLoadingHistory(false);
    }
  }, [datasetId]);

  useEffect(() => {
    if (!Number.isFinite(datasetId) || datasetId <= 0) {
      setError("知识库 ID 不合法");
      return;
    }
    Promise.all([datasetsApi.get(datasetId), chunkApi.getRetrievalConfig(datasetId)])
      .then(([detail, config]) => {
        setDataset(detail);
        setForm(config);
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : "加载知识库失败"));
    void loadHistory();
  }, [datasetId, loadHistory]);

  const runTest = async () => {
    if (!form) {
      return;
    }
    if (query.trim() === "") {
      setError("先输入一个用于测试的问题");
      return;
    }
    setRunning(true);
    setError("");
    const payload: RecallTestPayload = {
      query: query.trim(),
      mode: form.searchMode,
      topK: form.topK,
      candidateTopK: form.candidateTopK,
      scoreThreshold: form.scoreThreshold,
      rerank: form.rerank,
      hybridRank: form.searchMode === "hybrid" ? form.hybridRank : undefined,
      vectorWeight: form.vectorWeight,
      fullTextWeight: form.keywordWeight,
    };
    try {
      const result = await chunkApi.recallTest(datasetId, payload);
      setGroups((prev) => [{ id: nextGroupID.current++, payload, result }, ...prev].slice(0, 4));
      await loadHistory();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "召回测试失败");
    } finally {
      setRunning(false);
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
        <span className="text-ink-muted">检索追踪</span>
      </nav>

      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <span className="eyebrow">检索</span>
          <h1 className="text-[22px] font-semibold tracking-tight text-ink">追踪与召回测试</h1>
          <p className="max-w-2xl text-sm leading-6 text-ink-muted">
            历史记录是当时的快照，不会重新检索；测试参数只影响本次，不改知识库设置。
          </p>
        </div>
        <span className="reading text-xs text-ink-soft">
          历史 {historyTotal} 次{loadingHistory ? " · 刷新中" : ""}
        </span>
      </header>

      {error ? (
        <p className="rounded border border-fault/30 bg-fault-soft px-3 py-2 text-sm text-fault">
          {error}
        </p>
      ) : null}

      <div className="grid gap-4 xl:grid-cols-[340px_minmax(0,1fr)]">
        <Panel
          eyebrow="测试台"
          title="用临时参数试一次"
          description="参数默认取知识库设置，改动只作用于本次测试。"
          reading={form ? `${modeLabels[form.searchMode]} · TopK ${form.topK}` : undefined}
        >
          {form ? (
            <div className="flex flex-col gap-3">
              <Field label="问题" required>
                <textarea
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  rows={3}
                  placeholder="例如：怎么测量直流电压？"
                  className="w-full rounded border border-grid bg-white px-3 py-2 text-sm text-ink outline-none transition-colors placeholder:text-ink-soft focus:border-pri"
                />
              </Field>
              <Field label="检索方式">
                <Select
                  value={form.searchMode}
                  onChange={(event) =>
                    setForm({ ...form, searchMode: event.target.value as SearchMode })
                  }
                >
                  {Object.entries(modeLabels).map(([value, label]) => (
                    <option key={value} value={value}>{label}</option>
                  ))}
                </Select>
              </Field>
              {form.searchMode === "hybrid" ? (
                <Field label="融合方式" hint="加权看分数，RRF 只看两路名次">
                  <Select
                    value={form.hybridRank}
                    onChange={(event) =>
                      setForm({ ...form, hybridRank: event.target.value as HybridRank })
                    }
                  >
                    {Object.entries(rankLabels).map(([value, label]) => (
                      <option key={value} value={value}>{label}</option>
                    ))}
                  </Select>
                </Field>
              ) : null}
              <div className="grid grid-cols-2 gap-3">
                <Field label="返回条数">
                  <Input
                    type="number"
                    min={1}
                    value={form.topK}
                    onChange={(event) => setForm({ ...form, topK: Number(event.target.value) })}
                  />
                </Field>
                <Field label="召回条数">
                  <Input
                    type="number"
                    min={1}
                    value={form.candidateTopK}
                    onChange={(event) =>
                      setForm({ ...form, candidateTopK: Number(event.target.value) })
                    }
                  />
                </Field>
              </div>
              <Field
                label="相似度阈值"
                hint="0 表示不启用；全文检索下没有向量分，这一项不生效"
              >
                <Input
                  type="number"
                  step="0.05"
                  min={0}
                  max={1}
                  disabled={form.searchMode === "fulltext"}
                  value={form.scoreThreshold}
                  onChange={(event) =>
                    setForm({ ...form, scoreThreshold: Number(event.target.value) })
                  }
                />
              </Field>
              <label className="flex items-center gap-2 text-xs text-ink-muted">
                <input
                  type="checkbox"
                  checked={form.rerank}
                  onChange={(event) => setForm({ ...form, rerank: event.target.checked })}
                  className="h-3.5 w-3.5 accent-pri"
                />
                启用重排
              </label>
              <div className="flex flex-wrap items-center gap-2 border-t border-grid-soft pt-3">
                <Button variant="primary" busy={running} onClick={() => void runTest()}>
                  执行测试
                </Button>
                <Button disabled={groups.length === 0} onClick={() => setGroups([])}>
                  清空对比
                </Button>
              </div>
              <span className="text-[11px] leading-4 text-ink-soft">
                连续执行会触发频率限制；最多保留最近 4 组结果。
              </span>
            </div>
          ) : (
            <p className="text-xs text-ink-soft">加载中…</p>
          )}
        </Panel>

        <div className="flex flex-col gap-4">
          {groups.length === 0 ? (
            <div className="flex min-h-48 items-center justify-center rounded border border-dashed border-grid text-xs text-ink-soft">
              还没有测试结果。执行一次后，多组参数会在这里并列对比。
            </div>
          ) : (
            <div className="grid gap-4 min-[1500px]:grid-cols-2">
              {groups.map((group, index) => (
                <ResultPanel
                  key={group.id}
                  group={group}
                  label={`组 ${groups.length - index}`}
                  onRemove={() => setGroups((prev) => prev.filter((item) => item.id !== group.id))}
                />
              ))}
            </div>
          )}

          <Panel
            eyebrow="历史"
            title="检索记录"
            description="按时间倒序；展开可看每次运行的参数快照与阶段分数。"
            reading={`最近 ${history.length} 条`}
            actions={<Button busy={loadingHistory} onClick={() => void loadHistory()}>刷新</Button>}
          >
            {history.length === 0 && !loadingHistory ? (
              <p className="py-6 text-center text-xs text-ink-soft">还没有检索记录</p>
            ) : (
              <ul className="divide-y divide-grid-soft">
                {history.map((run) => (
                  <HistoryRun key={run.id} run={run} />
                ))}
              </ul>
            )}
          </Panel>
        </div>
      </div>
    </PageContainer>
  );
}

function ResultPanel({
  group,
  label,
  onRemove,
}: {
  group: RecallGroup;
  label: string;
  onRemove: () => void;
}) {
  const { result } = group;
  return (
    <Panel
      eyebrow={label}
      title={describePayload(group.payload)}
      reading={`召回 ${result.recalled} · 命中 ${result.hits.length} · 筛掉 ${result.filtered}`}
      description={`${result.latencyMs} ms · ${result.embeddingModel || "未记录向量模型"}${
        result.rerankSkipped ? ` · ${result.rerankSkipped}` : ""
      }`}
      actions={<Button onClick={onRemove}>移除</Button>}
    >
      {result.hits.length === 0 ? (
        <p className="py-4 text-center text-xs text-ink-soft">本次没有命中</p>
      ) : (
        <ul className="divide-y divide-grid-soft">
          {result.hits.map((hit) => (
            <ResultHit key={`${hit.rank}-${hit.segmentId}`} hit={hit} />
          ))}
        </ul>
      )}
    </Panel>
  );
}

function ResultHit({ hit }: { hit: SearchHit }) {
  return (
    <li className="border-l-2 border-l-pri/40 py-2 pl-2.5">
      <details>
        <summary className="flex cursor-pointer list-none flex-wrap items-baseline gap-2">
          <span className="reading w-5 shrink-0 text-[10px] text-ink-soft">
            {String(hit.rank).padStart(2, "0")}
          </span>
          <span className="min-w-0 flex-1 truncate text-[12px] text-ink">
            {hit.documentName}
            {hit.page > 0 ? ` · 第 ${hit.page} 页` : ""}
            {hit.titlePath.length > 0 ? ` · ${hit.titlePath.join(" / ")}` : ""}
          </span>
          <ScoreReading hit={hit} />
        </summary>
        <p className="mt-2 whitespace-pre-wrap rounded bg-paper px-2 py-1.5 text-[11px] leading-5 text-ink-muted">
          {hit.content}
        </p>
      </details>
    </li>
  );
}

function HistoryRun({ run }: { run: RetrievalRunInfo }) {
  return (
    <li className="py-2">
      <details>
        <summary className="flex cursor-pointer list-none flex-wrap items-baseline gap-2">
          <span className="reading shrink-0 text-[10px] text-ink-soft">
            {formatTime(run.createdAt)}
          </span>
          <span className="shrink-0 text-[10px] text-ink-muted">
            {modeLabels[run.mode] ?? run.mode}
          </span>
          <span className="min-w-0 flex-1 truncate text-[12px] text-ink">{run.query}</span>
          <span className="reading shrink-0 text-[10px] text-ink-soft">
            召回 {run.candidateCount} · 命中 {run.hitCount} · 筛掉 {run.config.filteredByThreshold ?? 0}
          </span>
          {run.errorMessage ? <span className="text-[10px] text-fault">失败</span> : null}
        </summary>
        <div className="mt-2 flex flex-col gap-2 border-t border-grid-soft pt-2">
          <p className="reading text-[10px] leading-5 text-ink-soft">
            {run.config.mode
              ? `TopK ${run.config.topK} · 候选 ${run.config.candidateTopK} · 阈值 ${run.config.scoreThreshold.toFixed(2)} · ${
                  run.config.rerank ? "重排开" : "重排关"
                }${run.config.rerankSkipped ? ` · ${run.config.rerankSkipped}` : ""}`
              : "参数快照：当时未记录"}
          </p>
          {run.errorMessage ? (
            <p className="text-[11px] text-fault">{run.errorMessage}</p>
          ) : null}
          {run.hits.length === 0 ? (
            <p className="text-[11px] text-ink-soft">本次没有命中明细</p>
          ) : (
            <ul className="divide-y divide-grid-soft">
              {run.hits.map((hit) => (
                <li key={hit.id} className="flex flex-wrap items-baseline gap-2 py-1.5">
                  <span className="reading w-5 shrink-0 text-[10px] text-ink-soft">
                    {String(hit.rank).padStart(2, "0")}
                  </span>
                  <span className="reading shrink-0 rounded bg-paper px-1.5 py-0.5 text-[10px] text-ink-muted">
                    {stageLabel(hit.stage)}
                  </span>
                  <span className="min-w-0 flex-1 truncate text-[11px] text-ink-muted">
                    {hit.documentName || `片段 #${hit.segmentId}`}
                    {hit.page > 0 ? ` · 第 ${hit.page} 页` : ""}
                    {hit.titlePath.length > 0 ? ` · ${hit.titlePath.join(" / ")}` : ""}
                  </span>
                  <ScoreReading hit={hit} />
                </li>
              ))}
            </ul>
          )}
        </div>
      </details>
    </li>
  );
}

function ScoreReading({ hit }: { hit: SearchHit }) {
  return (
    <span className="reading shrink-0 text-[10px] text-ink-soft">
      排序 {hit.score.toFixed(3)}
      {hit.vectorScore !== undefined ? ` · 向量 ${hit.vectorScore.toFixed(3)}` : ""}
      {hit.fullTextScore !== undefined ? ` · 关键词 ${hit.fullTextScore.toFixed(3)}` : ""}
      {hit.rerankScore !== undefined ? ` · 重排 ${hit.rerankScore.toFixed(3)}` : ""}
    </span>
  );
}

function describePayload(payload: RecallTestPayload): string {
  const parts = [modeLabels[payload.mode ?? "vector"]];
  if (payload.mode === "hybrid" && payload.hybridRank) {
    parts.push(rankLabels[payload.hybridRank]);
  }
  parts.push(`TopK ${payload.topK ?? 0}`);
  parts.push(`阈值 ${(payload.scoreThreshold ?? 0).toFixed(2)}`);
  parts.push(payload.rerank ? "重排开" : "重排关");
  return parts.join(" · ");
}

function stageLabel(stage: string): string {
  switch (stage) {
    case "vector":
      return "向量召回";
    case "fulltext":
      return "全文召回";
    case "hybrid":
      return "混合召回";
    case "rerank":
      return "重排";
    default:
      return stage;
  }
}

function formatTime(raw: string): string {
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) {
    return raw;
  }
  return date.toLocaleString("zh-CN", { hour12: false });
}
