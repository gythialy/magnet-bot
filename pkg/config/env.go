package config

import (
	"net/url"
	"os"
	"strconv"

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
