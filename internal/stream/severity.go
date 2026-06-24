package stream

import reclib "github.com/samestrin/atcr/reconcile"

// SeverityRank is the canonical severity ordinal rubric, re-exported from the
// extracted reconcile library (github.com/samestrin/atcr/reconcile, Epic 8.0),
// which is now its single source of truth. It is the same backing map the library
// uses, so a severity rename or a non-canonical casing can never desync fan-out
// truncation from reconcile merging. Read-only after init — concurrent consumers
// share it, so a write would race; copy it locally first if mutation is ever
// needed.
var SeverityRank = reclib.SeverityRank

// NormalizeSeverity upper-cases and trims a severity token to its canonical form
// so a SeverityRank lookup is case- and whitespace-insensitive. Re-exported from
// the reconcile library so every consumer normalizes through one helper.
func NormalizeSeverity(s string) string { return reclib.NormalizeSeverity(s) }
