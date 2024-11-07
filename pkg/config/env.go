package config

import (
	"net/url"
	"os"
	"strconv"

	"github.com/gythialy/magnet/pkg/constant"
	"github.com/rs/zerolog"
)

const defaultScheduleInterval = 1

func ManagerId() int64 {
	id := os.Getenv(constant.ManagerId)
	if id == "" {
		return 0
	} else {
		if i, err := strconv.ParseInt(id, 10, 64); err == nil {
			return i
		} else {
			return 0
		}
	}
}

func LogLevel() zerolog.Level {
	logLevel := os.Getenv(constant.LogLevel)
	if logLevel != "" {
		if level, err := zerolog.ParseLevel(logLevel); err == nil {
			return level
		}
	}
	return zerolog.DebugLevel
}

func BaseDir() string {
	cfgPath := os.Getenv(constant.ConfigPath)
	if cfgPath == "" {
		cfgPath, _ = os.Getwd()
	}
	return cfgPath
}

func MessageServerUrl() string {
	u := os.Getenv(constant.ServerURL)
	if u != "" {
		if parse, err := url.Parse(u); err == nil {
			return parse.Host
		}
	}
	return u
}

func TelegramToken() string {
	return os.Getenv(constant.TelegramBotToken)
}

func ScheduleInterval() int {
	result := defaultScheduleInterval
	interval := os.Getenv(constant.ScheduleInterval)
	if interval != "" {
		if i, err := strconv.Atoi(interval); err == nil {
			result = i
		}
	}
	return result
}

func GeminiAPIKey() string {
	return os.Getenv(constant.GeminiAPIKey)
}
