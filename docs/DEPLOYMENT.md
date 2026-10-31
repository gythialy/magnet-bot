# magnet-bot 部署手册

Telegram 公告订阅机器人：定时抓取采购公告 → 按用户关键词匹配 → 推送消息；可选接入
Gotenberg 把公告转成 PDF / PNG。

- 默认通过 **Telegram 长轮询**工作，自身只需出网访问 Telegram 与公告站点；配置
  `PUBLIC_BASE_URL` 后改用 **Webhook** 推送（见 §8），这时才需要公网入口。
- 只有 PDF/PNG 转换功能需要 **Gotenberg 能访问到 bot 的 HTTP 端口**（同一 Docker
  网络即可，不必公网）。

---

## 1. 组件与数据流

| 组件 | 作用 | 是否必需 |
| --- | --- | --- |
| magnet-bot | 抓取、匹配、推送、Telegram 命令处理、转换请求发起与回执 | ✅ |
| Gotenberg 8 | 把公告 URL 渲染成 PDF / 截图 PNG | 仅转换功能需要 |
| `bot.db`（SQLite） | 关键词、推送历史、告警记录 | 启动时自动创建 |

数据流：

1. 每 `SCHEDULE_INTERVAL` 小时抓取一轮公告，按每个用户的关键词匹配；
2. 命中即推送 Telegram HTML 消息；若公告含**复杂表格**（≥ 6 列或 ≥ 10 行），改为
   发**一条消息**：整页转成的 PDF + 正文作为它的 caption（正文超过 1000 字时截断）；
3. 用户执行 `/convertpdf`、`/convertimg` 时，bot 请求 Gotenberg；Gotenberg 完成后
   回调 `POST /pdf/{requestId}`，bot 再把文件发回聊天。

---

## 2. 前置条件

- Telegram bot token（@BotFather 创建），并把 bot 加进目标会话；
- 能出网访问公告站点 API 与 `api.telegram.org`；
- 需要转换功能时：Gotenberg 8 实例 + 一个 Gotenberg 可访问的 webhook 地址。

---

## 3. 环境变量

| 变量 | 必需 | 默认 | 说明 |
| --- | --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | ✅ | - | @BotFather 给的 token |
| `SERVER_URL` | ✅ | - | 公告站点根地址，如 `https://example.com`。只取 host，程序自己拼 `https://` |
| `TELEGRAM_BOT_NAME` | - | - | bot 用户名，用于生成 `/alarm` 分享链接 |
| `MANAGER_ID` | - | `0` | 允许执行 `/retry`、`/clean` 的 Telegram 用户 ID |
| `SCHEDULE_INTERVAL` | - | `1` | 抓取间隔（小时）；填 0、负数或非数字都按 1 处理 |
| `CRAWL_DAYS` | - | `1` | 每轮回溯天数；填 0、负数或非数字都按 1 处理 |
| `CONFIG_PATH` | - | 进程工作目录 | `bot.db`、`bot.log` 所在目录；容器内用 `/etc/magnet` |
| `LOG_LEVEL` | - | `debug` | `trace`/`debug`/`info`/`warn`/`error`；设为 `trace` 会同时开启抓取 HTTP 调试日志（很吵） |
| `PDF_SERVER_URL` | 转换功能 | - | Gotenberg 地址，如 `http://gotenberg:3000` |
| `WEBHOOK_SERVER_URL` | 转换功能 | - | Gotenberg 回调 bot 用的地址，**不带端口、不带结尾斜杠**，如 `http://magnet-bot` |
| `WEBHOOK_SERVER_PORT` | 转换功能 | `0` | bot webhook 监听端口，**必须显式设置**（不设等于随机端口，回调必失败） |
| `WEBHOOK_TOKEN` | - | - | 设置后回调必须带 `?token=...`，建议生产开启 |

> 组合规则：回调地址 = `WEBHOOK_SERVER_URL` + `:` + `WEBHOOK_SERVER_PORT` + `/pdf/{id}`。
> 例：`http://magnet-bot` + `8080` → `http://magnet-bot:8080/pdf/xxx`。

> **只有 `TELEGRAM_BOT_TOKEN` 和 `SERVER_URL` 是必填的**，其余不配就走默认值：
> 不配 `SCHEDULE_INTERVAL` 即每小时抓一次，不配 `CRAWL_DAYS` 即每轮回溯 1 天，
> 不配 `CONFIG_PATH` 即用进程工作目录。

