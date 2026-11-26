# magnet-bot

Telegram bot that crawls public procurement notices, matches them against
user-configured keywords, and pushes notifications. Also supports converting
notice URLs to PDF/PNG via Gotenberg.

```bash
docker run --name magnet-bot -d \
  -e TELEGRAM_BOT_TOKEN=<your_token> \
  -e SERVER_URL=https://example.com \
  ghcr.io/gythialy/magnet-bot:latest
```

See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for the full deployment guide
(Docker Compose, systemd, Gotenberg, webhook wiring and troubleshooting).

## Environment Variables

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | ✅ | - | Telegram bot token from @BotFather |
| `TELEGRAM_BOT_NAME` | - | - | Bot username, used when building alarm record deep links |
| `SERVER_URL` | ✅ | - | Host of the notice API, e.g. `https://example.com` (no trailing slash). Not validated at startup: leaving it empty starts fine but links cannot be built |
| `MANAGER_ID` | - | - | Comma-separated Telegram user IDs allowed to run admin commands (`/retry`, `/clean`, `/settings`) and manage access, e.g. `10001,10002` |
| `ALLOWED_USER_IDS` | - | - | Optional comma-separated seed list of regular users, synced into the `users` table on startup only (first-time rollout) |
| `SCHEDULE_INTERVAL` | - | `1` | How often the crawler runs, in hours |
| `CRAWL_DAYS` | - | `1` | How many days back the crawler looks for notices |
| `CONFIG_PATH` | - | working dir | Directory for `bot.db` and `bot.log` |
| `LOG_LEVEL` | - | `debug` | zerolog level: `trace`, `debug`, `info`, `warn`, `error`. `trace` also enables the crawler's HTTP debug logging (very noisy) |
| `WEBHOOK_SERVER_URL` | ✅ | - | Address Gotenberg calls back to (no port, no trailing slash), e.g. `http://magnet-bot`. Leaving it empty builds an invalid `:8080/pdf/...` callback |
| `WEBHOOK_SERVER_PORT` | ✅ | `0` | Local HTTP port the unified server listens on; also the Gotenberg callback port. Must be non-zero, otherwise callbacks go to a random port |
| `WEBHOOK_TOKEN` | - | - | Shared secret for webhook callbacks; enables auth when set |
| `PDF_SERVER_URL` | ✅ | - | Gotenberg service URL, e.g. `http://gotenberg:3000`. **Empty fails startup** with `create gotenberg client: empty hostname` |
| `PUBLIC_BASE_URL` | public mode | empty | Public HTTPS origin, e.g. `https://bot.example.com` (scheme included, no trailing slash). **Empty = legacy polling mode**: no webhook registration and no public entry point |
| `TELEGRAM_WEBHOOK_SECRET` | with webhook | - | 1–256 characters of `[A-Za-z0-9_-]`; Telegram echoes it in `X-Telegram-Bot-Api-Secret-Token` |
| `TELEGRAM_WEBHOOK_ENABLED` | - | `true` | Set `false` to keep polling even when `PUBLIC_BASE_URL` is set |
| `MINI_APP_ENABLED` | - | `true` | Set `false` to keep the legacy text commands instead of serving `/app/` and `/api/v1/` |

> `TELEGRAM_WEBHOOK_ENABLED` and `MINI_APP_ENABLED` accept `1/true/on` and
> `0/false/off`; an unrecognised value falls back to their default (`true`).

> All credential-like values (`TELEGRAM_BOT_TOKEN`, `WEBHOOK_TOKEN`, etc.)
> should be provided via environment variables or secrets management at
> deploy time. Never commit real values to the repository.

> Leaving `PUBLIC_BASE_URL` empty keeps the legacy polling mode: no webhook is
> registered and nothing has to be public — `WEBHOOK_SERVER_PORT` still serves
> `/healthz` and the internal Gotenberg callback. The webhook ingress setup
> (Caddy, cloudflared, Cloudflare caveats and rollback) is documented in
> [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) §8.

> Access control — `off` vs `restricted`, the administrator list and the Mini
> App's Settings tab — is documented in
> [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) §9. A database with no administrator
> starts in `off` mode; `MANAGER_ID` / `ALLOWED_USER_IDS` are startup seeds only,
> and day-to-day changes belong in the Settings tab.

## Design

The Mini App's visual vocabulary — tokens, the bilingual type ramp, layout rules,
component shapes and the states every screen owes an exit to — is
[docs/DESIGN.md](https://github.com/gythialy/magnet-bot/blob/main/docs/DESIGN.md).
The product intent behind it (who uses the bot, what it refuses to become) is
[docs/PRODUCT.md](https://github.com/gythialy/magnet-bot/blob/main/docs/PRODUCT.md).

Redesigns are drawn as a canvas under `design/`, one folder per pass; the current
one is `design/miniapp-ui-v5` (an Open Artifact canvas, not a static site).

## Tests

`go test ./...` runs the unit suite. The Mini App's browser specs sit behind a
build tag and need a local Chromium or Chrome:

```bash
make e2e                     # probes the usual install locations
E2E_CHROME_BIN=/path/to/chrome make e2e
```

The CI job is [.github/workflows/e2e.yaml](.github/workflows/e2e.yaml); a failed
spec also saves a screenshot and the DOM under `E2E_ARTIFACT_DIR`.
