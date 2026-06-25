package scorecard

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/version"
)

// ErrNoExportRecords is returned by Export when no record survives the filters
// (or the store is empty). The CLI maps it to exit 1 and prints the canonical
// user-facing guidance (AC 04-04); callers write no output on this path.
var ErrNoExportRecords = errors.New("no records match the export filters")

// SubmissionSchema is the version of the PUBLIC leaderboard submission format
// (Epic 10.0). It is intentionally decoupled from the on-disk store's
// SchemaVersion: the local record format and the public submission format evolve
// independently, so bumping one must not silently change the other.
const SubmissionSchema = 1

// PublicRecord is one aggregated reviewer row of the public submission schema,
// matching the Epic 10.0 spec JSON exactly. It is an allowlist: only these fields
// are ever emitted, so anything not here (run_id, paths, hostnames, keys, raw
// token/cost/count internals) cannot leak. SurvivedSkepticRate is the sole
// omitempty field — a nil pointer omits the key, which distinguishes "no
// verification ran for this group" (key absent) from "verification ran and every
// finding was refuted" (key present with value 0.0).
type PublicRecord struct {
	Model                         string   `json:"model"`
	Persona                       string   `json:"persona"`
	Runs                          int      `json:"runs"`
	FindingsRaisedAvg             float64  `json:"findings_raised_avg"`
	CorroborationRate             float64  `json:"corroboration_rate"`
	SurvivedSkepticRate           *float64 `json:"survived_skeptic_rate,omitempty"`
	CostPerCorroboratedFindingUSD float64  `json:"cost_per_corroborated_finding_usd"`
	LatencyP50MS                  int64    `json:"latency_p50_ms"`
}

// ExportEnvelope is the top-level public submission document (Epic 10.0 spec).
// The active filters are deliberately NOT echoed: they would leak query
// parameters about the submitter's local dataset, and a public submission is
// defined as an aggregate over the selected slice, not a description of the query.
type ExportEnvelope struct {
	SubmissionSchema int            `json:"submission_schema"`
	AtcrVersion      string         `json:"atcr_version"`
	SubmittedAt      string         `json:"submitted_at"`
	Reviewers        []PublicRecord `json:"reviewers"`
}

// reviewerAcc accumulates the raw per-run inputs for one (persona, model) group.
// The public per-reviewer metrics are derived ONLY at finalize() time: an average
// (findings_raised_avg), a median (latency_p50_ms), and a ratio-of-totals
// (cost_per_corroborated, corroboration_rate) cannot be composed from per-run
// PublicRecords without bias, so aggregation works from raw records and the
// public row is computed once at the end. persona/model are scrubbed at ingestion
// (the single anonymization point), so finalize() never re-scrubs.
type reviewerAcc struct {
	persona         string
	model           string
	runs            int
	raisedTotal     int
	corroborated    int
	costTotal       float64
	latencies       []int64
	verified        int
	refuted         int
	storedRates     []float64
	hasVerification bool
}

func (a *reviewerAcc) add(r Record) {
	a.runs++
	a.raisedTotal += clampNonNeg(r.FindingsRaised)
	a.corroborated += clampNonNeg(r.FindingsCorroborated)
	a.costTotal += clampNonNegF(r.CostUSD)
	a.latencies = append(a.latencies, clampNonNeg64(r.LatencyMS))
	// Any verification pointer present marks the group as verified; the counts
	// sum only over the runs that actually carried verification data.
	if r.FindingsVerified != nil {
		a.verified += clampNonNeg(*r.FindingsVerified)
		a.hasVerification = true
	}
	if r.FindingsRefuted != nil {
		a.refuted += clampNonNeg(*r.FindingsRefuted)
		a.hasVerification = true
	}
	if r.SurvivedSkepticRate != nil {
		a.storedRates = append(a.storedRates, clampRate(*r.SurvivedSkepticRate))
		a.hasVerification = true
	}
}

