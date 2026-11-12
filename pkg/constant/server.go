package constant

const (
	TelegramBotToken  = "TELEGRAM_BOT_TOKEN"
	TelegramBotName   = "TELEGRAM_BOT_NAME"
	ScheduleInterval  = "SCHEDULE_INTERVAL"
	ConfigPath        = "CONFIG_PATH"
	ManagerId         = "MANAGER_ID"
	ServerURL         = "SERVER_URL"
	DatabaseFile      = "bot.db"
	LogFile           = "bot.log"
	LogLevel          = "LOG_LEVEL"
	CrawlDays         = "CRAWL_DAYS"
	PDFEndPoint       = "/pdf/"
	WebhookServerURL  = "WEBHOOK_SERVER_URL"
	WebhookServerPort = "WEBHOOK_SERVER_PORT"
	WebhookToken      = "WEBHOOK_TOKEN"
	PDFServerUrl      = "PDF_SERVER_URL"
	PDFExtension      = ".pdf"
	ImgExtension      = ".png"

	// Public entry point settings. PUBLIC_BASE_URL is the only switch that
	// enables the public HTTP surface; the paths are fixed so deployments do
	// not have to keep the reverse proxy and the bot in sync.
	PublicBaseURL          = "PUBLIC_BASE_URL"
	TelegramWebhookSecret  = "TELEGRAM_WEBHOOK_SECRET"
	TelegramWebhookEnabled = "TELEGRAM_WEBHOOK_ENABLED"
	MiniAppEnabled         = "MINI_APP_ENABLED"
	TelegramWebhookPath    = "/telegram/webhook"
	MiniAppPath            = "/app/"
	// APIPath carries the version segment: the Mini App's assets are cached
	// forever, so a client can outlive the deployment that served it. A version
	// in the path lets a breaking response change ship as /api/v2 while an old
	// client fails against a 404 instead of misreading the new payload.
	APIPath    = "/api/v1/"
	HealthPath = "/healthz"
)
