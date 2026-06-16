package scorecard

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrNoExportRecords is returned by Export when no record survives the filters
// (or the store is empty). The CLI maps it to exit 1 and prints the canonical
// user-facing guidance (AC 04-04); callers write no output on this path.
var ErrNoExportRecords = errors.New("no records match the export filters")

// PublicRecord is one aggregated row of the v1 public submission schema. It is an
// allowlist: only these fields are ever emitted, so a field that is not here
// (run_id, paths, hostnames, keys) cannot leak. No field carries `omitempty` —
// an absent numeric field would itself encode information, so every field is
// always present, zero values included (AC 04-03).
type PublicRecord struct {
	Index                int     `json:"index"`
	Reviewer             string  `json:"reviewer"`
	Model                string  `json:"model"`
	Role                 string  `json:"role"`
	Runs                 int     `json:"runs"`
	FindingsRaised       int     `json:"findings_raised"`
	FindingsCorroborated int     `json:"findings_corroborated"`
	FindingsSolo         int     `json:"findings_solo"`
	CorroborationRate    float64 `json:"corroboration_rate"`
	FindingsVerified     int     `json:"findings_verified"`
	FindingsRefuted      int     `json:"findings_refuted"`
	// SurvivedSkepticRate is verified/(verified+refuted), or 0.0 when that
	// denominator is zero. 0.0 is therefore ambiguous: it means BOTH "no
	// verification ran for this group" and "every finding was refuted/unverifiable".
	// The field is always present (no omitempty, per the allowlist contract above)
	// and a sentinel is impossible because clampRate pins it to [0,1]; a consumer
	// disambiguates via findings_verified+findings_refuted > 0 (zero => no verification).
	SurvivedSkepticRate float64 `json:"survived_skeptic_rate"`
	CostUSD             float64 `json:"cost_usd"`
	TokensIn            int     `json:"tokens_in"`
	TokensOut           int     `json:"tokens_out"`
	LatencyMSAvg        int64   `json:"latency_ms_avg"`
}

// ExportFilters echoes the active filter values back in the envelope so a
// submission is self-describing about the slice of data it represents.
type ExportFilters struct {
	Since   string `json:"since"`
	Model   string `json:"model"`
	Persona string `json:"persona"`
}

// ExportEnvelope is the top-level v1 public submission document.
type ExportEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	ExportedAt    string         `json:"exported_at"`
	Filters       ExportFilters  `json:"filters"`
	Records       []PublicRecord `json:"records"`
}

// AnonymizeRecord maps one internal Record to a PublicRecord, copying only the
// v1 allowlist fields and dropping everything else (run_id, verification
// pointers become plain zero-safe values). It is the single field-mapping
// primitive: Export anonymizes every record through it before aggregating, so
// the allowlist is defined in exactly one place. A single record anonymizes to
// runs=1 with latency_ms_avg equal to its own latency. Identity strings
// (reviewer/model/role) are scrubbed of path-like and key-like substrings as
// defense-in-depth, even though they are not normally PII-bearing.
func AnonymizeRecord(raw Record) PublicRecord {
	pr := PublicRecord{
		Reviewer:             scrubField(raw.Reviewer),
		Model:                scrubField(raw.Model),
		Role:                 scrubField(raw.Role),
		Runs:                 1,
		FindingsRaised:       clampNonNeg(raw.FindingsRaised),
		FindingsCorroborated: clampNonNeg(raw.FindingsCorroborated),
		FindingsSolo:         clampNonNeg(raw.FindingsSolo),
		CorroborationRate:    clampRate(raw.CorroborationRate),
		CostUSD:              clampNonNegF(raw.CostUSD),
		TokensIn:             clampNonNeg(raw.TokensIn),
		TokensOut:            clampNonNeg(raw.TokensOut),
		LatencyMSAvg:         clampNonNeg64(raw.LatencyMS),
	}
	if raw.FindingsVerified != nil {
		pr.FindingsVerified = clampNonNeg(*raw.FindingsVerified)
	}
	if raw.FindingsRefuted != nil {
		pr.FindingsRefuted = clampNonNeg(*raw.FindingsRefuted)
	}
	if raw.SurvivedSkepticRate != nil {
		pr.SurvivedSkepticRate = clampRate(*raw.SurvivedSkepticRate)
	}
	return pr
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
	if f < 0 {
		return 0
	}
	return f
}

