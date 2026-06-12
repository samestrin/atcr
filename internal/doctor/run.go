package doctor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
)

// Status classes for a single endpoint probe. ok means the nonce marker came
// back in visible content; the rest are failure or warning classes.
const (
	StatusOK            = "ok"             // marker found in response content
	StatusOKWarning     = "ok_warning"     // HTTP 200 but marker absent/empty
	StatusAuthFailed    = "auth_failed"    // 401/403
	StatusNotFound      = "not_found"      // 404 (model or base_url)
	StatusRateLimited   = "rate_limited"   // 429
	StatusProviderError = "provider_error" // 5xx or other non-classified HTTP error
	StatusNetworkError  = "network_error"  // transport-level failure
	StatusTimeout       = "timeout"        // per-call deadline exceeded
	StatusMissingKey    = "missing_key"    // API key env var unset (no network call)
	StatusInvalidConfig = "invalid_config" // base_url empty/malformed (no network call)
)

// defaultConcurrency bounds simultaneous target probes when Options leaves it
// unset. Targets are independent; a small pool keeps a large roster civil to
// providers without serializing the self-test.
const defaultConcurrency = 8

// healthy reports whether a status counts as a working invocation path.
func healthy(status string) bool { return status == StatusOK || status == StatusOKWarning }

// Completer is the subset of llmclient.Client the doctor needs. Tests inject a
// fake; production passes a real *llmclient.Client.
type Completer interface {
	Complete(ctx context.Context, inv llmclient.Invocation) (string, error)
}

// Options tune the self-test.
type Options struct {
	MaxTokens   int           // completion budget (generous so thinking models emit the marker)
	Timeout     time.Duration // per-call deadline (0 = inherit ctx only)
	Nonce       string        // marker token embedded in the prompt
	Concurrency int           // max concurrent probes (0 = defaultConcurrency)
}

// Marker is the exact token a healthy endpoint must echo back.
func Marker(nonce string) string { return "ATCR-OK-" + nonce }

// Prompt is the trivial echo prompt sent to each endpoint.
func Prompt(nonce string) string { return "Reply with exactly: " + Marker(nonce) }

