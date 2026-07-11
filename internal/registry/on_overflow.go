package registry

import "strings"

// validOnOverflowPolicies is the F4 degradation ladder (plan 19.10). When a
// per-agent payload still exceeds its effective budget after per-model sizing,
// on_overflow selects how to degrade:
//
//   - "chunk"    (default) deliver the whole diff across window-sized chunks via
//     the Epic 14.3 chunker made window-aware — no content dropped;
//   - "truncate" shed the lowest-priority tail and flag it — lossy, last-resort;
//   - "fallback" route the slot to a fallback model (provenance recorded);
//   - "fail"     hard-fail loudly.
//
// All four are CONFIG-valid now so operators can declare intent, but only
// "chunk" and "truncate" dispatch in this sprint; "fallback"/"fail" are
// recognized-but-gated and error clearly at dispatch (Task 04) when their
// prerequisites are unmet (AC4). Dispatch enforcement lives in internal/fanout,
// not here — this package only parses, validates, and resolves the value.
var validOnOverflowPolicies = map[string]bool{
	"chunk":    true,
	"truncate": true,
	"fallback": true,
	"fail":     true,
}

// onOverflowValid reports whether a configured on_overflow policy is acceptable
// at load. Empty or whitespace-only is treated as unset (valid — it falls
// through to the next precedence tier), mirroring reviewStrategyValid.
func onOverflowValid(value string) bool {
	v := strings.TrimSpace(value)
	return v == "" || validOnOverflowPolicies[v]
}
