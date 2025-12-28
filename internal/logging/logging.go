// Package logging provides structured logging for the gobore application
// using the standard library's log/slog package.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
)

var (
	// defaultLogger is the global logger instance
	defaultLogger *slog.Logger
	// currentLevel stores the current log level
	currentLevel *slog.LevelVar
)

func init() {
	// Initialize with default INFO level logger
	currentLevel = new(slog.LevelVar)
	currentLevel.Set(slog.LevelInfo)
	defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: currentLevel,
	}))
}

// Init initializes the logging system with the specified verbosity level.
// If verbose is true, DEBUG level logging is enabled; otherwise, INFO level is used.
func Init(verbose bool) {
	InitWithWriter(os.Stdout, verbose)
}

// InitWithWriter initializes the logging system with a custom writer.
// This is useful for testing or redirecting logs.
func InitWithWriter(w io.Writer, verbose bool) {
	currentLevel = new(slog.LevelVar)
	if verbose {
		currentLevel.Set(slog.LevelDebug)
	} else {
		currentLevel.Set(slog.LevelInfo)
	}

	defaultLogger = slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: currentLevel,
	}))
}

// SetLevel sets the logging level directly
func SetLevel(level slog.Level) {
	currentLevel.Set(level)
}

// GetLevel returns the current logging level
func GetLevel() slog.Level {
	return currentLevel.Level()
}

// Logger returns the default logger instance
func Logger() *slog.Logger {
	return defaultLogger
}

// Info logs an info level message
func Info(msg string, args ...any) {
	defaultLogger.Info(msg, args...)
}

// Debug logs a debug level message
func Debug(msg string, args ...any) {
	defaultLogger.Debug(msg, args...)
}

// Error logs an error level message
func Error(msg string, args ...any) {
	defaultLogger.Error(msg, args...)
}

// Warn logs a warning level message
func Warn(msg string, args ...any) {
	defaultLogger.Warn(msg, args...)
}

// InfoContext logs an info level message with context
func InfoContext(ctx context.Context, msg string, args ...any) {
	defaultLogger.InfoContext(ctx, msg, args...)
}

// DebugContext logs a debug level message with context
func DebugContext(ctx context.Context, msg string, args ...any) {
	defaultLogger.DebugContext(ctx, msg, args...)
}

// ErrorContext logs an error level message with context
func ErrorContext(ctx context.Context, msg string, args ...any) {
	defaultLogger.ErrorContext(ctx, msg, args...)
}

// With returns a new logger with the given attributes
func With(args ...any) *slog.Logger {
	return defaultLogger.With(args...)
}

// MaskSecret masks a secret string for safe logging.
// If the secret is empty, returns "[none]".
// If the secret is 4 characters or less, returns "[***]".
// Otherwise, returns the first 2 characters followed by "***".
func MaskSecret(secret string) string {
	if secret == "" {
		return "[none]"
	}
	if len(secret) <= 4 {
		return "[***]"
	}
	return secret[:2] + "***"
}

// SecretAttr creates a slog attribute for a secret that masks the value
func SecretAttr(key, secret string) slog.Attr {
	return slog.String(key, MaskSecret(secret))
}
