package payload

import "strings"

// ScopeFocus renders an agent's per-agent scope categories (Epic 2.2) as a soft
// focus instruction appended to its persona prompt. It is a SOFT constraint:
// it steers the model toward the listed categories but the fan-out never
// hard-drops out-of-category findings, so a genuine cross-cutting issue is not
// silently lost. Blank entries are skipped; an empty/nil scope yields "" so an
// unscoped agent's prompt is unchanged. The leading blank lines separate the
// block from whatever the persona template rendered before it.
func ScopeFocus(scope []string) string {
	cats := make([]string, 0, len(scope))
	for _, c := range scope {
		if c = strings.TrimSpace(c); c != "" {
			cats = append(cats, c)
		}
	}
	if len(cats) == 0 {
		return ""
	}
	// Trust boundary: scope is operator-controlled registry config, validated at
	// load (registry.Load rejects blank/whitespace and control-character entries),
	// so each category is concatenated verbatim with no per-entry escaping. There
	// is intentionally no length bound here — scope is trusted operator input, not
	// attacker-controlled, so a pathological multi-KB entry would only bloat the
	// operator's own prompt. Add a length cap at config.go scope validation if
	// accidental prompt bloat becomes a concern.
	return "\n\n## Review Focus\nConcentrate this review on the following categories: " +
		strings.Join(cats, ", ") + ". Prioritize findings in these areas. This is a focus " +
		"hint, not a hard limit — still report any genuinely critical issue you find outside them."
}

// Per-payload-mode scope rules injected into persona prompts as {{.ScopeRule}}.
// diff and blocks constrain findings to the changed regions; files mode widens
// visibility to whole files and routes pre-existing issues to the out-of-scope
// category so the reconciler annotates rather than promotes them.
const (
	scopeChangedOnly = "Review only the changed regions. The payload shows you the change in context, " +
		"but a finding whose FILE:LINE falls outside the changed lines will be discarded before it " +
		"reaches the report when grounding is active (git-range reviews) — it is not enough for the " +
		"code to merely be visible in the surrounding context. If you must flag a genuine pre-existing " +
		"issue in unchanged code, give it CATEGORY out-of-scope so the reconciler annotates rather than " +
		"discards it. Stay on the diff."

	scopeFiles = "Full head-version content of each changed file is provided, so pre-existing issues " +
		"in unchanged regions may be visible. Focus your findings on the changed regions. A finding whose " +
		"FILE:LINE falls outside the changed lines will be discarded before it reaches the report when " +
		"grounding is active (git-range reviews) — it is not enough for the code to merely be visible in " +
		"the surrounding context. You may note a pre-existing issue separately, but give it CATEGORY " +
		"`out-of-scope` so the reconciler annotates rather than promotes it."
)

// categoryEnumeration renders the closed CATEGORY vocabulary.
//
// STUB — wrong answer on purpose (epic 35.16.4 T2, RED stage).
func categoryEnumeration() string { return "" }

// ScopeRule returns the scope instruction for a payload mode. diff and blocks
// share the changed-only rule (function-context expansion does not widen the
// review scope); files mode uses the wider rule. An unknown mode falls back to
// the conservative changed-only rule.
func ScopeRule(mode PayloadMode) string {
	switch mode {
	case ModeFiles:
		return scopeFiles
	case ModeDiff, ModeBlocks:
		return scopeChangedOnly
	default:
		return scopeChangedOnly
	}
}

// ScopeRuleForPayload returns the scope instruction for the payload text an
// agent will actually receive, accounting for per-file escalation (Epic 35.1).
//
// A persona prompt carries exactly one {{.ScopeRule}}, but escalation means one
// payload can mix modes. If ANY file in this payload was rendered as a full
// file, its unchanged regions are in front of the reviewer, so the payload gets
// the wider files-mode rule. Telling a reviewer to stay on the diff while
// handing it whole files is the failure mode worth avoiding; the reverse (the
// wider rule over a mostly-diff payload) merely permits findings the grounding
// gate already filters.
//
// The decision is made from the text rather than from []FileEntry because the
// chunked review path splits a payload into text chunks before rendering, and a
// chunk that contains no escalated file should keep the narrow rule. Detection
// keys on a files-mode header at the start of a line: inside a unified diff
// every content line carries a +/-/space prefix, so a header can never appear at
// column 0 unless a file really was rendered in files mode.
func ScopeRuleForPayload(mode PayloadMode, text string) string {
	if containsFilesHeader(text) {
		return scopeFiles
	}
	return ScopeRule(mode)
}

// containsFilesHeader reports whether text has a files-mode header at the start
// of any line.
func containsFilesHeader(text string) bool {
	if strings.HasPrefix(text, filesHeaderPrefix) {
		return true
	}
	return strings.Contains(text, "\n"+filesHeaderPrefix)
}
