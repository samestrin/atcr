package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient points a Client at an httptest TLS server, wiring the server's
// trusted Transport so the HTTPS-only send path succeeds against the self-signed
// cert. Only the Transport is swapped — the production CheckRedirect policy stays
// in force, so tests exercise redirect handling exactly as production runs it.
// Same-package (white-box) access to the unexported httpClient field is the
// injection seam; production callers only ever see New(endpoint).
func newTestClient(ts *httptest.Server) *Client {
	c := New(ts.URL)
	c.httpClient.Transport = ts.Client().Transport
	return c
}

// TestClient_Send_FiresFromGoroutine asserts Send dispatches the POST on a
// background goroutine (the call returns without blocking on the response) and
// the request is observed asynchronously: correct method, JSON content-type, and
// the exact four-key allowlisted body (AC 01-01).
func TestClient_Send_FiresFromGoroutine(t *testing.T) {
	var (
		gotMethod, gotCT string
		gotBody          map[string]any
		hits             int32
	)
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	c.Send(context.Background(), Event{Event: "review_run", Lang: "go", Lines: 450, Status: "success"})
	c.Wait() // drain the fire-and-forget goroutine so the assertions are deterministic

	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("expected exactly 1 telemetry request, got %d", n)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	wantKeys := map[string]bool{"event": true, "lang": true, "lines": true, "status": true}
	for k := range gotBody {
		if !wantKeys[k] {
			t.Errorf("unexpected key %q in telemetry payload", k)
		}
	}
	for k := range wantKeys {
		if _, ok := gotBody[k]; !ok {
			t.Errorf("missing key %q in telemetry payload", k)
		}
	}
}

// TestClient_Send_BoundedTimeout_UnblocksOnHangOrUnreachable proves the caller
// is never blocked by a hung endpoint: Send returns effectively instantly, and
// the background goroutine is itself bounded by requestTimeout so it exits
// cleanly rather than leaking (AC 01-02).
func TestClient_Send_BoundedTimeout_UnblocksOnHangOrUnreachable(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // hang until the test releases the handler
	}))
	defer ts.Close()
	defer close(release)

	c := newTestClient(ts)
	c.requestTimeout = 50 * time.Millisecond

	start := time.Now()
	c.Send(context.Background(), Event{Event: "review_run", Status: "success"})
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("Send blocked the caller for %v; must return immediately", elapsed)
	}

	// The in-flight request is bounded by requestTimeout, so draining completes
	// well before the hung handler would ever respond.
	done := make(chan struct{})
	go func() { c.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background telemetry goroutine did not exit within its bounded timeout")
	}
}

// TestClient_Send_RecoversFromInternalPanic forces a panic inside the goroutine
// body via the doRequest seam and asserts it is recovered — the parent never
// crashes and no panic propagates (AC 01-03).
func TestClient_Send_RecoversFromInternalPanic(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	restore := SetDoRequestForTest(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		panic("forced telemetry panic")
	})
	defer restore()

	c := newTestClient(ts)
	c.Send(context.Background(), Event{Event: "review_run", Status: "failure"})
	c.Wait() // if the panic were not recovered, this goroutine would crash the test binary

	// Reaching here means the defer recover() swallowed the panic.
}

// TestClient_Send_PayloadHasExactlyFourAllowlistedKeys locks the wire schema to
// exactly {event, lang, lines, status} with no omitempty ambiguity — an
// accidental new field (e.g. a file path) fails this immediately (AC 01-04).
func TestClient_Send_PayloadHasExactlyFourAllowlistedKeys(t *testing.T) {
	cases := []Event{
		{Event: "review_run", Lang: "go", Lines: 450, Status: "success"},
		{}, // zero value: all four keys must still serialize (no omitempty)
	}
	for _, ev := range cases {
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal Event: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if len(m) != 4 {
			t.Fatalf("payload has %d keys, want exactly 4: %s", len(m), raw)
		}
		for _, k := range []string{"event", "lang", "lines", "status"} {
			if _, ok := m[k]; !ok {
				t.Errorf("missing allowlisted key %q in %s", k, raw)
			}
		}
	}
}

