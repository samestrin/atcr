package scorecard

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedExportNow is a stable reference time for deterministic export tests:
// records are dated relative to it and it is passed to Export as both the
// envelope timestamp and the --since window anchor, so output is reproducible.
var fixedExportNow = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

// exportRec builds a reviewer record dated ageDays before fixedExportNow with a
// full metric set (incl. tokens) so export aggregation and preservation can be
// asserted exactly.
func exportRec(reviewer, model string, ageDays int) Record {
	ts := fixedExportNow.AddDate(0, 0, -ageDays).UTC().Format(time.RFC3339)
	return Record{
		SchemaVersion:        SchemaVersion,
		RecordType:           RecordTypeReviewer,
		RunID:                ts + "-" + reviewer,
		Reviewer:             reviewer,
		Model:                model,
		Role:                 "reviewer",
		FindingsRaised:       12,
		FindingsCorroborated: 7,
		FindingsSolo:         5,
		CorroborationRate:    ratio(7, 12),
		CostUSD:              0.04,
		TokensIn:             14200,
		TokensOut:            4000,
		LatencyMS:            9100,
	}
}

func parseEnvelope(t *testing.T, data []byte) ExportEnvelope {
	t.Helper()
	var env ExportEnvelope
	require.NoError(t, json.Unmarshal(data, &env), "export output must be valid JSON")
	return env
}

func TestExport_ValidJSON(t *testing.T) {
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)

	env := parseEnvelope(t, data)
	assert.Equal(t, 1, env.SchemaVersion)
	_, perr := time.Parse(time.RFC3339, env.ExportedAt)
	require.NoError(t, perr, "exported_at must be RFC3339")
	assert.Equal(t, "30d", env.Filters.Since)
	require.Len(t, env.Records, 1)
	assert.Equal(t, 0, env.Records[0].Index, "first record index is 0")
	assert.Equal(t, 1, env.Records[0].Runs, "a single source record aggregates to runs=1")
}

func TestExport_AnonymizationStripsRunID(t *testing.T) {
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	assert.NotContains(t, s, "run_id", "run_id key must not appear in public output")
	assert.NotContains(t, s, "-bruce", "the run_id value (timestamp-base) must not leak")
}

