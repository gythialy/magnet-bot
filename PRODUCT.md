# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

The only graphical surface is a Telegram Mini App: a web page rendered inside
the Telegram client's webview (iOS, Android and Telegram Desktop). There is no
native app, no browser-first web app and no separate account area.

## Users

**Primary: the operator.** One administrator self-hosts the bot for a small
circle of colleagues. They open the Mini App from Telegram, usually on a phone
and between other work, to answer one question: *did anything relevant come in,
and does the right person have access?* They also own the people list, the
access mode and the deployment status.

**Secondary: allow-listed colleagues.** A handful of trusted accounts use the
same app with fewer permissions: they browse the notices pushed to them, search
their own history, manage their own project and alarm keywords, and read their
own alarm records. They never see another account's rows and never see the
Settings tab.

Accounts are Telegram user ids. There is no signup, password, e-mail or separate
identity provider.

## Product Purpose

magnet watches public procurement notice sources on a schedule, matches each
notice against every account's keyword rules, and pushes the hits into Telegram.
It keeps the pushed history and the matched credit/punishment records in its own
SQLite database, can render a notice page into a PDF or PNG through Gotenberg,
and serves a Mini App over the same store for the three things chat is bad at:
searching a large history, editing keywords in bulk, and reading a record's
detail without scrolling back through messages.

Success is a person closing the Mini App within seconds, having either found the
notice they were waiting for or confirmed that nothing needs attention — with no
duplicate pushes and no revoked account still receiving messages.

## Positioning

The bot does not re-host the notice site and has no data product behind it. Its
mechanism is a private, self-hosted watch service: one process, one SQLite file,
the operator's own Telegram bot, and keyword rules that each account owns.
Identity is delegated to Telegram (bot user id; Mini App `initData` signature),
so there is no account system to build or breach, and the same allow-list gates
the chat, the API and background pushes. A feed reader or a hosted alerting SaaS
can copy the crawling, but not the "one operator, small trusted circle, own
database, no accounts" shape without becoming a different product.

## Operating Context

- Every interaction happens inside Telegram. The bot answers private chats only;
  groups, channels and service messages are ignored rather than answered.
- The Mini App runs in the Telegram client's webview, often on a mobile network,
  and follows the client's light/dark theme. The bot serves the page itself
  (embedded assets, no build step, no CDN; the Telegram WebApp SDK is vendored).
- The bot has two delivery modes. With a public HTTPS origin it registers a
  Telegram webhook and serves webhook, Mini App, API and Gotenberg callback from
  one listener. Without one it falls back to long polling: the listener still
  serves `/healthz`, the Gotenberg callback, and `/app/`/`/api/v1/` when the Mini App
  is enabled, but nothing is publicly reachable.
- Deployment is small and self-hosted: one container image (distroless,
  amd64/arm64), a writable volume for `bot.db` and `bot.log`, and a Gotenberg 8
  sidecar. A Gotenberg endpoint is required at startup even when conversion is
  never used, because the client refuses an empty URL.
- Conversion is asynchronous: Gotenberg renders the notice and calls the bot back
  (`POST /pdf/{id}`); the file is delivered as a chat message, not into the app.
- Schedules and "today" boundaries use `Asia/Shanghai`, independent of host tz.
- The repository is public. Tests, fixtures and committed config use synthetic
  data only; real notices, user ids and hostnames stay out of git.

## Capabilities and Constraints

### 1. Notice collection and keyword matching

- A scheduler runs one crawl every `SCHEDULE_INTERVAL` hours (default 1,
  singleton: a slow run never overlaps the next).
- The crawl reads the notice list from the configured `SERVER_URL` host for the
  last `CRAWL_DAYS` days (default 1), paged at 20 rows with a 200 ms gap and a
  30 s per-request timeout.
- Notice HTML is reduced to Telegram-safe HTML: relative links are absolutised,
  unsupported markup is flattened, and a table with at least 6 columns or 10
  rows is replaced by a placeholder and flagged for the PDF attachment path.
- Project keywords are rules, not plain strings. `+term`/bare terms are include
  terms (all must appear), `-term` is an exclude term (any appearance rejects the
  notice), and quoted phrases keep spaces. Matching is a case-sensitive substring
  test against the notice title and its open-tender code; spaces are stripped and
  Chinese brackets normalised first.