> 启用公网 Webhook / Mini App 时另有 4 个变量（`PUBLIC_BASE_URL`、`TELEGRAM_WEBHOOK_SECRET`、
> `TELEGRAM_WEBHOOK_ENABLED`、`MINI_APP_ENABLED`），且 `WEBHOOK_SERVER_PORT` 变为必填，
> 见 §8.2。

---

## 4. 方式一：Docker Compose（推荐）

```yaml
services:
  magnet-bot:
    image: ghcr.io/gythialy/magnet-bot:latest
    container_name: magnet-bot
    restart: unless-stopped
    environment:
      TELEGRAM_BOT_TOKEN: "${TELEGRAM_BOT_TOKEN}"
      SERVER_URL: "https://example.com"
      MANAGER_ID: "${MANAGER_ID}"
      SCHEDULE_INTERVAL: "1"
      CRAWL_DAYS: "1"
      CONFIG_PATH: "/etc/magnet"
      LOG_LEVEL: "info"
      PDF_SERVER_URL: "http://gotenberg:3000"
      WEBHOOK_SERVER_URL: "http://magnet-bot"   # 不带端口/结尾斜杠
      WEBHOOK_SERVER_PORT: "8080"
      WEBHOOK_TOKEN: "${WEBHOOK_TOKEN}"
    volumes:
      - ./data:/etc/magnet
    depends_on:
      - gotenberg

  gotenberg:
    image: gotenberg/gotenberg:8
    container_name: gotenberg
    restart: unless-stopped
    environment:
      API_TIMEOUT: "120s"  # 转换时限，默认 30s；必须小于 bot 侧 3 分钟 HTTP 超时
    shm_size: "1gb"        # Chromium 需要，太小会崩
```

要点：

- **不需要给 bot 映射端口**：webhook 走 Compose 内部网络，Gotenberg 用服务名
  `http://magnet-bot:8080` 回调即可。只有从宿主机/外部调试时才加 `ports: ["8080:8080"]`。
- **数据目录权限**：镜像以 `nonroot`（uid/gid 65532）运行，挂载目录要可写：

  ```bash
  mkdir -p data && sudo chown -R 65532:65532 data
  ```

- Gotenberg 必须能出网访问公告站点（转换是它去抓页面）。
- 调整 Gotenberg 参数用环境变量（如 `API_TIMEOUT`），**不要**用 `command:`：它会覆盖镜像
  默认的启动参数，容器直接起不来，bot 侧只会看到没有信息的 `request sending failed`。
- 镜像里已内置 `TZ=Asia/Shanghai`；调度器固定用 Asia/Shanghai，无 tzdata 时回退 +08:00。

启动：

```bash
export TELEGRAM_BOT_TOKEN=123456:ABC...
export MANAGER_ID=123456789
export WEBHOOK_TOKEN=$(openssl rand -hex 16)
docker compose up -d
docker compose logs -f magnet-bot
```

---

## 5. 方式二：docker run

```bash
docker network create magnet

docker run -d --name gotenberg --network magnet --restart unless-stopped \
  --shm-size=1gb -e API_TIMEOUT=120s gotenberg/gotenberg:8

docker run -d --name magnet-bot --network magnet --restart unless-stopped \
  -e TELEGRAM_BOT_TOKEN=123456:ABC... \
  -e SERVER_URL=https://example.com \
  -e CONFIG_PATH=/etc/magnet \
  -e PDF_SERVER_URL=http://gotenberg:3000 \
  -e WEBHOOK_SERVER_URL=http://magnet-bot \
  -e WEBHOOK_SERVER_PORT=8080 \
  -e WEBHOOK_TOKEN=$(openssl rand -hex 16) \
  -v "$PWD/data:/etc/magnet" \
  ghcr.io/gythialy/magnet-bot:latest
```

---

## 6. 方式三：二进制 + systemd

```bash
make build            # 产物 bin/magnet，纯静态（CGO_ENABLED=0）
sudo install -m 0755 bin/magnet /usr/local/bin/magnet
sudo useradd --system --home /var/lib/magnet --shell /usr/sbin/nologin magnet
sudo install -d -o magnet -g magnet /var/lib/magnet
```