func TestExport_AnonymizationStripsPathLike(t *testing.T) {
	rec := exportRec("bruce", "claude /Users/sam/secret ~/.config/atcr", 1)
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	for _, p := range []string{"/Users/", "/home/", "~/.config", `C:\`} {
		assert.NotContains(t, s, p, "export must strip path-like string %q", p)
	}
}

func TestExport_AnonymizationStripsAPIKeys(t *testing.T) {
	rec := exportRec("bruce", "claude sk-ant-abc123XYZ ghp_deadBEEF Bearer tok123", 1)
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	for _, k := range []string{"sk-ant-", "sk-", "ghp_", "xoxb-", "Bearer "} {
		assert.NotContains(t, s, k, "export must strip API-key pattern %q", k)
	}
}

func TestExport_AnonymizationStripsGluedPathAndWinPath(t *testing.T) {
	// A path glued to a non-space byte (host=/etc/passwd) and a Windows path must
	// both be stripped — the scrub is not anchored to whitespace.
	rec := exportRec("bruce", `host=/etc/passwd C:\Users\sam\id_rsa`, 1)
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	for _, p := range []string{"/etc/passwd", `C:\`, `\Users\`, "id_rsa"} {
		assert.NotContains(t, s, p, "must strip glued/windows path %q", p)
	}
}

func TestExport_AnonymizationStripsEmailAndMoreKeys(t *testing.T) {
	rec := exportRec("bruce", "claude user@host.com AKIAIOSFODNN7EXAMPLE glpat-abcDEF123", 1)
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	for _, k := range []string{"@host.com", "AKIA", "glpat-"} {
		assert.NotContains(t, s, k, "must strip secret/email pattern %q", k)
	}
}

func TestExport_AnonymizationStripsAlnumGluedAbsPath(t *testing.T) {
	// A path root glued directly to an alphanumeric byte (host/etc/passwd, no
	// separator) must still be stripped. scrubAbsPath deliberately PRESERVES an
	// alnum-preceded '/' so provider-prefixed model ids like "anthropic/claude-3"
	// survive; that allowance leaks an embedded absolute path unless an
	// embedded-path-root scrub also runs. Regression guard for the export.go:238 TD.
	rec := exportRec("bruce", "host/etc/passwd node/var/log/secret", 1)
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	for _, p := range []string{"/etc/passwd", "/etc/", "/var/log", "/var/"} {
		assert.NotContains(t, s, p, "must strip alnum-glued absolute path %q", p)
	}
}

func TestExport_PreservesProviderPrefixedModel(t *testing.T) {
	// A provider-prefixed model id carries an internal '/', which is NOT an
	// absolute path and must survive scrubbing.
	data, err := Export([]Record{exportRec("bruce", "anthropic/claude-3", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-3", parseEnvelope(t, data).Records[0].Model)
}

func TestExport_ClampsNegativeMetrics(t *testing.T) {
	// A corrupt-but-parseable record with negative counts must not produce a
	// negative or out-of-range metric in the public submission.
	rec := exportRec("bruce", "m", 1)
	rec.FindingsRaised = -5
	rec.FindingsCorroborated = -2
	rec.TokensIn = -100
	rec.CostUSD = -1.0
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Records[0]
	assert.GreaterOrEqual(t, r.FindingsRaised, 0)
	assert.GreaterOrEqual(t, r.FindingsCorroborated, 0)
	assert.GreaterOrEqual(t, r.TokensIn, 0)
	assert.GreaterOrEqual(t, r.CostUSD, 0.0)
	assert.GreaterOrEqual(t, r.CorroborationRate, 0.0)
	assert.LessOrEqual(t, r.CorroborationRate, 1.0)
}

func TestExport_MetricsPreserved(t *testing.T) {
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Records[0]
	assert.Equal(t, 12, r.FindingsRaised)
	assert.Equal(t, 7, r.FindingsCorroborated)
	assert.Equal(t, 5, r.FindingsSolo)
	assert.InDelta(t, ratio(7, 12), r.CorroborationRate, 1e-9)
	assert.InDelta(t, 0.04, r.CostUSD, 1e-9)
	assert.Equal(t, 14200, r.TokensIn)
	assert.Equal(t, 4000, r.TokensOut)
	assert.Equal(t, int64(9100), r.LatencyMSAvg)
}

func TestExport_ModelPersonaRolePreserved(t *testing.T) {
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Records[0]
	assert.Equal(t, "bruce", r.Reviewer, "persona names are not PII; preserved as-is")
	assert.Equal(t, "claude-sonnet-4-6", r.Model)
	assert.Equal(t, "reviewer", r.Role)
}

func TestExport_VerificationZeroWhenAbsent(t *testing.T) {
	// A record with no verification pointers must serialize zero values (not
	// omitted/null) for the verification metrics (AC 04-03 Scenario 8).
	data, err := Export([]Record{exportRec("bruce", "m", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	var raw []map[string]json.RawMessage
	env := struct {
		Records []map[string]json.RawMessage `json:"records"`
	}{}
	require.NoError(t, json.Unmarshal(data, &env))
	raw = env.Records
	require.Len(t, raw, 1)
	for _, f := range []string{"findings_verified", "findings_refuted", "survived_skeptic_rate"} {
		v, ok := raw[0][f]
		require.True(t, ok, "verification field %q must be present (no omitempty)", f)
		assert.NotEqual(t, "null", string(v), "%q must be a zero value, not null", f)
	}
}

func TestExport_Determinism(t *testing.T) {
	recs := []Record{
		exportRec("bruce", "claude-sonnet-4-6", 1),
		exportRec("alice", "gpt-4o", 2),
		exportRec("bruce", "claude-sonnet-4-6", 3),
	}
	a, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	b, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	assert.Equal(t, a, b, "Export must be byte-identical for identical input")
}

func TestExport_SortedByModelReviewerRole(t *testing.T) {
	recs := []Record{
		exportRec("bruce", "gpt-4", 1),
		exportRec("alice", "claude-sonnet-4-6", 1),
		exportRec("bruce", "claude-sonnet-4-6", 1),
	}
	data, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	recsOut := parseEnvelope(t, data).Records
	require.Len(t, recsOut, 3)
	// (model asc, reviewer asc): claude/alice, claude/bruce, gpt-4/bruce.
	assert.Equal(t, "claude-sonnet-4-6", recsOut[0].Model)
	assert.Equal(t, "alice", recsOut[0].Reviewer)
	assert.Equal(t, "claude-sonnet-4-6", recsOut[1].Model)
	assert.Equal(t, "bruce", recsOut[1].Reviewer)
	assert.Equal(t, "gpt-4", recsOut[2].Model)
	// indices are sequential in sorted order.
	for i, r := range recsOut {
		assert.Equal(t, i, r.Index)
	}
}

func TestExport_AggregatesRunsPerReviewerModel(t *testing.T) {
	recs := []Record{
		exportRec("bruce", "claude-sonnet-4-6", 1),
		exportRec("bruce", "claude-sonnet-4-6", 3),
	}
	data, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	out := parseEnvelope(t, data).Records
	require.Len(t, out, 1, "same (reviewer, model) collapses to one aggregated row")
	assert.Equal(t, 2, out[0].Runs)
	assert.Equal(t, 24, out[0].FindingsRaised, "raised is summed across runs")
}

func TestExport_FiltersApplied(t *testing.T) {
	recs := []Record{
		exportRec("bruce", "claude-sonnet-4-6", 2),
		exportRec("diana", "gpt-4o", 40), // older than 7d window
	}
	data, err := Export(recs, FilterOpts{Since: "7d", Model: "claude-sonnet-4-6"}, fixedExportNow)
	require.NoError(t, err)
	env := parseEnvelope(t, data)
	assert.Equal(t, "7d", env.Filters.Since)
	assert.Equal(t, "claude-sonnet-4-6", env.Filters.Model)
	require.Len(t, env.Records, 1)
	assert.Equal(t, "bruce", env.Records[0].Reviewer)
}

// TestExport_AllFiltersEchoedDistinctly pins the FilterOpts -> ExportFilters
// mapping with a distinct value per field (Persona was previously unasserted).
// Distinct values make a misaligned field mapping (e.g. a future reorder of the
// struct fields) surface as a wrong-field echo instead of a silent pass.
func TestExport_AllFiltersEchoedDistinctly(t *testing.T) {
	recs := []Record{exportRec("bruce", "claude-sonnet-4-6", 1)}
	opts := FilterOpts{Since: "7d", Model: "claude-sonnet-4-6", Persona: "bruce"}
	data, err := Export(recs, opts, fixedExportNow)
	require.NoError(t, err)
	env := parseEnvelope(t, data)
	assert.Equal(t, "7d", env.Filters.Since)
	assert.Equal(t, "claude-sonnet-4-6", env.Filters.Model)
	assert.Equal(t, "bruce", env.Filters.Persona)
}

func TestExport_NoMatchError(t *testing.T) {
	recs := []Record{exportRec("bruce", "claude-sonnet-4-6", 1)}
	_, err := Export(recs, FilterOpts{Since: "30d", Model: "nonexistent"}, fixedExportNow)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoExportRecords), "no-match must surface the sentinel error")
}

func TestExport_NoPIIPatternsInOutput(t *testing.T) {
	recs := []Record{
		exportRec("bruce", "claude-sonnet-4-6", 1),
		exportRec("alice", "gpt-4o", 2),
	}
	data, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	for _, pat := range []string{`(?m)^\s*"[^"]*":\s*"/`, `~/`, `sk-`, `ghp_`, `xoxb-`, `@`} {
		re := regexp.MustCompile(pat)
		assert.False(t, re.MatchString(s), "export output must not match PII pattern %q", pat)
	}
	assert.False(t, strings.Contains(s, "run_id"))
}

func TestAnonymizeRecord_StripsRunIDPreservesMetrics(t *testing.T) {
	v, ref := 4, 1
	rate := 0.8
	raw := Record{
		SchemaVersion:        SchemaVersion,
		RecordType:           RecordTypeReviewer,
		RunID:                "2026-06-15T10:00:00Z-abc123",
		Reviewer:             "bruce",
		Model:                "claude-sonnet-4-6",
		Role:                 "reviewer",
		FindingsRaised:       120,
		FindingsCorroborated: 78,
		FindingsSolo:         42,
		CorroborationRate:    0.65,
		CostUSD:              0.60,
		TokensIn:             213000,
		TokensOut:            60000,
		LatencyMS:            9100,
		FindingsVerified:     &v,
		FindingsRefuted:      &ref,
		SurvivedSkepticRate:  &rate,
	}
	pr := AnonymizeRecord(raw)
	assert.Equal(t, "bruce", pr.Reviewer)
	assert.Equal(t, "claude-sonnet-4-6", pr.Model)
	assert.Equal(t, "reviewer", pr.Role)
	assert.Equal(t, 1, pr.Runs, "a single record anonymizes to runs=1")
	assert.Equal(t, 120, pr.FindingsRaised)
	assert.Equal(t, 78, pr.FindingsCorroborated)
	assert.Equal(t, 42, pr.FindingsSolo)
	assert.InDelta(t, 0.65, pr.CorroborationRate, 1e-9)
	assert.Equal(t, 4, pr.FindingsVerified)
	assert.Equal(t, 1, pr.FindingsRefuted)
	assert.InDelta(t, 0.8, pr.SurvivedSkepticRate, 1e-9)
	assert.Equal(t, 0.60, pr.CostUSD)
	assert.Equal(t, 213000, pr.TokensIn)
	assert.Equal(t, 60000, pr.TokensOut)
	assert.Equal(t, int64(9100), pr.LatencyMSAvg)
}