- Rules containing a tender code (`\d{4}-[A-Z0-9]+-[A-Z0-9]+`, rendered with a
  leading "hot" mark in the message) are ordered before rules with excludes, then
  alphabetically, so the more specific match wins the message headline.
- At-most-once delivery: a `(user_id, url)` history row is claimed with an
  atomic insert before sending; the send rolls the claim back if nothing was
  delivered, and an in-process keyed lock prevents a second render. Multiple bot
  instances sharing the database are safe for the same reason.
- `/retry` deliberately bypasses the claim to re-push a window, and writes its own
  history rows so counters keep their meaning.

### 2. Telegram push delivery

- A matched notice becomes one HTML message: optional hot mark, linked title,
  notice time, the matched keywords in brackets, then the cleaned body.
- Long bodies are split at 4090 UTF-16 units; HTML tags are never cut, each chunk
  closes its open elements and the next one reopens them, and chunks are sent
  500 ms apart to stay inside Telegram's per-chat limits.
- The keyword counter counts distinct delivered notices per keyword (recorded
  only after delivery), so a re-crawled window, a forced re-push or a failed send
  cannot inflate it.
- Background pushes re-check access: keywords survive a revoke, so the sweep skips
  accounts that are no longer allowed, and `/retry` checks the target account
  before it crawls.

### 3. Alarm (credit) monitoring

- Alarm keywords are queried against two credit record kinds, `breakFaith`
  (breach of faith) and `suspend` (suspension), with a bounded fan-out of 8
  concurrent queries; a failing keyword is logged and skipped.
- One alarm message renders the punished entity, credit name/code, start and end
  date, handling department, detail, result and the related notice links. These
  field labels are Chinese in the chat template today.
- Deduplication is by business identity inside a `(user_id, credit_code)` claim:
  the same punishment is never pushed twice, a still-valid claim is kept, and a
  later punishment for the same credit code replaces an expired row and pushes
  once. Expiry is decided on the business identity, not on the end date alone
  (an end-date-based bug caused eight repeats of one alert in 00:27–07:27).
- The bot username builds each record's `t.me` detail deep link; records are
  scoped to the owning account.

### 4. Attachments and conversion

- A notice with a complex table is delivered as a single message: the full page
  rendered to PDF as the document, with the rendered body as its caption. The
  caption is truncated to Telegram's 1024-unit limit; the PDF always holds the
  complete page.
- If the attachment fails, the body still goes out as normal chunks and a pending
  row is enqueued. The retrier sweeps every minute with backoff
  1 m → 5 m → 15 m → 30 m → 1 h → 2 h (6 attempts over roughly four hours), a
  24 h TTL, a claim lease longer than the conversion timeout, and a 3 s gap
  between uploads.
- `POST /api/v1/convert` and `/convertpdf`/`/convertimg` share one service: check the
  notice host allow-list, derive a file name from the page title, post a
  "Converting…" placeholder in the chat, submit to Gotenberg, and swap the
  placeholder for the file when the callback arrives.
- Only URLs on the configured `SERVER_URL` host may be converted; the allow-list
  fails closed when the host is empty. Conversions are limited to 5 per user per
  minute, with in-flight dedupe so a double tap submits once.
- Files larger than 45 MB are refused before upload (Telegram's bot limit is
  50 MB); the callback body is capped at 20 MB and delivered by a 10-worker pool.

### 5. Statistics, lists and search (chat)

- `/statistics` is an inline report: bot version and build time, four counters
  (delivered history, project keywords, related notices — the sum of the keyword
  counters — and alarm keywords), the keyword leaderboard ordered by related
  notices, keyword detail, rename via a force-reply prompt, and delete with
  confirmation. All navigation state travels in the callback data
  (`stats:<action>:<page>:<id>`), so no server-side session exists and the
  message is edited in place.
- `/search_history_title <term>`, `/search_alarm_records <term>`, `/list_today`,
  `/alarm <id>` page through 20 rows per page with inline Previous/Next buttons.
