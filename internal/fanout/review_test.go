package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrF(f float64) *float64 { return &f }

// buildOneAgent drives the production buildSlots resolution seam for a single
// primary agent and returns its rendered Agent plus resolved payload mode — the
// (Agent, mode, error) shape these tests need. It restricts the roster to `name`
// so buildSlots emits exactly one primary slot, exercising the real add-closure
// resolution (EffectivePayloadMode, payloads[mode] lookup, scope injection,
// renderAgent) — the single production resolution site — rather than a duplicate.
func buildOneAgent(cfg *ReviewConfig, name string, payloads map[string]modePayload, rng ReviewRange, forceMode, scopeConstraint string) (Agent, string, error) {
	scoped := *cfg
	proj := *cfg.Project
	proj.Agents = []string{name}
	proj.SerialAgents = nil
	scoped.Project = &proj
	slots, modes, err := buildSlots(&scoped, payloads, rng, forceMode, scopeConstraint, false)
	if err != nil {
		return Agent{}, "", err
	}
	// Guard the single-slot assumption: restricting the roster to one non-chunked
	// agent must yield exactly one primary slot. A chunked-strategy cfg would emit
	// one slot per chunk (returning the first chunk's partial-payload agent —
	// silently wrong), and an empty roster would panic on slots[0]. Fail loudly
	// instead so callers see an error rather than a misleading agent or a crash.
	if len(slots) != 1 {
		return Agent{}, "", fmt.Errorf("buildOneAgent: expected exactly 1 slot for %q, got %d (chunked or empty roster is unsupported)", name, len(slots))
	}
	return slots[0].Primary, modes[name], nil
}

// initRepo creates a temp git repo with a base and head commit that change a Go
// file, returning the dir and the two SHAs.
func initRepo(t *testing.T) (dir, base, head string) {
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package main\n\nfunc a() {}\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base = run("rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package main\n\nfunc a() { b() }\n\nfunc b() {}\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "head")
	head = run("rev-parse", "HEAD")
	return dir, base, head
}

// mockProvider returns an httptest server speaking the OpenAI chat-completions
// shape. It returns 500 for any model in failModels, else a findings payload.
func mockProvider(t *testing.T, failModels ...string) *httptest.Server {
	t.Helper()
	// The circuit breaker keys on the registry provider name ("p"), which every
	// review test shares, and lives in a process-global registry (correct for
	// serve mode, where a provider outage in one review should fail-fast the next).
	// In the test binary that global is shared across tests, so a failing-provider
	// test would trip "p" for an unrelated later test. Isolate per test: start
	// clean and reset on cleanup.
	circuitbreaker.DefaultRegistry.Reset()
	t.Cleanup(circuitbreaker.DefaultRegistry.Reset)
	fail := map[string]bool{}
	for _, m := range failModels {
		fail[m] = true
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		if fail[req.Model] {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		content := "CRITICAL|auth.go:3|Unchecked call|Guard it|security|15|b() unchecked"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func twoAgentConfig(srvURL string) *ReviewConfig {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: srvURL}},
		Agents: map[string]registry.AgentConfig{
			"greta": {Provider: "p", Model: "m-greta", Persona: "greta", Temperature: ptrF(0.7)},
			"kai":   {Provider: "p", Model: "m-kai", Persona: "kai", Temperature: ptrF(0.7)},
		},
	}
	return &ReviewConfig{
		Registry:    reg,
		Project:     &registry.ProjectConfig{Agents: []string{"greta", "kai"}},
		Settings:    registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600, MaxSprintPlanBytes: registry.DefaultMaxSprintPlanBytes},
		PersonaDirs: registry.PersonaDirs{}, // empty → embedded personas
	}
}

func reviewReq(repo, root, base, head string) ReviewRequest {
	return ReviewRequest{
		Repo:       repo,
		Root:       root,
		Range:      ReviewRange{Base: base, Head: head, DetectionMode: "explicit", CommitCount: 1},
		Branch:     "feature/test",
		Date:       "2026-06-10",
		TimeSuffix: "120000",
		StartedAt:  time.Unix(1000, 0).UTC(),
	}
}

