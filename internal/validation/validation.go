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
// git-check-ref-format rules — no "..", no ASCII control chars (0x00–0x1f, 0x7f),
// no metacharacters (~^: space \ ? * [), and no leading "-". Length bound ≤ 255.
// It is intentionally NOT applied to the --base/--head flags, which take
// git *revisions* (HEAD^, HEAD~3, @{...}) that legitimately use these characters.
func GitRef(ref string) error {
	if ref == "" {
		return &ValidationError{"git ref", ref, "must not be empty"}
	}
	// No .., no ASCII control chars (0x00–0x1f/0x7f), no shell/git metacharacters, no leading dash.
	if strings.Contains(ref, "..") || strings.HasPrefix(ref, "-") ||
		strings.IndexFunc(ref, func(r rune) bool {
			return r < 0x20 || r == 0x7f || strings.ContainsRune("~^: \\?*[", r)
		}) >= 0 {
		return &ValidationError{"git ref", ref, "contains invalid characters"}
	}
	if len(ref) > 255 {
		return &ValidationError{"git ref", ref, "must be <= 255 characters"}
	}
	return nil
}

// FilePath validates a file path: non-empty, no path traversal (".."), and no
// reference to the system directories /etc, /proc, /sys (and their macOS canonical
// equivalents /private/etc and /private/var, since macOS /etc and /var are symlinks).
// Callers that accept relative paths should resolve them to an absolute, cleaned form
// (filepath.Abs) before validating, so a legitimate relative path is not rejected.
//
// The Unix guard covers forward-slash absolute paths; Windows volume paths
// (drive-letter system dirs such as C:\Windows or C:\Program Files) are rejected
// separately via a host-independent string check, so the protection holds on
// Windows as well as Unix.
func FilePath(path string) error {
	if path == "" {
		return &ValidationError{"file path", path, "must not be empty"}
	}
	// No path traversal: match only path segments that are exactly "..", not
	// filenames that happen to contain ".." as a substring (e.g. my..file).
	if path == ".." || strings.HasPrefix(path, "../") ||
		strings.Contains(path, "/../") || strings.HasSuffix(path, "/..") {
		return &ValidationError{"file path", path, "must not contain .."}
	}
	// No paths under the system directories /etc, /proc, /sys or their macOS
	// canonical paths (/private/etc, /private/var — macOS /etc and /var are symlinks).
	// Match a directory boundary (exact dir or "<dir>/" prefix).
	for _, sysDir := range []string{"/etc", "/proc", "/sys", "/private/etc", "/private/var"} {
		if path == sysDir || strings.HasPrefix(path, sysDir+"/") {
			return &ValidationError{"file path", path, "must not reference system directories"}
		}
	}
	// Windows volume/drive-letter system paths (C:\Windows, C:\Program Files).
	// The Unix guard above does not see these, so on Windows the system-dir
	// protection would otherwise be inert. Matched by host-independent string
	// comparison so the guard holds regardless of the running OS.
	if windowsSystemPath(path) {
		return &ValidationError{"file path", path, "must not reference system directories"}
	}
	return nil
}

// windowsSystemPath reports whether path is a Windows volume path under a system
// directory (\Windows, \Program Files). It requires a drive-letter prefix (C:)
// so a Unix path that merely contains "/windows" is not falsely rejected, and it
// is case-insensitive and separator-agnostic to match Windows path semantics.
func windowsSystemPath(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	if c := path[0]; (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
		return false
	}
	rest := strings.ToLower(strings.ReplaceAll(path[2:], "\\", "/"))
	for _, sysDir := range []string{"/windows", "/program files", "/program files (x86)"} {
		if rest == sysDir || strings.HasPrefix(rest, sysDir+"/") {
			return true
		}
	}
	return false
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

// validSeverities is the set of accepted severity levels (case-normalized).
var validSeverities = map[string]bool{"LOW": true, "MEDIUM": true, "HIGH": true, "CRITICAL": true}

// Severity validates a severity level (case-insensitive).
func Severity(s string) error {
	if !validSeverities[strings.ToUpper(s)] {
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