- `/start alarm_<id>` deep-links straight to one alarm detail, which is what the
  Records tab's "Open in chat" button uses.

### 6. Mini App

Served at `/app/` with hash routes. Every `GET`/`PATCH`/`POST`/`DELETE` call carries
the raw `initData` in `X-Telegram-Init-Data`; the server verifies it with the bot
token (HMAC-SHA256, one-hour TTL, 60 s future-skew tolerance), and no cookie,
session or password exists. Opened outside Telegram, the app shows a gate instead
of the shell.

- **Today** — the four statistics counters as a stat strip, title/tender-code
  search, range chips (Today / 7 days / All), a paged notice list, and a detail
  sheet with the notice's full text (sanitized, cached, with a force-refresh
  action), "open notice page" plus PDF/PNG conversion. Conversion starts in the
  app but the file arrives in the chat.
- **Keywords** — the account's project keywords with their notice counters,
  client-side filtering over the loaded list, batch add (comma-separated, at most
  50 per request, 128 runes each), rename and delete in one reusable dialog.
- **Alarms** — the same read/write surface for alarm keywords, in id order.
- **Records** — paged alarm records with server-side search by credit name, and a
  detail sheet rendered from data already loaded (dates, credit fields, page
  links) plus "Open in chat" through a Telegram deep link.
- **Settings** — administrator only; the tab is hidden until `/api/v1/me` confirms
  the role, and a revoked admin sees a revocation notice instead. Four cards:
  access mode (opening access to everyone is confirmed first, switching back to
  restricted is immediate), people (search, batch add by id, edit
  remark/role/enabled, delete), deployment status (version, build, counts,
  webhook state), and recently denied accounts with a one-tap allow, plus the
  guards that prevent locking the operator out.
- Paging is cursor-based (`offset`/`nextOffset`); the client never assumes a total
  it can trust. Default page size 20, hard cap 50.
- Reads are scoped to the caller's own rows by construction; admin endpoints add
  a role check after the access check.

### 7. Access control

- Two modes: `off` (any private-chat account is served) and `restricted` (only
  enabled rows in `users` are served). The mode is stored in `settings` and
  switched from the Settings tab.
- Bootstrap: with no administrator row the database starts in `off`; when an
  administrator exists the effective default is `restricted`. On every start the
  allow-list is re-synced from `MANAGER_ID` (administrators) and
  `ALLOWED_USER_IDS` (regular users): listed ids are created or updated, and
  seeded rows that left the environment are unflagged and demoted, except when
  that would remove the last administrator. Seeded rows carry `from_env`, so the
  environment owns their role and enabled flag while the remark stays editable.
- Four enforcement points: the bot update middleware (allowed account or denial),
  the API access middleware (after `initData`), background pushes (skip revoked
  accounts), and `/retry` (target check). Administrator routes add a role check.
- Denials are answered at most once per account per 10 minutes, recorded in a
  bounded in-memory list for the status screen, and never echo the message text.
  `last_seen` is written at most once per minute.
- Guards: an administrator cannot change their own access, cannot remove, demote
  or disable the last administrator, and cannot edit an environment-managed row's
  role/enabled. Authorisation decisions are cached for at most 30 s; access-mode
  writes invalidate the cache immediately.

### 8. Administration and operations

- `/settings` is administrator-only: it points at the Mini App settings screen
  when the app is enabled, and explains the environment variables otherwise.
- `/retry` (admin) forces one crawl and push for the caller; `/clean` (admin)
  prunes history older than the 30-day retention window.
- `/me` echoes the caller's Telegram chat payload; `/debug` sends a fixed deep
  link. Both are developer utilities, not user features.
- One unified HTTP listener serves `/healthz` (always), the Telegram webhook
  (webhook mode only), the Gotenberg callback, `/app/` and `/api/v1/` when enabled.
- Startup: validate configuration, open/migrate SQLite, bootstrap access control,
  normalise legacy scheme-less history URLs, prune keyword-match rows older than
  30 days. A failed bootstrap is fatal on purpose.
- Shutdown drains the listener (5 s), stops the scheduler and releases the worker
  pool; the Telegram webhook registration is intentionally kept so Telegram
  retains undelivered updates.
