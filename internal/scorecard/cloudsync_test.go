package scorecard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// cloudsyncServer spins up an httptest server that records the first request's
// method, Authorization header, and body, and replies with status. It returns
// the server (loopback http:// — the URL --sync-cloud tests point --cloud-endpoint
// at) and pointers to the captured request facts.
func cloudsyncServer(t *testing.T, status int) (*httptest.Server, *bool, *string, *[]byte) {
	t.Helper()
	got := false
	auth := ""
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = true
		auth = r.Header.Get("Authorization")
		body, _ = readAll(r)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &auth, &body
}

func readAll(r *http.Request) ([]byte, error) {
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

func sampleCloudRecord() CloudSyncRecord {
	return CloudSyncRecord{
		SchemaVersion: CloudSyncSchemaVersion,
		RunOutcome:    "success",
		CostUSD:       0.12,
		TokensIn:      300,
		TokensOut:     100,
		LatencyMS:     1500,
		Personas: []CloudSyncPersona{
			{PersonaIDHash: HashPersonaID("greta"), Model: "gpt-x", CostUSD: 0.08, TokensIn: 200, TokensOut: 60, LatencyMS: 1500},
			{PersonaIDHash: HashPersonaID("bruce"), Model: "claude-y", CostUSD: 0.04, TokensIn: 100, TokensOut: 40, LatencyMS: 900},
		},
	}
}

// TestPush_SuccessfulPush_BearerHeaderAndAllowlistedBody covers AC 04-02: a
// successful push POSTs a Bearer-authed body of allowlisted fields and returns nil.
func TestPush_SuccessfulPush_BearerHeaderAndAllowlistedBody(t *testing.T) {
	srv, got, auth, body := cloudsyncServer(t, http.StatusOK)

	err := Push(context.Background(), srv.URL, "valid-key", sampleCloudRecord())
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	if !*got {
		t.Fatal("server received no request")
	}
	if *auth != "Bearer valid-key" {
		t.Fatalf("Authorization = %q, want %q", *auth, "Bearer valid-key")
	}

	var top map[string]any
	if err := json.Unmarshal(*body, &top); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, string(*body))
	}
	// The API key must never appear in the body (header-only).
	if raw := string(*body); containsStr(raw, "valid-key") {
		t.Fatalf("API key leaked into request body: %s", raw)
	}
	assertAllowlisted(t, top)
}

// TestPush_401And403_ReturnErrCloudAuthRejected covers AC 04-04 EC1: both auth
// statuses map identically to the ErrCloudAuthRejected sentinel.
func TestPush_401And403_ReturnErrCloudAuthRejected(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv, _, _, _ := cloudsyncServer(t, status)
		err := Push(context.Background(), srv.URL, "bad-key", sampleCloudRecord())
		if !errors.Is(err, ErrCloudAuthRejected) {
			t.Fatalf("status %d: err = %v, want ErrCloudAuthRejected", status, err)
		}
	}
}

// TestPush_400And500_NotAuthRejected covers AC 04-04 EC3 / AC 04-02 ErrScenario2:
// a non-auth failure is an error but NOT ErrCloudAuthRejected.
func TestPush_400And500_NotAuthRejected(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		srv, _, _, _ := cloudsyncServer(t, status)
		err := Push(context.Background(), srv.URL, "valid-key", sampleCloudRecord())
		if err == nil {
			t.Fatalf("status %d: expected a push error", status)
		}
		if errors.Is(err, ErrCloudAuthRejected) {
			t.Fatalf("status %d must NOT map to ErrCloudAuthRejected", status)
		}
	}
}

// TestPush_UnreachableEndpoint_BoundedError covers AC 04-02 ErrScenario1: a hung
// endpoint returns a bounded error, not an indefinite hang.
func TestPush_UnreachableEndpoint_BoundedError(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never responds within the request timeout
	}))
	// Cleanup is LIFO: srv.Close waits for in-flight handlers, so close(block)
	// must run FIRST (registered LAST) or the two deadlock.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(block) })

	restore := cloudRequestTimeout
	cloudRequestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { cloudRequestTimeout = restore })

	done := make(chan error, 1)
	go func() { done <- Push(context.Background(), srv.URL, "k", sampleCloudRecord()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a bounded timeout error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Push did not honor its bounded timeout (hung)")
	}
}

// TestPush_RefusesNonHTTPSRemote covers AC 04-02 Security: a plaintext http
// remote (non-loopback) is refused before any request.
func TestPush_RefusesNonHTTPSRemote(t *testing.T) {
	err := Push(context.Background(), "http://example.com/ingest", "k", sampleCloudRecord())
	if err == nil {
		t.Fatal("plaintext http remote endpoint must be refused")
	}
	if errors.Is(err, ErrCloudAuthRejected) {
		t.Fatal("endpoint refusal must not masquerade as an auth rejection")
	}
}

