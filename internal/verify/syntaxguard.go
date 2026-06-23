package verify

import (
	"errors"
	"go/parser"
	"go/scanner"
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
// shape "Here is the fix:\n```go\n...\n```" is handled. The opening run is `{3,} so a
// CommonMark 4-backtick fence (used when the body itself contains a triple backtick)
// is captured as a unit rather than sliced at the inner three backticks; the language
// tag admits '#' (c#, f#) and may be followed by trailing whitespace. The closing run
// is anchored to its own line end ([ \t]*$ under (?m)), so an inline ``` mid-line —
// e.g. inside a Go string literal "```" — cannot prematurely close the block and
// truncate valid code. The pre-close newline stays optional (\n?), so a closing ```
// on the same line as the last code line still matches. Input is CRLF-normalized
// before matching (see normalizeNewlines), so the LF-only pattern also covers CRLF.
var fenceRe = regexp.MustCompile("(?sm)`{3,}[ \t]*([A-Za-z0-9_+#-]*)[^\n]*\n(.*?)\n?[ \t]*`{3,}[ \t]*$")

// declKeywordRe matches a line that begins (after optional whitespace) with a Go
// declaration keyword. Used in parseGoFix to select the most relevant parse error
// among the three strategy results, and in looksLikeNonGoBraces to detect Go code
// presence. Not used as a looksLikeGoCode signal (use packageClauseRe or
// funcSignatureRe for that — bare keywords are ambiguous with prose).
var declKeywordRe = regexp.MustCompile(`(?m)^\s*(package|import|func|type|var|const)\b`)

// blockOpenRe matches a line whose last non-whitespace character is a single opening
// brace introducing a block (e.g. "func add() {", "if x != nil {"). The line is
// anchored from start through a run of non-brace content to one trailing brace, so a
// line carrying an inner brace ("foo{bar{") or an inline "&Options{Retries: 3}" with
// the brace mid-line is NOT matched — block structure prose almost never produces, so
// it stays a reliable code signal without over-matching any line that merely ends in
// a brace.
var blockOpenRe = regexp.MustCompile(`(?m)^[ \t]*[^{]*\{[ \t]*$`)

// blockCloseRe matches a line that begins with a closing brace (e.g. "}",
// "} else {"). Same rationale as blockOpenRe.
var blockCloseRe = regexp.MustCompile(`(?m)^[ \t]*\}`)

// jsonKeyLineRe matches a line that begins (after optional whitespace) with a
// double-quoted key immediately followed by a colon — the canonical JSON object
// member shape ("name": ...). Go source almost never starts a line this way: a
// quoted string appears at line start only in a map/struct literal with string keys,
// which, being valid Go, parses cleanly and never reaches this guard (or, being
// broken Go, is an acceptable false negative under the conservative-recall policy).
// A `case "x":` label does NOT match — the line starts with `case`, not the quote.
var jsonKeyLineRe = regexp.MustCompile(`(?m)^\s*"(?:[^"\\]|\\.)*"\s*:`)

// packageClauseRe detects a package clause, i.e. the fix is shaped as a full file.
var packageClauseRe = regexp.MustCompile(`(?m)^\s*package\s+\w`)

// funcSignatureRe matches a Go function declaration: the func keyword followed
// by an identifier name and an opening parenthesis for the parameter list. The
// paren disambiguates from prose change-instructions such as "func should return
// an error", which never contain a trailing `(` on the same line. Methods with
// receivers (func (r *T) Method(...)) are intentionally excluded — they are caught
// by block structure when present, consistent with the conservative-recall policy.
var funcSignatureRe = regexp.MustCompile(`(?m)^\s*func\s+\w[^()\n]*\(`)

// nonGoFenceLangs are fenced-block language tags that explicitly denote a language
// other than Go. A fix fenced as one of these is not the Go guard's concern.
var nonGoFenceLangs = map[string]bool{
	"python": true, "py": true, "js": true, "javascript": true, "ts": true,
	"typescript": true, "jsx": true, "tsx": true, "sh": true, "bash": true,
	"shell": true, "zsh": true, "rust": true, "rs": true, "java": true,
	"kotlin": true, "kt": true, "c": true, "cpp": true, "c++": true, "cc": true,
	"cs": true, "c#": true, "csharp": true, "fs": true, "f#": true, "fsharp": true,
	"ruby": true, "rb": true, "php": true,
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
	fix = normalizeNewlines(fix)
	code, lang, hadFence := extractFencedCode(fix)
	if hadFence && nonGoFenceLangs[strings.ToLower(strings.TrimSpace(lang))] {
		return nil // explicitly another language — not this guard's concern
	}
	// Cap the size of the code actually parsed, not the raw fix. A small fenced
	// snippet wrapped in a huge prose blob is still a genuine fix and should be
	// validated; only the extracted code path is expensive.
	if len(code) > maxFixBytes {
		return nil // pathological size: not a genuine fix — skip the triple AST parse
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
//
// Conservative-recall by design (WONTFIX): the three strategies are OR-ed
// deliberately so ambiguous content gets every reasonable chance to parse before
// being flagged. This can let a fix mixing a declaration and a statement parse under
// an unintended strategy and mask a genuinely broken fix (a false negative). That is
// the intended trade-off — the guard prefers a false negative over a false positive,
// since a false "invalid" flag on a legitimate fix degrades trust. Tightening this to
// a single shape-matched strategy is a recorded WONTFIX, not an oversight.
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
		return fileErr // file strategy: positions already relative to src
	case declKeywordRe.MatchString(src):
		return stripPosition(declErr) // decl strategy: "package p\n" + src shifts lines by 1
	default:
		return stripPosition(stmtErr) // stmt strategy: 2-line header shifts lines by 2
	}
}

// stripPosition removes the position prefix (e.g. "3:13: ") from a scanner
// error returned by go/parser. Wrapped parse strategies produce positions
// relative to their synthetic header, not to the caller's src — stripping
// the prefix avoids misleading the caller with a line number that points to
// the wrong line in their input.
func stripPosition(err error) error {
	if err == nil {
		return nil
	}
	if list, ok := err.(scanner.ErrorList); ok && len(list) > 0 {
		return errors.New(list[0].Msg)
	}
	return err
}

// looksLikeGoCode reports whether unfenced text carries a strong LINE-STRUCTURAL
// signal of Go source: a line beginning with a declaration keyword, or paired
// open/close braces indicating genuine block structure. These are forms prose almost
// never produces. Crucially it does NOT treat an inline brace or `:=` embedded
// mid-sentence as code — "Pass &Options{Retries: 3} to the constructor" or "replace
// it with count := len(items)" are legitimate prose change-instructions and must pass
// through unflagged (false positives degrade trust). The cost is conservative recall:
// a one-line broken expression with no block structure is not flagged, since it is
// indistinguishable from prose.
// `package <ident>` and `func <ident>(` are the two keyword patterns that are
// prose-never: prose change-instructions almost never carry a package clause, and
// "func should …" prose lacks the `<ident>(` shape. Other declaration keywords
// (import, var, const, type) alone are ambiguous — "import the sync package" is
// valid prose. Require block structure as co-signal for those.
// A trailing opening brace alone is NOT a sufficient signal: prose change-instructions
// can end in a lone { (e.g. "Wrap the body in the literal that opens with {"), which
// satisfies blockOpenRe yet fails go/parser — a false positive. blockOpenRe must
// co-occur with blockCloseRe (a matching close-brace line) to be treated as code.
// Otherwise, obviously non-Go brace content (JSON/config, detected by
// looksLikeNonGoBraces) suppresses the block-brace signal so an unfenced JSON/config
// fix is not flagged (Epic 7.5). The suppression only ever turns a true into a false
// — it can reduce flagging but never add it — so it cannot regress the 7.1
// conservative-recall guarantee (AC2/AC4). It is consulted only after parseGoFix has
// already failed, so valid Go (including string-keyed map literals) never reaches it.
func looksLikeGoCode(src string) bool {
	// Two unambiguous code signals that prose almost never produces:
	//   package <ident>  — prose change-instructions never open with a package clause.
	//   func <ident>(    — prose starting with "func" (e.g. "func should return an
	//                     error") never has a parenthesised parameter list immediately
	//                     after the identifier; the paren is the disambiguation signal.
	// Other declaration keywords (import, var, const, type) without block structure
	// are ambiguous — "import the sync package" is valid prose. Require block
	// structure as co-signal for those cases.
	if packageClauseRe.MatchString(src) || funcSignatureRe.MatchString(src) {
		return true
	}
	if looksLikeNonGoBraces(src) {
		return false
	}
	// A trailing opening brace alone is not sufficient (prose can end in {): require a
	// matching close-brace line to co-occur, which genuine block structure always has.
	if blockOpenRe.MatchString(src) {
		return blockCloseRe.MatchString(src)
	}
	return blockCloseRe.MatchString(src)
}

// looksLikeNonGoBraces reports whether a parse-failed, unfenced fragment should be
// suppressed as obviously non-Go content: it matches any fragment that carries a
// JSON object-member line (a double-quoted key followed by a colon at line start) and
// no Go declaration keyword on any line — regardless of whether braces are present.
// The function name reflects its primary use case (JSON/config objects), but the
// check is intentionally broader per the AC4 false-negative-preferred policy: a
// quoted-key line with no decl keyword is sufficient to suppress, even without
// surrounding braces. The keyword check is a coarse whole-input line scan, not a
// structural key-aware parse, so a JSON value line beginning with a Go keyword
// defeats suppression and re-arms the block-brace signal. This is benign — it only
// re-arms pre-existing 7.1 behavior and never adds new flagging beyond 7.1 (AC2
// intact). It exists purely to SUPPRESS a false-positive invalid_syntax flag on
// unfenced JSON/config (Epic 7.5); it only ever reduces flagging. The detection is
// deliberately narrow (quoted keys only — bare `ident:` is not used, since Go struct
// literals, labels, cases, and map entries all produce it), keeping it conservative.
func looksLikeNonGoBraces(src string) bool {
	return jsonKeyLineRe.MatchString(src) && !declKeywordRe.MatchString(src)
}

// extractFencedCode returns the inner content of the first markdown code fence in
// fix along with its language tag and true; when fix is not fenced it returns fix
// unchanged, an empty language, and false. fix is expected to be newline-normalized.
//
// Only the FIRST fence is examined (accepted limitation, conservative-recall by
// design): a multi-block response shaped as a non-Go fence (e.g. ```text) followed by
// a broken Go fence validates the first block, matches nonGoFenceLangs, and returns
// nil — the broken Go in the second fence is never parsed (a silent false negative).
// Extending to scan every fence is deliberately out of scope; it would shift the
// guard's false-positive/false-negative balance, and the guard exists to avoid false
// positives that degrade trust. The gap is documented rather than fixed.
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
