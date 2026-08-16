package fanout

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initTruncatingRepo builds a two-file range whose diff exceeds the byte budget the
// test sets, so ApplyByteBudgetPreferEscalated sheds the larger file and keeps the
// smaller — the some-but-not-all case, which is the only one that can be silent
// (AllDropped already returns an error).
func initTruncatingRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte("package big\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base = run("rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.go"), []byte("package main\n\nfunc a() {}\n"), 0o644))
	var big strings.Builder
	big.WriteString("package big\n")
	for i := 0; i < 4000; i++ {
		big.WriteString("// a padding line that makes this file dominate the byte budget\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte(big.String()), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "head")
	head = run("rev-parse", "HEAD")
	return dir, base, head
}

// buildPayloads sheds WHOLE FILES largest-first when payload_byte_budget binds, and
// did so with no log line and no machine-readable record — while its two sibling
// builders (PrepareReviewFromRepo, PrepareReviewFromDiff) both warn on exactly this
// condition. On the 2026-08-15 external-repo run that silence cost 40 of 73 files:
// reviewers saw a quarter of the diff and the operator was told nothing.
func TestBuildPayloads_WarnsWhenTheByteBudgetShedsFiles(t *testing.T) {
	dir, base, head := initTruncatingRepo(t)

	cfg := &ReviewConfig{
		Registry: &registry.Registry{
			Agents: map[string]registry.AgentConfig{"greta": {Model: "m", Payload: string(payload.ModeDiff)}},
		},
		Project:  &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings: registry.Settings{PayloadByteBudget: 2048},
	}

	var buf bytes.Buffer
	logger, err := log.New("debug", "json", &buf)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), logger)

	payloads, _, err := buildPayloads(ctx, cfg, dir, base, head, false)
	require.NoError(t, err)
	require.Len(t, payloads, 1)
	var mp modePayload
	for _, v := range payloads {
		mp = v
	}
	require.True(t, mp.Truncation.Truncated,
		"precondition: the budget must have shed at least one file for this test to mean anything")
	require.NotEmpty(t, mp.Truncation.FilesDropped)

	got := buf.String()
	assert.Contains(t, got, "byte budget truncated",
		"the git-range builder must warn on the same condition its two siblings warn on")
	assert.Contains(t, got, "files_dropped",
		"the warning must name WHICH files were shed, not just that shedding happened")
}

// The chunked strategy renders every chunk-slot with a neutral payload.Truncation{}
// on purpose: truncation is a diff-wide event, so a per-chunk record would make each
// chunk claim the same dropped files, most of which never appear in it. The cost was
// that AgentStatus.Truncated/FilesDropped then stayed zero for every chunked agent,
// contradicting status.go's stated "never silent (AC 06-03)" contract.
//
// The honest place to record it is the merge: mergeResultGroup folds N chunk results
// into ONE persona record, so the diff-wide fact lands exactly once per agent.
func TestMergeResultGroup_PromotesDiffWideTruncationOntoTheMergedResult(t *testing.T) {
	trunc := payload.Truncation{Truncated: true, FilesDropped: []string{"dropped_a.go", "dropped_b.go"}}
	chunk := func(content string) Result {
		return Result{
			Agent: "greta", Status: StatusOK, Content: content,
			// Neutral per chunk — unchanged, and still what buildFallbackAgent reads.
			Truncation:     payload.Truncation{},
			DiffTruncation: trunc,
		}
	}

	merged := mergeResultGroup([]Result{chunk("f1"), chunk("f2")}, nil)

	assert.True(t, merged.Truncation.Truncated,
		"the merged persona record must carry the diff-wide truncation so statusFor reports it")
	assert.Equal(t, []string{"dropped_a.go", "dropped_b.go"}, merged.Truncation.FilesDropped)

	st := statusFor(merged, findingsResult{})
	assert.True(t, st.Truncated, "status.json is the artifact the contract promises this on")
	assert.Equal(t, []string{"dropped_a.go", "dropped_b.go"}, st.FilesDropped)
}

// A single-result group takes mergeChunkResults' fast path, which returns g[0]
// untouched. The promotion must not be shape-dependent: an agent whose chunk set
// collapsed to one result is exactly as truncated as one whose did not.
func TestMergeChunkResults_PromotesDiffWideTruncationOnTheSingleResultPath(t *testing.T) {
	merged := mergeChunkResults([]Result{{
		Agent: "greta", Status: StatusOK,
		Truncation:     payload.Truncation{},
		DiffTruncation: payload.Truncation{Truncated: true, FilesDropped: []string{"only.go"}},
	}})

	require.Len(t, merged, 1)
	assert.True(t, merged[0].Truncation.Truncated)
	assert.Equal(t, []string{"only.go"}, merged[0].Truncation.FilesDropped)
}

// The bulk path already carries the real diff-wide truncation in Truncation itself.
// Promotion must never overwrite a per-slot record that is already populated.
func TestMergeResultGroup_DoesNotOverwriteAnExistingTruncationRecord(t *testing.T) {
	own := payload.Truncation{Truncated: true, FilesDropped: []string{"this_slots_own.go"}}
	r := func() Result {
		return Result{
			Agent: "greta", Status: StatusOK, Content: "x",
			Truncation:     own,
			DiffTruncation: payload.Truncation{Truncated: true, FilesDropped: []string{"diff_wide.go"}},
		}
	}
	merged := mergeResultGroup([]Result{r(), r()}, nil)
	assert.Equal(t, []string{"this_slots_own.go"}, merged.Truncation.FilesDropped,
		"a slot that recorded its own shed keeps it; the diff-wide value is a fallback, not an override")
}
