// Package logging builds the logger of a command run and carries it in a context.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
)

type contextKey struct{}

// New returns a logger writing to w with the given level and format.
func New(w io.Writer, level, format string) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{}
	switch level {
	case "debug":
		opts.Level = slog.LevelDebug
	case "info":
		opts.Level = slog.LevelInfo
	case "warn":
		opts.Level = slog.LevelWarn
	case "error":
		opts.Level = slog.LevelError
	default:
		return nil, fmt.Errorf("unknown log level: %s", level)
	}

	switch format {
	case "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("unknown log format: %s", format)
	}
}

// NewContext returns a copy of ctx carrying logger.
func NewContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext returns the logger carried by ctx, or slog.Default if none.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