func TestRunReview_EndToEnd(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "2026-06-10_test", res.ID)
	assert.Equal(t, 2, res.Summary.Succeeded)
	assert.False(t, res.Summary.Partial)

	// Review directory layout.
	for _, sub := range []string{"payload", "sources", "reconciled"} {
		assert.DirExists(t, filepath.Join(res.Dir, sub))
	}
	assert.FileExists(t, filepath.Join(res.Dir, "payload", "blocks.txt"))

	// Per-agent findings with engine-set REVIEWER.
	for _, agent := range []string{"greta", "kai"} {
		fp := filepath.Join(res.Dir, "sources", "pool", "raw", "agent", agent, "findings.txt")
		data, err := os.ReadFile(fp)
		require.NoError(t, err, "missing %s findings", agent)
		parsed, err := stream.ParseSource(data)
		require.NoError(t, err)
		require.Len(t, parsed.Findings, 1)
		assert.Equal(t, agent, parsed.Findings[0].Reviewer)
		assert.Equal(t, "CRITICAL", parsed.Findings[0].Severity)
	}

	// Manifest provenance + latest pointer.
	mdata, err := os.ReadFile(filepath.Join(res.Dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.Equal(t, base, m.Base)
	assert.Equal(t, head, m.Head)
	assert.Equal(t, "explicit", m.DetectionMode)
	assert.ElementsMatch(t, []string{"greta", "kai"}, m.Roster)
	assert.Equal(t, "blocks", m.PerAgentPayload["greta"])
	assert.Equal(t, []string{"review"}, m.Stages, "1.x manifest records the review stage (Epic 1.1 reserved field)")

	latest, err := ReadLatest(repo)
	require.NoError(t, err)
	assert.Equal(t, res.ID, latest)
}

// TestRunReview_ForceBacksUpExistingDir is the Epic 4.7 AC8 integration test:
// running review twice against the same explicit --id fails the collision
// without --force, and with --force backs the prior directory up to <dir>.bak
// and scaffolds a fresh one.
func TestRunReview_ForceBacksUpExistingDir(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	req := reviewReq(repo, repo, base, head)
	req.IDOverride = "2026-06-10_fixed"

	// First review creates the directory.
	res1, err := RunReview(context.Background(), llmclient.New(), cfg, req)
	require.NoError(t, err)
	reviewDir := res1.Dir

	// Plant a marker so the backup provably carries the prior generation.
	marker := filepath.Join(reviewDir, "MARKER")
	require.NoError(t, os.WriteFile(marker, []byte("gen1"), 0o644))

	// Re-running the same --id WITHOUT --force fails the collision and names both
	// recovery paths (AC1).
	_, err = RunReview(context.Background(), llmclient.New(), cfg, req)
	require.Error(t, err, "re-running the same --id without --force must fail the collision")
	assert.Contains(t, err.Error(), "--resume")
	assert.Contains(t, err.Error(), "--force")
	// The collision must not have moved the existing directory.
	assert.FileExists(t, marker, "a failed collision must leave the existing review untouched")
	assert.NoDirExists(t, reviewDir+".bak", "no backup until --force is used")

	// WITH --force: back up the prior dir to <dir>.bak and scaffold fresh.
	req.Force = true
	res2, err := RunReview(context.Background(), llmclient.New(), cfg, req)
	require.NoError(t, err)
	assert.Equal(t, reviewDir, res2.Dir, "the fresh review reuses the same id/path")

	bakMarker := filepath.Join(reviewDir+".bak", "MARKER")
	data, err := os.ReadFile(bakMarker)
	require.NoError(t, err, "AC8: prior review dir must be backed up to <dir>.bak")
	assert.Equal(t, "gen1", string(data))

	// The fresh dir was scaffolded anew, so it does not carry the prior marker.
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "the fresh review dir must not carry the prior generation's marker")
}

