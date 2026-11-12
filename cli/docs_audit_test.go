package cli

// Documentation-audit regression guards (epic 15.0). These tests treat the
// compiled command tree and the real config schema as the source of truth and
// assert that the prose in docs/ (plus the root README.md) never drifts away
// from it. They exist so a future rename/removal that forgets the docs fails CI
// instead of shipping stale instructions to the website build that consumes
// docs/ as its single source of truth.
//
// Note: the root README.md is intentionally included in the audited set. That
// means edits to README.md are also subject to the command/flag and
// config-name/reconciler-name guards below, so a casual prose change that
// mentions the fictional "atcr.yaml" or "Reconciler v2" will fail CI.

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/gitexec"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/telemetry"
	"github.com/samestrin/atcr/internal/telemetrytest"
	"github.com/samestrin/atcr/internal/verify"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// repoRootDir ascends from the test working directory (the package dir,
// cli) until it finds go.mod, returning the repository root.
func repoRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// auditedMarkdown returns the set of markdown files this epic owns as the
// documentation source of truth: every .md under docs/ (decision records in
// docs/decisions/ included, epic 111) plus the root README.md. testdata
// fixtures and READMEs elsewhere in the tree are intentionally out of scope
// (epic 15.0 clarifications). CHANGELOG.md is out of scope too: it is a
// historical record, and an entry that says a flag was removed must keep
// naming it.
func auditedMarkdown(t *testing.T) map[string]string {
	t.Helper()
	root := repoRootDir(t)
	out := map[string]string{}
	var docs []string
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			docs = append(docs, p)
		}
		return err
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
	paths := append(docs, filepath.Join(root, "README.md"))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = string(b)
	}
	return out
}

// atcrInvocations returns the token lists (the words following "atcr") for every
// `atcr ...` invocation appearing inside an inline code span or a fenced code
// block whose content begins with atcr (optionally behind a `$ ` shell prompt).
// Anchoring to the start of a span/line keeps ordinary prose (e.g. the sentence
// "atcr is local-first") and cross-line adjacencies from registering as bogus
// references. Both ``` and ~~~ GFM fences are recognized.
//
// cmds is the set of real command names from the compiled tree; it is used to
// filter fenced-block lines so that prose ("atcr is ...") is not misread as a
// command reference. Fenced lines are captured only when they begin with a
// shell prompt or when the first token after "atcr" is a known command or flag.
func atcrInvocations(md string, cmds map[string]bool) [][]string {
	var out [][]string
	isCommandLike := func(s string) bool {
		fields := strings.Fields(s)
		if len(fields) < 2 {
			return false
		}
		first := fields[1]
		return strings.HasPrefix(first, "-") || cmds[first]
	}
	capture := func(s string) {
		s = strings.TrimSpace(s)
		s = strings.TrimSpace(strings.TrimPrefix(s, "$ "))
		if !strings.HasPrefix(s, "atcr ") {
			return
		}
		if fields := strings.Fields(s); len(fields) > 1 {
			out = append(out, fields[1:]) // drop the leading "atcr"
		}
	}
	for _, m := range regexp.MustCompile("`([^`\n]+)`").FindAllStringSubmatch(md, -1) {
		capture(m[1])
	}
	inFence := false
	for _, ln := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			// Fenced prose (e.g. "atcr is local-first") should not register as a
			// command reference. Only capture shell-prompt lines or lines whose
			// first token after "atcr" is a real command or flag.
			if strings.HasPrefix(trimmed, "$ ") || isCommandLike(trimmed) {
				capture(ln)
			}
		}
	}
	return out
}

// canonicalCommands walks the whole command tree and returns every command name
// (top-level and nested), plus cobra's auto-registered help/completion.
func canonicalCommands() map[string]bool {
	names := map[string]bool{"help": true, "completion": true}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			names[sub.Name()] = true
			walk(sub)
		}
	}
	walk(NewRootCmd())
	return names
}

// commandGroups maps each command that owns subcommands to the set of its direct
// child command names, so a nested verb (e.g. the "run" in `atcr benchmark run`)
// can be validated rather than only the first token.
func commandGroups() map[string]map[string]bool {
	groups := map[string]map[string]bool{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		children := c.Commands()
		if len(children) > 0 {
			set := map[string]bool{}
			for _, sub := range children {
				set[sub.Name()] = true
			}
			groups[c.Name()] = set
		}
		for _, sub := range children {
			walk(sub)
		}
	}
	walk(NewRootCmd())
	return groups
}

// canonicalFlags walks the whole command tree and returns every long flag name
// registered on any command (local + persistent).
func canonicalFlags() map[string]bool {
	// help (every command) and version (root) are cobra-managed universal flags
	// added lazily during Execute, so they never appear in the static flag sets
	// walked below; seed them explicitly.
	flags := map[string]bool{"help": true, "version": true}
	add := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) { flags[f.Name] = true })
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		add(c.LocalFlags())
		add(c.PersistentFlags())
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRootCmd())
	return flags
}

// reachableFlags returns every long flag usable on the specific atcr command
// addressed by an invocation's leading bare-word tokens: the resolved (deepest)
// command's local flags plus every ancestor command's persistent flags (cobra's
// inheritance), plus the universal --help (every command) and the root-only
// --version. This is per-command, so a flag defined on `benchmark run` (e.g.
// --checkpoint) is NOT considered valid on `review`, unlike the global union
// returned by canonicalFlags().
func reachableFlags(tokens []string) map[string]bool {
	flags := map[string]bool{"help": true}
	cur := NewRootCmd()
	chain := []*cobra.Command{cur}
	for _, tok := range tokens {
		if strings.HasPrefix(tok, "-") {
			break // stop at the first flag; remaining tokens are not subcommands
		}
		var next *cobra.Command
		for _, sub := range cur.Commands() {
			if sub.Name() == tok {
				next = sub
				break
			}
		}
		if next == nil {
			break // token is not a real subcommand of cur; stop resolving
		}
		cur = next
		chain = append(chain, cur)
	}
	add := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) { flags[f.Name] = true })
	}
	for i, c := range chain {
		add(c.PersistentFlags()) // ancestors contribute inherited persistent flags
		if i == len(chain)-1 {
			add(c.LocalFlags()) // the resolved command contributes its local flags
		}
	}
	// --version is registered only on the root command (added lazily by cobra when
	// Version is set); it is not inherited by subcommands. A doc that writes
	// `atcr --version` resolves to root (no bare-word command token → chain len 1).
	if len(chain) == 1 {
		flags["version"] = true
	}
	return flags
}

// validateSubcommandChain recursively validates that bare-word tokens following
// a command group are real subcommands, skipping any --flags that appear before
// the next subcommand. It reports at most one error per level.
func validateSubcommandChain(tokens []string, idx int, groups map[string]map[string]bool, leaves map[string]bool, isWord func(string) bool, reFlag *regexp.Regexp, path string, errs *[]string) {
	name := tokens[idx]
	children, isGroup := groups[name]
	if !isGroup || len(tokens) <= idx+1 {
		return
	}
	nextIdx := -1
	for i := idx + 1; i < len(tokens); i++ {
		if endsInvocation(tokens[i], leaves[name]) {
			return
		}
		if reFlag.MatchString(tokens[i]) {
			continue
		}
		if isWord(tokens[i]) {
			nextIdx = i
			break
		}
	}
	if nextIdx == -1 {
		return
	}
	next := tokens[nextIdx]
	if !children[next] {
		*errs = append(*errs, fmt.Sprintf("%s references `atcr %s %s` but %q is not a subcommand of %q", path, name, next, next, name))
		return
	}
	validateSubcommandChain(tokens, nextIdx, groups, leaves, isWord, reFlag, path, errs)
}

// endsLine reports whether a token ends the current command's argv for SHELL
// reasons — the rest of the line is a comment, a redirect, or a different
// command. This holds for every command, group or leaf, and so is applied
// unconditionally:
//
//   - a trailing shell comment — `atcr verify <id> --exec  # standalone verify`
//     read "standalone" as a subcommand of verify
//   - a second invocation on one line — `atcr verify → atcr debate` (a pipeline
//     diagram) read "atcr" as a subcommand of verify
//   - a pipe, operator, or redirect — everything after belongs to another process
func endsLine(tok string) bool {
	switch {
	case strings.HasPrefix(tok, "#"): // trailing shell comment
		return true
	case tok == "atcr": // a second invocation on the same line
		return true
	case tok == "|", tok == "&&", tok == "||", tok == ";":
		return true
	case strings.HasPrefix(tok, ">"): // shell redirect — `>`, `>>`, and the spaceless shape docs actually use (`>/dev/null 2>&1`)
		return true
	}
	return false
}

// isPositionalPlaceholder reports whether a token is a documentation placeholder
// standing in for an argument value (`<file>`, `[id-or-path]`).
func isPositionalPlaceholder(tok string) bool {
	return strings.HasPrefix(tok, "<") || strings.HasPrefix(tok, "[")
}

// endsInvocation reports whether a token terminates the argv of the command
// currently being validated, so the subcommand scan must stop rather than read
// the next bare word as a subcommand name.
//
// The placeholder half is scoped by groupTakesPositional, and that scoping is the
// whole point. A command can be BOTH a group and a leaf: `atcr verify` takes an
// optional [id-or-path] AND owns the `diff` subcommand. There a placeholder is
// precisely the leaf usage, so nothing after it can be a subcommand and stopping
// is correct.
//
// For a PURE group — `debt`, `benchmark`, `config`, `models`, `personas`, `skill`
// — there is no positional to stand in for, so a placeholder occupies the
// SUBCOMMAND slot and the next bare word must still be validated. Applying the
// relaxation to every group (as this did when it was introduced for verify) left
// seven of the eight groups unvalidated: `atcr benchmark <sub> frobnicate` and
// `atcr debt <file> frobnicate` both scored clean.
func endsInvocation(tok string, groupTakesPositional bool) bool {
	if endsLine(tok) {
		return true
	}
	return groupTakesPositional && isPositionalPlaceholder(tok)
}

// groupLeafCommands returns the group commands that ALSO accept a positional
// argument of their own — the group-and-leaf commands whose placeholder stop is
// legitimate. Derived from the compiled command's Use string ("verify
// [id-or-path]"), so the compiled tree stays the source of truth: a command that
// gains or loses a positional needs no edit here.
func groupLeafCommands() map[string]bool {
	out := map[string]bool{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		children := c.Commands()
		if len(children) > 0 {
			if fields := strings.Fields(c.Use); len(fields) > 1 {
				for _, f := range fields[1:] {
					if isPositionalPlaceholder(f) {
						out[c.Name()] = true
						break
					}
				}
			}
		}
		for _, sub := range children {
			walk(sub)
		}
	}
	walk(NewRootCmd())
	return out
}

// validateInvocationTokens checks a single `atcr ...` token sequence against the
// compiled command tree and returns any human-readable error messages.
func validateInvocationTokens(tokens []string, path string, cmds map[string]bool, groups map[string]map[string]bool) []string {
	var errs []string
	reFlag := regexp.MustCompile(`^--([a-z][a-z-]+)`)
	isWord := regexp.MustCompile(`^[a-z][a-z-]+$`).MatchString
	name0 := tokens[0]
	switch {
	case reFlag.MatchString(name0):
		// e.g. `atcr --version`: a root flag, validated in the flag loop below.
	case !cmds[name0]:
		errs = append(errs, fmt.Sprintf("%s references `atcr %s` but %q is not a real command", path, name0, name0))
	default:
		validateSubcommandChain(tokens, 0, groups, groupLeafCommands(), isWord, reFlag, path, &errs)
	}
	// Both halves of the auditor must agree about where the invocation ends.
	// Truncate at the first shell terminator so a flag named inside a trailing
	// comment, or belonging to a second command on the same line, is not
	// attributed to this one — the subcommand scan already stops there, and
	// leaving the flag loop unbounded made the two halves disagree.
	argv := tokens
	for i, tok := range tokens {
		if endsLine(tok) {
			argv = tokens[:i]
			break
		}
	}
	// Validate each --flag against the flags reachable on the *specific* command
	// this invocation addresses (per-command inheritance), not the global union of
	// every flag in the tree. This rejects e.g. `atcr review --checkpoint`, where
	// --checkpoint is real but belongs to `benchmark run`, matching the per-command
	// scope the error message claims.
	flags := reachableFlags(argv)
	for _, tok := range argv {
		if m := reFlag.FindStringSubmatch(tok); m != nil && !flags[m[1]] {
			errs = append(errs, fmt.Sprintf("%s references `--%s` on an `atcr %s` command, but no such flag exists", path, m[1], name0))
		}
	}
	return errs
}

// TestDocsReferenceOnlyRealCommands asserts, for every `atcr ...` invocation in
// the audited docs, that (a) the first token is a real command, (b) when that
// command is a group its next bare-word token is a real subcommand, and (c) every
// --flag on the line is a real flag somewhere in the compiled tree (AC1). Flags
// are validated only when attached to an atcr invocation, so unrelated docker/git
// flags in the same docs are not misread as atcr flags.
func TestDocsReferenceOnlyRealCommands(t *testing.T) {
	cmds := canonicalCommands()
	groups := commandGroups()
	for path, content := range auditedMarkdown(t) {
		for _, tokens := range atcrInvocations(content, cmds) {
			for _, err := range validateInvocationTokens(tokens, path, cmds, groups) {
				t.Errorf("%s", err)
			}
		}
	}
}

// The config-key ↔ flag mapping must be derivable without reading source:
// documented in one table in the config docs (registry.md) and in root help.
// Three pairs diverge (payload_mode→--payload, timeout_secs→--timeout,
// payload_byte_budget→--byte-budget); fail_on and max_parallel match and anchor
// the convention.
func TestConfigKeyFlagMappingIsDocumented(t *testing.T) {
	pairs := [][2]string{
		{"payload_mode", "--payload"},
		{"timeout_secs", "--timeout"},
		{"payload_byte_budget", "--byte-budget"},
		{"fail_on", "--fail-on"},
		{"max_parallel", "--max-parallel"},
	}

	_, helpOut := execCmdCapture(t, "--help")
	for _, p := range pairs {
		require.Contains(t, helpOut, p[0], "root help must name config key %s", p[0])
		require.Contains(t, helpOut, p[1], "root help must name flag %s for config key %s", p[1], p[0])
	}

	reg := auditedMarkdown(t)["docs/registry.md"]
	for _, p := range pairs {
		require.Contains(t, reg, p[0], "registry.md must name config key %s", p[0])
		require.Contains(t, reg, p[1], "registry.md must map config key %s to %s", p[0], p[1])
	}
}

