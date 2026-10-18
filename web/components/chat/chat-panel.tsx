"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { ThinkingBlock } from "@/components/chat/thinking-block";
import { ToolCallsBlock } from "@/components/chat/tool-calls-block";
import { TypewriterText } from "@/components/chat/typewriter-text";
import { Button } from "@/components/ui/button";
import { Field, Select } from "@/components/ui/field";
import { chatApi, datasetsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type {
  AppInfo,
  CitationInfo,
  DatasetInfo,
  MessageInfo,
  MessageToolCallInfo,
} from "@/types/api";

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
  /** 思考过程（推理模型才有，不进正文、不落库）。 */
  reasoning?: string;
  citations?: CitationInfo[];
  /** 智能体形态下的工具调用记录（实时事件与历史消息共用同一结构）。 */
  toolCalls?: MessageToolCallInfo[];
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
  // 工具调用默认展示；关闭只影响呈现，后端仍会执行并落库。
  const [showToolCalls, setShowToolCalls] = useState(true);
  const [historyLoaded, setHistoryLoaded] = useState(false);
  const [error, setError] = useState("");
  const localIdRef = useRef(-1);
  const abortRef = useRef<AbortController | null>(null);
  // 用户已经开始操作（发送 / 新建会话）后，迟到的历史加载不能覆盖当前界面。
  const historyBlockedRef = useRef(false);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  // 是否「钉在底部」：用户往上翻看历史时不能强行把他拉回来，
  // 但只要他在底部附近，新内容就必须跟得住（流式 + 打字机都在持续长高）。
  const stickRef = useRef(true);

  const needDataset = app.mode === "rag";
  // 智能体的知识库是可选的：选了才注册检索工具（与后端 D2 / D9 一致）。
  const supportsDataset = app.mode === "rag" || app.mode === "agent";

  // RAG 形态必须先选知识库；智能体可选用（选了才提供检索工具）。
  useEffect(() => {
    if (!supportsDataset) {
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
  }, [supportsDataset]);

  // 打开页面时恢复最近一次会话：否则刷新后既看不到历史回答，也看不到工具调用记录。
  // 失败不阻塞新对话——历史只是加分项，输入框仍然可用。
  useEffect(() => {
    let cancelled = false;
    setHistoryLoaded(false);
    chatApi
      .listConversations(app.id)
      .then(async (result) => {
        const latest = result.items[0];
        if (!latest || cancelled) {
          return;
        }
        const history = await chatApi.listMessages(latest.id);
        if (cancelled || historyBlockedRef.current) {
          return;
        }
        setConversationId(latest.id);
        setMessages(history.items.filter(isChatRole).map(toChatMessage));
      })
      .catch(() => {
        if (!cancelled) {
          setError("加载历史会话失败，可直接开始新对话");
        }
      })
      .finally(() => {
        if (!cancelled) {
          setHistoryLoaded(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [app.id]);

  const scrollToBottom = useCallback(() => {
    const element = scrollRef.current;
    if (element) {
      element.scrollTop = element.scrollHeight;
    }
  }, []);

  // 打字机每揭示一帧都会回调一次：只有「钉在底部」时才跟随，
  // 否则用户正在往上翻，页面会被一行行拽回去。
  const handleReveal = useCallback(() => {
    if (stickRef.current) {
      scrollToBottom();
    }
  }, [scrollToBottom]);

  const handleScroll = useCallback(() => {
    const element = scrollRef.current;
    if (!element) {
      return;
    }
    // 距底部 32px 以内都算「在底部」：轻微滚动不应该打断跟随。
    stickRef.current = element.scrollHeight - element.scrollTop - element.clientHeight < 32;
  }, []);

  // 新消息（用户提问）一定要滚到底部：这是用户自己的动作，不该被「没钉住」挡住。
  useEffect(() => {
    stickRef.current = true;
    scrollToBottom();
  }, [messages.length, scrollToBottom]);

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
    historyBlockedRef.current = true;

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
        {
          appId: app.id,
          question: text,
          conversationId,
          datasetId: supportsDataset ? datasetId : 0,
        },
        {
          onCitations: (payload) =>
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId ? { ...item, citations: payload.citations } : item,
              ),
            ),
          onReasoning: (payload) =>
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId
                  ? { ...item, reasoning: (item.reasoning ?? "") + payload.content }
                  : item,
              ),
            ),
          onToolCall: (payload) =>
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId
                  ? { ...item, toolCalls: mergeToolCall(item.toolCalls, payload) }
                  : item,
              ),
            ),
          onToolResult: (payload) =>
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId
                  ? { ...item, toolCalls: mergeToolCall(item.toolCalls, payload) }
                  : item,
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
  }, [app.id, conversationId, datasetId, needDataset, supportsDataset, question]);

  const handleNewConversation = () => {
    abortRef.current?.abort();
    historyBlockedRef.current = true;
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
            {app.mode === "rag"
              ? "先检索再回答，回答带来源引用"
              : app.mode === "agent"
                ? "按需调用检索 / 计算工具，调用过程可见"
                : "直接回答，不检索"}
          </span>
        </div>
        {supportsDataset ? (
          <div className="w-56">
            <Field
              label={needDataset ? "知识库" : "知识库（可选）"}
              hint={
                needDataset
                  ? "检索范围；回答里的 [n] 对应来源列表的第 n 条。"
                  : "选中后智能体可调用检索工具；不选则不提供检索工具。"
              }
            >
              <Select
                value={datasetId}
                onChange={(event) => setDatasetId(Number(event.target.value))}
              >
                {app.mode === "agent" ? <option value={0}>不使用知识库</option> : null}
                {datasets.length === 0 && app.mode !== "agent" ? (
                  <option value={0}>（还没有知识库）</option>
                ) : null}
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
          {app.mode === "agent" ? (
            <label className="flex h-[34px] items-center gap-1.5 text-xs text-ink-muted">
              <input
                type="checkbox"
                checked={showToolCalls}
                onChange={(event) => setShowToolCalls(event.target.checked)}
                className="h-3.5 w-3.5 accent-pri"
              />
              显示工具调用
            </label>
          ) : null}
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

      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto rounded border border-grid bg-white px-4 py-4"
      >
        {messages.length === 0 ? (
          // 空状态是「邀请使用」的位置：给出具体可做的事，而不是一句「暂无数据」。
          <div className="m-auto flex max-w-sm flex-col items-center gap-2 text-center">
            <span className="eyebrow">{historyLoaded ? "还没有对话" : "正在加载历史"}</span>
            {historyLoaded ? (
              <p className="text-xs leading-5 text-ink-muted">
                在下面输入问题开始。
                {needDataset
                  ? "回答会先检索所选知识库，并列出引用到的文档与页码，可以逐条核对。"
                  : app.mode === "agent"
                    ? "智能体会按需调用检索或计算工具，调用过程显示在回答上方。"
                    : "当前应用不检索知识库，直接由对话模型回答。"}
              </p>
            ) : null}
          </div>
        ) : null}
        {messages.map((message) => (
          <MessageBubble
            key={message.id}
            message={message}
            showToolCalls={showToolCalls}
            onReveal={handleReveal}
          />
        ))}
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

/** MessageBubble 渲染单条消息；助手消息额外展示思考过程、引用来源与耗时。 */
function MessageBubble({
  message,
  showToolCalls,
  onReveal,
}: {
  message: ChatMessage;
  showToolCalls: boolean;
  onReveal: () => void;
}) {
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
      {/* 思考过程放在回答上方：它是「先发生的事」，位置本身就表达了顺序。 */}
      <ThinkingBlock
        text={message.reasoning ?? ""}
        active={message.streaming === true && message.content === ""}
      />
      {showToolCalls && message.toolCalls && message.toolCalls.length > 0 ? (
        <ToolCallsBlock calls={message.toolCalls} />
      ) : null}
      <div className="max-w-[90%] self-start rounded border border-grid bg-paper px-3.5 py-2.5 text-sm leading-6 text-ink">
        {message.content === "" && message.streaming ? (
          <span className="text-ink-soft">正在检索并组织回答…</span>
        ) : (
          // 只有本次会话新产生的回答逐字揭示（本地消息的 id 为负）：
          // 历史消息重挂载时直接显示全文，不然每次进页面都要把旧回答重打一遍。
          <TypewriterText
            text={message.content}
            streamFinished={message.streaming !== true}
            onReveal={onReveal}
            animate={message.id < 0}
          />
        )}
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

/** 历史消息只还原用户与助手两类；system / tool 由链路内部维护。 */
function isChatRole(
  message: MessageInfo,
): message is MessageInfo & { role: "user" | "assistant" } {
  return message.role === "user" || message.role === "assistant";
}

function toChatMessage(message: MessageInfo & { role: "user" | "assistant" }): ChatMessage {
  return {
    id: message.id,
    role: message.role,
    content: message.content,
    citations: message.citations,
    toolCalls: message.toolCalls,
    latencyMs: message.latencyMs,
  };
}

/**
 * 合并同一次工具调用的开始与结果事件。
 *
 * 后端按轮次发两个事件（tool_call / tool_result），前端按 round 合并；
 * 结果事件可能不带入参（或反之），合并时保留已有值，避免后到的空字段把先前信息抹掉。
 */
function mergeToolCall(
  existing: MessageToolCallInfo[] | undefined,
  incoming: MessageToolCallInfo,
): MessageToolCallInfo[] {
  const next = [...(existing ?? [])];
  const index = next.findIndex((item) => item.round === incoming.round);
  if (index === -1) {
    next.push(incoming);
  } else {
    next[index] = {
      ...next[index],
      ...incoming,
      arguments: incoming.arguments || next[index].arguments,
      result: incoming.result || next[index].result,
    };
  }
  return next.sort((a, b) => a.round - b.round);
}
