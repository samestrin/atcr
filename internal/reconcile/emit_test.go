package reconcile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recAt() Options { return Options{ReconciledAt: time.Unix(1700000000, 0).UTC()} }

func TestReconcile_TwoReviewersAgreeHighConfidence(t *testing.T) {
	sources := []Source{
		{Name: "pool", Findings: []stream.Finding{
			mf("HIGH", "auth.go", 42, "token never expires here", "fix", "security", 15, "e", "greta"),
		}},
		{Name: "host", Findings: []stream.Finding{
			mf("HIGH", "auth.go", 43, "token never expires here", "fix", "security", 15, "e", "host"),
		}},
	}
	res := Reconcile(sources, recAt())
	require.Len(t, res.Findings, 1, "co-located identical findings collapse")
	assert.Equal(t, ConfHigh, res.Findings[0].Confidence)
	assert.Equal(t, []string{"greta", "host"}, res.Findings[0].Reviewers)
	assert.Equal(t, 1, res.Summary.ClustersCollapsed)
	assert.Equal(t, map[string]int{"pool": 1, "host": 1}, res.Summary.PerSourceCounts)
}

func TestReconcile_SortedBySeverityThenLocation(t *testing.T) {
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("LOW", "z.go", 5, "p1", "f", "style", 1, "e", "a"),
		mf("CRITICAL", "a.go", 1, "p2", "f", "sec", 9, "e", "b"),
		mf("MEDIUM", "m.go", 3, "p3", "f", "test", 4, "e", "c"),
	}}}
	res := Reconcile(sources, recAt())
	require.Len(t, res.Findings, 3)
	assert.Equal(t, "CRITICAL", res.Findings[0].Severity)
	assert.Equal(t, "MEDIUM", res.Findings[1].Severity)
	assert.Equal(t, "LOW", res.Findings[2].Severity)
}

func TestReconcile_AmbiguousAlwaysWrittenEvenWhenEmpty(t *testing.T) {
	res := Reconcile(nil, recAt())
	assert.NotNil(t, res.Ambiguous)
	assert.Empty(t, res.Ambiguous)
	assert.Equal(t, 0, res.Summary.TotalFindings)
}

func TestEmit_WritesAllArtifacts(t *testing.T) {
	dir := t.TempDir()
	sources := []Source{
		{Name: "pool", Findings: []stream.Finding{
			mf("CRITICAL", "auth.go", 42, "token never expires", "guard it", "security", 15, "saw it", "greta"),
		}},
		{Name: "host", Findings: []stream.Finding{
			mf("LOW", "auth.go", 42, "token never expires", "guard it", "security", 15, "also", "host"),
		}},
	}
	res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	require.NoError(t, Emit(dir, res))

	for _, name := range []string{FindingsTxt, FindingsJSON, ReportMD, SummaryJSON, AmbiguousJSON, DisagreementsJSON} {
		assert.FileExists(t, filepath.Join(dir, name))
	}

	// findings.txt parses back as 9-col reconciled with REVIEWERS + CONFIDENCE.
	tdata, _ := os.ReadFile(filepath.Join(dir, FindingsTxt))
	parsed, err := stream.ParseReconciled(tdata)
	require.NoError(t, err)
	require.Len(t, parsed.Findings, 1)
	assert.Equal(t, "CRITICAL", parsed.Findings[0].Severity, "max severity")
	assert.ElementsMatch(t, []string{"greta", "host"}, parsed.Findings[0].Reviewers)
	assert.Equal(t, "HIGH", parsed.Findings[0].Confidence)
	assert.Contains(t, parsed.Findings[0].Evidence, "disagreement: LOW vs CRITICAL",
		"disagreement folded into evidence for the flat contract")

	// findings.json carries the disagreement as a structured field.
	jdata, _ := os.ReadFile(filepath.Join(dir, FindingsJSON))
	var jf []JSONFinding
	require.NoError(t, json.Unmarshal(jdata, &jf))
	require.Len(t, jf, 1)
	assert.Equal(t, "LOW vs CRITICAL", jf[0].Disagreement)
	assert.Equal(t, []string{"greta", "host"}, jf[0].Reviewers)

	// summary.json has the required fields.
	sdata, _ := os.ReadFile(filepath.Join(dir, SummaryJSON))
	var sum Summary
	require.NoError(t, json.Unmarshal(sdata, &sum))
	assert.Equal(t, 1, sum.TotalFindings)
	assert.Equal(t, 1, sum.SeverityDisagreements)
	assert.Equal(t, "2023-11-14T22:13:20Z", sum.ReconciledAt)
	assert.ElementsMatch(t, []string{"pool", "host"}, sum.SourcesScanned)
}

