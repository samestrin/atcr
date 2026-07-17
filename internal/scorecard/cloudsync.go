package scorecard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
)

// CloudSyncSchemaVersion is the --sync-cloud payload schema version, emitted on
// every CloudSyncRecord so the atcr.dev backend can evolve the ingest contract
// independently of this CLI.
const CloudSyncSchemaVersion = 1

// ErrCloudAuthRejected is returned by Push when the endpoint rejects the API key
// (HTTP 401 or 403). Callers map it to the dedicated process exit code (exitAuth);
// every other push failure is a generic, non-fatal cloud-sync error that must not
// change the underlying command's already-finalized exit code.
var ErrCloudAuthRejected = errors.New("cloud sync rejected: API key was not accepted by the server")

// cloudRequestTimeout bounds a single --sync-cloud push so a slow or unreachable
// endpoint cannot indefinitely block review/reconcile completion. A package var
// only so tests can shrink it, mirroring internal/telemetry's requestTimeout seam.
var cloudRequestTimeout = 5 * time.Second

// cloudHTTPClient is a dedicated client instance (not http.DefaultClient) so the
// no-redirect policy below is isolated from the rest of the process; its nil
// Transport reuses the shared http.DefaultTransport connection pool (same as
// telemetry.Client and llmclient's default client) — only redirect policy is
// isolated, not the connection pool. CheckRedirect blocks redirect following:
// ValidateCloudEndpoint vets only the INITIAL URL, so a validated https://
// endpoint that 3xx-redirects to a same-host http:// target (a scheme downgrade)
// would otherwise make Go forward the Authorization: Bearer <ATCR_API_KEY>
// header in the clear. Mirrors the noRedirect convention in internal/llmclient
// and internal/registry.
var cloudHTTPClient = &http.Client{CheckRedirect: noRedirect}

// noRedirect halts redirect following so the Authorization: Bearer API key is never
// re-sent to a redirect target. Returning ErrUseLastResponse surfaces the 3xx
// response itself, which Push then treats as a generic non-2xx failure.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// CloudSyncPersona is one per-reviewer identity+metrics entry in a cloud-sync
// push. PersonaIDHash is a one-way HashPersonaID digest (pseudonymous, not
// anonymous; it must be hardened to a keyed HMAC-SHA256 before production
// endpoint activation to prevent dictionary attacks reversing enumerable persona names).
// Model is non-PII. The metrics are the real raw scorecard facts — the
// atcr.dev backend derives any "time/credits saved" figure from these (the CLI
// ships facts, not a fabricated ROI number).
type CloudSyncPersona struct {
	PersonaIDHash string  `json:"persona_id_hash"`
	Model         string  `json:"model"`
	CostUSD       float64 `json:"cost_usd"`
	TokensIn      int     `json:"tokens_in"`
	TokensOut     int     `json:"tokens_out"`
	LatencyMS     int64   `json:"latency_ms"`
}

// CloudSyncRecord is the dedicated --sync-cloud allowlist payload (Story 4). It
// is deliberately NOT a superset of the Epic 10.0 PublicRecord leaderboard
// schema: it carries only run-level raw metrics, the run outcome, and a
// per-persona list built from the real per-reviewer records (Persona identities
// hashed via HashPersonaID). It never carries raw source, file paths, run ids, or
// un-hashed reviewer/persona identifiers, preserving the PublicRecord privacy
// boundary while feeding the Persona Leaderboard.
type CloudSyncRecord struct {
	SchemaVersion    int                `json:"schema_version"`
	RunOutcome       string             `json:"run_outcome"`
	MetricsAvailable bool               `json:"metrics_available"`
	CostUSD          float64            `json:"cost_usd"`
	TokensIn         int                `json:"tokens_in"`
	TokensOut        int                `json:"tokens_out"`
	LatencyMS        int64              `json:"latency_ms"`
	Personas         []CloudSyncPersona `json:"personas"`
}

