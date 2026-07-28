package verify

// diffsmell.go is a PORT of llm-tools' `diff-smell` analyzer
// (github.com/samestrin/llm-tools, internal/support/commands/diff_smell.go) —
// copied and adapted rather than imported, because the upstream function lives
// under that module's own internal/ and is therefore not importable across
// modules. Like the original it depends only on stdlib regexp/strings, so the
// port adds no dependency to go.mod (Epic 35.3).
//
// Ported from upstream commit e885c953eddb45c648467a9f6f43bf3ebc586560 — the
// sole commit that introduced and last touched diff_smell.go. The SHA, not a
// release tag, is the drift anchor: it names the state of llm-tools'
// internal/support/commands package this file was copied from, and the v1.5.0
// tag this header used to cite PREDATES the file, so it never identified a
// version of it.
//
// Drift is tracked ONE-WAY, by deliberate choice: testdata/diffsmell/ holds a
// corpus of .diff files with the verdicts atcr's OWN analyzeDiff must produce
// (TestAnalyzeDiff_Corpus). It is NOT automatically verified against upstream —
// a two-way check would have to vendor the upstream analyzer or shell out to an
// installed llm-support binary, reintroducing exactly the cross-module coupling
// this port exists to avoid. When upstream changes, replay the corpus by hand
// and record any new divergence in the list below.
//
// It scans a unified diff for the mechanical fingerprints of an
// over-simplified ("reward-hacked") patch: a fix that makes a test pass by
// deleting the test or weakening its assertions, or that resolves a lint by
// suppressing it. generateFixes uses it as a pre-write gate on executor-produced
// fixes.
//
// Divergences from upstream, all deliberate:
//
//   - Types and helpers are unexported and prefixed `smell*` to fit this package.
//   - Callers must pre-filter with looksLikeUnifiedDiff. Upstream is always fed a
//     real diff (a file path or a git rev); here a Finding.Fix is free-form, so
//     non-diff content must be classified clean rather than parsed as one.
//   - A `+++ /dev/null` deletion hunk stays bound to its file instead of unbinding
//     the parser, and a deleted test file raises the new HARD `test_deleted`
//     smell. Upstream drops both, so deleting a whole test file alongside any
//     implementation change scored `clean` — the single most blatant reward hack.
//   - An added line that DISABLES a test (`t.Skip(`, `@pytest.mark.skip`,
//     `it.skip(`, `xit(`, `@Ignore`, `@Disabled`, `#[ignore]`, …) raises the new
//     HARD `test_skipped` smell. Upstream has no skip detector, so adding a skip
//     alongside any implementation change scored `clean` — the cheapest way there
//     is to make a failing test pass.
//   - A test file renamed to a non-test path raises the new HARD
//     `test_renamed_away` smell. Upstream records only the b/ side of the
//     `diff --git` header, so `foo_test.go` -> `foo_disabled.go` scored clean AND
//     counted the result as an implementation file, suppressing `test_only` for
//     everything else in the same diff.
//   - Assertions replaced one-for-one raise a SOFT `weakened_assertion`. Upstream
//     only fires on a net LOSS, so swapping a strong assertion for a weak one
//     scored clean.
//
// The `test_only` suppression for test-file findings is NOT applied here — the
// analyzer stays a faithful port in that respect, and the gate applies that
// policy at the call site where the finding's own path is known.

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Bounds on the rejection feedback fed back into the retry prompt. Every smell's
// Evidence is a verbatim added line from the MODEL's own diff, so without these a
// crafted fix (up to maxFixBytes) could balloon the retry prompt by orders of
// magnitude — paid for in tokens on every rejected fix. The type and severity
// names, which carry the actionable signal, are a closed vocabulary and are never
// truncated; only the quoted evidence and the item count are bounded.
// The FILE PATH is model-controlled too — it comes verbatim from
// `+++ b/<anything>` via smellHeaderPath, which caps nothing — so it is bounded
// on the same footing as the evidence, and the whole rendered string is capped as
// a backstop.
const (
	maxSmellEvidenceRunes = 200
	maxSmellFeedbackItems = 10
	maxSmellPathRunes     = 200
	maxSmellFeedbackRunes = maxSmellFeedbackItems * (maxSmellEvidenceRunes + maxSmellPathRunes + 64)
)

