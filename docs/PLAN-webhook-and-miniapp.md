# magnet-bot 功能规划：公网 Webhook + Mini App + 转换入口

> 状态：P1–P4 已完成（统一 HTTP server + webhook、Mini App 读写与转换入口、命令退役）；
> P5 后端已完成（`users`/`settings` 表、三处准入、管理 API），`/settings` 命令与 Settings 页
> 在 `feat/p5-access-control` 上实施中
> 日期：2026-10-30
> 基线：`main` @ `2a205a4`

---

## 1. 决策记录

| # | 决策 | 结论 |
|---|---|---|
| 1 | 公网入口 | Cloudflare Tunnel → 本地 Caddy（Let's Encrypt）→ bot 单个 HTTP 端口；**bot 不做 TLS**，不处理证书 |
| 2 | 域名 | 作为配置项 `PUBLIC_BASE_URL`，所有公网路径从它派生 |
| 3 | 接收更新 | 轮询 `getUpdates` → **Webhook（HTTPS 推送 + 回调）** |
| 4 | 管理界面 | 关键词 / 告警词的增删改查全部迁到 **Mini App** |
| 5 | 统计 | `/statistics` 退役，统计合并进 App 的 **Today 页顶部** |
| 6 | 列表 | `/list_today`、`/search_history_title` 退役，由 App 的 Today 页（日期范围 + 搜索）取代 |
| 7 | App 启用策略 | 上述命令在 **App 启用时**退役为「提示 + 打开 App 按钮」；App 未启用时保留旧功能（降级） |
| 8 | 转换入口 | **只在 Mini App**：Today 页行内 `📄/🖼` + 详情弹层；聊天列表与推送消息都不加转换按钮 |
| 9 | 推送公告 | **与现状保持一致**，不追加任何按钮 |
| 10 | 转换结果 | **发回聊天**（复用 Gotenberg + webhook 管线），App 内只做状态提示 |
| 11 | 转换命令 | `/convertpdf`、`/convertimg` 在 App 启用时退役（提示 + 按钮），未启用时保留 |
| 12 | App 列表加载 | **Load more 增量加载**，不做页码分页 |
| 13 | App 文案 | 英文，与 bot 现有文案一致 |
| 14 | 告警记录 | `/search_alarm_records` 也迁入 App 的 Alarms 页 |

---

## 2. 目标架构

```
Telegram 服务器 ──POST {PUBLIC_BASE_URL}/telegram/webhook──┐
Telegram 客户端 ──GET  {PUBLIC_BASE_URL}/app/、/api/*──────┤
                                                           ▼
                                             Cloudflare 边缘 (TLS 443)
                                                           │ cloudflared tunnel
                                                           ▼
                                             cloudflared (本地常驻，主动外连)
                                                           │ https://caddy:443 (noTLSVerify)
                                                           ▼
                                             Caddy (本地反代, Let's Encrypt)
                                                           │ http://magnet-bot:{PORT}
                                                           ▼
                        ┌──────────── magnet-bot (单端口, 明文, 仅内网) ─────────────┐
                        │ POST /telegram/webhook   Telegram 推送                    │
                        │ GET  /app/*              Mini App 静态页                  │
                        │ /api/*                   Mini App JSON API                │
                        │ GET  /healthz            健康检查                          │
                        └───────────────────────────────────────────────────────────┘
                                          ▲
                                          │ POST /pdf/{id}   内网直连，不经 Caddy / Cloudflare
                                          │
                                     Gotenberg
```

注意：`PUBLIC_BASE_URL` 本身已带 scheme（如 `https://t.example.com`），派生路径直接拼接，不要重复写 `https://`；Gotenberg 回调仍走 `WEBHOOK_SERVER_URL:WEBHOOK_SERVER_PORT` 的内网地址，与公网入口无关。

Telegram 侧要求：webhook URL 必须 HTTPS，端口只能是 **443 / 80 / 88 / 8443**；非 2xx 会重试；webhook 与 `getUpdates` 互斥。

---

## 3. 配置

