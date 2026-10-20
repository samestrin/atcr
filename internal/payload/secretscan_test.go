package payload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Every planted credential below is assembled at runtime. CI runs
// `gitleaks dir .` over the checked-out tree, so a literal key in this file
// would fail the secret-scan job; concatenation keeps the source clean while
// the scanner under test still sees the whole value.
var (
	plantedAWSKey     = "AKIA" + "QYLPMN5HHHFPZAM2"
	plantedGenericVal = "Zx9Kq2Lm8Np4" + "Rt7Vw3Yb6Cd1Fg5Hj0"
	pemBegin          = "-----BEGIN " + "RSA PRIVATE KEY-----"
	pemEnd            = "-----END " + "RSA PRIVATE KEY-----"
	pemBody           = "MIIEowIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHB7MhgHcTz6sE2I2yPB\n" +
		"+aFDrBz9vFqU4yJmR4Mkm3vXcbYlYzG7t5MPvXk2p6q1JQZ8R4sYvL9dW3hK2nQe7"
	plantedPEM = pemBegin + "\n+" + pemBody + "\n+" + pemEnd
)

// plantedPayload is a diff-shaped review payload carrying an AWS key, a
// generic API-token assignment and a PEM private key, next to the code
// identifiers the guard must leave alone.
func plantedPayload() string {
	return "diff --git a/config.go b/config.go\n" +
		"+const port = 8080\n" +
		"+// port=8080 and user.email are ordinary code, not secrets\n" +
		"+awsKey := \"" + plantedAWSKey + "\"\n" +
		"+api_token = \"" + plantedGenericVal + "\"\n" +
		"+key := `" + plantedPEM + "`\n" +
		"+fmt.Println(user.email)\n"
}

// fakeGitleaksScript stands in for the gitleaks binary. It records its argv
// and stdin, then behaves per FAKE_GITLEAKS_MODE: print the canned report
// (default), exit non-zero, print non-JSON, or hang past the timeout.
const fakeGitleaksScript = `#!/bin/sh
printf '%s\n' "$*" > "$FAKE_GITLEAKS_DIR/args"
cat > "$FAKE_GITLEAKS_DIR/stdin"
case "$FAKE_GITLEAKS_MODE" in
  fail) echo "boom" >&2; exit 2 ;;
  badjson) echo "not json"; exit 0 ;;
  hang) exec sleep 30 ;;
esac
cat "$FAKE_GITLEAKS_DIR/report.json"
`

type fakeFinding struct {
	RuleID string `json:"RuleID"`
	Secret string `json:"Secret"`
	Match  string `json:"Match"`
}

// installFakeGitleaks puts a fake gitleaks first (and only) on PATH and
// returns the directory it records into. The report is the canned gitleaks
// JSON array it prints in the default mode.
func installFakeGitleaks(t *testing.T, mode string, report []fakeFinding) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake gitleaks is a POSIX shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gitleaks"), []byte(fakeGitleaksScript), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// /bin stays on PATH for sh, cat and sleep; the fake still shadows any
	// real gitleaks because dir comes first.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin"+string(os.PathListSeparator)+"/usr/bin")
	t.Setenv("FAKE_GITLEAKS_DIR", dir)
	t.Setenv("FAKE_GITLEAKS_MODE", mode)
	return dir
}

// noGitleaksOnPath forces the built-in ruleset even on a machine that has a
// real gitleaks installed.
func noGitleaksOnPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// hitTexts returns the text each hit's span covers, keyed by kind.
func hitTexts(text string, hits []SecretHit) map[string][]string {
	out := map[string][]string{}
	for _, h := range hits {
		out[h.Kind] = append(out[h.Kind], text[h.Start:h.End])
	}
	return out
}

func assertCovered(t *testing.T, text string, hits []SecretHit, secret string) {
	t.Helper()
	start := strings.Index(text, secret)
	if start < 0 {
		t.Fatalf("test bug: secret not in text")
	}
	end := start + len(secret)
	for _, h := range hits {
		if h.Start <= start && h.End >= end {
			return
		}
	}
	t.Errorf("no hit covers the planted secret at [%d,%d); hits=%+v", start, end, hits)
}

