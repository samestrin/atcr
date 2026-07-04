package reconcile

import (
	"log/slog"
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
	total := len(jf)
	anchored := 0
	for i := range jf {
		name := sr.EnclosingSymbol(reclib.Finding{File: jf[i].File, Line: jf[i].Line})
		if !safeSymbolAnchor(name) {
			continue
		}
		prefix := "(" + name + ") "
		if strings.HasPrefix(jf[i].Problem, prefix) {
			anchored++
			continue
		}
		jf[i].Problem = prefix + jf[i].Problem
		anchored++
	}
	slog.Debug("symbol anchors stamped", "anchored", anchored, "total", total)
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

// StripSymbolAnchors removes any leading "(symbol) " anchors that stampSymbolAnchors
// may have prepended to a Problem cell, returning the underlying problem text.
//
// Identity correlation across reconcile artifacts must compare underlying problems,
// not display text: findings.json is symbol-anchored while ambiguous.json cluster
// members stay raw, so a consumer matching a finding to its cluster member (e.g.
// the debate gray-zone merge) must normalize away the anchor — it is a derived
// annotation, not part of a finding's identity. Stripping is greedy and applied to
// BOTH sides of such a comparison, so a problem that already began with a
// parenthesized token before stamping normalizes identically whether or not it was
// later stamped. Only a well-formed anchor — "(", a run of table-safe anchor
// characters (see safeSymbolAnchor), ")", exactly one space — is removed; a leading
// "(" that is not a well-formed anchor is left intact.
func StripSymbolAnchors(problem string) string {
	for {
		rest, ok := stripOneAnchor(problem)
		if !ok {
			return problem
		}
		problem = rest
	}
}

// stripOneAnchor removes a single leading "(name) " anchor from s and reports
// whether one was removed. name must satisfy safeSymbolAnchor, which forbids ')',
// so the first ')' unambiguously terminates the anchor.
func stripOneAnchor(s string) (string, bool) {
	if len(s) < 4 || s[0] != '(' {
		return s, false
	}
	close := strings.IndexByte(s, ')')
	if close < 0 {
		return s, false
	}
	if !safeSymbolAnchor(s[1:close]) {
		return s, false
	}
	after := s[close+1:]
	if len(after) == 0 || after[0] != ' ' {
		return s, false
	}
	return after[1:], true
}
