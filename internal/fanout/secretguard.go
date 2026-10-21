package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/payload"
)

// secret_guard policies (Epic 35.20). The registry validates and resolves the
// value into Settings.SecretGuard; this package enforces it.
const (
	secretGuardOff    = "off"
	secretGuardWarn   = "warn"
	secretGuardRedact = "redact"
	secretGuardBlock  = "block"
)

// ErrSecretGuardBlocked is returned when secret_guard=block finds a credential
// in a review prompt, or cannot trust the scan. It aborts the slot build, and
// with it the run, before anything is dispatched.
var ErrSecretGuardBlocked = errors.New("secret_guard=block: review prompt not sent")

// Labels for text that belongs to no diff file. The claim ledger and the
// Context Definitions block sit above the first file marker of a payload.
const (
	secretPreambleLabel = "payload preamble (claim ledger / context definitions)"
	secretScopeLabel    = "sprint-plan scope constraint"
)

// secretGuardPolicy resolves the effective policy. Settings built without the
// registry (tests, direct construction) carry "", which is the registry
// default, warn.
func secretGuardPolicy(cfg *ReviewConfig) string {
	if p := strings.TrimSpace(cfg.Settings.SecretGuard); p != "" {
		return p
	}
	return secretGuardWarn
}

// guardSegment is one piece of text bound for a provider. file names it in a
// warning or block error; mode, when set, says the text is a rendered payload
// whose per-file markers locate each hit's file.
type guardSegment struct {
	text string
	file string
	mode string
}

// guardSecrets scans each segment under policy and returns them in order:
// unchanged for off and warn, with every hit replaced by
// payload.SecretPlaceholder for redact. block returns an error naming agent,
// kind and file when anything is found, or when the scan could not be trusted.
// Neither the warning nor the error ever holds secret material: a SecretHit
// carries only a rule name and a span, and only kinds and paths are printed.
func guardSecrets(policy, agent string, segs ...guardSegment) ([]string, error) {
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = s.text
	}
	if policy == secretGuardOff {
		return out, nil
	}
	var (
		found     []secretFinding
		scanErr   error
		redactHit = make([][]payload.SecretHit, len(segs))
	)
	for i, s := range segs {
		if s.text == "" {
			continue
		}
		hits, err := payload.ScanPayloadContext(context.Background(), s.text, nil)
		if err != nil {
			scanErr = err
		}
		redactHit[i] = hits
		if len(hits) == 0 {
			continue
		}
		files := segmentFiles(s)
		for _, h := range hits {
			found = append(found, secretFinding{kind: h.Kind, file: files.at(h.Start, s.file)})
		}
	}
	if scanErr != nil {
		// ScanPayloadContext's error names only the failure mode, never scanner
		// output, so it is safe to print.
		fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: secret scan: %v; using the built-in ruleset\n", agent, scanErr)
	}
	switch policy {
	case secretGuardBlock:
		if len(found) > 0 {
			return nil, fmt.Errorf("agent %q: %w: %s; remove the credential or set secret_guard: redact", agent, ErrSecretGuardBlocked, describeFindings(found))
		}
		if scanErr != nil {
			return nil, fmt.Errorf("agent %q: %w: the secret scan failed, so the prompt could not be checked: %w", agent, ErrSecretGuardBlocked, scanErr)
		}
	case secretGuardRedact:
		for i := range out {
			out[i] = payload.RedactSecrets(out[i], redactHit[i])
		}
		if len(found) > 0 {
			fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: redacted %s from the review prompt (secret_guard: redact)\n", agent, describeFindings(found))
		}
	default:
		if len(found) > 0 {
			fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: the review prompt holds %s and is sent unchanged (secret_guard: warn); set secret_guard: redact or block to stop it\n", agent, describeFindings(found))
		}
	}
	return out, nil
}

// secretFinding is a hit reduced to what may be printed.
type secretFinding struct {
	kind string
	file string
}

// describeFindings renders "2 secret(s) [github-token, private-key] in a.go, b.pem",
// with kinds and files deduplicated and sorted.
func describeFindings(fs []secretFinding) string {
	kinds := map[string]bool{}
	files := map[string]bool{}
	for _, f := range fs {
		kinds[f.kind] = true
		files[f.file] = true
	}
	return fmt.Sprintf("%d secret(s) [%s] in %s", len(fs), strings.Join(sortedSet(kinds), ", "), strings.Join(sortedSet(files), ", "))
}

func sortedSet(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// fileSpan is one payload entry's byte range and the name it is reported under.
type fileSpan struct {
	start, end int
	name       string
}

type fileSpans []fileSpan

// segmentFiles maps a rendered payload segment to its per-file spans. Entry
// bodies are substrings of the payload in order (EntriesFromRenderedPayload),
// so each is located from the end of the last; the walk stops at a body it
// cannot locate, leaving later hits on the segment's own label.
func segmentFiles(s guardSegment) fileSpans {
	if s.mode == "" {
		return nil
	}
	var spans fileSpans
	cursor := 0
	for _, e := range payload.EntriesFromRenderedPayload(payload.PayloadMode(s.mode), s.text) {
		i := strings.Index(s.text[cursor:], e.Body)
		if i < 0 {
			break
		}
		start := cursor + i
		name := e.Path
		if name == "" {
			name = secretPreambleLabel
		}
		spans = append(spans, fileSpan{start: start, end: start + len(e.Body), name: name})
		cursor = start + len(e.Body)
	}
	return spans
}

// at names the file holding offset off, or fallback when no span does.
func (fs fileSpans) at(off int, fallback string) string {
	for _, f := range fs {
		if off >= f.start && off < f.end {
			return f.name
		}
	}
	return fallback
}
