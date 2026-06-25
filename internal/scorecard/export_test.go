package scorecard

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedExportNow is a stable reference time for deterministic export tests:
// records are dated relative to it and it is passed to Export as both the
// envelope timestamp and the --since window anchor, so output is reproducible.
var fixedExportNow = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

// exportRec builds a reviewer record dated ageDays before fixedExportNow with a
// full metric set so export aggregation and derivation can be asserted exactly.
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

func TestExport_EnvelopeMatchesSpec(t *testing.T) {
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)

	env := parseEnvelope(t, data)
	assert.Equal(t, SubmissionSchema, env.SubmissionSchema, "submission_schema is the public schema constant")
	assert.Equal(t, 1, env.SubmissionSchema, "spec pins submission_schema to 1")
	assert.Equal(t, version.Version, env.AtcrVersion, "atcr_version comes from internal/version")
	_, perr := time.Parse(time.RFC3339, env.SubmittedAt)
	require.NoError(t, perr, "submitted_at must be RFC3339")
	require.Len(t, env.Reviewers, 1)
	assert.Equal(t, 1, env.Reviewers[0].Runs, "a single source record aggregates to runs=1")
}

// TestExport_EnvelopeKeysAreSpecExact pins the exact top-level and per-reviewer
// JSON keys: no legacy key (schema_version/exported_at/records/filters) may leak,
// and no dropped field (tokens, role, index, the corroborated/solo/verified/
// refuted counts) may appear.
func TestExport_EnvelopeKeysAreSpecExact(t *testing.T) {
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)

	for _, k := range []string{`"submission_schema"`, `"atcr_version"`, `"submitted_at"`, `"reviewers"`} {
		assert.Contains(t, s, k, "envelope must carry spec key %s", k)
	}
	for _, k := range []string{`"model"`, `"persona"`, `"runs"`, `"findings_raised_avg"`,
		`"corroboration_rate"`, `"cost_per_corroborated_finding_usd"`, `"latency_p50_ms"`} {
		assert.Contains(t, s, k, "reviewer record must carry spec key %s", k)
	}
	for _, k := range []string{`"schema_version"`, `"exported_at"`, `"records"`, `"filters"`,
		`"reviewer"`, `"role"`, `"index"`, `"findings_raised"`, `"findings_corroborated"`,
		`"findings_solo"`, `"findings_verified"`, `"findings_refuted"`, `"cost_usd"`,
		`"tokens_in"`, `"tokens_out"`, `"latency_ms_avg"`, `"run_id"`} {
		assert.NotContains(t, s, k, "dropped/legacy key %s must not appear", k)
	}
}

func TestExport_FindingsRaisedAvgIsPerRun(t *testing.T) {
	// Two runs, 12 raised each => average 12.0 (NOT the sum 24).
	recs := []Record{
		exportRec("bruce", "claude-sonnet-4-6", 1),
		exportRec("bruce", "claude-sonnet-4-6", 3),
	}
	data, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	out := parseEnvelope(t, data).Reviewers
	require.Len(t, out, 1, "same (persona, model) collapses to one aggregated row")
	assert.Equal(t, 2, out[0].Runs)
	assert.InDelta(t, 12.0, out[0].FindingsRaisedAvg, 1e-9, "findings_raised_avg is per-run, not the total")
}

func TestExport_CostPerCorroboratedFinding(t *testing.T) {
	// One run: cost 0.04, corroborated 7 => 0.04/7.
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	assert.InDelta(t, 0.04/7.0, r.CostPerCorroboratedFindingUSD, 1e-9)
}

func TestExport_CostPerCorroboratedZeroWhenNoCorroboration(t *testing.T) {
	rec := exportRec("bruce", "m", 1)
	rec.FindingsCorroborated = 0
	rec.CostUSD = 0.5
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	assert.Equal(t, 0.0, r.CostPerCorroboratedFindingUSD, "no corroborated findings => 0, never Inf/NaN")
}

func TestExport_LatencyP50IsMedian(t *testing.T) {
	// Three runs with latencies 100, 9100, 200 => median 200.
	mk := func(age int, lat int64) Record {
		r := exportRec("bruce", "claude-sonnet-4-6", age)
		r.LatencyMS = lat
		return r
	}
	recs := []Record{mk(1, 100), mk(2, 9100), mk(3, 200)}
	data, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	assert.Equal(t, int64(200), r.LatencyP50MS, "p50 is the median of per-run latencies, not the mean")
}

