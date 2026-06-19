// Package validation centralizes user-input validation for the atcr CLI: git
// refs, file paths, review IDs, severities, and arbitrary enums. Each validator
// performs no I/O and returns a *ValidationError so callers can wrap it in a
// usage error (exit 2) for a consistent, field-aware message across commands.
package validation

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationError is a user-facing input validation error. It names the field,
// echoes the offending value, and explains the expected format.
type ValidationError struct {
	Field   string
	Value   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s %q: %s", e.Field, e.Value, e.Message)
}

// GitRef validates a git ref name (branch, tag, or SHA). It applies a subset of
// git-check-ref-format rules — no "..", no "~^: " or control chars, and a length
// bound. It is intentionally NOT applied to the --base/--head flags, which take
// git *revisions* (HEAD^, HEAD~3, @{...}) that legitimately use these characters.
func GitRef(ref string) error {
	if ref == "" {
		return &ValidationError{"git ref", ref, "must not be empty"}
	}
	// Git ref rules: no .., no ~, no ^, no :, no space, no control chars.
	if strings.Contains(ref, "..") || strings.ContainsAny(ref, "~^: \t\n") {
		return &ValidationError{"git ref", ref, "contains invalid characters"}
	}
	if len(ref) > 255 {
		return &ValidationError{"git ref", ref, "must be <= 255 characters"}
	}
	return nil
}

// FilePath validates a file path: non-empty, no path traversal (".."), and no
// reference to the system directories /etc, /proc, or /sys. Callers that accept
// relative paths should resolve them to an absolute, cleaned form (filepath.Abs)
// before validating, so a legitimate relative path is not rejected for "..".
//
// The system-directory guard is oriented at Unix absolute paths. On Windows
// (drive-letter/volume paths such as C:\Windows) it is inert; callers targeting
// Windows need volume-aware system-path detection (tracked as technical debt).
func FilePath(path string) error {
	if path == "" {
		return &ValidationError{"file path", path, "must not be empty"}
	}
	// No path traversal. This branch guards callers that validate raw,
	// unresolved input; the production call sites (cmd/atcr review.go and
	// report.go) call FilePath after filepath.Abs, which Cleans the path and so
	// already resolves "..", leaving this check as defense-in-depth there.
	if strings.Contains(path, "..") {
		return &ValidationError{"file path", path, "must not contain .."}
	}
	// No paths under the system directories /etc, /proc, /sys. Match a directory
	// boundary (the exact dir or a "<dir>/" prefix) so siblings like /etcd or
	// /system are not falsely rejected by a bare prefix check.
	for _, sysDir := range []string{"/etc", "/proc", "/sys"} {
		if path == sysDir || strings.HasPrefix(path, sysDir+"/") {
			return &ValidationError{"file path", path, "must not reference system directories"}
		}
	}
	return nil
}

// reviewIDPattern is the review-ID allowlist: alphanumerics, dash, underscore.
var reviewIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ReviewID validates a review ID (alphanumeric, dash, underscore; <= 100 chars).
func ReviewID(id string) error {
	if id == "" {
		return &ValidationError{"review ID", id, "must not be empty"}
	}
	if !reviewIDPattern.MatchString(id) {
		return &ValidationError{"review ID", id, "must contain only alphanumeric characters, dash, and underscore"}
	}
	if len(id) > 100 {
		return &ValidationError{"review ID", id, "must be <= 100 characters"}
	}
	return nil
}

// Severity validates a severity level (case-insensitive).
func Severity(s string) error {
	valid := map[string]bool{"LOW": true, "MEDIUM": true, "HIGH": true, "CRITICAL": true}
	if !valid[strings.ToUpper(s)] {
		return &ValidationError{"severity", s, "must be one of: LOW, MEDIUM, HIGH, CRITICAL"}
	}
	return nil
}

// Enum validates a value against a set of allowed values.
func Enum(field, value string, allowed []string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return &ValidationError{field, value, fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", "))}
}
