package utils

import (
	"io"
	"os"
	"path"

	"github.com/rs/zerolog/pkgerrors"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Config for logging
type Config struct {
	// Enable console logging
	ConsoleLoggingEnabled bool

	// EncodeLogsAsJson makes the log framework log JSON
	EncodeLogsAsJson bool
	// FileLoggingEnabled makes the framework log to a file
	// the fields below can be skipped if this value is false!
	FileLoggingEnabled bool
	// Directory to log to when file logging is enabled
	Directory string
	// Filename is the name of the logfile which will be placed inside the directory
	Filename string
	// MaxSize the max size in MB of the logfile before it's rolled
	MaxSize int
	// MaxBackups the max number of rolled files to keep
	MaxBackups int
	// MaxAge the max age in days to keep a logfile
	MaxAge   int
	LogLevel zerolog.Level
}

type Logger struct {
	*zerolog.Logger
}

func Configure(config Config) *Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	var writers []io.Writer

	if config.ConsoleLoggingEnabled {
		if config.EncodeLogsAsJson {
			writers = append(writers, os.Stderr)
		} else {
			writers = append(writers, zerolog.ConsoleWriter{Out: os.Stderr, NoColor: false})
		}
	}

	if config.FileLoggingEnabled {
		fileWriter := newRollingFile(config)
		if fileWriter != nil {
			if config.EncodeLogsAsJson {
				writers = append(writers, fileWriter)
			} else {
				writers = append(writers, zerolog.ConsoleWriter{Out: fileWriter, NoColor: true, TimeFormat: zerolog.TimeFormatUnix})
			}
		}
	}

	logger := zerolog.New(io.MultiWriter(writers...)).With().Timestamp().Caller().Logger().Level(config.LogLevel)
	logger.Info().
		Bool("fileLogging", config.FileLoggingEnabled).
		Bool("jsonLogOutput", config.EncodeLogsAsJson).
		Str("logDirectory", config.Directory).
		Str("fileName", config.Filename).
		Int("maxSizeMB", config.MaxSize).
		Int("maxBackups", config.MaxBackups).
		Int("maxAgeInDays", config.MaxAge).
		Msg("logging configured")

	return &Logger{Logger: &logger}
}

func newRollingFile(config Config) io.Writer {
	if err := os.MkdirAll(config.Directory, 0o744); err != nil {
		errorLogger := zerolog.New(os.Stderr).With().Timestamp().Logger()
		errorLogger.Error().Err(err).Str("path", config.Directory).Msg("can't create log directory")
		return nil
	}

	return &lumberjack.Logger{
		Filename:   path.Join(config.Directory, config.Filename),
		MaxBackups: config.MaxBackups,
		MaxSize:    config.MaxSize,
		MaxAge:     config.MaxAge,
		LocalTime:  true,
		Compress:   true,
	}
}
