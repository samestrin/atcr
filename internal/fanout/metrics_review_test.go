package fanout

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/stretchr/testify/require"
)

// TestWritePoolRecordsFindingMetrics verifies the findings emitted by agents are
// counted in total and bucketed by severity (Epic 4.4 atcr_findings_total /
// atcr_findings_by_severity). Findings are raw per-agent ("emitted by agents"),
// so two agents each emitting one HIGH counts as two.
func TestWritePoolRecordsFindingMetrics(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		okResult("greta", "CRITICAL|auth.go:42|Token|Fix|security|15|ev\nHIGH|main.go:88|Leak|Fix|concurrency|30|ev"),
		okResult("kai", "HIGH|x.go:1|p|f|cat|10|ev"),
	}
	_, err := WritePool(pool, results)
	require.NoError(t, err)

	check := func(name string, want int64) {
		t.Helper()
		if got := metrics.Counter(name).Value(); got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	check("atcr_findings_total", 3)
	check(metrics.Key("atcr_findings_by_severity", "severity", "HIGH"), 2)
	check(metrics.Key("atcr_findings_by_severity", "severity", "CRITICAL"), 1)
}

// TestRunReviewRecordsReviewMetrics is the Epic 4.4 integration test (AC9): a
// full review through RunReview increments atcr_reviews_total and the review/
// agent/findings metrics, end to end through the real HTTP client against a mock
// provider.
func TestRunReviewRecordsReviewMetrics(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)
	t.Setenv("ATCR_TEST_KEY", "secret")

	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.NotNil(t, res)

	check := func(name string, want int64) {
		t.Helper()
		if got := metrics.Counter(name).Value(); got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	check("atcr_reviews_total", 1)
	check("atcr_reviews_succeeded", 1)
	check("atcr_agents_total", 2)
	check("atcr_agents_succeeded", 2)
	check("atcr_findings_total", 2) // one CRITICAL per agent (mockProvider)
	check(metrics.Key("atcr_findings_by_severity", "severity", "CRITICAL"), 2)

	if got := metrics.Histogram("atcr_review_duration_seconds").Count(); got != 1 {
		t.Errorf("atcr_review_duration_seconds count = %d, want 1", got)
	}
}

// TestRecordReviewOutcome covers the three outcome branches directly, including
// interrupt precedence over failure.
func TestRecordReviewOutcome(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	recordReviewOutcome(false, false) // success
	recordReviewOutcome(false, true)  // failed
	recordReviewOutcome(true, true)   // interrupted wins over failed

	check := func(name string, want int64) {
		t.Helper()
		if got := metrics.Counter(name).Value(); got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	check("atcr_reviews_succeeded", 1)
	check("atcr_reviews_failed", 1)
	check("atcr_reviews_interrupted", 1)
}