func clampRate(f float64) float64 {
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	default:
		return f
	}
}

// Export filters, anonymizes, aggregates, and serializes the scorecard records
// into a deterministic v1 public submission document. Filters are applied BEFORE
// anonymization (AC 04-04). Records are aggregated by (reviewer, model, role) —
// summing finding/token/cost totals, counting runs, and averaging latency — then
// sorted ascending by (model, reviewer, role) and indexed by position, so the
// same input and exportedAt always produce byte-identical output. exportedAt is
// used both as the envelope timestamp and as the --since window anchor, so the
// document is fully reproducible (no hidden time.Now()).
func Export(records []Record, opts FilterOpts, exportedAt time.Time) ([]byte, error) {
	filtered, err := ApplyFilters(records, opts, exportedAt)
	if err != nil {
		return nil, err
	}
	if len(filtered) == 0 {
		return nil, ErrNoExportRecords
	}

	type key struct{ reviewer, model, role string }
	type acc struct {
		pr           PublicRecord
		latencyTotal int64
	}
	groups := map[key]*acc{}
	order := make([]key, 0)

	for _, r := range filtered {
		pub := AnonymizeRecord(r)
		k := key{pub.Reviewer, pub.Model, pub.Role}
		a, ok := groups[k]
		if !ok {
			a = &acc{pr: PublicRecord{Reviewer: pub.Reviewer, Model: pub.Model, Role: pub.Role}}
			groups[k] = a
			order = append(order, k)
		}
		p := &a.pr
		p.Runs += pub.Runs
		p.FindingsRaised += pub.FindingsRaised
		p.FindingsCorroborated += pub.FindingsCorroborated
		p.FindingsSolo += pub.FindingsSolo
		p.CostUSD += pub.CostUSD
		p.TokensIn += pub.TokensIn
		p.TokensOut += pub.TokensOut
		p.FindingsVerified += pub.FindingsVerified
		p.FindingsRefuted += pub.FindingsRefuted
		a.latencyTotal += pub.LatencyMSAvg
	}

	rows := make([]PublicRecord, 0, len(order))
	for _, k := range order {
		a := groups[k]
		p := a.pr
		p.CorroborationRate = ratio(p.FindingsCorroborated, p.FindingsRaised)
		p.SurvivedSkepticRate = ratio(p.FindingsVerified, p.FindingsVerified+p.FindingsRefuted)
		if p.Runs > 0 {
			p.LatencyMSAvg = a.latencyTotal / int64(p.Runs)
		}
		rows = append(rows, p)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Model != rows[j].Model {
			return rows[i].Model < rows[j].Model
		}
		if rows[i].Reviewer != rows[j].Reviewer {
			return rows[i].Reviewer < rows[j].Reviewer
		}
		return rows[i].Role < rows[j].Role
	})
	for i := range rows {
		rows[i].Index = i
	}

	env := ExportEnvelope{
		SchemaVersion: SchemaVersion,
		ExportedAt:    exportedAt.UTC().Format(time.RFC3339),
		// Explicit field assignment, not ExportFilters(opts): a type conversion
		// silently misaligns if FilterOpts fields are reordered (same types, new
		// order → wrong since/model/persona in the public envelope, no compile
		// error). Naming each field is immune to reordering and equally terse.
		Filters: ExportFilters{Since: opts.Since, Model: opts.Model, Persona: opts.Persona}, //nolint:staticcheck
		Records: rows,
	}
	return json.MarshalIndent(env, "", "  ")
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
