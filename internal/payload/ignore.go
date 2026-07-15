package payload

import "log/slog"

// ignoreMatcher stub — RED stage. Replaced with the real implementation in GREEN.
type ignoreMatcher struct{}

func newIgnoreMatcher(_ string, _ *slog.Logger) *ignoreMatcher { return &ignoreMatcher{} }

func (m *ignoreMatcher) active() bool { return false }

func (m *ignoreMatcher) match(_ string) bool { return false }
