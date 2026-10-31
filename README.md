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
| `TELEGRAM_BOT_NAME` | - | - | Bot username, used when building `/alarm` share links |
| `SERVER_URL` | ✅ | - | Host of the notice API, e.g. `https://example.com` (no trailing slash) |
| `MANAGER_ID` | - | - | Telegram user ID allowed to run admin commands (`/retry`, `/clean`) |
| `SCHEDULE_INTERVAL` | - | `1` | How often the crawler runs, in hours |
| `CRAWL_DAYS` | - | `1` | How many days back the crawler looks for notices |
| `CONFIG_PATH` | - | working dir | Directory for `bot.db` and `bot.log` |
| `LOG_LEVEL` | - | `debug` | zerolog level: `trace`, `debug`, `info`, `warn`, `error`. `trace` also enables the crawler's HTTP debug logging (very noisy) |
| `WEBHOOK_SERVER_URL` | - | - | Public URL of the webhook server (Gotenberg callback target) |
| `WEBHOOK_SERVER_PORT` | public mode | `0` | Local HTTP port the unified server listens on; also the Gotenberg callback port. Must be non-zero when `PUBLIC_BASE_URL` is set |
| `WEBHOOK_TOKEN` | - | - | Shared secret for webhook callbacks; enables auth when set |
| `PDF_SERVER_URL` | - | - | Gotenberg service URL for PDF/PNG conversion |
| `PUBLIC_BASE_URL` | public mode | empty | Public HTTPS origin, e.g. `https://bot.example.com` (scheme included, no trailing slash). **Empty = legacy polling mode**: no webhook registration and no public entry point |
| `TELEGRAM_WEBHOOK_SECRET` | with webhook | - | 1–256 characters of `[A-Za-z0-9_-]`; Telegram echoes it in `X-Telegram-Bot-Api-Secret-Token` |
| `TELEGRAM_WEBHOOK_ENABLED` | - | `true` | Set `false` to keep polling even when `PUBLIC_BASE_URL` is set |
| `MINI_APP_ENABLED` | - | `true` | Set `false` to keep the legacy text commands instead of serving `/app/` and `/api/` |

> All credential-like values (`TELEGRAM_BOT_TOKEN`, `WEBHOOK_TOKEN`, etc.)
> should be provided via environment variables or secrets management at
> deploy time. Never commit real values to the repository.

> Leaving `PUBLIC_BASE_URL` empty keeps the legacy polling mode: no webhook is
> registered and nothing has to be public — `WEBHOOK_SERVER_PORT` still serves
> `/healthz` and the internal Gotenberg callback. The webhook ingress setup
> (Caddy, cloudflared, Cloudflare caveats and rollback) is documented in
> [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) §8.