// TestClient_Send_EmptyEndpointNoOps proves an unset endpoint short-circuits
// before any goroutine spawns or request is attempted — the seam Story 2's
// opt-out mode reuses (AC 01-01 Edge Case 1).
func TestClient_Send_EmptyEndpointNoOps(t *testing.T) {
	var calls int32
	orig := currentDoRequest()
	doRequest.Store(doRequestFunc(func(client *http.Client, req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return orig(client, req)
	}))
	defer func() { doRequest.Store(orig) }()

	c := New("") // empty endpoint
	c.Send(context.Background(), Event{Event: "review_run", Status: "success"})
	c.Wait()

	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("empty-endpoint client attempted %d request(s); want 0 (no-op)", n)
	}
}

// TestClient_Send_NilReceiverNoOps guards the nil-client path so a missing
// (never-injected) client is a safe no-op rather than a nil dereference.
func TestClient_Send_NilReceiverNoOps(t *testing.T) {
	var c *Client
	c.Send(context.Background(), Event{Event: "review_run"}) // must not panic
	c.Wait()
}

// TestClient_Send_SetDoRequestForTest_NoRace exercises concurrent sends and
// concurrent mutation of the doRequest seam via SetDoRequestForTest. Under -race
// this reproduces the data race between the detached send goroutine reading the
// package global and another goroutine swapping it (TD-015).
func TestClient_Send_SetDoRequestForTest_NoRace(t *testing.T) {
	// Capture the production seam so we can restore it deterministically after
	// the concurrent mutation exercises below.
	orig := currentDoRequest()
	defer func() { doRequest.Store(orig) }()

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow enough to keep sends in flight while the seam is mutated.
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newTestClient(ts)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.Send(context.Background(), Event{Event: "review_run", Status: "success"})
		}()
		go func() {
			defer wg.Done()
			restore := SetDoRequestForTest(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
				return nil, errors.New("intercepted")
			})
			time.Sleep(time.Millisecond)
			restore()
		}()
	}
	wg.Wait()
	c.Wait()
}

// TestIsHTTPS_RequiresHost asserts isHTTPS rejects structurally-invalid HTTPS
// URLs that lack a host, preventing the client from spawning a goroutine for an
// endpoint that can never succeed (TD-016).
func TestIsHTTPS_RequiresHost(t *testing.T) {
	cases := []struct {
		endpoint string
		want     bool
	}{
		{"https://example.com/path", true},
		{"https://example.com", true},
		{"https://", false},
		{"https:///x", false},
		{"https:foo", false},
		{"", false},
		{"http://example.com", false},
	}
	for _, tc := range cases {
		got := isHTTPS(tc.endpoint)
		if got != tc.want {
			t.Errorf("isHTTPS(%q) = %v, want %v", tc.endpoint, got, tc.want)
		}
	}
}

// TestClient_Send_SurvivesCancelledContext proves that cancelling the caller's
// command context before the detached goroutine runs does not abort the in-flight
// telemetry request — the request context must be detached from the caller's
// lifetime (TD-017).
func TestClient_Send_SurvivesCancelledContext(t *testing.T) {
	var calls int32
	restore := SetDoRequestForTest(func(_ *http.Client, req *http.Request) (*http.Response, error) {
		if req.Context().Err() != nil {
			t.Errorf("request context was already done: %v", req.Context().Err())
		}
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	defer restore()

	c := New("https://example.com")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the goroutine is scheduled
	c.Send(ctx, Event{Event: "review_run", Status: "success"})
	c.Wait()

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("cancelled context prevented send; got %d request(s), want 1", n)
	}
}

// TestContext_RoundTrip covers the context injection seam: a client stored via
// NewContext is returned by FromContext, and a bare context yields nil (whose
// Send is a safe no-op).
func TestContext_RoundTrip(t *testing.T) {
	c := New("https://example.test")
	got := FromContext(NewContext(context.Background(), c))
	if got != c {
		t.Fatalf("FromContext returned %p, want %p", got, c)
	}
	if FromContext(context.Background()) != nil {
		t.Fatal("FromContext on a bare context must return nil")
	}
}

// TestClient_Send_Non2xxIsSwallowed drives the non-2xx branch: the endpoint
// returns 500, the request is still made, and the caller is unaffected (the
// failure is logged at debug and swallowed).
func TestClient_Send_Non2xxIsSwallowed(t *testing.T) {
	var hits int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	c.Send(context.Background(), Event{Event: "review_run", Status: "failure"})
	c.Wait()

	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("expected 1 request to the 500 endpoint, got %d", n)
	}
}