// The config-key ↔ flag mapping intro is prose in registry.md, which follows
// the no-hard-wrap rule: a hard-wrapped intro can split a phrase a drift test
// asserts with Contains, so the paragraph between the mapping header and the
// mapping table must be a single line.
func TestConfigKeyFlagMappingIntroIsSingleLine(t *testing.T) {
	reg := auditedMarkdown(t)["docs/registry.md"]
	start := strings.Index(reg, "### Config-key ↔ CLI flag mapping")
	require.GreaterOrEqual(t, start, 0, "registry.md has no config-key mapping header")
	rest := reg[start:]
	nl := strings.Index(rest, "\n")
	require.GreaterOrEqual(t, nl, 0)
	after := rest[nl+1:]
	end := strings.Index(after, "\n| Config key")
	require.GreaterOrEqual(t, end, 0, "mapping header not followed by the mapping table")
	intro := strings.TrimSpace(after[:end])
	require.NotEmpty(t, intro, "mapping intro must not be empty")
	require.NotContains(t, intro, "\n", "config-key mapping intro must be a single line, not hard-wrapped")
}

// TestSubcommandValidationSkipsFlags asserts that a bogus subcommand placed
// after a flag is still rejected (e.g. `atcr benchmark --json frobnicate`).
func TestSubcommandValidationSkipsFlags(t *testing.T) {
	cmds := canonicalCommands()
	groups := commandGroups()
	errs := validateInvocationTokens([]string{"benchmark", "--json", "frobnicate"}, "fixture", cmds, groups)
	found := false
	for _, err := range errs {
		if strings.Contains(err, "frobnicate") && strings.Contains(err, "not a subcommand") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected validation to reject bogus subcommand `frobnicate` after flag, got errors: %v", errs)
	}
}

// TestSubcommandValidationStopsAtInvocationEnd pins endsInvocation against the
// leaf-and-group case (`atcr verify` owns `diff` AND takes [id-or-path]). Each
// shape below is real prose from docs/ that the scanner misread as a subcommand
// slot the moment `verify` gained a child. Asserted directly rather than left to
// the docs so a later doc rewrite cannot silently retire the coverage.
func TestSubcommandValidationStopsAtInvocationEnd(t *testing.T) {
	cmds := canonicalCommands()
	groups := commandGroups()

	for _, tc := range []struct {
		name   string
		tokens []string
	}{
		{"trailing comment", []string{"verify", "<review-id>", "--exec", "#", "standalone", "verify"}},
		{"placeholder then comment", []string{"verify", "[id-or-path]", "#", "verify", "a", "review"}},
		{"second invocation on one line", []string{"verify", "→", "atcr", "debate"}},
		{"pipe", []string{"verify", "|", "jq"}},
		// Anchored on `verify`, the one group that is ALSO a leaf. This case used
		// to read {"debt", "<file>", "frobnicate"} and pass, which was the defect
		// rather than the contract: `debt` takes no positional, so the placeholder
		// sat in its subcommand slot and the bogus word went unchecked. That
		// shape is now asserted to be REJECTED, in
		// TestPlaceholderRelaxationIsScopedToGroupAndLeaf.
		{"angle placeholder", []string{"verify", "<review-id>", "frobnicate"}},
		{"double ampersand", []string{"debt", "&&", "frobnicate"}},
		{"double pipe", []string{"debt", "||", "frobnicate"}},
		{"semicolon", []string{"debt", ";", "frobnicate"}},
		{"redirect with space", []string{"debt", ">", "frobnicate"}},
		{"append redirect with space", []string{"debt", ">>", "frobnicate"}},
		{"redirect without space", []string{"debt", ">/dev/null", "frobnicate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if errs := validateInvocationTokens(tc.tokens, "fixture", cmds, groups); len(errs) != 0 {
				t.Errorf("expected `atcr %s` to pass, got errors: %v", strings.Join(tc.tokens, " "), errs)
			}
		})
	}

	// The stop rules must not disarm the check itself: a bogus subcommand with
	// no terminator in front of it is still rejected.
	errs := validateInvocationTokens([]string{"verify", "frobnicate"}, "fixture", cmds, groups)
	found := false
	for _, err := range errs {
		if strings.Contains(err, "frobnicate") && strings.Contains(err, "not a subcommand") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `atcr verify frobnicate` to still be rejected, got errors: %v", errs)
	}

	// And the real subcommand must resolve.
	if errs := validateInvocationTokens([]string{"verify", "diff", "--staged"}, "fixture", cmds, groups); len(errs) != 0 {
		t.Errorf("expected `atcr verify diff --staged` to pass, got errors: %v", errs)
	}
}

// TestPlaceholderRelaxationIsScopedToGroupAndLeaf asserts that stopping the
// subcommand scan at a positional placeholder applies ONLY to a command that is
// both a group and a leaf.
//
// `verify [id-or-path]` is the only such command in the tree: there a placeholder
// IS the documented leaf usage, so nothing after it can be a subcommand. Every
// other group (`debt`, `benchmark`, `config`, `models`, `personas`, `skill`) takes
// no positional of its own, so a placeholder in its subcommand slot is the author
// eliding the subcommand — and the next bare word must still be validated.
// Applying the relaxation to all eight silently disarmed the auditor for seven of
// them.
func TestPlaceholderRelaxationIsScopedToGroupAndLeaf(t *testing.T) {
	cmds := canonicalCommands()
	groups := commandGroups()

	// The group-and-leaf case must stay relaxed (this is what the stop exists for).
	for _, tokens := range [][]string{
		{"verify", "[id-or-path]", "#", "verify", "a", "review"},
		{"verify", "<review-id>", "--exec"},
	} {
		if errs := validateInvocationTokens(tokens, "fixture", cmds, groups); len(errs) != 0 {
			t.Errorf("expected `atcr %s` to pass (verify is a group AND a leaf), got errors: %v",
				strings.Join(tokens, " "), errs)
		}
	}

	// A pure group must still catch a bogus subcommand after a placeholder.
	for _, tokens := range [][]string{
		{"debt", "<file>", "frobnicate"},
		{"benchmark", "<sub>", "frobnicate"},
	} {
		errs := validateInvocationTokens(tokens, "fixture", cmds, groups)
		found := false
		for _, err := range errs {
			if strings.Contains(err, "frobnicate") && strings.Contains(err, "not a subcommand") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected `atcr %s` to be rejected (%q takes no positional of its own, so the placeholder is its subcommand slot), got errors: %v",
				strings.Join(tokens, " "), tokens[0], errs)
		}
	}
}

// TestFlagValidationHonorsInvocationEnd asserts the two halves of the auditor
// agree about where an invocation ends. The subcommand scan stops at a shell
// terminator; the flag loop must too, or a flag named inside a trailing comment
// or belonging to a second command on the same line is attributed to the first
// command and reported as a nonexistent flag.
func TestFlagValidationHonorsInvocationEnd(t *testing.T) {
	cmds := canonicalCommands()
	groups := commandGroups()
	for _, tokens := range [][]string{
		// --checkpoint is real, but on `benchmark run`; here it is inside a comment.
		{"verify", "<review-id>", "--exec", "#", "then", "run", "--checkpoint"},
		// --checkpoint belongs to the SECOND invocation on the line, not to review.
		{"review", "&&", "atcr", "benchmark", "run", "--checkpoint"},
	} {
		if errs := validateInvocationTokens(tokens, "fixture", cmds, groups); len(errs) != 0 {
			t.Errorf("expected `atcr %s` to pass (flags past a shell terminator are not this command's), got errors: %v",
				strings.Join(tokens, " "), errs)
		}
	}

	// The truncation must not disarm flag validation before the terminator.
	errs := validateInvocationTokens([]string{"review", "--checkpoint", "#", "note"}, "fixture", cmds, groups)
	found := false
	for _, err := range errs {
		if strings.Contains(err, "checkpoint") && strings.Contains(err, "no such flag") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `atcr review --checkpoint` to still be rejected before the comment, got errors: %v", errs)
	}
}

// TestFlagValidationIsPerCommand asserts that a documented --flag is validated
// against the flags reachable on the *specific* command it is attached to, not
// the global union of every flag in the tree. `--checkpoint` is a `benchmark run`
// flag and `--json` a `doctor` flag; neither exists on `review` or `init`, so a
// doc that writes them there must be rejected — while the same flags on their
// real owning commands must still pass.
func TestFlagValidationIsPerCommand(t *testing.T) {
	cmds := canonicalCommands()
	groups := commandGroups()

	// Wrong-command flags must be rejected.
	for _, tc := range [][]string{
		{"review", "--checkpoint"}, // checkpoint belongs to `benchmark run`
		{"init", "--json"},         // json belongs to `doctor`
	} {
		errs := validateInvocationTokens(tc, "fixture", cmds, groups)
		found := false
		for _, err := range errs {
			if strings.Contains(err, tc[1][2:]) && strings.Contains(err, "no such flag") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected `atcr %s %s` to be rejected (flag not on that command), got errors: %v", tc[0], tc[1], errs)
		}
	}

	// The same flags on their real owning commands must still pass.
	for _, tc := range [][]string{
		{"benchmark", "run", "--checkpoint"},
		{"doctor", "--json"},
	} {
		if errs := validateInvocationTokens(tc, "fixture", cmds, groups); len(errs) != 0 {
			t.Errorf("expected `atcr %s` to pass, got errors: %v", strings.Join(tc, " "), errs)
		}
	}
}

// TestConfigDocsUseRealConfigFilenameAndReconcilerName guards against the two
// drift tokens epic 15.0 was chartered to eliminate: the fictional `atcr.yaml`
// config filename (the real project config is `.atcr/config.yaml`) and the
// non-existent feature name "Reconciler v2" (the shipped feature is simply the
// multi-model reconciler). Neither may appear in any audited doc (AC2).
func TestConfigDocsUseRealConfigFilenameAndReconcilerName(t *testing.T) {
	reFilename := regexp.MustCompile(`\batcr\.yaml\b`)
	reReconciler := regexp.MustCompile(`(?i)reconciler v2`)
	for path, content := range auditedMarkdown(t) {
		if reFilename.MatchString(content) {
			t.Errorf("%s references `atcr.yaml`; the real project config is `.atcr/config.yaml`", path)
		}
		if reReconciler.MatchString(content) {
			t.Errorf("%s references \"Reconciler v2\", which is not a real feature name", path)
		}
	}
}

// configBlockDocumented reports whether ref documents block as a real config
// surface: either via a fenced YAML block with the key at column 0, or via a
// section heading whose text names the block. This rejects bare substring matches
// in prose or incidental YAML.
func configBlockDocumented(ref, block string) bool {
	name := strings.TrimSuffix(block, ":")
	// A section heading that names the block counts as documentation.
	reHeading := regexp.MustCompile("(?im)^#{2,3}\\s+.*\\b" + regexp.QuoteMeta(name) + "\\b")
	if reHeading.MatchString(ref) {
		return true
	}
	// A fenced YAML block containing the key at column 0 counts as documentation.
	reKey := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(block))
	inFence := false
	for _, ln := range strings.Split(ref, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```yaml") || strings.HasPrefix(trimmed, "```yml") {
			inFence = true
			continue
		}
		if trimmed == "```" {
			inFence = false
			continue
		}
		if inFence && reKey.MatchString(ln) {
			return true
		}
	}
	return false
}

// TestConfigBlockDocumentedRejectsProseOnly guards against config-block tokens
// being matched inside prose rather than as documented config surfaces.
func TestConfigBlockDocumentedRejectsProseOnly(t *testing.T) {
	prose := "This sentence mentions executor: but has no real documentation."
	if configBlockDocumented(prose, "executor:") {
		t.Error("prose-only reference should not count as documented config block")
	}
}

// TestReconcilerConfigSurfaceDocumented asserts that the user-facing config
// blocks that tune the multi-model reconciler pipeline — persona plus the
// debate/verify/executor sections — are all present in the configuration
// reference (AC2). Dedup is deliberately excluded: it is fixed internal behavior
// (hardcoded cutoffs in reconcile/dedupe.go), not a configurable surface.
func TestReconcilerConfigSurfaceDocumented(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "registry.md"))
	if err != nil {
		t.Fatalf("read registry.md: %v", err)
	}
	ref := string(b)
	for _, block := range []string{"persona:", "debate:", "verify:", "executor:"} {
		if !configBlockDocumented(ref, block) {
			t.Errorf("docs/registry.md does not document the `%s` config block", block)
		}
	}
}

// extractIndexLinkTarget normalizes a raw markdown link destination from
// docs/README.md: it strips any #anchor, removes a ./ prefix, and removes a
// trailing quoted title (e.g. `x.md "title"` → `x.md`).
func extractIndexLinkTarget(raw string) string {
	raw = strings.SplitN(raw, "#", 2)[0]
	raw = strings.TrimPrefix(raw, "./")
	raw = strings.TrimSpace(regexp.MustCompile(`\s+["'][^"']*["']$`).ReplaceAllString(raw, ""))
	return raw
}

// TestExtractIndexLinkTargetStripsTitle guards against markdown links that
// include a quoted title (e.g. `](x.md "title")`). The captured target must have
// the title stripped before the .md/anchor checks run.
func TestExtractIndexLinkTargetStripsTitle(t *testing.T) {
	got := extractIndexLinkTarget(`architecture.md "Architecture overview"`)
	if got != "architecture.md" {
		t.Errorf("expected 'architecture.md', got %q", got)
	}
}

