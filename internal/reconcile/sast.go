package reconcile

import (
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/sast"
)

// sastSourceName is the Summary.SourcesScanned / PerSourceCounts key the
// deterministic SAST findings (epic 50.0) are counted under.
const sastSourceName = "sast"

// sastReviewerPrefix prefixes the scanner name to form each SAST finding's
// reviewer, so a scanner can never be mistaken for an LLM persona.
const sastReviewerPrefix = "sast-"

// sastSeverity is the severity every SAST finding is merged at. A SARIF
// result carries no severity ParseSARIF keeps, and the Fixer only fixes a
// finding at or above min_severity_for_fix (default MEDIUM), so MEDIUM is the
// lowest level that is still fixed by default without failing a --fail-on HIGH
// gate on every scanner hit.
const sastSeverity = SevMedium

// mergeSAST appends the SAST findings in reviewDir's sidecar to res as
// HIGH-confidence findings, then restores the severity-first order. They are
// appended beside the LLM findings, never clustered with them: a scanner hit
// is deterministic evidence and keeps its own record and confidence. It runs
// before path validation, symbol anchoring, id stamping and ignore rules, so a
// SAST record is treated like any other from there on. No sidecar (the scanner
// is off by default) leaves res untouched; an unreadable or unsafe one is an
// error so the run never reports zero SAST findings when it could not tell.
func mergeSAST(res *Result, reviewDir string) error {
	rep, err := sast.ReadReport(reviewDir)
	if err != nil || rep == nil {
		return err
	}
	reviewer := sastReviewerPrefix + rep.Scanner
	for _, f := range rep.Findings {
		res.Findings = append(res.Findings, Merged{Finding: Finding{
			Severity:   sastSeverity,
			File:       f.File,
			Line:       f.Line,
			Problem:    sastProblem(f),
			Category:   "security",
			Evidence:   reviewer + ": rule " + oneLine(f.Rule),
			Reviewers:  []string{reviewer},
			Confidence: ConfHigh,
		}})
	}
	sortFindings(res.Findings)

	if res.Summary.PerSourceCounts == nil {
		res.Summary.PerSourceCounts = map[string]int{}
	}
	if _, seen := res.Summary.PerSourceCounts[sastSourceName]; !seen {
		res.Summary.SourcesScanned = append(res.Summary.SourcesScanned, sastSourceName)
		sort.Strings(res.Summary.SourcesScanned)
	}
	res.Summary.PerSourceCounts[sastSourceName] += len(rep.Findings)
	res.Summary.TotalFindings = len(res.Findings)
	return nil
}

// sastProblem renders a scanner result as one PROBLEM line: the message, then
// the rule id in brackets. Scanner text is collapsed to one line so it reads
// the same in findings.json, findings.txt and report.md.
func sastProblem(f sast.Finding) string {
	msg, rule := oneLine(f.Message), oneLine(f.Rule)
	switch {
	case msg == "" && rule == "":
		return "SAST finding"
	case msg == "":
		return "[" + rule + "]"
	case rule == "":
		return msg
	}
	return msg + " [" + rule + "]"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// sortFindings is the library's sortMerged order (severity, file, line,
// problem), which is unexported there.
func sortFindings(m []Merged) {
	sort.SliceStable(m, func(i, j int) bool {
		ri, rj := SeverityRank[NormalizeSeverity(m[i].Severity)], SeverityRank[NormalizeSeverity(m[j].Severity)]
		if ri != rj {
			return ri > rj
		}
		if m[i].File != m[j].File {
			return m[i].File < m[j].File
		}
		if m[i].Line != m[j].Line {
			return m[i].Line < m[j].Line
		}
		return m[i].Problem < m[j].Problem
	})
}
