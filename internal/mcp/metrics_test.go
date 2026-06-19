package mcp

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleMetricsReturnsPrometheus verifies the atcr_metrics handler renders
// the DefaultRegistry in Prometheus text format (Epic 4.4 AC4, resolved as an
// MCP tool rather than an HTTP endpoint — see the epic Clarifications).
func TestHandleMetricsReturnsPrometheus(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)
	metrics.Counter(metrics.NameReviewsTotal).Add(2)

	e := &engine{}
	_, out, err := e.handleMetrics(context.Background(), nil, MetricsArgs{})
	require.NoError(t, err)
	assert.Equal(t, "prometheus", out.Format)
	assert.Contains(t, out.Content, "atcr_reviews_total 2")
}

// TestHandleMetricsContextCancelled verifies a cancelled context short-circuits
// before rendering (parity with the other read handlers).
func TestHandleMetricsContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := &engine{}
	_, _, err := e.handleMetrics(ctx, nil, MetricsArgs{})
	require.Error(t, err)
}

// TestMetricsToolRegistered verifies the server advertises atcr_metrics.
func TestMetricsToolRegistered(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	assert.True(t, names[ToolMetrics], "atcr_metrics tool must be advertised")
}