// Verdict values, mirroring upstream's summary.verdict.
const (
	smellVerdictClean    = "clean"
	smellVerdictSoftOnly = "soft_only"
	smellVerdictHard     = "hard"
)

// Smell type names, mirroring upstream's smell.type.
const (
	smellTestOnly          = "test_only"
	smellWeakenedAssertion = "weakened_assertion"
	// smellTestDeleted is an atcr addition, not an upstream type: upstream has no
	// deletion detector at all. See the deletion branch in analyzeDiff.
	smellTestDeleted = "test_deleted"
	// smellTestSkipped is an atcr addition, not an upstream type: upstream has no
	// skip detector. See the skip branch in analyzeDiff.
	smellTestSkipped = "test_skipped"
	// smellTestRenamedAway is an atcr addition, not an upstream type: renaming a
	// test file to a non-test path disables it as effectively as deleting it.
	smellTestRenamedAway = "test_renamed_away"
	smellSuppression     = "suppression"
	smellEmptyCatch      = "empty_catch"
	smellStubBody        = "stub_body"
)

// Severity values, mirroring upstream's smell.severity. HARD smells reject a
// fix; SOFT smells accept it with a NEEDS_REVIEW annotation.
const (
	smellSeverityHard = "hard"
	smellSeveritySoft = "soft"
)

// smell is one over-simplification fingerprint found in a diff.
type smell struct {
	Type     string
	Severity string
	File     string
	Evidence string
}

// smellFiles lists the changed files split by role.
type smellFiles struct {
	Test []string
	Impl []string
}

// smellSummary aggregates the scan.
type smellSummary struct {
	TestFiles int
	ImplFiles int
	Hard      int
	Soft      int
	ByType    map[string]int
	Verdict   string
}

// smellResult is the full diff-smell output.
type smellResult struct {
	Files   smellFiles
	Smells  []smell
	Summary smellSummary
}

// --- detectors ---

