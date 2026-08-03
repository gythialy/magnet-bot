# magnet-bot

Telegram bot that crawls public procurement notices, matches them against
user-configured keywords, and pushes notifications. Also supports converting
notice URLs to PDF/PNG via Gotenberg.

```bash
docker run --name magnet-bot -d \
  -e TELEGRAM_BOT_TOKEN=<your_token> \
  ghcr.io/gythialy/manget-bot:latest
```

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
| `LOG_LEVEL` | - | `debug` | zerolog level: `trace`, `debug`, `info`, `warn`, `error` |
| `RESTY_TRACE` | - | - | Set to any value to enable HTTP debug logging for the crawler |
| `WEBHOOK_SERVER_URL` | - | - | Public URL of the webhook server (Gotenberg callback target) |
| `WEBHOOK_SERVER_PORT` | - | - | Local port the webhook server listens on |
| `WEBHOOK_TOKEN` | - | - | Shared secret for webhook callbacks; enables auth when set |
| `PDF_SERVER_URL` | - | - | Gotenberg service URL for PDF/PNG conversion |

> All credential-like values (`TELEGRAM_BOT_TOKEN`, `WEBHOOK_TOKEN`, etc.)
> should be provided via environment variables or secrets management at
> deploy time. Never commit real values to the repository.