func (a *reviewerAcc) finalize() PublicRecord {
	pr := PublicRecord{
		Model:                         a.model,
		Persona:                       a.persona,
		Runs:                          a.runs,
		FindingsRaisedAvg:             avgPerRun(a.raisedTotal, a.runs),
		CorroborationRate:             clampRate(ratio(a.corroborated, a.raisedTotal)),
		CostPerCorroboratedFindingUSD: costPer(a.costTotal, a.corroborated),
		LatencyP50MS:                  medianInt64(a.latencies),
	}
	// Emit the rate ONLY when real verdict data backs it. hasVerification merely
	// records that some verification pointer was present; a degenerate record can
	// carry zero counts AND no stored rate (verified+refuted==0, storedRates empty),
	// in which case there is no rate to report and the key must stay absent — a 0.0
	// here would be indistinguishable from a genuine all-refuted rate.
	if a.hasVerification && (a.verified+a.refuted > 0 || len(a.storedRates) > 0) {
		// Count-based aggregation (verified/(verified+refuted)) is authoritative
		// when verdict counts are present — AC 01-05 EC3 (rate from totals, not an
		// average of per-run rates). Only when NO counts survive in the group (a
		// corrupt/partial record carrying a rate pointer but nil count pointers, a
		// shape the public Record type permits) do we fall back to the mean of the
		// stored rates, rather than forcing ratio(0,0)=0 and silently zeroing a real
		// public value.
		var rate float64
		if a.verified+a.refuted > 0 {
			rate = clampRate(ratio(a.verified, a.verified+a.refuted))
		} else {
			sum := 0.0
			for _, v := range a.storedRates {
				sum += v
			}
			rate = clampRate(sum / float64(len(a.storedRates)))
		}
		pr.SurvivedSkepticRate = &rate
	}
	return pr
}

// AnonymizeRecord maps one internal Record to a single-run PublicRecord, scrubbing
// the persona/model identities and deriving the public metrics as if the record
// were a one-run group (avg == its raised, p50 == its latency, cost-per ==
// cost/corroborated). It shares the reviewerAcc finalize path so the allowlist
// and the derivation math live in exactly one place.
func AnonymizeRecord(raw Record) PublicRecord {
	a := reviewerAcc{persona: scrubField(raw.Reviewer), model: scrubField(raw.Model)}
	a.add(raw)
	return a.finalize()
}

// ScrubPublicRecord re-applies the identity-field scrub to the two string fields
// a PublicRecord carries (Model, Persona). It exists for callers that wrap an
// externally-supplied PublicRecord into a public artifact WITHOUT having gone
// through AnonymizeRecord's ingestion scrub — notably benchmark.BuildSubmission,
// which consumes a hand-suppliable run-result file. The numeric metrics are
// governed by the PublicRecord allowlist and are left untouched. Idempotent: a
// record already scrubbed at ingestion passes through unchanged.
func ScrubPublicRecord(r PublicRecord) PublicRecord {
	r.Model = scrubField(r.Model)
	r.Persona = scrubField(r.Persona)
	return r
}

// clampNonNeg* / clampRate guard the public submission against a corrupt-but-
// parseable source record: a negative count or an out-of-[0,1] rate in a public
// leaderboard is worse than a dropped value, so ingested metrics are bounded
// before they reach aggregation or serialization.
func clampNonNeg(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func clampNonNeg64(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

func clampNonNegF(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0
	}
	return f
}

func clampRate(f float64) float64 {
	switch {
	case math.IsNaN(f):
		return 0
	case math.IsInf(f, 1):
		return 1
	case math.IsInf(f, -1):
		return 0
	case f < 0:
		return 0
	case f > 1:
		return 1
	default:
		return f
	}
}

// Export filters, anonymizes, aggregates, and serializes the scorecard records
// into a deterministic public submission document. Filters are applied BEFORE
// anonymization (AC 04-04). Records are aggregated by (persona, model) — role is
// dropped from the public schema and is a constant ("reviewer") for reconcile
// records anyway — then sorted ascending by (model, persona), so the same input
// and exportedAt always produce byte-identical output. exportedAt is used both as
// the envelope timestamp and as the --since window anchor, so the document is
// fully reproducible (no hidden time.Now()).
func Export(records []Record, opts FilterOpts, exportedAt time.Time) ([]byte, error) {
	filtered, err := ApplyFilters(records, opts, exportedAt)
	if err != nil {
		return nil, err
	}
	if len(filtered) == 0 {
		return nil, ErrNoExportRecords
	}

	type key struct{ persona, model string }
	groups := map[key]*reviewerAcc{}
	order := make([]key, 0)

	for _, r := range filtered {
		// Scrub once, at ingestion: keying and storage use the scrubbed identity,
		// so finalize() never re-scrubs and two records that scrub to the same
		// identity merge into one group.
		persona := scrubField(r.Reviewer)
		model := scrubField(r.Model)
		k := key{persona, model}
		a, ok := groups[k]
		if !ok {
			a = &reviewerAcc{persona: persona, model: model}
			groups[k] = a
			order = append(order, k)
		}
		a.add(r)
	}

	rows := make([]PublicRecord, 0, len(order))
	for _, k := range order {
		rows = append(rows, groups[k].finalize())
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Model != rows[j].Model {
			return rows[i].Model < rows[j].Model
		}
		return rows[i].Persona < rows[j].Persona
	})

	env := ExportEnvelope{
		SubmissionSchema: SubmissionSchema,
		AtcrVersion:      version.Version,
		SubmittedAt:      exportedAt.UTC().Format(time.RFC3339),
		Reviewers:        rows,
	}
	return json.MarshalIndent(env, "", "  ")
}

