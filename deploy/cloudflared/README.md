# 公网演示部署（Cloudflare 隧道）

## 为什么用隧道，而不是把服务直接部署到服务器

那台 2C2G 服务器跑不动完整栈（Milvus 是内存大户），而且大陆服务器用域名走 80/443 需要 ICP 备案。
Cloudflare 隧道把本地这台 Mac 上的**完整栈**暴露到公网，绕过这两个限制。

流量路径：

```
浏览器 → Cloudflare 边缘 → cloudflared 容器 → host.docker.internal:3001 → Next → Go 后端
```

- 不需要公网 IP、不需要在服务器上开任何端口、不受备案拦截影响、HTTPS 自动签发。
- 因为前端与后端是**同源**（见 `web/next.config.mjs` 的 rewrites），只需要暴露 3001 一个端口，
  `/api/*`、`/healthz`、`/readyz` 都由 Next 按路径转发，不存在跨域问题。

## 当前配置（已完成，2026-10-10 验证通过）

| 项 | 值 |
| --- | --- |
| 访问地址 | https://einoflow.sql668.com |
| 隧道名 | `einoflow` |
| 子域 / 域 | `einoflow` / `sql668.com` |
| 服务 | `HTTP` → `host.docker.internal:3001` |

> 改子域只需在后台改路由并保存，本地代码和隧道都不用重启（路由会热更新）。

## 一次性配置

1. Cloudflare Dashboard → **Zero Trust** → Networks → **Tunnels** → Create a tunnel，
   类型选 **Cloudflared**，名字填 `einoflow`，保存后复制那串 token。

2. 在同一页的 **Public Hostname** 里新增一条：

   | 字段 | 值 |
   | --- | --- |
   | Subdomain | `einoflow` |
   | Domain | 你的域名 |
   | Service | `HTTP` → `host.docker.internal:3001` |

   ⚠️ 必须填 `host.docker.internal`，不能填 `127.0.0.1`——隧道跑在容器里，
   容器内的 `127.0.0.1` 指的是容器自己，不是你的 Mac。

3. 把 token 写进项目根目录的 `.env`（该文件已被 `.gitignore` 忽略）：

   ```
   CF_TUNNEL_TOKEN=eyJhIjoi...
   ```

4. 启动隧道：

   ```bash
   make tunnel-up
   ```

之后访问 `https://einoflow.sql668.com` 就是完整演示站（含 Milvus、Attu、观测面板）。

### 一个已经验证过的坑

用 token 运行时（远程管理型），**cloudflared 会忽略本地配置文件，只用后台下发的路由配置**。
实测日志会打印 `Updated to new configuration config="{\"ingress\":[...]}"`，里面的 hostname
就是后台配的那条——排查问题时这条日志很有用，能直接看出后台到底配了什么。

所以：**路由规则只能在 Cloudflare 后台改**，改完本地不用重启隧道（会热更新），
本仓库里也没有对应的配置文件（曾经放过一个，验证后确认无效已删除）。

## 常用命令

| 命令 | 作用 |
| --- | --- |
| `make tunnel-quick` | 免账号的临时隧道，生成随机 `*.trycloudflare.com` 地址，用于快速验证 |
| `make tunnel-up` | 启动固定域名的隧道（需先配 `CF_TUNNEL_TOKEN`） |
| `make tunnel-logs` | 查看隧道日志 |
| `make tunnel-down` | 停止隧道 |

## 注意事项

- **Mac 不能休眠**，休眠后隧道会断。演示前执行 `caffeinate -i`，或到「系统设置 → 锁定屏幕」把睡眠设为「永不」。
- **Docker Desktop 必须运行**，建议勾选「登录时启动」。
- 隧道容器带了 `--restart unless-stopped`，Docker 重启后会自动恢复。
- 演示建议用 `make web-start`（生产模式构建后运行）而不是 `pnpm dev`，首屏更快、更稳。
- **排查掉线**：访问域名出现 530 / error 1033 就是「隧道无活跃连接」。先看
  `docker ps --filter name=einoflow-tunnel`，再看 `make tunnel-logs`：
  - 日志有 `Registered tunnel connection` → 连接正常，问题在后端/前端是否在跑；
  - 日志停在 `graceful shutdown due to signal terminated` → 容器被停过，`make tunnel-up` 重启即可。
- **别在终端里回显 token**：`make tunnel-up` 已经加了 `@` 抑制回显；如果你手动执行 `docker run`，
  记得用 `--token "$(grep '^CF_TUNNEL_TOKEN=' .env | cut -d= -f2-)"` 这种方式从文件读，不要直接粘贴。

## 可选：把 Attu 也暴露出去

Attu（Milvus 可视化管理台）跑在 3000 端口。需要演示向量库管理时，
在隧道里再加一条 Public Hostname：子域名 `attu`，Service 填 `http://host.docker.internal:3000`。
