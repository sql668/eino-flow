"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import { PageContainer } from "@/components/layout/page-container";
import { Button } from "@/components/ui/button";
import { Field, Input, Select } from "@/components/ui/field";
import { chunkApi, datasetsApi } from "@/lib/api/console";
import { modelApi } from "@/lib/api/models";
import { ApiError } from "@/lib/http";
import type {
  ChunkOptions,
  DatasetInfo,
  HybridRank,
  ModelInstanceInfo,
  RetrievalConfig,
  SearchMode,
} from "@/types/api";

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
  // 可选的向量模型与该知识库当前的绑定（用于「这个库用哪个向量模型」）。
  const [embeddingModels, setEmbeddingModels] = useState<ModelInstanceInfo[]>([]);
  const [boundEmbeddingId, setBoundEmbeddingId] = useState(0);
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
    // 向量模型列表与绑定单独拉：任一项失败不该让整页设置都打不开。
    Promise.all([
      modelApi.listModels("embedding"),
      modelApi.listBindings("dataset", undefined, datasetId),
    ])
      .then(([models, bindings]) => {
        setEmbeddingModels(models);
        const bound = bindings.find((item) => item.modelType === "embedding");
        setBoundEmbeddingId(bound?.instanceId ?? 0);
      })
      .catch(() => setEmbeddingModels([]));
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

  // 切换该知识库的向量模型。
  //
  // 立即保存而不是等「保存」按钮：它是一次绑定操作（不是一段表单），
  // 拖到后面统一提交会让「我已改过但还没生效」这种中间态一直挂在界面上。
  const handleEmbeddingChange = async (instanceId: number) => {
    setSaving("embedding");
    setError("");
    setNotice("");
    try {
      if (instanceId === 0) {
        await modelApi.unbind("dataset", "embedding", undefined, datasetId);
        setBoundEmbeddingId(0);
        setNotice("已改为跟随全局默认向量模型");
      } else {
        await modelApi.bind({
          scope: "dataset",
          datasetId,
          modelType: "embedding",
          modelInstanceId: instanceId,
        });
        setBoundEmbeddingId(instanceId);
        setNotice("向量模型已绑定到本知识库；已入库的文档需要重新入库才能用新模型");
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "切换向量模型失败");
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
        <dl className="grid gap-3 sm:grid-cols-3">
          <ReadOnlyRow label="名称" value={dataset?.name ?? "—"} />
          <ReadOnlyRow label="状态" value={dataset?.status ?? "—"} />
          <div className="flex flex-col gap-0.5">
            <dt className="text-[11px] text-ink-soft">向量模型</dt>
            <dd>
              <Select
                value={boundEmbeddingId}
                disabled={saving === "embedding"}
                onChange={(event) => void handleEmbeddingChange(Number(event.target.value))}
              >
                <option value={0}>
                  跟随全局默认
                </option>
                {embeddingModels.map((model) => (
                  <option key={model.id} value={model.id}>
                    {model.displayName || model.modelName} · {model.providerName}
                  </option>
                ))}
              </Select>
            </dd>
            <span className="text-[11px] leading-4 text-ink-soft">
              换模型会改变向量空间：已入库的文档要用新模型重新入库（向量维度不同时必须重建集合）
            </span>
          </div>
        </dl>
        <p className="text-[11px] leading-5 text-ink-soft">
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
            {/* 关键词抽取：默认开启。关掉不只是少一列展示——关键词是全文检索的输入之一。 */}
            <div className="flex flex-col gap-2 border-t border-grid-soft pt-3">
              <span className="eyebrow !text-ink-soft">分段关键词</span>
              {/* 勾选框与数字输入按底边对齐：输入框有标签行，勾选框没有，
                  否则两者会错开一行的高度。 */}
              <div className="flex flex-wrap items-end gap-x-4 gap-y-2">
                <label className="flex h-[34px] items-center gap-2 text-xs text-ink-muted">
                  <input
                    type="checkbox"
                    checked={options.defaults.keywords?.enabled ?? true}
                    onChange={(event) =>
                      setOptions({
                        ...options,
                        defaults: {
                          ...options.defaults,
                          keywords: {
                            enabled: event.target.checked,
                            maxCount: options.defaults.keywords?.maxCount ?? 5,
                          },
                        },
                      })
                    }
                  />
                  入库时自动抽取
                </label>
                <Field label="每段最多" hint="关键词参与全文检索，也可以入完库后在分段浏览页手工修改">
                  <Input
                    type="number"
                    min={1}
                    max={10}
                    className="w-24"
                    value={options.defaults.keywords?.maxCount ?? 5}
                    onChange={(event) =>
                      setOptions({
                        ...options,
                        defaults: {
                          ...options.defaults,
                          keywords: {
                            enabled: options.defaults.keywords?.enabled ?? true,
                            maxCount: Number(event.target.value),
                          },
                        },
                      })
                    }
                  />
                </Field>
              </div>
            </div>
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
            <SearchModePicker
              value={retrieval.searchMode}
              onChange={(searchMode) => setRetrieval({ ...retrieval, searchMode })}
            />

            {/* 混合检索才需要融合方式：权重可调、RRF 免调权重（只看名次）。 */}
            {retrieval.searchMode === "hybrid" ? (
              <div className="flex flex-col gap-3 rounded border border-grid-soft bg-paper px-3 py-3">
                <div className="flex flex-wrap items-center gap-4">
                  <span className="eyebrow !text-ink-soft">融合方式</span>
                  <RankPicker
                    value={retrieval.hybridRank}
                    onChange={(hybridRank) => setRetrieval({ ...retrieval, hybridRank })}
                  />
                </div>
                {retrieval.hybridRank === "weighted" ? (
                  <div className="flex flex-col gap-2">
                    <div className="flex items-center gap-3">
                      <span className="w-16 text-xs text-ink-muted">语义权重</span>
                      <input
                        type="range"
                        min={0}
                        max={1}
                        step={0.05}
                        value={retrieval.vectorWeight}
                        className="h-1 w-full max-w-xs accent-pri"
                        onChange={(event) => {
                          const vectorWeight = Number(event.target.value);
                          setRetrieval({
                            ...retrieval,
                            vectorWeight,
                            keywordWeight: Number((1 - vectorWeight).toFixed(2)),
                          });
                        }}
                      />
                      <span className="reading text-xs text-ink">
                        {retrieval.vectorWeight.toFixed(2)}
                        <span className="text-ink-soft"> 语义 / </span>
                        {retrieval.keywordWeight.toFixed(2)}
                        <span className="text-ink-soft"> 关键词</span>
                      </span>
                    </div>
                    <span className="text-[11px] leading-4 text-ink-soft">
                      两路分数会被归一化后加权合并；权重偏语义适合口语化提问，偏关键词适合型号、编号这类精确词
                    </span>
                  </div>
                ) : (
                  <span className="text-[11px] leading-4 text-ink-soft">
                    RRF 只按两路各自的名次融合，不需要调权重：两路分数量纲不同（余弦在 0~1，BM25 无上界）时更稳
                  </span>
                )}
              </div>
            ) : null}

            <div className="grid gap-3 sm:grid-cols-3">
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
                hint="0 表示不启用。只在向量检索下生效：全文检索的 BM25 分数没有上界，混合检索拿到的是融合分，都卡不住余弦阈值"
              >
                <Input
                  type="number"
                  step="0.05"
                  min={0}
                  max={1}
                  disabled={retrieval.searchMode !== "vector"}
                  value={retrieval.scoreThreshold}
                  onChange={(event) =>
                    setRetrieval({ ...retrieval, scoreThreshold: Number(event.target.value) })
                  }
                />
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
              <span className="mb-2 block text-[11px] leading-5 text-ink-soft">
                检索方式与检索参数保存在知识库上：回答这一段的配置，不要每次提问都重配一遍。
              </span>
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

/** 三种检索方式的一句话说明。
 *
 * 描述里刻意写清「靠什么找」：使用者选的是检索策略，
 * 不是「向量 / 全文」这两个术语本身（术语对非算法背景的人没有信息量）。 */
const SEARCH_MODES: { mode: SearchMode; label: string; note: string; engine: string }[] = [
  {
    mode: "vector",
    label: "向量检索",
    note: "按语义相似度找，换个说法也能命中",
    engine: "dense · cosine",
  },
  {
    mode: "fulltext",
    label: "全文检索",
    note: "按词命中找，型号、编号这类精确词更准",
    engine: "sparse · bm25",
  },
  {
    mode: "hybrid",
    label: "混合检索",
    note: "两路并行召回后融合，兼顾语义与精确词",
    engine: "dense + bm25",
  },
];

function SearchModePicker({
  value,
  onChange,
}: {
  value: SearchMode;
  onChange: (mode: SearchMode) => void;
}) {
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="eyebrow !text-ink-soft">检索方式</legend>
      <div className="grid gap-2 sm:grid-cols-3">
        {SEARCH_MODES.map((item) => {
          const active = item.mode === value;
          return (
            <label
              key={item.mode}
              // 选中态用实心边框 + 淡蓝图底：三张卡片同为「同一组选择」，
              // 用边框而不是阴影表达选中，才和这套图纸语言一致。
              className={`flex cursor-pointer flex-col gap-1 rounded border px-3 py-2.5 transition-colors ${
                active
                  ? "border-pri bg-pri-soft"
                  : "border-grid bg-white hover:border-ink-soft"
              }`}
            >
              <span className="flex items-center gap-2">
                <input
                  type="radio"
                  name="search-mode"
                  className="accent-pri"
                  checked={active}
                  onChange={() => onChange(item.mode)}
                />
                <span className="text-sm font-medium text-ink">{item.label}</span>
                <span className="reading ml-auto text-[10px] text-ink-soft">{item.engine}</span>
              </span>
              <span className="text-[11px] leading-4 text-ink-muted">{item.note}</span>
            </label>
          );
        })}
      </div>
    </fieldset>
  );
}

function RankPicker({
  value,
  onChange,
}: {
  value: HybridRank;
  onChange: (rank: HybridRank) => void;
}) {
  const options: { rank: HybridRank; label: string }[] = [
    { rank: "weighted", label: "权重" },
    { rank: "rrf", label: "RRF" },
  ];
  return (
    <div className="flex items-center gap-1.5">
      {options.map((item) => (
        <button
          key={item.rank}
          type="button"
          onClick={() => onChange(item.rank)}
          className={`h-[26px] rounded px-2.5 text-xs transition-colors ${
            item.rank === value
              ? "bg-pri text-white"
              : "border border-grid bg-white text-ink-muted hover:border-ink-soft"
          }`}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}
