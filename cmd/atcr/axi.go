package main

import "context"

// axiContextKey is the unexported context key under which the --axi output mode
// travels from the root PersistentPreRunE (the single flag-parse point) to every
// stdout call site in review.go/resume.go. Mirroring log.FromContext /
// telemetry.FromContext keeps one propagation mechanism for the whole command
// tree, so a future third call site inherits correct behavior without a second,
// independent flag lookup (AC 01-04).
type axiContextKey struct{}

// newAXIContext returns ctx carrying the resolved --axi output mode.
func newAXIContext(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, axiContextKey{}, enabled)
}

// axiFromContext reports whether --axi token-dense output mode is active for this
// invocation. It falls back to false when the value is absent, so every non-axi
// command path (and any command that never registered the flag) is unaffected.
func axiFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(axiContextKey{}).(bool)
	return v
}
