// Package log is the single diagnostic sink for ATCR. It wraps log/slog to
// provide a consistent logger construction API, context-based propagation, and
// request correlation helpers. Production code constructs one logger in
// cmd/atcr and threads it through context; packages never rely on the slog
// package-global default logger.
package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// discardLogger is a shared no-op logger returned whenever no logger is
// available in a context. It is cached at package level so the common
// FromContext miss path allocates nothing.
var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// contextKey is an unexported type used as the context value key. Using a
// dedicated struct type (rather than a string) guarantees no collision with
// keys set by other packages.
type contextKey struct{}

var loggerKey = contextKey{}

// LevelFromString parses a textual log level into a slog.Level. Accepted
// values are debug, info, warn, and error (case-insensitive, surrounding
// whitespace ignored). An empty string defaults to info. Any other value
// returns an error naming the offending input.
func LevelFromString(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("log: invalid level %q (want debug, info, warn, or error)", s)
	}
}

// New constructs a *slog.Logger writing to w at the given level and format.
// Level accepts the values parsed by LevelFromString. Format accepts "text"
// (the default when empty) or "json"; any other value returns an error. The
// caller owns w (typically os.Stderr in cmd/atcr).
func New(level string, format string, w io.Writer) (*slog.Logger, error) {
	if w == nil {
		return nil, fmt.Errorf("log: nil writer")
	}
	lvl, err := LevelFromString(level)
	if err != nil {
		return nil, err
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		handler = slog.NewTextHandler(w, opts)
	case "json":
		handler = slog.NewJSONHandler(w, opts)
	default:
		return nil, fmt.Errorf("log: invalid format %q (want text or json)", format)
	}

	return slog.New(handler), nil
}

// NewContext returns a copy of ctx carrying logger. A nil ctx is treated as
// context.Background() so callers need not guard against it.
func NewContext(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext returns the logger stored by NewContext. When ctx is nil, carries
// no logger, or carries a nil logger, it returns a shared discard logger so
// callers can always log without a nil check.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return discardLogger
}
