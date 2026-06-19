package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{Field: "git ref", Value: "bad..ref", Message: "contains invalid characters"}
	assert.Equal(t, `invalid git ref "bad..ref": contains invalid characters`, err.Error())
}

func TestGitRef(t *testing.T) {
	// AC1: a plain branch name is valid.
	require.NoError(t, GitRef("main"))
	require.NoError(t, GitRef(strings.Repeat("a", 255))) // exactly the max length is allowed

	// AC2: ".." is rejected with the documented message.
	err := GitRef("invalid..ref")
	require.Error(t, err)
	assert.Equal(t, `invalid git ref "invalid..ref": contains invalid characters`, err.Error())

	// Each invalid-character class trips the same guard.
	for _, ref := range []string{"has space", "has\ttab", "has\nnewline", "tilde~", "caret^", "colon:"} {
		assert.Error(t, GitRef(ref), ref)
	}

	// Control chars, shell/git metacharacters, and leading dash that git-check-ref-format rejects.
	for _, ref := range []string{
		"has\rCR", "has\vVT", "has\fFF", "has\x7fDEL",
		"has\\backslash", "has?question", "has*asterisk", "has[bracket",
		"-leading-dash",
	} {
		assert.Error(t, GitRef(ref), "GitRef should reject %q", ref)
	}

	// Empty and over-length are distinct messages.
	assert.EqualError(t, GitRef(""), `invalid git ref "": must not be empty`)
	long := strings.Repeat("a", 256)
	assert.EqualError(t, GitRef(long), `invalid git ref "`+long+`": must be <= 255 characters`)
}

func TestFilePath(t *testing.T) {
	require.NoError(t, FilePath("reviews/out"))
	require.NoError(t, FilePath("/home/user/reviews"))

	assert.EqualError(t, FilePath(""), `invalid file path "": must not be empty`)
	assert.EqualError(t, FilePath("../escape"), `invalid file path "../escape": must not contain ..`)

	// AC3: system directories are rejected with the documented message.
	assert.EqualError(t, FilePath("/etc/passwd"),
		`invalid file path "/etc/passwd": must not reference system directories`)
	for _, p := range []string{"/proc/self", "/sys/kernel", "/etc", "/proc", "/sys"} {
		err := FilePath(p)
		require.Error(t, err, p)
		assert.Contains(t, err.Error(), "must not reference system directories", p)
	}

	// Directory-boundary match: siblings of the system dirs are NOT system
	// directories and must pass (no bare-prefix false positives).
	for _, p := range []string{"/etcd/data", "/etc-backup", "/system/x", "/procession"} {
		require.NoError(t, FilePath(p), p)
	}
}

func TestReviewID(t *testing.T) {
	require.NoError(t, ReviewID("2026-06-18_my-branch"))
	require.NoError(t, ReviewID(strings.Repeat("a", 100))) // exactly the max length is allowed

	assert.EqualError(t, ReviewID(""), `invalid review ID "": must not be empty`)

	// AC4: path-traversal input fails the alphanumeric/dash/underscore allowlist.
	assert.EqualError(t, ReviewID("../../../etc/passwd"),
		`invalid review ID "../../../etc/passwd": must contain only alphanumeric characters, dash, and underscore`)

	long := strings.Repeat("a", 101)
	assert.EqualError(t, ReviewID(long), `invalid review ID "`+long+`": must be <= 100 characters`)
}

func TestSeverity(t *testing.T) {
	for _, s := range []string{"low", "MEDIUM", "High", "critical"} {
		require.NoError(t, Severity(s), s) // case-insensitive
	}

	// AC5: an unknown value is rejected with the documented message.
	assert.EqualError(t, Severity("INVALID"),
		`invalid severity "INVALID": must be one of: LOW, MEDIUM, HIGH, CRITICAL`)
	assert.Error(t, Severity("")) // empty is not a valid severity
}

func TestEnum(t *testing.T) {
	allowed := []string{"md", "json", "checklist"}
	require.NoError(t, Enum("format", "json", allowed))

	err := Enum("format", "xml", allowed)
	require.Error(t, err)
	assert.Equal(t, `invalid format "xml": must be one of: md, json, checklist`, err.Error())
}
