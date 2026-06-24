package reconcile_test

import (
	"fmt"

	"github.com/samestrin/atcr/reconcile"
)

// ExampleReconcile demonstrates merging two reviewers' findings that point at the
// same location. The deterministic pipeline collapses them into one reconciled
// finding: severity is the max (with a "<lo> vs <hi>" disagreement annotation
// when the reviewers disagree), the agreeing reviewers are unioned, and
// confidence rises to HIGH because two distinct reviewers caught the same issue.
func ExampleReconcile() {
	sources := []reconcile.Source{
		{Name: "reviewer-a", Findings: []reconcile.Finding{{
			Severity:   "medium",
			File:       "auth.go",
			Line:       88,
			Problem:    "missing nil check on session",
			Fix:        "add nil guard",
			Category:   "bug",
			EstMinutes: 15,
			Evidence:   "stack trace",
			Reviewer:   "reviewer-a",
		}}},
		{Name: "reviewer-b", Findings: []reconcile.Finding{{
			Severity:   "high",
			File:       "auth.go",
			Line:       88,
			Problem:    "missing nil check on session",
			Fix:        "guard session before deref",
			Category:   "bug",
			EstMinutes: 20,
			Evidence:   "local repro",
			Reviewer:   "reviewer-b",
		}}},
	}

	result := reconcile.Reconcile(sources, reconcile.Options{})

	f := result.Findings[0]
	fmt.Printf("severity=%s file=%s line=%d\n", f.Severity, f.File, f.Line)
	fmt.Printf("reviewers=%v confidence=%s\n", f.Reviewers, f.Confidence)
	fmt.Printf("disagreement=%q\n", f.Disagreement)
	// Output:
	// severity=HIGH file=auth.go line=88
	// reviewers=[reviewer-a reviewer-b] confidence=HIGH
	// disagreement="MEDIUM vs HIGH"
}
