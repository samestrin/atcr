package reconcile

import (
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"
)

// symbolResolver is the optional capability a Grouper may implement to resolve a
// finding to the name of its nearest enclosing named AST block. The AST-grouping
// *lazyGrouper implements it; a proximity-only or nil grouper does not, so symbol
// stamping is skipped when AST grouping is unavailable or disabled.
type symbolResolver interface {
	EnclosingSymbol(reclib.Finding) string
}

// stampSymbolAnchors prepends a "(symbol) " anchor to each finding's Problem cell,
// resolving the finding's file+line to its nearest enclosing named AST block via
// g (epic 18.1). The anchor is a stable RELOCATE_KEY that lets /resolve-td find a
// finding's code after intervening edits drift its reported line number.
//
// It mutates jf in place and is a no-op when g cannot resolve symbols (a
// proximity-only grouper, AST grouping disabled via ATCR_DISABLE_AST_GROUPING, or
// a nil grouper). Per finding the anchor is omitted — Problem left byte-identical
// to pre-18.1 output — when no named block resolves (AC2 graceful degradation) or
// the resolved name is not table-safe (see safeSymbolAnchor): corrupting the
// pipe-delimited TD table or the "(symbol)" parse is worse than a missing anchor.
// Idempotent: a Problem already carrying its own resolved anchor is not
// double-stamped, so a re-reconcile never accretes "(f) (f) …".
func stampSymbolAnchors(jf []JSONFinding, g reclib.Grouper) {
	sr, ok := g.(symbolResolver)
	if !ok || sr == nil {
		return
	}
	for i := range jf {
		name := sr.EnclosingSymbol(reclib.Finding{File: jf[i].File, Line: jf[i].Line})
		if !safeSymbolAnchor(name) {
			continue
		}
		prefix := "(" + name + ") "
		if strings.HasPrefix(jf[i].Problem, prefix) {
			continue
		}
		jf[i].Problem = prefix + jf[i].Problem
	}
}

// safeSymbolAnchor reports whether name may be used as a "(name) " Problem-cell
// anchor. The anchor sits at the start of a cell in a pipe-delimited Markdown
// table that /resolve-td parses by matching the leading parenthesized group, so a
// name containing '|' (the table column separator), '(' or ')' (which would
// truncate or unbalance the parse), or any whitespace/control character (which
// would break the leading-token match) is rejected. A bare Go identifier always
// passes; an exotic brace-parser name like C++ "operator()" is rejected and that
// finding simply goes unanchored.
func safeSymbolAnchor(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch r {
		case '|', '(', ')':
			return false
		}
		if r <= ' ' { // rejects space (0x20) and all control chars (\t, \n, \r, …)
			return false
		}
	}
	return true
}