`/etc/systemd/system/magnet-bot.service`：

```ini
[Unit]
Description=magnet-bot
After=network-online.target

[Service]
User=magnet
Environment=TELEGRAM_BOT_TOKEN=123456:ABC...
Environment=SERVER_URL=https://example.com
Environment=CONFIG_PATH=/var/lib/magnet
Environment=LOG_LEVEL=info
Environment=SCHEDULE_INTERVAL=1
Environment=PDF_SERVER_URL=http://127.0.0.1:3000
Environment=WEBHOOK_SERVER_URL=http://127.0.0.1
Environment=WEBHOOK_SERVER_PORT=8080
ExecStart=/usr/local/bin/magnet
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable --now magnet-bot
journalctl -u magnet-bot -f
```

> 转换功能在这里要求 Gotenberg 与 bot 同机或内网可达；`WEBHOOK_SERVER_URL` 用
> `http://127.0.0.1` 只在 Gotenberg 跑在同一台机器时成立。

---

## 7. 首次启动自检

1. 日志第一行是版本信息：

   ```
   magnet develop-38c373f @ 2026-10-30T05:00:00Z
   ```

2. 首次升级到「链接补 `https://`」版本时，会打印一次回填结果（正常现象，幂等）：

   ```
   {"level":"info","rows":12853,"message":"normalized scheme-less history urls"}
   ```

3. `CONFIG_PATH` 下出现 `bot.db` 与 `bot.log`；
4. 在 Telegram 里发 `/start` 能收到帮助；
5. 发 `/add_keywords 测试关键词` 能收到成功回复；
6. 转换功能：发 `/convertpdf <公告URL>`，应收到 PDF；失败时日志里会有
   `URLToPDF`/`WebhookHandler` 相关错误。

---

## 8. 公网接入（Webhook）

长轮询模式不需要任何公网入口。想让 Telegram 主动把更新推给 bot（以及后续的 Mini App）
时才需要本章：一个域名、一个 TLS 入口，bot 自己仍然只监听明文端口。

### 8.1 数据流

```text
Telegram 服务器 / Telegram 客户端
        │  POST https://bot.example.com/telegram/webhook
        │  GET  https://bot.example.com/app/、/api/*
        ▼
Cloudflare 边缘（公共 TLS 终止，443）
        │  cloudflared 隧道（本端主动外连，不开放入站端口）
        ▼
cloudflared（本地常驻进程）
        │  service: https://caddy:443（originRequest.noTLSVerify）
        ▼
Caddy（本地反代，TLS 终止点）
        │  reverse_proxy magnet-bot:8080（明文 HTTP）
        ▼
magnet-bot（单端口、明文、仅 Compose 内网）
```

bot 内的路由与来源：

| 路由 | 用途 | 从哪里进来 |
| --- | --- | --- |
| `POST /telegram/webhook` | Telegram 推送；先校验 secret header，不匹配直接 403 | Cloudflare → Caddy → bot |
| `GET /healthz` | 健康检查，始终注册，返回 `ok` | Cloudflare → Caddy → bot |
| `GET /app/`、`/api/*` | Mini App 页面与 API；由 `MINI_APP_ENABLED` 控制，P2 起提供 | Cloudflare → Caddy → bot |
| `POST /pdf/{id}` | Gotenberg 转换回执 | 内网直连，不经 Caddy / Cloudflare |

要点：

- **bot 不处理证书、不监听 TLS**：HTTPS 在 Cloudflare 边缘（或 Caddy）终止，到 bot 的
  永远是明文 HTTP，所以 bot 里没有任何证书文件或 ACME 逻辑。
- 公网只有一个入口端口（`WEBHOOK_SERVER_PORT`，如 `8080`）：webhook、健康检查、后续的
  Mini App 共用它。
- Gotenberg 回调仍走内网 `WEBHOOK_SERVER_URL:WEBHOOK_SERVER_PORT`，与公网入口互不影响。
- 已知限制：**单实例假设**。一个 bot 进程对应一条 webhook 注册；两个实例会各自
  `SetWebhook` 互相覆盖，同一条更新也可能被重复投递。当前不支持多副本，不要用横向扩容
  解决吞吐问题。

---

### 8.2 新增环境变量