- Logging is zerolog to console and a rotated `bot.log` (10 MB × 10 files, 7 days),
  with one line per HTTP request that omits query strings; SQL is logged through
  the same logger. The status screen reports counts, webhook state and denials.

### 9. Limits and constraints

| Constraint | Value |
| --- | --- |
| Chat message budget | 4090 UTF-16 units per chunk; 1024 per caption |
| Chunk / upload pacing | 500 ms between chunks; 3 s between attachment uploads |
| Document size | Telegram bot limit, guarded at 45 MB; 20 MB callback body |
| API paging | default 20, hard cap 50 |
| API writes | 30 per user per minute, shared by keyword/alarm/admin mutations |
| Conversions | 5 per user per minute, in-flight dedupe |
| Batch sizes | 50 keywords or 50 user ids per request |
| Keyword length | 128 runes (UTF-8 bytes up to 64 KB body) |
| Crawl window | `CRAWL_DAYS` days back, default 1, page size 20, 200 ms gap |
| Alarm fan-out | 8 concurrent keyword queries, 30 s request timeout |
| Attachment retries | 6 attempts ≈ 4 h, 24 h TTL, 1 min sweep |
| History retention | 30 days on `/clean` |
| Keyword-match log | 30-day retention, pruned at startup |
| Authorisation cache | 30 s; mode switch immediate in-process |
| Denial throttle | one reply per account per 10 minutes |
| `initData` validity | 1 hour, 60 s future skew |
| Time zone | `Asia/Shanghai` for schedules and "today" |

### 10. Non-goals

- No public signup, multi-tenancy, billing or per-tenant branding.
- No group or channel support; the bot is a private-chat instrument.
- No browser-first web app, no server-rendered pages, no front-end build step.
- No message editing of delivered notifications and no per-user digest schedule;
  the only notification control is the keyword rules themselves.
- No hosted service, analytics SDK, third-party CDN or telemetry endpoint.
- No claim that converted files are archived: conversion is chat delivery only.

## Brand Commitments

- The product name is **magnet** (lowercase in copy and image tags); the bot
  presents itself as a personal instrument, not a SaaS product.
- Interface language is English, terse and literal: controls name their action,
  errors name the problem. Chat help and prompts follow the same voice, but the
  alarm message template and the complex-table placeholder are still Chinese —
  a known inconsistency, not a style decision.
- The app belongs to the Telegram client it is opened from: it follows the
  client's theme parameters, uses the platform type stack and never introduces a
  second accent colour of its own.
- The Mini App's visual world is recorded separately in [DESIGN.md](DESIGN.md)
  ("the printed column"): hairline rules, one accent, no cards in the reading
  flow.

## Evidence on Hand

- Real operating data (tens of thousands of notice history rows, keyword
  counters, alarm records) exists only in the operator's local database and must
  never be committed.
- The repository carries no logo, illustration, marketing copy, testimonials or
  customer evidence; none may be invented. The only product visuals are the Mini
  App's own UI and its design system ([DESIGN.md](DESIGN.md)).
- Tests and fixtures are synthetic by policy (290 test functions across
  `cmd/` and `pkg/`), and the Gotenberg integration test is skipped unless
  `GOTENBERG_URL` and `GOTENBERG_TEST_URL` are set.

## Product Principles

1. **One question per screen.** Each tab answers a single question; anything else
   belongs on another tab.
2. **Seconds, not sessions.** Latency is the feature: page the server, keep the
   first paint small, never make someone wait through an animation to learn the
   answer.
3. **Deliver at most once, then be honest.** Every push is claimed before it is
   sent and rolled back only when nothing arrived; "already delivered" is a
   property of the store, not of a hopeful retry.
4. **Trust is visible.** Access state, who is allowed and why, is always one tap
   away and written in words, not only implied by a switch.
5. **The chat does the talking.** Notifications, confirmations and converted
   files arrive as messages; the app is for browsing, editing and looking things
   up. Native over novel: the client's theme and familiar control patterns win
   over brand expression.

## Accessibility & Inclusion

- Touch targets at least 40 px, body contrast at least 4.5:1 (the theme's accent
  is re-derived when it fails), and every state is expressed in words as well as
  colour.
