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
// trusted client so the HTTPS-only send path succeeds against the self-signed
// cert. Same-package (white-box) access to the unexported httpClient field is the
// injection seam; production callers only ever see New(endpoint).
func newTestClient(ts *httptest.Server) *Client {
	c := New(ts.URL)
	c.httpClient = ts.Client()
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
func TestClient_Send_BoundsInFlightGoroutines(t *testing.T) {
	const burst = 300
	var cur, maxSeen int32
	block := make(chan struct{})
	restore := SetDoRequestForTest(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&cur, 1)
		for {
			m := atomic.LoadInt32(&maxSeen)
			if n <= m || atomic.CompareAndSwapInt32(&maxSeen, m, n) {
				break
			}
		}
		<-block // hold the slot so concurrency accumulates
		atomic.AddInt32(&cur, -1)
		return nil, errors.New("blocked stub")
	})
	defer restore()

	c := New("https://telemetry.test/ingest")
	for i := 0; i < burst; i++ {
		c.Send(context.Background(), Event{Event: "review_run"})
	}
	// Let the spawned goroutines reach the blocking seam before sampling the peak.
	time.Sleep(150 * time.Millisecond)
	peak := atomic.LoadInt32(&maxSeen)
	close(block)
	c.Wait()

	if peak > int32(maxInFlightSends) {
		t.Fatalf("peak concurrent in-flight sends = %d, want <= %d (Send must bound goroutines)", peak, maxInFlightSends)
	}
	if peak == 0 {
		t.Fatal("no sends reached the seam — test wiring is broken")
	}
}
