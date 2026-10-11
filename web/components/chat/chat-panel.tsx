"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Field, Select } from "@/components/ui/field";
import { chatApi, datasetsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { AppInfo, CitationInfo, DatasetInfo } from "@/types/api";

// 对话调试面板：流式渲染 + 引用来源。
//
// 状态放在组件内而不是全局 store：对话是「一个页面一份」的状态，
// 放进全局反而要额外处理「切应用时清空」；项目规则要求的是跨组件共享状态用 Zustand。
// 消息区是本页唯一滚动区域（web/AGENTS.md 第 4 节）。

interface ChatMessage {
  /** 本地临时消息没有落库 ID，用负数区分，避免与后端 ID 冲突。 */
  id: number;
  role: "user" | "assistant";
  content: string;
  citations?: CitationInfo[];
  streaming?: boolean;
  latencyMs?: number;
  model?: string;
}

export function ChatPanel({ app }: { app: AppInfo }) {
  const [conversationId, setConversationId] = useState(0);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [datasets, setDatasets] = useState<DatasetInfo[]>([]);
  const [datasetId, setDatasetId] = useState(0);
  const [question, setQuestion] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const localIdRef = useRef(-1);
  const abortRef = useRef<AbortController | null>(null);
  const bottomRef = useRef<HTMLDivElement | null>(null);

  const needDataset = app.mode === "rag";

  // RAG 形态必须先选知识库：后端在没有 datasetId 时会直接拒绝（见 service/chat.go）。
  useEffect(() => {
    if (!needDataset) {
      return;
    }
    datasetsApi
      .list(1, 50)
      .then((result) => {
        setDatasets(result.items);
        if (result.items.length > 0) {
          setDatasetId(result.items[0].id);
        }
      })
      .catch(() => setError("加载知识库列表失败"));
  }, [needDataset]);

  // 有新内容就滚到底部：对话页不滚就看不到最新回答。
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages]);

  useEffect(() => () => abortRef.current?.abort(), []);

  const handleSend = useCallback(async () => {
    const text = question.trim();
    if (text === "") {
      return;
    }
    if (needDataset && datasetId === 0) {
      setError("知识库问答需要先选择一个知识库");
      return;
    }

    setError("");
    setQuestion("");
    setSending(true);

    // 用户消息与占位的助手消息一起入列：助手消息随后续增量更新，
    // 避免「先空白、答完突然出现」的观感。
    const assistantId = localIdRef.current--;
    setMessages((prev) => [
      ...prev,
      { id: localIdRef.current--, role: "user", content: text },
      { id: assistantId, role: "assistant", content: "", streaming: true },
    ]);

    const controller = new AbortController();
    abortRef.current = controller;

    try {
      await chatApi.stream(
        { appId: app.id, question: text, conversationId, datasetId: needDataset ? datasetId : 0 },
        {
          onCitations: (payload) =>
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId ? { ...item, citations: payload.citations } : item,
              ),
            ),
          onDelta: (payload) =>
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId
                  ? { ...item, content: item.content + payload.content }
                  : item,
              ),
            ),
          onDone: (payload) => {
            setConversationId(payload.conversationId);
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId
                  ? {
                      ...item,
                      streaming: false,
                      latencyMs: payload.latencyMs,
                      model: payload.model,
                    }
                  : item,
              ),
            );
          },
          onError: (payload) => {
            setError(payload.message);
            setMessages((prev) =>
              prev.map((item) => (item.id === assistantId ? { ...item, streaming: false } : item)),
            );
          },
        },
        controller.signal,
      );
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "发送失败");
      setMessages((prev) =>
        prev.map((item) => (item.id === assistantId ? { ...item, streaming: false } : item)),
      );
    } finally {
      setSending(false);
      abortRef.current = null;
    }
  }, [app.id, conversationId, datasetId, needDataset, question]);

  const handleNewConversation = () => {
    abortRef.current?.abort();
    setConversationId(0);
    setMessages([]);
    setError("");
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 px-8 py-6">
      <header className="flex flex-wrap items-end gap-4 border-b border-grid pb-3">
        <div className="flex flex-col gap-0.5">
          <span className="eyebrow">对话调试</span>
          <h1 className="text-[22px] font-semibold tracking-tight text-ink">{app.name}</h1>
          <span className="text-xs text-ink-muted">
            {needDataset ? "先检索再回答，回答带来源引用" : "直接回答，不检索"}
          </span>
        </div>
        {needDataset ? (
          <div className="w-56">
            <Field label="知识库" hint="检索范围；回答里的 [n] 对应来源列表的第 n 条。">
              <Select
                value={datasetId}
                onChange={(event) => setDatasetId(Number(event.target.value))}
              >
                {datasets.length === 0 ? <option value={0}>（还没有知识库）</option> : null}
                {datasets.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </Select>
            </Field>
          </div>
        ) : null}
        <div className="ml-auto flex items-end gap-3">
          <span className="reading pb-2 text-xs text-ink-soft">
            {conversationId > 0 ? `会话 #${conversationId}` : "新会话"}
          </span>
          <Button onClick={handleNewConversation}>新会话</Button>
        </div>
      </header>

      {error ? (
        <p className="rounded border border-fault/30 bg-fault-soft px-3 py-2 text-sm text-fault">
          {error}
        </p>
      ) : null}

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto rounded border border-grid bg-white px-4 py-4">
        {messages.length === 0 ? (
          // 空状态是「邀请使用」的位置：给出具体可做的事，而不是一句「暂无数据」。
          <div className="m-auto flex max-w-sm flex-col items-center gap-2 text-center">
            <span className="eyebrow">还没有对话</span>
            <p className="text-xs leading-5 text-ink-muted">
              在下面输入问题开始。
              {needDataset
                ? "回答会先检索所选知识库，并列出引用到的文档与页码，可以逐条核对。"
                : "当前应用不检索知识库，直接由对话模型回答。"}
            </p>
          </div>
        ) : null}
        {messages.map((message) => (
          <MessageBubble key={message.id} message={message} />
        ))}
        <div ref={bottomRef} />
      </div>

      <div className="flex items-end gap-3">
        <div className="flex-1">
          <Field label="提问" hint="Enter 发送，Shift+Enter 换行。">
            <textarea
              value={question}
              onChange={(event) => setQuestion(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey) {
                  event.preventDefault();
                  void handleSend();
                }
              }}
              rows={2}
              placeholder="例如：用万用表测直流电压时要注意什么？"
              className="w-full rounded border border-grid bg-white px-3 py-2 text-sm text-ink outline-none transition-colors placeholder:text-ink-soft focus:border-pri"
            />
          </Field>
        </div>
        <div className="flex flex-col gap-1.5">
          <span className="h-4" aria-hidden="true" />
          <Button
            variant="primary"
            busy={sending}
            disabled={question.trim() === ""}
            onClick={() => void handleSend()}
          >
            发送
          </Button>
        </div>
      </div>
    </div>
  );
}