// TestDocsIndexCoversEveryDoc asserts that docs/README.md is the single
// source-of-truth index the website build consumes: it must link to every other
// docs/*.md file exactly, with no dangling links and no missing entries (T4). A
// new doc that is never linked, or a link to a doc that was deleted/renamed,
// fails this test.
func TestDocsIndexCoversEveryDoc(t *testing.T) {
	root := repoRootDir(t)
	docsDir := filepath.Join(root, "docs")

	all, err := filepath.Glob(filepath.Join(docsDir, "*.md"))
	if err != nil {
		t.Fatalf("glob docs: %v", err)
	}
	expected := map[string]bool{}
	for _, p := range all {
		base := filepath.Base(p)
		if base == "README.md" {
			continue // the index does not link itself
		}
		expected[base] = true
	}

	indexPath := filepath.Join(docsDir, "README.md")
	b, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("docs/README.md index must exist (T4): %v", err)
	}

	// Extract relative markdown link targets that point at a .md file, stripping
	// any #anchor and ./ prefix. External (http) and non-.md links are ignored.
	linked := map[string]bool{}
	re := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		target := m[1]
		if strings.Contains(target, "://") {
			continue
		}
		target = extractIndexLinkTarget(target)
		if !strings.HasSuffix(target, ".md") || strings.Contains(target, "/") {
			continue // only same-directory doc links count toward the index
		}
		base := filepath.Base(target)
		if base == "README.md" {
			continue
		}
		// Dangling-link guard: every linked doc must exist on disk.
		if _, err := os.Stat(filepath.Join(docsDir, base)); err != nil {
			t.Errorf("docs/README.md links %q but that file does not exist", base)
			continue
		}
		linked[base] = true
	}

	for doc := range expected {
		if !linked[doc] {
			t.Errorf("docs/README.md index does not link docs/%s", doc)
		}
	}
	for doc := range linked {
		if !expected[doc] {
			t.Errorf("docs/README.md links docs/%s which is not an expected doc", doc)
		}
	}
}

// TestArchitectureDocDescribesReconciler asserts that docs/architecture.md both
// (a) names the key stages/concepts of the real multi-model reconciler pipeline
// (review, reconcile, verify, debate, cluster, dedupe, confidence, persona) and
// (b) states the substantive, load-bearing facts about it — the dedupe merge and
// gray-zone thresholds (cross-checked live against reconcile.MergeThreshold /
// reconcile.GrayLow) and the ATCR_DISABLE_AST_GROUPING opt-out (AC3). The fact
// checks mean a doc that keeps the vocabulary but drifts on a real cutoff or
// env-var name fails, closing the gap where a keyword-only stub would have passed.
func TestArchitectureDocDescribesReconciler(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "architecture.md"))
	if err != nil {
		t.Fatalf("docs/architecture.md must exist (AC3): %v", err)
	}
	doc := string(b)
	lower := strings.ToLower(doc)
	for _, term := range []string{"review", "reconcile", "cluster", "dedupe", "confidence", "verify", "debate", "persona"} {
		if !strings.Contains(lower, term) {
			t.Errorf("docs/architecture.md does not describe the %q stage/concept of the reconciler", term)
		}
	}

	// Beyond keyword presence, assert the substantive claims the doc makes about
	// the reconciler so an inaccurate rewrite (wrong dedupe cutoff, renamed env
	// var) fails instead of passing on generic vocabulary. The threshold values
	// are cross-checked live against reconcile/dedupe.go, so changing the real
	// constant without mirroring it in the doc breaks this test.
	facts := []struct{ label, want string }{
		{"dedupe merge threshold (reconcile.MergeThreshold)", strconv.FormatFloat(reclib.MergeThreshold, 'g', -1, 64)},
		{"dedupe gray-zone floor (reconcile.GrayLow)", strconv.FormatFloat(reclib.GrayLow, 'g', -1, 64)},
		{"AST-grouping opt-out env var", "ATCR_DISABLE_AST_GROUPING"},
	}
	for _, f := range facts {
		if !strings.Contains(doc, f.want) {
			t.Errorf("docs/architecture.md omits the %s (%q); the doc must state this load-bearing fact, not just reconciler vocabulary", f.label, f.want)
		}
	}
}

// TestAtcrInvocationsSkipsFencedProse guards against fenced code blocks that
// contain prose about atcr (e.g. "atcr is local-first") being misread as command
// references. Only lines that look like real invocations — shell-prompt lines or
// lines whose first token after "atcr" is a known command or flag — are captured.
func TestAtcrInvocationsSkipsFencedProse(t *testing.T) {
	cmds := canonicalCommands()
	md := "```\natcr is local-first and not a real command\natcr benchmark run\n```"
	got := atcrInvocations(md, cmds)
	if len(got) != 1 {
		t.Fatalf("expected 1 invocation, got %d: %v", len(got), got)
	}
	if len(got[0]) != 2 || got[0][0] != "benchmark" || got[0][1] != "run" {
		t.Errorf("expected [[benchmark run]], got %v", got)
	}
}

// TestDocsClaimedFlagsAreReal asserts that every flag the docs explicitly call a
// "flag" via the “ `--x` flag “ idiom is a real flag on some command in the
// compiled tree (AC1). This catches prose that documents a CLI flag which does
// not exist, without matching docker/git flags used in unrelated examples.
func TestDocsClaimedFlagsAreReal(t *testing.T) {
	flags := canonicalFlags()
	re := regexp.MustCompile("`--([a-z][a-z-]+)`\\s+flag")
	for path, content := range auditedMarkdown(t) {
		for _, m := range re.FindAllStringSubmatch(content, -1) {
			name := m[1]
			if !flags[name] {
				t.Errorf("%s documents `--%s` as a flag but no such CLI flag exists", path, name)
			}
		}
	}
}

// TestDiffSmellVersionProbeInspectsHelpText pins the documented "is this atcr new
// enough?" probe against the shape that actually discriminates.
//
// Bare `atcr` is itself a command (the home view) with `Args: NoArgs`, so on an
// atcr that predates `diff-smell` cobra treats the unknown word as a positional
// and --help short-circuits to the ROOT help — exit 0. The same trap existed
// under the old `verify diff` spelling: `verify` is both a group and a leaf (it
// takes an optional [id-or-path]), so cobra read "diff" as a POSITIONAL and
// --help showed verify's own help, again exit 0. A probe keying on the exit
// status therefore reports "new enough" on every binary ever shipped, and the
// guarded call then dies with `unknown flag: --staged`. Under the documented
// pre-commit hook's `set -euo pipefail` that blocks every commit — the exact
// failure the probe exists to prevent.
//
// The probe must inspect the help TEXT instead. `--fail-on` is registered only
// on the scanner (never on the root or on `verify`), so grepping for it
// separates the two. The recipe probes the canonical `atcr diff-smell` first
// and falls back to the deprecated `atcr verify diff` alias, so a hook pasted
// during the one-minor-version alias window does not silently skip the gate.
func TestDiffSmellVersionProbeInspectsHelpText(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "diff-smell.md"))
	if err != nil {
		t.Fatalf("read docs/diff-smell.md: %v", err)
	}
	probed := 0
	canonical := 0
	for i, ln := range strings.Split(string(b), "\n") {
		if !strings.Contains(ln, "atcr diff-smell --help") && !strings.Contains(ln, "atcr verify diff --help") {
			continue
		}
		probed++
		if strings.Contains(ln, "atcr diff-smell --help") {
			canonical++
		}
		if !strings.Contains(ln, "grep") || !strings.Contains(ln, "--fail-on") {
			t.Errorf("docs/diff-smell.md:%d probes with %q, which exits 0 on an atcr that predates the command; "+
				"the probe must inspect the help text, e.g. `atcr diff-smell --help 2>&1 | grep -- '--fail-on' >/dev/null`",
				i+1, strings.TrimSpace(ln))
			continue
		}
		// `grep -q` exits at the first match and closes the pipe, so atcr is killed
		// by SIGPIPE (141); under `set -o pipefail` — which the documented
		// pre-commit hook enables — that becomes the pipeline's status and the
		// probe reports "too old" on a NEW atcr, silently skipping the gate.
		// Measured: PIPESTATUS=(141 0) with -q, (0 0) without.
		if regexp.MustCompile(`grep\s+(-\w*q|--quiet|--silent)`).MatchString(ln) {
			t.Errorf("docs/diff-smell.md:%d probes with %q; `grep -q` closes the pipe and SIGPIPEs atcr (141), "+
				"which `set -o pipefail` turns into a false \"too old\" result. Let grep drain its input: `| grep -- '--fail-on' >/dev/null`",
				i+1, strings.TrimSpace(ln))
		}
	}
	if probed == 0 {
		t.Fatal("docs/diff-smell.md documents no version probe; the version-discoverability recipe is the consumer-adoption path and must stay documented")
	}
	if canonical == 0 {
		t.Fatal("docs/diff-smell.md documents no `atcr diff-smell --help` probe; the canonical spelling must be probed first, with the deprecated `atcr verify diff` alias as the fallback")
	}
}

// fencedBlock returns the sole ```<lang> fenced block in md, or "" when there is
// not exactly one. Requiring exactly one keeps the caller from silently checking
// whichever block happened to come first if the doc later grows another.
func fencedBlock(md, lang string) string {
	var blocks []string
	var cur []string
	in := false
	for _, ln := range strings.Split(md, "\n") {
		if in {
			if strings.TrimSpace(ln) == "```" {
				blocks = append(blocks, strings.Join(cur, "\n")+"\n")
				cur, in = nil, false
				continue
			}
			cur = append(cur, ln)
			continue
		}
		if strings.TrimSpace(ln) == "```"+lang {
			in = true
		}
	}
	if len(blocks) != 1 {
		return ""
	}
	return blocks[0]
}

// TestDiffSmellDocExampleMatchesAnalyzer makes the WORKED EXAMPLE in
// docs/diff-smell.md a second golden rather than prose: it feeds the doc's own
// ```diff block through AnalyzeDiff and compares the result to the doc's own
// ```json block.
//
// TestSmellResult_GoldenJSON pins the shape against a copy of the document held
// in the test file, and its failure message tells the developer to update
// docs/diff-smell.md — but nothing mechanically checked that they did. The two
// were in sync, so the drift that golden test exists to prevent could still land
// undetected in the DOCS half of the contract, which is precisely what AC9 claims
// cannot happen.
func TestDiffSmellDocExampleMatchesAnalyzer(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "diff-smell.md"))
	if err != nil {
		t.Fatalf("read docs/diff-smell.md: %v", err)
	}
	md := string(b)

	gotDiff := fencedBlock(md, "diff")
	if gotDiff == "" {
		t.Fatal("docs/diff-smell.md must contain exactly one ```diff block — the worked example that documents the JSON shape")
	}
	wantJSON := fencedBlock(md, "json")
	if wantJSON == "" {
		t.Fatal("docs/diff-smell.md must contain exactly one ```json block — the output of the worked example")
	}

	got, err := json.MarshalIndent(verify.AnalyzeDiff(gotDiff), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	require.JSONEq(t, wantJSON, string(got),
		"the ```json block in docs/diff-smell.md no longer matches what AnalyzeDiff produces for the ```diff block "+
			"directly above it. The doc example is half of the public contract — update it, or fix the analyzer.")
}

// TestDiffSmellExitTwoCausesAreDocumented asserts BOTH exit tables enumerate
// every real cause of exit 2, and stay in sync with each other.
//
// They previously listed exactly four ("bad --fail-on value, unreadable file, git
// failure, or two named sources") while the command refused more than that. Each
// omission below is verified against the binary:
//
//	--rev --output=/tmp/pwn  -> 2   (a revision-shaped value starting with '-')
//	--rev ""                 -> 2   (an explicitly empty revision)
//	--diff ""                -> 2   (an explicitly empty diff path)
//	a source over the size cap -> 2
//
// The '-' rule in particular is a deliberate argument-injection guard, worth
// documenting rather than leaving as a surprise.
func TestDiffSmellExitTwoCausesAreDocumented(t *testing.T) {
	root := repoRootDir(t)
	// Each cause is a set of alternatives; the doc must express it somehow.
	causes := []struct {
		label string
		anyOf []string
	}{
		{"a revision-shaped value starting with '-'", []string{"start with '-'", "starts with `-`", "leading `-`", "`-`-leading", "begins with `-`"}},
		{"an explicitly empty --rev/--diff value", []string{"empty `--rev`", "empty value", "an empty ", "explicitly empty"}},
		{"input over the size cap", []string{"too large", "size cap", "over the cap"}},
	}
	for _, name := range []string{"diff-smell.md", "ci-integration.md"} {
		b, err := os.ReadFile(filepath.Join(root, "docs", name))
		if err != nil {
			t.Fatalf("read docs/%s: %v", name, err)
		}
		doc := string(b)
		for _, c := range causes {
			ok := false
			for _, alt := range c.anyOf {
				if strings.Contains(doc, alt) {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("docs/%s does not document exit 2 for %s; both exit tables must enumerate every cause", name, c.label)
			}
		}
	}
}

// TestDiffSubcommandShadowingIsDocumented asserts the docs state the one
// behaviour change adding `diff` under `verify` caused: cobra resolves a matching
// child before positional args, so a review id or path literally named `diff` is
// shadowed and reachable only as `atcr verify -- diff`.
//
// Verified: `atcr verify -- diff` reaches the parent and resolves
// .atcr/reviews/diff, while `atcr verify diff` reaches the scanner. The trade-off
// is recorded in CHANGELOG.md and in a code comment at cli/verify.go, but a user
// whose review id happens to be `diff` reads the docs, not the source — and got a
// diff scan with no hint why.
func TestDiffSubcommandShadowingIsDocumented(t *testing.T) {
	root := repoRootDir(t)
	for _, name := range []string{"diff-smell.md", "verification.md"} {
		b, err := os.ReadFile(filepath.Join(root, "docs", name))
		if err != nil {
			t.Fatalf("read docs/%s: %v", name, err)
		}
		if !strings.Contains(string(b), "atcr verify -- diff") {
			t.Errorf("docs/%s does not document the `atcr verify -- diff` escape hatch; "+
				"adding `diff` as a child of `verify` shadows a review id named `diff`, and that is "+
				"reachable no other way", name)
		}
	}
}

// TestDiffSmellOwnsFailOnAlone backs the probe above: it is only a valid version
// discriminator while `--fail-on` is reachable on the diff-smell scanner and NOT
// on the root or on `verify`. If a later change adds --fail-on to `verify`, the
// documented recipe silently starts reporting "new enough" on old binaries
// again.
func TestVerifyDiffOwnsFailOnAlone(t *testing.T) {
	if !reachableFlags([]string{"diff-smell"})["fail-on"] {
		t.Error("`atcr diff-smell` must own --fail-on; the documented version probe greps its help text for that flag")
	}
	if !reachableFlags([]string{"verify", "diff"})["fail-on"] {
		t.Error("the deprecated `atcr verify diff` alias must keep --fail-on; the probe's fallback greps its help text for that flag")
	}
	if reachableFlags([]string{"verify"})["fail-on"] {
		t.Error("`atcr verify` must NOT expose --fail-on: the documented version probe greps for it to tell a new atcr from an old one, and an inherited/duplicated flag would make an old binary indistinguishable")
	}
}

// configSetLong walks the compiled command tree to the `config set` subcommand
// and returns its Long help text, so the coverage checks below assert on the
// real, self-documenting CLI surface rather than a hardcoded copy.
func configSetLong(t *testing.T) string {
	t.Helper()
	for _, c := range NewRootCmd().Commands() {
		if c.Name() != "config" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() == "set" {
				return sub.Long
			}
		}
		t.Fatal("`atcr config` has no `set` subcommand")
	}
	t.Fatal("`atcr config` command is not registered")
	return ""
}