| 变量 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `PUBLIC_BASE_URL` | 启用公网能力时 | 空 | 如 `https://bot.example.com`。**留空 = 完全旧模式**（轮询 + 文本报表） |
| `TELEGRAM_WEBHOOK_SECRET` | 启用 webhook 时 | 空 | 1–256 字符 `[A-Za-z0-9_-]`，Telegram 每次推送带 `X-Telegram-Bot-Api-Secret-Token` |
| `WEBHOOK_SERVER_PORT` | 启用公网能力时 | `0` | bot HTTP 监听端口，**必须显式设置**（如 `8080`） |
| `TELEGRAM_WEBHOOK_ENABLED` | 否 | `true` | 关闭时即使有 `PUBLIC_BASE_URL` 也继续轮询 |
| `MINI_APP_ENABLED` | 否 | `true` | 关闭时不提供 `/app`、`/api`，旧命令保持可用 |

派生路径（固定）：

```
{PUBLIC_BASE_URL}/telegram/webhook   Telegram 推送
{PUBLIC_BASE_URL}/app/               Mini App 页面
{PUBLIC_BASE_URL}/api/               Mini App API
{PUBLIC_BASE_URL}/healthz            健康检查
```

Gotenberg 回调不走公网：仍是 `{WEBHOOK_SERVER_URL}:{WEBHOOK_SERVER_PORT}/pdf/{id}` 的内网直连。

启动校验：`PUBLIC_BASE_URL` 必须是 `https://`；启用 webhook 时 secret 必填、端口非 0；否则启动失败并给出明确错误。

---

## 4. 功能模块

### 4.1 公网接入与 Webhook（P1）

启动顺序：

1. 建 mux，`net.Listen(":{WEBHOOK_SERVER_PORT}")`，**绑定失败直接退出**
2. `go server.Serve(ln)`
3. `SetWebhook{URL, SecretToken, AllowedUpdates: [message, callback_query, edited_message], MaxConnections: 40}`
4. `go b.StartWebhook(ctx)`（只启动消费者，不再 getUpdates）
5. `SetWebhook` 失败 → 后台每 30s 重试并记日志（幂等），不阻塞启动

其他行为：

- webhook 处理器先校验 secret header（不匹配 403），再交给 `b.WebhookHandler()`；body 上限 1 MB
- 关闭时**不删除 webhook**：Telegram 最多保留 24h 更新，重启后继续投递
- 未启用 webhook（轮询模式）时，启动先 `DeleteWebhook`，避免残留 webhook 导致收不到更新
- `GET /healthz` 返回 200，供 Caddy/Cloudflare 探活

### 4.2 Mini App（P3/P4）

单页应用，静态资源 `go:embed` 进二进制（无 Node 构建步骤），与 API 同域部署。

**底部导航 3 个页签**：

#### Today（默认落地页，统计合并到这里）

- 顶部统计条：`History` / `Keywords` / `Related notices` / `Alarm keywords` 四个数字
- 搜索框（标题 / 项目编号）
- 日期范围 chips：`Today` / `7 days` / `All`
- 公告卡片：标题（两行截断）、`项目编号 · 时间`、🔥 标记；右侧动作 `📄 PDF` / `🖼 PNG` / `↗ Open`
- 点卡片 → 详情弹层：标题、编号时间、`Open notice page` / `PDF` / `PNG`
- 底部 `Load more` 增量加载
- 转换触发后：Toast `Converting PDF…` → `PDF sent to chat ✓`（文件由 bot 发回聊天）

#### Keywords

- 搜索、`+ Add`（逗号分隔批量添加，重复自动跳过并回报数量）
- 行：`#id`、关键词、关联公告数、`✏️` / `🗑`
- 编辑弹层、删除二次确认

#### Alarms

- `+ Add alarm keyword`（批量添加）
- 告警关键词行：`#id`、关键词、创建日期、`✏️` / `🗑`

#### Records

- 只读告警记录列表：信用主体、`业务编号 · 开始日期`
- 搜索（信用主体模糊匹配）+ `Load more` 游标分页
- 点整行 → 应用内详情弹层：公告标题、信用代码、起止时间、处罚部门/单位、具体情形、处罚结果、公告链接
- 弹层底部 `Open in chat` 用 `openTelegramLink` 回到对话（不再跳浏览器）
- 取代 `/search_alarm_records` 与 `/alarm <id>`

#### Settings（管理员）

- 顶部 segmented 切换两个 pane：
  - **People**：准入模式开关、人员 CRUD、最近被拒
  - **About**：版本/构建时间、webhook 状态、计数

**页面语言**：英文（与 bot 一致）；关键词内容原样展示。
**主题**：使用 Telegram `themeParams`，监听 `themeChanged`；非 Telegram 环境打开时提示从 bot 进入。

### 4.3 转换入口（P3）

