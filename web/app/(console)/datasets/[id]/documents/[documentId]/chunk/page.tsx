"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import { ChunkConfigPanel } from "@/components/chunk/config-panel";
import { ChunkPreviewPanel } from "@/components/chunk/preview-panel";
import { Button } from "@/components/ui/button";
import { chunkApi } from "@/lib/api/console";
import { useChunkWorkbenchStore } from "@/stores/chunk-workbench-store";
import type { RetrievalConfig } from "@/types/api";

// 分段工作台：左侧配置 / 右侧预览。
//
// 布局：整页不滚动，左右两栏**各自滚动**（配置很长、预览更长），
// 底部操作条固定在页面上，随时可以「保存并处理」。
export default function ChunkWorkbenchPage() {
  const params = useParams<{ id: string; documentId: string }>();
  const datasetId = Number(params.id);
  const documentId = Number(params.documentId);

  const load = useChunkWorkbenchStore((state) => state.load);
  const config = useChunkWorkbenchStore((state) => state.config);
  const preview = useChunkWorkbenchStore((state) => state.preview);
  const error = useChunkWorkbenchStore((state) => state.error);
  const notice = useChunkWorkbenchStore((state) => state.notice);
  const ingesting = useChunkWorkbenchStore((state) => state.ingesting);
  const saveAsDefault = useChunkWorkbenchStore((state) => state.saveAsDefault);
  const setSaveAsDefault = useChunkWorkbenchStore((state) => state.setSaveAsDefault);
  const ingest = useChunkWorkbenchStore((state) => state.ingest);
  const clearMessages = useChunkWorkbenchStore((state) => state.clearMessages);

  // 检索参数只读展示（真正的修改入口在知识库设置页）。
  const [retrieval, setRetrieval] = useState<RetrievalConfig | null>(null);

  useEffect(() => {
    if (Number.isFinite(datasetId) && Number.isFinite(documentId)) {
      void load(datasetId, documentId);
    }
  }, [datasetId, documentId, load]);

  useEffect(() => {
    chunkApi.getRetrievalConfig(datasetId).then(setRetrieval).catch(() => setRetrieval(null));
  }, [datasetId]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-grid bg-white px-8 py-3">
        <nav className="reading text-xs text-ink-soft">
          <Link href="/datasets" className="hover:text-pri">知识库</Link>
          <span className="mx-1.5">/</span>
          <Link href={`/datasets/${datasetId}`} className="hover:text-pri">文档</Link>
          <span className="mx-1.5">/</span>
          <span className="text-ink-muted">分段与清洗</span>
        </nav>
        <h1 className="text-base font-semibold tracking-tight text-ink">分段与清洗</h1>
        <span className="text-xs text-ink-soft">左侧调参数，右侧实时看结果</span>
      </header>

      {error || notice ? (
        <div
          className={`flex items-start justify-between gap-3 px-8 py-2 text-sm ${
            error ? "bg-fault-soft text-fault" : "bg-ok-soft text-ok"
          }`}
        >
          <span className="leading-6">{error || notice}</span>
          <button type="button" className="text-xs text-ink-soft" onClick={clearMessages}>
            关闭
          </button>
        </div>
      ) : null}

      <div className="flex min-h-0 flex-1">
        <aside className="w-[380px] shrink-0 overflow-y-auto border-r border-grid bg-white px-4 py-4">
          <ChunkConfigPanel retrieval={retrieval} />
        </aside>
        <main className="min-w-0 flex-1 overflow-y-auto px-6 py-4">
          <ChunkPreviewPanel />
        </main>
      </div>

      <footer className="flex flex-wrap items-center gap-3 border-t border-grid bg-white px-8 py-3">
        <span className="reading text-xs text-ink-muted">
          共 {preview?.total ?? 0} 段 · 按当前配置入库
        </span>
        <label className="flex items-center gap-2 text-xs text-ink-muted">
          <input
            type="checkbox"
            checked={saveAsDefault}
            onChange={(event) => setSaveAsDefault(event.target.checked)}
          />
          存为知识库默认（下次上传自动带出这套参数）
        </label>
        <div className="ml-auto flex items-center gap-2">
          <Link
            href={`/datasets/${datasetId}`}
            className="rounded border border-grid px-3 py-1.5 text-sm text-ink hover:border-ink-soft hover:bg-paper"
          >
            返回文档列表
          </Link>
          <Button
            variant="primary"
            busy={ingesting}
            disabled={!preview || preview.total === 0}
            onClick={() => void ingest()}
          >
            保存并处理
          </Button>
        </div>
        <span className="reading w-full text-[11px] text-ink-soft">
          配置指纹 {config.version ?? "—"} · 入库后写进每个片段，便于分辨「这批片段是哪套参数切的」
        </span>
      </footer>
    </div>
  );
}