- Dark theme is a first-class state, not an afterthought, and follows the
  Telegram client rather than a hardcoded palette.
- Feedback never depends on motion alone; errors name the problem and the
  recovery, and the app degrades to a gate screen rather than a broken shell when
  it is opened outside Telegram.

## Appendix A — Chat command reference

Commands are registered with Telegram and listed in `/start` help. When the Mini
App is enabled and publicly reachable, the retired commands answer with a prompt
plus an "Open app" button instead of running, and are hidden from the command
menu.

| Command | Access | Behaviour | Retired with Mini App |
| --- | --- | --- | --- |
| `/start` | allowed | Help text; `alarm_<id>` payload opens one alarm | no |
| `/me` | allowed | Echo the Telegram chat payload (dev utility) | no |
| `/debug` | allowed | Send a fixed alarm deep link (dev utility) | no |
| `/add_keywords <k1,k2>` | allowed | Add project keywords | yes |
| `/edit_keywords id1=kw1;id2=kw2` | allowed | Rename keywords | yes |
| `/delete_keywords <id1,id2>` | allowed | Delete keywords by id | yes |
| `/add_alarm_keywords <k1,k2>` | allowed | Add alarm keywords | yes |
| `/search_alarm_records <term>` | allowed | Page alarm records by credit name | yes |
| `/search_history_title <term>` | allowed | Page history by title | yes |
| `/list_today` | allowed | Page today's history | yes |
| `/alarm <id>` | allowed | Show one alarm detail | yes |
| `/convertpdf <url>` | allowed | Convert a notice to PDF | yes |
| `/convertimg <url>` | allowed | Convert a notice to PNG | yes |
| `/statistics` | allowed | Inline statistics report | yes |
| `/settings` | admin | Point at the Mini App settings screen | no |
| `/retry` | admin | Force one crawl and push for the caller | no |
| `/clean` | admin | Prune history older than 30 days | no |

Inline callback namespaces: `stats:`, `search:`, `alarm:`, `today:`.

Anything else falls through to the default handler, which echoes the raw update
as JSON — the historical debug fallback, not a user-facing behaviour.

## Appendix B — HTTP surface

All `/api/*` calls require a valid `X-Telegram-Init-Data` header; `/api/v1/admin/*`
additionally requires an enabled administrator. Errors use
`{"error":{"code":"..."}}` with `unauthorized`, `forbidden`,
`forbidden_admin`, `not_found`, `bad_request`, `too_many_requests`,
`conversion_failed`, `internal_error`, and admin guard answers
`409 {"error":{"code":"guard","message":"..."}}` (self-change, last
administrator, environment-managed row).

| Method and path | Purpose |
| --- | --- |
| `GET /healthz` | Plain-text liveness probe, always registered |
| `POST /telegram/webhook` | Telegram updates (webhook mode only, secret header) |
| `POST /pdf/{id}` | Gotenberg result callback (optional `?token=`) |
| `GET /app/` (and `/app` → 301) | Embedded Mini App shell and assets |
| `GET /api/v1/statistics` | Four counters plus bot username |
| `GET /api/v1/notices?range=today\|7d\|all&q=&offset=&limit=` | Paged notice history (`range` defaults to `today`) |
| `GET /api/v1/notices/content?url=&refresh=` | Sanitized full text of one owned notice; served from cache unless `refresh=1` |
| `GET /api/v1/keywords` | Project keywords, leaderboard order |
| `GET /api/v1/alarms` | Alarm keywords, id order |
| `GET /api/v1/alarm-records?q=&offset=&limit=` | Paged alarm records with detail fields and deep link |
| `POST /api/v1/convert` | Start a conversion (`url`, `format` = `pdf`/`png`); 202 |
| `GET /api/v1/me` | Caller identity, role, access mode, remark |
| `GET /api/v1/admin/users?q=&offset=&limit=` | Paged people list |
| `POST /api/v1/admin/users` | Batch add/update people (`userIds`, `role`, `remark`) |
| `PATCH /api/v1/admin/users/{id}` | Edit remark, role, enabled |
| `DELETE /api/v1/admin/users/{id}` | Remove an account (guarded) |
| `GET /api/v1/admin/settings` | Current access mode |
| `PATCH /api/v1/admin/settings` | Switch access mode (`off`/`restricted`) |
| `GET /api/v1/admin/status` | Version, build, counts, webhook state, recent denials |

