"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import { DocumentTable } from "@/components/dataset/document-table";
import { PageContainer } from "@/components/layout/page-container";
import { datasetsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { DatasetInfo } from "@/types/api";

// 知识库详情 = 文档列表页。
export default function DatasetDetailPage() {
  const params = useParams<{ id: string }>();
  const datasetId = Number(params.id);

  const [dataset, setDataset] = useState<DatasetInfo | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!Number.isFinite(datasetId) || datasetId <= 0) {
      setError("知识库 ID 不合法");
      return;
    }
    datasetsApi
      .get(datasetId)
      .then(setDataset)
      .catch((err) => setError(err instanceof ApiError ? err.message : "加载知识库失败"));
  }, [datasetId]);

  return (
    <PageContainer className="flex flex-col gap-5 px-8 py-7">
      <nav className="reading text-xs text-ink-soft">
        <Link href="/datasets" className="hover:text-pri">知识库</Link>
        <span className="mx-1.5">/</span>
        <span className="text-ink-muted">{dataset?.name ?? `#${datasetId}`}</span>
      </nav>

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <span className="eyebrow">知识库</span>
          <h1 className="text-[22px] font-semibold tracking-tight text-ink">
            {dataset?.name ?? "加载中…"}
          </h1>
          <p className="text-sm leading-6 text-ink-muted">{dataset?.description || "（无描述）"}</p>
        </div>
        <Link
          href={`/datasets/${datasetId}/settings`}
          className="rounded border border-grid bg-white px-3 py-1.5 text-sm text-ink transition-colors hover:border-ink-soft hover:bg-paper"
        >
          知识库设置
        </Link>
      </header>

      {error ? (
        <p className="rounded border border-fault/30 bg-fault-soft px-3 py-2 text-sm text-fault">
          {error}
        </p>
      ) : null}

      <DocumentTable datasetId={datasetId} />
    </PageContainer>
  );
}
