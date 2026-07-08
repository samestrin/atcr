package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/personas"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewerCorroborationRates_CollapsesModels(t *testing.T) {
	// One reviewer, two models → one collapsed rate of (3+1)/(4+6) = 0.4, not a
	// last-wins single-model rate (3/4 or 1/6).
	rows := []scorecard.LeaderboardRow{
		{Reviewer: "Sentinel", Model: "opus", FindingsCorroborated: 3, FindingsRaised: 4},
		{Reviewer: "sentinel", Model: "sonnet", FindingsCorroborated: 1, FindingsRaised: 6},
		{Reviewer: "tracer", Model: "opus", FindingsCorroborated: 0, FindingsRaised: 0},
	}
	rates := reviewerCorroborationRates(rows)
	assert.InDelta(t, 0.4, rates["sentinel"], 1e-9)
	assert.InDelta(t, 0.0, rates["tracer"], 1e-9) // raised==0 → 0, still present
	assert.Len(t, rates, 2)
}

// executeSplit runs the root command with separate stdout/stderr buffers so a
// test can verify the success→stdout / diagnostics→stderr contract that the
// shared-buffer `execute` helper cannot distinguish.
func executeSplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

const cmdValidPersonaYAML = `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
language:
  - go
version: "1.0.0"
description: "OWASP Top-10 security reviewer"
`

const cmdIndexJSON = `[
  {"name":"security/owasp","version":"1.0.0","description":"OWASP Top-10 security reviewer","path":"security/owasp.yaml"},
  {"name":"performance/tracer","version":"1.1.0","description":"Hot-path allocation finder","path":"performance/tracer.yaml"}
]`

// personasTestServer serves the given path→body map (200) and 404 otherwise.
func personasTestServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := routes[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withPersonasEnv points the CLI at srv and a temp personas dir for the duration
// of the test, restoring the overrides afterward. Returns the temp dir.
func withPersonasEnv(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ATCR_PERSONAS_URL", srv.URL)
	oldDir := personasDir
	personasDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { personasDir = oldDir })
	return dir
}

func TestPersonasClient_HasTimeout(t *testing.T) {
	c, ok := personasClient.(*http.Client)
	require.True(t, ok)
	assert.Greater(t, c.Timeout, time.Duration(0))
}

func TestPersonas_HelpListsSubcommands(t *testing.T) {
	out, err := execute(t, "personas", "--help")
	require.NoError(t, err)
	for _, sub := range []string{"install", "list", "search", "remove", "test", "upgrade"} {
		assert.Contains(t, out, sub)
	}
}

func TestPersonasInstall_Integration(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/security/owasp.yaml": cmdValidPersonaYAML})
	dir := withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "install", "security/owasp")
	require.NoError(t, err)
	assert.Contains(t, out, "security/owasp")
	assert.FileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
}

// TestPersonasInstall_DeliversCustomPrompt covers C2: `personas install` delivers
// the complete self-contained unit — the YAML plus its co-located <name>.md custom
// prompt — so the installed persona is resolvable with its model-tuned prompt.
func TestPersonasInstall_DeliversCustomPrompt(t *testing.T) {
	srv := personasTestServer(t, map[string]string{
		"/security/owasp.yaml": cmdValidPersonaYAML,
		"/security/owasp.md":   "You are a meticulous OWASP Top-10 reviewer.",
	})
	dir := withPersonasEnv(t, srv)

	_, err := execute(t, "personas", "install", "security/owasp")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "security", "owasp.yaml"))

	md, err := os.ReadFile(filepath.Join(dir, "security", "owasp.md"))
	require.NoError(t, err)
	assert.Equal(t, "You are a meticulous OWASP Top-10 reviewer.", string(md), "co-located custom prompt delivered")
}

func TestPersonasInstall_NotFoundExitsNonZero(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)

	_, err := execute(t, "personas", "install", "security/owasp")
	require.Error(t, err)
}

func bundleDjangoRoutes() map[string]string {
	return map[string]string{
		"/framework/django-orm.yaml":  cmdValidPersonaYAML,
		"/language/python-types.yaml": cmdValidPersonaYAML,
		"/security/owasp.yaml":        cmdValidPersonaYAML,
		"/security/secrets.yaml":      cmdValidPersonaYAML,
	}
}

func TestPersonasInstall_BundleClean(t *testing.T) {
	srv := personasTestServer(t, bundleDjangoRoutes())
	dir := withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "install", "bundle/django")
	require.NoError(t, err)
	for _, m := range []string{"framework/django-orm", "language/python-types", "security/owasp", "security/secrets"} {
		assert.Contains(t, out, m)
	}
	assert.FileExists(t, filepath.Join(dir, "framework", "django-orm.yaml"))
	assert.FileExists(t, filepath.Join(dir, "security", "secrets.yaml"))
}