// TestDocsAudit_ATCRTelemetryEnvVarCoverage asserts the `atcr config set` Long
// help text states the load-bearing ATCR_TELEMETRY fact — its name, that it
// names the ENABLED state directly, and the inverse-direction warning vs.
// ATCR_DISABLE_AST_GROUPING — so the CLI itself is a source of truth for the
// opt-out (AC 02-04, Phase-3 deliverable; the docs/telemetry.md fact-check is
// validated in Phase 5 / AC 05-03 once Story 5 creates that doc).
func TestDocsAudit_ATCRTelemetryEnvVarCoverage(t *testing.T) {
	long := configSetLong(t)
	for _, want := range []string{"ATCR_TELEMETRY", "ATCR_DISABLE_AST_GROUPING"} {
		if !strings.Contains(long, want) {
			t.Errorf("`atcr config set` Long text omits %q; it must document the env var and its inverse-direction footgun", want)
		}
	}
}

// TestDocsAudit_ConfigSetTelemetryFlagCoverage asserts the `atcr config set`
// Long help text documents the single supported `telemetry` key and its
// accepted boolean values, per AC 02-04 Edge Case 3 (self-documenting help).
func TestDocsAudit_ConfigSetTelemetryFlagCoverage(t *testing.T) {
	long := configSetLong(t)
	for _, want := range []string{"telemetry", "true", "false"} {
		if !strings.Contains(long, want) {
			t.Errorf("`atcr config set` Long text omits %q; it must document the only supported key and its accepted values", want)
		}
	}
}

// nestedYAMLKeyValue returns the value documented for key nested one level under
// the top-level block, inside a fenced YAML block in ref. It is deliberately
// stricter than configBlockDocumented: a bare mention of the key in prose, the
// key under a DIFFERENT top-level block, or the key at column 0 all fail to
// match, because none of those tell a reader where the key actually lives.
//
// The returned bool reports whether such a key was found at all, so a caller can
// distinguish "documented with the wrong value" from "not documented".
func nestedYAMLKeyValue(ref, block, key string) (string, bool) {
	reBlock := regexp.MustCompile("^" + regexp.QuoteMeta(block))
	// Indentation is required: a column-0 `key:` is a sibling of the block, not a
	// field of it.
	reKey := regexp.MustCompile(`^\s+` + regexp.QuoteMeta(key) + `:\s*(.*)$`)

	inFence, inBlock := false, false
	for _, ln := range strings.Split(ref, "\n") {
		trimmed := strings.TrimSpace(ln)
		if !inFence {
			if strings.HasPrefix(trimmed, "```yaml") || strings.HasPrefix(trimmed, "```yml") {
				inFence = true
			}
			continue
		}
		if trimmed == "```" {
			// Block scope never survives its fence.
			inFence, inBlock = false, false
			continue
		}
		if reBlock.MatchString(ln) {
			inBlock = true
			continue
		}
		// Any other column-0 key ends the block.
		if ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") {
			inBlock = false
			continue
		}
		if !inBlock {
			continue
		}
		if m := reKey.FindStringSubmatch(ln); m != nil {
			val := m[1]
			// Trailing `# comment` is presentation, not value.
			if i := strings.Index(val, "#"); i >= 0 {
				val = val[:i]
			}
			val = strings.Trim(strings.TrimSpace(val), `"'`)
			if val == "" {
				// `fallback:` with no value documents nothing actionable.
				continue
			}
			return val, true
		}
	}
	return "", false
}

// TestNestedYAMLKeyValueRejectsMisplacedKeys guards the helper above against the
// four ways `fallback:` can appear without actually documenting the
// `sandbox.fallback` surface.
func TestNestedYAMLKeyValueRejectsMisplacedKeys(t *testing.T) {
	cases := []struct {
		name string
		ref  string
	}{
		{"prose only", "The sandbox: block accepts fallback: os-level in some configs."},
		{"wrong parent block", "```yaml\nauto_fix:\n  fallback: os-level\n```"},
		{"column zero, not nested", "```yaml\nsandbox:\n  backend: docker\nfallback: os-level\n```"},
		{"empty value", "```yaml\nsandbox:\n  fallback:\n```"},
		{"scope leaks past fence", "```yaml\nsandbox:\n  backend: docker\n```\n\n```yaml\n  fallback: os-level\n```"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := nestedYAMLKeyValue(tc.ref, "sandbox:", "fallback"); ok {
				t.Errorf("nestedYAMLKeyValue matched %q; %s must not count as documenting sandbox.fallback", got, tc.name)
			}
		})
	}
}

// TestNestedYAMLKeyValueReadsValue asserts the helper strips a trailing comment
// and surrounding quotes, so a documented value can be compared to the shipped
// constant verbatim.
func TestNestedYAMLKeyValueReadsValue(t *testing.T) {
	cases := map[string]string{
		"plain":     "```yaml\nsandbox:\n  fallback: os-level\n```",
		"commented": "```yaml\nsandbox:\n  fallback: os-level     # opt-in, never automatic\n```",
		"quoted":    "```yaml\nsandbox:\n  fallback: \"os-level\"\n```",
	}
	for name, ref := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := nestedYAMLKeyValue(ref, "sandbox:", "fallback")
			if !ok {
				t.Fatal("nestedYAMLKeyValue found no fallback key")
			}
			if got != "os-level" {
				t.Errorf("nestedYAMLKeyValue = %q, want %q", got, "os-level")
			}
		})
	}
}

// TestAutoFixDocsDocumentSandboxFallback asserts docs/auto-fix.md documents the
// opt-in `sandbox.fallback` field with the value the code actually accepts. The
// expected value is READ FROM registry.SandboxFallbackOSLevel rather than
// hardcoded, so renaming the sentinel fails this test instead of silently
// desynchronizing the docs from the config validator (AC 03-05).
func TestAutoFixDocsDocumentSandboxFallback(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "auto-fix.md"))
	if err != nil {
		t.Fatalf("read auto-fix.md: %v", err)
	}
	ref := string(b)

	got, ok := nestedYAMLKeyValue(ref, "sandbox:", "fallback")
	if !ok {
		t.Fatal("docs/auto-fix.md does not document the `fallback:` key inside a fenced YAML `sandbox:` block")
	}
	if got != registry.SandboxFallbackOSLevel {
		t.Errorf("docs/auto-fix.md documents `sandbox.fallback: %s`, but the only value the config validator accepts is %q", got, registry.SandboxFallbackOSLevel)
	}
}

// markdownSection returns the body of the first `##`/`###` section whose heading
// matches re, up to the next heading of the same-or-higher level. It exists so a
// prose assertion can be scoped to the section under test: docs/auto-fix.md
// already says "opt-in" about `--exec` in its opening paragraph, so a
// whole-file substring check would pass vacuously.
//
// Lines inside a fenced code block are never treated as headings. Almost every
// YAML example in docs/ opens with a `# path/to/file` comment, and reading that
// as an H1 outranks any `##` section heading and ends the section at its first
// fence — silently, because the caller still gets a body and still runs its
// assertions, just against a fraction of the prose. That is exactly what
// happened to docs/auto-fix.md's `sandbox.fallback: os-level` section, whose
// example begins `# .atcr/config.yaml`: the section was cut off six lines in and
// TestAutoFixDocsStateFallbackIsOptIn reported prose missing that was four
// paragraphs below the cut.
func markdownSection(ref string, re *regexp.Regexp) (string, bool) {
	reHeading := regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	reFence := regexp.MustCompile("^\\s*(```|~~~)")
	var body []string
	level := 0
	inFence := false
	for _, ln := range strings.Split(ref, "\n") {
		if reFence.MatchString(ln) {
			inFence = !inFence
			if level > 0 {
				body = append(body, ln)
			}
			continue
		}
		var m []string
		if !inFence {
			m = reHeading.FindStringSubmatch(ln)
		}
		if level == 0 {
			if m != nil && re.MatchString(m[2]) {
				level = len(m[1])
			}
			continue
		}
		if m != nil && len(m[1]) <= level {
			break
		}
		body = append(body, ln)
	}
	if level == 0 {
		return "", false
	}
	return strings.Join(body, "\n"), true
}

// TestMarkdownSectionScopesToItsOwnHeading guards the helper against returning
// content from a sibling section, which is the bug that would let a scoped prose
// assertion pass vacuously again.
func TestMarkdownSectionScopesToItsOwnHeading(t *testing.T) {
	doc := "## Intro\nopt-in prose here\n\n## Target\nwanted body\n\n### Nested\nalso wanted\n\n# After\nunwanted"
	body, ok := markdownSection(doc, regexp.MustCompile(`Target`))
	if !ok {
		t.Fatal("markdownSection did not find the Target section")
	}
	if !strings.Contains(body, "wanted body") || !strings.Contains(body, "also wanted") {
		t.Errorf("markdownSection dropped its own body or a nested subsection: %q", body)
	}
	for _, unwanted := range []string{"opt-in prose here", "unwanted"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("markdownSection leaked sibling content %q into the section body", unwanted)
		}
	}
}

// TestMarkdownSectionIgnoresHeadingsInsideFencedBlocks guards the other
// direction: truncating a section EARLY is just as bad as leaking a sibling
// into it, and it is the quieter failure — the assertions built on the section
// still run, they just run against a fraction of the prose and report the rest
// as missing.
//
// A `#` comment is the first line of almost every YAML example in docs/, and
// docs/auto-fix.md's fallback section opens with one (`# .atcr/config.yaml`).
// Read as an H1 that outranks the `##` section heading, it cut that section off
// at its first code fence and made TestAutoFixDocsStateFallbackIsOptIn demand
// prose that was present four paragraphs further down.
func TestMarkdownSectionIgnoresHeadingsInsideFencedBlocks(t *testing.T) {
	for name, fence := range map[string]string{"backtick": "```", "tilde": "~~~"} {
		t.Run(name, func(t *testing.T) {
			doc := "## Target\nbefore fence\n\n" + fence + "yaml\n# .atcr/config.yaml\nsandbox:\n  fallback: os-level\n" + fence +
				"\n\nafter fence\n\n## Sibling\nunwanted"
			body, ok := markdownSection(doc, regexp.MustCompile(`Target`))
			if !ok {
				t.Fatal("markdownSection did not find the Target section")
			}
			if !strings.Contains(body, "after fence") {
				t.Errorf("a `#` comment inside a fenced block truncated the section: %q", body)
			}
			if !strings.Contains(body, "before fence") {
				t.Errorf("markdownSection dropped the body before the fence: %q", body)
			}
			if strings.Contains(body, "unwanted") {
				t.Errorf("markdownSection leaked the sibling section: %q", body)
			}
		})
	}
}

// TestAutoFixDocsStateFallbackIsOptIn asserts the prose carries the facts a
// reader's risk-acceptance decision depends on, and that AC 03-05's Edge Case 1
// names as the failure mode: the field is opt-in (never automatic), and the
// unset default is unchanged. A YAML example alone can imply neither.
//
// The assertion is scoped to the fallback section, not the whole file, because
// docs/auto-fix.md's opening paragraph already says "opt-in" about `--exec`.
func TestAutoFixDocsStateFallbackIsOptIn(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "auto-fix.md"))
	if err != nil {
		t.Fatalf("read auto-fix.md: %v", err)
	}
	ref := string(b)

	// The "automatic fallback" framing is what /refine-epic explicitly rejected.
	// Matched as a regex family, not a fixed blacklist, so a reworded variant
	// ("falls back automatically", "automatically engages") is caught too.
	reAutomatic := regexp.MustCompile(`(?i)automatic\w*\s+(fallback|engage)|falls?\s+back\s+automatic\w*|automatically\s+(falls?\s+back|engages)`)
	if m := reAutomatic.FindString(ref); m != "" {
		t.Errorf("docs/auto-fix.md uses the rejected phrasing %q; the fallback is config-gated, never automatic", m)
	}

	// Anchored to the heading that names the sentinel, not merely one containing
	// the word "fallback": otherwise an earlier fallback-named section could
	// satisfy these assertions while the real one rots.
	section, ok := markdownSection(ref, regexp.MustCompile(regexp.QuoteMeta(registry.SandboxFallbackOSLevel)))
	if !ok {
		t.Fatalf("docs/auto-fix.md has no section heading naming %q", registry.SandboxFallbackOSLevel)
	}
	section = strings.ToLower(section)
	for _, want := range []struct{ token, why string }{
		{"opt-in", "state the field is opt-in"},
		{"unset", "state what the unset default does"},
		{"refus", "describe the neither-backend-usable refusal (AC 03-04)"},
	} {
		if !strings.Contains(section, want.token) {
			t.Errorf("the fallback section of docs/auto-fix.md must %s (no %q found)", want.why, want.token)
		}
	}
}

// autoFixNonParityBullets splits the "What you give up relative to Docker"
// section into its top-level bullets, each returned as its bold lead (the text
// of the first **...**) and its full text with line breaks folded to spaces.
func autoFixNonParityBullets(section string) []struct{ lead, text string } {
	reLead := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	var bullets []struct{ lead, text string }
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		text := strings.Join(cur, " ")
		lead := ""
		if m := reLead.FindStringSubmatch(text); m != nil {
			lead = m[1]
		}
		bullets = append(bullets, struct{ lead, text string }{lead, text})
		cur = nil
	}
	for _, ln := range strings.Split(section, "\n") {
		switch {
		case strings.HasPrefix(ln, "- "):
			flush()
			cur = []string{strings.TrimPrefix(ln, "- ")}
		case len(cur) > 0 && (strings.HasPrefix(ln, " ") || strings.TrimSpace(ln) == ""):
			cur = append(cur, strings.TrimSpace(ln))
		default:
			flush()
		}
	}
	flush()
	return bullets
}

