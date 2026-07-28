package verify

// diffsmell.go is a PORT of llm-tools' `diff-smell` analyzer
// (github.com/samestrin/llm-tools, internal/support/commands/diff_smell.go,
// v1.5.0) — copied and adapted rather than imported, because the upstream
// function lives under that module's own internal/ and is therefore not
// importable across modules. Like the original it depends only on stdlib
// regexp/strings, so the port adds no dependency to go.mod (Epic 35.3).
//
// It scans a unified diff for the mechanical fingerprints of an
// over-simplified ("reward-hacked") patch: a fix that makes a test pass by
// deleting the test or weakening its assertions, or that resolves a lint by
// suppressing it. generateFixes uses it as a pre-write gate on executor-produced
// fixes.
//
// Divergences from upstream, both deliberate and both recorded in the epic's
// Clarifications section:
//
//   - Types and helpers are unexported and prefixed `smell*` to fit this package.
//   - Callers must pre-filter with looksLikeUnifiedDiff. Upstream is always fed a
//     real diff (a file path or a git rev); here a Finding.Fix is free-form, so
//     non-diff content must be classified clean rather than parsed as one.
//
// The `test_only` suppression for test-file findings is NOT applied here — the
// analyzer stays a faithful port, and the gate applies that policy at the call
// site where the finding's own path is known.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Bounds on the rejection feedback fed back into the retry prompt. Every smell's
// Evidence is a verbatim added line from the MODEL's own diff, so without these a
// crafted fix (up to maxFixBytes) could balloon the retry prompt by orders of
// magnitude — paid for in tokens on every rejected fix. The type and severity
// names, which carry the actionable signal, are a closed vocabulary and are never
// truncated; only the quoted evidence and the item count are bounded.
const (
	maxSmellEvidenceRunes = 200
	maxSmellFeedbackItems = 10
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
	smellSuppression       = "suppression"
	smellEmptyCatch        = "empty_catch"
	smellStubBody          = "stub_body"
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
	Line     int // new-file line number for added-line smells
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
	smellTestSegs  = map[string]bool{"test": true, "tests": true, "spec": true, "__tests__": true}

	// An added line that suppresses a linter / type checker.
	smellSuppressionRe = regexp.MustCompile(`(@ts-ignore|@ts-expect-error|eslint-disable|#\s*type:\s*ignore|#\s*noqa|#\s*pylint:\s*disable|#\s*pragma:\s*no\s*cover|#\s*nosec|//\s*nolint|@SuppressWarnings|#\s*rubocop:disable|@phpstan-ignore)`)

	// An added empty / swallowing exception handler.
	smellEmptyCatchRe = regexp.MustCompile(`(except\b[^:]*:\s*pass\b|catch\s*(\([^)]*\))?\s*\{\s*\}|rescue\b[^;]*;\s*end\b)`)

	// An added stub / not-implemented / deferred body.
	smellStubBodyRe = regexp.MustCompile(`(?i)(panic\s*\(|raise\s+NotImplementedError|throw\s+new\s+Error\s*\(\s*["']not[ _]?implemented|\bTODO\b|\bFIXME\b)`)

	// A line that asserts something (used to detect weakened test assertions).
	smellAssertionRe = regexp.MustCompile(`(?i)(\bassert\b|expect\s*\(|\.should\b|\.to(Be|Equal|Throw|Contain|Match|HaveBeen)|t\.(Error|Fatal|Errorf|Fatalf)|require\.\w|XCTAssert|EXPECT_|ASSERT_)`)

	smellHunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)`)
)

// isSmellTestPath reports whether a repo-relative path is a test file. It is
// precise — "latest_test_results.go" or "contest.go" are NOT tests.
func isSmellTestPath(p string) bool {
	p = strings.TrimPrefix(p, "./")
	for _, seg := range strings.Split(p, "/") {
		if smellTestSegs[seg] {
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

type smellAddedLine struct {
	text   string
	lineNo int
}

type smellFileChange struct {
	path    string
	isTest  bool
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
	newLineNo := 0

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

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			// "diff --git a/old b/new" — tentative file (overridden by +++).
			if p := smellBPathFromGitHeader(line); p != "" {
				cur = ensure(p)
			}
		case strings.HasPrefix(line, "+++ "):
			cur = ensure(smellHeaderPath(line[4:]))
		case strings.HasPrefix(line, "--- "):
			// old-file header — ignore
		case strings.HasPrefix(line, "@@"):
			newLineNo = smellNewHunkStart(line)
		case strings.HasPrefix(line, "+"):
			if cur != nil {
				cur.added = append(cur.added, smellAddedLine{text: line[1:], lineNo: newLineNo})
			}
			newLineNo++
		case strings.HasPrefix(line, "-"):
			if cur != nil {
				cur.removed = append(cur.removed, line[1:])
			}
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file" — ignore
		default:
			// context line (leading space) or stray line
			newLineNo++
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

	// HARD: the fix touched only tests.
	if testCount > 0 && implCount == 0 {
		add(smell{Type: smellTestOnly, Severity: smellSeverityHard, File: res.Files.Test[0],
			Evidence: "fix changed only test file(s); no implementation change"})
	}

	for _, fc := range files {
		// HARD: a test file removed more assertions than it added.
		if fc.isTest {
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
			if removedAsserts > addedAsserts {
				add(smell{Type: smellWeakenedAssertion, Severity: smellSeverityHard, File: fc.path,
					Evidence: "test removed assertion(s) without replacing them"})
			}
		}

		// SOFT: per-added-line fingerprints.
		for _, a := range fc.added {
			if smellSuppressionRe.MatchString(a.text) {
				add(smell{Type: smellSuppression, Severity: smellSeveritySoft, File: fc.path, Line: a.lineNo, Evidence: strings.TrimSpace(a.text)})
			}
			if smellEmptyCatchRe.MatchString(a.text) {
				add(smell{Type: smellEmptyCatch, Severity: smellSeveritySoft, File: fc.path, Line: a.lineNo, Evidence: strings.TrimSpace(a.text)})
			}
			if smellStubBodyRe.MatchString(a.text) {
				add(smell{Type: smellStubBody, Severity: smellSeveritySoft, File: fc.path, Line: a.lineNo, Evidence: strings.TrimSpace(a.text)})
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

// smellBPathFromGitHeader extracts the new path from "diff --git a/x b/y".
func smellBPathFromGitHeader(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return ""
	}
	return smellHeaderPath(fields[len(fields)-1])
}

// smellMaxHunkDigits bounds the hunk-start parse. Upstream accumulates digits
// unbounded, which silently wraps on a pathological header; here the diff is
// MODEL-generated, so an absurd `@@ -1 +999...9 @@` is reachable input and its
// wrapped (possibly negative) result would ride into a smell's Line and out
// through the FixReview annotation. 9 digits holds any real file (999,999,999
// lines) and cannot overflow int32-width arithmetic.
const smellMaxHunkDigits = 9

// smellNewHunkStart returns the new-file starting line of a hunk header, or 0.
// A header whose line number exceeds smellMaxHunkDigits yields 0 (unknown line)
// rather than a wrapped value — the same fail-soft posture the rest of this
// analyzer takes on unparseable input.
func smellNewHunkStart(line string) int {
	m := smellHunkRe.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	if len(m[1]) > smellMaxHunkDigits {
		return 0
	}
	n := 0
	for _, c := range m[1] {
		n = n*10 + int(c-'0')
	}
	return n
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
		if s.File != "" {
			b.WriteString(" at ")
			b.WriteString(s.File)
		}
		if ev := smellFlatten(s.Evidence); ev != "" {
			b.WriteString(": ")
			b.WriteString(truncateRunes(ev, maxSmellEvidenceRunes))
		}
	}
	return smellFlatten(b.String())
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
