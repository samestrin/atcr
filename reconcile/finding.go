package reconcile

// Finding is the library's unified wire-format finding: the core columns shared
// by a per-source input finding and a reconciled output finding. It carries the
// nine wire fields plus the reconciled reviewer list, confidence, the severity
// disagreement annotation, and the optional verification block.
//
// Path-validation fields (path_valid, path_warning, path_suggestion) and other
// ATCR-internal extensions are deliberately NOT part of this type: they stay in
// ATCR's boundary adapter so the public library schema is independent of
// ATCR-specific concerns.
//
// Serialization policy: the nine core wire fields (Severity..Evidence) are
// always emitted so a bare input finding has a stable shape. Reconciled-only
// fields (Reviewer, Reviewers, Confidence, Disagreement, Verification) use
// omitempty so a fully populated reconciled finding omits zero-value sidecar
// fields, while a bare input finding still emits its zero-value core fields.
type Finding struct {
	Severity     string        `json:"severity"`
	File         string        `json:"file"`
	Line         int           `json:"line"`
	Problem      string        `json:"problem"`
	Fix          string        `json:"fix"`
	Category     string        `json:"category"`
	EstMinutes   int           `json:"est_minutes"`
	Evidence     string        `json:"evidence"`
	Reviewer     string        `json:"reviewer,omitempty"`   // per-source 8th column
	Reviewers    []string      `json:"reviewers,omitempty"`  // reconciled 8th column
	Confidence   string        `json:"confidence,omitempty"` // reconciled 9th column
	Disagreement string        `json:"disagreement,omitempty"`
	Verification *Verification `json:"verification,omitempty"`
}
