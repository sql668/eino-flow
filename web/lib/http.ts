import { API_BASE_URL } from "@/lib/env";
import { ApiCode, type ApiResponse } from "@/types/api";

/** 统一的接口错误：HTTP 异常或业务码非 0 时抛出。 */
export class ApiError extends Error {
  readonly code: number;
  readonly status?: number;

  constructor(message: string, code: number, status?: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

type QueryValue = string | number | boolean | undefined;

export type RequestOptions = Omit<RequestInit, "body"> & {
  /** 请求体，会自动 JSON 序列化 */
  body?: unknown;
  /** 查询参数，值为 undefined 的项会被忽略 */
  query?: Record<string, QueryValue>;
};

/**
 * 拼接请求地址。
 * API_BASE_URL 为空时返回同源相对路径，交给浏览器按当前站点解析。
 */
function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  const search = new URLSearchParams();
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined) {
        search.set(key, String(value));
      }
    }
  }

  const queryString = search.toString();
  const suffix = queryString ? `${path}?${queryString}` : path;
  if (!API_BASE_URL) {
    return suffix;
  }
  return new URL(suffix, API_BASE_URL).toString();
}

/**
 * 统一请求封装：所有后端调用都必须经过这里，禁止在组件里裸写 fetch。
 * 负责：拼接地址、序列化请求体、解析统一响应结构、把异常转成 ApiError。
 */
export async function request<T = unknown>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const { body, query, headers, ...rest } = options;

  const response = await fetch(buildUrl(path, query), {
    ...rest,
    headers: {
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (!response.ok) {
    throw new ApiError(
      `请求失败: HTTP ${response.status}`,
      ApiCode.Internal,
      response.status,
    );
  }

  const payload = (await response.json()) as ApiResponse<T>;
  if (payload.code !== ApiCode.OK) {
    throw new ApiError(
      payload.message || "接口返回异常",
      payload.code,
      response.status,
    );
  }
  return payload.data as T;
}

/**
 * 上传文件（multipart/form-data）。
 *
 * 单独一个函数而不是塞进 request()：Content-Type 必须**留空**让浏览器自己带上
 * boundary，而 request() 只要 body 有值就会设成 application/json——硬塞进去必然上传失败。
 */
export async function uploadFile<T = unknown>(
  path: string,
  file: File,
  field = "file",
): Promise<T> {
  const form = new FormData();
  form.append(field, file);

  const response = await fetch(buildUrl(path), { method: "POST", body: form });
  if (!response.ok) {
    throw new ApiError(`上传失败: HTTP ${response.status}`, ApiCode.Internal, response.status);
  }
  const payload = (await response.json()) as ApiResponse<T>;
  if (payload.code !== ApiCode.OK) {
    throw new ApiError(payload.message || "上传失败", payload.code, response.status);
  }
  return payload.data as T;
}

export const http = {
  get: <T = unknown>(path: string, options?: RequestOptions) =>
    request<T>(path, { ...options, method: "GET" }),
  post: <T = unknown>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>(path, { ...options, method: "POST", body }),
  put: <T = unknown>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>(path, { ...options, method: "PUT", body }),
  delete: <T = unknown>(path: string, options?: RequestOptions) =>
    request<T>(path, { ...options, method: "DELETE" }),
};

/** 解析后的单帧 SSE 事件。 */
export interface SseEvent<T = unknown> {
  event: string;
  data: T;
}

export interface StreamSseOptions {
  body?: unknown;
  signal?: AbortSignal;
  onEvent: (event: SseEvent) => void;
}

/**
 * 以 POST 方式发起 SSE 流式请求（用于对话调试的逐字输出）。
 *
 * 说明：浏览器原生 EventSource 只支持 GET、无法携带请求体，
 * 因此统一用 fetch + ReadableStream 解析 text/event-stream。
 */
export async function streamSse(
  path: string,
  options: StreamSseOptions,
): Promise<void> {
  const response = await fetch(buildUrl(path), {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  });

  if (!response.ok || !response.body) {
    throw new ApiError(
      `流式请求失败: HTTP ${response.status}`,
      ApiCode.Internal,
      response.status,
    );
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  // 按 SSE 规范以空行分隔事件块；跨 chunk 的半个事件留在 buffer 里等下一次拼接。
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, "\n");

    let boundary = buffer.indexOf("\n\n");
    while (boundary !== -1) {
      const chunk = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      const event = parseSseChunk(chunk);
      if (event) {
        options.onEvent(event);
      }
      boundary = buffer.indexOf("\n\n");
    }
  }
}

/** 解析单个 SSE 事件块；无 data 字段时返回 null。 */
function parseSseChunk(chunk: string): SseEvent | null {
  let event = "message";
  const dataLines: string[] = [];

  for (const line of chunk.split("\n")) {
    if (line.startsWith("event:")) {
      event = line.slice("event:".length).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trim());
    }
  }

  if (dataLines.length === 0) {
    return null;
  }

  const raw = dataLines.join("\n");
  try {
    return { event, data: JSON.parse(raw) };
  } catch {
    // 后端可能推送非 JSON 的纯文本片段，保持原样透传。
    return { event, data: raw };
  }
}
