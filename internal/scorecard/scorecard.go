// Package scorecard emits a normalized per-reviewer eval record alongside each
// reconcile run and accumulates those records into a local monthly JSONL store
// (~/.config/atcr/scorecard/YYYY-MM.jsonl). Each run appends one record per
// reviewer plus one aggregate record. The store is local and never committed;
// records are the data prerequisite for the public Model-Eval Leaderboard
// (Epic 10.0). Cost is computed at emit time from the per-model rate table so a
// rate correction re-prices historical records on read.
package scorecard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/samestrin/atcr/internal/llmclient"
)

// SchemaVersion is the scorecard record schema version. It is emitted as an
// integer on every record so Epic 10.0's public submission format can evolve
// independently; a future change increments this and old records stay readable.
const SchemaVersion = 1

// Record type discriminators (AC 01-05): one "reviewer" record per participating
// reviewer plus one "aggregate" record summarizing the whole run.
const (
	RecordTypeReviewer  = "reviewer"
	RecordTypeAggregate = "aggregate"
)

// defaultRole labels per-reviewer records produced from a reconcile run. Every
// reconcile finding originates from the review stage, whose agents are reviewers
// by definition (skeptics/judges run in later stages), so the role is constant
// here rather than threaded per agent.
const defaultRole = "reviewer"

// Record is one scorecard JSONL line. The first block is always present; the
// verification block (pointers + omitempty) is present only when a valid
// reconciled/verification.json drove the run (AC 01-03) — a nil pointer omits
// the key entirely, while a pointer to 0 still serializes (0 is a valid value).
type Record struct {
	SchemaVersion        int     `json:"schema_version"`
	RecordType           string  `json:"record_type"`
	RunID                string  `json:"run_id"`
	Reviewer             string  `json:"reviewer"`
	Model                string  `json:"model"`
	Role                 string  `json:"role"`
	FindingsRaised       int     `json:"findings_raised"`
	FindingsCorroborated int     `json:"findings_corroborated"`
	FindingsSolo         int     `json:"findings_solo"`
	CorroborationRate    float64 `json:"corroboration_rate"`
	CostUSD              float64 `json:"cost_usd"`
	TokensIn             int     `json:"tokens_in"`
	TokensOut            int     `json:"tokens_out"`
	LatencyMS            int64   `json:"latency_ms"`

	FindingsVerified    *int     `json:"findings_verified,omitempty"`
	FindingsRefuted     *int     `json:"findings_refuted,omitempty"`
	SurvivedSkepticRate *float64 `json:"survived_skeptic_rate,omitempty"`
}

// Finding is the minimal per-finding input the emitter needs to compute
// per-reviewer corroboration metrics and (when verification is present) attribute
// skeptic verdicts to the reviewers that raised the finding.
type Finding struct {
	File      string
	Line      int
	Problem   string
	Reviewers []string
}

// ReviewerMeta carries the per-reviewer identity/usage sourced from the fan-out's
// persisted status.json (model + token usage + latency). reconcile runs as a
// separate process from the review, so this data must come from disk, not the
// in-memory fan-out Result. Cost is NOT carried here — it is derived at emit time
// from Model + tokens via llmclient.ComputeCostUSD.
type ReviewerMeta struct {
	Model     string
	TokensIn  int
	TokensOut int
	LatencyMS int64
}

// EmitOpts controls emission side-effects. NoScorecard suppresses all I/O (the
// --no-scorecard gate; checked first, before any directory creation). Dir
// overrides the store root (tests pin a temp dir); empty means the default user
// config dir. Diag is the sink for operational diagnostics (write failures,
// verification read/parse failures, orphan verdicts); a nil Diag defaults to
// os.Stderr so existing callers keep their prior behavior (Epic 3.4).
type EmitOpts struct {
	NoScorecard bool
	Dir         string
	Diag        io.Writer
}

// EmitInput bundles everything Emit needs for one run. Reviewers is keyed by
// reviewer name and defines the set of per-reviewer records (the reviewers that
// actually ran); Findings drives the corroboration counts. VerificationPath, when
// non-empty and pointing at a readable, well-formed verification.json, adds the
// conditional skeptic fields.
type EmitInput struct {
	RunID            string
	Findings         []Finding
	Reviewers        map[string]ReviewerMeta
	VerificationPath string
}

