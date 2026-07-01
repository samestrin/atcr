package registry

import "strings"

// validReviewStrategies is the Epic 14.3 fan-out strategy enum. "bulk" (the
// default) sends the whole diff in one prompt per persona, keeping API cost
// strictly bounded; "chunked" bin-packs each persona's diff into multiple calls
// each capped by that agent's max_context_lines, trading extra requests for
// smaller per-call context (fewer large-diff hallucinations).
var validReviewStrategies = map[string]bool{"bulk": true, "chunked": true}

// reviewStrategyValid reports whether a configured review_strategy is acceptable
// at load. Empty or whitespace-only is treated as unset (valid — it falls
// through to the next precedence tier), mirroring payloadModeValid.
func reviewStrategyValid(value string) bool {
	v := strings.TrimSpace(value)
	return v == "" || validReviewStrategies[v]
}
