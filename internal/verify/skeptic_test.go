package verify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
)

func sampleFinding() reconcile.JSONFinding {
	return reconcile.JSONFinding{
		Severity:   "HIGH",
		File:       "internal/server/handler.go",
		Line:       42,
		Problem:    "nil dereference",
		Fix:        "add nil check",
		Evidence:   "line 42 reads req.User.ID",
		Confidence: "HIGH",
		Reviewers:  []string{"alice"},
	}
}

func sampleEntries() []payload.FileEntry {
	return []payload.FileEntry{
		{Path: "internal/server/handler.go", Body: "func handle() { _ = req.User.ID }\n"},
		{Path: "internal/server/mw.go", Body: "func auth() {}\n"},
	}
}

func TestBuildSkepticPrompt_ContainsAllSections(t *testing.T) {
	t.Parallel()
	got := buildSkepticPrompt(sampleFinding(), sampleEntries())

	// (1) adversarial role framing
	assert.Contains(t, strings.ToLower(got), "adversarial skeptic")
	assert.Contains(t, strings.ToLower(got), "disprove")
	// (2) all finding detail fields
	assert.Contains(t, got, "nil dereference")
	assert.Contains(t, got, "add nil check")
	assert.Contains(t, got, "line 42 reads req.User.ID")
	assert.Contains(t, got, "HIGH")
	// (3) code context from both entries (path + body in fenced blocks)
	assert.Contains(t, got, "internal/server/handler.go")
	assert.Contains(t, got, "internal/server/mw.go")
	assert.Contains(t, got, "req.User.ID")
	assert.Contains(t, got, "```")
	// (4) tool-access instructions
	assert.Contains(t, strings.ToLower(got), "tools")
	// (5) JSON verdict envelope spec
	assert.Contains(t, got, `"verdict"`)
	assert.Contains(t, got, "confirmed|refuted|unverifiable")
	assert.Contains(t, got, `"reasoning"`)
}

func TestBuildSkepticPrompt_Deterministic(t *testing.T) {
	t.Parallel()
	f, e := sampleFinding(), sampleEntries()
	assert.Equal(t, buildSkepticPrompt(f, e), buildSkepticPrompt(f, e))
}

func TestBuildSkepticPrompt_EmptyEntries(t *testing.T) {
	t.Parallel()
	got := buildSkepticPrompt(sampleFinding(), nil)
	require.NotEmpty(t, got)
	assert.Contains(t, strings.ToLower(got), "adversarial skeptic")
	assert.Contains(t, got, "confirmed|refuted|unverifiable")
}

func TestBuildSkepticPrompt_EmptyOptionalFields(t *testing.T) {
	t.Parallel()
	f := sampleFinding()
	f.Fix = ""
	f.Evidence = ""
	got := buildSkepticPrompt(f, sampleEntries())
	require.NotEmpty(t, got)
	// Still well-formed: role framing + verdict spec present, problem still shown.
	assert.Contains(t, got, "nil dereference")
	assert.Contains(t, got, "confirmed|refuted|unverifiable")
}

func TestBuildSkepticPrompt_ZeroValueFinding(t *testing.T) {
	t.Parallel()
	got := buildSkepticPrompt(reconcile.JSONFinding{}, nil)
	require.NotEmpty(t, got)
	assert.Contains(t, strings.ToLower(got), "adversarial skeptic")
	assert.Contains(t, got, "confirmed|refuted|unverifiable")
}

func TestBuildSkepticPrompt_SpecialCharsVerbatim(t *testing.T) {
	t.Parallel()
	f := sampleFinding()
	f.Problem = "uses `backtick` and <html> & ünïcode"
	got := buildSkepticPrompt(f, sampleEntries())
	assert.Contains(t, got, "uses `backtick` and <html> & ünïcode")
}