/** MessageBubble 渲染单条消息；助手消息额外展示引用来源与耗时。 */
function MessageBubble({ message }: { message: ChatMessage }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="max-w-[80%] rounded bg-ink px-3.5 py-2.5 text-sm leading-6 text-white">
          {message.content}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="max-w-[90%] self-start rounded border border-grid bg-paper px-3.5 py-2.5 text-sm leading-6 text-ink">
        {message.content === "" && message.streaming ? (
          <span className="text-ink-soft">正在检索并组织回答…</span>
        ) : (
          <span className="whitespace-pre-wrap">{message.content}</span>
        )}
        {message.streaming && message.content !== "" ? (
          <span className="ml-0.5 inline-block h-4 w-1 animate-pulse bg-pri align-middle" />
        ) : null}
      </div>

      {message.citations && message.citations.length > 0 ? (
        <div className="flex max-w-[90%] flex-col gap-1">
          <span className="eyebrow !text-ink-soft">
            引用来源 · {message.citations.length} 条
          </span>
          {/* 来源做成可扫读的行：编号 + 文档名 + 页码 + 两阶段分数（等宽），
              比一排胶囊标签更容易核对「第 3 条到底来自哪儿」。 */}
          <ul className="divide-y divide-grid-soft rounded border border-grid bg-white">
            {message.citations.map((citation) => (
              <li
                key={`${citation.index}-${citation.segmentId}`}
                className="flex items-baseline gap-2 px-2.5 py-1.5"
              >
                <span className="reading w-6 shrink-0 text-[11px] text-ink-soft">
                  [{citation.index}]
                </span>
                <span className="min-w-0 flex-1 truncate text-[11px] text-ink-muted">
                  {citation.documentName}
                  {citation.page > 0 ? ` · 第 ${citation.page} 页` : ""}
                  {citation.titlePath.length > 0 ? ` · ${citation.titlePath.join(" / ")}` : ""}
                </span>
                <span className="reading shrink-0 text-[10px] text-ink-soft">
                  {citation.rerankScore !== undefined
                    ? `向量 ${citation.score.toFixed(3)} · 重排 ${citation.rerankScore.toFixed(3)}`
                    : `向量 ${citation.score.toFixed(3)}`}
                </span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {message.latencyMs !== undefined ? (
        <span className="reading text-[10px] text-ink-soft">
          {message.latencyMs} ms{message.model ? ` · ${message.model}` : ""}
        </span>
      ) : null}
    </div>
  );
}
