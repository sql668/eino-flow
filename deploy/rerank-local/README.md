# 本机重排服务（TEI 协议兼容）

重排是**可选能力**：不启动它，检索照常工作，只是少一层精排。

## 为什么有两个重排实现

重排需要一个 cross-encoder 模型，本仓库提供两条路，**协议完全相同**
（`GET /info`、`POST /rerank`），因此后端的 `重排服务` 厂商指向哪个都能用：

| 方式 | 适合 | 说明 |
| --- | --- | --- |
| `docker compose --profile rerank up -d tei` | Linux x86_64 服务器 | 官方 TEI 镜像，原生执行、无需装 Python。**该镜像只发布 linux/amd64** |
| `deploy/rerank-local`（本目录） | macOS（Apple Silicon / Intel）、以及不想装 Docker 的场景 | PyTorch 直接调用本机 GPU（Apple Metal / NVIDIA CUDA），也可退到 CPU |

为什么 Apple Silicon 上不建议用容器里的 TEI：镜像只有 amd64，容器里又拿不到 AVX2，
TEI 只能走标量内核。实测**同样模型、同样 2 条候选，模拟环境 216 秒，本机 MPS 服务 0.52 秒**。

## 快速开始

```bash
deploy/rerank-local/bootstrap.sh     # 建虚拟环境 + 装依赖 + 下载模型（幂等，可重复执行）
deploy/rerank-local/run.sh start     # 启动，默认 http://127.0.0.1:8080
deploy/rerank-local/run.sh status    # 查看状态与已加载的模型
```

然后在「模型配置 → 重排服务」里确认地址是 `http://127.0.0.1:8080`，
添加一个 `rerank` 类型的模型（默认 `BAAI/bge-reranker-v2-m3`）并设为默认。

## 环境变量

`bootstrap.sh` 与 `run.sh` 都读这套变量，按需覆盖：

| 变量 | 默认 | 用途 |
| --- | --- | --- |
| `TEI_MODEL_ID` | `BAAI/bge-reranker-v2-m3` | 模型（小机器建议换 `BAAI/bge-reranker-base` 等更小的模型） |
| `RERANK_MODEL_DIR` | `deploy/models/<模型名>` | 模型缓存目录 |
| `RERANK_PORT` | `8080` | 监听端口 |
| `RERANK_DEVICE` | `auto` | `auto` / `mps` / `cuda` / `cpu`；`auto` 按 mps → cuda → cpu 探测 |
| `HF_ENDPOINT` | 自动探测 | 留空时先试 huggingface.co，不可达则自动切 hf-mirror |
| `PIP_INDEX_URL` | 官方源 | 国内可设为镜像源加速安装 |
| `TORCH_INDEX_URL` | 官方源 | Linux 上装 CPU 版 torch 时可设为 `https://download.pytorch.org/whl/cpu` |

## 接口

与 TEI 保持一致，便于两种实现互换：

```bash
curl http://127.0.0.1:8080/info
# {"model_id":"...","model_dtype":"float32","device":"mps","torch":"2.14.1", ...}

curl -X POST http://127.0.0.1:8080/rerank \
  -H 'Content-Type: application/json' \
  -d '{"query":"如何测量直流电压","texts":["直流电压测量步骤：将功能转盘置于 V DC 档"]}'
# [{"index":0,"score":0.9976}]
```

约定：`raw_scores=false`（默认）时把单标签分类模型的 logit 过一遍 sigmoid，
分数落在 0~1，便于跨模型比较；返回按下标标注，调用方按 `index` 取值而不是按顺序
（顺序只是排序结果，不代表输入次序）。