// TestRunReview_OutputDirSkipsLatest verifies --output-dir writes the full
// review tree to the given path and does NOT repoint .atcr/latest (the pointer
// tracks interactive runs only; external orchestrators own their output dir).
func TestRunReview_OutputDirSkipsLatest(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	out := filepath.Join(t.TempDir(), "ext-review")
	req := reviewReq(repo, repo, base, head)
	req.OutputDir = out

	res, err := RunReview(context.Background(), llmclient.New(), cfg, req)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, out, res.Dir, "review dir must be the explicit --output-dir path")

	for _, sub := range []string{"payload", "sources", "reconciled"} {
		assert.DirExists(t, filepath.Join(out, sub))
	}
	assert.FileExists(t, filepath.Join(out, "manifest.json"))

	// .atcr/latest must NOT be written under the repo root.
	_, lerr := ReadLatest(repo)
	assert.Error(t, lerr, "--output-dir must not update .atcr/latest")
}

// ExecuteReview must stamp CompletedAt when it finalizes the manifest so
// downstream tools can derive run duration from manifest.json rather than
// finding the field silently absent (TD review.go:18). Because of omitempty a
// zero value never marshals, so the assertion checks both the parsed value and
// the on-disk key.
func TestRunReview_StampsCompletedAt(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)

	mdata, err := os.ReadFile(filepath.Join(res.Dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))

	require.False(t, m.CompletedAt.IsZero(), "completed_at must be stamped at finalization")
	assert.False(t, m.CompletedAt.Before(m.StartedAt), "completed_at must be >= started_at")
	assert.Contains(t, string(mdata), "completed_at", "completed_at must survive the JSON round-trip")
}

func TestRunReview_PartialWhenOneAgentFails(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t, "m-kai") // kai's model 500s; retries exhaust → failed
	cfg := twoAgentConfig(srv.URL)
	// Shrink kai's retry budget so the test is fast.
	res, err := RunReview(context.Background(), llmclient.New(llmclient.WithRetry(0, 0, 1)), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err, "≥1 success means no run-level error")
	assert.Equal(t, 1, res.Summary.Succeeded)
	assert.Equal(t, 1, res.Summary.Failed)
	assert.True(t, res.Summary.Partial)

	// Manifest records the partial flag.
	mdata, _ := os.ReadFile(filepath.Join(res.Dir, "manifest.json"))
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.True(t, m.Partial)
}

func TestRunReview_AllFailReturnsErrorButPreservesArtifacts(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t, "m-greta", "m-kai") // both fail
	cfg := twoAgentConfig(srv.URL)
	res, err := RunReview(context.Background(), llmclient.New(llmclient.WithRetry(0, 0, 1)), cfg, reviewReq(repo, repo, base, head))

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAllAgentsFailed)
	require.NotNil(t, res, "result preserved so the caller knows the review dir")
	// Artifacts still on disk for inspection (AC 03-02 Error Scenario 3).
	assert.FileExists(t, filepath.Join(res.Dir, "manifest.json"))
	assert.FileExists(t, filepath.Join(res.Dir, "sources", "pool", "summary.json"))
}

