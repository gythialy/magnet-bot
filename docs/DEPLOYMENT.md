# magnet-bot 部署手册

Telegram 公告订阅机器人：定时抓取采购公告 → 按用户关键词匹配 → 推送消息；可选接入
Gotenberg 把公告转成 PDF / PNG。

- 机器人通过 **Telegram 长轮询**工作，自身只需出网访问 Telegram 与公告站点。
- 只有 PDF/PNG 转换功能需要 **Gotenberg 能访问到 bot 的 webhook 端口**（同一 Docker
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
2. 命中即推送 Telegram HTML 消息；若公告含**复杂表格**（≥ 6 列或 ≥ 10 行），正文用
   占位符替代表格，并把整页转成 PDF 作为**回复附件**发送；
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
    command: ["--api-timeout=120s"]
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
  --shm-size=1gb gotenberg/gotenberg:8 --api-timeout=120s

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

## 8. 数据、日志与备份

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

## 9. 升级与回滚

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

## 10. 常见问题

**Q：`/convertpdf` 一直收不到文件**

按顺序排查：

1. `WEBHOOK_SERVER_PORT` 是否显式设置（不设 = 0 = 随机端口，必失败）；
2. Gotenberg 能否访问 `WEBHOOK_SERVER_URL:WEBHOOK_SERVER_PORT`（容器内用服务名）；
3. 设了 `WEBHOOK_TOKEN` 时，回调 URL 必须带 `?token=...`（bot 自动拼，手工测试要自己加）；
4. 请求记录 10 分钟后过期，超过后回调会返回 404。

**Q：Gotenberg 转换超时 / 502**

- 页面很长时把 `--api-timeout` 调到 120s（bot 侧 HTTP 超时是 2 分钟）；
- `shm_size` 至少 1GB，否则 Chromium 容易崩；
- 确认 Gotenberg 容器本身能出网访问公告站点。

**Q：复杂表格公告没有收到附件**

- 正文出现「（表格较大，已省略；完整内容见附件 PDF）」表示识别为复杂表格；
- 附件发送失败只记日志、不回滚推送（正文已送达），检查 `send project pdf` 日志；
- 未配置 `PDF_SERVER_URL` 时该功能不可用。

**Q：重复推送了历史公告**

- 检查 `bot.db` 是否被替换/清空；`histories` 表是去重依据；
- 启动日志里的 `normalized scheme-less history urls` 若每次都打印很多行，说明有人改写过 URL 字段。

**Q：容器启动即退出，日志报 `NewBotContext`**

- 多为 `TELEGRAM_BOT_TOKEN` 无效、`CONFIG_PATH` 不可写（uid 65532）、或
  `PDF_SERVER_URL` 格式非法（空值可以，非法 URL 会直接报错）。

**Q：没有健康检查端点**

bot 只暴露 `POST /pdf/{id}` 一个路由，GET 返回 405/404。编排层请用进程存活
（`restart: unless-stopped`）或日志判活，不要配 HTTP healthcheck。

---

## 11. 运维命令

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

## 12. 从源码构建镜像

```bash
docker build -t magnet-bot:local .
```

- 构建阶段用 `golang:1.27`，运行阶段是 `distroless/static-debian12:nonroot`（无 shell，
  排障请用日志或 `docker cp` 取 `bot.log`）；
- 镜像声明了 `VOLUME /etc/magnet`，所以 `CONFIG_PATH` 建议就用它。
