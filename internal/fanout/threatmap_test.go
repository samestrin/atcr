package fanout

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// threatMapRepo builds a range that changes auth.go, style.css and README.md:
// the security-relevant file, and the two the mapper is expected to drop.
func threatMapRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	writeFileAt(t, dir, "auth.go", "package main\n\nfunc a() {}\n")
	writeFileAt(t, dir, "style.css", "body { color: red; }\n")
	writeFileAt(t, dir, "README.md", "# demo\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	base = gitRun(t, dir, "rev-parse", "HEAD")
	writeFileAt(t, dir, "auth.go", "package main\n\nfunc a() { b() }\n\nfunc b() {}\n")
	writeFileAt(t, dir, "style.css", "body { color: blue; }\n")
	writeFileAt(t, dir, "README.md", "# demo\n\nMore words.\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")
	head = gitRun(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

// fakeMapper is a ThreatMapper that records every call and answers with keep,
// or with err when err is set.
type fakeMapper struct {
	mu    sync.Mutex
	calls [][]string
	keep  []string
	err   error
}

func (f *fakeMapper) Map(_ context.Context, paths []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), paths...))
	if f.err != nil {
		return nil, f.err
	}
	return f.keep, nil
}

// recordingCompleter is a reviewer completer that keeps every prompt it was
// sent, per model, and answers with no findings.
type recordingCompleter struct {
	mu      sync.Mutex
	prompts map[string][]string
}

func (c *recordingCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.prompts == nil {
		c.prompts = map[string][]string{}
	}
	c.prompts[inv.Model] = append(c.prompts[inv.Model], inv.Prompt)
	return "NO FINDINGS", nil
}

