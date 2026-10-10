"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import { DocumentPanel } from "@/components/dataset/document-panel";
import { PageContainer } from "@/components/layout/page-container";
import { datasetsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { DatasetInfo } from "@/types/api";

// 知识库详情页：看基本信息 + 管理文档。
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
      <nav className="text-xs text-neutral-500">
        <Link href="/datasets" className="hover:text-neutral-900">
          知识库
        </Link>
        <span className="mx-1.5">/</span>
        <span className="text-neutral-700">{dataset?.name ?? `#${datasetId}`}</span>
      </nav>

      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold tracking-tight">
          {dataset?.name ?? "加载中…"}
        </h1>
        <p className="text-sm leading-6 text-neutral-600">
          {dataset?.description || "（无描述）"}
        </p>
      </header>

      {error ? (
        <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>
      ) : null}

      {/* 文档面板自己管加载与轮询：它同时承担「上传 + 看状态 + 看分段」，拆开反而要来回传状态。 */}
      <DocumentPanel datasetId={datasetId} />
    </PageContainer>
  );
}
