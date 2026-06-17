package log

import (
	"encoding/base64"
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
//
// The token body is anchored to the token charset ([A-Za-z0-9._-], the base64url
// + dotted-segment alphabet real keys/JWTs use) rather than a greedy \S+: in a
// JSON-formatted log line a token is immediately followed by structural
// punctuation (`","next":...`), and \S+ would consume the closing quote and the
// following fields, corrupting the line.
var (
	bearerTokenPattern = regexp.MustCompile(`(?i)Bearer(?:\s+|%20)[A-Za-z0-9._-]+`)
	skKeyPattern       = regexp.MustCompile(`(?i)sk-[A-Za-z0-9._-]+`)
)

// Redactor scrubs secrets and absolute paths from log messages before they are
// emitted. It is immutable after construction and safe for concurrent use: all
// state (review root and the precomputed secret forms) is read-only, and Redact
// holds no mutable state.
type Redactor struct {
	root string
	// secretForms holds every distinct, non-empty form of each configured secret
	// to scrub — literal plus transport encodings — precomputed once at
	// construction so Redact does no per-call encoding allocation.
	secretForms []string
}

// NewRedactor builds a Redactor. reviewRoot, when non-empty, causes absolute
// paths under it to be rendered relative to it; secrets are exact values
// (e.g. registry API keys) scrubbed in their literal, URL-query-escaped,
// URL-path-escaped, and base64 (StdEncoding) forms. An empty reviewRoot disables
// path relativization; bearer/sk- scrubbing always applies regardless of
// configured secrets.
func NewRedactor(reviewRoot string, secrets ...string) *Redactor {
	var forms []string
	seen := make(map[string]struct{})
	add := func(v string) {
		if v == "" {
			return
		}
		if _, dup := seen[v]; dup {
			return
		}
		seen[v] = struct{}{}
		forms = append(forms, v)
	}
	for _, s := range secrets {
		if s == "" {
			continue
		}
		add(s)
		add(url.QueryEscape(s))
		add(url.PathEscape(s))
		add(base64.StdEncoding.EncodeToString([]byte(s)))
	}
	return &Redactor{
		root:        reviewRoot,
		secretForms: forms,
	}
}

// Redact returns msg with configured secrets, bearer/sk- shaped tokens, and
// absolute paths under the review root removed or relativized. The order is:
// explicit secrets (literal + URL-encoded), then generic token shapes, then
// path relativization.
//
// Configured secrets are matched VERBATIM (case-sensitive) in their literal,
// URL-query-escaped, URL-path-escaped, and base64 (StdEncoding) forms (all
// precomputed at construction). This is intentional: opaque secrets are echoed
// by upstream providers verbatim or omitted, never case-mangled, and case-folding
// a short secret would over-redact unrelated text that merely shares its letters.
// A secret echoed in a genuinely altered casing is therefore not caught by the
// exact-secret pass; the case-insensitive bearer/sk- shape patterns below are
// the backstop for the token shapes providers actually emit.
func (r *Redactor) Redact(msg string) string {
	out := msg

	// Verbatim, case-sensitive matches over the precomputed literal + transport
	// encodings — see the Redact doc comment for why casing is not folded.
	for _, form := range r.secretForms {
		out = strings.ReplaceAll(out, form, "[redacted]")
	}

	// Fast path: the regex engine is far costlier than a single byte scan, and the
	// overwhelming majority of log lines carry no token marker. Only run each
	// pattern when its literal marker is present under ASCII case folding (matching
	// the (?i) flag for the ASCII tokens these patterns target — keys and bearer
	// tokens are ASCII). This skips the regex engine entirely on common lines.
	if containsFoldASCII(out, "bearer") {
		out = bearerTokenPattern.ReplaceAllString(out, "Bearer [redacted]")
	}
	if containsFoldASCII(out, "sk-") {
		out = skKeyPattern.ReplaceAllString(out, "[redacted]")
	}

	out = relativizePaths(out, r.root)

	return out
}

// containsFoldASCII reports whether sub occurs in s under ASCII case folding,
// without allocating (unlike strings.Contains(strings.ToLower(s), sub)). sub
// must be lowercase ASCII. It is a cheap prefilter guarding the redaction
// regexes; sub is a short literal so the O(len(s)*len(sub)) scan stays linear in
// practice.
func containsFoldASCII(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		j := 0
		for ; j < len(sub); j++ {
			c := s[i+j]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != sub[j] {
				break
			}
		}
		if j == len(sub) {
			return true
		}
	}
	return false
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
