package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	goaxi "github.com/samestrin/go-axi"
	toon "github.com/toon-format/toon-go"
)

// The next-step suggestion registry (Epic 42.0, axi.md Principle 9 "Contextual
// Disclosure"). After a command finishes, an agent driving the CLI without the
// installed Skill has no built-in signal for what to run next; the registry is
// the one explicit map from "what just ran" to the commands that usually follow.
//
// A suggestion is a template, not a guess: runtime values the CLI cannot know
// for certain (the review id) stay literal placeholders such as <id>, and only
// the disambiguating flags on carryAllowlist carry their typed value forward.
// Any other carried flag renders as --flag <flag>, so a user-typed value never
// reaches agent-facing output unless it was allowlisted and sanitized (AC5).

// idPlaceholder is the literal token a template uses for the review id or path
// that every review-scoped command takes as its positional argument.
const idPlaceholder = "<id>"

// suggestionKey names what just ran: a command, or a command in a specific
// outcome (an interrupted review) whose next steps differ.
type suggestionKey string

const (
	suggestReview            suggestionKey = "review"
	suggestReviewInterrupted suggestionKey = "review:interrupted"
	suggestReport            suggestionKey = "report"
	suggestReconcile         suggestionKey = "reconcile"
	suggestVerify            suggestionKey = "verify"
	suggestDebate            suggestionKey = "debate"
	suggestStatus            suggestionKey = "status"
	suggestHome              suggestionKey = "home"
)

// nextStep is one candidate next command.
type nextStep struct {
	// Command is the subcommand path without the binary name ("debt list").
	Command string
	// Args are fixed tokens rendered verbatim after Command: placeholders such
	// as <id>, and fixed flags such as --resume.
	Args []string
	// Carry names flags of the current invocation to forward to this command
	// when the user set them. Allowlisted flags carry their sanitized value;
	// any other flag renders as --flag <flag>.
	Carry []string
	// StoreFrom names an allowlisted repo-root flag of the current invocation
	// from which to derive --store=<absolute root>/.atcr/debt, for a debt
	// command that should read the store that root's findings persisted to.
	StoreFrom string
	// ResumableOnly offers the step only when the review still has agents to
	// run: it is pending, or it failed (review --resume reruns failed agents).
	ResumableOnly bool
	// ReconciledOnly offers the step only when the review has reconciled data
	// (reconciled/findings.json), which report and verify read and refuse
	// without.
	ReconciledOnly bool
	// UnreconciledOnly offers the step only when the review settled without
	// reconciled data: not reconciled, nothing pending and not failed.
	UnreconciledOnly bool
	// CarryRange appends the invocation's ResumeRange, for the resume step.
	CarryRange bool
}

// carryAllowlist holds the disambiguating flags whose value may be carried into
// a suggestion: they pick WHICH repo a command acts on, so dropping them would
// point the next command at the wrong tree. Behavioral flags (--min-severity,
// --thorough, ...) are a choice the next run should make again, not carry.
var carryAllowlist = map[string]bool{
	"repo-root": true,
}

// suggestionRegistry maps what just ran to its ordered candidate next steps.
// review --resume is a flag on review, not a standalone `atcr resume` command.
var suggestionRegistry = map[suggestionKey][]nextStep{
	// A review reconciles in-process only under a gate threshold (--fail-on or
	// config fail_on) or --verify/--debate/--auto-fix; otherwise report and
	// verify would exit 2 until `atcr reconcile` runs, so that comes first.
	// A review whose agents all failed exits 1 but still gets its next steps:
	// status names the failures, and resume reruns the failed agents.
	suggestReview: {
		{Command: "reconcile", Args: []string{idPlaceholder}, UnreconciledOnly: true},
		{Command: "report", Args: []string{idPlaceholder}, ReconciledOnly: true},
		{Command: "verify", Args: []string{idPlaceholder}, ReconciledOnly: true},
		{Command: "status", Args: []string{idPlaceholder}, ResumableOnly: true},
		{Command: "review", Args: []string{"--resume", idPlaceholder}, ResumableOnly: true, CarryRange: true},
	},
	suggestReviewInterrupted: {
		{Command: "status", Args: []string{idPlaceholder}},
		{Command: "review", Args: []string{"--resume", idPlaceholder}, CarryRange: true},
	},
	// report takes no --repo-root, so there is nothing to carry into verify.
	suggestReport: {
		{Command: "debt list"},
		{Command: "verify", Args: []string{idPlaceholder}},
	},
	// reconcile --repo-root X persists to X/.atcr/debt, which a bare debt list
	// run from another directory would not read.
	suggestReconcile: {
		{Command: "debt list", StoreFrom: "repo-root"},
		{Command: "verify", Args: []string{idPlaceholder}, Carry: []string{"repo-root"}},
	},
	suggestVerify: {
		{Command: "report", Args: []string{idPlaceholder}},
	},
	suggestDebate: {
		{Command: "report", Args: []string{idPlaceholder}},
	},
	suggestStatus: {
		{Command: "reconcile", Args: []string{idPlaceholder}, UnreconciledOnly: true},
		{Command: "report", Args: []string{idPlaceholder}, ReconciledOnly: true},
		{Command: "review", Args: []string{"--resume", idPlaceholder}, ResumableOnly: true, CarryRange: true},
	},
	// report reads the latest review's reconciled data, so a first run, an
	// unreadable pointer and an unreconciled review get only review.
	suggestHome: {
		{Command: "review"},
		{Command: "report", Args: []string{idPlaceholder}, ReconciledOnly: true},
	},
}