// statusJSON reads an agent's status.json as a raw map, so a test can tell an
// absent key from an empty one.
func statusJSON(t *testing.T, reviewDir, agent string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(reviewDir, "sources", "pool", "raw", "agent", agent, "status.json"))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func readManifestFile(t *testing.T, reviewDir string) payload.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(reviewDir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

// Epic 37.0 T3 success criterion, first half: with a mapper that keeps only
// auth.go, every reviewer is sent auth.go and neither of the other two files,
// and each status.json lists the two in threat_map_dropped while the byte
// budget's files_dropped stays empty.
func TestRunReview_ThreatMap_SendsOnlyKeptFilesToEveryReviewer(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := threatMapRepo(t)
	cfg := twoAgentConfig("http://unused")
	mapper := &fakeMapper{keep: []string{"auth.go"}}
	req := reviewReq(repo, repo, base, head)
	req.ThreatMapper = mapper
	comp := &recordingCompleter{}

	res, err := RunReview(context.Background(), comp, cfg, req)
	require.NoError(t, err)

	require.Len(t, mapper.calls, 1, "the mapper is called once per run, not once per mode or agent")
	assert.Equal(t, []string{"README.md", "auth.go", "style.css"}, mapper.calls[0],
		"the mapper is shown every changed file, sorted")

	require.Len(t, comp.prompts, 2, "both reviewers ran")
	for model, prompts := range comp.prompts {
		for _, p := range prompts {
			assert.Contains(t, p, "auth.go", "%s must receive the kept file", model)
			assert.NotContains(t, p, "style.css", "%s must not receive a dropped file", model)
			assert.NotContains(t, p, "README.md", "%s must not receive a dropped file", model)
		}
	}

	for _, agent := range []string{"greta", "kai"} {
		st := statusJSON(t, res.Dir, agent)
		assert.Equal(t, []any{"README.md", "style.css"}, st["threat_map_dropped"], agent)
		assert.Equal(t, []any{}, st["files_dropped"], "%s: a mapper drop is not a budget shed", agent)
		assert.Equal(t, false, st["truncated"], agent)
	}

	m := readManifestFile(t, res.Dir)
	require.NotNil(t, m.ThreatMap)
	assert.Equal(t, []string{"auth.go"}, m.ThreatMap.Keep)
	assert.Equal(t, []string{"README.md", "style.css"}, m.ThreatMap.Dropped)
	assert.Empty(t, m.ThreatMap.Failed)

	audit := readBlocksPayload(t, res.Dir)
	assert.Contains(t, audit, "auth.go")
	assert.NotContains(t, audit, "style.css", "the audit payload artifact reflects the trimmed set")
}

// Epic 37.0 T3 success criterion, second half: a failing mapper fails open.
// Every reviewer gets all three files, status.json carries no
// threat_map_dropped key, and the manifest names the failure.
func TestRunReview_ThreatMap_FailingMapperReviewsEveryFile(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := threatMapRepo(t)
	cfg := twoAgentConfig("http://unused")
	mapper := &fakeMapper{err: errors.New("mapper timed out")}
	req := reviewReq(repo, repo, base, head)
	req.ThreatMapper = mapper
	comp := &recordingCompleter{}

	res, err := RunReview(context.Background(), comp, cfg, req)
	require.NoError(t, err)

	require.Len(t, comp.prompts, 2)
	for model, prompts := range comp.prompts {
		for _, p := range prompts {
			for _, f := range []string{"auth.go", "style.css", "README.md"} {
				assert.Contains(t, p, f, "%s must receive %s when the mapper failed", model, f)
			}
		}
	}
	for _, agent := range []string{"greta", "kai"} {
		st := statusJSON(t, res.Dir, agent)
		_, present := st["threat_map_dropped"]
		assert.False(t, present, "%s: nothing was dropped, so the omitempty key is absent", agent)
	}
	m := readManifestFile(t, res.Dir)
	require.NotNil(t, m.ThreatMap, "a failed map is still recorded, so a resume does not re-map")
	assert.Empty(t, m.ThreatMap.Keep)
	assert.Empty(t, m.ThreatMap.Dropped)
	assert.Contains(t, m.ThreatMap.Failed, "mapper timed out")
}

// Without a mapper nothing changes: no manifest record, no status key.
func TestRunReview_ThreatMap_OffByDefault(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := threatMapRepo(t)
	cfg := twoAgentConfig("http://unused")
	comp := &recordingCompleter{}

	res, err := RunReview(context.Background(), comp, cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)

	assert.Nil(t, readManifestFile(t, res.Dir).ThreatMap)
	for _, agent := range []string{"greta", "kai"} {
		_, present := statusJSON(t, res.Dir, agent)["threat_map_dropped"]
		assert.False(t, present, agent)
	}
	for _, prompts := range comp.prompts {
		for _, p := range prompts {
			assert.Contains(t, p, "style.css")
		}
	}
}

// The map runs before the byte budget: a budget that fits auth.go alone sheds
// nothing once the mapper has removed the other two, so files_dropped stays
// empty and the mapper's drops stay in their own field.
func TestPrepareReview_ThreatMap_TrimsBeforeTheByteBudget(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := threatMapRepo(t)
	cfg := twoAgentConfig("http://unused")

	// Size auth.go's entry so the budget fits it and nothing more.
	rb := payload.NewRangeBuilder(context.Background(), repo, base, head)
	entries, err := rb.BuildEntries(payload.ModeBlocks)
	require.NoError(t, err)
	var authBytes int64
	for _, e := range entries {
		if e.Path == "auth.go" {
			authBytes = int64(len(e.Body))
		}
	}
	require.NotZero(t, authBytes)
	cfg.Settings.PayloadByteBudget = authBytes

	req := reviewReq(repo, repo, base, head)
	req.ThreatMapper = &fakeMapper{keep: []string{"auth.go"}}
	prep, err := PrepareReview(context.Background(), cfg, req)
	require.NoError(t, err)
	for _, s := range prep.Slots {
		assert.False(t, s.Primary.Truncation.Truncated, s.Primary.Name)
		assert.Empty(t, s.Primary.Truncation.FilesDropped, "%s: the mapper ran first, so the budget shed nothing", s.Primary.Name)
	}
	assert.Equal(t, []string{"README.md", "style.css"}, prep.threatMapDropped)
}

// A resume applies the manifest's recorded scope and never calls the mapper
// again, even when the resume request carries one.
func TestPrepareResume_ThreatMap_ReusesRecordedScope(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := threatMapRepo(t)
	cfg := twoAgentConfig("http://unused")

	freshReq := reviewReq(repo, repo, base, head)
	freshReq.ThreatMapper = &fakeMapper{keep: []string{"auth.go"}}
	prep, err := PrepareReview(context.Background(), cfg, freshReq)
	require.NoError(t, err)

	again := &fakeMapper{keep: []string{"auth.go", "style.css", "README.md"}}
	resumeReq := reviewReq(repo, repo, base, head)
	resumeReq.ThreatMapper = again
	resumePrep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, resumeReq)
	require.NoError(t, err)

	assert.Empty(t, again.calls, "a resume must not re-map")
	require.NotEmpty(t, resumePrep.Slots)
	for _, s := range resumePrep.Slots {
		assert.Contains(t, s.Primary.Prompt, "auth.go", s.Primary.Name)
		assert.NotContains(t, s.Primary.Prompt, "style.css", s.Primary.Name)
		assert.NotContains(t, s.Primary.Prompt, "README.md", s.Primary.Name)
	}
	assert.Equal(t, []string{"README.md", "style.css"}, resumePrep.threatMapDropped)
}

// A resume of a run whose mapper failed keeps every file, as the original run did.
func TestPrepareResume_ThreatMap_FailedRecordKeepsEveryFile(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := threatMapRepo(t)
	cfg := twoAgentConfig("http://unused")

	freshReq := reviewReq(repo, repo, base, head)
	freshReq.ThreatMapper = &fakeMapper{err: errors.New("boom")}
	prep, err := PrepareReview(context.Background(), cfg, freshReq)
	require.NoError(t, err)

	resumePrep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	for _, s := range resumePrep.Slots {
		assert.Contains(t, s.Primary.Prompt, "style.css", s.Primary.Name)
		assert.Contains(t, s.Primary.Prompt, "README.md", s.Primary.Name)
	}
	assert.Empty(t, resumePrep.threatMapDropped)
}

// The phase is scoped to git-range reviews: the baseline and --diff-file
// preparations refuse a mapper rather than silently reviewing every file.
func TestPrepareReview_ThreatMap_RefusedOutsideGitRange(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, _, _ := threatMapRepo(t)
	cfg := twoAgentConfig("http://unused")
	req := reviewReq(repo, repo, "", "")
	req.ThreatMapper = &fakeMapper{keep: []string{"auth.go"}}

	_, err := PrepareReviewFromRepo(context.Background(), cfg, req)
	assert.ErrorIs(t, err, ErrThreatMapNeedsRange)

	_, err = PrepareReviewFromDiff(context.Background(), cfg, req, "diff --git a/x b/x\n")
	assert.ErrorIs(t, err, ErrThreatMapNeedsRange)
}
