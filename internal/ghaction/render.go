package ghaction

import (
	"fmt"
	"strings"

	"github.com/samestrin/atcr/internal/reconcile"
)

// CheckOutput is the rendered GitHub check-run output payload (the `output`
// object of a check run: a title, a short summary, and a longer markdown text).
type CheckOutput struct {
	Title   string
	Summary string
	Text    string
}

// maxCheckTextBytes bounds the rendered check-run output text. GitHub rejects a
// check run whose output.text exceeds 65535 bytes (HTTP 422); a large PR's
// findings table can cross that line, so the table is truncated with a notice
// rather than letting the post fail.
const maxCheckTextBytes = 60000

// fixAttributionPrefix mirrors the token the executor stage appends to a
// finding's Evidence field (internal/verify/executor.go). Evidence segments are
// joined with "; ", and the executor segment is exactly "fix by <name>".
const fixAttributionPrefix = "fix by "

// FixAttribution extracts the executor name from a finding's Evidence field,
// parsing the "; fix by <name>" token written by the executor stage (Epic 7.0).
// It returns "" when no attribution token is present (the common case before a
// fix has been generated, or for a review that never ran the executor).
func FixAttribution(evidence string) string {
	segs := strings.Split(evidence, "; ")
	for i := len(segs) - 1; i >= 0; i-- {
		seg := strings.TrimSpace(segs[i])
		if rest, ok := strings.CutPrefix(seg, fixAttributionPrefix); ok {
			if name := strings.TrimSpace(rest); name != "" {
				return name
			}
		}
	}
	return ""
}

// isRefuted reports whether a finding was disproved by the adversarial
// verification stage (Epic 3.0). A refuted finding is retained in the artifacts
// for audit but must never block CI, mirroring the reconcile gate's semantics.
func isRefuted(f reconcile.JSONFinding) bool {
	return f.Verification != nil && strings.EqualFold(strings.TrimSpace(f.Verification.Verdict), reconcile.VerdictRefuted)
}

// Conclusion computes the GitHub check-run conclusion for the findings under the
// given fail-on threshold. An empty threshold yields "neutral" — the check is
// informational and never blocks the merge. Otherwise it returns "failure" when
// any non-refuted finding sits at or above the threshold, else "success".
// failCount is the number of blocking findings (0 when the threshold is empty).
func Conclusion(findings []reconcile.JSONFinding, failOn string) (string, int) {
	if strings.TrimSpace(failOn) == "" {
		return "neutral", 0
	}
	count := 0
	for _, f := range findings {
		if isRefuted(f) {
			continue
		}
		if reconcile.AtOrAbove(f.Severity, failOn) {
			count++
		}
	}
	if count > 0 {
		return "failure", count
	}
	return "success", 0
}

// cell neutralizes a value for safe inclusion in a single markdown table cell:
// a literal pipe would break the column grammar, and an embedded newline would
// split the row across physical lines.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "/")
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

// location renders a finding's FILE:LINE anchor, omitting the line when it is
// unknown (0), matching the findings-format contract.
func location(f reconcile.JSONFinding) string {
	if f.Line <= 0 {
		return f.File
	}
	return fmt.Sprintf("%s:%d", f.File, f.Line)
}

// BuildCheckOutput renders the GitHub check-run output for a set of reconciled
// findings: a one-line title, a gate summary, and a markdown findings table.
func BuildCheckOutput(findings []reconcile.JSONFinding, failOn string) CheckOutput {
	total := len(findings)
	if total == 0 {
		return CheckOutput{
			Title:   "atcr — no findings",
			Summary: "atcr review found no findings.",
			Text:    "ATCR review completed with no findings.",
		}
	}

	conclusion, failCount := Conclusion(findings, failOn)

	var title string
	if strings.TrimSpace(failOn) == "" {
		title = fmt.Sprintf("atcr — %d finding(s)", total)
	} else {
		threshold, err := reconcile.ParseSeverity(failOn)
		if err != nil {
			threshold = failOn
		}
		title = fmt.Sprintf("atcr — %d finding(s), %d at/above %s", total, failCount, threshold)
	}

	var summary string
	switch conclusion {
	case "failure":
		summary = fmt.Sprintf("Gate failed: %d finding(s) at or above the threshold.", failCount)
	case "success":
		summary = "Gate passed: no findings at or above the threshold."
	default:
		summary = "Informational review — no merge gate configured."
	}

	var b strings.Builder
	switch conclusion {
	case "failure":
		fmt.Fprintf(&b, "**Gate failed:** %d finding(s) at or above the threshold.\n\n", failCount)
	case "success":
		b.WriteString("**Gate passed:** no findings at or above the threshold.\n\n")
	default:
		b.WriteString("Informational review — no merge gate configured.\n\n")
	}
	b.WriteString("| Severity | Location | Problem | Confidence |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	shownCount := 0
	for _, f := range findings {
		severity := cell(f.Severity)
		if canon, err := reconcile.ParseSeverity(f.Severity); err == nil {
			severity = canon
		}
		if isRefuted(f) {
			severity = fmt.Sprintf("%s (refuted)", severity)
		}
		row := fmt.Sprintf("| %s | %s | %s | %s |\n",
			severity, cell(location(f)), cell(f.Problem), cell(f.Confidence))
		// Stop before crossing GitHub's output.text limit, leaving room for the
		// truncation notice. The remaining findings still live in the artifacts.
		if b.Len()+len(row)+128 > maxCheckTextBytes {
			fmt.Fprintf(&b, "\n_…table truncated: %d of %d findings shown. See the uploaded artifacts for the full set._\n", shownCount, total)
			break
		}
		b.WriteString(row)
		shownCount++
	}

	return CheckOutput{Title: title, Summary: summary, Text: b.String()}
}