// A post-fan-out persistence failure (WritePool error after the roster ran)
// must leave the review reporting failed — not eternal in_progress. ExecuteReview
// writes a best-effort summary.json with the real agent results; when all agents
// failed the reader maps succeeded==0 to RunFailed (Epic 1.5). An invalid
// on-disk agent name makes WritePool's agentDirName reject the result,
// deterministically forcing the failure branch without any I/O fault injection.
func TestExecuteReview_WritePoolFailureMarksFailed(t *testing.T) {
	dir := t.TempDir()
	// Scaffold a manifest as PrepareReview would; a recent StartedAt keeps the
	// review inside its timeout window so the assertion isolates the failure
	// marker from stale inference.
	require.NoError(t, WriteManifest(dir, &payload.Manifest{
		Base: "a", Head: "b", Roster: []string{"greta"},
		StartedAt: time.Now().UTC(), TimeoutSecs: 600,
	}))
	// Make the fake fail for the empty model (the ".." agent has no model set)
	// so the results reflect an all-failed run, matching the all-failed assertion.
	fake := newFake()
	fake.failFor[""] = assertErr("simulated agent failure")
	prep := &PreparedReview{
		ID:    "x",
		Dir:   dir,
		Slots: []Slot{{Primary: Agent{Name: ".."}}}, // invalid dir name → WritePool errors
	}
	_, err := ExecuteReview(context.Background(), fake, prep)
	require.Error(t, err, "WritePool must fail on the invalid agent dir name")

	st, rerr := ReadReviewStatus(dir, "x")
	require.NoError(t, rerr)
	assert.Equal(t, RunFailed, st.Status, "a WritePool failure with all agents failed must surface as failed, not in_progress")
}

func TestRunReview_EmptyRosterShortCircuits(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := &ReviewConfig{
		Registry: &registry.Registry{},
		Project:  &registry.ProjectConfig{}, // no agents
		Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
	}
	res, err := RunReview(context.Background(), newFake(), cfg, reviewReq(repo, repo, base, head))
	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrEmptyRoster)
	// No review dir or latest pointer was created.
	assert.NoDirExists(t, filepath.Join(repo, ".atcr", "reviews"))
}

func TestRunReview_UnknownProviderIsBuildError(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := &ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{}, // greta's provider missing
			Agents:    map[string]registry.AgentConfig{"greta": {Provider: "ghost", Model: "m", Persona: "greta"}},
		},
		Project:  &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
	}
	_, err := RunReview(context.Background(), newFake(), cfg, reviewReq(repo, repo, base, head))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

// AC 06-03: a configured byte budget must surface as Truncated=true with the
// dropped files recorded in each agent's status.json — never silent. This is
// the integration proof that the payload_byte_budget knob actually feeds
// buildPayloads (the budget machinery was previously wired to a hardcoded 0).
func TestRunReview_ConfiguredBudgetRecordsTruncation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	dir := t.TempDir()
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte("package main\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base := run("rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.go"), []byte("package main\n\nfunc s() {}\n"), 0o644))
	big := "package main\n\nfunc b() {\n" + strings.Repeat("\t_ = \"padding padding padding padding\"\n", 400) + "}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte(big), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "head")
	head := run("rev-parse", "HEAD")

	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)
	cfg.Settings.PayloadByteBudget = 2048 // fits small.go's blocks, not big.go's

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(dir, dir, base, head))
	require.NoError(t, err)

	for _, agent := range []string{"greta", "kai"} {
		data, err := os.ReadFile(filepath.Join(res.Dir, "sources", "pool", "raw", "agent", agent, "status.json"))
		require.NoError(t, err)
		var st AgentStatus
		require.NoError(t, json.Unmarshal(data, &st))
		assert.True(t, st.Truncated, "agent %s: configured budget must record truncation", agent)
		assert.Contains(t, st.FilesDropped, "big.go", "agent %s: dropped file must be listed", agent)
	}
}

// TestPrepareReview_BudgetDropsAllFilesErrors verifies that a byte budget below
// every changed file's size causes PrepareReview to return ErrPayloadFullyDropped
// rather than forwarding an empty payload to the reviewer pool (budget.go:20-23).
func TestPrepareReview_BudgetDropsAllFilesErrors(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)
	cfg.Settings.PayloadByteBudget = 1 // below any real file size → all dropped

	_, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPayloadFullyDropped)
}