func TestScanPayload_GitleaksPath_UsesBinaryAndLocatesSpans(t *testing.T) {
	text := plantedPayload()
	dir := installFakeGitleaks(t, "", []fakeFinding{
		// RuleIDs the built-in ruleset never emits, so a pass proves the
		// hits came from the binary rather than the fallback.
		{RuleID: "fake-aws", Secret: plantedAWSKey, Match: plantedAWSKey},
		{RuleID: "fake-private-key", Secret: plantedPEM, Match: plantedPEM},
	})

	hits, err := ScanPayloadContext(context.Background(), text, nil)
	if err != nil {
		t.Fatalf("ScanPayloadContext err = %v, want nil", err)
	}
	got := hitTexts(text, hits)
	if want := []string{plantedAWSKey}; !equalStrings(got["fake-aws"], want) {
		t.Errorf("fake-aws spans = %q, want %q", got["fake-aws"], want)
	}
	if want := []string{plantedPEM}; !equalStrings(got["fake-private-key"], want) {
		t.Errorf("fake-private-key spans = %q, want %q", got["fake-private-key"], want)
	}
	if len(hits) != 2 {
		t.Errorf("len(hits) = %d, want 2 (built-in rules must not run on success): %+v", len(hits), hits)
	}

	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if !strings.HasPrefix(string(args), "stdin ") {
		t.Errorf("gitleaks argv = %q, want the stdin subcommand first", args)
	}
	for _, flag := range []string{"--report-format json", "--report-path -", "--exit-code 0"} {
		if !strings.Contains(string(args), flag) {
			t.Errorf("gitleaks argv = %q, missing %q", args, flag)
		}
	}
	if strings.Contains(string(args), "--redact") {
		t.Errorf("gitleaks argv = %q: --redact would hide the Secret needed to locate spans", args)
	}
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	if string(stdin) != text {
		t.Errorf("gitleaks stdin did not receive the payload verbatim")
	}
}

func TestScanPayload_GitleaksPath_MarksEveryOccurrence(t *testing.T) {
	text := "a=" + plantedAWSKey + "\nb=" + plantedAWSKey + "\n"
	installFakeGitleaks(t, "", []fakeFinding{
		{RuleID: "fake-aws", Secret: plantedAWSKey},
		// gitleaks reports each line separately; the duplicate must not
		// produce duplicate spans.
		{RuleID: "fake-aws", Secret: plantedAWSKey},
	})
	hits, err := ScanPayloadContext(context.Background(), text, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %+v, want one per occurrence (2)", hits)
	}
	if out := RedactSecrets(text, hits); strings.Contains(out, plantedAWSKey) {
		t.Errorf("redacted text still holds the key: %q", out)
	}
}

func TestScanPayload_GitleaksPath_CleanReportIsNoHit(t *testing.T) {
	installFakeGitleaks(t, "", []fakeFinding{})
	hits, err := ScanPayloadContext(context.Background(), "port=8080\nuser.email\n", nil)
	if err != nil || len(hits) != 0 {
		t.Fatalf("hits=%+v err=%v, want none", hits, err)
	}
}

func TestScanPayload_BuiltinPath_DetectsPlantedKeyAndPEM(t *testing.T) {
	noGitleaksOnPath(t)
	text := plantedPayload()

	hits, err := ScanPayloadContext(context.Background(), text, nil)
	if err != nil {
		t.Fatalf("built-in path err = %v, want nil (gitleaks absence is not a failure)", err)
	}
	assertCovered(t, text, hits, plantedAWSKey)
	assertCovered(t, text, hits, plantedGenericVal)
	assertCovered(t, text, hits, plantedPEM)

	kinds := map[string]bool{}
	for _, h := range hits {
		kinds[h.Kind] = true
	}
	for _, k := range []string{"aws-access-token", "generic-api-key", "private-key"} {
		if !kinds[k] {
			t.Errorf("missing kind %q in %+v", k, hits)
		}
	}
}

func TestScanPayload_ConvenienceWrapperMatchesContextForm(t *testing.T) {
	noGitleaksOnPath(t)
	text := plantedPayload()
	want, _ := ScanPayloadContext(context.Background(), text, nil)
	if got := ScanPayload(text); len(got) != len(want) || len(got) == 0 {
		t.Errorf("ScanPayload = %+v, want %+v", got, want)
	}
}

func TestBuiltinRules_EachTokenShape(t *testing.T) {
	cases := map[string]string{
		"github-token":        "ghp_" + strings.Repeat("aB3dE5fG7h", 3) + "aB3dE5",
		"github-fine-grained": "github_pat_" + strings.Repeat("aB3dE5fG7h", 8) + "aB",
		"stripe-access-token": "sk_" + "live_" + "4eC39HqLyjWDarjtT1zdp7dc",
		"slack-token":         "xoxb-" + "123456789012-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx",
		"openai-api-key":      "sk-" + "proj-" + "Ab3De5Fg7Hi9Jk1Lm3No5Pq7Rs9Tu",
	}
	for kind, secret := range cases {
		t.Run(kind, func(t *testing.T) {
			text := "x := \"" + secret + "\"\n"
			hits := scanBuiltin(text)
			assertCovered(t, text, hits, secret)
		})
	}
}

func TestBuiltinRules_PEMHeaderWithoutEndStillRedactsBody(t *testing.T) {
	// A diff hunk can cut a key block off before its END line; the header
	// alone must still be caught and the key material after it covered.
	text := "+" + pemBegin + "\n+" + pemBody + "\n+func next() {}\n"
	hits := scanBuiltin(text)
	assertCovered(t, text, hits, pemBegin)
	out := RedactSecrets(text, hits)
	if strings.Contains(out, "MIIEowIBAAKCAQEA") || strings.Contains(out, "aFDrBz9vFqU4") {
		t.Errorf("key body survived redaction: %q", out)
	}
	if !strings.Contains(out, "func next() {}") {
		t.Errorf("code after the key block was swallowed: %q", out)
	}
}