// autoFixNonParityProblems checks the non-parity list against what the OS-level
// backend still leaves unenforced after epic 35.24: CPU and the invoking-user
// identity everywhere, and the memory and PID caps on macOS only. Linux
// enforces both caps (a per-run cgroup v2, or RLIMIT_DATA for memory), so a
// bullet that lists sandbox.memory or sandbox.pids_limit as given up must say
// in its lead that it is about macOS.
func autoFixNonParityProblems(section string) []string {
	bullets := autoFixNonParityBullets(section)
	var problems []string
	var cpu, user bool
	reCapKey := regexp.MustCompile("`sandbox\\.(memory|pids_limit)`")
	reCapLead := regexp.MustCompile(`(?i)memory|\bpids?\b|process|resource cap`)
	reMacOS := regexp.MustCompile(`(?i)macos|darwin`)
	for _, b := range bullets {
		if strings.Contains(b.text, "`sandbox.cpus`") && regexp.MustCompile(`(?i)\bnot\s+enforced\b`).MatchString(b.text) {
			cpu = true
		}
		if regexp.MustCompile(`(?i)\binvoking\s+user\b`).MatchString(b.lead) {
			user = true
		}
		if (reCapKey.MatchString(b.text) || reCapLead.MatchString(b.lead)) && !reMacOS.MatchString(b.lead) {
			problems = append(problems, fmt.Sprintf("bullet %q lists the memory or PID cap as given up without scoping its lead to macOS; linux enforces both", b.lead))
		}
	}
	if !cpu {
		problems = append(problems, "no bullet states that `sandbox.cpus` is not enforced")
	}
	if !user {
		problems = append(problems, "no bullet's lead names the invoking-user identity")
	}
	return problems
}

// TestDocsAudit_AutoFixNonParityList pins docs/auto-fix.md's "What you give up
// relative to Docker" list to what the OS-level backend still does not enforce
// (epic 35.24 AC6): it must name CPU and the invoking-user identity, and must
// not name the linux memory or PID caps, which a per-run cgroup v2 now
// enforces. The darwin memory and PID caps stay listed: XNU rejects lowering
// RLIMIT_AS and RLIMIT_DATA, and RLIMIT_NPROC counts the whole user, not one run.
func TestDocsAudit_AutoFixNonParityList(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "auto-fix.md"))
	if err != nil {
		t.Fatalf("read auto-fix.md: %v", err)
	}
	section, ok := markdownSection(string(b), regexp.MustCompile(`What you give up relative to Docker`))
	if !ok {
		t.Fatal("docs/auto-fix.md has no \"What you give up relative to Docker\" section")
	}
	if len(autoFixNonParityBullets(section)) == 0 {
		t.Fatal("the non-parity section of docs/auto-fix.md has no bullet list")
	}
	for _, p := range autoFixNonParityProblems(section) {
		t.Errorf("docs/auto-fix.md non-parity list: %s", p)
	}
}

// TestDocsAudit_AutoFixNonParityListChecker drives the checker over the
// failures it exists to catch, so a checker that passes everything cannot keep
// TestDocsAudit_AutoFixNonParityList green.
func TestDocsAudit_AutoFixNonParityListChecker(t *testing.T) {
	const user = "- **Runs as the invoking user**, not uid 65534.\n"
	const cpu = "- **No CPU cap.** `sandbox.cpus` passes validation but is\n  **not enforced**.\n"
	const mac = "- **On macOS, no memory or PID cap.** `sandbox.memory` and\n  `sandbox.pids_limit` are not enforced there.\n"
	for _, tc := range []struct {
		name, doc string
		wantOK    bool
	}{
		{"current shape", user + cpu + mac, true},
		{"omits CPU", user + mac, false},
		{"omits invoking user", cpu + mac, false},
		{"linux memory listed", user + cpu + "- **No memory cap.** `sandbox.memory` is not enforced.\n", false},
		{"pid cap unscoped", user + cpu + "- **No PID cap**, on any platform.\n", false},
		{"pre-35.24 resource caps bullet", user + "- **No resource caps.** `sandbox.memory`, `sandbox.cpus`, and `sandbox.pids_limit`\n  still pass config validation but are **not enforced** by this backend.\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := autoFixNonParityProblems("Intro.\n\n" + tc.doc + "\nAfter.\n")
			if gotOK := len(problems) == 0; gotOK != tc.wantOK {
				t.Errorf("ok = %v, want %v; problems: %v", gotOK, tc.wantOK, problems)
			}
		})
	}
}

// TestExecutionDocsDescribeOSLevelBackend guards docs/execution.md, which
// auto-fix.md cross-links as the single source of truth for sandbox mechanics.
// Without this, both passages this sprint corrected could regress — including
// back to "Docker is the only implementation today" — with CI green, since every
// other assertion added here reads auto-fix.md only.
func TestExecutionDocsDescribeOSLevelBackend(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "execution.md"))
	if err != nil {
		t.Fatalf("read execution.md: %v", err)
	}
	ref := string(b)

	// The claim this sprint falsified. Matched loosely so a reworded revival
	// ("the only backend implemented today") is caught too.
	reOnlyImpl := regexp.MustCompile(`(?i)Docker is the only (implementation|backend)|only (implementation|backend) (today|available)`)
	if m := reOnlyImpl.FindString(ref); m != "" {
		t.Errorf("docs/execution.md still claims %q; the OS-level backend is reachable via sandbox.fallback", m)
	}

	// The guarantee list is written in container terms, so the page must name the
	// narrower set the OS-level backend actually delivers.
	section, ok := markdownSection(ref, regexp.MustCompile(`(?i)OS-level`))
	if !ok {
		t.Fatal("docs/execution.md has no section distinguishing the OS-level backend's guarantees from the Docker ones")
	}
	if !strings.Contains(section, registry.SandboxFallbackOSLevel) {
		t.Errorf("the OS-level section of docs/execution.md must name the %q config value", registry.SandboxFallbackOSLevel)
	}
	// It must say what is NOT provided, or it overstates parity (AC 03-05).
	// Token presence alone is not enough: a rewrite claiming the OS-level backend
	// PROVIDES these protections would pass a plain substring check. Bind each
	// token to an explicit negative qualifier in the same sentence.
	for _, want := range []string{"cap-drop", "no-new-privileges"} {
		re := regexp.MustCompile(`(?i)\bnot\b[^.]*\bprovide\b[^.]*\b` + regexp.QuoteMeta(want) + `\b`)
		if !re.MatchString(section) {
			t.Errorf("the OS-level section of docs/execution.md must state that %q is not provided in the same sentence as a negative qualifier", want)
		}
	}
	// The bind set and /tmp distinction are the positive guarantees that make the
	// negative claims meaningful; losing them would let the section silently drift.
	for _, want := range []string{"/usr", "/bin", "/lib"} {
		if !strings.Contains(section, want) {
			t.Errorf("the OS-level section of docs/execution.md must name the Linux bind root %q", want)
		}
	}
	// /tmp is not writable on EITHER platform, but by different mechanisms, and
	// naming both is what stops a reader assuming macOS silently inherits the
	// host's. The previous wording pinned here ("On Linux `/tmp` is a fresh
	// tmpfs") described a profile that still granted the host /tmp on macOS; that
	// grant has been removed, so this tracks the current guarantee rather than
	// the sentence that used to state it.
	// Matched with \s+ between words rather than as a literal: these are prose
	// sentences in a wrapped markdown bullet, so a literal Contains fails the
	// moment the phrase straddles a line break — a reflow, not a regression.
	for _, want := range []string{`fresh\s+` + "`" + `--tmpfs /tmp` + "`", `no\s+write\s+rule\s+for\s+` + "`" + `/tmp` + "`"} {
		if !regexp.MustCompile(want).MatchString(section) {
			t.Errorf("the OS-level section of docs/execution.md must distinguish Linux /tmp from macOS /tmp: no match for %q", want)
		}
	}
	if !strings.Contains(section, "No network egress") {
		t.Errorf("the OS-level section of docs/execution.md must state that network egress is blocked")
	}
}

// TestZeroBudgetRowDocumentsReviewsCap pins the `ok_warning (no input budget)`
// row in docs/registry.md against the verdict as it is actually computed.
// zeroBudgetVerdict used to be suppressed outright whenever doctor's own
// --max-tokens was typed; that guard is gone, and the cap operand is now
// reviewMaxTokens(DeclaredMaxTokens) — the agent's own declaration, else the
// built-in default. A row still describing the suppression tells an operator
// that a flagged doctor run was not budget-checked when it was, which is the
// opposite of the truth and the reason this guard is asserted rather than left
// to review. The hint's cap and the row's own max_tokens column genuinely
// differ under the flag, so the row must say so or the reader treats the two
// numbers as one.
func TestZeroBudgetRowDocumentsReviewsCap(t *testing.T) {
	reg := auditedMarkdown(t)["docs/registry.md"]
	row := zeroBudgetDocRow(t, reg)

	// The retired suppression, in the two forms the row used to state it.
	for _, gone := range []string{
		"Not reported when",
		"suppression is unconditional",
	} {
		if strings.Contains(row, gone) {
			t.Errorf("the no-input-budget row still documents the removed --max-tokens suppression (%q); the verdict now fires regardless of doctor's flag", gone)
		}
	}

	// What the row must say instead: the cap comes from review's resolution
	// order, the flag does not suppress it, and the hint's cap can differ from
	// the row's own column.
	for _, want := range []string{
		`max_tokens` + "` declaration",
		"8192",
		"regardless",
		"differ",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("the no-input-budget row must name %q so the operator can tell which cap the verdict used", want)
		}
	}
}

// zeroBudgetDocRow returns the single `ok_warning (no input budget)` status row
// from docs/registry.md. Isolating the row is what makes the negative
// assertions above meaningful: the marker-absent row directly above it
// legitimately talks about --max-tokens, so a whole-file Contains would pass on
// its text and never see this row drift.
//
// Anchored on the leading status cell, not on the phrase alone — the
// `on_overflow` config row also says "no input budget", and matching it instead
// would make every assertion here vacuous.
func zeroBudgetDocRow(t *testing.T, md string) string {
	t.Helper()
	const cell = "| `ok_warning` (no input budget) |"
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, cell) {
			return line
		}
	}
	t.Fatalf("docs/registry.md has no `ok_warning (no input budget)` status row")
	return ""
}

// TestVocabularyCeilingDocMatchesTheGuard pins docs/benchmark.md's description of
// the out-of-vocabulary ceiling to the comparison the guard actually makes.
//
// ExceedsVocabularyCeiling uses `>=`, cli/benchmark.go words its warning "is at or
// above", and the constant's own doc says INCLUSIVE — but the operator-facing doc
// called the ceiling "exclusive" while, in the same sentence, stating that a run
// sitting exactly on it trips the guard. An operator tuning against that paragraph
// reads the one word, not the clause after it.
//
// Anchored to the live behavior rather than to the prose alone: the oracle is a
// real call at exactly the ceiling, so the guard and the sentence describing it
// cannot drift apart in either direction.
func TestVocabularyCeilingDocMatchesTheGuard(t *testing.T) {
	atCeiling := benchmark.MaxOutOfVocabularyRate
	require.True(t, benchmark.ExceedsVocabularyCeiling(&atCeiling),
		"oracle: a run sitting exactly on the ceiling must trip the guard — the bound is inclusive")

	doc := auditedMarkdown(t)["docs/benchmark.md"]
	i := strings.Index(doc, "ceiling of `0.05`")
	require.GreaterOrEqual(t, i, 0, "docs/benchmark.md must document the 0.05 out-of-vocabulary ceiling")
	// The claim lives in the same sentence as the anchor; bound the window so a
	// legitimate "exclusive" elsewhere in the file cannot satisfy or break this.
	end := i + 200
	if end > len(doc) {
		end = len(doc)
	}
	sentence := doc[i:end]

	require.NotContains(t, sentence, "exclusive",
		"docs/benchmark.md calls the ceiling exclusive, but the guard compares with >= — a run exactly on 0.05 trips it")
	require.Contains(t, sentence, "inclusive",
		"docs/benchmark.md must call the ceiling inclusive, matching ExceedsVocabularyCeiling and the CLI's \"at or above\" wording")
}

// docUnits splits markdown into the units a reader takes in at once: each
// table row on its own, and each blank-line-separated paragraph otherwise.
func docUnits(md string) []string {
	var units []string
	var para []string
	flush := func() {
		if len(para) > 0 {
			units = append(units, strings.Join(para, "\n"))
			para = nil
		}
	}
	for _, ln := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(ln)
		switch {
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "|"):
			flush()
			units = append(units, ln)
		default:
			para = append(para, ln)
		}
	}
	flush()
	return units
}

// undeprecatedExportUnits returns every unit of md that names the retired
// `--export` spelling without also saying it is deprecated.
func undeprecatedExportUnits(md string) []string {
	var bad []string
	for _, u := range docUnits(md) {
		if strings.Contains(u, "--export") && !strings.Contains(strings.ToLower(u), "deprecat") {
			bad = append(bad, u)
		}
	}
	return bad
}

// TestUndeprecatedExportUnitsIsScopedToTheUnit pins the helper: a paragraph or
// table row naming --export passes only when that same unit says deprecated,
// and a deprecation note in a neighbouring row does not cover it.
func TestUndeprecatedExportUnitsIsScopedToTheUnit(t *testing.T) {
	md := "Run `atcr leaderboard --export`.\n\n" +
		"`--export` is deprecated; use `atcr leaderboard export`.\n\n" +
		"| `--export` | off | Deprecated, hidden. |\n" +
		"| `--output` | stdout | With `--export`, the file. |\n\n" +
		"Run `atcr leaderboard export`.\n"
	require.Equal(t, []string{
		"Run `atcr leaderboard --export`.",
		"| `--output` | stdout | With `--export`, the file. |",
	}, undeprecatedExportUnits(md))
}

