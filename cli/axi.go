package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
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

// axiModeFromContext is axiFromContext that also reports whether the root
// PersistentPreRunE resolved a mode at all; it did not when the run failed
// before or inside it (an unknown flag or command, a bad --log-format).
func axiModeFromContext(ctx context.Context) (axi, ok bool) {
	if ctx == nil {
		return false, false
	}
	axi, ok = ctx.Value(axiContextKey{}).(bool)
	return axi, ok
}

// legacyPipeDeprecation is the stderr notice every legacy pipe AXI route emits.
// stdout stays payload-only, so the notice never corrupts what an agent parses.
const legacyPipeDeprecation = "warning: pipe-delimited AXI output is deprecated and will be removed in a future release; migrate to standard TOON."

// warnLegacyPipe writes the deprecation notice through the context logger, so
// it honors --log-format: json mode carries it as a structured NDJSON line
// instead of raw text an NDJSON consumer would choke on. trigger names the
// switch that selected legacy pipe ("--legacy-pipe", "ATCR_LEGACY_PIPE",
// "--format pipe"), so the user can tell how to turn it off. The logger's sink
// is the command's error writer, so stdout stays payload-only.
func warnLegacyPipe(ctx context.Context, trigger string) {
	log.FromContext(ctx).Warn(fmt.Sprintf("%s (enabled by %s)", legacyPipeDeprecation, trigger))
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

// agentEnvMarkers are environment variables an agent harness sets in the shells
// it runs. Any one set to a non-empty value puts atcr on the agent path (epic
// 165 T1), even when stdout is a terminal.
var agentEnvMarkers = []string{"CLAUDECODE", "CODEX", "OPENCODE"}

// outputIsTerminal reports whether f is an interactive terminal. A seam so tests
// can stand a pipe in for a terminal.
var outputIsTerminal = func(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// axiDefaultAnnotation marks a command without an --axi flag whose output still
// follows the resolved mode (report picks its default --format from it).
const axiDefaultAnnotation = "atcr.axi-default"

// hasAXIMode reports whether cmd has an AXI rendering, and so whether the
// agent-path default applies to it. Commands without one keep their text.
func hasAXIMode(cmd *cobra.Command) bool {
	return cmd.Flags().Lookup("axi") != nil || cmd.Annotations[axiDefaultAnnotation] != ""
}

// resolveAXIMode decides whether cmd renders AXI, first rule that applies wins:
//  1. --axi or --human given on the command line (both at once is a usage error);
//  2. an explicit non-AXI output choice: --json (debt commands) or --auto-fix
//     (review's interactive flow);
//  3. ATCR_FORMAT=axi or ATCR_FORMAT=text;
//  4. an agent marker in the environment (agentEnvMarkers): AXI;
//  5. stdout: AXI when it is a file that is not a terminal (a pipe, a redirect),
//     human on a terminal. A writer that is not a file (an embedder's or a test's
//     buffer) has no stdout to inspect and keeps the human output.
//
// An unrecognized ATCR_FORMAT warns on w and is ignored, like ATCR_LEGACY_PIPE.
func resolveAXIMode(cmd *cobra.Command, w io.Writer) (bool, error) {
	return resolveAXIModeFlags(cmd.Flags(), cmd.OutOrStdout(), w)
}

// resolveAXIModeFlags is resolveAXIMode over a parsed flag set and a stdout, so
// the error path in runMain can apply the same rules to a flag set it re-parsed
// from the raw arguments (see errorModeIsAXI).
func resolveAXIModeFlags(flags *pflag.FlagSet, stdout io.Writer, w io.Writer) (bool, error) {
	axiSet := flags.Lookup("axi") != nil && flags.Changed("axi")
	human := optionalBoolFlag(flags, "human")
	if axiSet {
		axi, _ := flags.GetBool("axi")
		if axi && human {
			return false, usageError(errors.New("--axi and --human are mutually exclusive"))
		}
		return axi, nil
	}
	if human || optionalBoolFlag(flags, "json") || optionalBoolFlag(flags, "auto-fix") {
		return false, nil
	}
	if raw := strings.TrimSpace(os.Getenv("ATCR_FORMAT")); raw != "" {
		switch strings.ToLower(raw) {
		case "axi":
			return true, nil
		case "text":
			return false, nil
		}
		_, _ = fmt.Fprintf(w, "warning: unrecognized ATCR_FORMAT value %q (want axi or text); using the default output\n", raw)
	}
	for _, k := range agentEnvMarkers {
		if os.Getenv(k) != "" {
			return true, nil
		}
	}
	f, ok := stdoutFile(stdout)
	return ok && !outputIsTerminal(f), nil
}

// stdoutFile returns the *os.File behind w, looking through writers that wrap
// one (runMain's payloadWriter), so wrapping stdout never hides the terminal.
func stdoutFile(w io.Writer) (*os.File, bool) {
	for {
		switch v := w.(type) {
		case *os.File:
			return v, true
		case interface{ Unwrap() io.Writer }:
			w = v.Unwrap()
		default:
			return nil, false
		}
	}
}

// optionalBoolFlag is boolFlag for a flag the set may not define: absent is false.
func optionalBoolFlag(flags *pflag.FlagSet, name string) bool {
	if flags.Lookup(name) == nil {
		return false
	}
	v, _ := flags.GetBool(name)
	return v
}
