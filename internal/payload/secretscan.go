package payload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/log"
)

// SecretPlaceholder replaces every redacted secret span. It is stable so a
// reviewer sees the same marker for every redaction and a prompt-cache key
// computed over redacted text is deterministic.
const SecretPlaceholder = "«REDACTED-SECRET»"

// ErrSecretScanFailed reports that gitleaks was on PATH but its result could
// not be trusted (timeout, non-zero exit, malformed JSON, or a finding whose
// secret is not in the payload). The hits returned alongside it come from the
// built-in ruleset, so warn/redact still act on them; block fails closed.
var ErrSecretScanFailed = errors.New("secret scan: gitleaks failed")

// SecretHit is one detected secret: the rule that matched and its byte span
// [Start, End) in the scanned text. It never carries the secret value, so a
// hit is safe to log.
type SecretHit struct {
	Kind  string
	Start int
	End   int
}

// gitleaksTimeout bounds each gitleaks invocation. A var so tests can shrink it.
var gitleaksTimeout = 10 * time.Second

// ScanPayload returns the secret-shaped spans in text, using `gitleaks stdin`
// when the binary is on PATH and the built-in ruleset otherwise. A gitleaks
// failure silently falls back to the built-in ruleset; callers that must fail
// closed on it use ScanPayloadContext.
func ScanPayload(text string) []SecretHit {
	hits, _ := ScanPayloadContext(context.Background(), text, nil)
	return hits
}

// ScanPayloadContext is ScanPayload with a caller context and logger (nil →
// discard). When gitleaks is on PATH but fails, it logs a warning, returns the
// built-in ruleset's hits and an error wrapping ErrSecretScanFailed. A missing
// gitleaks is not a failure. Neither the log nor the error ever holds secret
// material or scanner output.
func ScanPayloadContext(ctx context.Context, text string, logger *slog.Logger) ([]SecretHit, error) {
	if logger == nil {
		logger = log.Discard()
	}
	bin, err := exec.LookPath("gitleaks")
	if err != nil {
		return scanBuiltin(text), nil
	}
	hits, err := scanGitleaks(ctx, bin, text)
	if err != nil {
		logger.Warn("secret scan: gitleaks failed, using the built-in ruleset", "err", err)
		return scanBuiltin(text), fmt.Errorf("%w: %w", ErrSecretScanFailed, err)
	}
	return hits, nil
}

// gitleaksFinding is the subset of a gitleaks JSON report entry the guard
// reads. gitleaks reports line/column, not byte offsets, so spans are
// recovered by searching for Secret (or Match) in the payload.
type gitleaksFinding struct {
	RuleID string `json:"RuleID"`
	Secret string `json:"Secret"`
	Match  string `json:"Match"`
}

// scanGitleaks pipes text through `gitleaks stdin` and maps its findings to
// byte spans. Errors name only the failure mode: gitleaks' stderr can echo
// payload content, so it is never surfaced.
func scanGitleaks(ctx context.Context, bin, text string) ([]SecretHit, error) {
	ctx, cancel := context.WithTimeout(ctx, gitleaksTimeout)
	defer cancel()
	// --exit-code 0: findings are not an error, so any non-zero exit is one.
	// No --redact: the Secret field is what locates each span.
	cmd := exec.CommandContext(ctx, bin, "stdin",
		"--no-banner", "--log-level", "error",
		"--report-format", "json", "--report-path", "-", "--exit-code", "0")
	cmd.Stdin = strings.NewReader(text)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// Bound Wait if a killed gitleaks left a child holding the pipes open.
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("timed out after %v", gitleaksTimeout)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("exited with status %d", exitErr.ExitCode())
		}
		return nil, errors.New("could not run")
	}
	var findings []gitleaksFinding
	if err := json.Unmarshal(stdout.Bytes(), &findings); err != nil {
		return nil, errors.New("report is not a JSON findings array")
	}
	var hits []SecretHit
	for _, f := range findings {
		needle := f.Secret
		if needle == "" || !strings.Contains(text, needle) {
			needle = f.Match
		}
		if needle == "" || !strings.Contains(text, needle) {
			return nil, fmt.Errorf("finding %q could not be located in the payload", f.RuleID)
		}
		hits = appendOccurrences(hits, text, needle, f.RuleID)
	}
	return dedupeHits(hits), nil
}

// appendOccurrences adds a hit for every non-overlapping occurrence of needle.
// Redacting all of them, not only the line gitleaks reported, keeps a secret
// pasted twice from leaking through its second copy.
func appendOccurrences(hits []SecretHit, text, needle, kind string) []SecretHit {
	for off := 0; ; {
		i := strings.Index(text[off:], needle)
		if i < 0 {
			return hits
		}
		start := off + i
		hits = append(hits, SecretHit{Kind: kind, Start: start, End: start + len(needle)})
		off = start + len(needle)
	}
}

func dedupeHits(hits []SecretHit) []SecretHit {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Start != hits[j].Start {
			return hits[i].Start < hits[j].Start
		}
		if hits[i].End != hits[j].End {
			return hits[i].End < hits[j].End
		}
		return hits[i].Kind < hits[j].Kind
	})
	out := hits[:0]
	for i, h := range hits {
		if i > 0 && h == hits[i-1] {
			continue
		}
		out = append(out, h)
	}
	return out
}

// builtinRule is one fixed-shape token pattern. When the pattern has a
// capture group, the span is the group (the value, not the key name).
type builtinRule struct {
	kind string
	re   *regexp.Regexp
	// entropic requires the matched value to look random (mixed letters and
	// digits, high Shannon entropy), filtering placeholders and prose.
	entropic bool
}