func TestExport_LatencyP50EvenCountAveragesMiddle(t *testing.T) {
	mk := func(age int, lat int64) Record {
		r := exportRec("bruce", "claude-sonnet-4-6", age)
		r.LatencyMS = lat
		return r
	}
	// Four runs: 100, 200, 300, 500 => median (200+300)/2 = 250.
	recs := []Record{mk(1, 100), mk(2, 200), mk(3, 300), mk(4, 500)}
	data, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	assert.Equal(t, int64(250), r.LatencyP50MS)
}

func TestExport_SurvivedSkepticOmittedWhenNoVerification(t *testing.T) {
	// No verification pointers => survived_skeptic_rate key must be omitted entirely.
	data, err := Export([]Record{exportRec("bruce", "m", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "survived_skeptic_rate",
		"absent verification omits the key (not 0.0, not null)")
	r := parseEnvelope(t, data).Reviewers[0]
	assert.Nil(t, r.SurvivedSkepticRate)
}

func TestExport_SurvivedSkepticPresentWhenVerified(t *testing.T) {
	v, ref := 4, 1
	rate := ratio(4, 5)
	rec := exportRec("bruce", "claude-sonnet-4-6", 1)
	rec.FindingsVerified = &v
	rec.FindingsRefuted = &ref
	rec.SurvivedSkepticRate = &rate
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	require.NotNil(t, r.SurvivedSkepticRate, "verification present => rate emitted")
	assert.InDelta(t, 0.8, *r.SurvivedSkepticRate, 1e-9)
}

func TestExport_SurvivedSkepticZeroIsEmittedWhenAllRefuted(t *testing.T) {
	// Verification ran but every finding was refuted: rate is a legitimate 0.0 and
	// must be EMITTED (pointer to 0.0), distinguishable from "no verification" (nil).
	v, ref := 0, 5
	rate := 0.0
	rec := exportRec("bruce", "claude-sonnet-4-6", 1)
	rec.FindingsVerified = &v
	rec.FindingsRefuted = &ref
	rec.SurvivedSkepticRate = &rate
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	assert.Contains(t, string(data), "survived_skeptic_rate", "ran-but-all-refuted emits 0.0, not omit")
	r := parseEnvelope(t, data).Reviewers[0]
	require.NotNil(t, r.SurvivedSkepticRate)
	assert.Equal(t, 0.0, *r.SurvivedSkepticRate)
}

func TestExport_SurvivedSkepticRateOnlyRecordNotZeroed(t *testing.T) {
	// Degenerate (corrupt/externally-supplied) record: a SurvivedSkepticRate
	// pointer is set but the verdict COUNT pointers are nil — a shape the public
	// Record type permits. finalize() must carry the stored rate, not force
	// ratio(0,0)=0 and silently zero a real public value.
	rate := 0.73
	rec := exportRec("bruce", "claude-sonnet-4-6", 1)
	rec.FindingsVerified = nil
	rec.FindingsRefuted = nil
	rec.SurvivedSkepticRate = &rate
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	require.NotNil(t, r.SurvivedSkepticRate, "a stored rate must still be emitted")
	assert.InDelta(t, 0.73, *r.SurvivedSkepticRate, 1e-9, "stored rate must not be zeroed when counts are absent")
}

func TestMedianInt64_EvenCountDoesNotOverflow(t *testing.T) {
	// Two near-MaxInt64 latencies: the naive (a+b)/2 overflows int64 and wraps to a
	// negative wrong answer. The overflow-safe form a+(b-a)/2 must return the true
	// floor-of-average and stay positive.
	a := int64(math.MaxInt64) - 3
	b := int64(math.MaxInt64) - 1
	got := medianInt64([]int64{a, b})
	want := a + (b-a)/2 // MaxInt64-2, computed without the overflowing sum
	assert.Equal(t, want, got, "even-count median must not overflow int64")
	assert.Positive(t, got, "an overflowing sum would flip the median negative")
}

func TestExport_SurvivedSkepticOmittedWhenVerificationRanButNoCountsOrRates(t *testing.T) {
	// Degenerate shape: verification pointers are present (hasVerification) but every
	// verdict count is zero AND no stored rate survives (verified+refuted==0,
	// storedRates empty). There is no rate data, so the key must be OMITTED — not
	// emitted as a misleading 0.0 that is indistinguishable from a genuine
	// all-refuted rate (the verified+refuted>0 case).
	v, ref := 0, 0
	rec := exportRec("bruce", "claude-sonnet-4-6", 1)
	rec.FindingsVerified = &v
	rec.FindingsRefuted = &ref
	rec.SurvivedSkepticRate = nil
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "survived_skeptic_rate",
		"no verdict counts and no stored rate => omit the key, not 0.0")
	r := parseEnvelope(t, data).Reviewers[0]
	assert.Nil(t, r.SurvivedSkepticRate)
}

func TestExport_DistinctIdentitiesMergeWhenTheyScrubEqual(t *testing.T) {
	// By-design invariant: grouping uses the SCRUBBED identity (Export ingestion
	// scrubs persona/model once, then keys by the result). Two records whose
	// Reviewer/Model differ BEFORE scrubbing but scrub to the same value must merge
	// into a single aggregated row. This locks the merge so a future refactor that
	// scrubbed AFTER keying — silently un-merging groups and changing public output —
	// is caught.
	r1 := exportRec("bruce /tmp/secretA", "gpt-4 /var/log/a", 1)
	r2 := exportRec("bruce /tmp/secretB", "gpt-4 /var/log/b", 2)
	data, err := Export([]Record{r1, r2}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	out := parseEnvelope(t, data).Reviewers
	require.Len(t, out, 1, "distinct pre-scrub identities that scrub equal must merge into one group")
	assert.Equal(t, 2, out[0].Runs, "the merged group aggregates both runs")
	assert.Equal(t, "bruce", out[0].Persona, "persona is the scrubbed identity")
	assert.Equal(t, "gpt-4", out[0].Model, "model is the scrubbed identity")
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
	rec := exportRec("bruce", "host/etc/passwd node/var/log/secret", 1)
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	for _, p := range []string{"/etc/passwd", "/etc/", "/var/log", "/var/"} {
		assert.NotContains(t, s, p, "must strip alnum-glued absolute path %q", p)
	}
}

func TestScrubField_ClosesDenylistGaps(t *testing.T) {
	// Backstop hardening (export.go:285): glued absolute paths under additional FHS
	// roots, UNC paths, no-TLD emails, and sk_/AIza credential shapes must all be
	// stripped from the identity backstop, not just the originally-covered set.
	cases := []struct{ in, mustNotContain string }{
		{"node/opt/secret/key", "/opt/"},
		{"host/srv/data/x", "/srv/"},
		{"a/mnt/vol/y", "/mnt/"},
		{"b/root/sshkey", "/root/"},
		{"c/private/keys/z", "/private/"},
		{"d/usr/local/secret", "/usr/"},
		{`\\fileserver\share`, "fileserver"},
		{"admin@localhost", "@localhost"},
		{"sk_live_abc123DEF", "sk_live_"},
		{"AIzaSyABCDEF0123", "AIza"},
	}
	for _, c := range cases {
		got := scrubField("claude " + c.in)
		assert.NotContains(t, got, c.mustNotContain,
			"scrubField must strip %q (from input %q)", c.mustNotContain, c.in)
	}
}

func TestScrubField_PreservesProviderModelAndUnscrubbed(t *testing.T) {
	// The hardened denylist must NOT over-strip a normal provider-prefixed model id
	// or a plain persona name.
	assert.Equal(t, "anthropic/claude-3", scrubField("anthropic/claude-3"))
	assert.Equal(t, "bruce", scrubField("bruce"))
}

func TestExport_PreservesProviderPrefixedModel(t *testing.T) {
	data, err := Export([]Record{exportRec("bruce", "anthropic/claude-3", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-3", parseEnvelope(t, data).Reviewers[0].Model)
}

func TestExport_ClampsNegativeMetrics(t *testing.T) {
	rec := exportRec("bruce", "m", 1)
	rec.FindingsRaised = -5
	rec.FindingsCorroborated = -2
	rec.CostUSD = -1.0
	rec.LatencyMS = -100
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	assert.GreaterOrEqual(t, r.FindingsRaisedAvg, 0.0)
	assert.GreaterOrEqual(t, r.CostPerCorroboratedFindingUSD, 0.0)
	assert.GreaterOrEqual(t, r.LatencyP50MS, int64(0))
	assert.GreaterOrEqual(t, r.CorroborationRate, 0.0)
	assert.LessOrEqual(t, r.CorroborationRate, 1.0)
}

func TestExport_PersonaAndModelPreserved(t *testing.T) {
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	assert.Equal(t, "bruce", r.Persona, "persona names are not PII; preserved as-is")
	assert.Equal(t, "claude-sonnet-4-6", r.Model)
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

func TestExport_SortedByModelPersona(t *testing.T) {
	recs := []Record{
		exportRec("bruce", "gpt-4", 1),
		exportRec("alice", "claude-sonnet-4-6", 1),
		exportRec("bruce", "claude-sonnet-4-6", 1),
	}
	data, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	out := parseEnvelope(t, data).Reviewers
	require.Len(t, out, 3)
	// (model asc, persona asc): claude/alice, claude/bruce, gpt-4/bruce.
	assert.Equal(t, "claude-sonnet-4-6", out[0].Model)
	assert.Equal(t, "alice", out[0].Persona)
	assert.Equal(t, "claude-sonnet-4-6", out[1].Model)
	assert.Equal(t, "bruce", out[1].Persona)
	assert.Equal(t, "gpt-4", out[2].Model)
}

func TestExport_FiltersAppliedButNotEchoed(t *testing.T) {
	recs := []Record{
		exportRec("bruce", "claude-sonnet-4-6", 2),
		exportRec("diana", "gpt-4o", 40), // older than 7d window
	}
	data, err := Export(recs, FilterOpts{Since: "7d", Model: "claude-sonnet-4-6"}, fixedExportNow)
	require.NoError(t, err)
	// Filters select the slice but are NOT echoed (they would leak query params
	// about the user's local dataset).
	assert.NotContains(t, string(data), "filters")
	env := parseEnvelope(t, data)
	require.Len(t, env.Reviewers, 1)
	assert.Equal(t, "bruce", env.Reviewers[0].Persona)
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

func TestAnonymizeRecord_SingleRunDerivedFields(t *testing.T) {
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
		FindingsCorroborated: 80,
		FindingsSolo:         40,
		CorroborationRate:    ratio(80, 120),
		CostUSD:              0.60,
		TokensIn:             213000,
		TokensOut:            60000,
		LatencyMS:            9100,
		FindingsVerified:     &v,
		FindingsRefuted:      &ref,
		SurvivedSkepticRate:  &rate,
	}
	pr := AnonymizeRecord(raw)
	assert.Equal(t, "bruce", pr.Persona)
	assert.Equal(t, "claude-sonnet-4-6", pr.Model)
	assert.Equal(t, 1, pr.Runs, "a single record anonymizes to runs=1")
	assert.InDelta(t, 120.0, pr.FindingsRaisedAvg, 1e-9, "single run: avg == raised")
	assert.InDelta(t, ratio(80, 120), pr.CorroborationRate, 1e-9)
	assert.InDelta(t, 0.60/80.0, pr.CostPerCorroboratedFindingUSD, 1e-9)
	assert.Equal(t, int64(9100), pr.LatencyP50MS)
	require.NotNil(t, pr.SurvivedSkepticRate)
	assert.InDelta(t, 0.8, *pr.SurvivedSkepticRate, 1e-9)
}

func TestClampNonNegF_RejectsNonFinite(t *testing.T) {
	assert.Equal(t, 0.0, clampNonNegF(math.NaN()), "NaN must clamp to 0")
	assert.Equal(t, 0.0, clampNonNegF(math.Inf(1)), "+Inf must clamp to 0")
	assert.Equal(t, 0.0, clampNonNegF(math.Inf(-1)), "-Inf must clamp to 0")
	assert.Equal(t, 5.0, clampNonNegF(5.0), "finite positive passes through")
}

func TestClampRate_RejectsNonFinite(t *testing.T) {
	assert.Equal(t, 0.0, clampRate(math.NaN()), "NaN must clamp to 0")
	assert.Equal(t, 1.0, clampRate(math.Inf(1)), "+Inf must clamp to 1")
	assert.Equal(t, 0.0, clampRate(math.Inf(-1)), "-Inf must clamp to 0")
	assert.Equal(t, 0.5, clampRate(0.5), "finite [0,1] passes through")
}