var (
	smellGoTestRe  = regexp.MustCompile(`_test\.go$`)
	smellJSTestRe  = regexp.MustCompile(`\.(test|spec)\.[cm]?[jt]sx?$`)
	smellPyTestRe  = regexp.MustCompile(`(^|/)(test_[^/]*\.py|[^/]*_test\.py)$`)
	smellRbTestRe  = regexp.MustCompile(`(^|/)[^/]*_(spec|test)\.rb$`)
	smellJVMTestRe = regexp.MustCompile(`(Test|Tests|Spec)\.(java|kt|kts|scala)$`)
	smellCSTestRe  = regexp.MustCompile(`(Test|Tests)\.cs$`)
	smellTestSegs  = map[string]bool{"test": true, "tests": true, "__tests__": true, "testdata": true}

	// An added line that suppresses a linter / type checker.
	smellSuppressionRe = regexp.MustCompile(`(@ts-ignore|@ts-expect-error|eslint-disable|#\s*type:\s*ignore|#\s*noqa|#\s*pylint:\s*disable|#\s*pragma:\s*no\s*cover|#\s*nosec|//\s*nolint|@SuppressWarnings|#\s*rubocop:disable|@phpstan-ignore)`)

	// An added empty / swallowing exception handler.
	smellEmptyCatchRe = regexp.MustCompile(`(except\b[^:]*:\s*pass\b|catch\s*(\([^)]*\))?\s*\{\s*\}|rescue\b[^;]*;\s*end\b)`)

	// An added stub / not-implemented / deferred body. panic only counts when its
	// argument is a not-implemented literal — idiomatic error panics are routine.
	// A bare TODO or FIXME only counts when it IS the whole added statement (a
	// standalone comment), not a trailing remark on real code.
	smellStubBodyRe = regexp.MustCompile(`(?i)(panic\s*\(\s*["'](?:not |unimplemented|todo)|raise\s+NotImplementedError|throw\s+new\s+Error\s*\(\s*["']not[ _]?implemented|^\s*(?://|#|--|/\*)\s*(?:TODO|FIXME)\b)`)

	// An added line that DISABLES a test. Skipping the test that proves a fix is
	// the most direct way to make a failing test pass, so this is HARD — unlike
	// the SOFT per-line fingerprints there is no legitimate reading of it inside a
	// fix. Every token is anchored on a word boundary so an identifier that merely
	// contains one (`prefixit(`, `unit.skipped()`) does not match.
	smellTestSkipRe = regexp.MustCompile(`(\bt\.Skipf?\(|@pytest\.mark\.skip\b|\b(?:it|test|describe|context)\.skip\(|\bxit\(|\bxdescribe\(|@Ignore\b|@Disabled\b|#\[\s*ignore\s*\])`)

	// A line that asserts something (used to detect weakened test assertions).
	smellAssertionRe = regexp.MustCompile(`(?i)(\bassert\b|expect\s*\(|\.should\b|\.to(Be|Equal|Throw|Contain|Match|HaveBeen)|t\.(Error|Fatal|Errorf|Fatalf)|require\.\w|XCTAssert|EXPECT_|ASSERT_)`)

	smellHunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)`)

	// The declared line counts of a hunk: `@@ -<start>[,<oldCount>] +<start>[,<newCount>] @@`.
	// An omitted count means 1. analyzeDiff uses them to know where the hunk BODY
	// ends, so trailing prose is not parsed as diff content.
	smellHunkCountsRe = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)
)

// isSmellTestPath reports whether a repo-relative path is a test file. It is
// precise — "latest_test_results.go" or "contest.go" are NOT tests. Directory
// segments are matched case-insensitively so "Test", "Tests" and ".NET"-style
// "MyProj.Tests" layouts count; the ".tests" suffix is anchored on a dot so
// "latest" still does not.
func isSmellTestPath(p string) bool {
	p = strings.TrimPrefix(p, "./")
	for _, seg := range strings.Split(p, "/") {
		lower := strings.ToLower(seg)
		if smellTestSegs[lower] || strings.HasSuffix(lower, ".tests") {
			return true
		}
	}
	return smellGoTestRe.MatchString(p) || smellJSTestRe.MatchString(p) || smellPyTestRe.MatchString(p) ||
		smellRbTestRe.MatchString(p) || smellJVMTestRe.MatchString(p) || smellCSTestRe.MatchString(p)
}

// looksLikeUnifiedDiff reports whether text is plausibly a unified diff, so the
// gate can pass free-form fix content (prose change-instructions, bare code, a
// fenced snippet) through as clean instead of feeding it to a parser that would
// mis-attribute it. Requiring a real header — `diff --git`, a `--- `/`+++ ` pair,
// or a hunk header — rather than merely a leading `+`/`-` keeps prose like
// "+ add a nil check" from being read as a diff.
func looksLikeUnifiedDiff(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	sawOldHeader := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			return true
		case smellHunkRe.MatchString(line):
			return true
		case strings.HasPrefix(line, "--- "):
			sawOldHeader = true
		case strings.HasPrefix(line, "+++ "):
			if sawOldHeader {
				return true
			}
		}
	}
	return false
}

// startsWithDiffHeader reports whether the FIRST non-blank line of text is a
// unified-diff header. It is the strict counterpart to looksLikeUnifiedDiff:
// where that one scans the whole input (correct for the gate, which loses nothing
// on a false positive), this one demands the diff lead the content, so a Go file
// that merely embeds a diff fixture in a raw string cannot claim the exemption.
func startsWithDiffHeader(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return strings.HasPrefix(line, "diff --git ") ||
			strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") ||
			smellHunkRe.MatchString(line)
	}
	return false
}

type smellAddedLine struct {
	text string
}

type smellFileChange struct {
	path    string
	isTest  bool
	deleted bool // the new side is /dev/null: the file is removed outright
	added   []smellAddedLine
	removed []string
}

// analyzeDiff scans a unified diff for over-simplification fingerprints. It
// never returns nil.
func analyzeDiff(diff string) *smellResult {
	res := &smellResult{
		Files:   smellFiles{Test: []string{}, Impl: []string{}},
		Smells:  []smell{},
		Summary: smellSummary{ByType: map[string]int{}},
	}

	files := []*smellFileChange{}
	byPath := map[string]*smellFileChange{}
	var cur *smellFileChange
	// lastOldPath is the most recent `--- a/<path>` header. It is what a deletion
	// hunk must be attributed to: its `+++` side is /dev/null, which carries no
	// path of its own.
	lastOldPath := ""
	// Old paths of test files renamed to a non-test path in this diff. Collected
	// from the `diff --git` header, the only line carrying the pre-rename name.
	renamedAwayTests := []string{}

	ensure := func(p string) *smellFileChange {
		if p == "" || p == "/dev/null" {
			return nil
		}
		if fc, ok := byPath[p]; ok {
			return fc
		}
		fc := &smellFileChange{path: p, isTest: isSmellTestPath(p)}
		byPath[p] = fc
		files = append(files, fc)
		return fc
	}

	// Hunk-body state. Header prefixes (`+++ `, `--- `, `diff --git `) are
	// AMBIGUOUS with content: inside a hunk an added line reading `+++ foo` and a
	// removed line reading `-- foo` carry those very prefixes. Outside a hunk the
	// converse holds — a bare `-` or `+` line is not diff content at all, it is
	// trailing prose (a markdown notes list, which fix-generating models append
	// constantly). Tracking the hunk's declared line counts resolves both.
	inHunk := false
	oldRemaining, newRemaining := 0, 0

	for _, line := range strings.Split(diff, "\n") {
		if inHunk {
			// A line that cannot be a hunk body line ends the hunk early — the
			// declared counts are only as trustworthy as the diff's author, and a
			// hand-written or model-emitted hunk header routinely miscounts. Fall
			// through to the header switch so the line is still dispatched.
			if smellIsHunkBodyLine(line) {
				switch {
				case strings.HasPrefix(line, `\`):
					// "\ No newline at end of file" — metadata, consumes no declared line
				case strings.HasPrefix(line, "+"):
					if cur != nil {
						cur.added = append(cur.added, smellAddedLine{text: line[1:]})
					}
					newRemaining--
				case strings.HasPrefix(line, "-"):
					if cur != nil {
						cur.removed = append(cur.removed, line[1:])
					}
					oldRemaining--
				default:
					// context line (leading space, or an empty line) — counts on both sides
					oldRemaining--
					newRemaining--
				}
				if oldRemaining <= 0 && newRemaining <= 0 {
					inHunk = false
				}
				continue
			}
			inHunk = false
		}

		switch {
		case strings.HasPrefix(line, "diff --git "):
			// "diff --git a/old b/new" — tentative file (overridden by +++).
			if p := smellBPathFromGitHeader(line); p != "" {
				cur = ensure(p)
				// The a/ side is the ONLY place a rename's old path appears. A test
				// renamed to a non-test path is disabled as surely as a deleted one,
				// and is worse for this analyzer than a deletion: the new path is
				// classified IMPL, which additionally suppresses test_only for
				// everything else in the diff.
				if a := smellAPathFromGitHeader(line); a != "" && a != p &&
					isSmellTestPath(a) && !isSmellTestPath(p) {
					renamedAwayTests = append(renamedAwayTests, a)
				}
			}
		case strings.HasPrefix(line, "+++ "):
			// DIVERGENCE from upstream: a `+++ /dev/null` header (a file DELETION)
			// made upstream's ensure() return nil, unbinding cur — which silently
			// discarded every `-` line of the deleted file. A deleted test file
			// therefore contributed no removed assertions, so a diff that deletes a
			// whole test AND edits an implementation file scored clean: test_only
			// could not fire (implCount > 0) and weakened_assertion had nothing to
			// count. That is the single most blatant reward hack this gate exists to
			// block. Keep cur bound to the file the deletion is about — from the
			// preceding `diff --git` header, or failing that the `--- a/<path>` one —
			// and record the deletion.
			p := smellHeaderPath(line[4:])
			switch {
			case p == "":
				// A `+++ ` header carrying only whitespace or a tab is MALFORMED, not a
				// deletion — only an explicit /dev/null is. Marking the currently bound
				// file deleted here produced the worst kind of false positive: a HARD
				// test_deleted on a diff that deletes nothing, so a good fix was
				// rejected, retried once, then withheld with a FixWarning accusing the
				// model of deleting a test file it never touched. Ignore the line.
			case p == "/dev/null":
				// Bind to the `--- a/<path>` line this deletion is paired with whenever
				// it names a DIFFERENT file than the one currently bound — not merely
				// when nothing is bound. looksLikeUnifiedDiff accepts a HEADERLESS diff
				// (old/new header pairs, no `diff --git` lines), and in that shape cur
				// still points at the PREVIOUS file when the deletion arrives: the
				// `cur == nil` guard stamped `deleted` on that innocent file and the
				// deleted test never entered `files` at all, scoring clean where the
				// identical diff with `diff --git` headers scored hard.
				if lastOldPath != "" && (cur == nil || cur.path != lastOldPath) {
					if fc := ensure(lastOldPath); fc != nil {
						cur = fc
					}
				}
				if cur != nil {
					cur.deleted = true
				}
			default:
				cur = ensure(p)
			}
		case strings.HasPrefix(line, "--- "):
			lastOldPath = smellHeaderPath(line[4:])
		case strings.HasPrefix(line, "@@"):
			// hunk header — start line numbers are deliberately not tracked (no
			// production consumer ever read them), but the declared LINE COUNTS are:
			// they bound the body, so anything after it is trailer text, not content.
			if m := smellHunkCountsRe.FindStringSubmatch(line); m != nil {
				oldRemaining = smellHunkCount(m[1])
				newRemaining = smellHunkCount(m[2])
				inHunk = oldRemaining > 0 || newRemaining > 0
			}
		default:
			// Outside a hunk body: a file-mode / index / rename / similarity line, a
			// markdown fence, or trailing prose. A bare `+`/`-` line here is NOT diff
			// content — every content line lives inside a hunk.
		}
	}

	implCount, testCount := 0, 0
	for _, fc := range files {
		if fc.isTest {
			testCount++
			res.Files.Test = append(res.Files.Test, fc.path)
		} else {
			implCount++
			res.Files.Impl = append(res.Files.Impl, fc.path)
		}
	}
	res.Summary.TestFiles = testCount
	res.Summary.ImplFiles = implCount

	add := func(s smell) {
		res.Smells = append(res.Smells, s)
		res.Summary.ByType[s.Type]++
		if s.Severity == smellSeverityHard {
			res.Summary.Hard++
		} else {
			res.Summary.Soft++
		}
	}

	// HARD: a test file was renamed out of the test namespace.
	for _, p := range renamedAwayTests {
		add(smell{Type: smellTestRenamedAway, Severity: smellSeverityHard, File: p,
			Evidence: "fix renamed a test file to a non-test path, disabling it"})
	}

	// HARD: the fix touched only tests.
	if testCount > 0 && implCount == 0 {
		add(smell{Type: smellTestOnly, Severity: smellSeverityHard, File: res.Files.Test[0],
			Evidence: "fix changed only test file(s); no implementation change"})
	}

	for _, fc := range files {
		if fc.isTest {
			// HARD: deleting a test file outright. DIVERGENCE from upstream, which has
			// no deletion detector — it relies on test_only, which cannot fire when the
			// same diff also edits an implementation file. Deleting the test IS the
			// reward hack, whether or not anything else changed, and unlike
			// weakened_assertion it does not depend on the deleted lines happening to
			// match the assertion regex.
			if fc.deleted {
				add(smell{Type: smellTestDeleted, Severity: smellSeverityHard, File: fc.path,
					Evidence: "fix deleted a test file outright"})
			}
			// HARD: disabling a test. Like test_deleted this is an atcr addition —
			// upstream has no skip detector, so `t.Skip("unrelated flake")` added
			// alongside a real implementation change scored clean: test_only cannot
			// fire (implCount > 0) and nothing else looks at skip tokens. Only ADDED
			// lines count; REMOVING a skip re-enables a test and is the opposite of a
			// reward hack.
			for _, a := range fc.added {
				if smellRelocated(fc.removed, a.text) {
					continue
				}
				if smellTestSkipRe.MatchString(a.text) {
					add(smell{Type: smellTestSkipped, Severity: smellSeverityHard, File: fc.path,
						Evidence: strings.TrimSpace(a.text)})
				}
			}
			removedAsserts, addedAsserts := 0, 0
			for _, r := range fc.removed {
				if smellAssertionRe.MatchString(r) {
					removedAsserts++
				}
			}
			for _, a := range fc.added {
				if smellAssertionRe.MatchString(a.text) {
					addedAsserts++
				}
			}
			switch {
			case removedAsserts > addedAsserts:
				// HARD: a net loss of assertions is unambiguous.
				add(smell{Type: smellWeakenedAssertion, Severity: smellSeverityHard, File: fc.path,
					Evidence: "test removed assertion(s) without replacing them"})
			case removedAsserts > 0 && removedAsserts == addedAsserts:
				// SOFT: assertions replaced one-for-one. DIVERGENCE from upstream, which
				// only compares counts and so misses the classic swap of
				// `require.Equal(t, want, got)` for `require.NotNil(t, got)` — equal
				// counts, strictly weaker check. Line counting cannot tell that apart
				// from a legitimate assertion rewrite, so it annotates rather than
				// blocks: SOFT is exactly the "a human should glance at this" tier. This
				// is also the backstop that survives evaluateFixSmell's test_only
				// suppression for test-path findings.
				add(smell{Type: smellWeakenedAssertion, Severity: smellSeveritySoft, File: fc.path,
					Evidence: "test replaced assertion(s) one-for-one; verify they were not weakened"})
			}
		}

		// SOFT: per-added-line fingerprints. A line whose text also appears
		// verbatim (modulo surrounding whitespace) among the file's REMOVED lines
		// was relocated or reindented, not introduced — stamping it as a new
		// shortcut false-positives on every move (TD: diffsmell.go:120).
		for i, a := range fc.added {
			if smellRelocated(fc.removed, a.text) {
				continue
			}
			if smellSuppressionRe.MatchString(a.text) {
				add(smell{Type: smellSuppression, Severity: smellSeveritySoft, File: fc.path, Evidence: strings.TrimSpace(a.text)})
			}
			// The empty-catch pattern must also see consecutive added lines
			// joined: formatters emit `catch (e) {` and `}` on separate lines,
			// and Go's \s matches the newline, so the pair reveals what a
			// strictly per-line scan cannot.
			pair := a.text
			if i+1 < len(fc.added) {
				pair = a.text + "\n" + fc.added[i+1].text
			}
			if smellEmptyCatchRe.MatchString(pair) {
				add(smell{Type: smellEmptyCatch, Severity: smellSeveritySoft, File: fc.path, Evidence: strings.TrimSpace(a.text)})
			}
			if smellStubBodyRe.MatchString(a.text) {
				add(smell{Type: smellStubBody, Severity: smellSeveritySoft, File: fc.path, Evidence: strings.TrimSpace(a.text)})
			}
		}
	}

	switch {
	case res.Summary.Hard > 0:
		res.Summary.Verdict = smellVerdictHard
	case res.Summary.Soft > 0:
		res.Summary.Verdict = smellVerdictSoftOnly
	default:
		res.Summary.Verdict = smellVerdictClean
	}
	return res
}

// smellRelocated reports whether an added line also appears verbatim — modulo
// leading/trailing whitespace — among the same file's removed lines. A line that
// was merely moved or reindented is not a NEW shortcut, so the per-line SOFT
// fingerprints (suppression, empty_catch, stub_body) must not fire on it.
func smellRelocated(removed []string, added string) bool {
	t := strings.TrimSpace(added)
	if t == "" {
		return false
	}
	for _, r := range removed {
		if strings.TrimSpace(r) == t {
			return true
		}
	}
	return false
}

// smellHeaderPath strips the "a/" or "b/" prefix and any trailing tab metadata
// from a `--- ` / `+++ ` header path.
func smellHeaderPath(s string) string {
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "a/")
	s = strings.TrimPrefix(s, "b/")
	return s
}

// smellBPathFromGitHeader extracts the new path from "diff --git a/x b/y". The
// b/ side is located by its separator rather than by whitespace splitting, so a
// path containing a space is not truncated; surrounding quotes (git's quoting of
// special-character paths) are stripped.
func smellBPathFromGitHeader(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	var b string
	if i := strings.LastIndex(rest, ` "b/`); i >= 0 {
		b = rest[i+2:]
	} else if i := strings.LastIndex(rest, " b/"); i >= 0 {
		b = rest[i+1:]
	} else {
		return ""
	}
	b = strings.Trim(b, `"`)
	return smellHeaderPath(b)
}