// TestClient_Send_ConcurrentSendsNoRace fires many overlapping sends from one
// process (the review + reconcile rapid-succession case) and drains them; run
// under -race it proves no shared mutable state is written unsafely (AC 01-01
// Edge Case 2).
func TestClient_Send_ConcurrentSendsNoRace(t *testing.T) {
	var hits int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Send(context.Background(), Event{Event: "review_run", Status: "success"})
		}()
	}
	wg.Wait()
	c.Wait()

	if n := atomic.LoadInt32(&hits); n != 25 {
		t.Fatalf("expected 25 telemetry requests, got %d", n)
	}
}

// TestClient_RequestTimeout_Race verifies no data race occurs on requestTimeout.
func TestClient_RequestTimeout_Race(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	c.requestTimeout = 10 * time.Millisecond

	for i := 0; i < 10; i++ {
		c.Send(context.Background(), Event{Event: "race_test"})
	}
	c.Wait()
}

// TestClient_Send_BoundsInFlightGoroutines covers the client.go:100 TD: Send must
// bound the number of concurrent background goroutines. A burst far larger than the
// cap fired against a blocking send seam must never exceed maxInFlightSends
// simultaneously in flight — excess pings are dropped, not spawned.
//
// Deterministic barrier instead of a fixed sleep: the seam signals an entry
// channel, and the test waits for the (cap+1)'th entry. If the cap is enforced,
// exactly maxInFlightSends entries ever occur and the wait ends on timeout — a
// held cap; if the cap is broken, the burst floods the seam and the (cap+1)'th
// entry arrives promptly — a proven breach. "The cap held" and "the test never
// got there" are distinguishable (the count==0 wiring check below).
func TestClient_Send_BoundsInFlightGoroutines(t *testing.T) {
	const burst = 300
	entered := make(chan struct{}, burst)
	block := make(chan struct{})
	restore := SetDoRequestForTest(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		<-block // hold the slot so concurrency accumulates
		return nil, errors.New("blocked stub")
	})
	defer restore()

	c := New("https://telemetry.test/ingest")
	for i := 0; i < burst; i++ {
		c.Send(context.Background(), Event{Event: "review_run"})
	}

	count := 0
	wait := time.NewTimer(2 * time.Second)
	defer wait.Stop()
barrier:
	for count < maxInFlightSends+1 {
		select {
		case <-entered:
			count++
		case <-wait.C:
			// No (cap+1)'th entry arrived: the cap held — every beyond-cap send was
			// dropped in dispatch before reaching the seam.
			break barrier
		}
	}
	close(block)
	c.Wait()

	if count > maxInFlightSends {
		t.Fatalf("%d sends reached the seam concurrently, want <= %d (Send must bound goroutines)", count, maxInFlightSends)
	}
	if count == 0 {
		t.Fatal("no sends reached the seam — test wiring is broken")
	}
}

