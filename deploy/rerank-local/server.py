#!/usr/bin/env python3
"""本机重排服务，实现与 TEI 兼容的 HTTP 协议。

为什么需要它：TEI 的 CPU 镜像**只发布 linux/amd64**。在 Apple Silicon 上要经过指令模拟，
而容器里拿不到 AVX2，只能走标量内核——实测 2 条候选耗时 216 秒，无法用于演示。
本服务直接跑在宿主上，用 PyTorch 的 GPU 后端（Apple MPS / NVIDIA CUDA）或 CPU，
绕开这两个问题。

跨平台（开源后别人也要能跑起来）：
  * `--device auto`（默认）按顺序探测 mps → cuda → cpu，同一份代码在 Mac / Linux / Windows 上都能用；
  * 有 GPU 就用 GPU，没有就退到 CPU，并在日志里说明用的是哪个——
    静默退到 CPU 会让人以为「开了 GPU 还是这么慢」。

**协议故意与 TEI 保持一致**（GET /info、POST /rerank），这样：
  * 后端不需要改任何代码（`tei` 厂商指向哪个地址就调哪个）；
  * x86_64 服务器上仍然用官方 TEI 镜像（原生执行、更快），Mac 上用本服务；
  * 同一份配置（base_url）在两种环境下都成立。

只依赖 transformers + torch，HTTP 用标准库，避免为了一个边车再引一套框架。
"""

from __future__ import annotations

import argparse
import json
import math
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import torch
from transformers import AutoModelForSequenceClassification, AutoTokenizer


def log(message: str) -> None:
    """打印带时间戳的日志（标准库 logging 对这个规模的服务过重）。"""
    print(f"[{time.strftime('%H:%M:%S')}] {message}", flush=True)


class Reranker:
    """把 (查询, 文档) 成对送进 cross-encoder，输出相关性分数。

    重排与向量化的关键差异：embedding 每段文本各算一个向量，
    而重排是「查询 + 文档」拼在一起过一遍整模型，所以开销与候选数线性相关。
    """

    def __init__(self, model_dir: str, device: str, dtype: str, max_length: int, batch_size: int):
        self.model_dir = model_dir
        self.device = torch.device(device)
        self.max_length = max_length
        self.batch_size = batch_size
        # MPS 上的推理不是线程安全的：并发前向会互相干扰，用锁把推理串起来。
        self._lock = threading.Lock()

        torch_dtype = torch.float16 if dtype == "float16" else torch.float32
        log(f"加载模型 {model_dir}（device={device}, dtype={dtype}）")
        self.tokenizer = AutoTokenizer.from_pretrained(model_dir)
        self.model = AutoModelForSequenceClassification.from_pretrained(
            model_dir, torch_dtype=torch_dtype
        )
        self.model.to(self.device)
        self.model.eval()

        # 预热一次：MPS 首次前向要编译内核，不预热会把耗时算到第一个用户请求上。
        started = time.time()
        self.rerank("预热", ["预热文本"])
        log(f"模型就绪，预热耗时 {time.time() - started:.2f}s")

    @torch.inference_mode()
    def rerank(self, query: str, texts: list[str]) -> list[float]:
        if not texts:
            return []
        scores: list[float] = []
        with self._lock:
            for start in range(0, len(texts), self.batch_size):
                batch = texts[start : start + self.batch_size]
                # 每篇文档与查询拼成一个序列：这就是 cross-encoder 的输入形态。
                encoded = self.tokenizer(
                    [query] * len(batch),
                    batch,
                    padding=True,
                    truncation=True,
                    max_length=self.max_length,
                    return_tensors="pt",
                )
                encoded = {key: value.to(self.device) for key, value in encoded.items()}
                logits = self.model(**encoded).logits.view(-1).float()
                scores.extend(logits.cpu().tolist())
        return scores