func TestPersonasInstall_BundlePartialSkip(t *testing.T) {
	srv := personasTestServer(t, bundleDjangoRoutes())
	dir := withPersonasEnv(t, srv)
	// Pre-install two members.
	for _, m := range []string{"framework/django-orm", "language/python-types"} {
		p := filepath.Join(dir, filepath.FromSlash(m)+".yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(cmdValidPersonaYAML), 0o644))
	}

	out, err := execute(t, "personas", "install", "bundle/django")
	require.NoError(t, err)
	assert.Contains(t, out, "already present")
	assert.Contains(t, out, "security/owasp")
}

func TestPersonasInstall_BundleUnknownExitsNonZero(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)

	_, _, err := executeSplit(t, "personas", "install", "bundle/nope")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err))
	// SilenceErrors is set on the root, so main prints the message; the test
	// asserts on the returned error, which main renders verbatim to stderr.
	assert.Contains(t, err.Error(), `unknown bundle: "nope"`)
}

func TestPersonasInstall_BundleEmptyNameIsUsageError(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)

	_, _, err := executeSplit(t, "personas", "install", "bundle/")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "bundle name is required")
}

func TestPersonasInstall_BundleMemberFailureExitsNonZero(t *testing.T) {
	routes := bundleDjangoRoutes()
	delete(routes, "/security/owasp.yaml") // one member 404s
	srv := personasTestServer(t, routes)
	dir := withPersonasEnv(t, srv)

	_, stderr, err := executeSplit(t, "personas", "install", "bundle/django")
	require.Error(t, err)
	assert.Contains(t, stderr, "failed to install security/owasp")
	assert.Contains(t, err.Error(), "1 of 4 bundle personas failed to install")
	// The other members still landed despite the mid-bundle failure.
	assert.FileExists(t, filepath.Join(dir, "security", "secrets.yaml"))
}

func TestPersonasList_Integration(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/security/owasp.yaml": cmdValidPersonaYAML})
	dir := withPersonasEnv(t, srv)
	require.NoError(t, personas.Install(http.DefaultClient, srv.URL, "security/owasp", dir))

	out, err := execute(t, "personas", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "bruce")          // built-in
	assert.Contains(t, out, "security/owasp") // community
	assert.Contains(t, out, "built-in")
	assert.Contains(t, out, "community")
}

// withPersonasScores swaps the scorecard loader for a fake so list --scores
// tests never touch the real scorecard store. When called is non-nil it is set
// true if the loader runs (used to assert the baseline path never loads scores).
func withPersonasScores(t *testing.T, data personasScoreData, loadErr error, called *bool) {
	t.Helper()
	old := personasScores
	personasScores = func(io.Writer) (personasScoreData, error) {
		if called != nil {
			*called = true
		}
		return data, loadErr
	}
	t.Cleanup(func() { personasScores = old })
}

func TestPersonasList_ScoresColumn(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withPersonasScores(t, personasScoreData{
		rates: map[string]float64{"sentinel": 0.72},
		path:  "/tmp/sc",
	}, nil, nil)

	stdout, _, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)
	assert.Contains(t, stdout, "CORROBORATION")
	assert.Regexp(t, `sentinel\s.*72\.0%`, stdout)
}

func TestPersonasList_ScoresNoDataFooter(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withPersonasScores(t, personasScoreData{
		rates: map[string]float64{},
		path:  "/home/u/.config/atcr/scorecard",
	}, nil, nil)

	stdout, _, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)
	assert.Contains(t, stdout, "n/a")
	assert.Contains(t, stdout, "No scorecard data found at /home/u/.config/atcr/scorecard")
}

func TestPersonasList_ScoresReadErrorDegradesGracefully(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withPersonasScores(t, personasScoreData{
		rates: map[string]float64{"bruce": 0.5},
		path:  "/home/u/.config/atcr/scorecard",
	}, errors.New("permission denied"), nil)

	stdout, stderr, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)
	assert.Contains(t, stdout, "CORROBORATION")
	assert.Contains(t, stdout, "n/a")
	assert.Contains(t, stderr, "permission denied")
	assert.Contains(t, stdout, "Scorecard data at /home/u/.config/atcr/scorecard is unreadable")
}