func TestSummary_SkippedSourcesRecorded(t *testing.T) {
	// Files Discover skipped (read error / bad header) must surface in
	// summary.json as skipped_sources + skipped_source_count (TD-020) — v1 is
	// warn-and-continue, so the record is the loud signal, not a non-zero exit.
	sources := []Source{
		{Name: "ci", SkippedFiles: []string{"sources/ci/findings.txt"}},
		{Name: "host", Findings: []stream.Finding{
			mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e", "host"),
		}},
	}
	res := Reconcile(sources, recAt())
	assert.Equal(t, []string{"sources/ci/findings.txt"}, res.Summary.SkippedSources)
	assert.Equal(t, 1, res.Summary.SkippedSourceCount)

	// Zero-skip runs serialize as an empty array, not null.
	res2 := Reconcile(nil, recAt())
	assert.NotNil(t, res2.Summary.SkippedSources)
	assert.Empty(t, res2.Summary.SkippedSources)
	assert.Equal(t, 0, res2.Summary.SkippedSourceCount)
}

func TestEmit_DeterministicOutput(t *testing.T) {
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a.go", 1, "alpha", "f", "sec", 10, "e", "greta"),
		mf("MEDIUM", "b.go", 9, "beta", "f", "test", 5, "e", "kai"),
	}}}
	d1, d2 := t.TempDir(), t.TempDir()
	require.NoError(t, Emit(d1, Reconcile(sources, recAt())))
	require.NoError(t, Emit(d2, Reconcile(sources, recAt())))
	for _, name := range []string{FindingsTxt, FindingsJSON, ReportMD, SummaryJSON, DisagreementsJSON} {
		a, _ := os.ReadFile(filepath.Join(d1, name))
		b, _ := os.ReadFile(filepath.Join(d2, name))
		assert.Equal(t, string(a), string(b), "%s must be byte-identical across runs", name)
	}
}

func TestRenderMarkdown_EscapesInjectionAndZeroFindings(t *testing.T) {
	var empty strings.Builder
	require.NoError(t, RenderMarkdown(&empty, Reconcile(nil, recAt())))
	assert.Contains(t, empty.String(), "No findings.")

	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a.go", 1, "<script>alert(1)</script>", "f", "sec", 10, "e", "greta"),
	}}}
	var b strings.Builder
	require.NoError(t, RenderMarkdown(&b, Reconcile(sources, recAt())))
	assert.NotContains(t, b.String(), "<script>", "HTML must be escaped")
	assert.Contains(t, b.String(), "&lt;script&gt;")
}

func TestReconcile_OutOfScopeAnnotatedNotGated(t *testing.T) {
	// AC 06-04: out-of-scope findings are annotated (kept in the artifacts),
	// counted in summary.json, listed in a separate report section, and
	// excluded from the severity gate.
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("CRITICAL", "a.go", 1, "pre-existing issue untouched by this change", "f", CategoryOutOfScope, 10, "e", "greta"),
		mf("HIGH", "b.go", 2, "real issue in the change", "f", "security", 10, "e", "greta"),
	}}}
	res := Reconcile(sources, recAt())
	assert.Equal(t, 1, res.Summary.OutOfScope, "summary carries the out-of-scope count")
	assert.Equal(t, 2, res.Summary.TotalFindings, "annotated, not dropped")
	assert.Equal(t, 1, CountAtOrAbove(res.Findings, SevHigh, false), "the out-of-scope CRITICAL must not trip --fail-on HIGH")

	var b strings.Builder
	require.NoError(t, RenderMarkdown(&b, res))
	out := b.String()
	require.Contains(t, out, "## Out-of-Scope Findings")
	main := out[:strings.Index(out, "## Out-of-Scope Findings")]
	assert.NotContains(t, main, "pre-existing issue", "out-of-scope finding lives only in its own section")
	assert.Contains(t, out, "pre-existing issue", "still listed for the human reader")
	assert.Contains(t, main, "real issue in the change")
}

