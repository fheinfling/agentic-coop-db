// Package observability wires structured logging, metrics, and (optional)
// tracing for the AIColDB gateway.
//
// Logging uses the standard library log/slog. Metrics use the prometheus
// client_golang collector registry — see metrics.go for the registered
// instruments. OpenTelemetry is optional and only initialised when
// AICOLDB_OTEL_EXPORTER_OTLP_ENDPOINT is set.
package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger returns a slog.Logger configured for the given level and format.
// "json" is the default and the only format supported by the agreed log
// pipeline; "text" is supported for local development only.
func NewLogger(level, format string) *slog.Logger {
	return newLogger(os.Stdout, level, format)
}

func newLogger(w io.Writer, level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl, AddSource: false}
	var h slog.Handler
	if strings.ToLower(format) == "text" {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(h).With("service", "aicoldb")
}

// FromContext returns a request-scoped logger if one was attached, otherwise
// the default logger.
type ctxKey int

const loggerKey ctxKey = 0

// WithLogger attaches the logger to ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromContext returns a logger; never nil.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
