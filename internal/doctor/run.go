package doctor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
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
	MaxTokens int // completion budget (generous so thinking models emit the marker)
	// MaxTokensSet reports that MaxTokens came from an explicit --max-tokens rather
	// than the flag's default. Without it the two are indistinguishable (the default
	// is a real number), and a target's declared max_tokens could never take
	// precedence — which is the whole point of consulting the declaration.
	MaxTokensSet bool
	Timeout      time.Duration // per-call deadline (0 = inherit ctx only)
	Nonce        string        // marker token embedded in the prompt
	Concurrency  int           // max concurrent probes (0 = defaultConcurrency)
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
	// ContextWindowTokens is the window the payload layer will size this agent's
	// prompt against, and WindowSource the tier it came from (declaration |
	// table | default) — the pre-flight view of a context_window_tokens
	// declaration, which is otherwise observable only as a failed provider call.
	ContextWindowTokens int    `json:"context_window_tokens,omitempty"`
	WindowSource        string `json:"window_source,omitempty"`
	// MaxTokens is the output cap this agent's probe actually ran at, after the
	// flag → declaration → unset resolution in probe(). Reported for the same reason
	// ContextWindowTokens is: the cap is applied silently, so an ok_warning row is
	// otherwise unreadable — the operator cannot tell whether the probe ran at the
	// agent's declaration or at doctor's default, which is exactly what decides
	// whether raising the declaration would change anything.
	//
	// ALWAYS present (deliberately NOT omitempty), matching the discipline
	// PoolSummary.TruncatedZeroFindings and FallbackCount state for themselves: a 0
	// must be distinguishable from a report written before this field existed. 0 here
	// means NO CAP WAS APPLIED, which arises two ways: the probe short-circuited on
	// invalid_config or missing_key above the budget resolution, or the resolved budget
	// was non-positive and probe() sent the request uncapped.
	//
	// The second case is unreachable through the CLI — cli/doctor.go rejects a
	// --max-tokens at or below 0, and an agent declaration is validated into
	// 1..MaxTokensCap — but Run is exported and probe() deliberately keeps the branch
	// rather than relying on that check one layer up, so the field's contract states it
	// too. MaxTokensSource is empty in both cases. Under omitempty these states would
	// collapse into one absent key.
	MaxTokens int `json:"max_tokens"`
	// MaxTokensSource names the tier MaxTokens resolved from — flag, declaration, or
	// default — the cap's counterpart to WindowSource, and for the same reason: the
	// number alone cannot show whether a declaration took effect, since an agent
	// declaring exactly doctor's default is indistinguishable from one declaring
	// nothing. Empty when no call was placed (MaxTokens 0).
	MaxTokensSource string `json:"max_tokens_source,omitempty"`
}

// The tiers MaxTokensSource can name, mirroring payload.WindowSource* for the window.
const (
	MaxTokensSourceFlag        = "flag"        // an explicit --max-tokens
	MaxTokensSourceDeclaration = "declaration" // the agent's own max_tokens
	MaxTokensSourceDefault     = "default"     // doctor's built-in probe budget
)

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
	// maxTokens is the resolved output cap the probe ran at (0 when none was
	// applied). Carried on the result rather than re-derived in Run so the reported
	// value cannot drift from the one actually sent.
	maxTokens int
	// maxTokensSource is the tier maxTokens came from, carried for the same reason.
	maxTokensSource string
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
		status, hint := pr.status, pr.hint
		if s, h, ok := zeroBudgetVerdict(tgt.Model, at.ContextWindowTokens, reviewMaxTokens(at.DeclaredMaxTokens), status); ok {
			status, hint = s, h
		}
		rep.Agents = append(rep.Agents, AgentResult{
			Agent:               at.Agent,
			Serial:              at.Serial,
			Provider:            tgt.Provider,
			Model:               tgt.Model,
			Status:              status,
			LatencyMS:           pr.latencyMS,
			Hint:                hint,
			Detail:              pr.detail,
			Source:              at.Source,
			ContextWindowTokens: at.ContextWindowTokens,
			WindowSource:        at.WindowSource,
			MaxTokens:           pr.maxTokens,
			MaxTokensSource:     pr.maxTokensSource,
		})
	}
	rep.ExitCode = exitVerdict(res, results)
	return rep
}