func TestRenderMarkdown_BacktickFilePathRendersInert(t *testing.T) {
	// A model-controlled File containing a backtick would close the code span
	// and let trailing text render as live markdown — the same injection class
	// AC 01-06 fixed in the report view (report/render.go codeSpan). Such paths
	// must fall back to HTML-escaped plain text instead of a code span.
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a`<i>.go", 1, "p", "f", "sec", 10, "e", "greta"),
	}}}
	var b strings.Builder
	require.NoError(t, RenderMarkdown(&b, Reconcile(sources, recAt())))
	out := b.String()
	assert.NotContains(t, out, "`a`", "backtick in path must not open/close a code span")
	assert.NotContains(t, out, "<i>", "HTML in path must be escaped")
	assert.Contains(t, out, "- a&#96;&lt;i&gt;.go:1 — ", "falls back to escaped plain text, no span")
}

func TestRenderMarkdown_PathWarningEscapesBacktickAndHTML(t *testing.T) {
	// The "did you mean ..." warning line renders File and PathSuggestion through
	// esc(), so backticks and HTML in those fields must be neutralized just like
	// in report/render.go esc() (TD reconcile emit.go:332).
	m := Merged{Finding: mf("HIGH", "a`<i>.go", 1, "p", "f", "security", 10, "e", "greta")}
	m.PathWarning = "file not found"
	m.PathSuggestion = "b`<i>.go"

	var b bytes.Buffer
	require.NoError(t, RenderMarkdown(&b, Result{Findings: []Merged{m}}))
	out := b.String()
	assert.Contains(t, out, "a&#96;&lt;i&gt;.go", "File with backtick must be escaped")
	assert.Contains(t, out, "b&#96;&lt;i&gt;.go", "PathSuggestion with backtick must be escaped")
	assert.NotContains(t, out, "<i>", "HTML must be escaped")
}

func TestRenderMarkdown_FlattensNewlineInjection(t *testing.T) {
	// A finding whose problem contains newlines must not inject markdown structure.
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a.go", 1, "line one\n## Forged Heading\n- forged bullet", "f", "sec", 10, "e", "greta"),
	}}}
	var b strings.Builder
	require.NoError(t, RenderMarkdown(&b, Reconcile(sources, recAt())))
	out := b.String()
	assert.NotContains(t, out, "\n## Forged Heading", "newlines flattened — no injected heading")
	assert.Contains(t, out, "line one ## Forged Heading - forged bullet")
}

// TestReadReconciledFindings_SharedLoaderContract pins the single shared loader
// both the CLI report command and the MCP report handler must use (TD
// report.go:71 dedup): a missing findings.json surfaces the raw os.ErrNotExist
// sentinel so each layer phrases its own guidance, an empty or malformed file
// is a parse error, and a valid file round-trips the JSONFinding records.
func TestReadReconciledFindings_SharedLoaderContract(t *testing.T) {
	reviewDir := t.TempDir()

	// Missing file → os.ErrNotExist sentinel, not a wrapped guidance string.
	_, err := ReadReconciledFindings(reviewDir)
	require.ErrorIs(t, err, os.ErrNotExist)

	path := filepath.Join(reviewDir, reconciledSubdir, FindingsJSON)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	// Empty file → parse error.
	require.NoError(t, os.WriteFile(path, []byte("  \n"), 0o644))
	_, err = ReadReconciledFindings(reviewDir)
	require.ErrorContains(t, err, "empty")

	// Malformed JSON → parse error.
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))
	_, err = ReadReconciledFindings(reviewDir)
	require.Error(t, err)
	require.NotErrorIs(t, err, os.ErrNotExist)

	// Valid file → records round-trip.
	body, jerr := json.Marshal([]JSONFinding{{Severity: "HIGH", File: "a.go", Line: 7, Problem: "p"}})
	require.NoError(t, jerr)
	require.NoError(t, os.WriteFile(path, body, 0o644))
	got, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "a.go", got[0].File)
	assert.Equal(t, 7, got[0].Line)
}

