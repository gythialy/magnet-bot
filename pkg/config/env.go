package config

import (
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gythialy/magnet/pkg/constant"
	"github.com/rs/zerolog"
)

const (
	defaultScheduleInterval = 1

	// allowedUserIDsEnv is the seed list of regular users. The constant package
	// carries only the variables that older releases already knew about; keeping
	// this one local keeps the existing constant API untouched.
	allowedUserIDsEnv = "ALLOWED_USER_IDS"
)

// ManagerId returns the first Telegram user ID allowed to run admin commands, or
// 0 when MANAGER_ID is unset or carries no valid id.
//
// MANAGER_ID may now list several comma-separated administrators; the legacy
// call sites expect a single id, so this returns the first one. For the historic
// single-value form the result is exactly what it always was. New code should use
// ManagerIds and authorise against the whole list.
func ManagerId() int64 {
	ids := ManagerIds()
	if len(ids) == 0 {
		return 0
	}
	return ids[0]
}

// ManagerIds returns every administrator in the comma-separated MANAGER_ID
// variable, in declaration order and without duplicates.
//
// Parsing is forgiving on purpose: MANAGER_ID is edited by an operator in a
// compose file, so a stray space or a typo must not take the whole bot down.
// Empty entries are skipped, and an unparsable or non-positive id is ignored with
// a warning naming the variable (never the whole value). An unset or entirely
// invalid variable yields an empty slice.
func ManagerIds() []int64 {
	return parseUserIDs(os.Getenv(constant.ManagerId), constant.ManagerId)
}

// AllowedUserIDs returns the comma-separated ALLOWED_USER_IDS seed list of
// regular users, in declaration order and without duplicates.
//
// It is the same parser as MANAGER_ID: empty entries are skipped and invalid ids
// are ignored with a warning. An unset variable yields an empty slice.
func AllowedUserIDs() []int64 {
	return parseUserIDs(os.Getenv(allowedUserIDsEnv), allowedUserIDsEnv)
}

// parseUserIDs splits a comma-separated id list. source is the environment
// variable name, used only for the warning so an operator knows which setting to
// fix.
func parseUserIDs(raw, source string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		// Telegram ids are positive; a zero or negative value cannot identify an
		// account, so it is treated as a typo rather than silently stored.
		if err != nil || id <= 0 {
			slog.Warn("ignoring invalid user id in environment", "variable", source, "value", part)
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	return ids
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