// zeroBudgetRemedy names the two declarations that can close an agent's input budget.
// Deliberately worded to match internal/fanout's constant of the same name, which the
// review fan-out prints when it hits this state at dispatch: doctor is the pre-flight
// surface for exactly that failure, and an operator who meets it in both places must not
// be handed two different remedies for one condition.
const zeroBudgetRemedy = "lower its max_tokens, or raise (or drop) its context_window_tokens declaration"

// zeroBudgetVerdict reports the warning doctor owes an agent whose resolved window funds
// NO input budget once its resolved output cap and the fixed prompt overhead are
// reserved. It returns ok=false when there is nothing to say.
//
// doctor is the only surface holding both operands, and it printed them side by side
// without comparing them — so an agent `atcr review` cannot size a payload for reported
// `ok` at exit 0. The probe cannot catch this itself: the nonce prompt is trivial, so it
// succeeds at any cap, which is precisely why the verdict has to be derived here from the
// numbers rather than observed from the response.
//
// The budget is asked of payload.EffectiveByteBudget rather than recomputed, so doctor
// and the fan-out cannot disagree about where the threshold sits — the overhead constant
// stays owned by the package that reserves it.
//
// The cap operand is REVIEW's, never the probe's — see reviewMaxTokens. That choice is
// what removed the guard this function used to carry against MaxTokensSourceFlag. The
// old guard suppressed the verdict outright whenever doctor's own --max-tokens was
// typed, which fixed a real false positive (`atcr doctor --max-tokens 30000` downgrading
// every agent on the 32768 default window) by discarding a real true positive with it:
// an agent whose OWN declaration closes the budget could no longer be reported at all
// under that flag. Drawing the operand from review's resolution order instead settles
// both cases without a tier special case — the false positive disappears because
// doctor's flag is not in that order, and the true positive survives because the
// declaration is. classify()'s rule still holds: no branch may assert which knob governs
// the real run. This one does not; it reports what review resolves ABSENT a review-side
// flag, which is the only claim available here and the one the hint is worded to make.
//
// Three guards, all load-bearing:
//   - maxTokens 0 means NO CAP APPLIES, not a cap of zero. Run cannot produce it —
//     reviewMaxTokens floors at the built-in default — so this guards the function's
//     contract for any other caller rather than a state the report can reach. Without it
//     a window too small to fund even the prompt overhead reports a closed budget and
//     blames a cap that is not there, pointing the operator at the wrong knob when the
//     window is what is at fault.
//   - window 0 means the window did NOT RESOLVE, not a window of zero — the state
//     render.go prints as "-" rather than as a number. An unresolved window is not a
//     closed budget: there is no budget to call closed. Without the guard,
//     ResolveContextWindow ignores the non-positive value and falls back to the 32768
//     default, so the budget computes to 0 for any cap at or above 28672 and the row is
//     blamed on its output cap — the same misattribution the maxTokens guard prevents,
//     from the other operand.
//   - only a HEALTHY probe is touched. A row already reporting auth_failed or missing_key
//     has a louder problem, and overwriting it with a budget warning would hide the
//     failure the operator has to fix first.
//
// An already-warning row (marker absent) is REPHRASED rather than skipped, and that case
// is the reason this exists at all: the marker-absent hint tells the operator to raise
// max_tokens, and at a closed budget that advice is actively harmful — the cap is
// reserved out of the same window, so following it makes the run worse, and the probe
// then passes at the higher cap because the nonce prompt is trivial. Leaving the row's
// original hint in place there would have preserved the exact trap this row was filed
// for.
func zeroBudgetVerdict(model string, window, maxTokens int, status string) (string, string, bool) {
	if !healthy(status) || maxTokens <= 0 || window <= 0 {
		return "", "", false
	}
	if payload.EffectiveByteBudget(model, &window, maxTokens) > 0 {
		return "", "", false
	}
	lead := "endpoint is healthy, but"
	if status == StatusOKWarning {
		// Contradict the marker-absent remedy explicitly. An operator who reads only the
		// first clause and reaches for --max-tokens is the failure being prevented.
		lead = "the marker was absent AND"
	}
	// The cap is named as REVIEW's and disclaimed as the probe's, because under
	// `atcr doctor --max-tokens N` the two differ and the number here is the one the
	// operator has to act on. Without that clause the hint reads as a statement about
	// the probe the row's other columns just reported at a different value.
	return StatusOKWarning, fmt.Sprintf(
		"%s the resolved window (%d tokens) leaves no input budget once the %d-token output cap `atcr review` will "+
			"resolve for this agent (not this probe's cap) and the fixed prompt overhead are reserved — review will ship "+
			"only the smallest single file, or refuse the run outright under on_overflow fail/fallback. Do NOT raise the "+
			"cap here: it is reserved out of this same window. Remedy: %s",
		lead, window, maxTokens, zeroBudgetRemedy), true
}