## Appendix C — Data model

| Table | Key | Holds |
| --- | --- | --- |
| `keywords` | `id` | Keyword text, owner, type (`PROJECT`/`ALARM`), delivered-notice counter, soft delete |
| `histories` | `(user_id, url)` | Delivered notice: title, tender-code flag, delivery time |
| `notice_contents` | `url` | Sanitized full text of a delivered notice, when it was fetched |
| `alarms` | `(user_id, credit_code)` | Credit/punishment record: business id, credit fields, dates, handling, page links |
| `keyword_notices` | `(keyword_id, url)` | Which notice a keyword actually delivered, the source of the counter |
| `pending_attachments` | `(user_id, url)` | Attachment retry queue: attempts, next attempt, lease, last error |
| `users` | `user_id` | Access row: remark, role, enabled, seeded-from-env flag, last seen |
| `settings` | `key` | Persisted settings; currently the access mode |

SQLite stores datetimes as text. Comparisons that matter for correctness (expiry,
range boundaries) are done on parsed time values or with SQLite `julianday()`,
never as string comparisons across different UTC offsets.

## Appendix D — Configuration

All settings are environment variables; [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)
is the authoritative guide (defaults, validation, startup failures). The
essential ones:

| Group | Variables |
| --- | --- |
| Telegram | `TELEGRAM_BOT_TOKEN`, `TELEGRAM_BOT_NAME`, `TELEGRAM_WEBHOOK_SECRET`, `TELEGRAM_WEBHOOK_ENABLED` |
| Public surface | `PUBLIC_BASE_URL`, `WEBHOOK_SERVER_URL`, `WEBHOOK_SERVER_PORT`, `MINI_APP_ENABLED` |
| Conversion | `PDF_SERVER_URL`, `WEBHOOK_TOKEN` |
| Access | `MANAGER_ID`, `ALLOWED_USER_IDS` |
| Crawling | `SERVER_URL`, `SCHEDULE_INTERVAL`, `CRAWL_DAYS` |
| Storage and logs | `CONFIG_PATH`, `LOG_LEVEL` |

`PUBLIC_BASE_URL` is the only switch that enables the public surface: empty means
polling mode, no webhook registration, and the Mini App reachable only locally.
When it is set and `TELEGRAM_WEBHOOK_ENABLED` is true, startup requires a valid
HTTPS base URL, a valid webhook secret and a non-zero listener port, and fails
otherwise instead of accepting updates it would then reject.

## Appendix E — Code map

| Path | Responsibility |
| --- | --- |
| `cmd/server` | Process entry point: build context, run until SIGINT/SIGTERM |
| `pkg/config` | Environment parsing, validation, public URL helpers |
| `pkg/constant` | Command names, env names, paths, version/build variables |
| `pkg/model` | GORM models, HTTP payload shapes for notice/credit APIs, message templates |
| `pkg/dal` | Data access, claims, access-control bootstrap and guards, migrations |
| `pkg/rule` | Keyword-rule parsing, matching and specificity ordering |
| `pkg/handler` | Bot wiring, crawler, push pipeline, commands, access enforcement, Gotenberg client, retries |
| `pkg/server` | HTTP transport: routing, Mini App API, admin API, auth, rate limits, static assets |
| `pkg/server/web` | Mini App shell, CSS and JS (no build step) |
| `pkg/utils` | HTML simplification, URL validation, logging, misc helpers |

The transport package (`pkg/server`) never imports `pkg/handler`: the handler
configures the bot and injects the HTTP handlers, which keeps the dependency
direction one-way.

## Appendix F — Documentation map

| Document | Purpose |
| --- | --- |
| [README.md](README.md) | Quick start and the environment-variable table |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | Deployment, webhook/Caddy/cloudflared, access control, operations |
| [DESIGN.md](DESIGN.md) | The Mini App's design system and visual rules |
| [PRODUCT.md](PRODUCT.md) (this file) | Full product and feature inventory, kept in the Impeccable schema so design work can read it |
