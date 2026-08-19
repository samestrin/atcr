package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturingCtx returns a context carrying a logger that writes to the returned
// buffer — the seam cli.Main/MainWithHooks binds to the caller-supplied stderr.
func capturingCtx() (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return log.NewContext(context.Background(), l), &buf
}

// The truncated-to-zero warning wrote to os.Stderr directly, escaping the writer seam
// cli/main.go states as the convention ("routes through the same redirectable seam as
// the logger instead of escaping a caller-supplied stderr"). An embedded caller using
// MainWithHooks(ctx, stdout, stderr, hooks) captured warnVocabularyDiagnostics and
// every log line but not this one.
//
// buildPayloads' byte-budget warning (review.go) is the working instance of the same
// pattern on the same call graph, so this is the established route, not a borrowed one.
func TestWritePool_TruncatedZeroWarningHonoursTheInjectedWriter(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{{Agent: "greta", Status: StatusOK, ResponseTruncated: true, Content: ""}}

	ctx, buf := capturingCtx()
	// Nothing may reach the process stderr: the point is that the caller's writer
	// receives it instead.
	stray := captureStderr(t, func() {
		_, err := writePool(ctx, pool, results, nil, "")
		require.NoError(t, err)
	})

	assert.Contains(t, buf.String(), "contributed nothing",
		"the warning must reach the injected logger, which is what an embedded caller supplies")
	assert.Contains(t, buf.String(), "greta", "and must still name the agent")
	assert.NotContains(t, stray, "contributed nothing",
		"and must NOT bypass that writer by going straight to os.Stderr")
}

// Same seam on the resume path, which is where the warning is the ONLY message printed.
func TestRebuildPool_TruncatedZeroWarningHonoursTheInjectedWriter(t *testing.T) {
	poolDir := filepath.Join(t.TempDir(), "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		{Agent: "greta", Status: StatusOK, ResponseTruncated: true, Content: ""},
	}, nil))

	ctx, buf := capturingCtx()
	stray := captureStderr(t, func() {
		_, _, err := RebuildPool(ctx, poolDir, []string{"greta"})
		require.NoError(t, err)
	})

	assert.Contains(t, buf.String(), "contributed nothing")
	assert.NotContains(t, stray, "contributed nothing")
}

// status.json and summary.json are two published views of one AgentStatus. statusFor
// normalizes a nil FilesDropped to [], but RebuildPool round-trips statuses off disk
// and never passes through it — so a status.json missing the key re-marshalled as
// "files_dropped": null inside the rebuilt summary.json. That is the exact asymmetry
// the normalization was added to close, left open on the sibling path.
func TestRebuildPool_NormalizesFilesDroppedLikeStatusFor(t *testing.T) {
	poolDir := filepath.Join(t.TempDir(), "pool")
	// A status.json with NO files_dropped key at all — the shape that unmarshals to nil.
	agentDir := filepath.Join(poolDir, "raw", "agent", "greta")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "status.json"),
		[]byte(`{"agent":"greta","status":"ok","findings_count":1}`), 0o644))

	ctx, _ := capturingCtx()
	_, statuses, err := RebuildPool(ctx, poolDir, []string{"greta"})
	require.NoError(t, err)
	require.Len(t, statuses, 1)

	assert.NotNil(t, statuses[0].FilesDropped,
		"an absent files_dropped must publish as [] (measured, nothing dropped), never null (unmeasured)")

	var ps PoolSummary
	raw, err := os.ReadFile(filepath.Join(poolDir, summaryFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &ps))
	assert.NotContains(t, string(raw), `"files_dropped":null`,
		"summary.json must not disagree with status.json about whether the shed list was measured")
}

// The sibling asymmetry in the same function: `var statuses []AgentStatus` is nil when
// no agent dir is on disk and marshals as "agents": null, where writePool's
// make(..., 0, n) yields []. Same measured-vs-unmeasured distinction, same artifact.
func TestRebuildPool_EmptyRosterPublishesEmptyAgentsNotNull(t *testing.T) {
	poolDir := filepath.Join(t.TempDir(), "pool")
	// An EXISTING but empty agent dir: RebuildPool requires the dir, so this is the
	// reachable "roster produced no statuses" shape.
	require.NoError(t, os.MkdirAll(filepath.Join(poolDir, "raw", "agent"), 0o755))

	ctx, _ := capturingCtx()
	_, _, err := RebuildPool(ctx, poolDir, nil)
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(poolDir, summaryFile))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"agents":null`,
		`a roster that produced no statuses is an empty set, not an absent one`)
}

// captureWarnLog runs fn with a context carrying a capturing logger and returns what
// the logger received. It replaces captureStderr for the pool warnings, which now go
// through the redirectable seam rather than the process stderr — the whole point of
// this change, so the tests that assert their content must read them where they land.
func captureWarnLog(t *testing.T, fn func(ctx context.Context)) string {
	t.Helper()
	ctx, buf := capturingCtx()
	fn(ctx)
	return buf.String()
}
