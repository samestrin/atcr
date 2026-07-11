package payload

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// The sprint-plan byte ceiling is caller-supplied (plan 19.10 F9): it is
// resolved from max_sprint_plan_bytes through internal/registry's precedence
// chain and passed into ReadSprintPlan/ScopeConstraint as maxBytes, so this
// package stays free of any internal/registry dependency (avoiding an import
// cycle). The ceiling bounds how much plan text is wrapped in a SCOPE CONSTRAINT
// block and prepended to every agent prompt; because the constraint is uncounted
// by ApplyByteBudget (which runs during payload build, before render), an
// uncapped plan would inflate every prompt past payload_byte_budget. The
// embedded default (registry.DefaultMaxSprintPlanBytes, 64 KiB) is large enough
// for any real sprint/epic plan; because the constraint is added after budget
// accounting it bounds the inflation rather than preventing overflow outright
// (Epic 12.2 AC6).

// maxSprintPlanReadCeiling is a hard upper bound on how many bytes
// ReadSprintPlan will ever buffer, regardless of what maxBytes the caller
// supplies. It protects against a misconfigured (or bypassed-validation) huge
// maxBytes that would otherwise exhaust memory via io.ReadAll. The value is
// intentionally generous (1 MiB) compared with the 64 KiB production default so
// legitimate operator overrides are not silently truncated at the default.
const maxSprintPlanReadCeiling int64 = 1 << 20

// ReadSprintPlan loads the sprint-plan file at path, returning its raw content.
// maxBytes is the caller-supplied byte ceiling (plan 19.10 F9): only maxBytes+1
// bytes are ever buffered so a huge or non-regular file cannot exhaust memory,
// and the extra byte lets ScopeConstraint detect an oversized source. It
// distinguishes three cases so the caller can scope a review correctly:
//
//   - path is empty (the --sprint-plan flag was not set) or the file does not
//     exist → ("", nil): no plan, the review proceeds diff-wide (Epic 12.2 AC2).
//   - the path exists but cannot be read (permission error, it is a directory,
//     etc.) → ("", err): the caller warns on stderr and proceeds without a
//     constraint rather than crashing the review (Epic 12.2 AC3).
//   - a readable file → (content, nil): its bytes verbatim, for ScopeConstraint
//     to sanitize, cap, and format.
//
// The plan file is trusted local operator input (Epic 12.2 Security
// Considerations), so no path-traversal or content sandboxing is performed here.
func ReadSprintPlan(path string, maxBytes int64) (content string, err error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		// A missing file is "no plan", not an error: the review proceeds diff-wide
		// (AC2). Any other read failure (permission, directory, IO) is surfaced so
		// the caller can warn (AC3).
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	// Bound memory use even if the path points at a huge or non-regular file: only
	// maxBytes+1 bytes are ever buffered. The extra byte lets ScopeConstraint
	// detect that the source was oversized and surface truncation.
	//
	// Clamp to a hard ceiling so a misconfigured huge maxBytes cannot exhaust
	// memory, and guard maxBytes+1 against int64 overflow (e.g. math.MaxInt64
	// wraps to negative and makes LimitReader read zero bytes).
	if maxBytes > maxSprintPlanReadCeiling {
		maxBytes = maxSprintPlanReadCeiling
	}
	if maxBytes < 0 {
		maxBytes = 0
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ScopeConstraint formats content as a SCOPE CONSTRAINT block to prepend to a
// reviewer's payload, immediately before the diff. It returns the block and
// whether the plan content was truncated to fit the caller-supplied maxBytes
// ceiling (plan 19.10 F9).
//
// Empty or whitespace-only content yields ("", false): there is nothing to
// constrain, so the review proceeds unconstrained (Epic 12.2 AC2). Otherwise the
// content is capped at maxBytes on a UTF-8 rune boundary (so the block is never
// invalid UTF-8) and wrapped with the constraint instruction. The block is a
// SOFT scope, mirroring ScopeFocus: it steers reviewers toward in-scope changes
// but explicitly preserves an escape hatch for genuinely critical out-of-scope
// issues, so a real security/data-loss bug is never silently lost.
//
// Cache-invalidation limitation for oversized plans: only the first maxBytes of
// the plan are reflected in the rendered prompt (and therefore in the diff-cache
// key). Two distinct plans that share the same leading bytes produce identical
// SCOPE CONSTRAINT blocks and identical cache keys, so an edit that changes only
// content beyond the cap will not invalidate cached review results. Keeping plans
// under the cap is the recommended fix.
//
// Like ScopeFocus, the plan is trusted operator input and template syntax is
// inert (payload is injected as data, never re-parsed). As a defense-in-depth
// measure, any BEGIN/END framing markers found in the plan body are neutralized
// before embedding so that machine-generated plan content crossing an untrusted
// boundary cannot close the framing block early and inject instructions.
func ScopeConstraint(content string, maxBytes int64) (block string, truncated bool) {
	plan := strings.TrimSpace(content)
	if plan == "" {
		return "", false
	}
	plan, truncated = capUTF8(plan, int(maxBytes))
	// Defense-in-depth: neutralize any marker lines that could close the framing
	// block early and inject instructions to the reviewer model. Sprint plans are
	// increasingly machine-generated from issue/PR text that crosses an untrusted
	// boundary (Epic 12.2 Security Considerations).
	plan = strings.ReplaceAll(plan, "----- BEGIN SPRINT PLAN -----", "-- BEGIN SPRINT PLAN --")
	plan = strings.ReplaceAll(plan, "----- END SPRINT PLAN -----", "-- END SPRINT PLAN --")
	var b strings.Builder
	b.WriteString("## SCOPE CONSTRAINT\n")
	b.WriteString("The sprint/epic plan below defines the active work items for this review. ")
	b.WriteString("Constrain your findings to files and changes directly related to these work items. ")
	b.WriteString("Suppress findings for unrelated changes that merely happen to appear in the diff — ")
	b.WriteString("for example dependency bumps, formatter or whitespace-only reformatting, and mechanical ")
	b.WriteString("refactors not described in the plan. This is a scope hint, not a hard limit: still report ")
	b.WriteString("any genuinely critical issue (security, data loss, crash) even if it falls outside the plan.\n\n")
	b.WriteString("----- BEGIN SPRINT PLAN -----\n")
	b.WriteString(plan)
	b.WriteString("\n----- END SPRINT PLAN -----\n\n")
	return b.String(), truncated
}

// capUTF8 scrubs invalid UTF-8 bytes from s, then truncates it to at most max bytes without splitting a multibyte rune,
// reporting whether a truncation occurred. The returned string is always valid
// UTF-8.
func capUTF8(s string, max int) (string, bool) {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}