// invocation is what the renderer knows about the command that just ran.
type invocation struct {
	// Flags maps each flag the user set to its value (see invocationFlags).
	Flags map[string]string
	// Pending reports whether the review is still pending.
	Pending bool
	// Failed reports whether the review ended failed.
	Failed bool
	// Reconciled reports whether the review has reconciled data
	// (reviewReconciled).
	Reconciled bool
	// ResumeRange holds the range flags a resume of the review must re-pass
	// (recordedRangeFlags); rendered only on CarryRange steps.
	ResumeRange []string
}

// recordedRangeFlags returns the flags that make `review --resume` re-derive the
// range the review in dir recorded in manifest.json. A resume aborts when the
// range it resolves differs from the recorded one (fanout.ValidateResumeRange),
// and a bare resume resolves the auto-detected range, so a review of an
// explicit (--base/--head) or --merge-commit range must name it again. An
// auto-detected range, a baseline scan and an unreadable manifest carry none.
func recordedRangeFlags(dir string) []string {
	if dir == "" {
		return nil
	}
	m, err := fanout.ReadManifest(dir)
	if err != nil || m.Baseline || m.Base == "" || m.Head == "" {
		return nil
	}
	switch m.DetectionMode {
	case gitrange.ModeExplicit:
		return []string{"--base=" + carryValue(m.Base), "--head=" + carryValue(m.Head)}
	case gitrange.ModeMergeCommit:
		return []string{"--merge-commit=" + carryValue(m.Head)}
	}
	return nil
}

// reviewReconciled reports whether reconcile ran for the review in reviewDir:
// reconciled/findings.json, the file report and verify read, is a regular file.
// The reconciled/ directory alone proves nothing, since every review is
// scaffolded with it.
func reviewReconciled(reviewDir string) bool {
	if reviewDir == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(reviewDir, "reconciled", reconcile.FindingsJSON))
	return err == nil && fi.Mode().IsRegular()
}

// offered reports whether s applies to inv.
func (s nextStep) offered(inv invocation) bool {
	switch {
	case s.ResumableOnly && !inv.Pending && !inv.Failed:
		return false
	case s.ReconciledOnly && !inv.Reconciled:
		return false
	case s.UnreconciledOnly && (inv.Reconciled || inv.Pending || inv.Failed):
		return false
	}
	return true
}

// invocationFlags returns the flags the user explicitly set on cmd, by name,
// with their string values. Flags left at their default are not "used" and so
// are never carried forward. A deprecated alias is reported under its canonical
// name, and the canonical flag wins when both are set, matching how the
// commands themselves resolve them (cli/reconcile.go).
func invocationFlags(cmd *cobra.Command) map[string]string {
	out := map[string]string{}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		name := f.Name
		if canon, ok := deprecatedFlagAliases[name]; ok {
			if cmd.Flags().Changed(canon) {
				return
			}
			name = canon
		}
		out[name] = f.Value.String()
	})
	return out
}

// deprecatedFlagAliases maps a deprecated flag name to the canonical flag it
// stands in for, so a value typed under the old name still carries forward.
var deprecatedFlagAliases = map[string]string{
	"repo": "repo-root",
}

// renderSuggestions renders key's next steps as runnable command lines for
// inv, in registry order. An unknown key renders nothing.
func renderSuggestions(key suggestionKey, inv invocation) []string {
	var out []string
	for _, s := range suggestionRegistry[key] {
		if !s.offered(inv) {
			continue
		}
		out = append(out, s.render(inv))
	}
	return out
}

