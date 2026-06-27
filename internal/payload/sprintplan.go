package payload

import (
	"os"
	"strings"
	"unicode/utf8"
)

// MaxSprintPlanBytes is the fixed byte ceiling applied to sprint-plan content
// before it is wrapped in a SCOPE CONSTRAINT block and prepended to every agent
// prompt. The constraint is uncounted by ApplyByteBudget (which runs during
// payload build, before render), so an uncapped plan would inflate every agent
// prompt past payload_byte_budget. At 16 KiB it is ~3% of the 512 KiB default
// budget — large enough for any real sprint/epic plan, small enough that the
// extra bytes cannot meaningfully bloat a prompt (Epic 12.2 AC6).
const MaxSprintPlanBytes int64 = 16384

// ReadSprintPlan loads the sprint-plan file at path, returning its raw content.
// It distinguishes three cases so the caller can scope a review correctly:
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
func ReadSprintPlan(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		// A missing file is "no plan", not an error: the review proceeds diff-wide
		// (AC2). Any other read failure (permission, directory, IO) is surfaced so
		// the caller can warn (AC3).
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

// ScopeConstraint formats content as a SCOPE CONSTRAINT block to prepend to a
// reviewer's payload, immediately before the diff. It returns the block and
// whether the plan content was truncated to fit MaxSprintPlanBytes.
//
// Empty or whitespace-only content yields ("", false): there is nothing to
// constrain, so the review proceeds unconstrained (Epic 12.2 AC2). Otherwise the
// content is capped at MaxSprintPlanBytes on a UTF-8 rune boundary (so the block
// is never invalid UTF-8) and wrapped with the constraint instruction. The block
// is a SOFT scope, mirroring ScopeFocus: it steers reviewers toward in-scope
// changes but explicitly preserves an escape hatch for genuinely critical
// out-of-scope issues, so a real security/data-loss bug is never silently lost.
//
// Like ScopeFocus, content is concatenated verbatim with no per-entry escaping:
// the plan is trusted operator input, and the payload is injected as template
// data (never re-parsed), so plan text containing template syntax is inert.
func ScopeConstraint(content string) (block string, truncated bool) {
	plan := strings.TrimSpace(content)
	if plan == "" {
		return "", false
	}
	plan, truncated = capUTF8(plan, int(MaxSprintPlanBytes))
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

// capUTF8 truncates s to at most max bytes without splitting a multibyte rune,
// reporting whether a truncation occurred. When s exceeds max it backs the cut
// off to the nearest preceding rune boundary, so the result is always valid
// UTF-8 even if that drops a few bytes below max.
func capUTF8(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}
