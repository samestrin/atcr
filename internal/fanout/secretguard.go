package fanout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

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

// scanSecrets is the scanner guardSecrets runs; a variable so tests can count
// its invocations.
var scanSecrets = payload.ScanPayloadContext

// Scan reuse (row secretguard.go:80). Every agent of a panel is handed the same
// payload and scope constraint, so guardSecrets caches each segment's scan and
// prints each warning once, instead of one gitleaks process (up to its timeout)
// and one warning per agent.
//
// guardSecrets has no run handle, and `atcr serve` and the MCP server keep one
// process alive across reviews, so a run is inferred: within a run each agent
// asks for a given set of segments once, so the same agent asking again starts
// a new run, which warns afresh and retries a scan that failed. A scan that
// succeeded depends only on the text and the scanner, so it is reused across
// runs (and across one agent's chunks, which share a scope constraint) until it
// expires. secretScanTTL bounds how long a result or a
// suppressed warning outlives its run when the next run's panel shares no
// agent, and secretScanCacheCap bounds the memory a long process holds.
var (
	secretScanNow      = time.Now
	secretScanTTL      = 5 * time.Minute
	secretScanCacheCap = 256
)

// runSeen is a cache entry's run: when it began and which agents it served.
type runSeen struct {
	at     time.Time
	agents map[string]bool
}

// claim records agent against the entry and reports whether the entry may be
// reused: false when it expired, or when perRun and agent already used it.
func (r *runSeen) claim(agent string, now time.Time, perRun bool) bool {
	if now.Sub(r.at) >= secretScanTTL || (perRun && r.agents[agent]) {
		return false
	}
	r.agents[agent] = true
	return true
}

func newRunSeen(agent string, now time.Time) runSeen {
	return runSeen{at: now, agents: map[string]bool{agent: true}}
}

type cachedScanResult struct {
	runSeen
	hits []payload.SecretHit
	err  error
}

var secretScanCache struct {
	sync.Mutex
	scans  map[string]*cachedScanResult
	warned map[string]*runSeen
}

// resetSecretScanCache forgets every cached scan and warning; tests call it
// for isolation.
func resetSecretScanCache() {
	secretScanCache.Lock()
	defer secretScanCache.Unlock()
	secretScanCache.scans = nil
	secretScanCache.warned = nil
}

func secretScanCacheLen() int {
	secretScanCache.Lock()
	defer secretScanCache.Unlock()
	return len(secretScanCache.scans) + len(secretScanCache.warned)
}

// scannerIdentity names what a scan result depends on besides the text: the
// gitleaks binary PATH resolves (or none, for the built-in ruleset) and the
// environment that points gitleaks at a config.
func scannerIdentity() string {
	bin, _ := exec.LookPath("gitleaks")
	return bin + "\x00" + os.Getenv("GITLEAKS_CONFIG") + "\x00" + os.Getenv("GITLEAKS_CONFIG_TOML")
}

func textDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// cachedScan returns the scan of text for agent, reusing a cached result. A
// scan error is cached with its hits for the rest of the run, so block fails
// closed for every agent that shares the text, not only the first.
func cachedScan(agent, scanner, text string) ([]payload.SecretHit, error) {
	key := scanner + "\x00" + textDigest(text)
	c := &secretScanCache
	c.Lock()
	if e := c.scans[key]; e != nil && e.claim(agent, secretScanNow(), e.err != nil) {
		c.Unlock()
		return e.hits, e.err
	}
	c.Unlock()

	hits, err := scanSecrets(context.Background(), text, nil)

	c.Lock()
	defer c.Unlock()
	if c.scans == nil || len(c.scans) >= secretScanCacheCap {
		c.scans = make(map[string]*cachedScanResult)
	}
	c.scans[key] = &cachedScanResult{runSeen: newRunSeen(agent, secretScanNow()), hits: hits, err: err}
	return hits, err
}

// firstWarning reports whether the warnings for this exact set of segments, under
// this policy and scanner, are still unprinted in the current run.
func firstWarning(agent, policy, scanner string, segs []guardSegment) bool {
	var b strings.Builder
	b.WriteString(policy + "\x00" + scanner)
	for _, s := range segs {
		b.WriteString("\x00" + textDigest(s.text))
	}
	key := b.String()
	c := &secretScanCache
	c.Lock()
	defer c.Unlock()
	now := secretScanNow()
	if e := c.warned[key]; e != nil && e.claim(agent, now, true) {
		return false
	}
	if c.warned == nil || len(c.warned) >= secretScanCacheCap {
		c.warned = make(map[string]*runSeen)
	}
	r := newRunSeen(agent, now)
	c.warned[key] = &r
	return true
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
		scanner   = scannerIdentity()
	)
	for i, s := range segs {
		if s.text == "" {
			continue
		}
		hits, err := cachedScan(agent, scanner, s.text)
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
	// Only warnings are deduplicated: a block error is returned to every agent.
	warn := (len(found) > 0 || scanErr != nil) && firstWarning(agent, policy, scanner, segs)
	if scanErr != nil && warn {
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
		if len(found) > 0 && warn {
			fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: redacted %s from the review prompt (secret_guard: redact)\n", agent, describeFindings(found))
		}
	default:
		if len(found) > 0 && warn {
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
