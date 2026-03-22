package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

const (
	DefaultLevel  = "info"
	DefaultFormat = "text"

	EnvLogLevel  = "KONTEXT_LOG_LEVEL"
	EnvLogFormat = "KONTEXT_LOG_FORMAT"
)

type Options struct {
	Level  string
	Format string
	Output io.Writer
}

var (
	defaultLoggerMu sync.RWMutex
	defaultLogger   = slog.Default()
)

func Init(opts Options) (*slog.Logger, error) {
	levelName := strings.TrimSpace(opts.Level)
	if levelName == "" {
		levelName = strings.TrimSpace(os.Getenv(EnvLogLevel))
	}
	if levelName == "" {
		levelName = DefaultLevel
	}

	formatName := strings.TrimSpace(opts.Format)
	if formatName == "" {
		formatName = strings.TrimSpace(os.Getenv(EnvLogFormat))
	}
	if formatName == "" {
		formatName = DefaultFormat
	}

	level, err := ParseLevel(levelName)
	if err != nil {
		return nil, err
	}

	formatName = strings.ToLower(formatName)
	output := opts.Output
	if output == nil {
		output = os.Stderr
	}

	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level <= slog.LevelDebug,
	}

	var handler slog.Handler
	switch formatName {
	case "text":
		handler = slog.NewTextHandler(output, handlerOpts)
	case "json":
		handler = slog.NewJSONHandler(output, handlerOpts)
	default:
		return nil, fmt.Errorf("invalid log format %q, expected text or json", formatName)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	defaultLoggerMu.Lock()
	defaultLogger = logger
	defaultLoggerMu.Unlock()

	return logger, nil
}

func Default() *slog.Logger {
	defaultLoggerMu.RLock()
	logger := defaultLogger
	defaultLoggerMu.RUnlock()
	if logger != nil {
		return logger
	}
	return slog.Default()
}

func ParseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q, expected debug, info, warn, or error", level)
	}
}

func CommandLogger(commandName string) *slog.Logger {
	if strings.TrimSpace(commandName) == "" {
		return Default()
	}
	return Default().With("command", commandName)
}
