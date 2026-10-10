"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { PageContainer } from "@/components/layout/page-container";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/field";
import { datasetsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { DatasetInfo } from "@/types/api";

// 知识库列表页。
//
// 知识库是「文档 → 解析 → 切分 → 向量化」的容器；创建后进入详情页上传文档。

const statusLabels: Record<string, string> = {
  active: "可用",
  indexing: "索引中",
  error: "异常",
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

  return (
    <PageContainer className="flex flex-col gap-5 px-8 py-7">
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold tracking-tight">知识库</h1>
        <p className="text-sm leading-6 text-neutral-600">
          上传文档后，平台会解析、切分并向量化入库；文档处理完成后即可在「知识库问答」应用里被检索到。
        </p>
      </header>

      {error ? (
        <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>
      ) : null}

      <section className="flex flex-wrap items-end gap-3 rounded-xl border border-neutral-200 bg-white px-4 py-3">
        <div className="min-w-48 flex-1">
          <Field label="知识库名" required>
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
            创建知识库
          </Button>
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <div className="flex items-center gap-3">
          <h2 className="text-sm font-medium text-neutral-500">
            全部知识库（{datasets.length}）
          </h2>
          <Button busy={loading} onClick={() => void load()}>
            刷新
          </Button>
        </div>

        {datasets.length === 0 ? (
          <p className="rounded-xl border border-dashed border-neutral-300 px-4 py-8 text-center text-xs text-neutral-400">
            {loading ? "加载中…" : "还没有知识库，先在上面创建一个"}
          </p>
        ) : (
          <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {datasets.map((dataset) => (
              <li
                key={dataset.id}
                className="flex flex-col gap-2 rounded-xl border border-neutral-200 bg-white px-4 py-3"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-sm font-semibold text-neutral-900">
                    {dataset.name}
                  </span>
                  <span className="shrink-0 rounded bg-neutral-100 px-1.5 py-0.5 text-[11px] text-neutral-600">
                    {statusLabels[dataset.status] ?? dataset.status}
                  </span>
                </div>
                <p className="min-h-10 text-xs leading-5 text-neutral-500">
                  {dataset.description || "（无描述）"}
                </p>
                <div className="flex items-center gap-2">
                  <Link
                    href={`/datasets/${dataset.id}`}
                    className="rounded-md bg-neutral-900 px-3 py-1.5 text-xs text-white transition-colors hover:bg-neutral-700"
                  >
                    管理文档
                  </Link>
                  <Button
                    variant="danger"
                    onClick={() => {
                      if (window.confirm(`确定删除知识库「${dataset.name}」吗？`)) {
                        void datasetsApi.remove(dataset.id).then(load);
                      }
                    }}
                  >
                    删除
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </PageContainer>
  );
}