// RandomNonce returns a fresh per-run nonce so a cached or replayed response
// cannot masquerade as a live success.
func RandomNonce() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// AgentResult is one row of the doctor report.
type AgentResult struct {
	Agent     string `json:"agent"`
	Serial    bool   `json:"serial"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Hint      string `json:"hint,omitempty"`
	Detail    string `json:"detail,omitempty"` // bounded error snippet; for network_error may include base_url host/path — never the API key, which lives only in Authorization headers
	Source    string `json:"source,omitempty"` // definition tier: user | project
}

// Report is the full doctor outcome. ExitCode is 0 when every directly-listed
// roster agent has a working invocation path, 1 otherwise.
type Report struct {
	Agents   []AgentResult `json:"agents"`
	ExitCode int           `json:"-"`
}

// probeResult is the outcome of one distinct target.
type probeResult struct {
	status    string
	latencyMS int64
	hint      string
	detail    string
}

// Run probes every distinct target once (bounded concurrency), maps results
// back to every effective-roster agent, and computes the exit verdict. It never
// returns an error: configuration/transport problems are encoded as per-agent
// statuses so the report is always complete.
func Run(ctx context.Context, c Completer, res *Resolution, opts Options) *Report {
	results := make([]probeResult, len(res.Targets))

	conc := opts.Concurrency
	if conc <= 0 {
		conc = defaultConcurrency
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i := range res.Targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = probe(ctx, c, res.Targets[i], opts)
		}(i)
	}
	wg.Wait()

	rep := &Report{}
	for _, at := range res.Agents {
		tgt := res.Targets[at.TargetIdx]
		pr := results[at.TargetIdx]
		rep.Agents = append(rep.Agents, AgentResult{
			Agent:     at.Agent,
			Serial:    at.Serial,
			Provider:  tgt.Provider,
			Model:     tgt.Model,
			Status:    pr.status,
			LatencyMS: pr.latencyMS,
			Hint:      pr.hint,
			Detail:    pr.detail,
			Source:    at.Source,
		})
	}
	rep.ExitCode = exitVerdict(res, results)
	return rep
}

// exitVerdict returns 0 when every directly-listed agent has at least one
// healthy target along its path (primary or any fallback), 1 otherwise.
func exitVerdict(res *Resolution, results []probeResult) int {
	statusOf := map[string]string{}
	for _, at := range res.Agents {
		statusOf[at.Agent] = results[at.TargetIdx].status
	}
	for _, path := range res.Paths {
		working := false
		for _, node := range path {
			if healthy(statusOf[node]) {
				working = true
				break
			}
		}
		if !working {
			return 1
		}
	}
	return 0
}

// probe runs pre-flight checks (no network) then a single invocation, and
// classifies the outcome.
func probe(ctx context.Context, c Completer, tgt Target, opts Options) probeResult {
	if strings.TrimSpace(tgt.BaseURL) == "" {
		return probeResult{status: StatusInvalidConfig, hint: "provider base_url is empty — set it in registry.yaml"}
	}
	u, err := url.Parse(tgt.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return probeResult{status: StatusInvalidConfig, hint: "provider base_url is not a valid http(s) URL"}
	}
	if strings.HasSuffix(u.Path, "/chat/completions") || u.RawQuery != "" || u.Fragment != "" {
		return probeResult{status: StatusInvalidConfig, hint: "provider base_url should be the API base (e.g. https://api.openai.com/v1), not a full endpoint path or URL with query/fragment"}
	}
	if os.Getenv(tgt.APIKeyEnv) == "" {
		return probeResult{status: StatusMissingKey, hint: "set the " + tgt.APIKeyEnv + " environment variable"}
	}

	callCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	var maxTokens *int
	if opts.MaxTokens > 0 {
		v := opts.MaxTokens
		maxTokens = &v
	}

	start := time.Now()
	content, err := c.Complete(callCtx, llmclient.Invocation{
		BaseURL:   tgt.BaseURL,
		APIKeyEnv: tgt.APIKeyEnv,
		Model:     tgt.Model,
		MaxTokens: maxTokens,
		Prompt:    Prompt(opts.Nonce),
	})
	latency := time.Since(start).Milliseconds()
	return classify(content, err, opts.Nonce, latency, tgt)
}

// maxDetailBytes bounds a network-error detail string so a hostile or verbose
// transport error cannot bloat the report.
const maxDetailBytes = 512

// classify turns a completion result into a probe outcome.
func classify(content string, err error, nonce string, latencyMS int64, tgt Target) probeResult {
	if err == nil {
		if strings.Contains(content, Marker(nonce)) {
			return probeResult{status: StatusOK, latencyMS: latencyMS}
		}
		return probeResult{
			status:    StatusOKWarning,
			latencyMS: latencyMS,
			hint:      "HTTP 200 but marker absent/empty — raise --max-tokens (thinking models spend the budget on reasoning)",
		}
	}

	var se *llmclient.HTTPStatusError
	if errors.As(err, &se) {
		status, hint := StatusProviderError, ""
		switch se.Status {
		case 401, 403:
			status, hint = StatusAuthFailed, "check the API key in "+tgt.APIKeyEnv
		case 404:
			status, hint = StatusNotFound, "check the model name and the provider base_url"
		case 429:
			status, hint = StatusRateLimited, "provider rate limit — retry later or test a smaller --agents subset"
		}
		return probeResult{status: status, latencyMS: latencyMS, hint: hint, detail: se.Snippet}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return probeResult{status: StatusTimeout, latencyMS: latencyMS, hint: "raise --timeout"}
	}
	if errors.Is(err, context.Canceled) {
		// Parent-context cancellation (e.g. Ctrl-C) is a teardown, not a slow
		// endpoint: keep the timeout class but do not advise raising --timeout.
		return probeResult{status: StatusTimeout, latencyMS: latencyMS, hint: "self-test canceled before the endpoint responded"}
	}

	// err.Error() may embed the transport address (base_url host/path) but never
	// the API key, which only appears in Authorization headers — safe to surface.
	return probeResult{status: StatusNetworkError, latencyMS: latencyMS, detail: bounded(err.Error())}
}

// bounded clamps a detail string to maxDetailBytes.
func bounded(s string) string {
	if len(s) > maxDetailBytes {
		return s[:maxDetailBytes]
	}
	return s
}