转换只在 Mini App 内发起；聊天侧不新增按钮，推送公告保持现状。

| 入口 | 交互 |
|---|---|
| Today 列表行 | `📄 PDF` / `🖼 PNG` / `↗ Open` 三个动作 |
| 公告详情弹层 | `Open notice page` / `PDF` / `PNG` |

流程：

1. App 调 `POST /api/convert`（`{url, format}`，带 initData）
2. 服务端校验：URL 属于该用户的历史记录、且同 `SERVER_URL` 域
3. 异步调用 Gotenberg，结果由 bot 发回聊天：删除「Converting…」占位 → 发送文件（PDF/PNG），caption 为公告 URL
4. App 内 Toast：`Converting PDF…` → `PDF sent to chat ✓`

规则：

- 每用户限流（建议 5 次/分钟），超限返回 429 并提示
- 同一 URL + 格式在转换中忽略重复请求
- Gotenberg 异常：返回错误并在聊天里保留占位提示，不静默失败
- 未启用 App 的部署：`/convertpdf`、`/convertimg` 命令保持可用

### 4.4 命令迁移（P5）

| 命令 | App 启用时 | App 未启用时 |
|---|---|---|
| `/statistics` | 提示 + `📊 Open statistics` 按钮 | 现有文本报表 |
| `/add_keywords` | 提示 + 打开 App 按钮 | 保留 |
| `/edit_keywords` | 同上 | 保留 |
| `/delete_keywords` | 同上 | 保留 |
| `/add_alarm_keywords` | 同上 | 保留 |
| `/list_today` | 同上 | 保留 |
| `/search_history_title` | 同上 | 保留 |
| `/search_alarm_records` | 同上（进 App 的 Records） | 保留 |
| `/alarm <id>` | 提示 + 打开 App 按钮（详情在 App 内展开） | 保留 |
| `/convertpdf`、`/convertimg` | 提示 + 打开 App 按钮 | 保留 |
| `/me`、`/start`、`/retry`、`/clean` | 保留 | 保留 |

配套调整：

- `SetMyCommands` 移除退役命令
- 菜单按钮（`setChatMenuButton`）指向 Mini App
- `/start` 帮助文案更新（去掉退役命令，加「打开 App」）

---

## 5. API 设计