// A range whose only commit is an empty commit has CommitCount > 0 but zero
// changed files, so every payload mode builds empty. PrepareReview must refuse
// before scaffolding — never fire the provider pool at an empty payload — and
// leave no review dir or latest pointer behind.
func TestPrepareReview_OnlyEmptyCommitsRejectedBeforeScaffold(t *testing.T) {
	repo, _, head := initRepo(t)
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	run("commit", "-q", "--allow-empty", "-m", "empty")
	emptyHead := run("rev-parse", "HEAD")

	cfg := twoAgentConfig("http://unused")
	req := reviewReq(repo, repo, head, emptyHead)
	_, err := PrepareReview(context.Background(), cfg, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoReviewableContent)
	assert.NoDirExists(t, filepath.Join(repo, ".atcr", "reviews"), "no review dir may be scaffolded for an empty payload")
}

// A client retry of atcr_review with the same explicit id (plausible while the
// first run still shows running) must not launch a second fan-out into the SAME
// review directory — the second PrepareReview must refuse, not scaffold.
// TestPrepareReview_RejectsOutputDirAndIDOverride verifies that PrepareReview
// returns an error when both OutputDir and IDOverride are set — the CLI enforces
// this at flag-parse time but PrepareReview must also guard for direct/MCP callers.
func TestPrepareReview_RejectsOutputDirAndIDOverride(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")
	req := reviewReq(repo, repo, base, head)
	req.OutputDir = filepath.Join(t.TempDir(), "out")
	req.IDOverride = "custom-id"
	_, err := PrepareReview(context.Background(), cfg, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestPrepareReview_RejectsExistingOverrideID(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")
	req := reviewReq(repo, repo, base, head)
	req.IDOverride = "custom-review-id"

	_, err := PrepareReview(context.Background(), cfg, req)
	require.NoError(t, err)

	_, err = PrepareReview(context.Background(), cfg, req)
	require.Error(t, err, "second prepare with the same override id must refuse")
	assert.Contains(t, err.Error(), "custom-review-id")
}

// TestPrepareReview_ForceWithDerivedIdWarnsStderr verifies that --force without
// --id or --output-dir emits a stderr notice because the derived id path never
// collides and Force is a silent no-op there.
func TestPrepareReview_ForceWithDerivedIdWarnsStderr(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")
	req := reviewReq(repo, repo, base, head)
	req.Force = true

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	_, err = PrepareReview(context.Background(), cfg, req)
	require.NoError(t, err)

	require.NoError(t, w.Close())
	os.Stderr = oldStderr
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	assert.Contains(t, string(out), "--force has no effect without --id or --output-dir")
}

// TestPrepareReview_ForceBackupEmitsStderrNotice verifies that a --force
// overwrite of an existing explicit-id review emits a stderr breadcrumb naming
// the .bak directory, so an operator who forced by mistake knows where the
// prior tree was preserved.
func TestPrepareReview_ForceBackupEmitsStderrNotice(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")

	req := reviewReq(repo, repo, base, head)
	req.IDOverride = "2026-06-10_backed"

	_, err := PrepareReview(context.Background(), cfg, req)
	require.NoError(t, err)

	req.Force = true

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	_, err = PrepareReview(context.Background(), cfg, req)
	require.NoError(t, err)

	require.NoError(t, w.Close())
	os.Stderr = oldStderr
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	assert.Contains(t, string(out), "backed up prior review to")
	assert.Contains(t, string(out), "2026-06-10_backed.bak")
}

// TestPrepareReview_RejectsSystemOutputDir verifies the engine itself rejects an
// --output-dir under a system directory (/etc, /proc, /sys), not only the CLI
// flag parser. PrepareReview is public API reachable by the MCP handler and
// direct callers, so the system-path reject must fire for every caller, before
// any --force backup or scaffold touches the filesystem.
func TestPrepareReview_RejectsSystemOutputDir(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")

	req := reviewReq(repo, repo, base, head)
	req.OutputDir = "/etc/atcr-td-output"
	req.Force = true

	_, err := PrepareReview(context.Background(), cfg, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system directories")
}

// A payloads map missing the agent's effective mode must be an explicit build
// error (like the adjacent unknown-agent/unknown-provider lookups), never a
// silently empty payload that produces a plausible-looking vacuous review.
func TestBuildOneAgent_MissingPayloadModeErrors(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	_, _, err := buildOneAgent(cfg, "greta", map[string]modePayload{}, ReviewRange{Base: "a", Head: "b"}, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greta")
	assert.Contains(t, err.Error(), "blocks")
}

// resolveHeadSHA must normalize symbolic refs and short SHAs to the full 40-byte
// SHA before the value is stamped as head_sha in the manifest review stage.
func TestResolveHeadSHA(t *testing.T) {
	repo, base, head := initRepo(t)

	full, err := resolveHeadSHA(repo, head)
	require.NoError(t, err)
	assert.Equal(t, head, full, "full SHA must pass through unchanged")

	short := head[:12]
	resolved, err := resolveHeadSHA(repo, short)
	require.NoError(t, err)
	assert.Equal(t, head, resolved, "short SHA must resolve to full SHA")

	// HEAD is the second commit in initRepo.
	resolvedHEAD, err := resolveHeadSHA(repo, "HEAD")
	require.NoError(t, err)
	assert.Equal(t, head, resolvedHEAD, "symbolic HEAD must resolve to full SHA")

	// Base is also a valid commit but not the current HEAD.
	resolvedBase, err := resolveHeadSHA(repo, base)
	require.NoError(t, err)
	assert.Equal(t, base, resolvedBase)
}

// snapshotManifestFields must recognize the live worktree even when the paths
// are represented differently (trailing slash, relative vs. absolute, symlinks).
func TestSnapshotManifestFields_PathNormalization(t *testing.T) {
	dir := t.TempDir()

	mode, _, _ := snapshotManifestFields(dir, dir, "")
	assert.Equal(t, "live", mode, "identical paths should be live")

	mode, _, _ = snapshotManifestFields(dir+"/", dir, "")
	assert.Equal(t, "live", mode, "trailing slash should not force worktree mode")

	absDir, err := filepath.Abs(dir)
	require.NoError(t, err)
	mode, _, _ = snapshotManifestFields(dir, absDir, "")
	assert.Equal(t, "live", mode, "relative vs absolute same dir should be live")

	// A genuinely different directory must still be worktree mode.
	other := t.TempDir()
	mode, _, _ = snapshotManifestFields(other, dir, "")
	assert.Equal(t, "worktree", mode, "different directories should be worktree")
}

func TestLoadReviewConfig_DiscoversConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	regYAML := `providers:
  openai:
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1
agents:
  greta:
    provider: openai
    model: gpt-4
`
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(regYAML), 0o644))

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	projYAML := "agents:\n  - greta\npayload_mode: diff\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "config.yaml"), []byte(projYAML), 0o644))

	cfg, err := LoadReviewConfig(root, registry.CLIOverrides{})
	require.NoError(t, err)
	assert.Contains(t, cfg.Registry.Agents, "greta")
	assert.Equal(t, []string{"greta"}, cfg.Project.Agents)
	assert.Equal(t, "diff", cfg.Settings.PayloadMode, "project tier overrides the blocks default")
}

// TestExecuteReview_ManifestNotMutatedOnWriteFailure verifies that a failed
// WriteManifest at finalization does not leave p.manifest in a mutated state.
// If p.manifest is mutated before WriteManifest succeeds and the caller retries
// with the same PreparedReview, it would observe stale snapshot/completion data
// from the failed attempt (review.go:343).
//
// Mechanism: Dir is chmod'd to 0555 after PrepareReview so atomicWriteFile
// cannot create its temp file directly in Dir (write bit absent), while
// Dir/sources/pool/ retains its own 0755 permissions so WritePool still
// completes. The failing final WriteManifest is therefore the only error path
// exercised.
func TestExecuteReview_ManifestNotMutatedOnWriteFailure(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	p, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)

	// Precondition: CompletedAt is zero from PrepareReview.
	require.True(t, p.manifest.CompletedAt.IsZero(), "precondition: CompletedAt must be zero before ExecuteReview")

	// Make Dir itself unwriteable so atomicWriteFile cannot create a temp file
	// when writing manifest.json. Dir/sources/pool/ keeps its own 0755 so
	// WritePool still succeeds; only the final WriteManifest fails.
	require.NoError(t, os.Chmod(p.Dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(p.Dir, 0o755) }) // restore for t.TempDir cleanup

	_, execErr := ExecuteReview(context.Background(), llmclient.New(), p)
	require.Error(t, execErr, "ExecuteReview must fail when the final WriteManifest cannot write")

	// p.manifest must NOT be mutated — CompletedAt should still be zero.
	assert.True(t, p.manifest.CompletedAt.IsZero(),
		"p.manifest must not be mutated when WriteManifest fails: use a local copy and assign back only on success")
}