// --- Epic 1.1: reserved per-finding verification block (absent in 1.x) ---

// TestJSONFinding_VerificationOmittedIn1x verifies a 1.x findings.json record
// (no verification produced) marshals without a "verification" key, so the
// reserved block is genuinely absent until a future stage populates it.
func TestJSONFinding_VerificationOmittedIn1x(t *testing.T) {
	f := JSONFinding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Reviewers: []string{"greta"}, Confidence: "MEDIUM"}
	data, err := json.Marshal(f)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "verification", "reserved verification block absent in 1.x")
	assert.Nil(t, f.Verification)
}

// TestJSONFinding_VerificationRoundTrips verifies a findings.json record that
// carries a verification block parses into the struct and re-marshals intact —
// renderers and readers must tolerate its presence (forward compatibility).
func TestJSONFinding_VerificationRoundTrips(t *testing.T) {
	raw := `{"severity":"HIGH","file":"a.go","line":1,"problem":"p","fix":"f","category":"security","est_minutes":10,"evidence":"e","reviewers":["greta"],"confidence":"HIGH","verification":{"verdict":"confirmed","skeptic":"otto","notes":"reproduced"}}`
	var f JSONFinding
	require.NoError(t, json.Unmarshal([]byte(raw), &f))
	require.NotNil(t, f.Verification)
	assert.Equal(t, "confirmed", f.Verification.Verdict)
	assert.Equal(t, "otto", f.Verification.Skeptic)
	assert.Equal(t, "reproduced", f.Verification.Notes)

	out, err := json.Marshal(f)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"verification"`)
	assert.Contains(t, string(out), `"verdict":"confirmed"`)
}

// TestReadReconciledFindings_ToleratesVerificationPresenceAndAbsence verifies
// the shared findings.json reader handles records both with and without the
// reserved verification block.
func TestReadReconciledFindings_ToleratesVerificationPresenceAndAbsence(t *testing.T) {
	reviewDir := t.TempDir()
	dir := filepath.Join(reviewDir, reconciledSubdir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := `[
  {"severity":"HIGH","file":"a.go","line":1,"problem":"p1","reviewers":["greta"],"confidence":"MEDIUM"},
  {"severity":"LOW","file":"b.go","line":2,"problem":"p2","reviewers":["otto"],"confidence":"LOW","verification":{"verdict":"refuted","skeptic":"dax","notes":"n"}}
]`
	require.NoError(t, os.WriteFile(filepath.Join(dir, FindingsJSON), []byte(body), 0o644))
	got, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Nil(t, got[0].Verification, "absent verification tolerated")
	require.NotNil(t, got[1].Verification, "present verification tolerated")
	assert.Equal(t, "refuted", got[1].Verification.Verdict)
}

// TestJSONFinding_EmptyVerdictLoadsDefensively verifies that a findings.json
// record carrying an empty verdict string loads without error. Per
// docs/findings-format.md, an absent or unrecognized verdict (including "")
// must be treated as "unverified" by consumers — the loader itself is not the
// validation boundary.
func TestJSONFinding_EmptyVerdictLoadsDefensively(t *testing.T) {
	reviewDir := t.TempDir()
	dir := filepath.Join(reviewDir, reconciledSubdir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := `[{"severity":"HIGH","file":"a.go","line":1,"problem":"p","reviewers":["greta"],"confidence":"MEDIUM","verification":{"verdict":"","skeptic":"otto","notes":""}}]`
	require.NoError(t, os.WriteFile(filepath.Join(dir, FindingsJSON), []byte(body), 0o644))
	got, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err, "empty verdict must load without error")
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Verification)
	assert.Equal(t, "", got[0].Verification.Verdict, "empty verdict preserved as-is; consumers treat it as unverified per docs/findings-format.md")
}

// TestJSONFindings_PreservesVerification verifies that a Merged finding with a
// non-nil Verification block is carried through JSONFindings into findings.json
// (Epic 3.0 forward-compatibility).
func TestJSONFindings_PreservesVerification(t *testing.T) {
	res := Result{Findings: []Merged{{
		Finding:      mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "greta"),
		Disagreement: "",
		Verification: &Verification{Verdict: "confirmed", Skeptic: "otto", Notes: "reproduced"},
	}}}

	got := res.JSONFindings()
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Verification, "Verification must be preserved by JSONFindings")
	assert.Equal(t, "confirmed", got[0].Verification.Verdict)
	assert.Equal(t, "otto", got[0].Verification.Skeptic)
	assert.Equal(t, "reproduced", got[0].Verification.Notes)

	// Full RenderJSON/ReadReconciledFindings round-trip.
	dir := t.TempDir()
	reconDir := filepath.Join(dir, reconciledSubdir)
	require.NoError(t, Emit(reconDir, res))
	readBack, err := ReadReconciledFindings(dir)
	require.NoError(t, err)
	require.Len(t, readBack, 1)
	require.NotNil(t, readBack[0].Verification, "Verification must survive round-trip")
	assert.Equal(t, "confirmed", readBack[0].Verification.Verdict)
	assert.Equal(t, "otto", readBack[0].Verification.Skeptic)
	assert.Equal(t, "reproduced", readBack[0].Verification.Notes)

	// A nil Verification must still marshal without a "verification" key.
	res2 := Result{Findings: []Merged{{Finding: mf("LOW", "b.go", 2, "p2", "f2", "style", 1, "e", "bruce")}}}
	data, err := json.Marshal(res2.JSONFindings())
	require.NoError(t, err)
	assert.NotContains(t, string(data), "verification", "nil Verification must stay omitted")
}

// TestJSONFinding_PathSuggestionOmittedWhenEmpty: a finding with no suggestion
// must serialize without a path_suggestion key — byte-identical to pre-5.4
// output (Epic 5.4 AC6 / Success Criteria: no findings.json change when absent).
func TestJSONFinding_PathSuggestionOmittedWhenEmpty(t *testing.T) {
	f := JSONFinding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Reviewers: []string{"greta"}, Confidence: "MEDIUM"}
	data, err := json.Marshal(f)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "path_suggestion", "empty suggestion must be omitted")
}

// TestJSONFindings_CarriesPathSuggestion: a Merged finding's PathSuggestion is
// carried into the JSON schema and survives a RenderJSON round-trip (AC6).
func TestJSONFindings_CarriesPathSuggestion(t *testing.T) {
	m := Merged{Finding: mf("HIGH", "internal/auth/validator.go", 1, "p", "f", "security", 10, "e", "greta")}
	m.PathWarning = "file not found"
	m.PathSuggestion = "internal/auth/validate.go"
	res := Result{Findings: []Merged{m}}

	got := res.JSONFindings()
	require.Len(t, got, 1)
	assert.Equal(t, "internal/auth/validate.go", got[0].PathSuggestion)

	dir := t.TempDir()
	reconDir := filepath.Join(dir, reconciledSubdir)
	require.NoError(t, Emit(reconDir, res))
	readBack, err := ReadReconciledFindings(dir)
	require.NoError(t, err)
	require.Len(t, readBack, 1)
	assert.Equal(t, "internal/auth/validate.go", readBack[0].PathSuggestion)
	// The original hallucinated path is preserved (suggest-only, AC7).
	assert.Equal(t, "internal/auth/validator.go", readBack[0].File)
}

// TestRenderMarkdown_ShowsPathSuggestion: report.md renders a "(did you mean …)"
// clause next to the file-not-found warning when a suggestion exists (AC6).
func TestRenderMarkdown_ShowsPathSuggestion(t *testing.T) {
	m := Merged{Finding: mf("HIGH", "internal/auth/validator.go", 1, "p", "f", "security", 10, "e", "greta")}
	m.PathWarning = "file not found"
	m.PathSuggestion = "internal/auth/validate.go"

	var b bytes.Buffer
	require.NoError(t, RenderMarkdown(&b, Result{Findings: []Merged{m}}))
	out := b.String()
	assert.Contains(t, out, "File not found")
	assert.Contains(t, out, "did you mean")
	assert.Contains(t, out, "internal/auth/validate.go")
}
