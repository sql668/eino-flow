"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";

import { KeywordEditor } from "@/components/dataset/keyword-editor";
import { PageContainer } from "@/components/layout/page-container";
import { Button } from "@/components/ui/button";
import { Panel } from "@/components/ui/field";
import { Reading, ReadingRow } from "@/components/ui/reading";
import { documentsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { DocumentInfo, SegmentInfo } from "@/types/api";

// 分段浏览：看每个分段到底被切成了什么，并就地修改关键词。
//
// 为什么要单独一页：工作台的预览是**入库前**的（只有前 50 条、没有关键词），
// 而入库后最常见的两个问题是「这一段为什么被切开」和「关键词抓得不对」，
// 两者都要看真实落库的数据。关键词在这里改，改完即时同步到检索索引。

const PAGE_SIZE = 20;

export default function DocumentSegmentsPage() {
  const params = useParams<{ id: string; documentId: string }>();
  const datasetId = Number(params.id);
  const documentId = Number(params.documentId);

  const [document, setDocument] = useState<DocumentInfo | null>(null);
  const [segments, setSegments] = useState<SegmentInfo[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [savingId, setSavingId] = useState(0);
  const [expanded, setExpanded] = useState<number[]>([]);

  const load = useCallback(
    async (targetPage: number) => {
      setLoading(true);
      try {
        const result = await documentsApi.segments(documentId, targetPage, PAGE_SIZE);
        setSegments(result.items);
        setTotal(result.total);
        setPage(result.page);
        setError("");
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "加载分段失败");
      } finally {
        setLoading(false);
      }
    },
    [documentId],
  );

  useEffect(() => {
    if (!Number.isFinite(documentId) || documentId <= 0) {
      setError("文档 ID 不合法");
      return;
    }
    void load(1);
    documentsApi
      .get(documentId)
      .then(setDocument)
      .catch(() => setDocument(null));
  }, [documentId, load]);

  const handleSaveKeywords = async (segment: SegmentInfo, keywords: string[]) => {
    setSavingId(segment.id);
    setError("");
    setNotice("");
    try {
      const saved = await documentsApi.updateSegmentKeywords(documentId, segment.id, keywords);
      // 就地替换这一行：整页重拉会让用户滚到自己已经找不到的位置。
      setSegments((items) => items.map((item) => (item.id === saved.id ? saved : item)));
      setNotice(`第 ${saved.chunkIndex + 1} 段的关键词已保存`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "保存关键词失败");
    } finally {
      setSavingId(0);
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const totalRunes = segments.reduce((sum, item) => sum + item.charCount, 0);
  const withKeywords = segments.filter((item) => item.keywords.length > 0).length;

  return (
    <PageContainer className="flex flex-col gap-5 px-8 py-7">
      <nav className="reading text-xs text-ink-soft">
        <Link href="/datasets" className="hover:text-pri">知识库</Link>
        <span className="mx-1.5">/</span>
        <Link href={`/datasets/${datasetId}`} className="hover:text-pri">
          {document?.name ?? `文档 #${documentId}`}
        </Link>
        <span className="mx-1.5">/</span>
        <span className="text-ink-muted">分段</span>
      </nav>

      <header className="flex flex-col gap-1">
        <span className="eyebrow">分段浏览</span>
        <h1 className="text-[22px] font-semibold tracking-tight text-ink">
          {document?.name ?? `文档 #${documentId}`}
        </h1>
        <p className="text-sm leading-6 text-ink-muted">
          这里显示的是已入库的分段。关键词可以手工修改，改完立即同步到全文检索的索引。
        </p>
      </header>

      {error ? (
        <p className="rounded border border-fault/30 bg-fault-soft px-3 py-2 text-sm text-fault">{error}</p>
      ) : null}
      {notice ? (
        <p className="rounded border border-ok/30 bg-ok-soft px-3 py-2 text-sm text-ok">{notice}</p>
      ) : null}

      <Panel
        eyebrow="读数"
        reading={`第 ${page} / ${totalPages} 页`}
        actions={
          <div className="flex items-center gap-1.5">
            <Button disabled={page <= 1 || loading} onClick={() => void load(page - 1)}>
              上一页
            </Button>
            <Button disabled={page >= totalPages || loading} onClick={() => void load(page + 1)}>
              下一页
            </Button>
          </div>
        }
      >
        <ReadingRow columns={4}>
          <Reading label="分段总数" value={String(total)} unit="段" size="sm" />
          <Reading label="本页字数" value={totalRunes.toLocaleString("zh-CN")} unit="字" size="sm" />
          <Reading
            label="本页有关键词"
            value={`${withKeywords}/${segments.length}`}
            size="sm"
            tone={withKeywords === 0 ? "warn" : "default"}
            note={withKeywords === 0 ? "关键词不参与检索" : undefined}
          />
          <Reading
            label="分段模式"
            value={document?.chunkMode ? modeLabel(document.chunkMode) : "—"}
            size="sm"
          />
        </ReadingRow>
      </Panel>

      {segments.length === 0 ? (
        <p className="rounded border border-dashed border-grid px-4 py-10 text-center text-xs text-ink-soft">
          {loading ? "加载中…" : "这篇文档还没有分段：先在文档列表里完成分段与入库"}
        </p>
      ) : (
        <div className="flex flex-col gap-2">
          {segments.map((segment) => {
            const open = expanded.includes(segment.id);
            return (
              <article
                key={segment.id}
                className="grid grid-cols-[64px_1fr_300px] gap-x-4 rounded border border-grid bg-white px-4 py-3"
              >
                {/* 左栏：序号 + 页码，固定宽度让整列对齐（这是「图纸」的标注栏） */}
                <div className="flex flex-col gap-1">
                  <span className="reading text-sm font-semibold text-ink">
                    {String(segment.chunkIndex + 1).padStart(3, "0")}
                  </span>
                  <span className="reading text-[10px] text-ink-soft">
                    {segment.page > 0 ? `p${segment.page}` : "—"}
                  </span>
                  <span className="reading text-[10px] text-ink-soft">{segment.charCount} 字</span>
                </div>

                {/* 中栏：章节路径 + 正文 */}
                <div className="flex min-w-0 flex-col gap-1">
                  <span className="truncate text-[11px] text-ink-muted">
                    {segment.titlePath.length > 0 ? segment.titlePath.join(" › ") : "（无章节）"}
                  </span>
                  <p
                    className={`whitespace-pre-wrap text-[13px] leading-6 text-ink ${
                      open ? "" : "line-clamp-2"
                    }`}
                  >
                    {segment.content}
                  </p>
                  <button
                    type="button"
                    className="self-start text-[11px] text-pri hover:text-pri-ink"
                    onClick={() =>
                      setExpanded((ids) =>
                        open ? ids.filter((id) => id !== segment.id) : [...ids, segment.id],
                      )
                    }
                  >
                    {open ? "收起" : "展开全文"}
                  </button>
                </div>

                {/* 右栏：关键词（唯一可编辑的字段） */}
                <div className="border-l border-grid-soft pl-4">
                  <KeywordEditor
                    keywords={segment.keywords}
                    busy={savingId === segment.id}
                    onSave={(keywords) => handleSaveKeywords(segment, keywords)}
                  />
                </div>
              </article>
            );
          })}
        </div>
      )}
    </PageContainer>
  );
}

function modeLabel(mode: string): string {
  const labels: Record<string, string> = {
    title: "按标题",
    length: "按长度",
    separator: "按分隔符",
    parent_child: "父子分段",
  };
  return labels[mode] ?? mode;
}
