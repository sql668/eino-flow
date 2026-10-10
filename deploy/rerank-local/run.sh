#!/usr/bin/env bash
# 本机重排服务的启停脚本：start / stop / restart / status / logs。
#
# 与主脚本 scripts/einoflow.sh 的关系：主脚本负责「依赖容器 + 后端 + 前端」，
# 这个是**可选的本地组件**（重排）。分开是为了让不装 Python 的人也能正常跑项目——
# 重排是可降级能力：不启动它，检索照常工作，只是少一层精排。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RUN_DIR="${PROJECT_ROOT}/.run"
LOG_DIR="${PROJECT_ROOT}/logs"
PID_FILE="${RUN_DIR}/rerank.pid"
LOG_FILE="${LOG_DIR}/rerank.log"

PORT="${RERANK_PORT:-8080}"
MODEL_DIR="${RERANK_MODEL_DIR:-${PROJECT_ROOT}/deploy/models/${TEI_MODEL_ID:-BAAI/bge-reranker-v2-m3}}"
DEVICE="${RERANK_DEVICE:-auto}"
VENV_PYTHON="${SCRIPT_DIR}/.venv/bin/python"

ok() { printf '[OK] %s\n' "$1"; }
info() { printf '[..] %s\n' "$1"; }
warn() { printf '[!!] %s\n' "$1"; }
err() { printf '[XX] %s\n' "$1" >&2; }

is_running() {
  [ -f "${PID_FILE}" ] || return 1
  local pid
  pid="$(cat "${PID_FILE}" 2>/dev/null || true)"
  [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null
}

require_ready() {
  if [ ! -x "${VENV_PYTHON}" ]; then
    err "尚未准备环境，请先执行：deploy/rerank-local/bootstrap.sh"
    return 1
  fi
  if [ ! -f "${MODEL_DIR}/config.json" ]; then
    err "未找到模型目录 ${MODEL_DIR}，请先执行：deploy/rerank-local/bootstrap.sh"
    return 1
  fi
}

cmd_start() {
  if is_running; then
    warn "重排服务已在运行 (pid $(cat "${PID_FILE}"))，跳过"
    return 0
  fi
  require_ready || return 1

  mkdir -p "${RUN_DIR}" "${LOG_DIR}"
  info "启动重排服务（端口 ${PORT}，设备 ${DEVICE}，日志 ${LOG_FILE}）"
  # nohup + 重定向：脚本退出后服务继续跑，且不继承调用方的 stdout。
  (
    cd "${PROJECT_ROOT}" && exec nohup "${VENV_PYTHON}" "${SCRIPT_DIR}/server.py" \
      --model "${MODEL_DIR}" --device "${DEVICE}" --port "${PORT}"
  ) >>"${LOG_FILE}" 2>&1 </dev/null &
  echo $! >"${PID_FILE}"

  # 等模型加载完成（首次要读 2GB 权重并预热，给足时间）。
  local i=0
  while [ "${i}" -lt 120 ]; do
    if curl -fsS -m 2 -o /dev/null "http://127.0.0.1:${PORT}/health" 2>/dev/null; then
      ok "重排服务就绪：http://127.0.0.1:${PORT}"
      return 0
    fi
    if ! is_running; then
      err "进程已退出，查看日志：${LOG_FILE}"
      return 1
    fi
    sleep 1
    i=$((i + 1))
  done
  err "120 秒内未就绪，查看日志：${LOG_FILE}"
  return 1
}

cmd_stop() {
  if ! is_running; then
    warn "重排服务未在运行"
    rm -f "${PID_FILE}"
    return 0
  fi
  local pid
  pid="$(cat "${PID_FILE}")"
  info "停止重排服务 (pid ${pid})"
  kill "${pid}" 2>/dev/null || true
  local i=0
  while [ "${i}" -lt 20 ] && kill -0 "${pid}" 2>/dev/null; do
    sleep 0.5
    i=$((i + 1))
  done
  kill -9 "${pid}" 2>/dev/null || true
  rm -f "${PID_FILE}"
  ok "重排服务已停止"
}

cmd_status() {
  if is_running; then
    ok "重排服务运行中 (pid $(cat "${PID_FILE}"))"
    curl -sS -m 3 "http://127.0.0.1:${PORT}/info" 2>/dev/null || warn "端口无响应"
    printf '\n'
  else
    warn "重排服务未运行（检索仍可用，只是不做重排）"
  fi
}

case "${1:-status}" in
  start) cmd_start ;;
  stop) cmd_stop ;;
  restart) cmd_stop; cmd_start ;;
  status) cmd_status ;;
  logs) tail -f "${LOG_FILE}" ;;
  *)
    err "用法：$0 [start|stop|restart|status|logs]"
    exit 1
    ;;
esac
