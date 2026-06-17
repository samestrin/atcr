package log

import "log/slog"

// Correlation attribute keys attached to log lines so concurrent reviews and
// agents can be traced by grep. Exported for downstream packages (cmd/atcr,
// internal/fanout) that assert on or filter by these keys.
const (
	AttrReviewID  = "review_id"
	AttrAgentName = "agent_name"
)

// WithReviewID returns a logger that includes review_id on every log line. It
// uses slog.Logger.With, which is immutable and returns a new logger without
// mutating the input. A nil logger returns nil so callers can chain safely and
// nil-check once at the end.
//
// Call once per key on a base logger: slog APPENDS attributes (it does not
// replace), so applying this to a logger that already carries review_id emits
// the key twice. The intended wiring (review.go attaches review_id once;
// fanout attaches agent_name once) never double-wraps.
func WithReviewID(logger *slog.Logger, reviewID string) *slog.Logger {
	if logger == nil {
		return nil
	}
	return logger.With(AttrReviewID, reviewID)
}

// WithAgent returns a logger that includes agent_name on every log line. Like
// WithReviewID it is immutable and nil-safe. Called once per agent invocation
// in the fanout loop.
func WithAgent(logger *slog.Logger, agentName string) *slog.Logger {
	if logger == nil {
		return nil
	}
	return logger.With(AttrAgentName, agentName)
}