| 变量 | 必需 | 默认 | 说明 |
| --- | --- | --- | --- |
| `PUBLIC_BASE_URL` | 启用公网时 ✅ | - | 对外入口，**必须 https 且自带 scheme**，如 `https://bot.example.com`；结尾 `/` 会被自动去掉。**留空 = 保持轮询模式**：不注册 webhook、没有公网入口，本地仍监听 `WEBHOOK_SERVER_PORT` 供 Gotenberg 回调与 `/healthz` 使用 |
| `TELEGRAM_WEBHOOK_SECRET` | 启用公网时 ✅ | - | 1–256 个 `[A-Za-z0-9_-]` 字符；Telegram 每次推送带 `X-Telegram-Bot-Api-Secret-Token`，不匹配直接 403 |
| `TELEGRAM_WEBHOOK_ENABLED` | - | `true` | 设为 `false` 时，即使配了 `PUBLIC_BASE_URL` 也继续用 `getUpdates` 轮询 |
| `MINI_APP_ENABLED` | - | `true` | 控制是否注册 `/app/`、`/api/`；设为 `false` 时旧文本命令保持可用（App 页面 P2 起提供） |
| `WEBHOOK_SERVER_PORT` | 启用公网时 ✅ | `0` | bot HTTP 监听端口（如 `8080`），**公网模式下必须非 0**；它同时是 Gotenberg 回调端口，不要再开第二个 |

派生路径（固定，不需要在反代里逐个配置）：

```
{PUBLIC_BASE_URL}/telegram/webhook    Telegram 推送
{PUBLIC_BASE_URL}/healthz             健康检查
{PUBLIC_BASE_URL}/app/                Mini App 页面（P2 起）
{PUBLIC_BASE_URL}/api/                Mini App API（P2 起）
```

> Telegram 侧限制：webhook URL 必须是 HTTPS，且端口只能是 **443 / 80 / 88 / 8443**。
> 所以 `PUBLIC_BASE_URL` 不要带自定义端口，端口由公网入口统一收口。

生成 secret（写进 `.env` 或 secret manager，不要提交仓库、不要贴进聊天记录）：

```bash
openssl rand -hex 32
```

启动校验（以 `pkg/config` 的 `Validate()` 为准）：

1. `PUBLIC_BASE_URL` 为空 → 直接通过（轮询模式），其余变量都不校验；
2. 非空时必须能解析成 URL、scheme 必须是 `https`、必须带 host，否则启动失败并打印原因；
3. `TELEGRAM_WEBHOOK_ENABLED=false` → 校验到此为止，不要求 secret 与端口；
4. 启用 webhook 时：`TELEGRAM_WEBHOOK_SECRET` 必填且匹配 `^[A-Za-z0-9_-]{1,256}$`；
   `WEBHOOK_SERVER_PORT` 必须非 0（不设等于随机端口，webhook 回调都不通）。

---

### 8.3 Caddy

`Caddyfile`：

```caddy
bot.example.com {
    encode zstd gzip
    reverse_proxy magnet-bot:8080
}
```

- **路径不需要在 Caddy 里单独配置**：`reverse_proxy` 会原样透传 URI，`/telegram/webhook`、
  `/healthz`、`/app/`、`/api/` 都由 bot 自己的 mux 注册，Caddy 只负责 TLS 与转发；
  只有当某个子路径要交给别的服务时，才需要 `handle` 按路径拆分。
- `magnet-bot:8080` 依赖同一 Compose 网络里的服务名解析；bot 与 Caddy 不在同一个
  compose 项目时，要显式接到同一个外部网络。
- 证书：公网侧由 Cloudflare 边缘提供，Caddy 与 cloudflared 之间用内部证书即可，所以 8.4 里
  要 `noTLSVerify: true`。若坚持让 Caddy 自己申请 Let's Encrypt 证书，必须保证 ACME 校验能
  到达 Caddy（HTTP-01 要在隧道里额外暴露 80 端口，或用 DNS-01），否则签发会一直失败。

Compose 片段（只列公网入口相关的服务，其余沿用 §4）：

