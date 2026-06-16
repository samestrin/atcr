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
// (or the store is empty). Its text is the exact user-facing message the CLI
// surfaces (AC 04-04): callers map it to exit 1 without writing any output.
var ErrNoExportRecords = errors.New("No records match the specified filters. Try widening --since or removing filters.")

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
	SurvivedSkepticRate  float64 `json:"survived_skeptic_rate"`
	CostUSD              float64 `json:"cost_usd"`
	TokensIn             int     `json:"tokens_in"`
	TokensOut            int     `json:"tokens_out"`
	LatencyMSAvg         int64   `json:"latency_ms_avg"`
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
		FindingsRaised:       raw.FindingsRaised,
		FindingsCorroborated: raw.FindingsCorroborated,
		FindingsSolo:         raw.FindingsSolo,
		CorroborationRate:    raw.CorroborationRate,
		CostUSD:              raw.CostUSD,
		TokensIn:             raw.TokensIn,
		TokensOut:            raw.TokensOut,
		LatencyMSAvg:         raw.LatencyMS,
	}
	if raw.FindingsVerified != nil {
		pr.FindingsVerified = *raw.FindingsVerified
	}
	if raw.FindingsRefuted != nil {
		pr.FindingsRefuted = *raw.FindingsRefuted
	}
	if raw.SurvivedSkepticRate != nil {
		pr.SurvivedSkepticRate = *raw.SurvivedSkepticRate
	}
	return pr
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
		Filters:       ExportFilters{Since: opts.Since, Model: opts.Model, Persona: opts.Persona},
		Records:       rows,
	}
	return json.MarshalIndent(env, "", "  ")
}

// scrubField removes path-like and credential-like substrings from an identity
// string. Absolute paths are anchored to start-of-token (after whitespace or at
// the string start) so a provider-prefixed model id like "anthropic/claude-3"
// (an internal '/', not an absolute path) is preserved, while "/Users/sam/x" is
// stripped. Whitespace is then collapsed.
func scrubField(s string) string {
	s = scrubPath.ReplaceAllString(s, "$1")
	s = scrubWinPath.ReplaceAllString(s, "")
	s = scrubKey.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

var (
	scrubPath    = regexp.MustCompile(`(^|\s)(~|/)\S*`)
	scrubWinPath = regexp.MustCompile(`[A-Za-z]:\\\S*`)
	scrubKey     = regexp.MustCompile(`sk-\S+|ghp_\S+|xoxb-\S+|Bearer\s+\S+`)
)
