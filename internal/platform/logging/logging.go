// Package logging builds the structured JSON logger (slog to stderr).
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns a JSON slog.Logger writing to stderr at the level named by
// LOG_LEVEL (debug, info, warn, error; default info).
func New() *slog.Logger {
	return NewWithWriter(os.Stderr, os.Getenv("LOG_LEVEL"))
}

// NewWithWriter is New with an explicit writer and level name.
func NewWithWriter(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l}))
}