所有 `/api/*` 请求必须携带 `X-Telegram-Init-Data: <raw initData>`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/statistics` | 计数 + Top 关键词 |
| GET | `/api/notices?range=today\|7d\|all&q=&offset=&limit=` | Today 列表（含搜索/增量加载） |
| POST | `/api/convert` | `{"url":"...","format":"pdf\|png"}`，异步转换并发回聊天 |
| GET | `/api/keywords` | 关键词列表 |
| POST | `/api/keywords` | `{"keywords":"a,b,c"}` 批量添加 |
| PATCH | `/api/keywords/{id}` | `{"keyword":"..."}` |
| DELETE | `/api/keywords/{id}` | 删除 |
| GET | `/api/alarms` | 告警关键词列表 |
| POST | `/api/alarms` | 批量添加 |
| PATCH | `/api/alarms/{id}` | 重命名 |
| DELETE | `/api/alarms/{id}` | 删除 |
| GET | `/api/alarm-records?q=&offset=&limit=` | 告警记录 |

鉴权算法（官方）：

```
secret = HMAC_SHA256("WebAppData", bot_token)
hash  == HMAC_SHA256(data_check_string, secret)
data_check_string = 除 hash 外所有字段按 key 排序后的 "k=v"，用 \n 连接
```

校验通过后要求 `auth_date` 在 1 小时内；用户身份只取校验后的 `user.id`（私聊下等于 chat id），所有读写按该用户过滤。

响应约定：成功 200/204；鉴权失败 401；越权/不存在 403/404；参数错误 400；转换中 202。

---

## 6. 安全

| 面 | 措施 |
|---|---|
| Webhook | secret header 双校验、body 1 MB 上限、只处理三种 update、仅监听内网端口 |
| App API | initData HMAC + `auth_date` 时效、同源部署（无 CORS）、按 `user.id` 隔离数据 |
| 转换 | 同域白名单、服务端签发 token（客户端无法指定任意 URL）、每用户限流 |
| 公网暴露 | 只暴露 Caddy 的 443；bot 端口不直接映射公网 |
| 日志 | 鉴权失败、webhook 错误、转换失败均记录，不记录 initData 原文 |

---

## 7. 数据模型

**无表结构变更**：

- 关键词与告警词共用 `keywords` 表，`type` 区分（`PROJECT` / `ALARM`）
- 公告列表来自 `histories`（`user_id`、`url`、`title`、`updated_at`、`has_tender_code`）
- 告警记录来自 `alarms`

复用现有 DAL：`Keyword.Insert` / `EditById` / `DeleteByIds` / `GetByUserIdAndType`，`History.GetTodayByUserId` / `SearchByTitle`，`Alarm.SearchByName`。

---

## 8. 实施阶段与 PR

| 阶段 | PR | 内容 | 依赖 | 规模（估） |
|---|---|---|---|---|
| P1 | PR-1 | HTTP server 抽取、webhook 模式、配置、`/healthz`、部署文档 | 无（默认关闭） | ~350 行 |
| P2 | PR-2 | Mini App 只读：embed、initData 鉴权、`/api/statistics`、`/api/notices`、Today（含统计条）/Keywords/Alarms 列表 | P1 | ~600 行 |
| P3 | PR-3 | Mini App 写操作：关键词/告警词 CRUD、`/api/convert` | P2 | ~450 行 |
| P4 | PR-4 | 命令退役、菜单按钮、帮助文案、`SetMyCommands` | P3 | ~150 行 |

每阶段独立可回滚：P1 由 `PUBLIC_BASE_URL` 控制；P2–P4 由 `MINI_APP_ENABLED` 控制。

---

## 9. 验收标准

**P1**

- `getWebhookInfo` 的 `url` 正确、`last_error_message` 为空、`pending_update_count` 归零
- 命令与回调全部正常；断网恢复后更新续投
- 清空 `PUBLIC_BASE_URL` 重启 → 回到轮询，且无残留 webhook

**P2**

- App 能打开 Today / Keywords / Alarms 三个页签；Today 顶部统计与 bot 数据一致
- Today 页搜索、日期范围、Load more 正常
- 未登录 / initData 过期 → 401；跨用户访问 → 403/404

**P3**

- App 内完成关键词与告警词的增删改，刷新后 bot 侧数据同步
- 在 App 内转换：聊天里出现对应 PDF/PNG 文件，App 内状态提示正确
- 非本人 URL、非白名单 URL、超限请求均被拒绝

**P4**

- 退役命令返回提示 + 打开 App 按钮
- `MINI_APP_ENABLED=false` 时旧命令行为与当前版本一致

---

## 10. 部署（Cloudflare Tunnel + Caddy）

Caddy：

```caddy
bot.example.com {
    encode zstd gzip
    reverse_proxy magnet-bot:8080
}
```

> 路径不需要在 Caddy 里单独配置：`reverse_proxy` 会原样透传 URI，`/telegram/webhook` 由 bot 的 HTTP server 自己注册，Caddy 只负责 TLS 与转发。只有当 App 静态资源要交给别的服务时，才需要 `handle` 按路径拆分。

cloudflared：

```yaml
tunnel: <uuid>
credentials-file: /etc/cloudflared/<uuid>.json
ingress:
  - hostname: bot.example.com
    service: https://caddy:443
    originRequest:
      noTLSVerify: true
  - service: http_status:404