// render builds one command line: binary, command, fixed args, the recorded
// range (CarryRange), the derived --store, then each Carry flag the invocation set, in Carry order.
func (s nextStep) render(inv invocation) string {
	parts := append([]string{"atcr", s.Command}, s.Args...)
	if s.CarryRange {
		parts = append(parts, inv.ResumeRange...)
	}
	// A blank root asserts nothing (reconcile normalizes it to the CWD), so it
	// derives no store. A relative root is made absolute, as persistence does:
	// debt list refuses a --store containing "..".
	//
	// The store is the one localdebt.StoreDir resolves for that root, as
	// persistence does, so a configured debt.store is the store suggested. An
	// invalid setting suggests no --store rather than a wrong one: the bare
	// `debt list` resolves the setting itself and reports the mistake.
	if v, ok := inv.Flags[s.StoreFrom]; ok && strings.TrimSpace(v) != "" {
		if abs, err := filepath.Abs(v); err == nil {
			v = abs
		}
		if store, err := localdebt.StoreDir(v); err == nil {
			parts = append(parts, "--store="+carryValue(store))
		}
	}
	for _, name := range s.Carry {
		v, ok := inv.Flags[name]
		if !ok {
			continue
		}
		if !carryAllowlist[name] {
			parts = append(parts, "--"+name, "<"+name+">")
			continue
		}
		parts = append(parts, "--"+name+"="+carryValue(v))
	}
	return strings.Join(parts, " ")
}

// carryValue makes a user-typed flag value safe to emit: goaxi.SanitizeString
// drops control bytes and invalid UTF-8 (as axiText does for AXI payload text),
// the line breaks and tabs it keeps become spaces so a suggestion stays one
// line, and anything that is not a plain shell word is single-quoted so the
// line runs as shown and cannot be mistaken for a <placeholder>.
func carryValue(v string) string {
	v = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, goaxi.SanitizeString(v))
	if v != "" && strings.IndexFunc(v, func(r rune) bool { return !shellPlain(r) }) < 0 {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// shellPlain reports whether r needs no quoting in a POSIX shell word.
func shellPlain(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune("/._-:,@+=%", r)
}

// writeNextSteps prints key's next steps as human "Next: <command>" lines, for
// the commands with no AXI mode. w is the command's stderr, so stdout stays
// exactly what it was before suggestions existed.
func writeNextSteps(w io.Writer, key suggestionKey, inv invocation) {
	for _, line := range renderSuggestions(key, inv) {
		_, _ = fmt.Fprintln(w, "Next: "+line)
	}
}

// writeNextLines is writeNextSteps on cmd's stderr.
func writeNextLines(cmd *cobra.Command, key suggestionKey, inv invocation) {
	writeNextSteps(cmd.ErrOrStderr(), key, inv)
}

// writeAXIOrNextSteps closes a command that has an --axi mode but no legacy pipe
// encoder. Under --axi, stdout gets payload as one TOON object and then key's
// next steps as a help[] block, and no Next: line is written anywhere. Otherwise
// the command has already printed its human output, so only the Next: lines
// follow (writeNextStepsCmd), exactly as before --axi existed.
func writeAXIOrNextSteps(cmd *cobra.Command, payload toon.Object, key suggestionKey, inv invocation) error {
	if !axiFromContext(cmd.Context()) {
		writeNextStepsCmd(cmd, key, inv)
		return nil
	}
	return writeAXIHelp(cmd.OutOrStdout(), payload, renderSuggestions(key, inv))
}

// writeAXIHelp encodes payload as standard TOON, then appends lines as a help[]
// block. The two decode together as one value carrying both.
func writeAXIHelp(w io.Writer, payload toon.Object, lines []string) error {
	if _, err := goaxi.EncodeChecked(w, payload); err != nil {
		return fmt.Errorf("axi encoder: %w", err)
	}
	if err := goaxi.WriteHelp(w, lines); err != nil {
		return fmt.Errorf("axi encoder: %w", err)
	}
	return nil
}

// writeNextStepsCmd prints key's Next: lines on cmd's stderr. Under
// --log-format json each line rides the context logger instead (the
// warnLegacyPipe pattern), so stderr stays NDJSON-only.
func writeNextStepsCmd(cmd *cobra.Command, key suggestionKey, inv invocation) {
	if !jsonLogFormat(cmd) {
		writeNextSteps(cmd.ErrOrStderr(), key, inv)
		return
	}
	logger := log.FromContext(cmd.Context())
	for _, line := range renderSuggestions(key, inv) {
		logger.Warn("Next: " + line)
	}
}
