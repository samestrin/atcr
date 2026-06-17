package log

import (
	"context"
	"fmt"
	"log/slog"
)

// redactingHandler wraps a base slog.Handler and scrubs every emitted record
// through a Redactor before it reaches the sink: the record message, all string
// attribute values (including those bound earlier via WithAttrs and nested group
// values), and the string form of error-valued attributes. This makes redaction
// enforced-by-construction rather than opt-in per call site — no log line at any
// level can bypass it (AC5, AC6; the TD-007 enforcement model).
type redactingHandler struct {
	base slog.Handler
	r    *Redactor
}

// Enabled delegates to the base handler so level filtering is unchanged.
func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

// Handle rebuilds the record with a redacted message and redacted attribute
// values, then forwards it to the base handler.
func (h *redactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.Redact(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.redactAttr(a))
		return true
	})
	return h.base.Handle(ctx, out)
}

// WithAttrs redacts the pre-bound attribute values before they are stored on the
// base handler, so attributes attached once (e.g. agent_name) are scrubbed too.
func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = h.redactAttr(a)
	}
	return &redactingHandler{base: h.base.WithAttrs(red), r: h.r}
}

// WithGroup delegates, preserving the redacting wrapper for the grouped handler.
func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{base: h.base.WithGroup(name), r: h.r}
}

// redactAttr scrubs a single attribute's value. String values are redacted
// directly; error values are redacted by their message; group values recurse;
// every other kind (bool, int, time, ...) passes through unchanged. Attribute
// keys are never redacted — they are static field names, not secret-bearing.
func (h *redactingHandler) redactAttr(a slog.Attr) slog.Attr {
	// Correlation keys (review_id, agent_name) are internally-generated
	// identifiers, not secret-bearing, and AC9 requires them greppable verbatim.
	// Exempt their values from redaction so a correlation key that happens to look
	// secret-shaped (e.g. a branch slug starting "sk-") is not scrubbed when bound
	// on top of a redactor — the case once a root secret-redactor exists.
	if a.Key == AttrReviewID || a.Key == AttrAgentName {
		return a
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.r.Redact(v.String()))
	case slog.KindGroup:
		group := v.Group()
		red := make([]slog.Attr, len(group))
		for i, g := range group {
			red[i] = h.redactAttr(g)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(red...)}
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, h.r.Redact(err.Error()))
		}
		// Any other value (struct, slice, map, Stringer) could carry a secret in
		// its rendered form, which the base handler would marshal to the sink
		// verbatim. Render it the way slog's text handler would (%+v, preserving
		// field names), scrub that, and substitute the redacted string only when
		// redaction actually removed something — otherwise pass the original attr
		// through so non-secret values keep their native marshaling/type.
		rendered := fmt.Sprintf("%+v", v.Any())
		if red := h.r.Redact(rendered); red != rendered {
			return slog.String(a.Key, red)
		}
		return a
	default:
		return a
	}
}

// WithRedactor returns a logger whose every emitted record is scrubbed by r at
// the sink (message + string/error attribute values), composing with any
// attributes already attached (WithReviewID/WithAgent). It returns logger
// unchanged when logger or r is nil, so callers can wire it unconditionally.
func WithRedactor(logger *slog.Logger, r *Redactor) *slog.Logger {
	if logger == nil || r == nil {
		return logger
	}
	return slog.New(&redactingHandler{base: logger.Handler(), r: r})
}