func TestBuiltinRules_NoFalsePositivesOnCodeIdentifiers(t *testing.T) {
	clean := []string{
		"port=8080",
		"user.email",
		"cfg.port = 8080\nfmt.Println(user.email)\n",
		`token = "${GITHUB_TOKEN}"`,
		`api_key = "your-api-key-here"`,
		`secret = "changeme"`,
		`password = "xxxxxxxxxxxxxxxxxxxxxxxx"`,
		`tokenizer := newTokenizer(input)`,
		"-----BEGIN " + "PUBLIC KEY-----",
		`sk-learn is a library`,
	}
	for _, text := range clean {
		if hits := scanBuiltin(text); len(hits) != 0 {
			t.Errorf("scanBuiltin(%q) = %+v, want no hit", text, hits)
		}
	}
	noGitleaksOnPath(t)
	if hits := ScanPayload("port=8080\nuser.email\n"); len(hits) != 0 {
		t.Errorf("ScanPayload(port/user.email) = %+v, want no hit", hits)
	}
}

func TestScanPayload_GitleaksFailureFallsBackToBuiltin(t *testing.T) {
	cases := []struct {
		name   string
		mode   string
		report []fakeFinding
	}{
		{name: "non-zero exit", mode: "fail"},
		{name: "bad json", mode: "badjson"},
		{name: "timeout", mode: "hang"},
		// A finding whose Secret is nowhere in the payload cannot be
		// redacted, so the result is not trustworthy either.
		{name: "unlocatable secret", report: []fakeFinding{{RuleID: "fake", Secret: "not-in-the-payload"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installFakeGitleaks(t, tc.mode, tc.report)
			prev := gitleaksTimeout
			gitleaksTimeout = 300 * time.Millisecond
			t.Cleanup(func() { gitleaksTimeout = prev })
			logger, buf := captureLogger()
			text := plantedPayload()

			startedAt := time.Now()
			hits, err := ScanPayloadContext(context.Background(), text, logger)
			if elapsed := time.Since(startedAt); elapsed > 5*time.Second {
				t.Errorf("scan took %v; the per-call timeout did not bound it", elapsed)
			}

			if !errors.Is(err, ErrSecretScanFailed) {
				t.Fatalf("err = %v, want ErrSecretScanFailed so block can fail closed", err)
			}
			// The fallback hits are the built-in ruleset's.
			assertCovered(t, text, hits, plantedAWSKey)
			assertCovered(t, text, hits, plantedPEM)

			logged := buf.String()
			if !strings.Contains(logged, "level=WARN") || !strings.Contains(logged, "built-in") {
				t.Errorf("want a WARN naming the built-in fallback, got %q", logged)
			}
			for _, secret := range []string{plantedAWSKey, plantedGenericVal, "MIIEowIBAAKCAQEA", "boom"} {
				if strings.Contains(logged, secret) || strings.Contains(err.Error(), secret) {
					t.Errorf("secret material or scanner stderr %q leaked into the log/error", secret)
				}
			}
		})
	}
}

func TestRedactSecrets_ReplacesEachSpanWithPlaceholder(t *testing.T) {
	noGitleaksOnPath(t)
	text := plantedPayload()
	out := RedactSecrets(text, ScanPayload(text))

	for _, secret := range []string{plantedAWSKey, plantedGenericVal, "MIIEowIBAAKCAQEA", pemBegin} {
		if strings.Contains(out, secret) {
			t.Errorf("redacted payload still holds %q", secret)
		}
	}
	if n := strings.Count(out, SecretPlaceholder); n != 3 {
		t.Errorf("placeholder count = %d, want 3 (one per planted secret): %q", n, out)
	}
	for _, keep := range []string{"const port = 8080", "port=8080", "fmt.Println(user.email)", "awsKey := \""} {
		if !strings.Contains(out, keep) {
			t.Errorf("redaction removed surrounding code %q: %q", keep, out)
		}
	}
}

func TestRedactSecrets_SpanHandling(t *testing.T) {
	text := "0123456789"
	p := SecretPlaceholder
	cases := []struct {
		name string
		hits []SecretHit
		want string
	}{
		{"no hits", nil, text},
		{"single", []SecretHit{{Kind: "k", Start: 2, End: 4}}, "01" + p + "456789"},
		{"unordered", []SecretHit{{Start: 7, End: 9}, {Start: 1, End: 2}}, "0" + p + "23456" + p + "9"},
		{"overlapping merge", []SecretHit{{Start: 2, End: 6}, {Start: 4, End: 8}}, "01" + p + "89"},
		{"nested", []SecretHit{{Start: 1, End: 9}, {Start: 3, End: 4}}, "0" + p + "9"},
		{"adjacent stay separate", []SecretHit{{Start: 0, End: 2}, {Start: 2, End: 4}}, p + p + "456789"},
		{"invalid spans ignored", []SecretHit{{Start: -1, End: 3}, {Start: 5, End: 50}, {Start: 6, End: 6}, {Start: 8, End: 7}}, text},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RedactSecrets(text, tc.hits); got != tc.want {
				t.Errorf("RedactSecrets = %q, want %q", got, tc.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