// TestPush_DoesNotFollowRedirect_NoBearerLeak covers the cloudsync.go:38 TD: the
// dedicated client must NOT follow redirects, so a validated https endpoint that
// 3xx-redirects to a plaintext/other target cannot forward the
// Authorization: Bearer <ATCR_API_KEY> header — ValidateCloudEndpoint only vets the
// initial URL, not a redirect target.
func TestPush_DoesNotFollowRedirect_NoBearerLeak(t *testing.T) {
	targetGot := false
	targetAuth := ""
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetGot = true
		targetAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/ingest", http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	err := Push(context.Background(), redirector.URL, "secret-key", sampleCloudRecord())
	if err == nil {
		t.Fatal("Push must not silently succeed by following a redirect response")
	}
	if errors.Is(err, ErrCloudAuthRejected) {
		t.Fatalf("a 3xx redirect must not map to ErrCloudAuthRejected: %v", err)
	}
	if targetGot {
		t.Fatalf("Push followed the redirect to the target (Authorization=%q) — the Bearer key must never be forwarded to a redirect target", targetAuth)
	}
}

// TestPush_TransportError_RedactsEndpointUserinfo covers the cloudsync.go:173 TD: a
// --cloud-endpoint carrying embedded userinfo (https://user:pass@host) must have its
// password redacted from the transport-error message — that error is surfaced to
// stderr as `warning: %v` by finishCloudSync, so echoing the raw endpoint leaks the
// password to logs.
func TestPush_TransportError_RedactsEndpointUserinfo(t *testing.T) {
	restore := cloudRequestTimeout
	cloudRequestTimeout = 200 * time.Millisecond
	t.Cleanup(func() { cloudRequestTimeout = restore })

	// https scheme passes ValidateCloudEndpoint; port 1 is closed, so Do fails at
	// transport — exercising the error path that formats the endpoint string.
	err := Push(context.Background(), "https://user:secretpass@127.0.0.1:1/ingest", "k", sampleCloudRecord())
	if err == nil {
		t.Fatal("expected a transport error against a closed port")
	}
	if containsStr(err.Error(), "secretpass") {
		t.Fatalf("endpoint password leaked into error message: %v", err)
	}
}

// TestValidateCloudEndpoint_Cases covers AC 04-02 EC4 and the HTTPS-with-loopback
// exemption.
func TestValidateCloudEndpoint_Cases(t *testing.T) {
	cases := []struct {
		endpoint string
		ok       bool
	}{
		{"https://atcr.dev/dashboard", true},
		{"https://mock.test/ingest", true},
		{"http://127.0.0.1:8080/ingest", true}, // loopback http permitted (httptest)
		{"http://localhost:8080/ingest", true},
		{"http://[::1]:8080/ingest", true},
		{"http://example.com/ingest", false}, // plaintext remote
		{"", false},                          // empty
		{"not-a-url", false},                 // no scheme/host
		{"ftp://atcr.dev/x", false},          // wrong scheme
	}
	for _, c := range cases {
		err := ValidateCloudEndpoint(c.endpoint)
		if c.ok && err != nil {
			t.Errorf("ValidateCloudEndpoint(%q) = %v, want nil", c.endpoint, err)
		}
		if !c.ok && err == nil {
			t.Errorf("ValidateCloudEndpoint(%q) = nil, want error", c.endpoint)
		}
	}
}