// smellIsHunkBodyLine reports whether line has the shape of a unified-diff hunk
// body line: a one-character prefix of ' ', '+', '-' or '\', or an empty line
// (some emitters write a bare "" for an empty context line). Anything else ends
// the hunk, which keeps a miscounted `@@` header from swallowing the next file's
// header or a trailing prose block.
func smellIsHunkBodyLine(line string) bool {
	if line == "" || line == "\r" {
		return true
	}
	switch line[0] {
	case ' ', '+', '-', '\\':
		return true
	}
	return false
}

// smellHunkCount parses a hunk header's optional line-count capture. An omitted
// count means exactly one line, per the unified-diff format. A count too large
// for an int fails OPEN — the hunk stays unbounded and smellIsHunkBodyLine ends
// it — because failing closed would let a crafted `@@ -1,<20 digits> +1 @@`
// header wrap to a negative count, skip the body entirely, and hide every smell
// in it.
func smellHunkCount(s string) int {
	if s == "" {
		return 1
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return math.MaxInt32
	}
	return n
}

// smellAPathFromGitHeader extracts the OLD path from "diff --git a/x b/y" — the
// only line in a rename that carries it. It mirrors smellBPathFromGitHeader's
// separator-based split so a path containing a space is not truncated.
func smellAPathFromGitHeader(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	var a string
	if i := strings.LastIndex(rest, ` "b/`); i >= 0 {
		a = rest[:i]
	} else if i := strings.LastIndex(rest, " b/"); i >= 0 {
		a = rest[:i]
	} else {
		return ""
	}
	a = strings.Trim(strings.TrimSpace(a), `"`)
	return smellHeaderPath(a)
}

