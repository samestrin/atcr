package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/samestrin/atcr/internal/reconcile"
)

// SARIF document constants. The schema URI points at SchemaStore's
// sarif-2.1.0-rtm.5.json — the variant GitHub Code Scanning validates against
// and the fixture renderSarif's output is schema-checked against in tests.
const (
	sarifSchemaURI = "https://json.schemastore.org/sarif-2.1.0-rtm.5.json"
	sarifVersion   = "2.1.0"
	sarifToolName  = "atcr"
	sarifToolURI   = "https://github.com/samestrin/atcr"
	// sarifNoMessage is the fallback for a finding with no Problem text: SARIF's
	// result.message.text is a required field that must be non-empty.
	sarifNoMessage = "(no description)"
)

// The SARIF 2.1.0 struct tree. Field order here fixes JSON key order (encoding/json
// emits struct fields in declaration order), which keeps renderSarif output
// deterministic for golden-file tests. Every finding-derived string flows through
// encoding/json's standard escaping — the correct and sufficient defense against
// JSON injection for a JSON sink (no HTML esc() needed, unlike the markdown view).
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string    `json:"id"`
	ShortDescription sarifText `json:"shortDescription"`
	FullDescription  sarifText `json:"fullDescription"`
}

// sarifText is SARIF's multiformatMessageString shape ({"text": "..."}), reused
// for rule descriptions and result messages.
type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string             `json:"ruleId"`
	Level     string             `json:"level"`
	Message   sarifText          `json:"message"`
	Locations []sarifLocationObj `json:"locations"`
}

// sarifLocationObj is a SARIF location entry. The type is named with the Obj
// suffix so the sarifLocation(f) helper (Story 3) can own the plain name.
type sarifLocationObj struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
}

// renderSarif emits findings as a SARIF 2.1.0 log document. It mirrors
// renderJSON's conventions (nil-slice guard so results[]/rules[] are [] not null,
// json.MarshalIndent with two-space indent, trailing newline). Marshal errors are
// wrapped with context before propagation, unlike renderJSON which returns them raw.
// A single pass builds results[]; sarifRules does a second single pass for the
// deduped rule catalog — both O(n), no quadratic scan.
func renderSarif(w io.Writer, findings []reconcile.JSONFinding) error {
	return renderSarifWithDiag(w, findings, os.Stderr)
}

// renderSarifWithDiag is the testable core of renderSarif. The diag sink is
// passed as a parameter so callers (including concurrent renderers and tests)
// do not share a mutable package-level sink.
func renderSarifWithDiag(w io.Writer, findings []reconcile.JSONFinding, diag io.Writer) error {
	results := make([]sarifResult, 0, len(findings))
	for _, f := range findings {
		results = append(results, sarifResult{
			RuleID:    sarifRuleID(f.Category),
			Level:     sarifLevel(f.Severity, diag),
			Message:   sarifText{Text: sarifMessageText(f)},
			Locations: []sarifLocationObj{sarifLocation(f)},
		})
	}

	doc := sarifLog{
		Schema:  sarifSchemaURI,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           sarifToolName,
				InformationURI: sarifToolURI,
				Rules:          sarifRules(findings),
			}},
			Results: results,
		}},
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sarif: %w", err)
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// sarifRules collects one rule per distinct finding Category in first-seen order
// (a Go map alone is unordered, so a seen-set guards an ordered slice). An empty
// Category is treated as any other distinct value (one rule with id ""). The
// returned slice is always non-nil so it marshals as [] not null for empty input.
func sarifRules(findings []reconcile.JSONFinding) []sarifRule {
	rules := make([]sarifRule, 0)
	seen := make(map[string]bool)
	for _, f := range findings {
		id := sarifRuleID(f.Category)
		if seen[id] {
			continue
		}
		seen[id] = true
		rules = append(rules, sarifRule{
			ID:               id,
			ShortDescription: sarifText{Text: id},
			FullDescription:  sarifText{Text: fmt.Sprintf("ATCR findings categorized as '%s'.", id)},
		})
	}
	return rules
}

// sarifMessageText returns the finding's Problem text, falling back to a fixed
// non-empty string so the required SARIF message.text field is never empty.
func sarifMessageText(f reconcile.JSONFinding) string {
	if strings.TrimSpace(f.Problem) != "" {
		return f.Problem
	}
	return sarifNoMessage
}

// sarifRuleID returns the category to use as a SARIF rule id. An empty or
// whitespace-only category is mapped to a sentinel value so the emitted rule
// catalog and every result.ruleId reference a real, non-empty identifier.
func sarifRuleID(category string) string {
	if strings.TrimSpace(category) != "" {
		return category
	}
	return "uncategorized"
}

// sarifLevel maps an ATCR severity to a SARIF result.level. It is the SOLE
// severity-comparison site in this file: it derives its branches from the
// canonical reclib.SeverityRank rubric (normalized via reclib.NormalizeSeverity)
// rather than a locally redefined severity map, so a rubric change can never
// silently desync this mapping (the TD-0052 failure mode). CRITICAL/HIGH → error,
// MEDIUM → warning, LOW → note; any unrecognized or empty token (rank 0) falls
// back to "warning". The return is always one of error/warning/note — never
// "none" (which GitHub Code Scanning does not display) and never empty. A
// non-empty token that still ranks 0 is treated as upstream corruption and
// emits a diagnostic to sarifDiag (see below); the level stays "warning".
func sarifLevel(severity string, diag io.Writer) string {
	rank := reclib.SeverityRank[reclib.NormalizeSeverity(severity)]
	switch {
	case rank >= reclib.SeverityRank[reclib.SevHigh]:
		return "error"
	case rank == reclib.SeverityRank[reclib.SevMedium]:
		return "warning"
	case rank == reclib.SeverityRank[reclib.SevLow]:
		return "note"
	default:
		// Rank 0 splits two ways: an empty/blank token is empty-by-design (a
		// finding with no severity) and stays silent; a non-empty token that
		// still ranked 0 is unrecognized garbage — a typo'd or externally
		// corrupted findings.json value — so emit a diagnostic to surface the
		// corruption rather than downgrading it invisibly. Per AC 02-01 the
		// returned level stays "warning" in both cases.
		if strings.TrimSpace(severity) != "" {
			_, _ = fmt.Fprintf(diag, "atcr: sarif: unrecognized severity %q; defaulting to \"warning\"\n", severity)
		}
		return "warning"
	}
}

// sarifLocation builds a SARIF physical location for a finding. artifactLocation.uri
// is f.File verbatim (already repo-root-relative by the time it reaches the report
// layer — no normalization). Columns are not tracked in ATCR's finding pipeline, so
// startColumn is synthesized to 1; endColumn is 2 for Line > 0 because SARIF 2.1.0's
// endColumn is exclusive (a 1,1 start/end would be a zero-length region). For Line <= 0
// (file-level findings — both Line == 0 and negative, via a single <= 0 boundary,
// mirroring internal/ghaction/render.go's location() precedent) a full 1,1,1,1 region is
// synthesized rather than omitted, since GitHub Code Scanning requires all four region
// fields for a result to display.
func sarifLocation(f reconcile.JSONFinding) sarifLocationObj {
	startLine, endLine := f.Line, f.Line
	endColumn := 2
	if f.Line <= 0 {
		startLine, endLine = 1, 1
		endColumn = 1
	}
	return sarifLocationObj{PhysicalLocation: sarifPhysicalLocation{
		ArtifactLocation: sarifArtifactLocation{URI: f.File},
		Region:           sarifRegion{StartLine: startLine, StartColumn: 1, EndLine: endLine, EndColumn: endColumn},
	}}
}
