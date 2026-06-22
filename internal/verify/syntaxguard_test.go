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

func TestValidateGoFixSyntax_NonGoFenceNotFlagged(t *testing.T) {
	src := "```python\ndef add(a, b):\n    return a + b\n```"
	assert.NoError(t, validateGoFixSyntax(src), "an explicitly non-Go fenced block must not be flagged by the Go guard")
}

func TestValidateGoFixSyntax_EmptyNotFlagged(t *testing.T) {
	assert.NoError(t, validateGoFixSyntax(""), "empty fix must not be flagged")
	assert.NoError(t, validateGoFixSyntax("   \n\t"), "whitespace-only fix must not be flagged")
}

func TestValidateGoFixSyntax_ErrorMentionsSyntax(t *testing.T) {
	src := "func add(a, b int) int {\n\treturn a +\n}"
	err := validateGoFixSyntax(src)
	require.Error(t, err)
	// The parser error should carry a recognizable syntax-error signal.
	assert.True(t, strings.Contains(err.Error(), "expected") || strings.Contains(err.Error(), "unexpected"),
		"the returned error should be a go/parser syntax error, got: %v", err)
}
