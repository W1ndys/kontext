package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultLevel  = "info"
	DefaultFormat = "text"

	EnvLogLevel  = "KONTEXT_LOG_LEVEL"
	EnvLogFormat = "KONTEXT_LOG_FORMAT"
	EnvLogFile   = "KONTEXT_LOG_FILE"
	EnvLogDir    = "KONTEXT_LOG_DIR"

	defaultLogDirName    = ".kontext"
	defaultLogsSubdir    = "logs"
	defaultLogFilePrefix = "kontext-"
	defaultLogTimeFormat = "20060102-150405.000"
)

type Options struct {
	Level      string
	Format     string
	Output     io.Writer
	File       string
	ConsoleLog bool // 是否同时输出到控制台（默认 false，仅写日志文件）
}

var (
	defaultLoggerMu sync.RWMutex
	defaultLogger   = slog.Default()
	currentLogPath  string
	currentLogFile  *os.File
)

// Init 根据选项初始化全局日志记录器，支持控制台和文件双输出。
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

	consoleHandler, err := newHandler(formatName, output, &slog.HandlerOptions{
		Level:     level,
		AddSource: level <= slog.LevelDebug,
	})
	if err != nil {
		return nil, err
	}

	filePath := strings.TrimSpace(opts.File)
	if filePath == "" {
		filePath = strings.TrimSpace(os.Getenv(EnvLogFile))
	}
	if filePath == "" {
		if resolved, resolveErr := DefaultLogFilePath(time.Now()); resolveErr == nil {
			filePath = resolved
		}
	}

	var handlers []slog.Handler
	if opts.ConsoleLog {
		handlers = append(handlers, consoleHandler)
	}
	var file *os.File
	var fileOpenErr error
	if filePath != "" {
		file, fileOpenErr = openLogFile(filePath)
		if fileOpenErr == nil {
			fileHandler, handlerErr := newHandler(formatName, file, &slog.HandlerOptions{
				Level:     slog.LevelDebug,
				AddSource: true,
			})
			if handlerErr != nil {
				_ = file.Close()
				return nil, handlerErr
			}
			handlers = append(handlers, fileHandler)
		}
	}

	logger := slog.New(newFanoutHandler(handlers...))
	slog.SetDefault(logger)

	defaultLoggerMu.Lock()
	if currentLogFile != nil && currentLogFile != file {
		_ = currentLogFile.Close()
	}
	defaultLogger = logger
	currentLogPath = filePath
	currentLogFile = file
	defaultLoggerMu.Unlock()

	if fileOpenErr != nil {
		logger.Warn("open log file failed",
			"path", filePath,
			"error", fileOpenErr,
		)
	}

	return logger, nil
}

// Default 返回当前全局日志记录器实例。
func Default() *slog.Logger {
	defaultLoggerMu.RLock()
	logger := defaultLogger
	defaultLoggerMu.RUnlock()
	if logger != nil {
		return logger
	}
	return slog.Default()
}

// ParseLevel 将日志级别字符串解析为 slog.Level，支持 debug/info/warn/error。
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

// CommandLogger 返回带有 command 属性的子日志记录器。
func CommandLogger(commandName string) *slog.Logger {
	if strings.TrimSpace(commandName) == "" {
		return Default()
	}
	return Default().With("command", commandName)
}

// CurrentLogFilePath 返回当前日志文件的路径。
func CurrentLogFilePath() string {
	defaultLoggerMu.RLock()
	path := currentLogPath
	defaultLoggerMu.RUnlock()
	return path
}

// DefaultLogFilePath 根据时间戳生成默认的日志文件路径。
func DefaultLogFilePath(now time.Time) (string, error) {
	dirPath := strings.TrimSpace(os.Getenv(EnvLogDir))
	if dirPath == "" {
		var err error
		dirPath, err = DefaultLogDirPath()
		if err != nil {
			return "", err
		}
	}

	fileName := defaultLogFilePrefix + now.Format(defaultLogTimeFormat) + ".log"
	return filepath.Join(dirPath, fileName), nil
}

// DefaultLogDirPath 返回默认的日志目录路径，位于当前工作目录下的 .kontext/logs/。
func DefaultLogDirPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve current working directory failed: %w", err)
	}
	return filepath.Join(cwd, defaultLogDirName, defaultLogsSubdir), nil
}

// 打开日志文件，若目录不存在则自动创建
func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create log directory failed: %w", err)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
}

// 根据格式名称创建对应的 slog.Handler，支持 text 和 json 格式
func newHandler(format string, output io.Writer, opts *slog.HandlerOptions) (slog.Handler, error) {
	switch format {
	case "text":
		return slog.NewTextHandler(output, opts), nil
	case "json":
		return slog.NewJSONHandler(output, opts), nil
	default:
		return nil, fmt.Errorf("invalid log format %q, expected text or json", format)
	}
}

type fanoutHandler struct {
	handlers []slog.Handler
}

// 创建扇出日志处理器，将日志同时分发到多个 Handler
func newFanoutHandler(handlers ...slog.Handler) slog.Handler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			filtered = append(filtered, h)
		}
	}
	return &fanoutHandler{handlers: filtered}
}

// Enabled 当任意子 Handler 启用了该级别时返回 true。
func (h *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, child := range h.handlers {
		if child.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle 将日志记录分发到所有已启用该级别的子 Handler。
func (h *fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, child := range h.handlers {
		if !child.Enabled(ctx, record.Level) {
			continue
		}
		if err := child.Handle(ctx, record.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// WithAttrs 返回一个新的扇出 Handler，所有子 Handler 均附加指定属性。
func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, 0, len(h.handlers))
	for _, child := range h.handlers {
		next = append(next, child.WithAttrs(attrs))
	}
	return &fanoutHandler{handlers: next}
}

// WithGroup 返回一个新的扇出 Handler，所有子 Handler 均添加指定分组。
func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, 0, len(h.handlers))
	for _, child := range h.handlers {
		next = append(next, child.WithGroup(name))
	}
	return &fanoutHandler{handlers: next}
}