// NewCloudSyncRecord builds a CloudSyncRecord for the finalized run at reviewDir
// with the given outcome ("success"/"failure"). Per-persona identity and metrics
// are sourced from the real per-reviewer AgentStatus rows in the run's pool
// summary — NEVER the zero-value aggregate (whose Reviewer/Model are empty, which
// would hash the empty string on every run and defeat the Persona Leaderboard).
// Run-level metrics are the summed cost/tokens and the slowest-agent latency,
// mirroring scorecard.Emit's aggregate. It is best-effort: an unreadable pool
// summary yields an outcome-carrying, metrics-less record rather than an error, so
// a missing summary never blocks the push.
func NewCloudSyncRecord(reviewDir, outcome string) CloudSyncRecord {
	rec := CloudSyncRecord{SchemaVersion: CloudSyncSchemaVersion, RunOutcome: outcome}
	ps, err := fanout.ReadPoolSummary(reviewDir)
	if err != nil {
		rec.MetricsAvailable = false
		return rec
	}
	rec.MetricsAvailable = true
	for _, a := range ps.Agents {
		name := strings.TrimSpace(a.Agent)
		if name == "" {
			continue
		}
		cost := llmclient.ComputeCostUSD(a.Model, a.TokensIn, a.TokensOut)
		rec.Personas = append(rec.Personas, CloudSyncPersona{
			PersonaIDHash: HashPersonaID(name),
			Model:         a.Model,
			CostUSD:       cost,
			TokensIn:      a.TokensIn,
			TokensOut:     a.TokensOut,
			LatencyMS:     a.DurationMS,
		})
		rec.CostUSD += cost
		rec.TokensIn += a.TokensIn
		rec.TokensOut += a.TokensOut
		if a.DurationMS > rec.LatencyMS {
			rec.LatencyMS = a.DurationMS // run latency ~ slowest agent (parallel)
		}
	}
	return rec
}

// ValidateCloudEndpoint rejects an empty, malformed, or plaintext-remote endpoint
// so the Bearer API key is never transmitted in the clear. https:// is always
// accepted; http:// is accepted ONLY when the host is a loopback address
// (127.0.0.1 / ::1 / localhost) — the mechanism httptest servers rely on in tests
// — never for a remote host.
func ValidateCloudEndpoint(endpoint string) error {
	e := strings.TrimSpace(endpoint)
	if e == "" {
		return errors.New("--cloud-endpoint must be a valid https:// URL")
	}
	u, err := url.Parse(e)
	if err != nil || u.Host == "" {
		return errors.New("--cloud-endpoint must be a valid https:// URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return errors.New("--cloud-endpoint must use https:// (plaintext http:// is allowed only for a loopback host)")
	default:
		return errors.New("--cloud-endpoint must be a valid https:// URL")
	}
}

// redactEndpoint returns endpoint with any embedded userinfo password masked, for
// safe inclusion in an error surfaced to stderr/logs. A --cloud-endpoint of the form
// https://user:pass@host would otherwise echo the password verbatim; url.Redacted()
// replaces it with "xxxxx". An unparseable endpoint reveals nothing.
func redactEndpoint(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return ""
	}
	return u.Redacted()
}

// isLoopbackHost reports whether host is localhost or a loopback IP literal.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Push POSTs rec to endpoint as JSON with an Authorization: Bearer <apiKey>
// header, bounded by cloudRequestTimeout. The API key is sent ONLY in the header
// — never in the body or any returned error. A 401/403 response returns
// ErrCloudAuthRejected (mapped to exitAuth by callers); any other non-2xx, a
// transport error, or a timeout returns a generic, non-fatal error naming neither
// the key nor the raw server body. A validated caller supplies a good endpoint;
// Push re-validates defensively.
func Push(ctx context.Context, endpoint, apiKey string, rec CloudSyncRecord) error {
	if err := ValidateCloudEndpoint(endpoint); err != nil {
		return err
	}
	// Fail closed on a missing credential, symmetric with the defensive endpoint
	// re-validation above: an empty key would otherwise send "Authorization: Bearer "
	// (trailing space) and waste a round-trip / read as anonymous server-side.
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("cloud sync failed: missing API key")
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("cloud sync failed: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, cloudRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("cloud sync failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := cloudHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloud sync failed: request to %s failed: %w", redactEndpoint(endpoint), err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain (bounded) so the keep-alive connection can be reused when the
	// server's ack body fits under the 64KB cap; a larger body leaves bytes
	// unread, so Go closes the connection instead of returning it to the
	// pool. The cap is a DoS guard sized well above the expected small ack.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		// Never echo the response body — avoids leaking server-side internals.
		return fmt.Errorf("%w (%d)", ErrCloudAuthRejected, resp.StatusCode)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return fmt.Errorf("cloud sync failed: server returned %d", resp.StatusCode)
	}
	return nil
}