```yaml
services:
  magnet-bot:
    environment:
      WEBHOOK_SERVER_PORT: "8080"
      PUBLIC_BASE_URL: "https://bot.example.com"
      TELEGRAM_WEBHOOK_SECRET: "${TELEGRAM_WEBHOOK_SECRET}"
      TELEGRAM_WEBHOOK_ENABLED: "true"
      MINI_APP_ENABLED: "true"
    # 不需要 ports: —— Caddy 在同一网络内直连 magnet-bot:8080

  caddy:
    image: caddy:2
    container_name: caddy
    restart: unless-stopped
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - ./caddy_data:/data

  cloudflared:
    image: cloudflare/cloudflared:latest
    container_name: cloudflared
    restart: unless-stopped
    command: tunnel --config /etc/cloudflared/config.yml run
    volumes:
      - ./cloudflared:/etc/cloudflared:ro
```

---

### 8.4 cloudflared

`./cloudflared/config.yml`：

```yaml
tunnel: <TUNNEL_UUID>
credentials-file: /etc/cloudflared/<TUNNEL_UUID>.json
ingress:
  - hostname: bot.example.com
    service: https://caddy:443
    originRequest:
      noTLSVerify: true
  - service: http_status:404
```

- `noTLSVerify: true` 是给 Caddy 的本地证书用的：Cloudflare 边缘到本机这一段不做证书校验，
  公网侧仍是有效的 Let's Encrypt 证书。
- 末尾的 `service: http_status:404` 是兜底规则，**必须保留**：cloudflared 要求 ingress
  覆盖所有 hostname，缺这条会拒绝启动。
- 想跳过 Caddy（让隧道直接对 bot 讲 HTTP）就把 `service` 改成 `http://magnet-bot:8080`，
  这时不需要 `noTLSVerify`，链路也少一跳。
- cloudflared 是**主动外连**，不需要在路由器上做端口映射，也不需要公网 IP。

---

### 8.5 启动顺序与失败行为

1. 读配置并 `Validate()`：不通过 → 打印明确错误并退出（规则见 8.2）；
2. 绑定 `:{WEBHOOK_SERVER_PORT}`：失败（端口被占、地址非法）→ **直接退出**，不会悄悄降级成
   轮询。理由：Caddy 已经在往这个端口转发，半启动的进程只会让更新静默丢失；
3. 后台开始 `Serve` 处理请求，`/healthz` 立即可用；
4. 调 `SetWebhook` 注册 `{PUBLIC_BASE_URL}/telegram/webhook`：失败**不阻塞启动**，后台
   每 **30 秒**重试一次并记日志（幂等，可以安全重试）；
5. 收到 SIGTERM 正常退出：**不删除 webhook**。更新在 Telegram 侧最多保留 24 小时，
   重启后继续投递，不需要人工重新注册；
6. 反之，轮询模式的启动流程是：先 `DeleteWebhook` 清掉残留，再 `getUpdates`——残留的
   webhook 会让 `getUpdates` 一直收不到更新（报告 409）。

> `SetWebhook` 失败期间 bot 本身是健康的，`/healthz` 照样返回 `ok`，但 Telegram 不会推送。
> 以 `getWebhookInfo` 的 `url` 是否为空为准，不要只看容器状态或健康检查。

---

### 8.6 自检

```bash
# 1) 健康检查：期望输出 ok
curl -sS "${PUBLIC_BASE_URL}/healthz"; echo

# 2) Telegram 侧注册状态
curl -sS "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getWebhookInfo" \
  | jq '{url, last_error_message, pending_update_count}'
```

判定标准：

- `url` 等于 `{PUBLIC_BASE_URL}/telegram/webhook`；
- `last_error_message` 缺失或为空字符串；
- `pending_update_count` 归零（刚重启时会短暂非 0，稳定后必须为 0）。

若公网 `/healthz` 不通，先确认 bot 自己在监听：临时给 bot 加 `ports: ["8080:8080"]`，
`curl -sS http://127.0.0.1:8080/healthz`，排完再删掉这段映射。本机通、公网不通，问题就在
Caddy / cloudflared / Cloudflare 这条链路上。

---

### 8.7 Cloudflare 注意事项

- **关闭该 hostname 的 Bot Fight Mode**，或对 `/telegram/webhook` 加 WAF **Skip** 规则；
  否则 Telegram 的 POST 可能被 challenge/拦截，表现为 `getWebhookInfo` 里
  `last_error_message` 是 `403` 或错误码 `1010`。
- 用 IP 规则放行时可加 Telegram 来源网段 `149.154.160.0/20`、`91.108.4.0/22`；注意跳过
  规则要覆盖这些请求，否则放行之后仍可能被托管规则挑战。