// smellTypes returns the distinct smell types in res, sorted, for a stable
// annotation string. Returns nil for a nil or clean result.
func smellTypes(res *smellResult) []string {
	if res == nil || len(res.Smells) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(res.Smells))
	out := make([]string, 0, len(res.Smells))
	for _, s := range res.Smells {
		if !seen[s.Type] {
			seen[s.Type] = true
			out = append(out, s.Type)
		}
	}
	sort.Strings(out)
	return out
}

// smellFeedback renders the evidence the gate appends to the retry prompt. It is
// flattened to a single line: the retry prompt is a plain interpolated string
// with no role separation, so an embedded newline could forge a prompt line
// (the same CR/LF hazard buildFixPrompt's sanitized persona/rules guard against).
// Returns "" for a nil or clean result.
func smellFeedback(res *smellResult) string {
	if res == nil || len(res.Smells) == 0 {
		return ""
	}
	var b strings.Builder
	for i, s := range res.Smells {
		if i >= maxSmellFeedbackItems {
			fmt.Fprintf(&b, "; (+%d more)", len(res.Smells)-i)
			break
		}
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(s.Type)
		b.WriteString(" (")
		b.WriteString(s.Severity)
		b.WriteString(")")
		if p := smellFlatten(s.File); p != "" {
			b.WriteString(" at ")
			b.WriteString(truncateRunes(p, maxSmellPathRunes))
		}
		if ev := smellFlatten(s.Evidence); ev != "" {
			b.WriteString(": ")
			b.WriteString(truncateRunes(ev, maxSmellEvidenceRunes))
		}
	}
	return truncateRunes(smellFlatten(b.String()), maxSmellFeedbackRunes)
}

// truncateRunes shortens s to at most n runes, appending an ellipsis when it was
// longer. Rune-based so a multibyte character is never split.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "..."
}

// smellFlatten collapses CR/LF to spaces so evidence text is safe to interpolate
// into a single prompt line, mirroring sanitizeDeclineReason's flattening.
func smellFlatten(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
