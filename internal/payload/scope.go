package payload

// Per-payload-mode scope rules injected into persona prompts as {{.ScopeRule}}.
// diff and blocks constrain findings to the changed regions; files mode widens
// visibility to whole files and routes pre-existing issues to the out-of-scope
// category so the reconciler annotates rather than promotes them.
const (
	scopeChangedOnly = "Review only the changed regions. The payload shows you the change in context, " +
		"but findings on lines outside the changed range are out of scope and will be flagged " +
		"during reconciliation. Stay on the diff."

	scopeFiles = "Full head-version content of each changed file is provided, so pre-existing issues " +
		"in unchanged regions may be visible. Focus your findings on the changed regions. You may note " +
		"a pre-existing issue separately, but give it CATEGORY `out-of-scope` so the reconciler annotates " +
		"rather than promotes it."
)

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