- **不要**对 `/telegram/webhook` 开启缓存、请求体改写、Header 改写或任何 Transform Rule：
  secret header 与 body 必须原样到达 bot。
- Telegram 要求的 webhook 端口只有 443 / 80 / 88 / 8443，这也正好是 Cloudflare 代理支持的
  端口，所以 `PUBLIC_BASE_URL` 不要带自定义端口。
- Telegram 与 bot 出网是两条独立链路：入口通了但 bot 出不了网，一样收不到更新。

---

### 8.8 回滚

```bash
# 1) 清空 PUBLIC_BASE_URL（.env 里注释掉或留空）
# 2) 重启 bot
docker compose up -d magnet-bot
docker compose logs -f magnet-bot | grep -iE "webhook|polling"
```

- 启动时检测到 `PUBLIC_BASE_URL` 为空 → 回到轮询；启动过程会先 `DeleteWebhook` 清掉残留，
  **不需要手工调 Bot API**。
- 想彻底下线公网入口：停掉 cloudflared（或在 Cloudflare 面板停用该 hostname 的路由），
  Caddy 站点留着也无害。
- `WEBHOOK_SERVER_PORT` **不要一起清掉**：Gotenberg 回调还在用它。
- 回滚不需要动 `bot.db`，也不影响已推送历史。

---

### 8.9 排障：常见 `last_error_message`

| 现象 | 含义 | 处理 |
| --- | --- | --- |
| 错误码 `1010`，或 403 且响应体是 Cloudflare 错误页 | Cloudflare 拦截（Bot Fight Mode / WAF / 托管规则） | 加 WAF Skip 规则或关闭 Bot Fight Mode（见 8.7） |
| `403` 且响应体很干净、没有 Cloudflare 字样 | bot 自己的 secret 校验没过 | 改过 `TELEGRAM_WEBHOOK_SECRET` 后要重启，让 `SetWebhook` 带上新值；确认中间层没改写 header |
| `404` | 路径或反代配置错 | 核对 URL 是否为 `/telegram/webhook`、Caddy 是否 `reverse_proxy magnet-bot:8080`、`WEBHOOK_SERVER_PORT` 与容器实际监听端口是否一致 |
| `502` / `521` / `522` | Cloudflare 到源站不通 | cloudflared 是否在线、Caddy 是否在跑、`service:` 是否指向正确的容器与协议 |
| `409` 或 `terminated by other getUpdates request` | 同一 token 有两个消费者 | 只保留一个 bot 实例；确认旧容器已停、没有残留 webhook 同时推送 |
| `url` 为空、`pending_update_count` 一直是 0 | `SetWebhook` 一直失败 | 看 bot 日志里的重试记录；多为公网不可达（先按 8.6 自检）或 token 错误 |

---

## 9. 数据、日志与备份

| 文件 | 说明 |
| --- | --- |
| `bot.db` | SQLite，含 `keywords`、`histories`、`alarms` 三张表；启动时自动建表/迁移 |
| `bot.log` | zerolog 滚动日志：单文件 10MB，保留 10 个备份、7 天 |

备份（停机或在线均可，SQLite 单文件复制建议先 `sqlite3 bot.db '.backup ...'`）：

```bash
sqlite3 data/bot.db ".backup 'backup-$(date +%F).db'"
```

`histories` 是去重依据，**不要手工清空**，否则会重复推送历史公告。

---

## 10. 升级与回滚

```bash
# 升级（镜像方式）
docker compose pull && docker compose up -d

# 回滚
docker compose down
docker run ... ghcr.io/gythialy/magnet-bot:<上一个版本>
```

- 数据库结构变更由 `AutoMigrate` 在启动时完成，向后兼容；升级前建议先备份 `bot.db`。
- 升级不会重复推送：`histories` 的 URL 在启动时会被规范化为绝对地址（幂等）。

---

## 11. 常见问题

**Q：`/convertpdf` 一直收不到文件**

按顺序排查：

1. `WEBHOOK_SERVER_PORT` 是否显式设置（不设 = 0 = 随机端口，必失败）；
2. Gotenberg 能否访问 `WEBHOOK_SERVER_URL:WEBHOOK_SERVER_PORT`（容器内用服务名）；
3. 设了 `WEBHOOK_TOKEN` 时，回调 URL 必须带 `?token=...`（bot 自动拼，手工测试要自己加）；
4. 请求记录 10 分钟后过期，超过后回调会返回 404。