// TestDocsShowLeaderboardExportOnlyAsDeprecatedAlias (epic 35.22 T7) — the
// public-submission document is `atcr leaderboard export`; `leaderboard
// --export` survives only as a hidden alias removed in
// leaderboardExportAliasRemoval. Every doc, the README, the shipped skill
// files and every command's help text may still name the old spelling, but
// only where the same paragraph or table row says it is deprecated, so no
// reader is taught the retired form as the current one.
func TestDocsShowLeaderboardExportOnlyAsDeprecatedAlias(t *testing.T) {
	sources := auditedMarkdown(t)
	skillFiles, err := filepath.Glob(filepath.Join(repoRootDir(t), "skills", "*", "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, skillFiles)
	for _, p := range skillFiles {
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		rel, _ := filepath.Rel(repoRootDir(t), p)
		sources[rel] = string(b)
	}
	for path, md := range sources {
		for _, u := range undeprecatedExportUnits(md) {
			t.Errorf("%s names --export without saying it is deprecated; write `atcr leaderboard export`:\n%s", path, u)
		}
	}

	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, text := range []string{c.Short, c.Long, c.Example} {
			for _, u := range undeprecatedExportUnits(text) {
				t.Errorf("`%s` help names --export without saying it is deprecated:\n%s", c.CommandPath(), u)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRootCmd())
}

// mdFence is one fenced code block of a markdown page: its info-string
// language, the 1-based line of the opening fence, and its body with the
// fence's own indentation removed (a fence nested in a list item is indented).
type mdFence struct {
	lang string
	line int
	body string
}

// markdownFences returns every ``` or ~~~ fenced block in md, in order. A fence
// closes only on a line holding the same marker, so a ~~~ block may quote ```.
func markdownFences(md string) []mdFence {
	var out []mdFence
	var cur *mdFence
	var marker, indent string
	var body []string
	for i, ln := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(ln)
		if cur != nil {
			if trimmed == marker {
				cur.body = strings.Join(body, "\n")
				out = append(out, *cur)
				cur, body = nil, nil
				continue
			}
			body = append(body, strings.TrimPrefix(ln, indent))
			continue
		}
		for _, m := range []string{"```", "~~~"} {
			if strings.HasPrefix(trimmed, m) {
				marker = m
				indent = ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
				cur = &mdFence{lang: strings.TrimSpace(strings.TrimPrefix(trimmed, m)), line: i + 1}
				break
			}
		}
	}
	return out
}

// withoutDebtTableRows returns md unchanged (stub).
func withoutDebtTableRows(md string) string { return md }

// envVarRef matches an atcr-owned environment variable name in prose or code.
var envVarRef = regexp.MustCompile(`\bATCR_[A-Z0-9_]*[A-Z0-9]\b`)

// knownEnvVars returns every ATCR_* name that appears inside a Go string
// literal anywhere in the module, tests included. String literals rather than
// os.Getenv calls, because atcr also HANDS variables to child processes
// (ATCR_PREVIEW_URL is set for a Playwright spec, never read), and tests
// included because docs name developer knobs that only a test reads
// (ATCR_REPLAY, ATCR_REDTEAM_RUNS). Comments are excluded: a variable that
// survives only in a comment is gone from the binary.
func knownEnvVars(t *testing.T) map[string]bool {
	t.Helper()
	root := repoRootDir(t)
	known := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == ".paperclip" || n == "node_modules" || n == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				for _, v := range envVarRef.FindAllString(lit.Value, -1) {
					known[v] = true
				}
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	return known
}

// undocumentedEnvVars returns, for one page, each ATCR_* name it mentions that
// the module never spells in a string literal.
func undocumentedEnvVars(path, md string, known map[string]bool) []string {
	var errs []string
	seen := map[string]bool{}
	for i, ln := range strings.Split(md, "\n") {
		for _, v := range envVarRef.FindAllString(ln, -1) {
			if !known[v] && !seen[v] {
				seen[v] = true
				errs = append(errs, fmt.Sprintf("%s:%d names environment variable %s, but atcr never reads or sets it", path, i+1, v))
			}
		}
	}
	return errs
}

// TestDocsNameOnlyRealEnvVars (epic 111 T6) fails when a docs page or the
// README names an ATCR_* environment variable that no Go source spells.
func TestDocsNameOnlyRealEnvVars(t *testing.T) {
	known := knownEnvVars(t)
	require.True(t, known["ATCR_TELEMETRY"], "the env-var scan found no ATCR_TELEMETRY; the scan itself is broken")
	for path, content := range auditedMarkdown(t) {
		for _, e := range undocumentedEnvVars(path, content, known) {
			t.Error(e)
		}
	}
}

// TestUndocumentedEnvVarsCatchesAFakeName is the deliberate break for the guard
// above: a page naming a variable atcr lacks is reported, a real one is not.
func TestUndocumentedEnvVarsCatchesAFakeName(t *testing.T) {
	known := knownEnvVars(t)
	// Built by concatenation: spelled whole, this literal would itself make the
	// name "known" to the scan above.
	fake := "ATCR_" + "NO_SUCH_KNOB"
	errs := undocumentedEnvVars("docs/x.md", "set `ATCR_TELEMETRY=0`\nor `"+fake+"=1`\n", known)
	require.Equal(t, []string{"docs/x.md:2 names environment variable " + fake + ", but atcr never reads or sets it"}, errs)

	// A technical-debt row records what is wrong, so it may name a variable
	// atcr lacks; prose on the same page still may not.
	page := debtTableHeader + "| G | [ ] | LOW | a.go:1 | the comment says `" + fake + "=1` | rename it | docs | 5 | paperclip-builder | 0 |\n" +
		"\nThen set `" + fake + "=1`.\n"
	require.Equal(t, []string{"docs/x.md:5 names environment variable " + fake + ", but atcr never reads or sets it"},
		undocumentedEnvVars("docs/x.md", page, known))
}

// debtTableHeader is the technical-debt table layout the company rules fix,
// header and separator, as the test pages below write it.
const debtTableHeader = "| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |\n" +
	"|---|---|---|---|---|---|---|---|---|---|\n"

// TestDebtTableRowsAreNotDocsClaims is the known-good half of the command,
// flag and config-key guards: a technical-debt row quotes the wrong command,
// flag or key it records, and is not reported, while the same names in prose
// or in a config block on that page still are.
func TestDebtTableRowsAreNotDocsClaims(t *testing.T) {
	cmds, groups := canonicalCommands(), commandGroups()
	page := debtTableHeader +
		"| G | [ ] | LOW | docs/x.md:3 | names `atcr frobnicate` and `atcr debt list --no-such-flag` | drop them | docs | 5 | paperclip-builder | 0 |\n" +
		"\nRun `atcr frobnicate`.\n"
	var errs []string
	for _, tokens := range atcrInvocations(withoutDebtTableRows(page), cmds) {
		errs = append(errs, validateInvocationTokens(tokens, "docs/x.md", cmds, groups)...)
	}
	require.Equal(t, []string{"docs/x.md references `atcr frobnicate` but \"frobnicate\" is not a real command"}, errs)

	s := loadConfigSchema()
	page = debtTableHeader +
		"| G | [ ] | LOW | docs/x.md:1 | the block names `providers.<name>.no_such_key` | drop it | docs | 5 | paperclip-builder | 0 |\n" +
		"\n```yaml\nproviders:\n  my-llm:\n    no_such_key: 1\n```\n"
	require.Equal(t, []string{`docs/x.md:4: config block names key "providers.<name>.no_such_key", which no atcr config file has`},
		configBlockErrors("docs/x.md", page, s))

	// Only a table with the debt layout is exempt: an ordinary doc table that
	// names a command atcr lacks is still a claim.
	errs = nil
	for _, tokens := range atcrInvocations(withoutDebtTableRows("| Command | Use |\n|---|---|\n| `atcr frobnicate` | x |\n"), cmds) {
		errs = append(errs, validateInvocationTokens(tokens, "docs/x.md", cmds, groups)...)
	}
	require.Len(t, errs, 1)
}

// configSchema is the set of documented-config key paths the binary accepts:
// the user registry, the project config and the project registry overlay, as
// collectConfigKeys renders them (`<name>` for a user-chosen map key). freeform
// holds the paths whose value is a map of scalars or an untyped value, under
// which any key is accepted.
type configSchema struct {
	paths    map[string]bool
	freeform map[string]bool
	top      map[string]bool
}

func loadConfigSchema() configSchema {
	s := configSchema{paths: map[string]bool{}, freeform: map[string]bool{}, top: map[string]bool{}}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(registry.Registry{}),
		reflect.TypeOf(registry.ProjectConfig{}),
		reflect.TypeOf(registry.ProjectRegistry{}),
	} {
		keys := map[string]string{}
		collectConfigKeys(typ, "", keys)
		for k, typ := range keys {
			s.paths[k] = true
			if !strings.Contains(k, ".") {
				s.top[k] = true
			}
			typ = strings.SplitN(typ, ",", 2)[0]
			if strings.HasPrefix(typ, "map[") && !strings.Contains(typ, "registry.") {
				s.freeform[k] = true
			}
			if typ == "interface {}" || typ == "yaml.Node" || typ == "*yaml.Node" {
				s.freeform[k] = true
			}
		}
	}
	return s
}

// has reports whether path is a key, or the parent of a key, in the schema.
func (s configSchema) has(path string) bool {
	if s.paths[path] {
		return true
	}
	for k := range s.paths {
		if strings.HasPrefix(k, path+".") {
			return true
		}
	}
	return false
}

// unknownKeys walks a YAML node and returns each mapping key path the schema
// does not have. A key unknown at its own name may still be a user-chosen map
// key, so `<name>` is tried before the key is reported.
func (s configSchema) unknownKeys(n *yaml.Node, prefix string) []string {
	var out []string
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			out = append(out, s.unknownKeys(c, prefix)...)
		}
	case yaml.MappingNode:
		parent := strings.TrimSuffix(prefix, ".")
		if parent != "" && s.freeform[parent] {
			return nil
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			switch {
			case s.has(prefix + k):
				out = append(out, s.unknownKeys(n.Content[i+1], prefix+k+".")...)
			case s.has(prefix + "<name>"):
				out = append(out, s.unknownKeys(n.Content[i+1], prefix+"<name>.")...)
			default:
				out = append(out, prefix+k)
			}
		}
	}
	return out
}

// configBlockErrors checks every YAML fence on a page that is an atcr config
// block: one whose top-level keys include an atcr config key. Other YAML on the
// page (a GitHub workflow, an ignore.yaml, a LiteLLM config) has none and is
// left alone. A config block must parse, and every key in it must exist.
func configBlockErrors(path, md string, s configSchema) []string {
	var errs []string
	for _, f := range markdownFences(md) {
		if f.lang != "yaml" && f.lang != "yml" {
			continue
		}
		var probe map[string]any
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(f.body), &doc); err != nil || yaml.Unmarshal([]byte(f.body), &probe) != nil {
			for _, ln := range strings.Split(f.body, "\n") {
				if k, _, ok := strings.Cut(ln, ":"); ok && s.top[k] {
					errs = append(errs, fmt.Sprintf("%s:%d: atcr config block does not parse as YAML: %v", path, f.line, err))
					break
				}
			}
			continue
		}
		isConfig := false
		for k := range probe {
			if s.top[k] {
				isConfig = true
			}
		}
		if !isConfig {
			continue
		}
		for _, k := range s.unknownKeys(&doc, "") {
			errs = append(errs, fmt.Sprintf("%s:%d: config block names key %q, which no atcr config file has", path, f.line, k))
		}
	}
	return errs
}

// TestDocsConfigBlocksNameOnlyRealKeys (epic 111 T6) fails when a docs page or
// the README shows a registry.yaml or .atcr/config.yaml block with a key the
// binary's config schema lacks.
func TestDocsConfigBlocksNameOnlyRealKeys(t *testing.T) {
	s := loadConfigSchema()
	for path, content := range auditedMarkdown(t) {
		for _, e := range configBlockErrors(path, content, s) {
			t.Error(e)
		}
	}
}

// TestConfigBlockErrorsCatchesAFakeKey is the deliberate break for the guard
// above: an unknown nested key and an unknown top-level key beside a real one
// are reported; user-chosen map keys and non-atcr YAML are not.
func TestConfigBlockErrorsCatchesAFakeKey(t *testing.T) {
	s := loadConfigSchema()
	md := "```yaml\nproviders:\n  my-llm:\n    base_url: http://x\n    no_such_key: 1\nfail_on: high\nbogus_top: 1\n```\n\n" +
		"```yaml\non: push\njobs:\n  x:\n    runs-on: ubuntu\n```\n"
	require.Equal(t, []string{
		`docs/x.md:1: config block names key "providers.<name>.no_such_key", which no atcr config file has`,
		`docs/x.md:1: config block names key "bogus_top", which no atcr config file has`,
	}, configBlockErrors("docs/x.md", md, s))
}

// --- Documented examples (epic 111 T6) --------------------------------------
//
// The examples docs-audit/inventory.md classifies hermetic run here, each in a
// fresh copy of one fixture workspace: a git repository with a bare `origin`, an
// `atcr init --offline` roster, a reconciled review with two findings, the debt
// store and scorecard records that reconcile writes, and the files individual
// examples name. Each fence runs as one bash script under `set -eo pipefail`, so
// any command that exits non-zero fails the example and names the command.
//
// `atcr` inside an example is this test binary: the shim on PATH re-executes it
// with docsExampleAsAtcrEnv set, and init() below hands that process straight to
// Main, so an example runs the same command tree the static guards read, with no
// separate build. The child keeps the telemetry transport guard and the consent
// env pinned off, as every test in this package does.

// docsExampleAsAtcrEnv turns the test binary into the atcr CLI (see init).
const docsExampleAsAtcrEnv = "ATCR_DOCS_EXAMPLE_AS_ATCR"

// docsExampleEscapeExit is the exit code a child uses when a telemetry send
// escaped the test-host allowlist, so the example fails rather than phoning home.
const docsExampleEscapeExit = 97

func init() {
	if os.Getenv(docsExampleAsAtcrEnv) != "1" {
		return
	}
	telemetry.SetDoRequestForTest(telemetrytest.GuardHosts)
	code := Main(context.Background(), os.Stdout, os.Stderr)
	if escapes := telemetrytest.Escapes(); len(escapes) > 0 {
		fmt.Fprintf(os.Stderr, "docs example: telemetry send escaped the test-host allowlist: %v\n", escapes)
		code = docsExampleEscapeExit
	}
	os.Exit(code)
}

