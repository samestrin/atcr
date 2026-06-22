package verify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateGoFixSyntax is the local syntax guard (Epic 7.1): it parses a generated
// fix with go/parser and returns a non-nil error ONLY when the fix is plausibly Go
// code yet fails to parse. Free-form prose change-instructions and explicitly
// non-Go fenced blocks pass through with a nil error so a legitimate fix is never
// falsely flagged (false positives degrade trust, which is exactly what this guard
// exists to prevent).

func TestValidateGoFixSyntax_ValidFullFile(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n"
	assert.NoError(t, validateGoFixSyntax(src), "a complete, valid Go file must pass")
}

func TestValidateGoFixSyntax_ValidTopLevelDecl(t *testing.T) {
	// A function declaration with no package clause (a common minimal-change fix).
	src := "func add(a, b int) int {\n\treturn a + b\n}"
	assert.NoError(t, validateGoFixSyntax(src), "a valid top-level decl (no package) must pass")
}

func TestValidateGoFixSyntax_ValidStatementSnippet(t *testing.T) {
	// A bare statement fragment that is only valid inside a function body.
	src := "if err != nil {\n\treturn err\n}"
	assert.NoError(t, validateGoFixSyntax(src), "a valid statement fragment must pass")
}

func TestValidateGoFixSyntax_ValidFencedGo(t *testing.T) {
	src := "```go\nfunc add(a, b int) int {\n\treturn a + b\n}\n```"
	assert.NoError(t, validateGoFixSyntax(src), "valid Go inside a ```go fence must pass")
}

func TestValidateGoFixSyntax_InvalidFencedGo(t *testing.T) {
	// Missing closing brace — clearly code (fenced), clearly broken.
	src := "```go\nfunc add(a, b int) int {\n\treturn a + b\n```"
	err := validateGoFixSyntax(src)
	require.Error(t, err, "syntactically broken Go inside a fence must be flagged")
}

func TestValidateGoFixSyntax_InvalidFullFile(t *testing.T) {
	// A full file (package clause present) whose body is syntactically broken.
	src := "package main\n\nfunc main() {\n\treturn a +\n}\n"
	require.Error(t, validateGoFixSyntax(src), "a broken full Go file must be flagged")
}

func TestValidateGoFixSyntax_InvalidUnfencedCode(t *testing.T) {
	// No fence, but braces make it plainly code; the body is broken Go.
	src := "func add(a, b int) int {\n\treturn a +\n}"
	err := validateGoFixSyntax(src)
	require.Error(t, err, "broken Go with block structure must be flagged even without a fence")
}

func TestValidateGoFixSyntax_InvalidShortAssign(t *testing.T) {
	// `:=` is a strong Go signal; the snippet is malformed.
	src := "x := func( {"
	require.Error(t, validateGoFixSyntax(src), "broken code with := must be flagged")
}

func TestValidateGoFixSyntax_ProseInstructionNotFlagged(t *testing.T) {
	src := "Add a nil check before calling Connect() so a closed pool does not panic."
	assert.NoError(t, validateGoFixSyntax(src), "a prose change-instruction must never be flagged")
}

func TestValidateGoFixSyntax_ProseWithKeywordsNotFlagged(t *testing.T) {
	// Contains the words "if" and "return" but no code structure (no braces, no :=).
	src := "If the input is nil, return an error to the caller instead of dereferencing it."
	assert.NoError(t, validateGoFixSyntax(src), "prose mentioning keywords but lacking code structure must not be flagged")
}

// Prose that embeds an inline Go-ish fragment (a struct literal, a := expression)
// inside a sentence must NOT be flagged: the brace/:= is mid-sentence, not block
// structure. These are the realistic "precise change instruction" outputs the
// guard must pass through (independent-review HIGH).
func TestValidateGoFixSyntax_ProseWithInlineBracesNotFlagged(t *testing.T) {
	src := "Pass &Options{Retries: 3} to the constructor instead of nil."
	assert.NoError(t, validateGoFixSyntax(src), "prose embedding an inline struct literal must not be flagged")
}

func TestValidateGoFixSyntax_ProseWithInlineShortAssignNotFlagged(t *testing.T) {
	src := "Replace it with count := len(items) and return early when it is zero."
	assert.NoError(t, validateGoFixSyntax(src), "prose embedding an inline := expression must not be flagged")
}

// A fenced code block preceded by prose ("Here is the fix:\n```go ... ```") is the
// most common LLM output shape; the fence must be detected and only the fenced Go
// validated (independent-review HIGH).
func TestValidateGoFixSyntax_LeadingProseBeforeFenceValid(t *testing.T) {
	src := "Here is the fix:\n\n```go\nfunc add(a, b int) int { return a + b }\n```"
	assert.NoError(t, validateGoFixSyntax(src), "valid Go in a fence with leading prose must pass")
}

func TestValidateGoFixSyntax_LeadingProseBeforeFenceInvalid(t *testing.T) {
	src := "Here is the fix:\n\n```go\nfunc broken() int {\n\treturn\n```"
	require.Error(t, validateGoFixSyntax(src), "broken Go in a fence with leading prose must be flagged")
}

