package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	goaxi "github.com/samestrin/go-axi"
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
	// PendingOnly offers the step only when the review is still pending.
	PendingOnly bool
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
	suggestReview: {
		{Command: "report", Args: []string{idPlaceholder}},
		{Command: "verify", Args: []string{idPlaceholder}},
	},
	suggestReviewInterrupted: {
		{Command: "status", Args: []string{idPlaceholder}},
		{Command: "review", Args: []string{"--resume", idPlaceholder}},
	},
	suggestReport: {
		{Command: "debt list"},
		{Command: "verify", Args: []string{idPlaceholder}, Carry: []string{"repo-root"}},
	},
	suggestReconcile: {
		{Command: "debt list"},
		{Command: "verify", Args: []string{idPlaceholder}, Carry: []string{"repo-root"}},
	},
	suggestVerify: {
		{Command: "report", Args: []string{idPlaceholder}},
	},
	suggestDebate: {
		{Command: "report", Args: []string{idPlaceholder}},
	},
	suggestStatus: {
		{Command: "report", Args: []string{idPlaceholder}},
		{Command: "review", Args: []string{"--resume", idPlaceholder}, PendingOnly: true},
	},
	suggestHome: {
		{Command: "review"},
		{Command: "report", Args: []string{idPlaceholder}},
	},
}

// invocation is what the renderer knows about the command that just ran.
type invocation struct {
	// Flags maps each flag the user set to its value (see invocationFlags).
	Flags map[string]string
	// Pending reports whether the review is still pending, gating PendingOnly.
	Pending bool
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
		if s.PendingOnly && !inv.Pending {
			continue
		}
		out = append(out, s.render(inv))
	}
	return out
}

// render builds one command line: binary, command, fixed args, then each Carry
// flag the invocation set, in Carry order.
func (s nextStep) render(inv invocation) string {
	parts := append([]string{"atcr", s.Command}, s.Args...)
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
