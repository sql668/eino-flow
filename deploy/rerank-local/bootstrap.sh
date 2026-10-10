#!/usr/bin/env bash
# 本机重排服务的一键准备脚本：建虚拟环境 → 装依赖 → 下载模型。
#
# 为什么要脚本而不是写一段 README：开源之后别人要能照着跑起来，而这里有三处
# 与机器/网络相关的差异，手工照抄很容易踩：
#   1) PyTorch 的安装源（CPU 版 / CUDA 版 / 国内镜像）；
#   2) HuggingFace 是否可达（国内需要镜像站，且镜像站不支持 Xet 存储）；
#   3) 模型是否已经下载过（TEI 那份可以直接复用，不必再下 2GB）。
#
# 幂等：重复执行只会补齐缺失的部分。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

VENV_DIR="${SCRIPT_DIR}/.venv"
MODEL_ID="${TEI_MODEL_ID:-BAAI/bge-reranker-v2-m3}"
MODEL_DIR="${RERANK_MODEL_DIR:-${PROJECT_ROOT}/deploy/models/${MODEL_ID}}"

info() { printf '[rerank] %s\n' "$1"; }
err() { printf '[rerank] 错误：%s\n' "$1" >&2; }

# 探测 HuggingFace 可达性：可达就用官方，不可达自动切镜像站。
# 让代码去适配网络，而不是让使用者先读文档再改环境变量。
detect_hf_endpoint() {
  if [ -n "${HF_ENDPOINT:-}" ]; then
    printf '%s' "${HF_ENDPOINT}"
    return
  fi
  if curl -fsS -m 6 -o /dev/null https://huggingface.co/api/models 2>/dev/null; then
    printf '%s' "https://huggingface.co"
  else
    printf '%s' "https://hf-mirror.com"
  fi
}

HF_ENDPOINT_RESOLVED="$(detect_hf_endpoint)"
info "HuggingFace 端点：${HF_ENDPOINT_RESOLVED}"

if [ ! -d "${VENV_DIR}" ]; then
  if command -v uv >/dev/null 2>&1; then
    info "用 uv 创建虚拟环境"
    uv venv "${VENV_DIR}"
  else
    info "用 python3 -m venv 创建虚拟环境"
    python3 -m venv "${VENV_DIR}"
  fi
fi

PYTHON="${VENV_DIR}/bin/python"
[ -x "${PYTHON}" ] || PYTHON="${VENV_DIR}/Scripts/python.exe"  # Windows (Git Bash)

# 依赖：只有 torch / transformers / sentencepiece。
# 国内可设 PIP_INDEX_URL 走镜像；Linux 上若要 CPU 版 torch，可设 TORCH_INDEX_URL。
PIP_ARGS=(--quiet)
if [ -n "${PIP_INDEX_URL:-}" ]; then
  PIP_ARGS+=(--index-url "${PIP_INDEX_URL}")
fi
if [ -n "${TORCH_INDEX_URL:-}" ]; then
  PIP_ARGS+=(--extra-index-url "${TORCH_INDEX_URL}")
fi

info "安装依赖（torch / transformers / sentencepiece，首次约 1~2 分钟）"
"${PYTHON}" -m pip install "${PIP_ARGS[@]}" --upgrade pip >/dev/null
"${PYTHON}" -m pip install "${PIP_ARGS[@]}" torch transformers sentencepiece

if [ ! -f "${MODEL_DIR}/config.json" ]; then
  info "下载模型 ${MODEL_ID} 到 ${MODEL_DIR}"
  # HF_HUB_DISABLE_XET=1：镜像站不代理 HuggingFace 新的 Xet 存储（会返回 401），
  # 关掉之后走传统文件下载，镜像站才可用。
  HF_ENDPOINT="${HF_ENDPOINT_RESOLVED}" \
  HF_HUB_DISABLE_XET=1 \
  "${PYTHON}" -c "
import os
from huggingface_hub import snapshot_download
snapshot_download('${MODEL_ID}', local_dir='${MODEL_DIR}')
print('模型已就绪:', '${MODEL_DIR}')
"
else
  info "模型已存在，跳过下载：${MODEL_DIR}"
fi

cat <<EOF

[rerank] 准备完成。启动方式：

  deploy/rerank-local/run.sh start

随后在「模型配置 → 重排服务」里把地址填成 http://127.0.0.1:${RERANK_PORT:-8080}
（默认值就是这个，通常无需修改），添加一个 rerank 模型并设为默认。
EOF