// A fenced block emitted with CRLF line endings must be recognized and its valid Go
// must pass (independent-review HIGH).
func TestValidateGoFixSyntax_CRLFFencedValid(t *testing.T) {
	src := "```go\r\nfunc add(a, b int) int {\r\n\treturn a + b\r\n}\r\n```"
	assert.NoError(t, validateGoFixSyntax(src), "valid Go in a CRLF-terminated fence must pass")
}

// A fence whose closing ``` sits on the same line as the last code line (no newline
// before the close) must still be recognized (independent-review MEDIUM).
func TestValidateGoFixSyntax_ClosingFenceSameLineValid(t *testing.T) {
	src := "```go\nfunc add(a, b int) int { return a + b }```"
	assert.NoError(t, validateGoFixSyntax(src), "valid Go with the closing fence on the code line must pass")
}

// The '#' must be captured as part of the fence language tag so a c#/f# block is
// recognized as explicitly non-Go and skipped — even when its body carries Go-like
// block structure that would otherwise be parsed and flagged.
func TestValidateGoFixSyntax_CSharpFenceNotFlagged(t *testing.T) {
	src := "```c#\npublic void F() {\n    var x = 1\n}\n```"
	assert.NoError(t, validateGoFixSyntax(src), "a c# fenced block must not be flagged by the Go guard")
}

func TestValidateGoFixSyntax_FSharpFenceNotFlagged(t *testing.T) {
	src := "```f#\nlet f () =\n    let mutable x = 1\n    x\n```"
	assert.NoError(t, validateGoFixSyntax(src), "an f# fenced block must not be flagged by the Go guard")
}

// Valid Go whose body contains a string literal with a triple-backtick run must not
// be truncated by a premature in-body fence close: the closing fence is recognized
// only on its own line (anchored to line end), so an inline ``` cannot close the
// block.
func TestValidateGoFixSyntax_FencedGoWithTripleBacktickStringValid(t *testing.T) {
	src := "```go\nfunc f() string {\n\treturn \"```\"\n}\n```"
	assert.NoError(t, validateGoFixSyntax(src), "an inline triple-backtick string must not prematurely close the fence")
}

// A CommonMark 4-backtick fence (used when the body itself contains a triple
// backtick) must be matched as a unit: `{3,} captures the full opening/closing run
// rather than slicing at the inner three backticks.
func TestValidateGoFixSyntax_FourBacktickFenceValid(t *testing.T) {
	src := "````go\nfunc f() string { return \"```\" }\n````"
	assert.NoError(t, validateGoFixSyntax(src), "a 4-backtick fence wrapping triple-backtick Go must parse cleanly")
}

// Trailing whitespace after the opening fence's language tag (```go␠␠) must not
// prevent fence recognition; valid Go inside still passes.
func TestValidateGoFixSyntax_TrailingWhitespaceAfterOpenFenceValid(t *testing.T) {
	src := "```go  \nfunc add(a, b int) int { return a + b }\n```"
	assert.NoError(t, validateGoFixSyntax(src), "whitespace after the opening backticks must be tolerated")
}

func TestValidateGoFixSyntax_NonGoFenceNotFlagged(t *testing.T) {
	src := "```python\ndef add(a, b):\n    return a + b\n```"
	assert.NoError(t, validateGoFixSyntax(src), "an explicitly non-Go fenced block must not be flagged by the Go guard")
}

func TestValidateGoFixSyntax_EmptyNotFlagged(t *testing.T) {
	assert.NoError(t, validateGoFixSyntax(""), "empty fix must not be flagged")
	assert.NoError(t, validateGoFixSyntax("   \n\t"), "whitespace-only fix must not be flagged")
}

// A pathological multi-hundred-KB completion that is plausibly Go (block
// structure) yet does not parse must be short-circuited by the size cap rather
// than driven through the triple full-file AST parse. Above the cap the guard
// returns nil. Untrusted model output can be arbitrarily large; the triple
// parser.ParseFile passes run concurrently across the worker pool, so an
// unbounded parse is a real cost.
func TestValidateGoFixSyntax_OversizedInputNotFlagged(t *testing.T) {
	big := strings.Repeat("func f() {\n", 30000) // ~300KB, unbalanced braces — plausibly Go, does not parse
	require.Greater(t, len(big), 256*1024, "fixture must exceed the size cap")
	assert.NoError(t, validateGoFixSyntax(big), "input above the size cap must short-circuit to nil")
}

// BenchmarkValidateGoFixSyntax_LargeInput asserts the large-input path is bounded:
// with the size cap in place it must not perform the triple AST build.
func BenchmarkValidateGoFixSyntax_LargeInput(b *testing.B) {
	big := strings.Repeat("func f() {\n", 30000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = validateGoFixSyntax(big)
	}
}

func TestValidateGoFixSyntax_ErrorMentionsSyntax(t *testing.T) {
	src := "func add(a, b int) int {\n\treturn a +\n}"
	err := validateGoFixSyntax(src)
	require.Error(t, err)
	// The parser error should carry a recognizable syntax-error signal.
	assert.True(t, strings.Contains(err.Error(), "expected") || strings.Contains(err.Error(), "unexpected"),
		"the returned error should be a go/parser syntax error, got: %v", err)
}