// reviewDefaultMaxTokens is the cap `atcr review` applies to an agent that
// declares none (and the default cli/doctor.go gives --max-tokens for the same
// reason). It references payload.DefaultOutputTokens — the single source the
// fan-out's own defaultMaxTokens also references — so the two cannot drift;
// TestReviewMaxTokens_MirrorsTheFanOutDefault pins them together.
const reviewDefaultMaxTokens = payload.DefaultOutputTokens

// reviewMaxTokens is the output cap `atcr review` will resolve for an agent, given the
// agent's own declaration. It reproduces resolveMaxTokens MINUS review's own
// --max-tokens tier, which is unknowable from here: doctor cannot see how review will
// later be invoked, so it reports the cap review uses absent a review-side flag.
//
// Doctor's --max-tokens deliberately has no influence. It decides which invocation is
// PROBED, and probe evidence is about that invocation; the budget verdict is about the
// review run instead, and those two operands genuinely differ under the flag.
func reviewMaxTokens(declared int) int {
	if declared > 0 {
		return declared
	}
	return reviewDefaultMaxTokens
}

// exitVerdict returns 0 when every directly-listed agent has at least one
// healthy target along its path (primary or any fallback), 1 otherwise.
// An empty roster (no agents configured) returns 2 — a usage/config error,
// not a healthy result.
func exitVerdict(res *Resolution, results []probeResult) int {
	if len(res.Agents) == 0 {
		return 2 // no agents configured — nothing was tested
	}
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
	// An explicit --max-tokens is the operator's per-run choice and wins. Otherwise
	// the target's own declared cap applies, so an agent that declares 32000 is
	// probed at 32000 rather than at the default and then told to raise a cap it
	// already raised — the hint config.go cites as this field's motivation.
	//
	// The tier is recorded alongside the number because the number cannot carry it:
	// an agent declaring exactly doctor's default probes identically to one declaring
	// nothing, and the remedy differs between those cases.
	budget := opts.MaxTokens
	budgetSrc := MaxTokensSourceDefault
	if opts.MaxTokensSet {
		budgetSrc = MaxTokensSourceFlag
	}
	if !opts.MaxTokensSet && tgt.MaxTokens > 0 {
		budget = tgt.MaxTokens
		budgetSrc = MaxTokensSourceDeclaration
	}
	// A non-positive budget applies no cap at all, so it carries no tier either — the
	// field pair stays self-consistent for any caller rather than relying on the CLI's
	// rejection of a --max-tokens at or below 0 one layer up.
	if budget <= 0 {
		budgetSrc = ""
	}
	var maxTokens *int
	if budget > 0 {
		v := budget
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
	pr := classify(content, err, opts.Nonce, latency, tgt, budgetSrc)
	pr.maxTokens = budget
	pr.maxTokensSource = budgetSrc
	return pr
}

// maxDetailBytes bounds a network-error detail string so a hostile or verbose
// transport error cannot bloat the report.
const maxDetailBytes = 512

// classify turns a completion result into a probe outcome. budgetSrc names the tier the
// probe's output cap resolved from, so the marker-absent remedy can point at the knob
// that actually governed THIS probe.
func classify(content string, err error, nonce string, latencyMS int64, tgt Target, budgetSrc string) probeResult {
	if err == nil {
		// Strip the prompt text before checking for the marker so an endpoint
		// that echoes the request verbatim (a common misconfiguration) does not
		// produce a false-positive StatusOK.
		stripped := strings.ReplaceAll(content, Prompt(nonce), "")
		if strings.Contains(stripped, Marker(nonce)) {
			return probeResult{status: StatusOK, latencyMS: latencyMS}
		}
		// The remedy names the knob that capped THIS probe. Which one that is depends on
		// the resolved tier: without --max-tokens the probe used the agent's declaration,
		// which `atcr review` also resolves; with it, the flag did, and telling the
		// operator to raise a declaration their own flag overrode is a no-op.
		//
		// Deliberately short. Neither branch asserts which knob "governs the real run" —
		// review has its own --max-tokens that overrides the declaration (resolveMaxTokens),
		// so any such claim is conditional on how review is later invoked and cannot be
		// made truthfully from here.
		hint := "HTTP 200 but marker absent/empty (thinking models spend the budget on reasoning) — raise this agent's max_tokens declaration, or pass `atcr review --max-tokens N`"
		if budgetSrc == MaxTokensSourceFlag {
			hint = "HTTP 200 but marker absent/empty (thinking models spend the budget on reasoning) — this probe was capped by your explicit --max-tokens; raise it to re-probe. `atcr review` resolves its own cap separately"
		}
		return probeResult{
			status:    StatusOKWarning,
			latencyMS: latencyMS,
			hint:      hint,
		}
	}

	var se *llmclient.HTTPStatusError
	if errors.As(err, &se) {
		status, hint := StatusProviderError, ""
		switch se.Status {
		case 401:
			status, hint = StatusAuthFailed, "check the API key in "+tgt.APIKeyEnv
		case 403:
			// A 403 is authenticated-but-refused: quota exhaustion, billing state,
			// or a permission scope far more often than a bad credential. Naming
			// the key here misdirects the whole investigation, so point at the
			// captured upstream body instead — it carries the real reason.
			status, hint = StatusAuthFailed, "authenticated but refused — likely quota, billing, or a permission scope; read detail for the upstream reason"
		case 404:
			status, hint = StatusNotFound, "check the model name and the provider base_url"
		case 429:
			status, hint = StatusRateLimited, "provider rate limit — retry later or test a smaller --agents subset"
		}
		return probeResult{status: status, latencyMS: latencyMS, hint: hint, detail: scrubCredentials(se.Snippet, tgt)}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return probeResult{status: StatusTimeout, latencyMS: latencyMS, hint: "raise --timeout"}
	}
	if errors.Is(err, context.Canceled) {
		// Parent-context cancellation (e.g. Ctrl-C) is a teardown, not a slow
		// endpoint: keep the timeout class but do not advise raising --timeout.
		return probeResult{status: StatusTimeout, latencyMS: latencyMS, hint: "self-test canceled before the endpoint responded"}
	}

	detail := scrubCredentials(bounded(err.Error()), tgt)
	return probeResult{status: StatusNetworkError, latencyMS: latencyMS, detail: detail}
}

// scrubCredentials enforces credential exclusion on a detail string surfaced in
// the report or JSON output, rather than relying on upstream redaction. It removes
// the target's API key value and any base_url userinfo (username/password embedded
// in a misconfigured proxy URL). Applied to every error detail — both the
// HTTPStatusError snippet and the transport-error path — so the two paths cannot
// drift in what they exclude.
func scrubCredentials(detail string, tgt Target) string {
	if key := os.Getenv(tgt.APIKeyEnv); key != "" {
		detail = strings.ReplaceAll(detail, key, "[redacted]")
	}
	if u, uerr := url.Parse(tgt.BaseURL); uerr == nil && u.User != nil {
		if pass, ok := u.User.Password(); ok && pass != "" {
			detail = strings.ReplaceAll(detail, pass, "[redacted]")
		}
		if usr := u.User.Username(); usr != "" {
			detail = strings.ReplaceAll(detail, usr, "[redacted]")
		}
	}
	return detail
}

// bounded clamps a detail string to maxDetailBytes on a rune boundary so
// multi-byte UTF-8 sequences are never split, producing invalid output.
func bounded(s string) string { return clampRunes(s, maxDetailBytes) }

// clampRunes truncates s to at most max bytes, walking back to the last valid
// rune boundary. Shared by the probe's detail bound and the table renderer's
// tighter per-row bound so the two cannot drift in how they cut UTF-8.
func clampRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	// Walk back to the last valid rune boundary (at most utf8.UTFMax-1 steps).
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
