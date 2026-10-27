package fanout

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callLog records the order the visual stage's three steps ran in.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, s)
}

func (l *callLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

// orderAuthor is call 1's fake: it records the prompt and answers with a spec.
type orderAuthor struct {
	log    *callLog
	prompt string
}

func (a *orderAuthor) Persona() string {
	return "# {{.AgentName}} — frontend QA reviewer\n\n{{.Payload}}"
}

func (a *orderAuthor) Complete(_ context.Context, prompt string) (string, error) {
	a.log.add("author")
	a.prompt = prompt
	return "```ts\nimport { test } from '@playwright/test';\ntest('home', async () => {});\n```\n", nil
}

// orderBrowser is the browser's fake: it records the run and returns one
// screenshot.
type orderBrowser struct {
	log *callLog
	run BrowserRun
}

func (b *orderBrowser) Name() string                    { return "fake-browser" }
func (b *orderBrowser) Preflight(context.Context) error { return nil }
func (b *orderBrowser) Run(_ context.Context, run BrowserRun) (BrowserResult, error) {
	b.log.add("browser")
	b.run = run
	return BrowserResult{Backend: b.Name(), Screenshots: []BrowserScreenshot{{Name: "home", Path: "home.png", Data: []byte("\x89PNG\r\n\x1a\nfake")}}}, nil
}

// orderVision is call 2's fake chatter: it records the request and answers
// with one out-of-scope finding on the spec.
type orderVision struct {
	log      *callLog
	messages []llmclient.Message
	reply    string
}

func (v *orderVision) Chat(_ context.Context, _ llmclient.Invocation, messages []llmclient.Message, _ []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	v.log.add("review")
	v.messages = messages
	reply := v.reply
	return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", Content: &reply}}, nil
}

func newOrderStage(t *testing.T) (*VisualStage, *callLog, *orderAuthor, *orderBrowser, *orderVision) {
	t.Helper()
	l := &callLog{}
	author := &orderAuthor{log: l}
	browser := &orderBrowser{log: l}
	vision := &orderVision{log: l, reply: "NO FINDINGS"}
	stage := &VisualStage{
		Author:     author,
		Browser:    browser,
		Vision:     vision,
		Agent:      VisionAgent{Name: "eye", Agent: registry.AgentConfig{Model: "m-eye", SupportsVision: true}, Loopback: true},
		PreviewURL: "http://localhost:3000",
	}
	return stage, l, author, browser, vision
}

// T5 success criterion: with fakes for all three steps, the stage runs the
// author call, then the browser on the spec it wrote, then the review call.
func TestVisualStage_RunsAuthorThenBrowserThenReview(t *testing.T) {
	stage, l, author, browser, vision := newOrderStage(t)
	repo := t.TempDir()

	res, err := stage.RunVisual(context.Background(), VisualStageInput{
		RepoDir: repo,
		RunID:   "2026-06-10_test",
		Prompt:  payload.PayloadContext{Payload: "diff --git a/app.tsx b/app.tsx\n"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"author", "browser", "review"}, l.get())

	assert.Equal(t, "e2e/felix-2026-06-10-test.spec.ts", browser.run.Spec, "the browser runs the spec call 1 wrote")
	assert.Equal(t, repo, browser.run.RepoDir)
	assert.Equal(t, "http://localhost:3000", browser.run.PreviewURL)
	_, statErr := os.Stat(filepath.Join(repo, "e2e", "felix-2026-06-10-test.spec.ts"))
	assert.NoError(t, statErr)

	assert.Contains(t, author.prompt, visualAuthorCallNote, "call 1 says which call it is")
	assert.Contains(t, author.prompt, "diff --git a/app.tsx")
	require.Len(t, vision.messages, 1)
	text := vision.messages[0].Parts[len(vision.messages[0].Parts)-1].Text
	assert.Contains(t, text, visualReviewCallNote, "call 2 says which call it is")
	assert.Contains(t, text, "e2e/felix-2026-06-10-test.spec.ts")
	assert.Equal(t, StatusOK, res.Result.Status)
}

// A failing step stops the stage: nothing after it runs.
func TestVisualStage_StopsAtTheFirstFailingStep(t *testing.T) {
	stage, l, _, _, _ := newOrderStage(t)
	repo := t.TempDir()
	// An existing spec makes call 1 refuse before the model is called.
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "e2e"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "e2e", "felix-r1.spec.ts"), []byte("mine"), 0o644))

	_, err := stage.RunVisual(context.Background(), VisualStageInput{RepoDir: repo, RunID: "r1"})
	require.ErrorIs(t, err, ErrSpecExists)
	assert.Empty(t, l.get(), "neither the browser nor the review runs after call 1 fails")
}

