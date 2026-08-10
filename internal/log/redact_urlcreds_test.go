package log

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRedact_ScrubsURLEmbeddedCredentials covers the DOCKER_HOST shape.
//
// warnOSLevelFallbackEngaged logs the docker preflight cause, and that cause
// carries the daemon's raw stderr and the joined `docker run` argv. An operator
// pointing DOCKER_HOST at a remote daemon with inline credentials
// (tcp://user:pass@host:2376) therefore had the password reach stderr and CI logs
// verbatim: the warning fires under the BASE redactor — cli/main.go's
// NewRedactor("") — because resolveExec runs before correlateAndRedact installs
// the review-scoped one, and the base redactor only knew Bearer/sk- shapes.
//
// internal/verify/exec.go's own truncateCause comment already named this as the
// real fix: "credentials embedded in a URL would pass straight through into
// stderr and CI logs ... Bounding it is the cheap half; the redactor is the real
// fix."
func TestRedact_ScrubsURLEmbeddedCredentials(t *testing.T) {
	r := NewRedactor("") // the base redactor: no review root, no configured secrets

	for _, tc := range []struct {
		name        string
		in          string
		mustNotHave string
		mustHave    []string
	}{
		{
			name:        "docker host with inline credentials",
			in:          `sandbox preflight: docker daemon unreachable: Cannot connect to tcp://admin:hunter2@dockerhost:2376`,
			mustNotHave: "hunter2",
			mustHave:    []string{"tcp://", "dockerhost:2376", "docker daemon unreachable"},
		},
		{
			name:        "https with credentials mid-sentence keeps the tail",
			in:          `fetching https://bob:s3cr3t@registry.example.com/v2/ and then continuing`,
			mustNotHave: "s3cr3t",
			mustHave:    []string{"registry.example.com/v2/", "and then continuing"},
		},
		{
			name:        "empty password still scrubbed",
			in:          `ssh://root:@10.0.0.5/repo`,
			mustNotHave: "root:",
			mustHave:    []string{"10.0.0.5/repo"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Redact(tc.in)
			assert.NotContains(t, got, tc.mustNotHave, "the credential must not survive redaction")
			for _, want := range tc.mustHave {
				assert.Contains(t, got, want,
					"redaction must scrub the credential, not swallow the operator's diagnostic")
			}
		})
	}
}

// TestRedact_LeavesCredentiallessURLsIntact is the false-positive guard, and the
// case that decides the pattern's shape.
//
// `ssh://git@github.com/owner/repo` is ubiquitous in this tool's own output and
// carries NO secret — `git` is a well-known username, not a credential. Requiring
// a `user:pass` colon pair is what keeps these untouched; a pattern matching any
// `user@host` would redact half the repository URLs atcr prints.
func TestRedact_LeavesCredentiallessURLsIntact(t *testing.T) {
	r := NewRedactor("")

	for _, in := range []string{
		"cloning ssh://git@github.com/samestrin/atcr",
		"GET https://api.github.com/repos/samestrin/atcr",
		"unix:///var/run/docker.sock",
		"see https://example.com/docs#user:guide",
		"plain text with no scheme at all",
	} {
		assert.Equal(t, in, r.Redact(in), "a URL carrying no inline credential must render byte-identically")
	}
}

// TestRedact_URLCredentialScrubIsRootIndependent pins that this pattern belongs
// to the always-on tier.
//
// Path relativization is gated on a review root, and the redactor active when the
// fallback warning fires has none. A credential-scrubbing pattern gated the same
// way would therefore be off in exactly the place the leak was found, so it sits
// with bearerTokenPattern/skKeyPattern instead: unconditional, every redactor,
// every root.
func TestRedact_URLCredentialScrubIsRootIndependent(t *testing.T) {
	const in = `connecting to tcp://svc:p4ssw0rd@10.1.2.3:2376`
	for _, r := range []*Redactor{
		NewRedactor(""),
		NewRedactor("/some/review/root"),
		NewRedactor("/some/review/root", "an-unrelated-configured-secret"),
	} {
		got := r.Redact(in)
		assert.NotContains(t, got, "p4ssw0rd")
		assert.Contains(t, got, "10.1.2.3:2376")
	}
}

// TestRedact_ScrubsEveryURLCredentialOccurrence guards against a pattern that
// stops after the first match — a joined `docker run` argv can carry the endpoint
// more than once.
func TestRedact_ScrubsEveryURLCredentialOccurrence(t *testing.T) {
	r := NewRedactor("")
	in := `docker -H tcp://a:secret1@h1:2376 run --add-host x=tcp://b:secret2@h2:2376`
	got := r.Redact(in)

	assert.NotContains(t, got, "secret1")
	assert.NotContains(t, got, "secret2")
	assert.Equal(t, 2, strings.Count(got, "[redacted]@"),
		"both endpoints must be scrubbed, not just the first")
}
