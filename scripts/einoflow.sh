#!/usr/bin/env bash
#
# EinoFlow 一键启停脚本
#
# 用法：
#   ./scripts/einoflow.sh start            启动依赖容器 + 后端 + 前端（仅本地访问）
#   ./scripts/einoflow.sh start --tunnel   同上，并额外开启 Cloudflare 公网隧道
#   ./scripts/einoflow.sh stop             停止前端 + 后端 + 隧道，并暂停依赖容器
#   ./scripts/einoflow.sh restart [--tunnel]   等价于 stop 后 start
#   ./scripts/einoflow.sh status           查看容器、进程、健康检查与访问地址
#   ./scripts/einoflow.sh logs [组件]      实时查看日志：backend / web / deps / tunnel
#   ./scripts/einoflow.sh tunnel [up|down|status]   单独控制公网隧道
#
# 环境变量（从项目根目录 .env 读取，也可在命令前临时覆盖）：
#   EINOFLOW_SERVER_PORT    后端端口，默认 8888
#   WEB_PORT                前端端口，默认 3001
#   EINOFLOW_WEB_MODE       prod（默认，构建后运行）或 dev（开发热更新）
#   EINOFLOW_DEMO_DOMAIN    公网演示域名，用于 status 展示
#   CF_TUNNEL_TOKEN         Cloudflare 隧道 token，为空则跳过隧道
#
# 说明：后端与前端以「构建产物直接运行」的方式跑在后台，PID 记在 .run/，日志写在 logs/。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RUN_DIR="$ROOT_DIR/.run"
LOG_DIR="$ROOT_DIR/logs"
WEB_MODE="${EINOFLOW_WEB_MODE:-prod}"

mkdir -p "$RUN_DIR" "$LOG_DIR"

# ---------- 输出样式 ----------
if [ -t 1 ]; then
  C_B=$'\033[36m'; C_G=$'\033[32m'; C_Y=$'\033[33m'; C_R=$'\033[31m'; C_0=$'\033[0m'
else
  C_B=; C_G=; C_Y=; C_R=; C_0=
fi
info() { printf '%s>>>%s %s\n' "$C_B" "$C_0" "$*"; }
ok()   { printf '%s[OK]%s %s\n' "$C_G" "$C_0" "$*"; }
warn() { printf '%s[!]%s  %s\n' "$C_Y" "$C_0" "$*"; }
err()  { printf '%s[×]%s  %s\n' "$C_R" "$C_0" "$*" >&2; }

# ---------- 基础工具 ----------

# 载入 .env，让后端能拿到 EINOFLOW_* 与隧道 token。
load_env() {
  if [ -f "$ROOT_DIR/.env" ]; then
    set -a
    # shellcheck disable=SC1091
    . "$ROOT_DIR/.env"
    set +a
  else
    warn "未找到 .env，将使用 backend/configs/config.yaml 的默认值（密码为空）"
  fi
}

pid_file() { printf '%s/%s.pid' "$RUN_DIR" "$1"; }

# 判断某个组件的后台进程是否还活着。
is_running() {
  local f pid
  f="$(pid_file "$1")"
  [ -f "$f" ] || return 1
  pid="$(cat "$f" 2>/dev/null || true)"
  [ -n "$pid" ] || return 1
  kill -0 "$pid" 2>/dev/null
}

# 按 PID 文件停止进程：先 SIGTERM 优雅退出，超时再 SIGKILL。
kill_by_pidfile() {
  local name="$1" f pid i
  f="$(pid_file "$name")"
  if is_running "$name"; then
    pid="$(cat "$f")"
    info "停止 $name (pid $pid)"
    kill "$pid" 2>/dev/null || true
    i=0
    while [ "$i" -lt 20 ] && kill -0 "$pid" 2>/dev/null; do
      sleep 0.5
      i=$((i + 1))
    done
    if kill -0 "$pid" 2>/dev/null; then
      warn "$name 未在 10s 内退出，强制结束"
      kill -9 "$pid" 2>/dev/null || true
    fi
    ok "$name 已停止"
  fi
  rm -f "$f"
}

# 端口是否被占用（用于启动前自检，避免启动失败却看不出原因）。
port_in_use() {
  nc -z 127.0.0.1 "$1" >/dev/null 2>&1
}

# 轮询 HTTP 直到就绪。
wait_http() {
  local url="$1" name="$2" timeout="${3:-60}" i=0
  while [ "$i" -lt "$timeout" ]; do
    if curl -fsS -o /dev/null --max-time 2 "$url" 2>/dev/null; then
      ok "$name 就绪"
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  err "$name 在 ${timeout}s 内未就绪，查看 logs/ 下的日志"
  return 1
}