func TestVisualSpecRunID_MapsAReviewIDToTheSpecNamePattern(t *testing.T) {
	cases := map[string]string{
		"2026-06-10_test":                   "2026-06-10-test",
		"2026-06-10_Feature.X":              "2026-06-10-feature-x",
		"2026-06-10_a__b-120000":            "2026-06-10-a-b-120000",
		"__":                                "review",
		strings.Repeat("a", 100):            strings.Repeat("a", 64-len(VisualPersonaName)-1),
		"x" + strings.Repeat("_", 70) + "y": "x-y",
	}
	for in, want := range cases {
		got := visualSpecRunID(in)
		assert.Equal(t, want, got, in)
		_, err := visualSpecName(got)
		assert.NoError(t, err, "%q must make a valid spec name", in)
	}
}

// felixConfig is twoAgentConfig with felix in the roster and the registry.
func felixConfig() *ReviewConfig {
	cfg := twoAgentConfig("http://unused")
	cfg.Registry.Agents[VisualPersonaName] = registry.AgentConfig{Provider: "p", Model: "m-felix", Persona: VisualPersonaName}
	cfg.Project.Agents = append(cfg.Project.Agents, VisualPersonaName)
	return cfg
}

func captureLog(ctx context.Context) (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	return log.NewContext(ctx, slog.New(slog.NewTextHandler(&buf, nil))), &buf
}

// T5 success criterion: felix in the roster gets no diff-reviewer slot, and
// the manifest roster (what status and resume read) leaves it out too.
func TestPrepareReview_FelixGetsNoReviewerSlot(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	cfg := felixConfig()

	prep, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	var names []string
	for _, s := range prep.Slots {
		names = append(names, s.Primary.Name)
	}
	assert.ElementsMatch(t, []string{"greta", "kai"}, names)
	assert.ElementsMatch(t, []string{"greta", "kai"}, readManifestFile(t, prep.Dir).Roster)
}

