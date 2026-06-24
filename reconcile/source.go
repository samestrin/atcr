package reconcile

// Source is one reconcile input: a named origin (e.g. a reviewer or tool) and
// the findings it produced. Reconcile merges the findings across all sources.
//
// This is the library's public input shape — Name plus library Findings only.
// ATCR's discovery layer carries additional I/O bookkeeping (skipped rows,
// skipped files) in its own internal type and converts to []Source at the
// boundary; that bookkeeping is not part of the public library surface.
type Source struct {
	Name     string    `json:"name"`
	Findings []Finding `json:"findings"`
}