// docExample names one documented fence and how to run it. A fence is found by
// anchor, a substring that must occur in exactly one fence on the page, so a
// line-number drift does not lose it and an edit that removes it fails loudly.
type docExample struct {
	page   string
	anchor string
	// then lists anchors of later fences on the same page that continue the
	// same procedure; they run in order in the same workspace.
	then []string
	// subst replaces documentation placeholders with fixture values, in order.
	// Values may use {{id}}, {{finding}}, {{run}}, {{ws}} and {{cloud}}.
	subst []string
	// setup prepares the workspace beyond the shared fixture.
	setup func(t *testing.T, w exampleWorkspace)
	// skip is why the fence cannot run without a provider, network or an
	// external tool. A skip is reported as SKIP, never as a pass.
	skip string
	// ownFixture builds the fixture in place instead of copying it. A
	// scorecard run id hashes the review directory's absolute path
	// (scorecard.RunIDForReviewDir), so a copy cannot look its records up by
	// path.
	ownFixture bool
}

// exampleWorkspace is one example's copy of the fixture.
type exampleWorkspace struct {
	dir  string // holds home/, repo/, origin.git/ and tmp/
	repo string
	home string
	env  []string // exampleEnv for this workspace
}

// documentedExamples is every fence docs-audit/inventory.md classifies hermetic.
var documentedExamples = []docExample{
	{page: "docs/agentic-consumption.md", anchor: "atcr diff-smell --staged --json > smell.json"},
	{page: "docs/agentic-consumption.md", anchor: "ATCR_AXI_MAX_LINES=5000 atcr report --format axi"},
	{page: "docs/agentic-consumption.md", anchor: "atcr report --format axi > findings.toon 2> report.log"},
	{page: "docs/auto-fix.md", anchor: `GOMODCACHE="$(go env GOMODCACHE)"`,
		subst: []string{"/path/to/repo", "{{ws}}/gomod", "<atcr cache path>", "{{ws}}/tmp/atcr-modcache"},
		setup: func(t *testing.T, w exampleWorkspace) {
			writeFile(t, filepath.Join(w.dir, "gomod", "go.mod"), "module example.test/x\n\ngo 1.21\n")
		}},
	{page: "docs/benchmark.md", anchor: "atcr benchmark verify --suite-path ./my-suite"},
	{page: "docs/benchmark.md", anchor: "atcr benchmark export --in run.json\n",
		subst: []string{"/tmp/", "{{ws}}/tmp/"}},
	{page: "docs/diff-smell.md", anchor: "atcr diff-smell                             # scan HEAD"},
	{page: "docs/diff-smell.md", anchor: `echo "warning: atcr is too old for the diff-smell gate; skipping it" >&2`},
	{page: "docs/diff-smell.md", anchor: "# .git/hooks/pre-commit"},
	{page: "docs/history.md", anchor: "install -d -m 700 .atcr/history"},
	{page: "docs/history.md", anchor: "atcr history                                   # last 90 days"},
	{page: "docs/history.md", anchor: "atcr history --prune 365d"},
	{page: "docs/payload-modes.md", anchor: "ATCR_REPLAY=1 ATCR_REPLAY_REF=main",
		skip: "a developer measurement: it runs internal/payload's own opt-in test over the atcr checkout's `main` history, not an atcr command"},
	{page: "docs/personas-install.md", anchor: "atcr personas list\n# NAME"},
	{page: "docs/personas-install.md", anchor: "atcr personas list --scores\n"},
	{page: "docs/personas-install.md", anchor: "atcr personas remove security/owasp\n# Removed",
		setup: func(t *testing.T, w exampleWorkspace) {
			writeFile(t, filepath.Join(w.home, ".config", "atcr", "personas", "security", "owasp.yaml"), cmdValidPersonaYAML)
		}},
	{page: "docs/personas-install.md", anchor: "atcr personas test delia\n# PASS"},
	{page: "docs/registry.md", anchor: "atcr trust                 # list project providers"},
	{page: "docs/release-process.md", anchor: "go run ./cmd/changelog assemble --version 0.6.0",
		setup: copyChangelogModule},
	{page: "docs/release-process.md", anchor: "golangci-lint run --timeout 5m --config ../.golangci.yml",
		skip: "needs the external golangci-lint binary and reruns the reconcile module's whole race suite, which CI's reconcile job already runs"},
	{page: "docs/scorecard.md", anchor: "# By run_id", ownFixture: true,
		subst: []string{"2026-06-14T10:00:00Z-abc123", "{{run}}"}},
	{page: "docs/scorecard.md", anchor: "# Default: last 30 days"},
	{page: "docs/scorecard.md", anchor: "# Anonymized JSON to stdout",
		subst: []string{"/tmp/", "{{ws}}/tmp/"}},
	{page: "docs/scorecard.md", anchor: "atcr personas list --pairs"},
	{page: "docs/scorecard.md", anchor: "atcr personas list --scores --detail bruce"},
	{page: "docs/scorecard.md", anchor: "atcr reconcile --no-scorecard"},
	{page: "docs/skill-usage.md", anchor: "atcr skill export                 # -> .claude/skills/atcr/"},
	{page: "docs/skill-usage.md", anchor: "atcr skill export --dest ~/.someagent/skills/atcr"},
	{page: "docs/skill-usage.md", anchor: "atcr debt resolve                        # preview open items",
		subst: []string{"<id>", "{{id}}"}},
	{page: "docs/technical-debt.md", anchor: "atcr debt convert --dry-run   # see what would move"},
	{page: "docs/technical-debt.md", anchor: "git mv docs/technical-debt/README.md docs/technical-debt/README.import.md",
		then: []string{
			"atcr debt import docs/technical-debt/README.import.md --store docs/technical-debt/store --dry-run",
			"atcr debt import docs/technical-debt/README.import.md --store docs/technical-debt/store\n",
			"atcr debt report --format md --output docs/technical-debt/README.md --store docs/technical-debt/store",
		}},
	{page: "docs/technical-debt.md", anchor: "atcr debt locks --json --store <store>",
		subst: []string{"<store>", ".atcr/debt", "<your name>", "alice", "<README>", "docs/technical-debt/README.md", "<id>", "{{finding}}"}},
	{page: "docs/technical-debt.md", anchor: "atcr debt list                                  # everything"},
	{page: "docs/technical-debt.md", anchor: "atcr debt add \\\n"},
	{page: "docs/technical-debt.md", anchor: "atcr debt import docs/technical-debt/README.md --dry-run"},
	{page: "docs/technical-debt.md", anchor: "atcr debt dashboard                                   # print the dashboard"},
	{page: "docs/technical-debt.md", anchor: "atcr debt report                                         # print the report"},
	{page: "docs/technical-debt.md", anchor: "atcr debt resolve                              # open items",
		subst: []string{"<id>", "{{id}}"}},
	{page: "docs/technical-debt.md", anchor: "atcr debt recluster <id> --kind wrong-merge",
		subst: []string{"<finding-id>", "{{finding}}", "<id>", "{{id}}"}},
	{page: "docs/technical-debt.md", anchor: "atcr debt compact --dry-run"},
	{page: "docs/technical-debt.md", anchor: "atcr debt convert --dry-run   # report what a real run would move"},
	{page: "docs/technical-debt.md", anchor: "atcr debt backfill-justifications --dry-run  # reports"},
	{page: "docs/technical-debt.md", anchor: "$ atcr debt backfill-justifications --dry-run"},
	{page: "docs/technical-debt.md", anchor: "atcr debt sync-outcomes --td path/to/technical-debt/README.md",
		subst: []string{"path/to/technical-debt/README.md", "docs/technical-debt/README.md"}},
	{page: "docs/technical-debt.md", anchor: "atcr debt validate                          # ground every open row"},
	{page: "docs/technical-debt.md", anchor: "atcr debt lock <id>...",
		// Three alternative usages, not one sequence: lock and unlock one item,
		// and lock the other under an explicit holder.
		subst: []string{"lock <id>...", "lock {{finding}}", "unlock <id>...", "unlock {{finding}}", "<id> --holder", "{{id}} --holder"}},
	{page: "docs/technical-debt.md", anchor: "atcr debt locks                      # table"},
	{page: "docs/technical-debt.md", anchor: "atcr ignore 8421025ce7cde4d3",
		subst: []string{"8421025ce7cde4d3", "{{id}}", "2026-10-20-abcdef", "abc123"}},
	{page: "docs/technical-debt.md", anchor: "$ atcr debt add --severity HIGH --file internal/x/y.go:12"},
	{page: "docs/technical-debt.md", anchor: "#!/usr/bin/env bash",
		setup: func(t *testing.T, w exampleWorkspace) {
			out, err := runDocExample(w.repo, w.env, "atcr debt dashboard --output docs/debt-report.md")
			require.NoError(t, err, "%s", out)
		}},
	{page: "docs/telemetry.md", anchor: "atcr config set telemetry false   # disable"},
	{page: "docs/telemetry.md", anchor: "export ATCR_API_KEY=<your-key>",
		subst: []string{"<your-key>", "docs-example-key", "--sync-cloud ...", "--sync-cloud --cloud-endpoint {{cloud}}"}},
	{page: "docs/telemetry.md", anchor: "atcr config set quality_signal true"},
	{page: "docs/verification.md", anchor: "atcr diff-smell --staged --fail-on hard"},
}

// locateFence returns the single fence on md that contains anchor. When more
// than one does, a fence whose whole body is the anchor wins.
func locateFence(md, anchor string) (mdFence, error) {
	var hits []mdFence
	for _, f := range markdownFences(md) {
		if strings.Contains(f.body+"\n", anchor) {
			hits = append(hits, f)
		}
	}
	if len(hits) > 1 {
		var exact []mdFence
		for _, f := range hits {
			if strings.TrimSpace(f.body) == strings.TrimSpace(anchor) {
				exact = append(exact, f)
			}
		}
		if len(exact) == 1 {
			return exact[0], nil
		}
	}
	if len(hits) != 1 {
		return mdFence{}, fmt.Errorf("anchor %q matches %d fences, want exactly 1", anchor, len(hits))
	}
	return hits[0], nil
}

// exampleScript turns a fence into the script that runs it. A console fence
// keeps only its `$ ` prompt lines and their continuations; the rest is output.
func exampleScript(f mdFence) string {
	if f.lang != "console" {
		return f.body
	}
	var out []string
	cont := false
	for _, ln := range strings.Split(f.body, "\n") {
		switch {
		case strings.HasPrefix(ln, "$ "):
			ln = strings.TrimPrefix(ln, "$ ")
		case !cont:
			continue
		}
		out = append(out, ln)
		cont = strings.HasSuffix(ln, "\\")
	}
	return strings.Join(out, "\n")
}

// unfilledPlaceholder matches a `<placeholder>` left in a script after
// substitution; running one would hand atcr a literal "<id>".
var unfilledPlaceholder = regexp.MustCompile(`<[a-z][a-z -]*>`)

// runDocExample runs script under bash in dir with env and returns its
// combined output. A non-zero exit is an error naming the failing command.
// env must set HOME: an empty env would inherit the developer's own, and the
// example would write to their real ~/.config/atcr.
func runDocExample(dir string, env []string, script string) (string, error) {
	if !slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "HOME=") }) {
		return "", fmt.Errorf("refusing to run a docs example without a fixture HOME in its environment")
	}
	if m := unfilledPlaceholder.FindString(script); m != "" {
		return "", fmt.Errorf("placeholder %s has no fixture value", m)
	}
	prelude := "set -eo pipefail\ntrap 'echo \"docs example: command failed (exit $?): $BASH_COMMAND\" >&2' ERR\n"
	cmd := exec.Command("bash", "-c", prelude+script)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%v\n%s", err, out)
	}
	return string(out), nil
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

// goEnvValue reads one `go env` value from the developer's toolchain, so an
// example that runs go reuses the warm module and build caches under its own
// fixture HOME instead of reaching for the network.
func goEnvValue(t *testing.T, key string) string {
	t.Helper()
	out, err := exec.Command("go", "env", key).Output()
	require.NoError(t, err, "go env %s", key)
	return strings.TrimSpace(string(out))
}

// exampleEnv is the whole environment an example runs with: nothing is
// inherited but PATH (behind the atcr shim) and the go caches.
func exampleEnv(t *testing.T, w exampleWorkspace, shimDir string) []string {
	return []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + w.home,
		"TMPDIR=" + filepath.Join(w.dir, "tmp"),
		"USER=docs",
		"ATCR_TELEMETRY=0",
		"ATCR_QUALITY_SIGNAL=0",
		"GIT_AUTHOR_NAME=" + gitexec.FixtureName, "GIT_AUTHOR_EMAIL=" + gitexec.FixtureEmail,
		"GIT_COMMITTER_NAME=" + gitexec.FixtureName, "GIT_COMMITTER_EMAIL=" + gitexec.FixtureEmail,
		"GIT_CONFIG_NOSYSTEM=1",
		"GOPROXY=off", "GOTOOLCHAIN=local", "GOFLAGS=",
		"GOCACHE=" + goEnvValue(t, "GOCACHE"),
		"GOMODCACHE=" + goEnvValue(t, "GOMODCACHE"),
	}
}

// writeAtcrShim writes the `atcr` on PATH that re-executes this test binary as
// the CLI and returns its directory.
func writeAtcrShim(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	dir := t.TempDir()
	shim := fmt.Sprintf("#!/bin/sh\n%s=1 exec %s \"$@\"\n", docsExampleAsAtcrEnv, strconv.Quote(self))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "atcr"), []byte(shim), 0o755))
	return dir
}