// TestExecuteReview_SnapshotFailureRecordsFailedMode verifies that when a
// snapshot is attempted (tool agent + non-empty Head) but SnapshotFor fails,
// the finalized manifest records snapshot_mode="failed" so a reader can
// distinguish a failed-snapshot run from one where no snapshot was attempted
// (internal/payload/manifest.go:74).
func TestExecuteReview_SnapshotFailureRecordsFailedMode(t *testing.T) {
	repo, _, _ := initRepo(t)
	dir := t.TempDir()

	m := &payload.Manifest{
		Base: "a", Head: "b", Roster: []string{"greta"},
		StartedAt: time.Now().UTC(), TimeoutSecs: 600,
	}
	require.NoError(t, WriteManifest(dir, m))

	// A nonexistent SHA forces SnapshotFor to fail while anyToolAgent returns
	// true, so the snapshot path is entered and the failure branch is exercised.
	prep := &PreparedReview{
		ID:       "x",
		Dir:      dir,
		Repo:     repo,
		Head:     "0000000000000000000000000000000000000000",
		Slots:    []Slot{{Primary: Agent{Name: "greta", Tools: true}}},
		manifest: m,
	}
	_, _ = ExecuteReview(context.Background(), newFake(), prep)

	mdata, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var got payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &got))

	require.NotNil(t, got.Review, "tool agent must produce a non-nil review stage")
	assert.Equal(t, "failed", got.Review.SnapshotMode,
		"a failed snapshot must record snapshot_mode=failed so the state is observable in the manifest")
}

