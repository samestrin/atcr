package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

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

// legacyPipeDeprecation is the stderr notice every legacy pipe AXI route emits.
// stdout stays payload-only, so the notice never corrupts what an agent parses.
const legacyPipeDeprecation = "warning: pipe-delimited AXI output is deprecated and will be removed in a future release; migrate to standard TOON."

// warnLegacyPipe writes the deprecation notice. trigger names the switch that
// selected legacy pipe ("--legacy-pipe", "ATCR_LEGACY_PIPE", "--format pipe"),
// so the user can tell how to turn it off. Write errors are ignored: a broken
// stderr must not fail a run whose payload is on stdout.
func warnLegacyPipe(w io.Writer, trigger string) {
	_, _ = fmt.Fprintf(w, "%s (enabled by %s)\n", legacyPipeDeprecation, trigger)
}

// legacyPipeFromEnv reports whether ATCR_LEGACY_PIPE requests the legacy pipe
// encoder. It is a global switch over every AXI surface (report --format axi,
// review --axi, bare atcr --axi); non-AXI output ignores it. Any value
// strconv.ParseBool accepts as true enables it; anything else is off. An
// unparseable non-empty value warns on stderr and fails open to standard TOON,
// so a typo is visible rather than silently ignored (matching
// axiMaxLinesFromEnv).
func legacyPipeFromEnv(w io.Writer) bool {
	raw, ok := os.LookupEnv("ATCR_LEGACY_PIPE")
	if !ok || strings.TrimSpace(raw) == "" {
		return false
	}
	on, err := strconv.ParseBool(raw)
	if err != nil {
		_, _ = fmt.Fprintf(w, "warning: unrecognized ATCR_LEGACY_PIPE value %q; standard TOON output is in effect\n", raw)
		return false
	}
	return on
}

// legacyPipeContextKey carries the resolved legacy-pipe choice (--legacy-pipe or
// ATCR_LEGACY_PIPE) alongside the --axi mode, through the same single
// flag-parse point.
type legacyPipeContextKey struct{}

// newLegacyPipeContext returns ctx carrying the resolved legacy-pipe choice.
func newLegacyPipeContext(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, legacyPipeContextKey{}, enabled)
}

// legacyPipeFromContext reports whether --axi output should use the legacy pipe
// encoder. It falls back to false when the value is absent.
func legacyPipeFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(legacyPipeContextKey{}).(bool)
	return v
}