func TestPersonasList_BaselineDoesNotLoadScores(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	called := false
	withPersonasScores(t, personasScoreData{}, nil, &called)

	_, err := execute(t, "personas", "list")
	require.NoError(t, err)
	assert.False(t, called, "scorecard must not be loaded without --scores")
}

func TestPersonasListHelpContainsScoresFlag(t *testing.T) {
	out, err := execute(t, "personas", "list", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "--scores")
	assert.Contains(t, out, "corroboration")
	assert.Contains(t, out, "n/a")
}

func TestPersonasSearch_Integration(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdIndexJSON})
	withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "search", "security")
	require.NoError(t, err)
	assert.Contains(t, out, "security/owasp")
	assert.NotContains(t, out, "performance/tracer")
}

func TestPersonasSearch_NoMatch(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdIndexJSON})
	withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "search", "quantum")
	require.NoError(t, err)
	assert.Contains(t, out, "No personas found")
}

// searchGuardMsg is the AC 03-03 canonical usage-error string, pinned and reused
// by every guard path (no keyword and no non-empty --model/--provider).
const searchGuardMsg = "provide a keyword, --model, or --provider"

// cmdStructuredIndexJSON is a mock index carrying structured provider/model so the
// CLI-level --model/--provider filter paths can be exercised end-to-end.
const cmdStructuredIndexJSON = `[
  {"name":"amara","version":"1.0.0","description":"General-purpose reviewer","path":"open/amara.yaml","provider":"openrouter","model":"deepseek-chat"},
  {"name":"gina","version":"1.0.0","description":"API contract reviewer","path":"frontier/gina.yaml","provider":"openai","model":"gpt-4"}
]`

func TestPersonasSearch_EmptyKeywordIsUsageError(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdIndexJSON})
	withPersonasEnv(t, srv)

	// A single empty positional arg with no flags: after trimming, keyword and both
	// flags are empty, so the canonical guard fires (AC 03-03 Error Scenario 1).
	_, _, err := executeSplit(t, "personas", "search", "")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), searchGuardMsg)
}

// TestPersonasSearch_ModelFlagOnly covers AC 03-03 Scenario 1: --model with no
// positional keyword succeeds (Args relaxed to MaximumNArgs(1)).
func TestPersonasSearch_ModelFlagOnly(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdStructuredIndexJSON})
	withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "search", "--model", "deepseek")
	require.NoError(t, err)
	assert.Contains(t, out, "amara")
	assert.NotContains(t, out, "gina")
}

// TestPersonasSearch_ProviderFlagOnly covers AC 03-03 Scenario 2: --provider with
// no positional keyword succeeds.
func TestPersonasSearch_ProviderFlagOnly(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdStructuredIndexJSON})
	withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "search", "--provider", "openai")
	require.NoError(t, err)
	assert.Contains(t, out, "gina")
	assert.NotContains(t, out, "amara")
}

// TestPersonasSearch_KeywordPlusFlag covers AC 03-03 Scenario 3: one positional arg
// plus a flag is accepted (exactly one positional under MaximumNArgs(1)).
func TestPersonasSearch_KeywordPlusFlag(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdStructuredIndexJSON})
	withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "search", "deepseek", "--model", "deepseek-chat")
	require.NoError(t, err)
	assert.Contains(t, out, "amara")
	assert.NotContains(t, out, "gina")
}

// TestPersonasSearch_NoKeywordNoFlagsIsUsageError covers AC 03-03 Edge Case 1 /
// Error Scenario 1: bare `search` with no args and no flags returns the canonical
// usage error, not a silent unfiltered run.
func TestPersonasSearch_NoKeywordNoFlagsIsUsageError(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdStructuredIndexJSON})
	withPersonasEnv(t, srv)

	_, _, err := executeSplit(t, "personas", "search")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), searchGuardMsg)
}

// TestPersonasSearch_TwoPositionalArgsRejected covers AC 03-03 Edge Case 2:
// MaximumNArgs(1) rejects two positional args before RunE.
func TestPersonasSearch_TwoPositionalArgsRejected(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdStructuredIndexJSON})
	withPersonasEnv(t, srv)

	_, _, err := executeSplit(t, "personas", "search", "foo", "bar")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

// TestPersonasSearch_WhitespaceKeywordWithFlagSucceeds covers AC 03-03 Edge Case 3:
// a whitespace-only keyword is trimmed to absent; --model satisfies the guard.
func TestPersonasSearch_WhitespaceKeywordWithFlagSucceeds(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdStructuredIndexJSON})
	withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "search", "   ", "--model", "deepseek")
	require.NoError(t, err)
	assert.Contains(t, out, "amara")
}