// builtinRules is a deliberately small, high-signal set: vendor token shapes
// with fixed prefixes plus a keyword-gated generic assignment. Full secret
// detection is gitleaks' job; this only covers its absence.
var builtinRules = []builtinRule{
	{kind: "aws-access-token", re: regexp.MustCompile(`\b(?:A3T[A-Z0-9]|AKIA|ASIA|ABIA|ACCA)[A-Z2-7]{16}\b`)},
	{kind: "github-token", re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,255}\b`)},
	{kind: "github-fine-grained", re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{82}\b`)},
	{kind: "stripe-access-token", re: regexp.MustCompile(`\b(?:sk|rk)_(?:live|test|prod)_[A-Za-z0-9]{10,99}\b`)},
	{kind: "slack-token", re: regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`)},
	{kind: "openai-api-key", re: regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-|live-)?[A-Za-z0-9_-]{20,}`), entropic: true},
	{kind: "generic-api-key", re: regexp.MustCompile(
		`(?i)[A-Za-z0-9_.-]*(?:api[_-]?key|access[_-]?key|secret|token|passw(?:or)?d|private[_-]?key|credential)[A-Za-z0-9_.-]*["']?\s*(?::=|=|:)\s*["'\x60]([^"'\x60\s]{16,})["'\x60]`),
		entropic: true},
}

var (
	pemHeaderRe = regexp.MustCompile(`-----BEGIN[ A-Z0-9_-]*PRIVATE KEY(?: BLOCK)?-----`)
	pemFooterRe = regexp.MustCompile(`-----END[ A-Z0-9_-]*PRIVATE KEY(?: BLOCK)?-----`)
	// pemBodyLineRe matches a line of key material, optionally behind a
	// diff marker: base64 runs and RFC 1421 headers like Proc-Type.
	pemBodyLineRe = regexp.MustCompile(`^[+\- ]?(?:[A-Za-z0-9+/=]+|[A-Za-z-]+: .*)?\r?$`)
)

// scanBuiltin applies the built-in ruleset.
func scanBuiltin(text string) []SecretHit {
	var hits []SecretHit
	for _, r := range builtinRules {
		for _, m := range r.re.FindAllStringSubmatchIndex(text, -1) {
			start, end := m[0], m[1]
			if len(m) >= 4 && m[2] >= 0 {
				start, end = m[2], m[3]
			}
			if r.entropic && !looksRandom(text[start:end]) {
				continue
			}
			hits = append(hits, SecretHit{Kind: r.kind, Start: start, End: end})
		}
	}
	hits = append(hits, scanPEM(text)...)
	return dedupeHits(hits)
}

// scanPEM spans each private-key block from its BEGIN header through its END
// footer. A diff hunk can cut the block short, so with no footer the span runs
// over the key-material lines that follow the header.
func scanPEM(text string) []SecretHit {
	var hits []SecretHit
	for _, h := range pemHeaderRe.FindAllStringIndex(text, -1) {
		start, end := h[0], h[1]
		if f := pemFooterRe.FindStringIndex(text[end:]); f != nil && !pemHeaderRe.MatchString(text[end:end+f[0]]) {
			end += f[1]
		} else {
			end = pemBodyEnd(text, end)
		}
		hits = append(hits, SecretHit{Kind: "private-key", Start: start, End: end})
	}
	return hits
}

// pemBodyEnd returns the offset just past the run of key-material lines that
// start after the header ending at from.
func pemBodyEnd(text string, from int) int {
	end := from
	for end < len(text) && text[end] == '\n' {
		lineStart := end + 1
		lineEnd := strings.IndexByte(text[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd += lineStart
		}
		line := text[lineStart:lineEnd]
		if strings.TrimLeft(line, "+- \r") == "" || !pemBodyLineRe.MatchString(line) {
			break
		}
		end = lineEnd
	}
	return end
}

// looksRandom reports whether v reads like generated key material rather
// than a placeholder, an env reference or a word.
func looksRandom(v string) bool {
	if strings.ContainsAny(v, "${}<>") {
		return false
	}
	var letters, digits bool
	for _, c := range v {
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			letters = true
		}
	}
	return letters && digits && shannonEntropy(v) >= 3.5
}

func shannonEntropy(s string) float64 {
	counts := map[rune]int{}
	n := 0
	for _, c := range s {
		counts[c]++
		n++
	}
	var h float64
	for _, c := range counts {
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}

// RedactSecrets replaces each hit's span in text with SecretPlaceholder.
// Overlapping or nested spans collapse into one placeholder; spans outside
// text or empty are ignored. Hits need not be sorted.
func RedactSecrets(text string, hits []SecretHit) string {
	spans := make([][2]int, 0, len(hits))
	for _, h := range hits {
		if h.Start < 0 || h.End > len(text) || h.Start >= h.End {
			continue
		}
		spans = append(spans, [2]int{h.Start, h.End})
	}
	if len(spans) == 0 {
		return text
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	var b strings.Builder
	b.Grow(len(text))
	pos := 0
	for i := 0; i < len(spans); {
		start, end := spans[i][0], spans[i][1]
		for i++; i < len(spans) && spans[i][0] < end; i++ {
			end = max(end, spans[i][1])
		}
		b.WriteString(text[pos:start])
		b.WriteString(SecretPlaceholder)
		pos = end
	}
	b.WriteString(text[pos:])
	return b.String()
}
