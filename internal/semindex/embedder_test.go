package semindex

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// embedServer replies to every embedding call with vectors whose first element
// is the input's position across ALL calls, so a test can prove batching keeps
// the order. fail, when it returns a non-zero status, fails that call.
func embedServer(t *testing.T, fail func(call int) int) (*httptest.Server, *atomic.Int32, *[]int) {
	t.Helper()
	var calls atomic.Int32
	var sizes []int
	seen := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		assert.Equal(t, pinnedEmbedPath, r.URL.Path)
		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, pinnedModel, req.Model)
		if fail != nil {
			if code := fail(n); code != 0 {
				w.WriteHeader(code)
				return
			}
		}
		sizes = append(sizes, len(req.Input))
		type item struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		}
		items := make([]item, 0, len(req.Input))
		for i := len(req.Input) - 1; i >= 0; i-- {
			items = append(items, item{Index: i, Embedding: vec(float64(seen + i))})
		}
		seen += len(req.Input)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &sizes
}

func TestEmbed_BatchesAndKeepsInputOrder(t *testing.T) {
	srv, calls, sizes := embedServer(t, nil)
	reg := metrics.NewRegistry()
	e, err := NewEmbedder(Config{EmbeddingEndpoint: srv.URL, Metrics: reg})
	require.NoError(t, err)
	texts := make([]string, embedBatchSize+3)
	for i := range texts {
		texts[i] = "t"
	}
	vecs, err := e.Embed(context.Background(), texts)
	require.NoError(t, err)
	require.Len(t, vecs, len(texts))
	for i, v := range vecs {
		assert.Equal(t, float64(i), v[0], "vector %d out of order", i)
	}
	assert.Equal(t, int32(2), calls.Load())
	assert.Equal(t, []int{embedBatchSize, 3}, *sizes)
	assert.Zero(t, reg.Counter(UnreachableMetric).Value())
}

func TestEmbed_NoTextsMakesNoCall(t *testing.T) {
	srv, calls, _ := embedServer(t, nil)
	e, err := NewEmbedder(Config{EmbeddingEndpoint: srv.URL, Metrics: metrics.NewRegistry()})
	require.NoError(t, err)
	vecs, err := e.Embed(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, vecs)
	assert.Zero(t, calls.Load())
}

func TestEmbed_FailureMidRunIsOneCountedClassifiedError(t *testing.T) {
	srv, _, _ := embedServer(t, func(call int) int {
		if call == 2 {
			return http.StatusBadGateway
		}
		return 0
	})
	reg := metrics.NewRegistry()
	e, err := NewEmbedder(Config{EmbeddingEndpoint: srv.URL, Metrics: reg})
	require.NoError(t, err)
	vecs, err := e.Embed(context.Background(), make([]string, embedBatchSize+1))
	assert.Nil(t, vecs, "never a partial result")
	var se *Error
	require.True(t, errors.As(err, &se))
	assert.Equal(t, ClassStatus, se.Class)
	assert.Equal(t, int64(1), reg.Counter(UnreachableMetric).Value())
}

func TestEmbed_TotalBudgetSpansBatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(40 * time.Millisecond)
		var req struct{ Input []string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(embeddingReply(len(req.Input))))
	}))
	t.Cleanup(srv.Close)
	e, err := NewEmbedder(Config{EmbeddingEndpoint: srv.URL, CallTimeout: time.Second, TotalBudget: 60 * time.Millisecond, Metrics: metrics.NewRegistry()})
	require.NoError(t, err)
	_, err = e.Embed(context.Background(), make([]string, 2*embedBatchSize))
	var se *Error
	require.True(t, errors.As(err, &se))
	assert.Equal(t, ClassTimeout, se.Class)
}

func TestEmbed_RefusesPlainHTTPToNonLoopback(t *testing.T) {
	reg := metrics.NewRegistry()
	e, err := NewEmbedder(Config{EmbeddingEndpoint: "http://embed.example.com", Metrics: reg})
	require.NoError(t, err)
	_, err = e.Embed(context.Background(), []string{"x"})
	var se *Error
	require.True(t, errors.As(err, &se))
	assert.Equal(t, ClassInsecureScheme, se.Class)
	assert.Equal(t, int64(1), reg.Counter(UnreachableMetric).Value())

	e, err = NewEmbedder(Config{EmbeddingEndpoint: "http://embed.example.com", AllowInsecureHTTP: true, Metrics: reg})
	require.NoError(t, err)
	_, err = e.Embed(context.Background(), []string{"x"})
	require.True(t, errors.As(err, &se))
	assert.NotEqual(t, ClassInsecureScheme, se.Class, "allow_insecure_http lifts the refusal")
}

func TestNewEmbedder_NeedsNoQdrantOrCollection(t *testing.T) {
	e, err := NewEmbedder(Config{EmbeddingEndpoint: "https://embed.example.com"})
	require.NoError(t, err)
	assert.Equal(t, DefaultCallTimeout, e.c.cfg.CallTimeout)
	assert.Equal(t, DefaultTotalBudget, e.c.cfg.TotalBudget)

	for _, bad := range []Config{
		{},
		{EmbeddingEndpoint: "https://u:p@embed.example.com"},
		{EmbeddingEndpoint: "https://embed.example.com?k=v"},
		{EmbeddingEndpoint: "https://embed.example.com", CallTimeout: -1},
	} {
		_, err := NewEmbedder(bad)
		assert.Error(t, err, "%+v", bad)
	}
}