// TestPersonasSearch_EmptyFlagValueTreatedAsAbsent covers AC 03-03 Edge Case 4: an
// empty/whitespace flag value is trimmed to absent and MUST NOT trigger an
// unfiltered whole-index match — the canonical guard fires instead.
func TestPersonasSearch_EmptyFlagValueTreatedAsAbsent(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdStructuredIndexJSON})
	withPersonasEnv(t, srv)

	_, _, err := executeSplit(t, "personas", "search", "--model", "   ")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), searchGuardMsg)
}

func TestPersonasRemove_Integration(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/security/owasp.yaml": cmdValidPersonaYAML})
	dir := withPersonasEnv(t, srv)
	require.NoError(t, personas.Install(http.DefaultClient, srv.URL, "security/owasp", dir))

	out, err := execute(t, "personas", "remove", "security/owasp")
	require.NoError(t, err)
	assert.Contains(t, out, "Removed")
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
}

func TestPersonasRemove_BuiltinRejected(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	_, err := execute(t, "personas", "remove", "bruce")
	require.Error(t, err)
}

func TestPersonasUpgrade_Integration(t *testing.T) {
	newer := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	srv := personasTestServer(t, map[string]string{"/security/owasp.yaml": newer})
	dir := withPersonasEnv(t, srv)
	// Pre-install v1.0.0.
	p := filepath.Join(dir, "security", "owasp.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(cmdValidPersonaYAML), 0o644))

	out, err := execute(t, "personas", "upgrade", "security/owasp")
	require.NoError(t, err)
	assert.Contains(t, out, "1.1.0")
	got, _ := os.ReadFile(p)
	assert.Equal(t, newer, string(got))
}

// stubFixtureRunner lets the test drive the `test` subcommand's outcome without
// a live LLM.
type stubFixtureRunner struct{ outcome personas.FixtureOutcome }

func (s stubFixtureRunner) RunFixture(string) (personas.FixtureOutcome, error) {
	return s.outcome, nil
}

func withFixtureRunner(t *testing.T, r personas.FixtureRunner) {
	t.Helper()
	old := personasFixtureRunner
	personasFixtureRunner = r
	t.Cleanup(func() { personasFixtureRunner = old })
}

func TestPersonasTest_Pass(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 3, Total: 3}})

	out, err := execute(t, "personas", "test", "sentinel")
	require.NoError(t, err)
	assert.Contains(t, out, "PASS")
}

func TestPersonasTest_FailExitsNonZero(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 2, Total: 3}})

	stdout, _, err := executeSplit(t, "personas", "test", "sentinel")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err)) // exit 1
	assert.Contains(t, stdout, "FAIL")          // report on stdout
}

func TestPersonasTest_NoFixture(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: false}})

	out, err := execute(t, "personas", "test", "sentinel")
	require.NoError(t, err)
	assert.Contains(t, out, "No fixture")
}

// TestPersonasTest_DefaultRunnerBuiltinFixture exercises the production default
// runner (TemplateFixtureRunner) — no stub injected — confirming that a built-in
// persona with an embedded fixture reports PASS without a live LLM call.
func TestPersonasTest_DefaultRunnerBuiltinFixture(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	out, err := execute(t, "personas", "test", "sentinel")
	require.NoError(t, err)
	assert.Contains(t, out, "PASS")
}

// TestPersonasTest_DefaultRunnerNoFixtureBuiltin confirms that a built-in
// persona without an embedded fixture (e.g. "bruce") reports no fixture and
// exits 0 without a live LLM call.
func TestPersonasTest_DefaultRunnerNoFixtureBuiltin(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	out, err := execute(t, "personas", "test", "bruce")
	require.NoError(t, err)
	assert.Contains(t, out, "No fixture")
}

func TestPersonasUpgrade_ConflictExitsUsage(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	_, err := execute(t, "personas", "upgrade", "--all", "security/owasp")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err)) // exit 2
}

func TestPersonasUpgrade_NoArgsExitsUsage(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	_, err := execute(t, "personas", "upgrade")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err)) // exit 2
}

func TestPersonasUpgrade_AllEmpty(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	out, err := execute(t, "personas", "upgrade", "--all")
	require.NoError(t, err)
	assert.Contains(t, out, "No community personas installed")
}

func TestPersonasTest_ZeroCasesWarn(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 0, Total: 0}})

	stdout, stderr, err := executeSplit(t, "personas", "test", "sentinel")
	require.NoError(t, err)
	assert.Contains(t, stderr, "WARN")
	assert.NotContains(t, stdout, "PASS")
}