**Q：Gotenberg 转换超时 / 502**

- 用环境变量 `API_TIMEOUT`（默认 30s，建议 120s）调整转换时限。写成
  `command: ["--api-timeout=120s"]` 会让容器起不来，现象是 bot 只报
  `request sending failed`；
- bot 侧 HTTP 超时是 3 分钟，**必须大于** `API_TIMEOUT`：两者相同时客户端先失败，
  Gotenberg 返回的 504 和错误体就看不到了；
- `shm_size` 至少 1GB，否则 Chromium 容易崩；
- 确认 Gotenberg 容器本身能出网访问公告站点。

**Q：复杂表格公告是怎么发的？**

- 只发一条消息：整页 PDF + 正文作为 caption，不再单独发一条附件消息；
- Telegram 单条消息的 caption 上限是 1024 个 UTF-16 字符（普通正文消息是 4096），
  所以正文超过 1000 字时会截断并以「…」结尾——完整内容本来就在 PDF 里；
- 判定复杂表格的阈值：**单个表格列数 ≥ 6 或行数 ≥ 10**；被判定后正文里会出现
  「（表格较大，已省略；完整内容见附件 PDF）」；
- 转换/发送失败时退回「正文分条送达 + 后台补发附件」：失败记录写入 `pending_attachments`
  表，按 1m / 5m / 15m / 30m / 1h / 2h 退避补发，成功看 `resent notice pdf`，
  放弃（6 次或 24 小时）看 `giving up on notice pdf`；
- 同一用户同一公告只排一条，多实例共享 `bot.db` 时用 `locked_until` 租约互斥；
- `PDF_SERVER_URL` 为空时 bot 启动即失败（Gotenberg 是必填依赖）。

**Q：重复推送了历史公告**

- 检查 `bot.db` 是否被替换/清空；`histories` 表是去重依据；
- 启动日志里的 `normalized scheme-less history urls` 若每次都打印很多行，说明有人改写过 URL 字段。

**Q：容器启动即退出，日志报 `NewBotContext`**

- 多为 `TELEGRAM_BOT_TOKEN` 无效、`CONFIG_PATH` 不可写（uid 65532）、或
  `PDF_SERVER_URL` 格式非法（空值可以，非法 URL 会直接报错）。
- 公网配置错误也在这里退出：`PUBLIC_BASE_URL` 不是 https，或启用了 webhook 却缺
  `TELEGRAM_WEBHOOK_SECRET` / `WEBHOOK_SERVER_PORT=0`（校验规则见 §8.2）。

**Q：健康检查端点是什么**

`GET /healthz` 始终注册，返回 `200` 与纯文本 `ok`（无鉴权），可以直接用作 HTTP
healthcheck；`/healthz` 上的非 GET 返回 405。未启用的能力（例如 Mini App 的 `/app/`、
`/api/`，P2 之前）不注册路由，访问得到 404。

---

## 12. 运维命令

| 命令 | 说明 |
| --- | --- |
| `/start` | 帮助 |
| `/add_keywords <关键词>` | 新增关键词（`+` 前缀表示必须包含） |
| `/edit_keywords <id> <关键词>` | 修改 |
| `/delete_keywords <id>...` | 删除（仅限本人） |
| `/search_history_title <关键词>` | 搜索历史推送 |
| `/list_today` | 今日公告 |
| `/add_alarm_keywords` / `/alarm` / `/search_alarm_records` | 告警关键词与记录 |
| `/convertpdf <URL>` / `/convertimg <URL>` | 转 PDF / 截图（需 Gotenberg） |
| `/statistics` | 运行统计 |
| `/retry` / `/clean` | 管理员（`MANAGER_ID`）重推失败记录 / 清理 |

---

## 13. 从源码构建镜像

```bash
docker build -t magnet-bot:local .
```

- 构建阶段用 `golang:1.27`，运行阶段是 `distroless/static-debian12:nonroot`（无 shell，
  排障请用日志或 `docker cp` 取 `bot.log`）；
- 镜像声明了 `VOLUME /etc/magnet`，所以 `CONFIG_PATH` 建议就用它。