def build_handler(reranker: Reranker, started_at: float):
    """构造 HTTP 处理器：闭包持有模型实例，避免全局变量。"""

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt: str, *args) -> None:  # noqa: A003
            # 默认实现会把每个请求打到 stderr，噪音太大；只保留我们自己关心的日志。
            return

        def _write_json(self, status: int, payload) -> None:
            body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self) -> None:  # noqa: N802
            if self.path == "/info":
                # /info 报出真实加载的模型与设备：配置页据此判断「界面写的」与「实际跑的」是否一致。
                self._write_json(200, {
                    "model_id": reranker.model_dir,
                    "model_dtype": "float16" if reranker.model.dtype == torch.float16 else "float32",
                    "device": str(reranker.device),
                    "torch": torch.__version__,
                    "max_input_length": reranker.max_length,
                    "version": "rerank-mps-1.0",
                    "uptime_seconds": int(time.time() - started_at),
                })
                return
            if self.path == "/health":
                self._write_json(200, {"status": "ok"})
                return
            self._write_json(404, {"detail": "not found"})

        def do_POST(self) -> None:  # noqa: N802
            if self.path != "/rerank":
                self._write_json(404, {"detail": "not found"})
                return

            try:
                length = int(self.headers.get("Content-Length", "0"))
                payload = json.loads(self.rfile.read(length) or b"{}")
            except (ValueError, json.JSONDecodeError) as error:
                self._write_json(400, {"detail": f"请求体不是合法 JSON: {error}"})
                return

            query = (payload.get("query") or "").strip()
            texts = payload.get("texts") or []
            if not query:
                self._write_json(400, {"detail": "query 不能为空"})
                return
            if not isinstance(texts, list):
                self._write_json(400, {"detail": "texts 必须是字符串数组"})
                return

            raw_scores = bool(payload.get("raw_scores", False))
            try:
                scores = reranker.rerank(query, [str(text) for text in texts])
            except Exception as error:  # noqa: BLE001 - 服务边界，统一转成 500 并保留原因
                log(f"重排失败: {error}")
                self._write_json(500, {"detail": f"重排失败: {error}"})
                return

            results = []
            for index, score in enumerate(scores):
                # 与 TEI 对齐：raw_scores=false 时对单标签分类模型做 sigmoid，
                # 让分数落在 0~1，便于跨模型比较与展示。
                value = score if raw_scores else 1.0 / (1.0 + math.exp(-score))
                results.append({"index": index, "score": value})
            # TEI 也是按分数降序返回；客户端按下标取值，顺序不影响正确性。
            results.sort(key=lambda item: item["score"], reverse=True)
            self._write_json(200, results)

    return Handler


def detect_device(requested: str) -> str:
    """按 mps → cuda → cpu 的顺序探测可用设备。

    把「选设备」这件事收进代码而不是文档：使用者不该为了跑起来先去查自己机器有什么。
    """
    if requested != "auto":
        return requested
    if torch.backends.mps.is_available():
        return "mps"
    if torch.cuda.is_available():
        return "cuda"
    return "cpu"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="TEI 协议兼容的本机重排服务（MPS / CUDA / CPU）")
    parser.add_argument(
        "--model",
        default="deploy/models/BAAI/bge-reranker-v2-m3",
        help="模型目录（HF 格式）或 HuggingFace 仓库名",
    )
    parser.add_argument(
        "--device",
        default="auto",
        help="PyTorch 设备：auto（按 mps → cuda → cpu 探测）/ mps / cuda / cpu",
    )
    parser.add_argument("--dtype", default="float32", choices=["float16", "float32"])
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--max-length", type=int, default=512, help="单条序列最大 token 数")
    parser.add_argument("--batch-size", type=int, default=8)
    return parser.parse_args()


def main() -> int:
    args = parse_args()

    device = detect_device(args.device)
    if args.device != "auto" and args.device == "mps" and not torch.backends.mps.is_available():
        # 显式指定却不可用：直接失败并说明原因，不要偷偷换成 CPU。
        log("当前环境的 PyTorch 不支持 MPS（需要 Apple Silicon + macOS 12.3+），请改用 --device cpu 或 auto")
        return 2
    if device == "cpu":
        log("未检测到可用 GPU（MPS/CUDA），使用 CPU 推理：能跑但明显更慢，建议减小 candidate_top_k")

    started_at = time.time()
    reranker = Reranker(args.model, device, args.dtype, args.max_length, args.batch_size)

    server = ThreadingHTTPServer((args.host, args.port), build_handler(reranker, started_at))
    log(f"监听 http://{args.host}:{args.port}（GET /info、POST /rerank）")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        log("收到中断，退出")
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
