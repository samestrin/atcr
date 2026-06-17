package log

import (
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
