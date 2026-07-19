package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
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

// homeState is the resolved live review state the home view renders: whether any
// review has run yet and, if so, its id and status.
type homeState struct {
	hasReview bool
	reviewID  string
	status    string
}

// relHome renders path with the user's home-directory prefix replaced by "~"
// (axi.md Principle 8's example). It follows the same filepath.Rel-plus-fallback
// idiom the codebase already uses for path display (internal/tools/dispatch.go's
// relDisplay, internal/log/redact.go's relativizePaths): a path under home renders
// as "~/rel", the home dir itself as "~", and anything outside home (or when home
// can't be resolved) falls back to the verbatim path — never a broken "~/../.."
// substitution.
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
	return "~" + string(filepath.Separator) + rel
}

// resolveHomeState resolves the current review's id/status via the same
// anchorDir("") + fanout.ReadReviewStatus path `atcr status` uses. A missing
// .atcr/latest pointer (anchorDir error) is the first-run "no reviews yet" state
// (AC3) — NOT a usage error, deliberately unlike status.go which wraps the same
// condition in usageError (exit 2). Any status-read failure likewise degrades to
// the no-review state so the home view can never error or emit empty output.
func resolveHomeState() homeState {
	dir, err := anchorDir("")
	if err != nil {
		return homeState{hasReview: false}
	}
	st, err := fanout.ReadReviewStatus(dir, filepath.Base(dir))
	if err != nil {
		return homeState{hasReview: false}
	}
	return homeState{hasReview: true, reviewID: st.ReviewID, status: st.Status}
}

// renderHomeView writes the non-axi home view: the ~-relativized executable path,
// atcr's one-line description, and the current review state — or, when no review
// has run yet, an explicit no-reviews-yet line (AC1/AC3). It never returns a
// coded error: a first-run repo renders guidance, not a failure.
func renderHomeView(w io.Writer, execPath, description string, st homeState) error {
	if _, err := fmt.Fprintln(w, relHome(execPath)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, description); err != nil {
		return err
	}
	if st.hasReview {
		_, err := fmt.Fprintf(w, "Latest review: %s (%s)\n", st.reviewID, st.status)
		return err
	}
	_, err := fmt.Fprintln(w, "No reviews yet — run `atcr review` to start one.")
	return err
}

// runHome renders the Content-First home view (axi.md Principle 8) for a bare
// `atcr` invocation — the case where the root command's RunE fires because no
// subcommand was given. It replaces the former cmd.Help() call. Cobra's
// -h/--help and --version short-circuit before RunE, so they are structurally
// unaffected; every subcommand keeps its own RunE.
func runHome(cmd *cobra.Command) error {
	execPath, err := homeExecutable()
	if err != nil {
		// os.Executable rarely fails; fall back to the command name so the home
		// view still renders content rather than erroring (AC3: never error).
		execPath = "atcr"
	}
	st := resolveHomeState()

	// --axi renders the same home-view data as a token-dense TOON payload, read
	// from the context the root PersistentPreRunE populated from the root-local
	// --axi flag (axiFromContext) — the same context-propagation plumbing
	// review/resume reuse (Epic 31.0), not a parallel mode switch.
	if axiFromContext(cmd.Context()) {
		reviewID, status := "", "none"
		if st.hasReview {
			reviewID, status = st.reviewID, st.status
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
