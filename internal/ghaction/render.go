package ghaction

import "github.com/samestrin/atcr/internal/reconcile"

// CheckOutput is the rendered GitHub check-run output payload.
type CheckOutput struct {
	Title   string
	Summary string
	Text    string
}

// FixAttribution extracts the executor name from a finding's Evidence field.
func FixAttribution(evidence string) string { return "" }

// Conclusion computes the GitHub check-run conclusion under a fail-on threshold.
func Conclusion(findings []reconcile.JSONFinding, failOn string) (string, int) { return "", 0 }

// BuildCheckOutput renders the check-run output for the findings.
func BuildCheckOutput(findings []reconcile.JSONFinding, failOn string) CheckOutput {
	return CheckOutput{}
}
