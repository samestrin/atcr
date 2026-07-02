package main

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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// repoRootDir ascends from the test working directory (the package dir,
// cmd/atcr) until it finds go.mod, returning the repository root.
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
	walk(newRootCmd())
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
	walk(newRootCmd())
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
	walk(newRootCmd())
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
	cur := newRootCmd()
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
func validateSubcommandChain(tokens []string, idx int, groups map[string]map[string]bool, isWord func(string) bool, reFlag *regexp.Regexp, path string, errs *[]string) {
	name := tokens[idx]
	children, isGroup := groups[name]
	if !isGroup || len(tokens) <= idx+1 {
		return
	}
	nextIdx := -1
	for i := idx + 1; i < len(tokens); i++ {
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
	validateSubcommandChain(tokens, nextIdx, groups, isWord, reFlag, path, errs)
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
		validateSubcommandChain(tokens, 0, groups, isWord, reFlag, path, &errs)
	}
	// Validate each --flag against the flags reachable on the *specific* command
	// this invocation addresses (per-command inheritance), not the global union of
	// every flag in the tree. This rejects e.g. `atcr review --checkpoint`, where
	// --checkpoint is real but belongs to `benchmark run`, matching the per-command
	// scope the error message claims.
	flags := reachableFlags(tokens)
	for _, tok := range tokens {
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
		if !strings.Contains(ref, block) {
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

// TestArchitectureDocDescribesReconciler asserts that docs/architecture.md
// contains the key terms of the real multi-model reconciler pipeline (AC3). The
// required tokens are the pipeline stages (review, reconcile, verify, debate)
// and the reconciler's core operations (cluster, dedupe, confidence, persona).
// This is a keyword-presence guard, not a structural guarantee: a stub that
// sprinkles these terms could pass, but a doc that omits any of them fails.
func TestArchitectureDocDescribesReconciler(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "architecture.md"))
	if err != nil {
		t.Fatalf("docs/architecture.md must exist (AC3): %v", err)
	}
	lower := strings.ToLower(string(b))
	for _, term := range []string{"review", "reconcile", "cluster", "dedupe", "confidence", "verify", "debate", "persona"} {
		if !strings.Contains(lower, term) {
			t.Errorf("docs/architecture.md does not describe the %q stage/concept of the reconciler", term)
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
