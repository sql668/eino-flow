"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { PageContainer } from "@/components/layout/page-container";
import { Button } from "@/components/ui/button";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { Field, Input, Panel } from "@/components/ui/field";
import { datasetsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { DatasetInfo } from "@/types/api";

// 知识库列表。
//
// 布局思路：把「知识库」当成一组待检对象列出来——每行一条，行内右侧是它的读数
// （状态 / 向量模型 / 创建时间），而不是做成卡片墙。卡片墙适合浏览图片，
// 这类「以参数与状态为主」的资源列表用行式布局扫读更快。

const statusMeta: Record<string, { label: string; className: string }> = {
  active: { label: "可用", className: "bg-ok-soft text-ok" },
  indexing: { label: "索引中", className: "bg-warn-soft text-warn" },
  error: { label: "异常", className: "bg-fault-soft text-fault" },
};

export default function DatasetsPage() {
  const [datasets, setDatasets] = useState<DatasetInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ name: "", description: "" });

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await datasetsApi.list();
      setDatasets(result.items);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "加载知识库失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleCreate = async () => {
    if (form.name.trim() === "") {
      setError("知识库名不能为空");
      return;
    }
    setCreating(true);
    setError("");
    try {
      await datasetsApi.create({ name: form.name.trim(), description: form.description.trim() });
      setForm({ name: "", description: "" });
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "创建知识库失败");
    } finally {
      setCreating(false);
    }
  };

  const { confirm, dialog } = useConfirm();

  return (
    <PageContainer className="flex flex-col gap-5 px-8 py-7">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <span className="eyebrow">知识库</span>
          <h1 className="text-[22px] font-semibold tracking-tight text-ink">文档与检索</h1>
          <p className="max-w-2xl text-sm leading-6 text-ink-muted">
            上传文档后先解析，再由你确认分段与清洗才入库；入库后即可在「知识库问答」应用里被检索到。
          </p>
        </div>
        <span className="reading text-xs text-ink-soft">
          {datasets.length} 个知识库{loading ? " · 刷新中" : ""}
        </span>
      </header>

      {error ? (
        <p className="rounded border border-fault/30 bg-fault-soft px-3 py-2 text-sm text-fault">
          {error}
        </p>
      ) : null}

      <Panel eyebrow="新建" title="创建一个知识库">
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-48 flex-1">
            <Field label="名称" required>
              <Input
                value={form.name}
                onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="产品手册库"
              />
            </Field>
          </div>
          <div className="min-w-48 flex-[2]">
            <Field label="描述" hint="给未来的自己留一句说明：这批文档是干什么用的。">
              <Input
                value={form.description}
                onChange={(event) =>
                  setForm((prev) => ({ ...prev, description: event.target.value }))
                }
                placeholder="可选"
              />
            </Field>
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="h-4" aria-hidden="true" />
            <Button variant="primary" busy={creating} onClick={handleCreate}>
              创建
            </Button>
          </div>
        </div>
      </Panel>

      <Panel
        eyebrow="全部"
        title="知识库清单"
        reading={`${datasets.length} 项`}
        actions={
          <Button busy={loading} onClick={() => void load()}>
            刷新
          </Button>
        }
      >
        {datasets.length === 0 ? (
          <p className="py-8 text-center text-xs text-ink-soft">
            {loading ? "加载中…" : "还没有知识库，先在上面创建一个"}
          </p>
        ) : (
          <ul className="divide-y divide-grid-soft">
            {datasets.map((dataset, index) => {
              const meta = statusMeta[dataset.status] ?? {
                label: dataset.status,
                className: "bg-grid-soft text-ink-muted",
              };
              return (
                <li key={dataset.id} className="flex flex-wrap items-center gap-3 py-3">
                  <span className="reading w-8 text-xs text-ink-soft">
                    {String(index + 1).padStart(2, "0")}
                  </span>
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <div className="flex items-center gap-2">
                      <Link
                        href={`/datasets/${dataset.id}`}
                        className="truncate text-sm font-medium text-ink hover:text-pri"
                      >
                        {dataset.name}
                      </Link>
                      <span className={`rounded-sm px-1.5 py-0.5 text-[11px] ${meta.className}`}>
                        {meta.label}
                      </span>
                    </div>
                    <span className="truncate text-xs text-ink-muted">
                      {dataset.description || "（无描述）"}
                    </span>
                  </div>
                  <span className="reading hidden text-xs text-ink-soft sm:block">
                    {dataset.embeddingModel || "跟随全局模型"}
                  </span>
                  <span className="reading hidden text-xs text-ink-soft md:block">
                    {dataset.createdAt.slice(0, 10)}
                  </span>
                  <div className="flex items-center gap-2">
                    <Link
                      href={`/datasets/${dataset.id}`}
                      className="rounded border border-grid px-3 py-1.5 text-xs text-ink transition-colors hover:border-ink-soft hover:bg-paper"
                    >
                      管理文档
                    </Link>
                    <Button
                      variant="danger"
                      onClick={async () => {
                        const ok = await confirm({
                          title: `删除知识库「${dataset.name}」？`,
                          description:
                            "库内的文档、分段与向量都会被清理，检索将不再命中这些内容；原文件保留在对象存储中但不可再恢复。",
                          confirmLabel: "删除",
                          tone: "danger",
                        });
                        if (ok) {
                          void datasetsApi.remove(dataset.id).then(load);
                        }
                      }}
                    >
                      删除
                    </Button>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </Panel>
      {dialog}
    </PageContainer>
  );
}