// TestNewCloudSyncRecord_PersonasFromRealReviewers covers the Q2 caveat: each
// persona's hash is HashPersonaID(<real reviewer name>), NEVER the zero-value
// empty-string hash that hashing the aggregate record would produce.
func TestNewCloudSyncRecord_PersonasFromRealReviewers(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "sources", "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := `{"agents":[` +
		`{"agent":"greta","model":"gpt-x","tokens_in":200,"tokens_out":60,"duration_ms":1500},` +
		`{"agent":"bruce","model":"claude-y","tokens_in":100,"tokens_out":40,"duration_ms":900}` +
		`],"total":2,"succeeded":2}`
	if err := os.WriteFile(filepath.Join(poolDir, "summary.json"), []byte(summary), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := NewCloudSyncRecord(dir, "success")
	if rec.RunOutcome != "success" {
		t.Fatalf("RunOutcome = %q", rec.RunOutcome)
	}
	if !rec.MetricsAvailable {
		t.Fatal("MetricsAvailable = false, want true when pool summary is readable")
	}
	if len(rec.Personas) != 2 {
		t.Fatalf("Personas len = %d, want 2", len(rec.Personas))
	}
	emptyHash := HashPersonaID("")
	seen := map[string]bool{}
	for _, p := range rec.Personas {
		if p.PersonaIDHash == emptyHash {
			t.Fatalf("persona hash is the empty-string digest (aggregate zero-value leaked): %+v", p)
		}
		seen[p.PersonaIDHash] = true
	}
	if !seen[HashPersonaID("greta")] || !seen[HashPersonaID("bruce")] {
		t.Fatalf("persona hashes not sourced from real reviewer names: %+v", rec.Personas)
	}
	// Run-level aggregates: cost/tokens summed, latency = slowest agent.
	if rec.TokensIn != 300 || rec.TokensOut != 100 {
		t.Fatalf("token aggregates wrong: in=%d out=%d", rec.TokensIn, rec.TokensOut)
	}
	if rec.LatencyMS != 1500 {
		t.Fatalf("LatencyMS = %d, want 1500 (slowest agent)", rec.LatencyMS)
	}
}

// TestNewCloudSyncRecord_TrimsAgentNameBeforeHashing covers the cloudsync.go:102
// TD: the empty-agent gate trims (strings.TrimSpace), so the hash must hash the
// SAME trimmed value — otherwise a whitespace-padded reviewer name hashes to a
// different digest than its clean form and fragments the Persona Leaderboard into
// two buckets for one identity.
func TestNewCloudSyncRecord_TrimsAgentNameBeforeHashing(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "sources", "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := `{"agents":[` +
		`{"agent":"  greta  ","model":"gpt-x","tokens_in":200,"tokens_out":60,"duration_ms":1500}` +
		`],"total":1,"succeeded":1}`
	if err := os.WriteFile(filepath.Join(poolDir, "summary.json"), []byte(summary), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := NewCloudSyncRecord(dir, "success")
	if len(rec.Personas) != 1 {
		t.Fatalf("Personas len = %d, want 1", len(rec.Personas))
	}
	if got := rec.Personas[0].PersonaIDHash; got != HashPersonaID("greta") {
		t.Fatalf("padded agent hashed to %q, want the trimmed digest HashPersonaID(%q)", got, "greta")
	}
}

// TestNewCloudSyncRecord_MissingPoolSummary covers the best-effort degrade: an
// unreadable pool summary still yields an outcome-carrying record (no panic).
func TestNewCloudSyncRecord_MissingPoolSummary(t *testing.T) {
	rec := NewCloudSyncRecord(t.TempDir(), "failure")
	if rec.RunOutcome != "failure" {
		t.Fatalf("RunOutcome = %q, want failure", rec.RunOutcome)
	}
	if len(rec.Personas) != 0 {
		t.Fatalf("expected no personas without a pool summary, got %d", len(rec.Personas))
	}
}

// TestNewCloudSyncRecord_MissingPoolSummary_SignalsUnavailableMetrics covers AC
// 04-05: when the pool summary cannot be read the record must explicitly signal
// that metrics are unavailable so the backend can distinguish "no data" from
// "zero cost / zero reviewers".
func TestNewCloudSyncRecord_MissingPoolSummary_SignalsUnavailableMetrics(t *testing.T) {
	rec := NewCloudSyncRecord(t.TempDir(), "failure")
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	got, ok := m["metrics_available"]
	if !ok {
		t.Fatal("metrics_available field missing from cloud-sync record")
	}
	if got != false {
		t.Fatalf("metrics_available = %v, want false", got)
	}
}

// TestCloudSyncRecord_NotSupersetOfPublicRecord covers the plan's risk mitigation:
// CloudSyncRecord shares no field NAME with PublicRecord's leaderboard allowlist
// beyond the innocuous "model" identity, so it cannot silently become a superset.
func TestCloudSyncRecord_NotSupersetOfPublicRecord(t *testing.T) {
	pub := jsonKeys(t, PublicRecord{})
	cloud := jsonKeys(t, CloudSyncRecord{})
	for k := range cloud {
		if k == "model" {
			continue // shared, non-PII identity — allowed on both
		}
		if pub[k] {
			t.Errorf("CloudSyncRecord reuses PublicRecord field %q — must stay a distinct allowlist", k)
		}
	}
}

// assertAllowlisted fails if the marshaled cloud-sync body carries any disallowed
// key (raw source, file paths, or un-hashed identifiers), at the top level or
// inside a persona entry (AC 04-02 EC3).
func assertAllowlisted(t *testing.T, top map[string]any) {
	t.Helper()
	disallowed := []string{"path", "source", "file", "reviewer", "reviewers", "run_id", "runid", "persona", "diff"}
	check := func(m map[string]any) {
		for _, bad := range disallowed {
			if _, ok := m[bad]; ok {
				t.Errorf("disallowed key %q present in cloud-sync payload", bad)
			}
		}
	}
	check(top)
	if ps, ok := top["personas"].([]any); ok {
		for _, p := range ps {
			if pm, ok := p.(map[string]any); ok {
				check(pm)
			}
		}
	}
}

func jsonKeys(t *testing.T, v any) map[string]bool {
	t.Helper()
	rt := reflect.TypeOf(v)
	keys := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name := tag
		for j := 0; j < len(tag); j++ {
			if tag[j] == ',' {
				name = tag[:j]
				break
			}
		}
		if name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
