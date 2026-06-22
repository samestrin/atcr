package verify

import (
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// Local syntax guard for generated fixes (Epic 7.1).
//
// The executor's Fix field is free-form: the prompt asks for "corrected code or a
// precise change instruction" (executor.go), so a fix may be a complete Go file, a
// bare declaration, a statement fragment, a fenced code block, or plain English
// prose. validateGoFixSyntax parses the fix with go/parser and reports a syntax
// error ONLY when the content is plausibly Go code yet fails to parse. Prose and
// explicitly non-Go fenced blocks pass through (nil), because a false "invalid"
// flag on a legitimate fix degrades trust — the exact failure mode this guard
// exists to prevent. The trade-off is conservative recall: an unfenced fragment
// with no block structure (no braces, no `:=`, no leading declaration keyword) is
// not flagged even if malformed, since it is indistinguishable from prose.

// fenceRe matches a markdown code fence anywhere in the input (not anchored to the
// start), capturing the optional language tag (group 1) and the fenced body (group
// 2). Leading/trailing prose around the fence is ignored, so the very common LLM
// shape "Here is the fix:\n```go\n...\n```" is handled. The newline before the
// closing fence is optional, so a closing ``` on the same line as the last code
// line still matches. Input is CRLF-normalized before matching (see
// normalizeNewlines), so the LF-only pattern also covers CRLF fences.
var fenceRe = regexp.MustCompile("(?s)```([A-Za-z0-9_+-]*)\n(.*?)\n?```")

// declKeywordRe matches a line that begins (after optional whitespace) with a Go
// top-level / statement keyword that is a strong signal the text is code rather
// than prose. Anchored per-line via the multiline flag.
var declKeywordRe = regexp.MustCompile(`(?m)^\s*(package|import|func|type|var|const)\b`)

// blockOpenRe matches a line whose last non-whitespace character is an opening
// brace (e.g. "func add() {", "if x != nil {"). This is block structure prose
// almost never produces — unlike an inline "&Options{Retries: 3}" where the brace
// sits mid-line — so it is a reliable code signal.
var blockOpenRe = regexp.MustCompile(`(?m)\{[ \t]*$`)

// blockCloseRe matches a line that begins with a closing brace (e.g. "}",
// "} else {"). Same rationale as blockOpenRe.
var blockCloseRe = regexp.MustCompile(`(?m)^[ \t]*\}`)

// packageClauseRe detects a package clause, i.e. the fix is shaped as a full file.
var packageClauseRe = regexp.MustCompile(`(?m)^\s*package\s+\w`)

// nonGoFenceLangs are fenced-block language tags that explicitly denote a language
// other than Go. A fix fenced as one of these is not the Go guard's concern.
var nonGoFenceLangs = map[string]bool{
	"python": true, "py": true, "js": true, "javascript": true, "ts": true,
	"typescript": true, "jsx": true, "tsx": true, "sh": true, "bash": true,
	"shell": true, "zsh": true, "rust": true, "rs": true, "java": true,
	"kotlin": true, "kt": true, "c": true, "cpp": true, "c++": true, "cc": true,
	"cs": true, "csharp": true, "ruby": true, "rb": true, "php": true,
	"swift": true, "sql": true, "html": true, "css": true, "scss": true,
	"yaml": true, "yml": true, "json": true, "toml": true, "xml": true,
	"text": true, "txt": true, "markdown": true, "md": true, "diff": true,
	"patch": true, "dockerfile": true, "make": true, "makefile": true,
}

// maxFixBytes caps the input validateGoFixSyntax will parse. parseGoFix runs up to
// three full parser.ParseFile passes per fix, concurrently across the worker pool,
// driven by untrusted model output; a pathological multi-megabyte completion is not
// a realistic fix, so above this size the guard short-circuits to nil rather than
// pay the triple AST build. 256KiB is far above any genuine code-snippet fix.
const maxFixBytes = 256 * 1024

// validateGoFixSyntax returns a non-nil error when fix is plausibly Go code that
// fails to parse, and nil otherwise (valid Go, prose, or non-Go content).
func validateGoFixSyntax(fix string) error {
	if len(fix) > maxFixBytes {
		return nil // pathological size: not a genuine fix — skip the triple AST parse
	}
	fix = normalizeNewlines(fix)
	code, lang, hadFence := extractFencedCode(fix)
	if hadFence && nonGoFenceLangs[strings.ToLower(strings.TrimSpace(lang))] {
		return nil // explicitly another language — not this guard's concern
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	parseErr := parseGoFix(code)
	if parseErr == nil {
		return nil // parses cleanly as Go under at least one strategy
	}
	// Did not parse. Flag only when the content is plausibly code; otherwise it is
	// a prose change-instruction and must pass untouched.
	if hadFence || looksLikeGoCode(code) {
		return parseErr
	}
	return nil
}

// parseGoFix tries to parse src as Go under three strategies, from most to least
// complete: as a whole file, as top-level declarations (prefixed with a synthetic
// package clause), and as statements (wrapped in a synthetic function body). It
// returns nil if any strategy parses cleanly. When none do, it returns the error
// from the strategy that best matches the content's shape, so the message is
// meaningful (a file-shaped fix reports the file error, etc.).
func parseGoFix(src string) error {
	fset := token.NewFileSet()
	const mode = parser.SkipObjectResolution

	_, fileErr := parser.ParseFile(fset, "", src, mode)
	if fileErr == nil {
		return nil
	}
	_, declErr := parser.ParseFile(fset, "", "package p\n"+src, mode)
	if declErr == nil {
		return nil
	}
	_, stmtErr := parser.ParseFile(fset, "", "package p\nfunc _() {\n"+src+"\n}\n", mode)
	if stmtErr == nil {
		return nil
	}

	switch {
	case packageClauseRe.MatchString(src):
		return fileErr
	case declKeywordRe.MatchString(src):
		return declErr
	default:
		return stmtErr
	}
}

// looksLikeGoCode reports whether unfenced text carries a strong LINE-STRUCTURAL
// signal of Go source: a line beginning with a declaration keyword, a line ending
// in an opening brace, or a line beginning with a closing brace. These are forms
// prose almost never produces. Crucially it does NOT treat an inline brace or `:=`
// embedded mid-sentence as code — "Pass &Options{Retries: 3} to the constructor"
// or "replace it with count := len(items)" are legitimate prose change-instructions
// and must pass through unflagged (false positives degrade trust). The cost is
// conservative recall: a one-line broken expression with no block structure is not
// flagged, since it is indistinguishable from prose.
func looksLikeGoCode(s string) bool {
	return declKeywordRe.MatchString(s) || blockOpenRe.MatchString(s) || blockCloseRe.MatchString(s)
}

// extractFencedCode returns the inner content of the first markdown code fence in
// fix along with its language tag and true; when fix is not fenced it returns fix
// unchanged, an empty language, and false. fix is expected to be newline-normalized.
func extractFencedCode(fix string) (code, lang string, fenced bool) {
	m := fenceRe.FindStringSubmatch(fix)
	if m == nil {
		return fix, "", false
	}
	return m[2], m[1], true
}

// normalizeNewlines collapses CRLF and lone CR line endings to LF so the LF-only
// fence and line-structural regexes apply uniformly regardless of the provider's
// line-ending convention.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
