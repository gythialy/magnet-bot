package constant

const (
	TelegramBotToken = "TELEGRAM_BOT_TOKEN"
	TelegramBotName  = "TELEGRAM_BOT_NAME"
	ScheduleInterval = "SCHEDULE_INTERVAL"
	GeminiModel      = "GEMINI_MODEL"
	GeminiAPIKey     = "GEMINI_API_KEY"
	ConfigPath       = "CONFIG_PATH"
	ManagerId        = "MANAGER_ID"
	ServerURL        = "SERVER_URL"
	DatabaseFile     = "bot.db"
	LogFile          = "bot.log"
	LogLevel         = "LOG_LEVEL"
	CrawlDays        = "CRAWL_DAYS"
	RestyTrace       = "RESTY_TRACE"
	// Gemini rate limits are tunable via env so they can match the actual
	// API tier (free tier RPD is much lower than paid tiers).
	GeminiRequestsPerMinute = "GEMINI_REQUESTS_PER_MINUTE"
	GeminiRequestsPerDay    = "GEMINI_REQUESTS_PER_DAY"

	PDFEndPoint       = "/pdf/"
	WebhookServerURL  = "WEBHOOK_SERVER_URL"
	WebhookServerPort = "WEBHOOK_SERVER_PORT"
	WebhookToken      = "WEBHOOK_TOKEN"
	PDFServerUrl      = "PDF_SERVER_URL"
	PDFExtension      = ".pdf"
	ImgExtension      = ".png"
)
