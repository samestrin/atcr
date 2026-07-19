package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/report"
	"github.com/spf13/cobra"
)

// homeExecutable and homeUserDir are seams over os.Executable / os.UserHomeDir so
// tests can pin the binary path and home directory, making the home view's
// ~-relativized executable path deterministic.
var (
	homeExecutable = os.Executable
	homeUserDir    = os.UserHomeDir
)

// homeState is the resolved live review state the home view renders. It is a
// tri-state: no review has run yet (the first-run guidance line), a review is
// readable (id + status), or a pointer exists but the review it names could not
// be read (unavailable — reported honestly rather than masked as first-run).
type homeState struct {
	hasReview   bool
	unavailable bool
	reviewID    string
	status      string
}

// relHome renders path with the user's home-directory prefix replaced by "~"
// (axi.md Principle 8's example). It follows the same filepath.Rel-plus-fallback
// idiom the codebase already uses for path display (internal/tools/dispatch.go's
// relDisplay, internal/log/redact.go's relativizePaths): a path under home renders
// as "~/rel", the home dir itself as "~", and anything outside home (or when home
// can't be resolved) falls back to the verbatim path — never a broken "~/../.."
// substitution. The "~" form is ALWAYS forward-slash (~/rel), on every platform:
// the string feeds the agent-facing AXI payload, whose cross-platform TOON
// consumers parse the documented forward-slash shape.
func relHome(path string) string {
	home, err := homeUserDir()
	if err != nil || home == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~/" + filepath.ToSlash(rel)
}

// resolveHomeState resolves the current review's id/status via the same
// anchorDir("") + fanout.ReadReviewStatus path `atcr status` uses. It never
// errors — the home view must stay exit 0 (AC3) — but it distinguishes causes so
// it does not silently mask them:
//   - a genuinely absent .atcr/latest pointer (os.ErrNotExist) is the first-run
//     state: no review has run, render the guidance line. os.ReadFile follows
//     symlinks, so a DANGLING .atcr/latest symlink also surfaces as ErrNotExist;
//     Lstat (which does not follow the link) separates the two — a pointer that
//     exists but points nowhere is the unavailable state, never first-run;
//   - a present-but-corrupt/empty pointer, or a pointer naming a review that
//     cannot be read (pruned, corrupt manifest, permission), is reported as the
//     explicit "unavailable" state — NOT conflated with first-run, unlike a naive
//     fail-open — and the cause is logged at debug so the degrade is observable.
func resolveHomeState(ctx context.Context) homeState {
	dir, err := anchorDir("")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, lerr := os.Lstat(filepath.Join(".atcr", "latest")); lerr == nil {
				log.FromContext(ctx).Debug("home view: .atcr/latest is a dangling pointer", "err", err)
				return homeState{unavailable: true}
			}
			return homeState{} // no .atcr/latest at all: true first-run (AC3)
		}
		log.FromContext(ctx).Debug("home view: .atcr/latest pointer unreadable", "err", err)
		return homeState{unavailable: true}
	}
	st, err := fanout.ReadReviewStatus(dir, filepath.Base(dir))
	if err != nil {
		log.FromContext(ctx).Debug("home view: latest review status unreadable",
			"review", filepath.Base(dir), "err", err)
		return homeState{unavailable: true, reviewID: filepath.Base(dir)}
	}
	return homeState{hasReview: true, reviewID: st.ReviewID, status: st.Status}
}

// renderHomeView writes the non-axi home view: the ~-relativized executable path,
// atcr's one-line description, and the current review state — the latest review's
// id/status, an explicit no-reviews-yet line on a first run (AC1/AC3), or an
// honest "unavailable" line when a pointer exists but its review can't be read. It
// never returns a coded error: a first-run repo renders guidance, not a failure.
func renderHomeView(w io.Writer, execPath, description string, st homeState) error {
	// sanitizeDisplay strips terminal control bytes (ESC/C0/C1, line separators)
	// from the executable path — an attacker-controlled install path could otherwise
	// smuggle ANSI escapes to the terminal. This mirrors the control-byte safety the
	// AXI renderer already enforces via toonQuote, keeping the two renderers in parity.
	if _, err := fmt.Fprintln(w, sanitizeDisplay(relHome(execPath))); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, description); err != nil {
		return err
	}
	switch {
	case st.hasReview:
		_, err := fmt.Fprintf(w, "Latest review: %s (%s)\n", st.reviewID, st.status)
		return err
	case st.unavailable:
		if st.reviewID != "" {
			_, err := fmt.Fprintf(w, "Latest review %s is unavailable — its status could not be read.\n", st.reviewID)
			return err
		}
		_, err := fmt.Fprintln(w, ".atcr/latest pointer file is corrupted or unreadable — run `atcr review` to regenerate it.")
		return err
	default:
		_, err := fmt.Fprintln(w, "No reviews yet — run `atcr review` to start one.")
		return err
	}
}

// runHome renders the Content-First home view (axi.md Principle 8) for a bare
// `atcr` invocation — the case where the root command's RunE fires because no
// subcommand was given. It replaces the former cmd.Help() call. Cobra's
// -h/--help and --version short-circuit before RunE, so they are structurally
// unaffected; every subcommand keeps its own RunE.
func runHome(cmd *cobra.Command) error {
	ctx := cmd.Context()
	execPath, err := homeExecutable()
	if err != nil {
		// os.Executable rarely fails; fall back to the invocation name so the
		// home view still renders content rather than erroring (AC3: never
		// error), and log the cause at debug so the degrade is not silent.
		// os.Args[0] reflects how the binary was actually invoked — a renamed
		// or wrapped binary renders its real name; with no argv at all, the
		// command's own name is the last-ditch value, not a hardcoded literal.
		log.FromContext(ctx).Debug("home view: os.Executable failed, using argv/command-name fallback", "err", err)
		if len(os.Args) > 0 && os.Args[0] != "" {
			execPath = os.Args[0]
		} else {
			execPath = cmd.Name()
		}
	}
	st := resolveHomeState(ctx)

	// --axi renders the same home-view data as a token-dense TOON payload, read
	// from the context the root PersistentPreRunE populated from the root-local
	// --axi flag (axiFromContext) — the same context-propagation plumbing
	// review/resume reuse (Epic 31.0), not a parallel mode switch.
	if axiFromContext(ctx) {
		reviewID, status := "", "none"
		switch {
		case st.hasReview:
			reviewID, status = st.reviewID, st.status
		case st.unavailable:
			reviewID, status = st.reviewID, "unavailable"
		}
		return report.RenderHomeViewAXI(cmd.OutOrStdout(), report.HomeViewAXI{
			ExecPath:     relHome(execPath),
			Description:  cmd.Short,
			ReviewID:     reviewID,
			ReviewStatus: status,
		})
	}

	return renderHomeView(cmd.OutOrStdout(), execPath, cmd.Short, st)
}