// TestClient_Send_PerDestinationSemaphoreIndependence proves the in-flight cap is
// PER DESTINATION: with the usage-ping semaphore saturated by sends blocked inside
// the transport seam, the quality-signal surface must still dispatch. A single
// shared semaphore let a hung destination hold every slot and starve its sibling —
// the coupling the per-endpoint gate explicitly eliminated.
func TestClient_Send_PerDestinationSemaphoreIndependence(t *testing.T) {
	release := make(chan struct{})
	var qualityHits int32
	restore := SetDoRequestForTest(func(_ *http.Client, req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "quality-signal") {
			atomic.AddInt32(&qualityHits, 1)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		<-release // usage-ping sends hold their in-flight slots
		return nil, errors.New("blocked stub")
	})
	defer restore()

	c := NewWithQualitySignal(testUsageEndpoint, testQualityEndpoint)
	// Fire more usage pings than the cap: exactly maxInFlightSends acquire a slot
	// synchronously inside dispatch (the rest are dropped), so the usage semaphore
	// is deterministically full once these calls return.
	for i := 0; i < maxInFlightSends*2; i++ {
		c.Send(context.Background(), Event{Event: "review_run"})
	}
	c.SendQualitySignal(context.Background(), []QualitySignal{{PersonaIDHash: "h", Model: "m"}})

	// The quality signal has its own semaphore, so it must reach the seam promptly
	// even though every usage slot is held. Poll with a deadline rather than
	// sleeping a fixed interval.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&qualityHits) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	c.Wait()

	if n := atomic.LoadInt32(&qualityHits); n != 1 {
		t.Fatalf("quality signal dispatches = %d, want 1 — a saturated usage-ping semaphore must not starve the sibling surface", n)
	}
}

// TestClient_Send_RefusesHTTPSDowngradeRedirect proves the client never follows a
// redirect to a plaintext-http target: a 308 (which replays the POST body via
// GetBody) must be refused rather than transmit the payload in the clear. isHTTPS
// vets only the INITIAL URL; the CheckRedirect policy vets every hop.
func TestClient_Send_RefusesHTTPSDowngradeRedirect(t *testing.T) {
	var plaintextHits int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&plaintextHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer plain.Close()

	var tlsHits int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tlsHits, 1)
		w.Header().Set("Location", plain.URL+"/downgrade")
		w.WriteHeader(http.StatusPermanentRedirect) // 308 replays the POST body
	}))
	defer ts.Close()

	c := newTestClient(ts)
	c.Send(context.Background(), Event{Event: "review_run", Status: "success"})
	c.Wait()

	if n := atomic.LoadInt32(&tlsHits); n != 1 {
		t.Fatalf("expected 1 request to the TLS redirector, got %d", n)
	}
	if n := atomic.LoadInt32(&plaintextHits); n != 0 {
		t.Fatalf("telemetry payload followed an https->http downgrade: %d plaintext request(s), want 0", n)
	}
}

// --- Epic 35.12: two-destination routing ------------------------------------

