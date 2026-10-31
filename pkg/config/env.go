package config

import (
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gythialy/magnet/pkg/constant"
	"github.com/rs/zerolog"
)

const defaultScheduleInterval = 1

// ManagerId returns the Telegram user ID allowed to run admin commands, or 0
// when MANAGER_ID is unset or is not a valid int64.
func ManagerId() int64 {
	id := os.Getenv(constant.ManagerId)
	if id == "" {
		return 0
	}
	i, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0
	}
	return i
}

// LogLevel returns LOG_LEVEL, falling back to debug for unset or invalid values.
func LogLevel() zerolog.Level {
	if logLevel := os.Getenv(constant.LogLevel); logLevel != "" {
		if level, err := zerolog.ParseLevel(logLevel); err == nil {
			return level
		}
	}
	return zerolog.DebugLevel
}

// BaseDir returns CONFIG_PATH, falling back to the working directory.
func BaseDir() string {
	if cfgPath := os.Getenv(constant.ConfigPath); cfgPath != "" {
		return cfgPath
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// MessageServerUrl returns the host of SERVER_URL, e.g.
// "https://example.com/foo" becomes "example.com". Absolute links are built
// from it by handler.Crawler.absoluteURL.
func MessageServerUrl() string {
	u := os.Getenv(constant.ServerURL)
	if u == "" {
		return ""
	}
	if parse, err := url.Parse(u); err == nil && parse.Host != "" {
		return parse.Host
	}
	return u
}

// PublicBaseURL returns PUBLIC_BASE_URL without trailing slashes, or "" when
// the public entry point is not configured. An empty value means polling mode,
// so callers must treat it as "not configured" rather than invalid.
func PublicBaseURL() string {
	return strings.TrimRight(os.Getenv(constant.PublicBaseURL), "/")
}

// TelegramWebhookSecret returns TELEGRAM_WEBHOOK_SECRET, the value Telegram
// echoes back in X-Telegram-Bot-Api-Secret-Token. It is returned verbatim
// because whitespace would be rejected by the header check anyway.
func TelegramWebhookSecret() string {
	return os.Getenv(constant.TelegramWebhookSecret)
}

// TelegramWebhookEnabled reports TELEGRAM_WEBHOOK_ENABLED, defaulting to true.
// Setting it to false keeps polling even when PUBLIC_BASE_URL is configured.
func TelegramWebhookEnabled() bool {
	return boolEnv(constant.TelegramWebhookEnabled, true)
}

// MiniAppEnabled reports MINI_APP_ENABLED, defaulting to true. Disabling it
// keeps the legacy text commands instead of serving /app and /api.
func MiniAppEnabled() bool {
	return boolEnv(constant.MiniAppEnabled, true)
}

// boolEnv parses the boolean spellings people actually put in compose files.
// Unrecognised values fall back to fallback so a typo cannot silently disable
// a feature that was meant to stay on.
func boolEnv(name string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "on":
		return true
	case "0", "false", "off":
		return false
	default:
		return fallback
	}
}

// TelegramToken returns TELEGRAM_BOT_TOKEN.
func TelegramToken() string {
	return os.Getenv(constant.TelegramBotToken)
}

// TelegramName returns TELEGRAM_BOT_NAME, used when building /alarm share links.
func TelegramName() string {
	return os.Getenv(constant.TelegramBotName)
}

// ScheduleInterval returns how often the crawler runs, in hours.
//
// Values below one are ignored rather than passed on: gocron cannot schedule a
// zero interval, so an invalid setting must not break startup.
func ScheduleInterval() int {
	if interval := os.Getenv(constant.ScheduleInterval); interval != "" {
		if i, err := strconv.Atoi(interval); err == nil && i >= 1 {
			return i
		}
	}
	return defaultScheduleInterval
}
