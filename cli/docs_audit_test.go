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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/verify"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
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
// documentation source of truth: every docs/*.md plus the root README.md.
// testdata fixtures and READMEs elsewhere in the tree are intentionally out of
// scope (epic 15.0 clarifications).
func auditedMarkdown(t *testing.T) map[string]string {
	t.Helper()
	root := repoRootDir(t)
	out := map[string]string{}
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatalf("glob docs: %v", err)
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
