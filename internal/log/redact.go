package log

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// bearerTokenPattern and skKeyPattern match secret-shaped tokens so a foreign or
// transformed secret cannot leak even when it is not a configured value. They
// mirror the defense-in-depth scrub in internal/llmclient/client.go so the two
// redaction sites stay consistent. Compiled once at package level (concurrent
// safe; never recompiled per Redact call).
var (
	bearerTokenPattern = regexp.MustCompile(`(?i)Bearer(?:\s+|%20)\S+`)
	skKeyPattern       = regexp.MustCompile(`(?i)sk-\S+`)
)

// Redactor scrubs secrets and absolute paths from log messages before they are
// emitted. It is immutable after construction and safe for concurrent use: all
// state (review root and the configured secret list) is read-only, and Redact
// holds no mutable state.
type Redactor struct {
	root    string
	secrets []string
}

// NewRedactor builds a Redactor. reviewRoot, when non-empty, causes absolute
// paths under it to be rendered relative to it; secrets are exact values
// (e.g. registry API keys) scrubbed in both literal and URL-encoded form. An
// empty reviewRoot disables path relativization; bearer/sk- scrubbing always
// applies regardless of configured secrets.
func NewRedactor(reviewRoot string, secrets ...string) *Redactor {
	return &Redactor{
		root:    reviewRoot,
		secrets: append([]string(nil), secrets...),
	}
}

// Redact returns msg with configured secrets, bearer/sk- shaped tokens, and
// absolute paths under the review root removed or relativized. The order is:
// explicit secrets (literal + URL-encoded), then generic token shapes, then
// path relativization.
func (r *Redactor) Redact(msg string) string {
	out := msg

	for _, s := range r.secrets {
		if s == "" {
			continue
		}
		out = strings.ReplaceAll(out, s, "[redacted]")
		if enc := url.QueryEscape(s); enc != s {
			out = strings.ReplaceAll(out, enc, "[redacted]")
		}
	}

	out = bearerTokenPattern.ReplaceAllString(out, "Bearer [redacted]")
	out = skKeyPattern.ReplaceAllString(out, "[redacted]")

	out = relativizePaths(out, r.root)

	return out
}

// relativizePaths strips occurrences of "<cleanRoot>/" from s so absolute paths
// under the review root render relative. It requires the path separator after
// the root so a path that merely shares a prefix (e.g. "/x/repository" vs root
// "/x/repo") is left untouched. An empty, "." or filesystem-root value is a
// no-op to avoid stripping every separator.
func relativizePaths(s, root string) string {
	if root == "" {
		return s
	}
	clean := filepath.Clean(root)
	if clean == "" || clean == "." || clean == string(filepath.Separator) {
		return s
	}
	return strings.ReplaceAll(s, clean+string(filepath.Separator), "")
}
