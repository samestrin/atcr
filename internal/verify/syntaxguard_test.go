package verify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateGoFixSyntax is the local syntax guard (Epic 7.1 / Epic 7.5): it parses a
// generated fix with go/parser and returns a non-nil error ONLY when the fix is
// plausibly Go code yet fails to parse. Free-form prose change-instructions and
// explicitly non-Go fenced blocks pass through with a nil error so a legitimate fix
// is never falsely flagged (false positives degrade trust, which is exactly what this
// guard exists to prevent). Epic 7.5 adds suppression for unfenced JSON/config
// brace-content — see the labelled section below.

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
	// `:=` is deliberately not a code signal (inline `:=` appears in prose; see
	// TestValidateGoFixSyntax_ProseWithInlineShortAssignNotFlagged). This line ends
	// in a lone { with no matching close-brace line — blockOpenRe alone is no longer
	// a sufficient code signal (it fires on prose too). This is a documented
	// conservative-recall boundary: prefer the false negative over a false positive.
	src := "x := func( {"
	assert.NoError(t, validateGoFixSyntax(src), "a line ending in { with no block-close is the conservative-recall boundary — not flagged")
}

// Characterization of the guard's deliberate conservative-recall boundary: an
// unfenced, single-line broken fragment with NO block structure (no trailing open
// brace, no leading close brace, no declaration keyword) is indistinguishable from a
// prose change-instruction and is intentionally NOT flagged. This is a documented
// trade-off (false-negative preferred over false-positive); do not "fix" it by
// loosening looksLikeGoCode, which would reintroduce the false-positive class the
// guard exists to avoid.
func TestValidateGoFixSyntax_BrokenUnfencedNoBlockStructureNotFlagged(t *testing.T) {
	src := "result = compute(a, b"
	assert.NoError(t, validateGoFixSyntax(src), "an unfenced broken one-liner with no block structure is the documented conservative-recall boundary")
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

// A fence with a space between the opening backticks and the language tag
// (CommonMark permits ``` go as well as ```go) must still be recognized so a
// malformed Go block inside it is flagged rather than silently passing.
func TestValidateGoFixSyntax_SpacedFenceInvalidGoFlagged(t *testing.T) {
	// The fenced body is broken Go, but without fence extraction the surrounding
	// backticks hide the code signal and the unfenced text does not look like Go,
	// so the bug would silently pass.
	src := "``` go\nreturn a +\n```"
	err := validateGoFixSyntax(src)
	require.Error(t, err, "broken Go in a spaced fence must be flagged")
}

func TestValidateGoFixSyntax_NonGoFenceNotFlagged(t *testing.T) {
	src := "```python\ndef add(a, b):\n    return a + b\n```"
	assert.NoError(t, validateGoFixSyntax(src), "an explicitly non-Go fenced block must not be flagged by the Go guard")
}

// A non-Go fence whose info string contains trailing words (CommonMark allows an
// info string after the language tag) must still be recognized as non-Go, even
// when the fenced body carries Go-like block structure that would otherwise be
// flagged as broken Go.
func TestValidateGoFixSyntax_NonGoFenceWithInfoStringNotFlagged(t *testing.T) {
	src := "```python extra\nfunc add(a, b int) int {\n\treturn a +\n}\n```"
	assert.NoError(t, validateGoFixSyntax(src), "a non-Go fence with trailing info string must not be flagged by the Go guard")
}

// blockOpenRe must only fire on a clean block-open line (non-brace content then a
// single trailing brace), not any line that merely ends in '{'. A prose-ish line
// carrying an inner brace and no other code signal must pass through unflagged so the
// guard does not false-flag a change-instruction as broken Go.
func TestValidateGoFixSyntax_InlineDoubleBraceLineNotFlagged(t *testing.T) {
	src := "replace it with x{y{"
	assert.NoError(t, validateGoFixSyntax(src), "a line with an inner brace must not be treated as a Go block open")
}

// A prose change-instruction whose LAST character is a lone opening brace satisfies
// blockOpenRe but must NOT be flagged: blockOpenRe alone is not a reliable code signal
// when there is no matching close-brace line in the same snippet. This is the primary
// false-positive case blockOpenRe-co-occurrence guards against.
func TestValidateGoFixSyntax_ProseLineEndingInLoneBraceNotFlagged(t *testing.T) {
	src := "Wrap the body in the literal that opens with {"
	assert.NoError(t, validateGoFixSyntax(src), "a prose change-instruction ending in a lone { must not be flagged as invalid Go")
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

// --- Epic 7.5: unfenced non-Go (JSON/config) brace-content suppression ---

// AC1: an unfenced multi-line JSON object with block braces must not be flagged. Its
// braces satisfy the block-brace signal and it fails to parse as Go, but it is data,
// not code — the residual false positive Epic 7.5 closes.
func TestValidateGoFixSyntax_UnfencedJSONObjectNotFlagged(t *testing.T) {
	src := "{\n  \"timeout\": 30,\n  \"retries\": 3\n}"
	require.NoError(t, validateGoFixSyntax(src), "an unfenced JSON object must not be flagged as invalid Go")
}

// AC1 boundary: an empty JSON object has no block-brace lines and is not flagged.
func TestValidateGoFixSyntax_EmptyJSONObjectNotFlagged(t *testing.T) {
	src := "{}"
	require.NoError(t, validateGoFixSyntax(src), "an empty JSON object must not be flagged as invalid Go")
}

// AC1 boundary: a trailing-comma JSON object is still JSON-shaped (quoted-key line)
// and is suppressed rather than flagged invalid_syntax.
func TestValidateGoFixSyntax_TrailingCommaJSONObjectNotFlagged(t *testing.T) {
	src := "{\n  \"timeout\": 30,\n  \"retries\": 3,\n}"
	require.NoError(t, validateGoFixSyntax(src), "a trailing-comma JSON object must not be flagged as invalid Go")
}

// AC1: nested unfenced JSON is likewise suppressed.
func TestValidateGoFixSyntax_UnfencedNestedJSONNotFlagged(t *testing.T) {
	src := "{\n  \"server\": {\n    \"host\": \"localhost\",\n    \"port\": 8080\n  }\n}"
	require.NoError(t, validateGoFixSyntax(src), "an unfenced nested JSON object must not be flagged")
}

// AC1: a JSON array of objects (leading `[`, quoted-key members) is also suppressed.
func TestValidateGoFixSyntax_UnfencedJSONArrayNotFlagged(t *testing.T) {
	src := "[\n  {\n    \"name\": \"a\",\n    \"on\": true\n  }\n]"
	require.NoError(t, validateGoFixSyntax(src), "an unfenced JSON array of objects must not be flagged")
}

// AC1 boundary: a single-key JSON object whose only key contains an escaped quote
// must still be recognized as JSON-shaped and suppressed, not flagged invalid_syntax.
func TestValidateGoFixSyntax_UnfencedJSONObjectEscapedQuoteKeyNotFlagged(t *testing.T) {
	src := "{\n  \"a\\\"b\": 1\n}"
	require.NoError(t, validateGoFixSyntax(src), "a single-key JSON object with an escaped-quote key must not be flagged")
}

// AC2 guard: the JSON suppression must only reduce flagging — broken Go with block
// braces but NO JSON quoted-key shape must STILL be flagged (nothing previously
// flagged-as-broken-Go becomes spared just because it has braces).
func TestValidateGoFixSyntax_BrokenGoWithBracesStillFlagged_75(t *testing.T) {
	src := "func add(a, b int) int {\n\treturn a +\n}"
	require.Error(t, validateGoFixSyntax(src), "JSON suppression must not spare brace-structured broken Go")
}

// The size cap must apply to the extracted code, not the raw fix, so a small
// fenced invalid-Go snippet is still validated even when wrapped in a huge prose
// blob that would otherwise exceed the cap.
func TestValidateGoFixSyntax_SizeCapAppliesToExtractedCode(t *testing.T) {
	bigProse := strings.Repeat("This is a sentence. ", 20000) // >256KB
	src := bigProse + "\n```go\nfunc add(\n```\n" + bigProse
	err := validateGoFixSyntax(src)
	require.Error(t, err, "invalid Go inside a small fence must be flagged even when wrapped in huge prose")
}

// AC2 boundary: the JSON anchor is a quoted key at LINE START. A broken Go switch
// whose case label is a quoted string (`case "foo":`) does not start with the quote,
// so it is not mistaken for JSON and remains flagged.
func TestValidateGoFixSyntax_BrokenGoSwitchStringCaseStillFlagged(t *testing.T) {
	src := "switch s {\ncase \"foo\":\n\treturn 1 +\n}"
	require.Error(t, validateGoFixSyntax(src), "a broken Go switch with a quoted case label must still be flagged")
}

// AC4 characterization (deliberate trade-off): a BROKEN Go map/struct literal with
// string keys parses-fail then matches the JSON quoted-key shape, so it is suppressed
// (a false negative). This is the accepted cost of closing the unfenced-JSON false
// positive under the conservative-recall policy — a false negative is preferred over a
// false positive. Do NOT "fix" this by narrowing the suppression; it would risk
// reintroducing the JSON false positive. (A VALID such literal parses cleanly and
// never reaches this path.)
func TestValidateGoFixSyntax_BrokenGoMapSuppressed(t *testing.T) {
	src := "{\n  \"a\": 1,\n  \"b\":\n}" // broken (missing value) but JSON-shaped
	require.NoError(t, validateGoFixSyntax(src),
		"a broken JSON-shaped fix is suppressed — the accepted conservative-recall false negative")
}

// looksLikeNonGoBraces is the suppression predicate: true only for JSON/config object
// shapes (a line beginning with a quoted key) with no Go declaration keyword.
func TestLooksLikeNonGoBraces(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected bool
	}{
		{"quoted-key object is non-Go", "{\n  \"k\": 1\n}", true},
		{"empty-key object is non-Go", "{\n  \"\": 1\n}", true},
		{"unicode-key object is non-Go", "{\n  \"café\": 1\n}", true},
		{"escaped-quote key is non-Go", "{\n  \"a\\\"b\": 1\n}", true},
		{"a Go func is not non-Go braces", "func f() {\n\treturn 1\n}", false},
		{"a Go type decl is not non-Go braces", "type T struct {\n\tX int\n}", false},
		{"a quoted case label does not start the line", "switch s {\ncase \"x\":\n}", false},
		// Go keyword overrides JSON-key match: both must hold for suppression.
		{"json key AND Go keyword: keyword wins", "func init() {\n  \"k\": 1\n}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, looksLikeNonGoBraces(tt.src))
		})
	}
}