// captureRequestURLs installs a do-request seam recording the destination URL and
// body of every outbound send, so a test can prove which payload went where. The
// two surfaces are distinguishable at the wire by body shape: the usage ping is a
// JSON object, the quality signal a JSON array.
func captureRequestURLs(t *testing.T) func() []struct{ URL, Body string } {
	t.Helper()
	var (
		mu   sync.Mutex
		hits []struct{ URL, Body string }
	)
	restore := SetDoRequestForTest(func(_ *http.Client, req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		mu.Lock()
		hits = append(hits, struct{ URL, Body string }{req.URL.String(), string(b)})
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	t.Cleanup(restore)
	return func() []struct{ URL, Body string } {
		mu.Lock()
		defer mu.Unlock()
		out := make([]struct{ URL, Body string }, len(hits))
		copy(out, hits)
		return out
	}
}

const (
	testUsageEndpoint   = "https://ingest.test/api/v1/telemetry"
	testQualityEndpoint = "https://ingest.test/api/v1/quality-signal"
)

// TestClient_NewWithQualitySignal_RoutesToDistinctEndpoints is Epic 35.12 AC1b: the
// quality signal must NOT share the usage-ping destination. A single shared
// `endpoint` field silently reintroduces the bug — the quality signal's JSON array
// lands on the usage handler, whose closed allowlist answers 400, and the fail-open
// path drops it with nothing surfacing the loss. Asserting on req.URL per call is
// what makes that regression impossible to reintroduce unnoticed.
func TestClient_NewWithQualitySignal_RoutesToDistinctEndpoints(t *testing.T) {
	hits := captureRequestURLs(t)

	c := NewWithQualitySignal(testUsageEndpoint, testQualityEndpoint)
	c.Send(context.Background(), Event{Event: "review_run", Lang: "go", Lines: 1, Status: "success"})
	c.SendQualitySignal(context.Background(), []QualitySignal{{PersonaIDHash: "h", Model: "m"}})
	c.Wait()

	got := hits()
	if len(got) != 2 {
		t.Fatalf("expected 2 sends (one per surface), got %d: %+v", len(got), got)
	}
	for _, h := range got {
		isArray := strings.HasPrefix(strings.TrimSpace(h.Body), "[")
		want := testUsageEndpoint
		surface := "usage ping"
		if isArray {
			want = testQualityEndpoint
			surface = "quality signal"
		}
		if h.URL != want {
			t.Errorf("%s posted to %q, want %q", surface, h.URL, want)
		}
	}
}

// TestClient_NewWithQualitySignal_PerPathEmptyEndpointNoOps is Epic 35.12 AC1c: an
// empty endpoint is a silent no-op PER PATH — no request, no error, no panic — so
// the two constants can be activated on independent schedules rather than as one
// release. A shared field would make either empty value disable both surfaces.
func TestClient_NewWithQualitySignal_PerPathEmptyEndpointNoOps(t *testing.T) {
	t.Run("empty usage endpoint silences only the ping", func(t *testing.T) {
		hits := captureRequestURLs(t)
		c := NewWithQualitySignal("", testQualityEndpoint)
		c.Send(context.Background(), Event{Event: "review_run", Status: "success"})
		c.SendQualitySignal(context.Background(), []QualitySignal{{PersonaIDHash: "h", Model: "m"}})
		c.Wait()

		got := hits()
		if len(got) != 1 {
			t.Fatalf("expected exactly 1 send (quality signal only), got %d: %+v", len(got), got)
		}
		if got[0].URL != testQualityEndpoint {
			t.Errorf("quality signal posted to %q, want %q", got[0].URL, testQualityEndpoint)
		}
	})

	t.Run("empty quality endpoint silences only the quality signal", func(t *testing.T) {
		hits := captureRequestURLs(t)
		c := NewWithQualitySignal(testUsageEndpoint, "")
		c.Send(context.Background(), Event{Event: "review_run", Status: "success"})
		c.SendQualitySignal(context.Background(), []QualitySignal{{PersonaIDHash: "h", Model: "m"}})
		c.Wait()

		got := hits()
		if len(got) != 1 {
			t.Fatalf("expected exactly 1 send (usage ping only), got %d: %+v", len(got), got)
		}
		if got[0].URL != testUsageEndpoint {
			t.Errorf("usage ping posted to %q, want %q", got[0].URL, testUsageEndpoint)
		}
	})
}

// TestNew_KeepsSharedDestinationForBothPayloads pins New's documented
// single-destination meaning. It is NOT an accident of the two-destination
// refactor: ~40 existing call sites (and every test that discriminates the two
// surfaces by body shape rather than URL) depend on one client sending both
// payloads to one place. Changing New to leave the quality destination unset would
// silently zero out those tests.
func TestNew_KeepsSharedDestinationForBothPayloads(t *testing.T) {
	hits := captureRequestURLs(t)

	c := New(testUsageEndpoint)
	c.Send(context.Background(), Event{Event: "review_run", Status: "success"})
	c.SendQualitySignal(context.Background(), []QualitySignal{{PersonaIDHash: "h", Model: "m"}})
	c.Wait()

	got := hits()
	if len(got) != 2 {
		t.Fatalf("expected 2 sends, got %d: %+v", len(got), got)
	}
	for _, h := range got {
		if h.URL != testUsageEndpoint {
			t.Errorf("New(endpoint) must send both payloads to %q, got %q", testUsageEndpoint, h.URL)
		}
	}
}

// TestClient_SendQualitySignal_NilReceiverNoOps is the quality-signal twin of
// TestClient_Send_NilReceiverNoOps. It guards a real panic path introduced with
// the second destination: SendQualitySignal reads its endpoint off the receiver
// BEFORE dispatch's own nil check runs, so the accessor must be nil-safe.
func TestClient_SendQualitySignal_NilReceiverNoOps(t *testing.T) {
	var c *Client
	c.SendQualitySignal(context.Background(), []QualitySignal{{PersonaIDHash: "h", Model: "m"}}) // must not panic
	c.Wait()
}