// TestExecuteReview_SnapshotFailureWarnsViaLogger verifies that when a snapshot
// fails in ExecuteReview, the warning routes through the context logger (Warn
// level) rather than raw stderr. This pins the single-sink contract: operators
// must see tool-harness degradation in the structured log stream, not as an
// unstructured stderr blurb that bypasses LOG_LEVEL and redaction.
func TestExecuteReview_SnapshotFailureWarnsViaLogger(t *testing.T) {
	repo, _, _ := initRepo(t)
	dir := t.TempDir()

	m := &payload.Manifest{
		Base: "a", Head: "b", Roster: []string{"greta"},
		StartedAt: time.Now().UTC(), TimeoutSecs: 600,
	}
	require.NoError(t, WriteManifest(dir, m))

	var buf bytes.Buffer
	capture := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := log.NewContext(context.Background(), capture)

	prep := &PreparedReview{
		ID:       "x",
		Dir:      dir,
		Repo:     repo,
		Head:     "0000000000000000000000000000000000000000",
		Slots:    []Slot{{Primary: Agent{Name: "greta", Tools: true}}},
		manifest: m,
	}
	_, _ = ExecuteReview(ctx, newFake(), prep)

	got := buf.String()
	assert.Contains(t, got, "snapshot",
		"snapshot failure must emit a structured log line containing 'snapshot' so operators see it in the log stream")
	assert.Contains(t, got, "WARN",
		"snapshot failure must log at Warn level so it rides LOG_LEVEL filtering")
}