// Emit computes per-reviewer metrics, builds one record per reviewer plus one
// aggregate record, and appends them to the monthly JSONL store. It is
// best-effort: a write failure for one record is logged and the run continues, so
// scorecard emission never fails the caller's reconcile. The NoScorecard gate is
// the first check — when set, Emit returns immediately with zero I/O (no directory
// creation, no file open).
func Emit(in EmitInput, opts EmitOpts) error {
	// Suppression gate — intentionally the FIRST statement, before resolveDir or
	// any file I/O, so --no-scorecard creates no directory and opens no file.
	if opts.NoScorecard {
		return nil
	}

	dir, err := resolveDir(opts.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scorecard: write failed: %v\n", err)
		return err
	}

	verified, refuted, hasVerification := verdictTallies(in)

	// Deterministic reviewer order so the JSONL line order is stable.
	names := make([]string, 0, len(in.Reviewers))
	for name := range in.Reviewers {
		names = append(names, name)
	}
	sort.Strings(names)

	records := make([]Record, 0, len(names)+1)
	var agg Record
	agg.SchemaVersion = SchemaVersion
	agg.RecordType = RecordTypeAggregate
	agg.RunID = in.RunID
	var aggVerified, aggRefuted int

	for _, name := range names {
		meta := in.Reviewers[name]
		raised, corroborated := reviewerCounts(name, in.Findings)
		rec := Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			RunID:                in.RunID,
			Reviewer:             name,
			Model:                meta.Model,
			Role:                 defaultRole,
			FindingsRaised:       raised,
			FindingsCorroborated: corroborated,
			FindingsSolo:         raised - corroborated,
			CorroborationRate:    ratio(corroborated, raised),
			CostUSD:              llmclient.ComputeCostUSD(meta.Model, meta.TokensIn, meta.TokensOut),
			TokensIn:             meta.TokensIn,
			TokensOut:            meta.TokensOut,
			LatencyMS:            meta.LatencyMS,
		}
		if hasVerification {
			v, r := verified[name], refuted[name]
			rate := ratio(v, v+r)
			rec.FindingsVerified = &v
			rec.FindingsRefuted = &r
			rec.SurvivedSkepticRate = &rate
			aggVerified += v
			aggRefuted += r
		}

		agg.FindingsRaised += rec.FindingsRaised
		agg.FindingsCorroborated += rec.FindingsCorroborated
		agg.FindingsSolo += rec.FindingsSolo
		agg.CostUSD += rec.CostUSD
		agg.TokensIn += rec.TokensIn
		agg.TokensOut += rec.TokensOut
		if rec.LatencyMS > agg.LatencyMS {
			agg.LatencyMS = rec.LatencyMS // run latency ~ slowest reviewer (parallel)
		}

		records = append(records, rec)
	}

	// Aggregate corroboration rate is computed from totals, not an average of
	// per-reviewer rates (AC 01-05 EC3).
	agg.CorroborationRate = ratio(agg.FindingsCorroborated, agg.FindingsRaised)
	if hasVerification {
		agg.FindingsVerified = &aggVerified
		agg.FindingsRefuted = &aggRefuted
		rate := ratio(aggVerified, aggVerified+aggRefuted)
		agg.SurvivedSkepticRate = &rate
	}
	// Aggregate is appended LAST so it is the final line of the run's batch.
	records = append(records, agg)

	var firstErr error
	for _, rec := range records {
		if err := Append(dir, rec); err != nil {
			fmt.Fprintf(os.Stderr, "scorecard: write failed: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// reviewerCounts returns how many findings name raised and how many of those were
// corroborated (the finding carried 2+ distinct reviewers). Solo is the
// difference, computed by the caller. The O(reviewers x findings) scan (one pass
// per reviewer, recomputing distinctCount per match) is intentional: emission is
// a once-per-reconcile, best-effort path over a handful of reviewers and a
// diff-bounded finding set, so a single-pass precompute buys no observable speed.
func reviewerCounts(name string, findings []Finding) (raised, corroborated int) {
	for _, f := range findings {
		if !contains(f.Reviewers, name) {
			continue
		}
		raised++
		if distinctCount(f.Reviewers) >= 2 {
			corroborated++
		}
	}
	return raised, corroborated
}

// verdictTallies reads VerificationPath and attributes each finding's skeptic
// verdict to the reviewers that raised that finding (matched by file+line+problem
// against in.Findings). It returns per-reviewer confirmed/refuted counts and
// whether a valid verification.json was present. An absent, unreadable, or
// malformed file degrades to no verification (fields omitted), per AC 01-03.
func verdictTallies(in EmitInput) (verified, refuted map[string]int, present bool) {
	if in.VerificationPath == "" {
		return nil, nil, false
	}
	data, err := os.ReadFile(in.VerificationPath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "scorecard: verification read failed: %v\n", err)
		}
		return nil, nil, false
	}
	var vf verificationFile
	if err := json.Unmarshal(data, &vf); err != nil {
		fmt.Fprintf(os.Stderr, "scorecard: verification parse failed: %v\n", err)
		return nil, nil, false
	}

	// Map finding location -> reviewers so a verdict credits the right reviewers.
	// Two findings can share one (file,line,problem) key with different reviewers;
	// union (deduped) rather than overwrite so a verdict on that location credits
	// every reviewer that raised it, not just the last one seen.
	reviewersByKey := make(map[string][]string, len(in.Findings))
	for _, f := range in.Findings {
		k := findingKey(f.File, f.Line, f.Problem)
		for _, rev := range f.Reviewers {
			if !contains(reviewersByKey[k], rev) {
				reviewersByKey[k] = append(reviewersByKey[k], rev)
			}
		}
	}

	verified = map[string]int{}
	refuted = map[string]int{}
	for _, vfind := range vf.Findings {
		revs, ok := reviewersByKey[findingKey(vfind.File, vfind.Line, vfind.Problem)]
		if !ok {
			// Orphan verdict: a verification finding with no matching raised finding.
			// The exact (file,line,problem) key is canonical across the pipeline
			// (findings.json and verification.json derive from the same reconciled
			// objects), so a miss means real under-counting — warn rather than drop it
			// silently (mirrors verify's orphan_verdict diagnostic).
			fmt.Fprintf(os.Stderr, "scorecard: verification finding %s:%d has no matching raised finding; verdict attribution skipped\n", vfind.File, vfind.Line)
			continue
		}
		switch normalizeVerdict(vfind.Verdict) {
		case verdictConfirmed:
			for _, r := range revs {
				verified[r]++
			}
		case verdictRefuted:
			for _, r := range revs {
				refuted[r]++
			}
		}
	}
	return verified, refuted, true
}

// verificationFile is the minimal subset of reconciled/verification.json the
// emitter parses: each finding's location plus its skeptic verdict. It mirrors
// internal/verify.VerificationFile but stays local so the scorecard package has
// no dependency on the verify package.
type verificationFile struct {
	Findings []struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Problem string `json:"problem"`
		Verdict string `json:"verdict"`
	} `json:"findings"`
}

// Verdict values (lower-cased) matching internal/verify's enum.
const (
	verdictConfirmed = "confirmed"
	verdictRefuted   = "refuted"
)

func normalizeVerdict(v string) string {
	out := make([]rune, 0, len(v))
	for _, r := range v {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			// drop ALL whitespace (internal included, not just surrounding), so a
			// reformatted verdict like " Con firmed " still normalizes to "confirmed"
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func findingKey(file string, line int, problem string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", file, line, problem)
}

// ratio returns num/den as a float, or 0.0 when den == 0 (never NaN/Inf).
func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// distinctCount counts distinct non-empty reviewer names in a finding's reviewer
// list (the list is deduped upstream, but the emitter does not rely on that).
func distinctCount(xs []string) int {
	seen := make(map[string]bool, len(xs))
	for _, x := range xs {
		if x != "" {
			seen[x] = true
		}
	}
	return len(seen)
}