```

环境变量：

```bash
WEBHOOK_SERVER_PORT=8080
PUBLIC_BASE_URL=https://bot.example.com
TELEGRAM_WEBHOOK_SECRET=$(openssl rand -hex 32)
```

验证：

```bash
curl -s "https://api.telegram.org/bot${TOKEN}/getWebhookInfo" | jq .
```

Cloudflare 注意：

- 关闭该 hostname 的 **Bot Fight Mode**，或对 `/telegram/webhook` 加 WAF 跳过规则；否则 Telegram 的 POST 可能被 challenge（表现为 `last_error_message` 403/1010）
- Telegram 来源 IP 段可用于放行：`149.154.160.0/20`、`91.108.4.0/22`
- 不要开启对 `/telegram/webhook` 的缓存或请求体改写

回滚：清空 `PUBLIC_BASE_URL` 重启即可回到轮询模式。

---

## 11. 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 公网入口故障 | 更新最多丢失 24h 后重投 | Cloudflare Tunnel + 健康检查；保留轮询回退开关 |
| Cloudflare challenge | webhook 收不到更新 | WAF 跳过规则 / 关闭 Bot Fight Mode；`getWebhookInfo` 监控 |
| 多副本部署 | 同一更新重复投递 | 当前单实例；未来多副本需共享队列 |
| App 写接口被滥用 | 数据被改 | initData 校验 + 时效 + 按用户隔离 + 限流 |
| 转换被刷 | Gotenberg 压力 | 每用户限流、同 URL+格式去重、URL 归属校验 |
| Bot API 10.2 域名限制 | App 方法失效 | 页面与 API 同域，不引外部 CDN |

---

## 12. 未决项

1. `PUBLIC_BASE_URL` 的实际域名（文档中先用 `bot.example.com` 占位）
2. 转换限流阈值（建议 5 次/分钟/用户）
3. App 是否需要「批量删除」与「新增关键词时按告警/项目区分」的快捷入口（可后续迭代）

---

## 13. 访问控制与管理员设置台

### 13.1 数据模型

`users`（主键即 Telegram id，不做软删——删掉一行必须立刻停止匹配）

| 列 | 类型 | 说明 |
|---|---|---|
| `user_id` | INTEGER PK | Telegram 用户 ID |
| `remark` | TEXT | 管理员备注，不参与鉴权 |
| `role` | INTEGER | `0` 普通用户 / `1` 管理员（线上 JSON 用 `"user"` / `"admin"`） |
| `enabled` | BOOL | `restricted` 模式下的开关；停用保留角色与备注 |
| `from_env` | BOOL | 由 `MANAGER_ID` / `ALLOWED_USER_IDS` 同步的行，只允许改备注 |
| `created_at` / `updated_at` | DATETIME | 管理编辑时间；`last_seen` 不写 `updated_at` |
| `last_seen` | DATETIME NULL | 最近活动，最多每分钟一次 |

`settings`（键值表，新增设置不需要迁移）

| 列 | 说明 |
|---|---|
| `key` | 主键，目前只有 `access_mode` |
| `value` | `off` / `restricted` |
| `updated_at` | 最近修改时间 |

引导（`dal.EnsureBootstrap`，启动时、开始服务前）：先把环境变量同步进 `users`，再决定模式——
`settings.access_mode` 已有非空值就沿用（重启不覆盖页面上的选择），否则「有管理员 → 走
`restricted`」「没有 → `off` + WARN」。授权缓存 30s TTL，所有写操作立即 `Invalidate`。

### 13.2 准入三执行点

| 执行点 | 实现 | 行为 |
|---|---|---|
| bot 更新 | `accessMiddleware`（`bot.WithMiddlewares`，包住默认 handler） | 只服务私聊；未授权消息 10 分钟最多提示一次，回调立即 `AnswerCallbackQuery`；群/频道更新直接丢弃 |
| Mini App `/api/*` | `requireAllowed`（套在 `NewInitDataMiddleware` 之后、整个 mux 外面） | 403 `forbidden`；`/api/admin/*` 再叠 `requireAdmin` → 403 `forbidden_admin` |
| 后台推送 / 补发 | `InfoProcessor.config` / `Get`、`AttachmentRetrier` | 跳过未授权账号；关键词、待补发附件保留，复权后下一轮续发 |

判定的唯一入口是 `dal.User.IsAllowed` / `IsAdmin`；`IsAdmin` 与访问模式无关（`off` 不等于人人
管理员）。数据层失败方向收口：设置读失败按 `restricted`、未知模式值只放行管理员。

### 13.3 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/me` | 身份与当前模式：`userId`、`role`、`accessMode`、`remark` |
| GET | `/api/admin/users?q=&offset=&limit=` | 名单；`q` 匹配备注模糊或 user id 精确；默认 20、上限 50 |
| POST | `/api/admin/users` | 批量添加/更新 `{"userIds":"10001,10002","role":"user","remark":""}`；单批上限 50，无法解析的条目走 `ignored` |
| PATCH | `/api/admin/users/{id}` | `{"remark":?,"role":?,"enabled":?}`（指针字段，缺省=不改） |
| DELETE | `/api/admin/users/{id}` | 撤销访问，204 |
| GET / PATCH | `/api/admin/settings` | 读/写 `{"accessMode":"off"}` 或 `{"accessMode":"restricted"}` |
| GET | `/api/admin/status` | 版本、计数（users/admins/keywords/histories/alarms）、webhook 状态、最近被拒（内存，最多 50 条） |

写操作与关键词/告警词共用 **30 次/分钟/用户** 的滑动窗口预算（429 `too_many_requests`）。
护栏拒绝统一 409 `{"error":{"code":"guard","message":"..."}}`。

### 13.4 与 MANAGER_ID 的合并

- 管理员**只**由 `users` 表决定；`MANAGER_ID` 是启动时的种子，解析为逗号分隔列表
  （`config.ManagerIDs`，新增），旧单值 `MANAGER_ID=10001` 语义不变；`config.ManagerId`
  作为兼容访问器保留第一个值，旧调用点无需改动。
- **没有** `ADMIN_USER_IDS`。`ALLOWED_USER_IDS` 是可选的白名单种子，只在启动时同步。
- 页面授予的管理员与环境变量同步的管理员在鉴权上完全等价，区别只是 `from_env=1` 的行不能
  在页面改角色/状态或删除（下次启动会被环境变量改回去），只能改备注。
- 防锁死：环境变量列表删掉某人时，若他是最后一个启用中的管理员则保留角色并 WARN；页面侧
  另有「不能动自己」「不能删/停用/降级最后一个管理员」两条护栏。

### 13.5 Settings 页结构

四张卡片 + 页签可见性：

- **Access control**：当前模式（`off` / `restricted`）切换 + 人数概览
- **People**：搜索框、分页列表（id / 备注 / 角色 / 启用 / `from_env` / 最近活跃）、行内改备注、
  切角色、启用停用、删除；批量添加（粘贴逗号分隔 id，最多 50，可选角色与备注，结果分
  added / updated / ignored）
- **Status**：版本、各表计数、webhook 状态（只读）
- **Recent denied**：最近被拒账号（内存最多 50 条，重启清空），可一键 Allow
- 入口：tabbar 第四页签 `Settings`（默认 `hidden`，`/api/me` 确认管理员后才显示）；
  `/settings` 命令回复按钮直达 `{AppURL}#/settings`
- 非管理员：命令回 `Admins only.`；手输 `#/settings` 只看到 `Admins only` 空状态，不发起
  `/api/admin/*` 请求；被撤销的账号整页显示 `Access revoked. Ask the administrator.`

### 13.6 验收矩阵

| # | 状态 | bot 私聊 | Mini App | 后台推送 |
|---|---|---|---|---|
| 1 | 管理员（`role=1`，`enabled=1`） | 全部命令；App 启用时退役命令回「去 App」提示 + 按钮 | 全部页签 + Settings | 收 |
| 2 | 已授权用户（`role=0`，`enabled=1`） | 未退役命令可用（`/start`、`/me`）；退役命令只回提示；`/settings` 回 `Admins only.` | Today / Keywords / Alarms / Records | 收 |
| 3 | 撤销（行删除） | `Access denied. This bot is private. Ask the owner for access.`（10 分钟一次） | `/api/me` 403 `forbidden`，整页显示 `Access revoked. Ask the administrator.` | 跳过，数据保留 |
| 4 | 停用（`enabled=0`） | 同撤销 | 同撤销 | 跳过，数据保留 |
| 5 | `off` 模式 | 任何人可用 | 任何人可用（`/api/admin/*` 仍需管理员） | 按各自关键词推送 |

护栏验收：

| 操作 | 预期 |
|---|---|
| 删除 / 停用 / 降级自己 | 409 `cannot change your own access` |
| 删除 / 停用 / 降级最后一个启用中的管理员 | 409 `cannot remove the last administrator` |
| 改 `from_env=1` 行的角色或启用状态、删除该行 | 409 `managed by MANAGER_ID / ALLOWED_USER_IDS`（改备注放行） |

运维视角的同一份内容（启动引导、回滚、排障）见 `docs/DEPLOYMENT.md` §9。

---

## 附录：与现有代码的对应关系

| 现有实现 | 规划后 |
|---|---|
| `pkg/handler/bot_context.go` `go ctx.Bot.Start()` | webhook / 轮询双模式 |
| `startWebhookServer()`（仅 Gotenberg 回调） | 统一 HTTP server（webhook + App + API + healthz） |
| `pkg/handler/statistics.go`（文本报表 + 内联按钮） | App 启用时降级为提示；未启用时保留 |
| `paginatedSearchResult` / `paginatedTodayResult` | App Today 页；降级模式下原样保留 |
| `paginatedAlarms` | App Alarms 页；降级模式下保留 |
| `convertURL()`（仅命令触发） | 抽出 `startConversion()`，命令与 App API 共用 |
| `commands_handler.go` 关键词命令 | App Keywords 页；降级模式下保留 |