// T5 success criterion: felix in the roster with no visual stage (the CLI
// builds none without --preview-url) is skipped with a warning.
func TestPrepareReview_FelixWithoutPreviewURLIsSkippedWithAWarning(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	ctx, buf := captureLog(context.Background())

	prep, err := PrepareReview(ctx, felixConfig(), reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	assert.Nil(t, prep.visual)
	assert.Contains(t, buf.String(), "level=WARN")
	assert.Contains(t, buf.String(), VisualPersonaName)
	assert.Contains(t, buf.String(), "--preview-url")
}

func TestPrepareReview_NoFelixNoVisualWarning(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	ctx, buf := captureLog(context.Background())

	_, err := PrepareReview(ctx, twoAgentConfig("http://unused"), reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "--preview-url")
}

// fakeVisual is a VisualReviewer that records its input and the moment it ran
// relative to the reviewers.
type fakeVisual struct {
	log   *callLog
	in    VisualStageInput
	reply string
	err   error
}

func (f *fakeVisual) RunVisual(_ context.Context, in VisualStageInput) (VisualReviewResult, error) {
	f.log.add("visual")
	f.in = in
	if f.err != nil {
		return VisualReviewResult{}, f.err
	}
	return VisualReviewResult{Result: Result{Agent: "eye", Content: f.reply, Status: StatusOK, Model: "m-eye", ImagesSentOffMachine: true}}, nil
}

// orderedCompleter is a reviewer completer that records into the shared log.
type orderedCompleter struct{ log *callLog }

func (c orderedCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	c.log.add("reviewer:" + inv.Model)
	return "NO FINDINGS", nil
}

// With a visual stage, the review runs it after the reviewers and writes its
// result as felix, with its findings in the pool and the off-machine flag in
// status.json.
func TestRunReview_VisualStageRunsAfterTheReviewersAsFelix(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	l := &callLog{}
	vis := &fakeVisual{log: l, reply: "```json\n[{\"severity\":\"HIGH\",\"file_line\":\"e2e/felix-x.spec.ts:9\",\"problem\":\"Pay button clipped\",\"fix\":\"Remove overflow hidden\",\"category\":\"out-of-scope\",\"est_minutes\":10,\"evidence\":\"home screenshot\"}]\n```\n"}
	req := reviewReq(repo, repo, base, head)
	req.Visual = vis

	res, err := RunReview(context.Background(), orderedCompleter{log: l}, felixConfig(), req)
	require.NoError(t, err)
	calls := l.get()
	require.Len(t, calls, 3)
	assert.Equal(t, "visual", calls[2], "the visual stage runs after every reviewer")
	assert.NotContains(t, calls, "reviewer:m-felix")

	abs, _ := filepath.Abs(repo)
	assert.Equal(t, abs, vis.in.RepoDir)
	assert.Equal(t, res.ID, vis.in.RunID)
	assert.Contains(t, vis.in.Prompt.Payload, "diff --git", "felix sees the reviewers' diff")
	assert.Equal(t, VisualPersonaName, vis.in.Prompt.AgentName)

	st := statusJSON(t, res.Dir, VisualPersonaName)
	assert.Equal(t, StatusOK, st["status"])
	assert.Equal(t, true, st["images_sent_off_machine"])
	findings, err := os.ReadFile(filepath.Join(res.Dir, "sources", "pool", findingsFile))
	require.NoError(t, err)
	assert.Contains(t, string(findings), "Pay button clipped", "the out-of-scope visual finding survives the grounding gate")
}

// A visual stage failure is felix's failure, not the review's.
func TestRunReview_VisualStageFailureIsRecordedOnFelix(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	req := reviewReq(repo, repo, base, head)
	req.Visual = &fakeVisual{log: &callLog{}, err: errors.New("browser exploded")}

	res, err := RunReview(context.Background(), orderedCompleter{log: &callLog{}}, felixConfig(), req)
	require.NoError(t, err)
	st := statusJSON(t, res.Dir, VisualPersonaName)
	assert.Equal(t, StatusFailed, st["status"])
	assert.Contains(t, st["error"], "browser exploded")
	assert.True(t, res.Summary.Partial)
}

// A visual stage handed in without felix in the roster does not run.
func TestRunReview_VisualStageNeedsFelixInTheRoster(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	l := &callLog{}
	req := reviewReq(repo, repo, base, head)
	req.Visual = &fakeVisual{log: l}

	_, err := RunReview(context.Background(), orderedCompleter{log: l}, twoAgentConfig("http://unused"), req)
	require.NoError(t, err)
	assert.NotContains(t, l.get(), "visual")
}

func TestPrepareReviewFromRepo_RefusesAVisualStage(t *testing.T) {
	repo, _, _ := initRepo(t)
	req := reviewReq(repo, repo, "", "")
	req.Visual = &fakeVisual{log: &callLog{}}
	_, err := PrepareReviewFromRepo(context.Background(), felixConfig(), req)
	assert.ErrorIs(t, err, ErrVisualNeedsRange)
}

func TestPrepareReviewFromDiff_RefusesAVisualStage(t *testing.T) {
	repo, _, _ := initRepo(t)
	req := reviewReq(repo, repo, "", "")
	req.Visual = &fakeVisual{log: &callLog{}}
	_, err := PrepareReviewFromDiff(context.Background(), felixConfig(), req, "diff --git a/x b/x\n")
	assert.ErrorIs(t, err, ErrVisualNeedsRange)
}

// A resume rebuilds the pool from the manifest roster, which leaves felix out;
// felix's record from the original run must still reach the rebuilt pool.
func TestRebuildPool_KeepsTheVisualRecordOnResume(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	req := reviewReq(repo, repo, base, head)
	req.Visual = &fakeVisual{log: &callLog{}, reply: "```json\n[{\"severity\":\"HIGH\",\"file_line\":\"e2e/felix-x.spec.ts:9\",\"problem\":\"Pay button clipped\",\"fix\":\"f\",\"category\":\"out-of-scope\",\"est_minutes\":10,\"evidence\":\"e\"}]\n```\n"}
	res, err := RunReview(context.Background(), orderedCompleter{log: &callLog{}}, felixConfig(), req)
	require.NoError(t, err)
	poolDir := filepath.Join(res.Dir, "sources", "pool")

	roster := visualRebuildRoster(readManifestFile(t, res.Dir).Roster, poolDir)
	assert.Equal(t, []string{"greta", "kai", VisualPersonaName}, roster)
	_, statuses, err := RebuildPool(context.Background(), poolDir, roster)
	require.NoError(t, err)
	assert.Len(t, statuses, 3)
	findings, err := os.ReadFile(filepath.Join(poolDir, findingsFile))
	require.NoError(t, err)
	assert.Contains(t, string(findings), "Pay button clipped")

	assert.Equal(t, []string{"greta"}, visualRebuildRoster([]string{"greta"}, t.TempDir()), "no felix record, no felix")
}

// A roster of felix alone has no reviewer to dispatch; without a visual stage
// the review is refused before anything is scaffolded.
func TestPrepareReview_FelixAloneWithoutAVisualStageIsAnEmptyRoster(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := felixConfig()
	cfg.Project.Agents = []string{VisualPersonaName}
	_, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	assert.ErrorIs(t, err, ErrEmptyRoster)
}

// With a visual stage, felix alone is a valid roster: the review runs only it.
func TestRunReview_FelixAloneRunsOnlyTheVisualStage(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	cfg := felixConfig()
	cfg.Project.Agents = []string{VisualPersonaName}
	l := &callLog{}
	req := reviewReq(repo, repo, base, head)
	req.Visual = &fakeVisual{log: l, reply: "NO FINDINGS"}

	res, err := RunReview(context.Background(), orderedCompleter{log: l}, cfg, req)
	require.NoError(t, err)
	assert.Equal(t, []string{"visual"}, l.get())
	assert.Equal(t, 1, res.Summary.Succeeded)
}
