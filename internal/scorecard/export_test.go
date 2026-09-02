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

// TestExport_AtcrVersionReflectsLinkTimeOverride proves the envelope's
// atcr_version is sourced live from internal/version rather than a baked-in
// constant: overriding version.Version — as a release `-ldflags "-X ...Version="`
// build does at link time — changes the exported atcr_version. Restores the
// default so other tests still observe the neutral 0.0.0.
func TestExport_AtcrVersionReflectsLinkTimeOverride(t *testing.T) {
	orig := version.Version
	t.Cleanup(func() { version.Version = orig })

	version.Version = "9.9.9-override"
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)

	env := parseEnvelope(t, data)
	assert.Equal(t, "9.9.9-override", env.AtcrVersion,
		"atcr_version must reflect a link-time override of version.Version")
}

func TestExport_EnvelopeMatchesSpec(t *testing.T) {
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)

	env := parseEnvelope(t, data)
	assert.Equal(t, SubmissionSchema, env.SubmissionSchema, "submission_schema is the public schema constant")
	assert.Equal(t, 2, env.SubmissionSchema, "epic 35.16.6.2 bumped submission_schema to 2 when the benchmark envelope gained coverage")
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
	for _, k := range []string{`"suite_case_ids"`, `"reviewer_coverage"`} {
		assert.NotContains(t, s, k,
			"benchmark-only key %s must not leak into the production envelope — the submission_schema 2 bump is additive-only on this producer (docs/scorecard.md)", k)
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
	// One run: cost 0.04, corroborated 7 => 0.04/7, key present (real value).
	data, err := Export([]Record{exportRec("bruce", "claude-sonnet-4-6", 1)},
		FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	require.NotNil(t, r.CostPerCorroboratedFindingUSD, "corroborated findings exist => key must be present")
	assert.InDelta(t, 0.04/7.0, *r.CostPerCorroboratedFindingUSD, 1e-9)
}

func TestExport_CostPerCorroboratedAbsentWhenNoCorroboration(t *testing.T) {
	// Paid but zero corroborated findings: the metric is undefined, so the key
	// must be OMITTED (nil pointer) — never a 0.0 that reads identically to a
	// genuinely free reviewer.
	rec := exportRec("bruce", "m", 1)
	rec.FindingsCorroborated = 0
	rec.CostUSD = 0.5
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	assert.NotContains(t, s, `"cost_per_corroborated_finding_usd"`, "omitempty must drop the key when undefined")
	r := parseEnvelope(t, data).Reviewers[0]
	assert.Nil(t, r.CostPerCorroboratedFindingUSD, "no corroborated findings => key absent, not 0.0")
}

func TestExport_CostPerCorroboratedPresentAndZeroWhenGenuinelyFree(t *testing.T) {
	// A genuinely free reviewer (cost 0) WITH corroborated findings must still
	// carry the key, with value 0.0 — this is the disambiguating case the epic
	// exists for: present-and-zero (free) vs absent (paid, uncorroborated).
	rec := exportRec("bruce", "m", 1)
	rec.CostUSD = 0
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	assert.Contains(t, s, `"cost_per_corroborated_finding_usd"`, "genuinely free reviewer still carries the key")
	r := parseEnvelope(t, data).Reviewers[0]
	require.NotNil(t, r.CostPerCorroboratedFindingUSD)
	assert.Equal(t, 0.0, *r.CostPerCorroboratedFindingUSD)
}

func TestExport_CostPerCorroborated_GroupAggregation(t *testing.T) {
	// Two records in the same group, both with zero corroborated findings:
	// the summed corroborated count is zero, so the key must be omitted.
	rec1 := exportRec("bruce", "m", 1)
	rec1.FindingsCorroborated = 0
	rec1.CostUSD = 0.5
	rec2 := exportRec("bruce", "m", 2)
	rec2.FindingsCorroborated = 0
	rec2.CostUSD = 0.3
	data, err := Export([]Record{rec1, rec2}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	s := string(data)
	assert.NotContains(t, s, `"cost_per_corroborated_finding_usd"`, "group total corroborated=0 omits the key")
	r := parseEnvelope(t, data).Reviewers[0]
	assert.Nil(t, r.CostPerCorroboratedFindingUSD)

	// Mixed group: one paid-but-uncorroborated record plus one free-and-corroborated
	// record. The key must be present and use the GROUP totals, not a single
	// record's fields.
	rec3 := exportRec("otto", "m", 1)
	rec3.FindingsCorroborated = 0
	rec3.CostUSD = 0.5
	rec4 := exportRec("otto", "m", 2)
	rec4.FindingsCorroborated = 7
	rec4.CostUSD = 0
	data, err = Export([]Record{rec3, rec4}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r = parseEnvelope(t, data).Reviewers[0]
	require.NotNil(t, r.CostPerCorroboratedFindingUSD, "mixed group has corroborated>0 => key present")
	assert.InDelta(t, 0.5/7.0, *r.CostPerCorroboratedFindingUSD, 1e-9,
		"cost-per uses group totals (0.5 cost / 7 corroborated), not per-record fields")
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
	if r.CostPerCorroboratedFindingUSD != nil {
		assert.GreaterOrEqual(t, *r.CostPerCorroboratedFindingUSD, 0.0)
	}
	assert.GreaterOrEqual(t, r.LatencyP50MS, int64(0))
	assert.GreaterOrEqual(t, r.CorroborationRate, 0.0)
	assert.LessOrEqual(t, r.CorroborationRate, 1.0)
}

// TestExport_ClampsNegativeMetrics leaves FindingsCorroborated at -2, which clamps
// to 0 at ingestion, so CostPerCorroboratedFindingUSD is always nil there and its
// nil-guarded assertion never exercises the non-nil pointer branch. This test uses a
// positive FindingsCorroborated alongside a negative CostUSD so the clamp is actually
// checked through that branch.
func TestExport_ClampsNegativeCostThroughNonNilCostPer(t *testing.T) {
	rec := exportRec("bruce", "m", 1)
	rec.FindingsCorroborated = 3
	rec.CostUSD = -5.0
	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	require.NotNil(t, r.CostPerCorroboratedFindingUSD, "positive FindingsCorroborated keeps cost-per non-nil")
	assert.GreaterOrEqual(t, *r.CostPerCorroboratedFindingUSD, 0.0, "negative CostUSD must clamp to non-negative through the non-nil pointer branch")
}

func TestExport_CostPerCorroboratedFinding_ClampsOverflowingTotal(t *testing.T) {
	r1 := exportRec("bruce", "m", 1)
	r1.CostUSD = math.MaxFloat64
	r2 := exportRec("bruce", "m", 2)
	r2.CostUSD = math.MaxFloat64
	data, err := Export([]Record{r1, r2}, FilterOpts{Since: "30d"}, fixedExportNow)
	require.NoError(t, err)
	r := parseEnvelope(t, data).Reviewers[0]
	require.NotNil(t, r.CostPerCorroboratedFindingUSD)
	assert.False(t, math.IsInf(*r.CostPerCorroboratedFindingUSD, 0), "cost-per must stay finite even when the group's summed cost overflows float64")
	assert.False(t, math.IsNaN(*r.CostPerCorroboratedFindingUSD))
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
	require.NotNil(t, pr.CostPerCorroboratedFindingUSD)
	assert.InDelta(t, 0.60/80.0, *pr.CostPerCorroboratedFindingUSD, 1e-9)
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

// TestRunLeaderboardExport_SemanticContract is the AC 03-03 safety net: it pins the
// SEMANTIC shape of scorecard.Export's output for a fixed, representative fixture —
// (persona,model) aggregation, the sort order, per-group aggregate math, the
// verification block's presence-when-data-exists, and the absence of the telemetry
// persona_id_hash — so Story 3's hashing path cannot weaken, bypass, or extend the
// Epic 10.0 public leaderboard export. Replaces an opaque byte-for-byte SHA-256
// checksum that failed on any formatting/ordering change and hid the actual contract.
func TestRunLeaderboardExport_SemanticContract(t *testing.T) {
	carol := exportRec("carol", "claude-opus-4-6", 1)
	fv, fr := 3, 1
	ssr := 0.75
	carol.FindingsVerified = &fv
	carol.FindingsRefuted = &fr
	carol.SurvivedSkepticRate = &ssr

	recs := []Record{
		exportRec("bruce", "claude-sonnet-4-6", 1),
		exportRec("alice", "gpt-4o", 2),
		exportRec("bruce", "claude-sonnet-4-6", 3), // same (persona,model) as row 1: aggregates
		carol,
	}
	data, err := Export(recs, FilterOpts{Since: "365d"}, fixedExportNow)
	require.NoError(t, err)

	env := parseEnvelope(t, data)

	// (1) Group count + membership by (persona,model): bruce's two runs collapse into
	// one group; alice and carol each form their own — three reviewer rows total.
	require.Len(t, env.Reviewers, 3, "distinct (persona,model) pairs aggregate to 3 reviewer rows")

	// (3) Sort order: ascending by (Model, Persona) — claude-opus < claude-sonnet < gpt.
	type pm struct{ model, persona string }
	gotOrder := make([]pm, 0, len(env.Reviewers))
	for _, r := range env.Reviewers {
		gotOrder = append(gotOrder, pm{r.Model, r.Persona})
	}
	assert.Equal(t, []pm{
		{"claude-opus-4-6", "carol"},
		{"claude-sonnet-4-6", "bruce"},
		{"gpt-4o", "alice"},
	}, gotOrder, "reviewers must be sorted ascending by (Model, Persona)")

	byPersona := map[string]PublicRecord{}
	for _, r := range env.Reviewers {
		byPersona[r.Persona] = r
	}

	// (2) Per-group aggregate math. bruce aggregates two identical runs; alice/carol one.
	assert.Equal(t, 2, byPersona["bruce"].Runs, "bruce's two same-key runs aggregate to runs=2")
	assert.Equal(t, 1, byPersona["alice"].Runs)
	assert.Equal(t, 1, byPersona["carol"].Runs)
	for _, p := range []string{"bruce", "alice", "carol"} {
		r := byPersona[p]
		assert.InDelta(t, 12.0, r.FindingsRaisedAvg, 1e-9, "%s: avg raised per run", p)
		assert.InDelta(t, ratio(7, 12), r.CorroborationRate, 1e-9, "%s: corroboration is ratio-of-totals", p)
		assert.Equal(t, int64(9100), r.LatencyP50MS, "%s: p50 latency", p)
		require.NotNil(t, r.CostPerCorroboratedFindingUSD, "%s: cost-per-corroborated present", p)
	}
	assert.InDelta(t, (2*0.04)/(2*7), *byPersona["bruce"].CostPerCorroboratedFindingUSD, 1e-9,
		"bruce cost-per-corroborated is summed cost over summed corroborated findings")
	assert.InDelta(t, 0.04/7, *byPersona["alice"].CostPerCorroboratedFindingUSD, 1e-9)

	// (4) Verification block: only carol carries verification data, so only carol's
	// survived_skeptic_rate is present; bruce/alice omit it (nil pointer → omitempty).
	require.NotNil(t, byPersona["carol"].SurvivedSkepticRate, "carol has verification data")
	assert.InDelta(t, 0.75, *byPersona["carol"].SurvivedSkepticRate, 1e-9)
	assert.Nil(t, byPersona["bruce"].SurvivedSkepticRate, "no verification data → survived_skeptic_rate omitted")
	assert.Nil(t, byPersona["alice"].SurvivedSkepticRate, "no verification data → survived_skeptic_rate omitted")

	// (5) The telemetry persona hash must never leak onto the PUBLIC leaderboard schema.
	assert.NotContains(t, string(data), "persona_id_hash",
		"the public leaderboard export must not carry the telemetry persona_id_hash")

	// (6) Empty input still returns ErrNoExportRecords with the same identity.
	_, eerr := Export(nil, FilterOpts{Since: "365d"}, fixedExportNow)
	assert.ErrorIs(t, eerr, ErrNoExportRecords)
}

// TestExport_UsesOneDenominatorEra pins that a public submission never averages
// the two FindingsRaised definitions together.
//
// Epic 35.16.6.5 put the Tier-4-routed findings into the denominator. Export
// applies no consensus_level or era filter at all — unlike TrustPriors — so a
// submission spanning the change reported a corroboration_rate and a
// findings_raised_avg computed from two incompatible counts, under a
// submission_schema integer that did not move. The same prefer-current rule
// TrustPriors uses applies here: when the set holds any current-era record, only
// those count; when it holds none, the older records are used unchanged so an
// existing store still exports.
func TestExport_UsesOneDenominatorEra(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	recs := []Record{
		// Pre-epic: phantoms were not in the denominator, so a flattering 1.00.
		{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer,
			RunID: "2026-07-01T00:00:00Z-old", Reviewer: "bruce", Model: "m",
			FindingsRaised: 1, FindingsCorroborated: 1,
		},
		// Current era: the same reviewer with its phantoms counted.
		{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer,
			RunID: "2026-07-02T00:00:00Z-new", Reviewer: "bruce", Model: "m",
			RaisedIncludesUnresolved: true,
			FindingsRaised:           4, FindingsCorroborated: 1,
		},
	}

	out, err := Export(recs, FilterOpts{}, base.AddDate(0, 0, 1))
	require.NoError(t, err)
	env := parseEnvelope(t, out)
	require.Len(t, env.Reviewers, 1)
	assert.InDelta(t, 0.25, env.Reviewers[0].CorroborationRate, 0.0001,
		"only the current-era record may contribute; blending the two gives 0.4")
	assert.InDelta(t, 4.0, env.Reviewers[0].FindingsRaisedAvg, 0.0001,
		"findings_raised_avg carries the same exposure and must use the same set")
}

// TestExport_PreUnresolvedOnlyStoreStillExports pins the other half: a store that
// predates the change entirely still produces a submission, unchanged. Dropping
// those records outright would silently empty an existing submitter's export.
func TestExport_PreUnresolvedOnlyStoreStillExports(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	recs := []Record{{
		SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer,
		RunID: "2026-07-01T00:00:00Z-old", Reviewer: "bruce", Model: "m",
		FindingsRaised: 2, FindingsCorroborated: 1,
	}}

	out, err := Export(recs, FilterOpts{}, base.AddDate(0, 0, 1))
	require.NoError(t, err)
	env := parseEnvelope(t, out)
	require.Len(t, env.Reviewers, 1)
	assert.InDelta(t, 0.5, env.Reviewers[0].CorroborationRate, 0.0001)
}

// TestExport_EraFilterDoesNotDropOtherReviewers pins the export half of the
// per-reviewer era rule.
//
// The era partition was applied to the whole record slice at once, so ONE
// reviewer carrying a current-era record truncated the public submission to that
// reviewer alone. A store with eleven reviewers and a hundred runs each published
// one reviewer at runs:1, and nothing in ExportEnvelope marked the truncation — no
// record count, and submission_schema does not move — so a board consumer read the
// missing reviewers as never used, indistinguishable from a submitter that
// genuinely never ran them.
//
// Epic 35.16.6.8 added raised_denominator to each public row, which is NOT a fix
// for this: it says which definition produced a row's numbers, not that rows are
// missing. The partition still has to be per-reviewer, and this test is still what
// holds it that way.
//
// Export is a SEPARATE consumer from TrustPriors and needs its own guard: the two
// share the helper today, and a future change that re-widens the partition for one
// of them must fail here too.
func TestExport_EraFilterDoesNotDropOtherReviewers(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	recs := []Record{
		// bruce: pre-epic only, and it has not run since the upgrade.
		{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer,
			RunID: "2026-07-01T00:00:00Z-bruce", Reviewer: "bruce", Model: "m",
			FindingsRaised: 2, FindingsCorroborated: 1,
		},
		// greta: one current-era record — enough to trip the global partition.
		{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer,
			RunID: "2026-07-02T00:00:00Z-greta", Reviewer: "greta", Model: "m",
			RaisedIncludesUnresolved: true,
			FindingsRaised:           4, FindingsCorroborated: 1,
		},
	}

	out, err := Export(recs, FilterOpts{}, base.AddDate(0, 0, 1))
	require.NoError(t, err)
	env := parseEnvelope(t, out)

	require.Len(t, env.Reviewers, 2,
		"greta crossing into the current era must not erase bruce from the submission")
	byPersona := map[string]PublicRecord{}
	for _, r := range env.Reviewers {
		byPersona[r.Persona] = r
	}
	require.Contains(t, byPersona, "bruce")
	assert.InDelta(t, 0.5, byPersona["bruce"].CorroborationRate, 0.0001,
		"bruce's own records are all pre-epic and internally consistent, so they are used unchanged")
	assert.InDelta(t, 0.25, byPersona["greta"].CorroborationRate, 0.0001)
}

// TestExport_EraFilterIsIndependentOfTheUserFilters pins the second symptom of the
// global partition: because the era rule runs AFTER ApplyFilters, its scope used
// to depend on what the user's own flags happened to select. `--persona bruce`
// took the fallback path (no current-era record in that slice) and reported
// bruce's full history, while the unfiltered export dropped bruce entirely — two
// invocations of the same command disagreeing about the same reviewer. With the
// partition keyed per reviewer, the two must agree.
func TestExport_EraFilterIsIndependentOfTheUserFilters(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	recs := []Record{
		{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer,
			RunID: "2026-07-01T00:00:00Z-bruce", Reviewer: "bruce", Model: "m",
			FindingsRaised: 2, FindingsCorroborated: 1,
		},
		{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer,
			RunID: "2026-07-02T00:00:00Z-greta", Reviewer: "greta", Model: "m",
			RaisedIncludesUnresolved: true,
			FindingsRaised:           4, FindingsCorroborated: 1,
		},
	}
	at := base.AddDate(0, 0, 1)

	scopedOut, err := Export(recs, FilterOpts{Persona: "bruce"}, at)
	require.NoError(t, err)
	scoped := parseEnvelope(t, scopedOut)
	require.Len(t, scoped.Reviewers, 1)

	wholeOut, err := Export(recs, FilterOpts{}, at)
	require.NoError(t, err)
	whole := parseEnvelope(t, wholeOut)

	var bruceWhole PublicRecord
	for _, r := range whole.Reviewers {
		if r.Persona == "bruce" {
			bruceWhole = r
		}
	}
	assert.Equal(t, scoped.Reviewers[0].CorroborationRate, bruceWhole.CorroborationRate,
		"the same reviewer must report the same rate whether or not the user narrowed the export")
	assert.Equal(t, scoped.Reviewers[0].Runs, bruceWhole.Runs,
		"and the same sample size — a filter must not change which of a reviewer's records count")
}

// ExportSelected is EXPORTED API that skips ApplyFilters entirely, and ApplyFilters is
// the only place a non-reviewer record is dropped on the export path. A caller handing
// it raw store records would otherwise publish an aggregate row as a reviewer row in
// the public envelope — the aggregate sums every reviewer's findings, so it would enter
// the leaderboard as a phantom reviewer outscoring the real ones. The sole live caller
// passes PublishedSet output, so nothing depends on the looseness; the guard makes the
// invariant the package's, not the caller's.
func TestExportSelected_DropsNonReviewerRecords(t *testing.T) {
	recs := []Record{
		{
			SchemaVersion: 1, RecordType: RecordTypeReviewer, RunID: "2026-06-15T00:00:00Z-a",
			Reviewer: "greta", Model: "claude-sonnet", FindingsRaised: 3, FindingsCorroborated: 2,
		},
		{
			SchemaVersion: 1, RecordType: RecordTypeAggregate, RunID: "2026-06-15T00:00:00Z-a",
			Reviewer: "", Model: "", FindingsRaised: 99, FindingsCorroborated: 99,
		},
	}

	data, err := ExportSelected(recs, fixedExportNow)
	require.NoError(t, err)

	var env ExportEnvelope
	require.NoError(t, json.Unmarshal(data, &env))
	require.Len(t, env.Reviewers, 1, "the aggregate row must not reach the envelope")
	assert.Equal(t, "claude-sonnet", env.Reviewers[0].Model)
	assert.Equal(t, "greta", env.Reviewers[0].Persona)
}

// A selection of nothing but non-reviewer records has nothing publishable in it, and
// must read as the ordinary no-records error rather than an envelope carrying one
// empty-identity row built from the aggregates.
func TestExportSelected_OnlyNonReviewerRecordsIsNoRecords(t *testing.T) {
	recs := []Record{
		{
			SchemaVersion: 1, RecordType: RecordTypeAggregate, RunID: "2026-06-15T00:00:00Z-a",
			FindingsRaised: 99, FindingsCorroborated: 99,
		},
	}
	_, err := ExportSelected(recs, fixedExportNow)
	require.ErrorIs(t, err, ErrNoExportRecords)
}

// TestExport_CarriesTheRaisedDenominator pins the cross-submitter half of the era
// problem, which the per-store prefer-newest rule does not touch.
//
// unresolvedEraRuns guarantees a single submission is computed under ONE
// definition. It says nothing about two submissions: submitter A on an old atcr
// publishes bruce at 1.00, submitter B on a new one publishes bruce at 0.60, both
// stamped submission_schema 2, and the board ranks them against each other. The
// envelope has to say which rule produced the number.
func TestExport_CarriesTheRaisedDenominator(t *testing.T) {
	t.Run("current-era records publish the current definition", func(t *testing.T) {
		rec := exportRec("bruce", "claude-sonnet-4-6", 1)
		rec.RaisedDenominator = RaisedDenominatorCurrent
		rec.RaisedIncludesUnresolved = true

		out, err := Export([]Record{rec}, FilterOpts{Since: "all"}, fixedExportNow)
		require.NoError(t, err)
		env := parseEnvelope(t, out)
		require.Len(t, env.Reviewers, 1)
		assert.Equal(t, RaisedDenominatorCurrent, env.Reviewers[0].RaisedDenominator)
	})

	t.Run("a pre-epic store still publishes, labelled as what it is", func(t *testing.T) {
		// The key is NOT omitempty, so this row says "definition 1" out loud
		// rather than staying silent — silence is the ambiguity being removed.
		out, err := Export([]Record{exportRec("greta", "gpt-5", 1)},
			FilterOpts{Since: "all"}, fixedExportNow)
		require.NoError(t, err)
		env := parseEnvelope(t, out)
		require.Len(t, env.Reviewers, 1)
		assert.Equal(t, 1, env.Reviewers[0].RaisedDenominator,
			"an unmarked record is the pre-epic definition, and the envelope must say so")
		assert.Contains(t, string(out), `"raised_denominator"`,
			"the key is never omitted: a submission that will not say which rule it used is the defect")
	})

	t.Run("a corrupt version cannot delete a reviewer's real history", func(t *testing.T) {
		// The store is a plain JSONL file a user can edit. An out-of-range version
		// would otherwise win prefer-newest outright and become that reviewer's
		// only cohort — one bad line silently discarding every genuine record.
		// EXCLUSION keeps the intent without the clamp's defect: the garbage line
		// is dropped from the era window entirely instead of being re-labelled
		// current and blended — the same answer for a corrupt 999 and for a
		// legitimate record written by a newer binary under a definition this one
		// does not implement.
		good := exportRec("bruce", "claude-sonnet-4-6", 1)
		good.RaisedIncludesUnresolved = true
		good.RaisedDenominator = RaisedDenominatorCurrent
		corrupt := exportRec("bruce", "claude-sonnet-4-6", 1)
		corrupt.RaisedIncludesUnresolved = true
		corrupt.RaisedDenominator = 999

		out, err := Export([]Record{good, corrupt}, FilterOpts{Since: "all"}, fixedExportNow)
		require.NoError(t, err)
		env := parseEnvelope(t, out)
		require.Len(t, env.Reviewers, 1)
		assert.Equal(t, 1, env.Reviewers[0].Runs,
			"the corrupt record is excluded from the era window, so only the good record's run is counted")
		assert.Equal(t, RaisedDenominatorCurrent, env.Reviewers[0].RaisedDenominator,
			"and the published era is the genuine current definition, never the corrupt value")
	})

	t.Run("the two 'included' eras are separable, which a bool could not do", func(t *testing.T) {
		// Both of these stamp RaisedIncludesUnresolved=true. Before the
		// denominator version existed they were indistinguishable, so a window
		// holding both blended two definitions while reporting one.
		older := exportRec("bruce", "claude-sonnet-4-6", 1)
		older.RaisedIncludesUnresolved = true // era 2: every routed finding charged
		newer := exportRec("bruce", "claude-sonnet-4-6", 1)
		newer.RaisedIncludesUnresolved = true
		newer.RaisedDenominator = RaisedDenominatorCurrent // era 3: doc-shielded carved out

		out, err := Export([]Record{older, newer}, FilterOpts{Since: "all"}, fixedExportNow)
		require.NoError(t, err)
		env := parseEnvelope(t, out)
		require.Len(t, env.Reviewers, 1)
		assert.Equal(t, RaisedDenominatorCurrent, env.Reviewers[0].RaisedDenominator)
		assert.Equal(t, 1, env.Reviewers[0].Runs,
			"prefer-newest must drop the older definition's run rather than average across both")
	})

	t.Run("ExportSelected runs the era pass internally: mixed-era input is separated, not blended", func(t *testing.T) {
		// ExportSelected is exported precisely so an embedder can aggregate a slice
		// directly. The single-definition guarantee is therefore structural INSIDE
		// it: mixed-era records for one reviewer are split prefer-newest before
		// aggregation, exactly as PublishedSet does on the Export path.
		era2 := exportRec("bruce", "claude-sonnet-4-6", 1)
		era2.RaisedIncludesUnresolved = true // denominator 2 via the bool fallback
		era2.FindingsRaised = 10
		era2.FindingsCorroborated = 9
		era3 := exportRec("bruce", "claude-sonnet-4-6", 1)
		era3.RaisedIncludesUnresolved = true
		era3.RaisedDenominator = RaisedDenominatorCurrent
		era3.FindingsRaised = 4
		era3.FindingsCorroborated = 1

		out, err := ExportSelected([]Record{era2, era3}, fixedExportNow)
		require.NoError(t, err)
		env := parseEnvelope(t, out)
		require.Len(t, env.Reviewers, 1)
		assert.Equal(t, 1, env.Reviewers[0].Runs,
			"the era-2 record must be dropped prefer-newest, not blended into the published row")
		assert.Equal(t, RaisedDenominatorCurrent, env.Reviewers[0].RaisedDenominator)
		assert.InDelta(t, 0.25, env.Reviewers[0].CorroborationRate, 1e-9,
			"the rate is computed from the era-3 record alone (1/4), not the blend (10/14)")
	})
}
