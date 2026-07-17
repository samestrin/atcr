package localdebt

// qualitysignal.go builds the content-free, per-(persona, model) quality signal
// (Sprint 30.0) from the append-only local-debt stream. It reads only the
// Reviewers, Model, and Status fields already present on Record — never code,
// file paths, or problem/fix text — so the aggregated shape is structurally
// incapable of carrying finding content.

// foldTerminalByID folds the append-only record stream by ID down to at most one
// TERMINAL record per id, discarding ids that never reached a terminal
// (resolved/wontfix/deferred) status. It reuses FoldRecords for the precedence
// logic — terminal wins over open, higher-precedence terminal wins over lower
// (wontfix > resolved > deferred), later timestamp breaks a same-rank tie — and
// then keeps only the ids whose effective record is terminal, because an
// unresolved (still-open) finding is not yet a quality signal. The fold is O(n):
// FoldRecords does a single keyed pass and this adds one linear filter, with no
// per-id rescan of the whole stream.
func foldTerminalByID(records []Record) []Record {
	effective := FoldRecords(records)
	terminal := make([]Record, 0, len(effective))
	for _, r := range effective {
		if IsClosedStatus(r.Status) {
			terminal = append(terminal, r)
		}
	}
	return terminal
}