# 等待依赖容器全部进入 running 且非 unhealthy 状态。
wait_deps() {
  local timeout="${1:-90}" i=0 status
  while [ "$i" -lt "$timeout" ]; do
    status="$(docker ps --filter 'name=einoflow-' --format '{{.Status}}' 2>/dev/null || true)"
    if [ -n "$status" ] \
      && ! printf '%s' "$status" | grep -qE 'health: starting|unhealthy|Restarting' \
      && [ "$(printf '%s\n' "$status" | grep -c 'Up')" -ge 6 ]; then
      ok "依赖容器全部就绪"
      return 0
    fi
    sleep 2
    i=$((i + 2))
  done
  warn "依赖容器在 ${timeout}s 内未全部就绪，用 ./scripts/einoflow.sh status 查看"
  return 0
}

# ---------- 各组件 ----------

start_deps() {
  info "启动依赖容器（PostgreSQL / Redis / MinIO / etcd / Milvus / Attu）"
  docker compose up -d
  wait_deps
}

start_backend() {
  if is_running backend; then
    warn "后端已在运行 (pid $(cat "$(pid_file backend)"))，跳过"
    return 0
  fi
  if port_in_use "${EINOFLOW_SERVER_PORT:-8888}"; then
    err "端口 ${EINOFLOW_SERVER_PORT:-8888} 已被占用，请先释放（./scripts/einoflow.sh status 可查看）"
    return 1
  fi

  info "构建后端"
  (cd backend && go build -o bin/server ./cmd/server)

  info "启动后端（日志：logs/backend.log）"
  # 两个要点：
  #   1) exec 让子 shell 被服务进程替换，PID 即服务本身，停止时不会残留子进程；
  #   2) 重定向加在整个子 shell 上，避免后台进程继承调用方的 stdout 管道——
  #      否则 `./scripts/einoflow.sh start | tail` 这类用法会因管道写端不关闭而一直挂住。
  (
    cd "$ROOT_DIR/backend" && exec nohup ./bin/server
  ) >>"$LOG_DIR/backend.log" 2>&1 </dev/null &
  echo $! >"$(pid_file backend)"
  wait_http "http://127.0.0.1:${EINOFLOW_SERVER_PORT:-8888}/healthz" "后端"
}

start_web() {
  if is_running web; then
    warn "前端已在运行 (pid $(cat "$(pid_file web)"))，跳过"
    return 0
  fi
  if port_in_use "${WEB_PORT:-3001}"; then
    err "端口 ${WEB_PORT:-3001} 已被占用，请先释放（Milvus Attu 也占用 3000，注意区分）"
    return 1
  fi

  if [ "$WEB_MODE" = "dev" ]; then
    info "以开发模式启动前端（日志：logs/web.log）"
  else
    info "构建前端（生产模式）"
    (cd web && pnpm build)
    info "启动前端（日志：logs/web.log）"
  fi

  # 用 node_modules/.bin/next 而不是 pnpm：避免 pnpm 再套一层子进程，导致停止时杀不干净。
  local next_bin="$ROOT_DIR/web/node_modules/.bin/next"
  if [ "$WEB_MODE" = "dev" ]; then
    (
      cd "$ROOT_DIR/web" && exec nohup "$next_bin" dev -p "${WEB_PORT:-3001}"
    ) >>"$LOG_DIR/web.log" 2>&1 </dev/null &
  else
    (
      cd "$ROOT_DIR/web" && exec nohup "$next_bin" start -p "${WEB_PORT:-3001}"
    ) >>"$LOG_DIR/web.log" 2>&1 </dev/null &
  fi
  echo $! >"$(pid_file web)"
  wait_http "http://127.0.0.1:${WEB_PORT:-3001}/" "前端" 90
}

start_tunnel() {
  if [ -z "${CF_TUNNEL_TOKEN:-}" ]; then
    warn "未配置 CF_TUNNEL_TOKEN，跳过隧道（公网不可访问）"
    return 0
  fi
  docker rm -f einoflow-tunnel >/dev/null 2>&1 || true
  docker run -d --name einoflow-tunnel --restart unless-stopped \
    cloudflare/cloudflared:latest tunnel --no-autoupdate run --token "$CF_TUNNEL_TOKEN" >/dev/null
  sleep 8
  if docker logs einoflow-tunnel 2>&1 | grep -q "Registered tunnel connection"; then
    ok "隧道已连接"
  else
    warn "隧道已启动但尚未注册连接，稍后用 ./scripts/einoflow.sh logs tunnel 查看"
  fi
}

stop_tunnel() {
  if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -q '^einoflow-tunnel$'; then
    info "停止隧道容器"
    docker rm -f einoflow-tunnel >/dev/null 2>&1 || true
    ok "隧道已停止"
  fi
}

