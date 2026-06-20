package llmclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompleteWithUsage_RecordsSuccessfulAttempt verifies a single successful
// round-trip surfaces exactly one CallRecord, marked ReachedWire with a non-zero
// Duration — the per-attempt telemetry the metrics layer counts and times.
func TestCompleteWithUsage_RecordsSuccessfulAttempt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		okResponse(w, "findings here")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	content, _, records, err := c.CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "review this",
	})
	require.NoError(t, err)
	assert.Equal(t, "findings here", content)
	require.Len(t, records, 1, "one successful attempt = one CallRecord")
	assert.True(t, records[0].ReachedWire, "a completed round-trip reached the wire")
	assert.Positive(t, records[0].Duration, "the attempt's duration must be stamped")
}

// TestCompleteWithUsage_RecordsEachRetry verifies dispatch accumulates one
// CallRecord per HTTP attempt: a 503-then-200 sequence yields two records, both
// having reached the wire. This is the per-attempt (retries-included) contract
// the counter relies on.
func TestCompleteWithUsage_RecordsEachRetry(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		okResponse(w, "ok")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	_, _, records, err := c.CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "review this",
	})
	require.NoError(t, err)
	require.Len(t, records, 2, "503 then 200 = two attempts = two CallRecords")
	for i, rec := range records {
		assert.Truef(t, rec.ReachedWire, "attempt %d got a provider response, so it reached the wire", i)
	}
}

// TestCompleteWithUsage_CancelBeforeSend_NotReachedWire verifies that a context
// cancelled before any bytes are written yields a record marked NOT ReachedWire,
// so the metrics layer counts it as zero API calls (AC2). The records slice is
// still non-nil (an attempt was entered) so the consumer uses per-record
// semantics rather than the Turns fallback.
func TestCompleteWithUsage_CancelBeforeSend_NotReachedWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		okResponse(w, "should not be reached")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call so the request is never written

	c := fastRetry(srv.Client())
	_, _, records, err := c.CompleteWithUsage(ctx, Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "review this",
	})
	require.Error(t, err)
	require.Len(t, records, 1, "the attempt was entered, so one record exists")
	assert.False(t, records[0].ReachedWire, "no bytes were written, so the call never reached the wire")
}