// buildExampleFixture builds the shared workspace every example copies.
func buildExampleFixture(t *testing.T, shimDir string) exampleWorkspace {
	t.Helper()
	root := repoRootDir(t)
	dir := t.TempDir()
	w := exampleWorkspace{dir: dir, repo: filepath.Join(dir, "repo"), home: filepath.Join(dir, "home")}
	for _, d := range []string{w.repo, w.home, filepath.Join(dir, "tmp")} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	w.env = exampleEnv(t, w, shimDir)
	sh := func(script string) {
		t.Helper()
		out, err := runDocExample(w.repo, w.env, script)
		require.NoError(t, err, "fixture step failed:\n%s\n%s", script, out)
	}

	writeFile(t, filepath.Join(w.repo, "a.go"), "package x\n")
	sh("git init -q -b main && git add -A && git commit -qm one")
	writeFile(t, filepath.Join(w.repo, "internal", "x", "y.go"), "package x\n\nfunc f() {\n\tfor {\n\t}\n}\n")
	sh("git add -A && git commit -qm two && git diff HEAD~1 HEAD > patch.diff")
	sh("git init -q --bare ../origin.git && git remote add origin ../origin.git && git push -q origin main")
	sh("atcr init --offline </dev/null >/dev/null")

	// A review with the same two findings from two reviewers, reconciled: the
	// debt store, the scorecard store and .atcr/latest all come from it. Both
	// sit below the roster's `fail_on: high`, so the reconcile examples pass
	// the gate.
	for _, p := range []string{"bruce", "sasha"} {
		writeFile(t, filepath.Join(w.repo, ".atcr", "reviews", "abc123", "sources", p, "findings.txt"),
			"# atcr-findings/v1\n"+
				"MEDIUM|internal/x/y.go:4|unbounded retry loop|cap retries|correctness|30|for {} never exits|"+p+"\n"+
				"LOW|a.go:1|style nit|rename|style|5|exported name|"+p+"\n")
	}
	writeFile(t, filepath.Join(w.repo, ".atcr", "reviews", "abc123", "sources", "pool", "summary.json"),
		`{"agents":[{"agent":"bruce","status":"ok","model":"claude-sonnet-4-6","findings_count":2},`+
			`{"agent":"sasha","status":"ok","model":"claude-sonnet-4-6","findings_count":2}],"total":2,"succeeded":2}`)
	require.NoError(t, os.MkdirAll(filepath.Join(w.repo, ".atcr", "reviews", "abc123", "reconciled"), 0o755))
	writeFile(t, filepath.Join(w.repo, ".atcr", "latest"), "abc123\n")
	sh("atcr reconcile >/dev/null 2>&1")

	// The files individual examples name.
	sh("mkdir -p docs/technical-debt && atcr debt report --format md --output docs/technical-debt/README.md >/dev/null")
	writeFile(t, filepath.Join(w.repo, ".planning", "history", "2026-06.jsonl"), "{}\n")
	writeFile(t, filepath.Join(w.repo, ".atcr", "registry.yaml"),
		"providers:\n  team-llm:\n    base_url: https://llm.example.test/v1\n    api_key_env: TEAM_LLM_KEY\n")
	require.NoError(t, os.Symlink(filepath.Join(root, "benchmarks"), filepath.Join(w.repo, "benchmarks")))
	require.NoError(t, os.Symlink(filepath.Join(root, "benchmarks", "standard-v1"), filepath.Join(w.repo, "my-suite")))
	writeFile(t, filepath.Join(w.repo, "run.json"), fullCoverageRunResult(t, filepath.Join(root, "benchmarks", "standard-v1", "suite.json")))
	sh("git add -A && git commit -qm fixtures && git push -q origin main")
	return w
}

// fullCoverageRunResult writes a run-result in which one reviewer scored every
// case the suite declares, the shape `atcr benchmark export` accepts.
func fullCoverageRunResult(t *testing.T, suitePath string) string {
	t.Helper()
	raw, err := os.ReadFile(suitePath)
	require.NoError(t, err)
	var suite struct {
		Suite        string `json:"suite"`
		SuiteVersion string `json:"suite_version"`
		Cases        []struct {
			ID string `json:"id"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &suite))
	ids := make([]string, 0, len(suite.Cases))
	for _, c := range suite.Cases {
		ids = append(ids, c.ID)
	}
	body, err := json.Marshal(map[string]any{
		"suite": suite.Suite, "suite_version": suite.SuiteVersion, "generated_at": "2026-06-24T12:00:00Z",
		"suite_case_ids":    ids,
		"reviewer_coverage": []any{map[string]any{"model": "m-primary", "persona": "bruce", "case_ids": ids}},
		"reviewers": []any{map[string]any{"model": "m-primary", "persona": "bruce", "runs": len(ids),
			"findings_raised_avg": 1.0, "corroboration_rate": 0.5, "latency_p50_ms": 10}},
	})
	require.NoError(t, err)
	return string(body)
}

// copyChangelogModule gives the release-process example a module of its own
// holding cmd/changelog, its one internal import, and the repo's changelog, so
// assembling a release writes there and never to this checkout.
func copyChangelogModule(t *testing.T, w exampleWorkspace) {
	t.Helper()
	root := repoRootDir(t)
	for _, rel := range []string{"go.mod", "go.sum", "CHANGELOG.md"} {
		b, err := os.ReadFile(filepath.Join(root, rel))
		require.NoError(t, err)
		writeFile(t, filepath.Join(w.repo, rel), string(b))
	}
	for _, dir := range []string{"cmd/changelog", "internal/gitexec", "changelog.d"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		require.NoError(t, err)
		for _, e := range entries {
			if e.IsDir() || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(root, dir, e.Name()))
			require.NoError(t, err)
			writeFile(t, filepath.Join(w.repo, dir, e.Name()), string(b))
		}
	}
}

// copyWorkspace copies the fixture for one example; cp -a keeps the symlinks
// and the relative `origin` remote intact.
func copyWorkspace(t *testing.T, src exampleWorkspace) exampleWorkspace {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "ws")
	out, err := exec.Command("cp", "-a", src.dir, dst).CombinedOutput()
	require.NoError(t, err, "copy fixture: %s", out)
	return exampleWorkspace{dir: dst, repo: filepath.Join(dst, "repo"), home: filepath.Join(dst, "home")}
}

// fixtureIDs reads the ids examples substitute for <id> and friends: the MEDIUM
// and LOW debt items, and the scorecard run id of the reconciled review.
func fixtureIDs(t *testing.T, w exampleWorkspace) (id, finding, run string) {
	t.Helper()
	out, err := runDocExample(w.repo, w.env, "atcr debt list --json")
	require.NoError(t, err)
	var items []struct {
		ID       string `json:"id"`
		Severity string `json:"severity"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &items), "debt list --json: %s", out)
	for _, it := range items {
		switch it.Severity {
		case "MEDIUM":
			id = it.ID
		case "LOW":
			finding = it.ID
		}
	}
	require.NotEmpty(t, id, "fixture has no MEDIUM debt item")
	require.NotEmpty(t, finding, "fixture has no LOW debt item")
	out, err = runDocExample(w.repo, w.env, "atcr scorecard ./.atcr/reviews/abc123 --json")
	require.NoError(t, err)
	var sc struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sc), "scorecard --json: %s", out)
	require.NotEmpty(t, sc.RunID, "fixture review has no scorecard run id")
	return id, finding, sc.RunID
}

// TestDocsHermeticExamplesRun (epic 111 T6) runs every documented example the
// inventory classifies hermetic and fails when one exits non-zero.
func TestDocsHermeticExamplesRun(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatal("bash is required to run the documented examples")
	}
	shim := writeAtcrShim(t)
	fixture := buildExampleFixture(t, shim)
	id, finding, run := fixtureIDs(t, fixture)

	var cloudPosts atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Header.Get("Authorization") == "Bearer docs-example-key" {
			cloudPosts.Add(1)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer cloud.Close()

	pages := auditedMarkdown(t)
	for _, ex := range documentedExamples {
		md, ok := pages[ex.page]
		require.True(t, ok, "%s is not an audited page", ex.page)
		var scripts []string
		line := 0
		for _, a := range append([]string{ex.anchor}, ex.then...) {
			f, err := locateFence(md, a)
			require.NoError(t, err, "%s", ex.page)
			if line == 0 {
				line = f.line
			}
			scripts = append(scripts, exampleScript(f))
		}
		// page:line names the subtest: a bare name keeps shell metacharacters
		// out of the t.TempDir paths an example's commands are handed.
		t.Run(fmt.Sprintf("%s:%d", strings.TrimPrefix(ex.page, "docs/"), line), func(t *testing.T) {
			if ex.skip != "" {
				t.Skipf("not hermetic: %s", ex.skip)
			}
			var w exampleWorkspace
			id, finding, run := id, finding, run
			if ex.ownFixture {
				w = buildExampleFixture(t, shim)
				id, finding, run = fixtureIDs(t, w)
			} else {
				w = copyWorkspace(t, fixture)
				w.env = exampleEnv(t, w, shim)
			}
			if ex.setup != nil {
				ex.setup(t, w)
			}
			fill := strings.NewReplacer("{{id}}", id, "{{finding}}", finding, "{{run}}", run,
				"{{ws}}", w.dir, "{{cloud}}", cloud.URL)
			for _, s := range scripts {
				for i := 0; i+1 < len(ex.subst); i += 2 {
					s = strings.ReplaceAll(s, ex.subst[i], fill.Replace(ex.subst[i+1]))
				}
				out, err := runDocExample(w.repo, w.env, s)
				require.NoError(t, err, "%s example failed:\n%s", ex.page, s)
				t.Logf("%s", out)
			}
		})
	}
	require.Positive(t, cloudPosts.Load(), "the --sync-cloud example never reached the fake cloud endpoint with its documented key")
}

// TestDocsReferenceOnlyRealCommandsCatchesAFakeCommandAndFlag is the deliberate
// break for the command and flag guard: a page naming a command and a flag the
// binary lacks is reported for both.
func TestDocsReferenceOnlyRealCommandsCatchesAFakeCommandAndFlag(t *testing.T) {
	cmds, groups := canonicalCommands(), commandGroups()
	md := "Run `atcr frobnicate` first.\n\n```bash\natcr debt list --no-such-flag\natcr debt list --json\n```\n"
	var errs []string
	for _, tokens := range atcrInvocations(md, cmds) {
		errs = append(errs, validateInvocationTokens(tokens, "docs/x.md", cmds, groups)...)
	}
	require.Equal(t, []string{
		"docs/x.md references `atcr frobnicate` but \"frobnicate\" is not a real command",
		"docs/x.md references `--no-such-flag` on an `atcr debt` command, but no such flag exists",
	}, errs)
}

// TestLocateFenceRequiresExactlyOne pins the anchor lookup: an anchor that no
// fence holds, or that two fences hold equally, is an error rather than a
// silently skipped or arbitrarily chosen example.
func TestLocateFenceRequiresExactlyOne(t *testing.T) {
	md := "```bash\natcr debt list\n```\n\n```bash\natcr debt list\natcr debt list --json\n```\n\n```bash\natcr history\natcr history --since 30d\n```\n"
	f, err := locateFence(md, "atcr debt list")
	require.NoError(t, err, "a fence whose whole body is the anchor wins over one that merely contains it")
	require.Equal(t, 1, f.line)
	_, err = locateFence(md, "atcr history --since 30d")
	require.NoError(t, err)
	_, err = locateFence(md, "atcr leaderboard")
	require.ErrorContains(t, err, "matches 0 fences")
	_, err = locateFence(md+"```bash\natcr history\natcr history --since 30d\n```\n", "atcr history --since 30d")
	require.ErrorContains(t, err, "matches 2 fences, want exactly 1")
}

// TestExampleScriptKeepsOnlyConsolePrompts pins that a console fence runs its
// prompt lines (with continuations) and never its printed output.
func TestExampleScriptKeepsOnlyConsolePrompts(t *testing.T) {
	f := mdFence{lang: "console", body: "$ atcr debt add --severity HIGH \\\n    --file a.go:1\nAdded HIGH item 1234.\n$ atcr debt list\nID  SEVERITY"}
	require.Equal(t, "atcr debt add --severity HIGH \\\n    --file a.go:1\natcr debt list", exampleScript(f))
	f.lang = "bash"
	require.Equal(t, f.body, exampleScript(f))
}

// TestRunDocExampleFailsABrokenExample is the deliberate break for the
// example runner: a documented command that exits non-zero, a placeholder with
// no fixture value, and an environment without a fixture HOME all fail, and
// the failure names the command.
func TestRunDocExampleFailsABrokenExample(t *testing.T) {
	shim := writeAtcrShim(t)
	dir := t.TempDir()
	w := exampleWorkspace{dir: dir, repo: dir, home: filepath.Join(dir, "home")}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tmp"), 0o755))
	env := exampleEnv(t, w, shim)

	out, err := runDocExample(dir, env, "atcr debt list --help >/dev/null")
	require.NoError(t, err, "a working example passes: %s", out)

	_, err = runDocExample(dir, env, "atcr debt list --help >/dev/null\natcr debt list --no-such-flag\natcr debt list --help")
	require.Error(t, err)
	require.Contains(t, err.Error(), "command failed (exit 2): atcr debt list --no-such-flag")

	_, err = runDocExample(dir, env, "atcr debt list --help | atcr frobnicate")
	require.Error(t, err, "pipefail: a failure on the right of a pipe still fails the example")

	_, err = runDocExample(dir, env, "atcr debt resolve <id>")
	require.ErrorContains(t, err, "placeholder <id> has no fixture value")

	_, err = runDocExample(dir, nil, "true")
	require.ErrorContains(t, err, "without a fixture HOME")
}

// TestDocExampleTelemetryEscapeFails pins the hermeticity of the example
// runner: a child that sends the usage ping to a real host is intercepted by
// the transport guard and exits docsExampleEscapeExit, failing the example.
func TestDocExampleTelemetryEscapeFails(t *testing.T) {
	shim := writeAtcrShim(t)
	dir := t.TempDir()
	w := exampleWorkspace{dir: dir, repo: dir, home: filepath.Join(dir, "home")}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tmp"), 0o755))
	writeFile(t, filepath.Join(dir, ".atcr", "reviews", "r", "sources", "bruce", "findings.txt"),
		"# atcr-findings/v1\nLOW|a.go:1|x|f|style|1|ev|bruce\n")
	writeFile(t, filepath.Join(dir, ".atcr", "latest"), "r\n")
	writeFile(t, filepath.Join(dir, "a.go"), "package x\n")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".atcr", "reviews", "r", "reconciled"), 0o755))

	_, err := runDocExample(dir, exampleEnv(t, w, shim), "git init -q && atcr init --offline </dev/null >/dev/null && ATCR_TELEMETRY=1 atcr reconcile")
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("exit %d", docsExampleEscapeExit))
	require.Contains(t, err.Error(), "telemetry send escaped the test-host allowlist")
}
