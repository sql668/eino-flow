# MinerU 文档解析服务（CPU 版）
#
# 为什么不直接用 MinerU 官方 Dockerfile：它基于 vllm/vllm-openai 镜像，强制要求 NVIDIA GPU
# 和 CUDA，在 Apple Silicon 开发机上跑不起来。这里用 CPU 路线。
#
# Linux 上 MinerU 的基础包会自动选择 ONNX / CPU 跑小模型，**不需要 Torch**（省下约 2GB），
# 所以这个镜像既能用于本机开发，也能直接部署到 x86_64 的云服务器上。
#
# 能力边界：basic 档不需要 VLM。standard / advanced 档需要 VLM（llama.cpp + GGUF 模型），
# 体积和内存会大幅上升，本项目暂不启用。

FROM python:3.12-slim

# libgl1：opencv 运行时依赖
# libgomp1：ONNX Runtime 的 OpenMP 依赖
# curl：供下面的 healthcheck 使用
RUN apt-get update \
    && apt-get install -y --no-install-recommends libgl1 libgomp1 curl \
    && rm -rf /var/lib/apt/lists/*

# 基础包在 Linux 上包含 ONNX + llama.cpp，不含 Torch
RUN pip install --no-cache-dir --upgrade "mineru>=4.0,<5"

# 国内环境默认走 ModelScope 下载模型（HuggingFace 直连会被墙）
ENV MINERU_MODEL_SOURCE=modelscope \
    MINERU_HOME=/data/mineru

# 模型持久化到 MINERU_HOME 下的 volume，容器重建时不用重新下载
VOLUME ["/data/mineru"]

EXPOSE 8000

# 首次解析时才加载模型，所以给足启动观察期
HEALTHCHECK --interval=30s --timeout=10s --start-period=300s --retries=5 \
    CMD curl -fsS http://127.0.0.1:8000/v1/health || exit 1

ENTRYPOINT ["mineru-kit", "api-server", "--host", "0.0.0.0", "--port", "8000"]
