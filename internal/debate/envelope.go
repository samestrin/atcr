package debate

import (
	"encoding/json"
	"strings"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
)

// Judge ruling outcomes. uphold/overturn/split are the judge's verdicts; unresolved
// is the sentinel parseRuling records when the judge output is missing or garbage,
// or when the item could not be debated (casting failed, judge halted).
const (
	OutcomeUphold     = "uphold"
	OutcomeOverturn   = "overturn"
	OutcomeSplit      = "split"
	OutcomeUnresolved = "unresolved"
)

// Cluster decisions for a gray-zone pair (recorded; physical re-merge is applied
// via the reconcile adjudication path).
const (
	ClusterMerge    = "merge"
	ClusterSeparate = "separate"
)

// Ruling is the parsed judge envelope for one debated item. Outcome is always one
// of the four constants above (never empty). SettledSeverity is the severity the
// finding should carry after the ruling (canonical upper-case, or "" when the
// judge gave none). ClusterDecision is set only for a gray-zone item the judge
// ruled on. Reasoning is the judge's free-text rationale.
type Ruling struct {
	Outcome         string
	SettledSeverity string
	ClusterDecision string
	Reasoning       string
}

// Verdict maps the ruling outcome to a reconcile verdict for the finding's
// Verification block: uphold and split confirm the finding (it survived), overturn
// refutes it, and unresolved yields "" (no verdict change). ChallengeSurvived is
// true exactly when the finding survived (uphold/split).
func (r Ruling) Verdict() string {
	switch r.Outcome {
	case OutcomeUphold, OutcomeSplit:
		return reconcile.VerdictConfirmed
	case OutcomeOverturn:
		return reconcile.VerdictRefuted
	default:
		return ""
	}
}

// ChallengeSurvived reports whether the finding survived the challenge (the judge
// upheld it, possibly at an adjusted severity).
func (r Ruling) ChallengeSurvived() bool {
	return r.Outcome == OutcomeUphold || r.Outcome == OutcomeSplit
}

// parseRuling extracts a Ruling from a raw judge response. It never fails on bad
// input — an unparseable, empty, or out-of-enum response degrades to Outcome
// "unresolved" with a diagnostic Reasoning — so an item is never dropped because
// the judge produced garbage (mirroring verify.parseVerdict's tolerant contract).
//
// Extraction tolerates real LLM output: a bare JSON object, one wrapped in
// markdown fences, or one embedded in prose are all handled by scanning for the
// first balanced {...} object carrying an "outcome" key.
func parseRuling(response string) Ruling {
	if strings.TrimSpace(response) == "" {
		return Ruling{Outcome: OutcomeUnresolved, Reasoning: "empty_response"}
	}

	rest := response
	for {
		obj := extractJSONObject(rest)
		if obj == "" {
			next := strings.IndexByte(rest, '{')
			if next < 0 {
				break
			}
			rest = rest[next+1:]
			continue
		}
		var candidate struct {
			Outcome         *string `json:"outcome"`
			SettledSeverity string  `json:"settled_severity"`
			ClusterDecision string  `json:"cluster_decision"`
			Reasoning       string  `json:"reasoning"`
		}
		if json.Unmarshal([]byte(obj), &candidate) == nil && candidate.Outcome != nil {
			return ruleFromCandidate(*candidate.Outcome, candidate.SettledSeverity, candidate.ClusterDecision, candidate.Reasoning, response)
		}
		idx := strings.Index(rest, obj)
		rest = rest[idx+len(obj):]
	}

	return Ruling{Outcome: OutcomeUnresolved, Reasoning: "malformed_output: " + truncate(response)}
}

// ruleFromCandidate normalizes and validates a parsed candidate into a Ruling. An
// out-of-enum outcome degrades to unresolved; an unknown severity or cluster
// decision is dropped to "" rather than failing the ruling.
func ruleFromCandidate(outcome, settled, cluster, reasoning, raw string) Ruling {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case OutcomeUphold:
		outcome = OutcomeUphold
	case OutcomeOverturn:
		outcome = OutcomeOverturn
	case OutcomeSplit:
		outcome = OutcomeSplit
	default:
		return Ruling{Outcome: OutcomeUnresolved, Reasoning: "invalid_outcome: " + truncate(outcome) + " (raw: " + truncate(raw) + ")"}
	}

	sev := stream.NormalizeSeverity(settled)
	if !reviewSeverity(sev) {
		sev = ""
	}
	cd := strings.ToLower(strings.TrimSpace(cluster))
	if cd != ClusterMerge && cd != ClusterSeparate {
		cd = ""
	}
	return Ruling{Outcome: outcome, SettledSeverity: sev, ClusterDecision: cd, Reasoning: reasoning}
}

// reviewSeverity reports whether s is a canonical review severity.
func reviewSeverity(s string) bool {
	switch s {
	case reconcile.SevCritical, reconcile.SevHigh, reconcile.SevMedium, reconcile.SevLow:
		return true
	default:
		return false
	}
}

// truncate caps a diagnostic string at 2000 bytes on a rune boundary.
func truncate(s string) string {
	const cap = 2000
	if len(s) <= cap {
		return s
	}
	cut := cap
	for cut > 0 && (s[cut]&0xC0) == 0x80 { // back up off a UTF-8 continuation byte
		cut--
	}
	return s[:cut] + "…[truncated]"
}

// extractJSONObject returns the first balanced {...} object in s, or "" when none
// exists. Brace matching is string-aware: braces inside a JSON string literal (and
// escaped quotes) do not affect depth, so a reasoning value containing braces does
// not truncate the object early. Mirrors verify.extractJSONObject.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