// avgPerRun returns total/runs as a float, or 0.0 when runs == 0.
func avgPerRun(total, runs int) float64 {
	if runs <= 0 {
		return 0
	}
	return float64(total) / float64(runs)
}

// costPer returns total cost divided by the corroborated-finding count, or 0.0
// when there are no corroborated findings (the metric is undefined; 0 is the
// documented sentinel, never Inf/NaN).
func costPer(totalCost float64, corroborated int) float64 {
	if corroborated <= 0 {
		return 0
	}
	return totalCost / float64(corroborated)
}

// medianInt64 returns the p50 (median) of the latencies: the middle element for
// an odd count, and for an even count the integer median — the FLOOR of the
// average of the two middle elements (p50 is an int64 millisecond field, so it is
// reported as a whole millisecond, never rounded up). Empty slice returns 0. The
// input is copied before sorting so the caller's accumulator order is not mutated.
func medianInt64(xs []int64) int64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	sorted := make([]int64, n)
	copy(sorted, xs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if n%2 == 1 {
		return sorted[n/2]
	}
	// lo + (hi-lo)/2 is the overflow-safe form of (lo+hi)/2: it never sums two
	// near-MaxInt64 values, while still yielding floor((lo+hi)/2) since hi >= lo.
	lo, hi := sorted[n/2-1], sorted[n/2]
	return lo + (hi-lo)/2
}

// scrubField is defense-in-depth over the allowlist: reviewer/model/role are the
// only string fields the public schema carries, and they are controlled internal
// identities, but a crafted or corrupt source record could embed PII in them, so
// path-like, email, and credential-like substrings are stripped before emission.
// The allowlist (PublicRecord) is the primary guarantee — this is the backstop.
//
// An absolute path is a '/'-run whose '/' is NOT immediately preceded by an
// alphanumeric: that strips "/Users/sam", "=/etc/passwd", and "x:/tmp" while
// preserving a provider-prefixed model id like "anthropic/claude-3" (the '/' sits
// after an alnum). Windows paths and '~'-runs are stripped anywhere; the
// credential denylist covers common token/key shapes (it is necessarily
// incomplete — hence the allowlist as the real boundary). Whitespace is then
// collapsed.
func scrubField(s string) string {
	s = scrubWinPath.ReplaceAllString(s, "")
	s = scrubHome.ReplaceAllString(s, "")
	s = scrubEmbeddedPath.ReplaceAllString(s, "")
	s = scrubAbsPath.ReplaceAllString(s, "$1")
	s = scrubEmail.ReplaceAllString(s, "")
	s = scrubKey.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

var (
	scrubWinPath = regexp.MustCompile(`[A-Za-z]:\\\S*`)
	scrubHome    = regexp.MustCompile(`~\S*`)
	// scrubEmbeddedPath removes a whole whitespace-delimited token that embeds a
	// known absolute-path root, even when the '/' is glued to a preceding
	// alphanumeric (e.g. "host/etc/passwd"). scrubAbsPath below deliberately keeps
	// an alnum-preceded '/' so provider-prefixed model ids like "anthropic/claude-3"
	// survive — but that allowance would otherwise leak an embedded system path. A
	// real path root never appears in a model id, so dropping the token is safe.
	scrubEmbeddedPath = regexp.MustCompile(`\S*(?:/etc/|/Users/|/home/|/var/|/tmp/)\S*`)
	// Leading capture preserves the non-path byte before the stripped '/'-run.
	scrubAbsPath = regexp.MustCompile(`(^|[^A-Za-z0-9])/\S*`)
	scrubEmail   = regexp.MustCompile(`[\w.+-]+@[\w.-]+\.\w+`)
	scrubKey     = regexp.MustCompile(`(?i)\b(sk-[a-z0-9-]+|ghp_\w+|gho_\w+|ghu_\w+|ghs_\w+|ghr_\w+|github_pat_\w+|glpat-\S+|xox[baprs]-\S+|xapp-\S+|akia[a-z0-9]{16}|asia[a-z0-9]+|bearer\s+\S+|(?:authorization|api[_-]?key|token)\s*[:=]\s*\S+)`)
)