# ---------- 命令 ----------

cmd_start() {
  load_env
  local with_tunnel=0
  [ "${1:-}" = "--tunnel" ] && with_tunnel=1

  start_deps
  start_backend
  start_web
  # 隧道是可选的：默认只做本地启动，需要公网访问时加 --tunnel。
  if [ "$with_tunnel" = "1" ]; then
    start_tunnel
  fi
  echo
  cmd_status
}

cmd_stop() {
  load_env
  stop_tunnel
  kill_by_pidfile web
  kill_by_pidfile backend
  info "暂停依赖容器（数据保留在 volume 中）"
  docker compose stop
  ok "已全部停止"
}

cmd_restart() {
  local with_tunnel=""
  [ "${1:-}" = "--tunnel" ] && with_tunnel="--tunnel"
  cmd_stop
  echo
  # shellcheck disable=SC2086
  cmd_start $with_tunnel
}

cmd_tunnel() {
  load_env
  case "${1:-status}" in
    up)   start_tunnel ;;
    down) stop_tunnel ;;
    status)
      if docker ps --format '{{.Names}}' 2>/dev/null | grep -q '^einoflow-tunnel$'; then
        ok "隧道运行中"
        docker logs --tail 3 einoflow-tunnel 2>&1 | sed 's/^/    /'
      else
        warn "隧道未运行（本地访问不受影响）"
      fi
      ;;
    *) err "用法：$0 tunnel [up|down|status]"; exit 1 ;;
  esac
}

cmd_status() {
  load_env

  echo "${C_B}=== 依赖容器 ===${C_0}"
  docker compose ps --format 'table {{.Name}}\t{{.Status}}' 2>/dev/null || warn "docker 不可用"

  echo
  echo "${C_B}=== 本地进程 ===${C_0}"
  local name
  for name in backend web; do
    if is_running "$name"; then
      ok "$name 运行中 (pid $(cat "$(pid_file "$name")"))"
    else
      warn "$name 未运行"
    fi
  done

  echo
  echo "${C_B}=== 健康检查 ===${C_0}"
  local url
  for url in "http://127.0.0.1:${EINOFLOW_SERVER_PORT:-8888}/healthz|后端 /healthz" \
             "http://127.0.0.1:${WEB_PORT:-3001}/|前端首页"; do
    local u="${url%%|*}" label="${url##*|}"
    if curl -fsS -o /dev/null --max-time 2 "$u" 2>/dev/null; then
      ok "$label 可访问"
    else
      warn "$label 不可访问"
    fi
  done

  echo
  echo "${C_B}=== 访问地址 ===${C_0}"
  printf '  本地前端：http://127.0.0.1:%s\n' "${WEB_PORT:-3001}"
  printf '  本地后端：http://127.0.0.1:%s\n' "${EINOFLOW_SERVER_PORT:-8888}"
  if [ -n "${EINOFLOW_DEMO_DOMAIN:-}" ]; then
    if docker ps --format '{{.Names}}' 2>/dev/null | grep -q '^einoflow-tunnel$'; then
      printf '  公网演示：https://%s\n' "$EINOFLOW_DEMO_DOMAIN"
    else
      printf '  公网演示：https://%s （隧道未运行）\n' "$EINOFLOW_DEMO_DOMAIN"
    fi
  fi
  printf '  Milvus 管理台：http://127.0.0.1:3000\n'
}

cmd_logs() {
  case "${1:-}" in
    backend) tail -f "$LOG_DIR/backend.log" ;;
    web)     tail -f "$LOG_DIR/web.log" ;;
    deps)    docker compose logs -f --tail 50 ;;
    tunnel)  docker logs -f --tail 50 einoflow-tunnel ;;
    *)       err "用法：$0 logs [backend|web|deps|tunnel]"; exit 1 ;;
  esac
}

usage() {
  cat <<'EOF'
EinoFlow 一键启停脚本

  start              启动依赖容器 + 后端 + 前端（仅本地访问）
  start --tunnel     同上，并额外开启 Cloudflare 公网隧道
  stop               停止前端 + 后端 + 隧道，并暂停依赖容器
  restart [--tunnel] 重启全部
  status             查看容器、进程、健康检查与访问地址
  logs [组件]        实时日志：backend | web | deps | tunnel
  tunnel [up|down|status]   单独控制公网隧道
EOF
}

case "${1:-}" in
  start)   cmd_start "${2:-}" ;;
  stop)    cmd_stop ;;
  restart) cmd_restart "${2:-}" ;;
  status)  cmd_status ;;
  logs)    cmd_logs "${2:-}" ;;
  tunnel)  cmd_tunnel "${2:-status}" ;;
  *)       usage; exit 1 ;;
esac
