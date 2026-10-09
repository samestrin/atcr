package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSalvagedStatus marks a source leaf's status.json salvaged, the way
// internal/fanout's statusFor does.
func writeSalvagedStatus(t *testing.T, reviewDir, leaf string) {
	t.Helper()
	dir := filepath.Join(reviewDir, "sources", leaf)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status.json"),
		[]byte(`{"agent":"`+leaf+`","status":"ok","salvaged":true}`), 0o644))
}

// TestStampJustifications_SalvagedSourceContributesNoJustification closes the
// anchor hole the leading-run exclusion structurally cannot see.
//
// A SALVAGED review.md is raw promoted reasoning_content: the provider returned
// no content and the client moved the model's chain-of-thought into it. It
// carries no <think> tags at all, so SplitThink returns it unchanged, the draft
// line set is empty, and buildAnchorIndex indexes the WHOLE file. matchNarrative
// ranks by tier first, so a reasoning line citing the exact FILE:LINE outranks a
// real reviewer's prose that only mentions the file — and the published
// justification is then drawn from a reply every lane refused as an abandoned
// draft. Justification and source_report persist into localdebt's append-only
// store, so no later reconcile can replace the forged provenance.
func TestStampJustifications_SalvagedSourceContributesNoJustification(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", ""+
		"Let me work through this.\n"+
		"I think **`internal/auth/token.go:42`** is where the signature check belongs.\n"+
		"But I am not sure yet.\n")
	writeSalvagedStatus(t, reviewDir, "host")

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"host"}}}
	stampJustifications(jf, reviewDir)

	assert.Empty(t, jf[0].Justification,
		"a salvaged reply is reasoning the model never committed to — it cannot be a justification")
	assert.Nil(t, jf[0].SourceReport,
		"and it must not be published as the finding's provenance")
}

// The sharper case: a salvaged source must not OUTRANK a real reviewer's prose.
// Tier beats reviewer preference in matchNarrative, so the salvaged leaf's exact
// line citation wins over a real leaf that only names the file.
func TestStampJustifications_SalvagedSourceDoesNotOutrankRealProse(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "aaa-salvaged", ""+
		"Thinking: **`internal/auth/token.go:42`** might be the spot. Draft only.\n")
	writeSalvagedStatus(t, reviewDir, "aaa-salvaged")
	writeReview(t, reviewDir, "zzz-real", ""+
		"## Findings\n"+
		"1. **`internal/auth/token.go:42` — JWT signature not verified.** The real narrative.\n")

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"aaa-salvaged", "zzz-real"}}}
	stampJustifications(jf, reviewDir)

	require.NotNil(t, jf[0].SourceReport)
	assert.Equal(t, "sources/zzz-real/review.md", jf[0].SourceReport.Path,
		"the committed reviewer's prose is the only real provenance here")
	assert.Contains(t, jf[0].Justification, "The real narrative")
	assert.NotContains(t, jf[0].Justification, "Draft only")
}

// And a source with no status.json, or one that is not salvaged, must be
// unaffected — the overwhelming majority, and the fail-open direction: an
// unreadable status must never silently drop a real reviewer's justification.
func TestStampJustifications_UnsalvagedAndStatuslessSourcesStillMatch(t *testing.T) {
	for _, tc := range []struct{ name, status string }{
		{"no status.json", ""},
		{"status says not salvaged", `{"agent":"host","status":"ok"}`},
		{"status is malformed", `{not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reviewDir := t.TempDir()
			writeReview(t, reviewDir, "host", ""+
				"## Findings\n"+
				"1. **`internal/auth/token.go:42` — JWT signature not verified.** The real narrative.\n")
			if tc.status != "" {
				dir := filepath.Join(reviewDir, "sources", "host")
				require.NoError(t, os.WriteFile(filepath.Join(dir, "status.json"), []byte(tc.status), 0o644))
			}

			jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"host"}}}
			stampJustifications(jf, reviewDir)

			assert.Contains(t, jf[0].Justification, "The real narrative",
				"only an explicit salvaged:true may withhold a justification")
		})
	}
}

// writeStatusBody writes a source leaf's status.json verbatim, for the
// stop-reason shapes internal/fanout's statusFor now records.
func writeStatusBody(t *testing.T, reviewDir, leaf, body string) {
	t.Helper()
	dir := filepath.Join(reviewDir, "sources", leaf)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status.json"), []byte(body), 0o644))
}

// TestStampJustifications_StopReasonSalvageContributesItsNarrative: a reply
// salvaged on a stop finish reason is a finished answer on the reasoning
// channel, not an abandoned draft. Its findings ship (the unchunked findings
// lane honours it), so its narrative is their provenance and must be indexed.
// Before the change the bare salvaged bit withheld it whole.
func TestStampJustifications_StopReasonSalvageContributesItsNarrative(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", ""+
		"## Findings\n"+
		"1. **`internal/auth/token.go:42` — JWT signature not verified.** The committed answer.\n")
	writeStatusBody(t, reviewDir, "host",
		`{"agent":"host","status":"ok","salvaged":true,"salvaged_on_stop":true}`)

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"host"}}}
	stampJustifications(jf, reviewDir)

	assert.Contains(t, jf[0].Justification, "The committed answer",
		"a stop-reason salvage is a finished answer; its narrative is real provenance")
	require.NotNil(t, jf[0].SourceReport)
	assert.Equal(t, "sources/host/review.md", jf[0].SourceReport.Path)
}

// The tier-outranking guard still holds for a truncated (abandoned) salvage
// beside a stop-reason one: only the abandoned draft is withheld.
func TestStampJustifications_AbandonedSalvageStillWithheldBesideStopReasonOne(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "aaa-truncated", ""+
		"Thinking: **`internal/auth/token.go:42`** might be the spot. Draft only.\n")
	writeSalvagedStatus(t, reviewDir, "aaa-truncated")
	writeReview(t, reviewDir, "zzz-stopped", ""+
		"## Findings\n"+
		"1. **`internal/auth/token.go:42` — JWT signature not verified.** The committed answer.\n")
	writeStatusBody(t, reviewDir, "zzz-stopped",
		`{"agent":"zzz-stopped","status":"ok","salvaged":true,"salvaged_on_stop":true}`)

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"aaa-truncated", "zzz-stopped"}}}
	stampJustifications(jf, reviewDir)

	require.NotNil(t, jf[0].SourceReport)
	assert.Equal(t, "sources/zzz-stopped/review.md", jf[0].SourceReport.Path)
	assert.NotContains(t, jf[0].Justification, "Draft only")
}
