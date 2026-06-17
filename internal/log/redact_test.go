package log

import (
	"encoding/base64"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestRedact_ExactSecret(t *testing.T) {
	r := NewRedactor("", "supersecretvalue")
	out := r.Redact("the token is supersecretvalue, do not leak")
	if strings.Contains(out, "supersecretvalue") {
		t.Fatalf("secret leaked: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("expected redaction marker: %q", out)
	}
}

func TestRedact_URLEncodedSecret(t *testing.T) {
	secret := "a b/c=d"
	enc := url.QueryEscape(secret)
	if enc == secret {
		t.Fatal("test precondition: encoded form must differ from raw")
	}
	r := NewRedactor("", secret)
	out := r.Redact("payload contains " + enc + " encoded")
	if strings.Contains(out, enc) {
		t.Fatalf("URL-encoded secret leaked: %q", out)
	}
}

// TestRedact_Base64EncodedSecret verifies a configured secret echoed in
// base64 (e.g. an Authorization header value) is scrubbed, not just its literal
// and URL-query forms.
func TestRedact_Base64EncodedSecret(t *testing.T) {
	secret := "supersecretvalue"
	enc := base64.StdEncoding.EncodeToString([]byte(secret))
	r := NewRedactor("", secret)
	out := r.Redact("Authorization: Basic " + enc)
	if strings.Contains(out, enc) {
		t.Fatalf("base64-encoded secret leaked: %q", out)
	}
}

// TestRedact_PathEscapedSecret verifies a secret transformed via url.PathEscape
// (a different escaping than QueryEscape) is scrubbed.
func TestRedact_PathEscapedSecret(t *testing.T) {
	secret := "my secret tok"
	enc := url.PathEscape(secret)
	if enc == secret || enc == url.QueryEscape(secret) {
		t.Fatal("test precondition: PathEscape form must differ from raw and QueryEscape")
	}
	r := NewRedactor("", secret)
	out := r.Redact("path segment " + enc + " seen")
	if strings.Contains(out, enc) {
		t.Fatalf("path-escaped secret leaked: %q", out)
	}
}

func TestRedact_BearerToken(t *testing.T) {
	r := NewRedactor("")
	out := r.Redact("Authorization: Bearer abc123XYZtoken")
	if strings.Contains(out, "abc123XYZtoken") {
		t.Fatalf("bearer token leaked: %q", out)
	}
	if !strings.Contains(out, "Bearer [redacted]") {
		t.Fatalf("expected bearer redaction: %q", out)
	}
}

// TestRedact_URLEncodedBearerToken verifies a percent-encoded space between
// "Bearer" and the token (Bearer%20<token>, as it appears in a logged URL or
// error body) is still scrubbed — the pattern must not require literal whitespace.
func TestRedact_URLEncodedBearerToken(t *testing.T) {
	r := NewRedactor("")
	out := r.Redact("GET /x?h=Bearer%20abc123XYZtoken failed")
	if strings.Contains(out, "abc123XYZtoken") {
		t.Fatalf("URL-encoded bearer token leaked: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("expected redaction marker: %q", out)
	}
}

func TestRedact_SKKey(t *testing.T) {
	r := NewRedactor("")
	out := r.Redact("api key sk-abcDEF123456 used")
	if strings.Contains(out, "sk-abcDEF123456") {
		t.Fatalf("sk key leaked: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("expected redaction marker: %q", out)
	}
}

// TestRedact_SKKeyNoOverRedactInJSON verifies the sk- pattern stops at the token
// boundary instead of greedily consuming following JSON punctuation and adjacent
// fields. A greedy \S+ would swallow `","user":"alice"}` and mangle the line.
func TestRedact_SKKeyNoOverRedactInJSON(t *testing.T) {
	r := NewRedactor("")
	out := r.Redact(`{"auth":"sk-abc123DEF","user":"alice"}`)
	if strings.Contains(out, "sk-abc123DEF") {
		t.Fatalf("sk key not redacted: %q", out)
	}
	if !strings.Contains(out, `"user":"alice"`) {
		t.Fatalf("adjacent JSON field over-redacted: %q", out)
	}
}

// TestRedact_BearerNoOverRedactInJSON verifies the bearer pattern likewise stops
// at the token charset boundary.
func TestRedact_BearerNoOverRedactInJSON(t *testing.T) {
	r := NewRedactor("")
	out := r.Redact(`{"hdr":"Bearer abc.def-123","next":"keepme"}`)
	if strings.Contains(out, "abc.def-123") {
		t.Fatalf("bearer token not redacted: %q", out)
	}
	if !strings.Contains(out, `"next":"keepme"`) {
		t.Fatalf("adjacent JSON field over-redacted: %q", out)
	}
}

func TestRedact_SKKeyCaseInsensitive(t *testing.T) {
	r := NewRedactor("")
	for _, in := range []string{"SK-ABCdef123456", "Sk-mixedCase789"} {
		out := r.Redact("token " + in + " here")
		if strings.Contains(out, in) {
			t.Fatalf("case-variant sk key leaked: %q", out)
		}
	}
}

// Mirrors TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens in llmclient:
// a foreign bearer token and an sk- key in the same message are both scrubbed
// even when neither is the configured secret.
func TestRedact_ForeignBearerAndSKTokens(t *testing.T) {
	r := NewRedactor("", "configuredkey")
	out := r.Redact("upstream said Bearer foreignToken9 and sk-foreignKey42 failed")
	if strings.Contains(out, "foreignToken9") {
		t.Fatalf("foreign bearer token leaked: %q", out)
	}
	if strings.Contains(out, "sk-foreignKey42") {
		t.Fatalf("foreign sk key leaked: %q", out)
	}
}

func TestRedact_NoSecretsStillScrubsBearerAndSK(t *testing.T) {
	r := NewRedactor("") // no configured secrets
	out := r.Redact("Bearer leakme and sk-leakme2")
	if strings.Contains(out, "leakme") {
		t.Fatalf("bearer/sk not scrubbed without configured secrets: %q", out)
	}
}

func TestRedact_AbsolutePathRelativized(t *testing.T) {
	r := NewRedactor("/home/u/repo")
	out := r.Redact("error in /home/u/repo/internal/log/redact.go:42")
	if strings.Contains(out, "/home/u/repo") {
		t.Fatalf("absolute path not relativized: %q", out)
	}
	if !strings.Contains(out, "internal/log/redact.go:42") {
		t.Fatalf("relative path lost: %q", out)
	}
}

func TestRedact_TrailingSlashRoot(t *testing.T) {
	r := NewRedactor("/home/u/repo/")
	out := r.Redact("at /home/u/repo/cmd/atcr/main.go")
	if strings.Contains(out, "/home/u/repo") {
		t.Fatalf("trailing-slash root not handled: %q", out)
	}
	if !strings.Contains(out, "cmd/atcr/main.go") {
		t.Fatalf("relative path lost: %q", out)
	}
}

func TestRedact_PathOutsideRootUnchanged(t *testing.T) {
	r := NewRedactor("/home/u/repo")
	msg := "reading /etc/hosts for config"
	if out := r.Redact(msg); out != msg {
		t.Fatalf("path outside root was modified: %q", out)
	}
}

func TestRedact_PartialPathOverlapUnchanged(t *testing.T) {
	// /home/u/repository must NOT match root /home/u/repo (no separator boundary).
	r := NewRedactor("/home/u/repo")
	msg := "see /home/u/repository/file.go"
	if out := r.Redact(msg); out != msg {
		t.Fatalf("partial path overlap incorrectly stripped: %q", out)
	}
}

func TestRedact_NoRootPathNoOp(t *testing.T) {
	r := NewRedactor("")
	msg := "absolute /var/data/file stays put"
	if out := r.Redact(msg); out != msg {
		t.Fatalf("path modified when no root configured: %q", out)
	}
}

func TestRedact_RootSlashNoOp(t *testing.T) {
	// Pathological root "/" must not strip every slash from the message.
	r := NewRedactor("/")
	msg := "path /a/b/c remains"
	if out := r.Redact(msg); out != msg {
		t.Fatalf("root \"/\" stripped slashes: %q", out)
	}
}

func TestRedact_MultiplePassesCompose(t *testing.T) {
	r := NewRedactor("/home/u/repo", "mysecret")
	out := r.Redact("mysecret leaked at /home/u/repo/x.go via Bearer tok7 and sk-key8")
	for _, bad := range []string{"mysecret", "/home/u/repo", "tok7", "sk-key8"} {
		if strings.Contains(out, bad) {
			t.Fatalf("composition failed, %q leaked: %q", bad, out)
		}
	}
	if !strings.Contains(out, "x.go") {
		t.Fatalf("relative path lost during composition: %q", out)
	}
}

func TestRedact_EmptyMessage(t *testing.T) {
	r := NewRedactor("/home/u/repo", "secret")
	if out := r.Redact(""); out != "" {
		t.Fatalf("empty message produced output: %q", out)
	}
}

func TestRedact_EmptySecretsIgnored(t *testing.T) {
	r := NewRedactor("", "")
	msg := "nothing to redact here"
	if out := r.Redact(msg); out != msg {
		t.Fatalf("empty secret caused unexpected redaction: %q", out)
	}
}

// TestRedact_PrefilterPreservesCaseVariants guards the ASCII case-fold prefilter:
// uppercase/mixed-case token markers must still trigger the regex pass. A naive
// case-sensitive strings.Contains prefilter would skip these and leak the token.
func TestRedact_PrefilterPreservesCaseVariants(t *testing.T) {
	r := NewRedactor("")
	cases := map[string]string{
		"BEARER UPPERTOKEN1": "UPPERTOKEN1",
		"Bearer MixedTok2":   "MixedTok2",
		"SK-UPPERKEY3":       "UPPERKEY3",
	}
	for in, leak := range cases {
		out := r.Redact(in)
		if strings.Contains(out, leak) {
			t.Fatalf("case-variant token leaked past prefilter: in=%q out=%q", in, out)
		}
	}
}

func TestContainsFoldASCII(t *testing.T) {
	cases := []struct {
		s, sub string
		want   bool
	}{
		{"has Bearer here", "bearer", true},
		{"HAS BEARER", "bearer", true},
		{"no marker", "bearer", false},
		{"SK-key", "sk-", true},
		{"prefix sk", "sk-", false},
		{"anything", "", true},
		{"ab", "abc", false},
	}
	for _, c := range cases {
		if got := containsFoldASCII(c.s, c.sub); got != c.want {
			t.Fatalf("containsFoldASCII(%q,%q)=%v, want %v", c.s, c.sub, got, c.want)
		}
	}
}

func BenchmarkRedact_NoMatch(b *testing.B) {
	r := NewRedactor("/home/u/repo", "configuredsecret")
	msg := "handled request id=12345 status=200 latency=4ms user=alice path=/api/v1/items"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.Redact(msg)
	}
}

func BenchmarkRedact_WithToken(b *testing.B) {
	r := NewRedactor("/home/u/repo", "configuredsecret")
	msg := "auth failed Authorization: Bearer abc123def456 for /home/u/repo/x.go"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.Redact(msg)
	}
}

func TestRedact_ConcurrentSafety(t *testing.T) {
	r := NewRedactor("/home/u/repo", "concurrentsecret")
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				out := r.Redact("concurrentsecret at /home/u/repo/f.go Bearer t sk-k")
				if strings.Contains(out, "concurrentsecret") {
					t.Errorf("race produced leak: %q", out)
					return
				}
			}
		}()
	}
	wg.Wait()
}
