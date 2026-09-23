package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/personas"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadPersonasScores_RealStoreSumsAcrossModelsAndCase migrates the intent of
// the deleted reviewerCorroborationRates unit test (one reviewer, two models,
// mixed case → one collapsed rate) into an end-to-end exercise of
// loadPersonasScores against a real scorecard store, now that the aggregation
// itself lives in scorecard.TrustPriors and is unit-tested there directly
// (internal/scorecard/trust_test.go).
func TestLoadPersonasScores_RealStoreSumsAcrossModelsAndCase(t *testing.T) {
	isolate(t)
	storeRecord(t, reviewerRec("2026-07-01T10:00:00Z-r1", "Sasha", "opus", 4, 3))
	storeRecord(t, reviewerRec("2026-07-02T10:00:00Z-r2", "SASHA", "sonnet", 6, 1))
	storeRecord(t, reviewerRec("2026-07-03T10:00:00Z-r3", "Penny", "opus", 0, 0))

	data, err := loadPersonasScores(io.Discard)
	require.NoError(t, err)
	assert.InDelta(t, 0.4, data.rates["sasha"], 1e-9)
	assert.Contains(t, data.rates, "penny", "minRuns=0 at this call site must keep a single-run reviewer")
	assert.InDelta(t, 0.0, data.rates["penny"], 1e-9)
}

// TestLoadPersonasScores_PreV2StoreRendersNoScores pins the one operator-visible
// regression sprint 36.0 phase 2 introduces, so it is a decision on the record
// rather than a surprise in the field.
//
// `atcr personas list --scores` calls TrustPriors(dir, 0) — it opts out of the
// DefaultTrustMinRuns floor, which used to mean "show every reviewer with any
// history at all". It does NOT opt out of the outcome-eligibility filter, and
// every record written before schema 2 carries no outcome. So on an existing
// install the table renders all-n/a with the "no data" footer until fresh runs
// accumulate, even though the store is full and perfectly readable.
//
// That is intended: a rate computed from runs nobody classified is not a
// measurement, and absent-means-neutral is the same contract the floor already
// had. It is pinned here because the alternative — discovering it from a user —
// is much worse, and because a future change that quietly re-admits unclassified
// records should have to delete this test to do it.
func TestLoadPersonasScores_PreV2StoreRendersNoScores(t *testing.T) {
	isolate(t)
	for i := 0; i < scorecard.DefaultTrustMinRuns*2; i++ {
		rec := reviewerRec(
			fmt.Sprintf("%s-v1%02d", time.Now().UTC().Format(time.RFC3339), i),
			"sasha", "opus", 4, 2)
		rec.SchemaVersion = 1
		rec.Outcome = "" // exactly how a pre-sprint-36.0 record reads back
		storeRecord(t, rec)
	}

	data, err := loadPersonasScores(io.Discard)
	require.NoError(t, err, "a readable pre-v2 store is not an error")
	assert.Empty(t, data.rates,
		"unclassified history yields no rate; absent is neutral, not punitive")

	// The same store DOES still read — this is exclusion, not a broken read.
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	recs, err := scorecard.ReadAll(dir, scorecard.ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.Len(t, recs, scorecard.DefaultTrustMinRuns*2,
		"every record is readable; only trust scoring declines to count them")
}

// TestLoadPersonasScores_EmptyStoreYieldsEmptyMapNoError locks the AC4
// "no data" regression path against the real store (not the injected
// personasScores fake TestPersonasList_ScoresNoDataFooter uses).
func TestLoadPersonasScores_EmptyStoreYieldsEmptyMapNoError(t *testing.T) {
	isolate(t)

	data, err := loadPersonasScores(io.Discard)
	require.NoError(t, err)
	assert.Empty(t, data.rates)
}

// TestLoadPersonasScores_UnreadableDirDoesNotError locks in the one real
// behavior change from delegating to scorecard.TrustPriors: today
// loadPersonasScores propagates a genuine ReadAll error (e.g. permission
// denied); TrustPriors' best-effort contract swallows it instead, so
// --scores degrades to the "no data" footer rather than an error banner.
func TestLoadPersonasScores_UnreadableDirDoesNotError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission bits do not block root reads")
	}
	isolate(t)
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o755))
	require.NoError(t, os.Mkdir(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	data, err := loadPersonasScores(io.Discard)
	require.NoError(t, err)
	assert.Empty(t, data.rates)
}

// executeSplit runs the root command with separate stdout/stderr buffers so a
// test can verify the success→stdout / diagnostics→stderr contract that the
// shared-buffer `execute` helper cannot distinguish.
func executeSplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := NewRootCmd()
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
		rates: map[string]float64{"sasha": 0.72},
		path:  "/tmp/sc",
	}, nil, nil)

	stdout, _, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)
	assert.Contains(t, stdout, "CORROBORATION")
	assert.Regexp(t, `sasha\s.*72\.0%`, stdout)
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

// The only reachable load error — DefaultDir failing — returns a ZERO
// personasScoreData with path == "", so the error footer interpolated an empty
// path: "Scorecard data at  is unreadable" — a double space and no location, on
// the one path where naming the location is the whole point. The error branch
// must name the underlying error instead.
func TestPersonasList_ScoresLoadErrorNamesTheUnderlyingError(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withPersonasScores(t, personasScoreData{}, errors.New("scorecard dir cannot be resolved"), nil)

	stdout, _, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)
	assert.Contains(t, stdout, "Scorecard data location could not be resolved: scorecard dir cannot be resolved")
	assert.NotContains(t, stdout, "unreadable",
		"the empty-path footer must not appear on the path where data.path is blank")
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
	assert.Contains(t, stdout, "Scorecard data location could not be resolved: permission denied",
		"the error footer names the underlying error, which is the only reachable shape when data.path is blank")
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
	// Pin the exact keyword-path wording (AC 03-02 Edge Case 1) so a collapse into
	// the generic flag-only "No personas found" branch is detectable.
	assert.Contains(t, out, `No personas found matching "quantum"`)
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

// --- AC 03-04: renderPersonaSearch provider/model columns -------------------

// TestRenderPersonaSearch_IncludesProviderModelColumns covers AC 03-04 Scenario 1:
// the rendered table header is exactly NAME VERSION PROVIDER MODEL DESCRIPTION (in
// that pinned order) and a populated row carries the provider/model values.
func TestRenderPersonaSearch_IncludesProviderModelColumns(t *testing.T) {
	var buf bytes.Buffer
	entries := []personas.PersonaIndexEntry{
		{Name: "deepseek-reviewer", Version: "1.0.0", Provider: "deepseek", Model: "deepseek-coder", Description: "Coder-tuned"},
	}
	require.NoError(t, renderPersonaSearch(&buf, entries))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 2)
	assert.Equal(t, []string{"NAME", "VERSION", "PROVIDER", "MODEL", "DESCRIPTION"}, strings.Fields(lines[0]),
		"header column order is pinned exactly")
	assert.Contains(t, lines[1], "deepseek")
	assert.Contains(t, lines[1], "deepseek-coder")
}

// TestRenderPersonaSearch_EmptyProviderModelPlaceholder covers AC 03-04 Edge Case 1:
// empty Provider/Model render as the "-" placeholder, matching the existing empty-
// Version convention.
func TestRenderPersonaSearch_EmptyProviderModelPlaceholder(t *testing.T) {
	var buf bytes.Buffer
	entries := []personas.PersonaIndexEntry{
		{Name: "general", Version: "1.0.0", Provider: "", Model: "", Description: "General reviewer"},
	}
	require.NoError(t, renderPersonaSearch(&buf, entries))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	fields := strings.Fields(lines[1])
	require.GreaterOrEqual(t, len(fields), 5)
	assert.Equal(t, "general", fields[0])
	assert.Equal(t, "1.0.0", fields[1])
	assert.Equal(t, "-", fields[2], "empty PROVIDER renders as placeholder")
	assert.Equal(t, "-", fields[3], "empty MODEL renders as placeholder")
}

// TestRenderPersonaSearch_StripsControlChars: community-index string fields are
// untrusted; a crafted field must not smuggle a raw ANSI escape into the user's
// terminal, nor an embedded newline/tab that would split one row into extra rows
// or columns. Every dynamic cell flows through the shared writeTable path, so the
// sanitization must strip C0 control characters (tab, newline, CR, ESC) uniformly.
func TestRenderPersonaSearch_StripsControlChars(t *testing.T) {
	var buf bytes.Buffer
	entries := []personas.PersonaIndexEntry{
		{Name: "evil/persona", Version: "1.0.0", Provider: "anthropic", Model: "claude",
			Description: "red\x1b[31m\nFAKE\tROW"},
	}
	require.NoError(t, renderPersonaSearch(&buf, entries))

	out := buf.String()
	assert.NotContains(t, out, "\x1b", "raw ANSI ESC must be stripped from an index field")
	// writeTable emits exactly one header line + one data line, each newline-
	// terminated. An embedded newline in a field would add a third → row injection.
	assert.Equal(t, 2, strings.Count(out, "\n"), "header + exactly one data row (no injected rows)")
}

// TestPersonasSearch_OutputHasProviderModelColumns covers AC 03-04 Scenario 2:
// end-to-end, `search --model deepseek` output includes the Provider/Model columns
// populated for the matching persona.
func TestPersonasSearch_OutputHasProviderModelColumns(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdStructuredIndexJSON})
	withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "search", "--model", "deepseek")
	require.NoError(t, err)
	assert.Contains(t, out, "PROVIDER")
	assert.Contains(t, out, "MODEL")
	assert.Contains(t, out, "openrouter")
	assert.Contains(t, out, "deepseek-chat")
}

// TestPersonasSearch_HelpCitesModelProviderFlags guards the CONFIG SURFACE that
// Phase 7 onboarding docs will cite: `search --help` must name --model/--provider
// and show the discover-by-model example, so a later edit cannot silently drop the
// flag names or example the docs reference.
func TestPersonasSearch_HelpCitesModelProviderFlags(t *testing.T) {
	out, err := execute(t, "personas", "search", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "--model")
	assert.Contains(t, out, "--provider")
	assert.Contains(t, out, "search --model deepseek")
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

// TestPersonasUpgrade_BindingSlugReport covers AC 04-01: a persona with a
// family/channel binding reports the before→after resolved slug on stdout.
func TestPersonasUpgrade_BindingSlugReport(t *testing.T) {
	cat := `{"data":[` +
		`{"id":"deepseek/deepseek-v4.0","canonical_slug":"deepseek/deepseek-v4.0","created":1700000000,"expiration_date":null},` +
		`{"id":"deepseek/deepseek-v4.1","canonical_slug":"deepseek/deepseek-v4.1","created":1780000000,"expiration_date":null}]}`
	srv := personasTestServer(t, map[string]string{"/models": cat})
	dir := withPersonasEnv(t, srv)
	t.Setenv("ATCR_CATALOG_URL", srv.URL)
	installed := `provider: openrouter
model: deepseek/deepseek-v4.0
role: reviewer
binding: deepseek@stable
version: "1.0.0"
`
	p := filepath.Join(dir, "vendor", "delia.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(installed), 0o644))

	out, err := execute(t, "personas", "upgrade", "vendor/delia")
	require.NoError(t, err)
	assert.Contains(t, out, "deepseek/deepseek-v4.0 → deepseek/deepseek-v4.1")
	got, _ := os.ReadFile(p)
	assert.Contains(t, string(got), "deepseek/deepseek-v4.1")
}

// TestPersonasUpgrade_AllDryRunReportsNoWrite covers AC 04-03: --all --dry-run
// reports one before→after (or unchanged) line per bound persona and writes
// nothing to disk.
func TestPersonasUpgrade_AllDryRunReportsNoWrite(t *testing.T) {
	cat := `{"data":[` +
		`{"id":"deepseek/deepseek-v4.0","canonical_slug":"deepseek/deepseek-v4.0","created":1700000000,"expiration_date":null},` +
		`{"id":"deepseek/deepseek-v4.1","canonical_slug":"deepseek/deepseek-v4.1","created":1780000000,"expiration_date":null}]}`
	srv := personasTestServer(t, map[string]string{"/models": cat})
	dir := withPersonasEnv(t, srv)
	t.Setenv("ATCR_CATALOG_URL", srv.URL)
	changing := `provider: openrouter
model: deepseek/deepseek-v4.0
role: reviewer
binding: deepseek@stable
version: "1.0.0"
`
	unchanged := `provider: openrouter
model: deepseek/deepseek-v4.1
role: reviewer
binding: deepseek@stable
version: "1.0.0"
`
	pa := filepath.Join(dir, "vendor", "aa.yaml")
	pb := filepath.Join(dir, "vendor", "bb.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(pa), 0o755))
	require.NoError(t, os.WriteFile(pa, []byte(changing), 0o644))
	require.NoError(t, os.WriteFile(pb, []byte(unchanged), 0o644))

	out, err := execute(t, "personas", "upgrade", "--all", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "deepseek/deepseek-v4.0 → deepseek/deepseek-v4.1")
	assert.Contains(t, out, "(unchanged)")

	gotA, _ := os.ReadFile(pa)
	assert.Equal(t, changing, string(gotA), "dry-run must not write the changing persona")
	gotB, _ := os.ReadFile(pb)
	assert.Equal(t, unchanged, string(gotB), "dry-run must not write the unchanged persona")
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

	out, err := execute(t, "personas", "test", "sasha")
	require.NoError(t, err)
	assert.Contains(t, out, "PASS")
}

func TestPersonasTest_FailExitsNonZero(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 2, Total: 3}})

	stdout, _, err := executeSplit(t, "personas", "test", "sasha")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err)) // exit 1
	assert.Contains(t, stdout, "FAIL")          // report on stdout
}

func TestPersonasTest_NoFixture(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: false}})

	out, err := execute(t, "personas", "test", "sasha")
	require.NoError(t, err)
	assert.Contains(t, out, "No fixture")
}

// TestPersonasTest_DefaultRunnerBuiltinFixture exercises the production default
// runner (TemplateFixtureRunner) — no stub injected — confirming that a built-in
// persona with an embedded fixture reports PASS without a live LLM call.
func TestPersonasTest_DefaultRunnerBuiltinFixture(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	out, err := execute(t, "personas", "test", "sasha")
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

// TestPersonasUpgrade_FetchFailure_ExitsUsage covers the exit-code contract:
// a pure catalog fetch failure during a bound-persona upgrade is an infra/
// environment failure and must exit 2 (usage/command failure), not 1 (substantive
// result), so CI consumers can distinguish infra problems from real upgrades.
func TestPersonasUpgrade_FetchFailure_ExitsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			http.Error(w, "upstream boom", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	dir := withPersonasEnv(t, srv)
	t.Setenv("ATCR_CATALOG_URL", srv.URL)

	installed := `provider: openrouter
model: deepseek/deepseek-v4.0
role: reviewer
binding: deepseek@stable
version: "1.0.0"
`
	p := filepath.Join(dir, "vendor", "delia.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(installed), 0o644))

	_, err := execute(t, "personas", "upgrade", "vendor/delia")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err), "fetch/environment failure must exit 2, not 1")
}

func TestPersonasTest_ZeroCasesWarn(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 0, Total: 0}})

	stdout, stderr, err := executeSplit(t, "personas", "test", "sasha")
	require.NoError(t, err)
	assert.Contains(t, stderr, "WARN")
	assert.NotContains(t, stdout, "PASS")
}

// --- AC 06-04: the explainability summary column ----------------------------

func TestPersonasList_ScoresRendersTheExplainabilitySummary(t *testing.T) {
	// AC 06-04. The maintainer's question is "can I drop or repoint this lens?",
	// so the column has to say how many cases the rate rests on and how many were
	// set aside — a rate alone cannot distinguish a lens measured over forty
	// cases from one measured over two.
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withPersonasScores(t, personasScoreData{
		rates: map[string]float64{"sasha": 0.72},
		details: map[string]scorecard.PersonaScoreDetail{
			"sasha": {Counted: 20, Excluded: 5, Raised: 25, Reasons: map[string]int{
				scorecard.ReasonOutcomeIneligible: 5,
			}},
		},
		path: "/tmp/sc",
	}, nil, nil)

	stdout, _, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)
	assert.Contains(t, stdout, "CASES")
	assert.Regexp(t, `sasha\s.*72\.0%\s.*20 counted`, stdout)
	assert.Contains(t, stdout, "5 excluded (outcome-ineligible)")
}

// The columns read all history with no floor; reconcile reads a window with a
// floor. Two populations under two floors, so the surface must say which one
// each figure describes and which lenses reconcile actually acts on.
func TestPersonasList_ScoresFooterNamesTheScopeAndTheLensesReconcileUses(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withPersonasScores(t, personasScoreData{
		rates: map[string]float64{"sasha": 0.72, "penny": 0.5},
		inUse: map[string]float64{"sasha": 0.7},
		path:  "/tmp/sc",
	}, nil, nil)

	stdout, _, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)
	assert.Contains(t, stdout, "all run history with no run floor")
	assert.Contains(t, stdout, fmt.Sprintf("last %d days with a %d-run floor",
		int(scorecard.DefaultTrustWindow.Hours()/24), scorecard.DefaultTrustMinRuns))
	assert.Contains(t, stdout, "In use by reconcile: sasha\n")
}

func TestPersonasList_ScoresFooterSaysNoneWhenNoLensClearsTheProductionFloor(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withPersonasScores(t, personasScoreData{
		rates: map[string]float64{"sasha": 0.72},
		path:  "/tmp/sc",
	}, nil, nil)

	stdout, _, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)
	assert.Contains(t, stdout, "In use by reconcile: none\n")
}

func TestPersonasList_ScoresSummaryIsNotAPerCaseDump(t *testing.T) {
	// The bound task 5.3 exists to hold. Three reason labels on one persona must
	// still render as ONE line naming the dominant reason, never one line per
	// reason or per case — a thirteen-lens panel would be unreadable.
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withPersonasScores(t, personasScoreData{
		rates: map[string]float64{"sasha": 0.5},
		details: map[string]scorecard.PersonaScoreDetail{
			"sasha": {Counted: 10, Excluded: 9, Reasons: map[string]int{
				scorecard.ReasonOutcomeIneligible:    2,
				scorecard.ReasonNotInOpportunitySet:  7,
				scorecard.ReasonNoRecognizedCategory: 3,
			}},
		},
		path: "/tmp/sc",
	}, nil, nil)

	stdout, _, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)

	sashaLines := 0
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "sasha") {
			sashaLines++
		}
	}
	assert.Equal(t, 1, sashaLines, "one persona renders on exactly one row")
	assert.Contains(t, stdout, "9 excluded (category-not-in-opportunity-set)",
		"the dominant exclusion reason is named; the others are summarised away")
}

func TestPersonasList_ScoresRendersNoDataRatherThanAFabricatedZero(t *testing.T) {
	// AC 06-05 Edge Cases 1/2 at the render layer. A persona below
	// DefaultTrustMinRuns is absent from BOTH scorecard maps, and the table must
	// say so rather than printing "0 counted", which reads as "measured, found
	// nothing" — the opposite of the truth.
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withPersonasScores(t, personasScoreData{
		rates:   map[string]float64{},
		details: map[string]scorecard.PersonaScoreDetail{},
		path:    "/tmp/sc",
	}, nil, nil)

	stdout, _, err := executeSplit(t, "personas", "list", "--scores")
	require.NoError(t, err)
	assert.Contains(t, stdout, "CASES")
	assert.NotContains(t, stdout, "0 counted",
		"an unmeasured lens must render n/a, never a fabricated zero-case summary")
}

func TestFormatScoreDetail_SummaryShapes(t *testing.T) {
	// The renderer's own contract, table-driven so each shape is named.
	tests := []struct {
		name   string
		detail *personas.ScoreDetail
		want   string
	}{
		{"no data at all", nil, "n/a"},
		{"zero exclusions renders an explicit 0, not an omitted clause (AC 06-04)", &personas.ScoreDetail{Counted: 20}, "20 counted · 0 excluded"},
		{
			"one exclusion reason",
			&personas.ScoreDetail{Counted: 20, Excluded: 5, Reasons: map[string]int{
				scorecard.ReasonOutcomeIneligible: 5,
			}},
			"20 counted · 5 excluded (outcome-ineligible)",
		},
		{
			"TD-032's annotation is not an exclusion",
			&personas.ScoreDetail{Counted: 24, Reasons: map[string]int{
				scorecard.ReasonNoRecognizedCategory: 4,
			}},
			"24 counted (4 unlabelled) · 0 excluded",
		},
		{
			"annotation and exclusion together",
			&personas.ScoreDetail{Counted: 24, Excluded: 3, Reasons: map[string]int{
				scorecard.ReasonNoRecognizedCategory: 4,
				scorecard.ReasonNotInOpportunitySet:  3,
			}},
			"24 counted (4 unlabelled) · 3 excluded (category-not-in-opportunity-set)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatScoreDetail(tt.detail))
		})
	}
}

func TestFormatScoreDetail_TiedReasonsAreDeterministic(t *testing.T) {
	// Two reasons at the same count must not render differently between runs —
	// Go's map iteration order is randomised, so the tie-break has to be the
	// ScoreReasons() vocabulary order rather than whichever key came out first.
	d := &personas.ScoreDetail{Counted: 5, Excluded: 4, Reasons: map[string]int{
		scorecard.ReasonOutcomeIneligible:   2,
		scorecard.ReasonNotInOpportunitySet: 2,
	}}
	first := formatScoreDetail(d)
	for i := 0; i < 50; i++ {
		assert.Equal(t, first, formatScoreDetail(d))
	}
	assert.Contains(t, first, "(outcome-ineligible)",
		"a tie resolves to the earlier member of ScoreReasons()")
}

func TestToPersonaDetails_ConvertsWithoutAliasingOrLosingLabels(t *testing.T) {
	// cli/ is the one layer that imports BOTH internal/personas and
	// internal/scorecard, so this is where the duplicated DTO is proven
	// equivalent. internal/personas must not import internal/scorecard (its
	// allowlist in internal/boundaries_test.go), which is why the conversion
	// exists at all.
	in := map[string]scorecard.PersonaScoreDetail{
		"dax": {Counted: 20, Excluded: 5, Reasons: map[string]int{
			scorecard.ReasonOutcomeIneligible:    5,
			scorecard.ReasonNoRecognizedCategory: 2,
		}},
	}
	out := toPersonaDetails(in)

	require.Contains(t, out, "dax")
	assert.Equal(t, 20, out["dax"].Counted)
	assert.Equal(t, 5, out["dax"].Excluded)
	assert.Equal(t, 5, out["dax"].Reasons[scorecard.ReasonOutcomeIneligible])
	assert.Equal(t, 2, out["dax"].Reasons[scorecard.ReasonNoRecognizedCategory])

	// No aliasing across the boundary.
	out["dax"].Reasons[scorecard.ReasonOutcomeIneligible] = 999
	assert.Equal(t, 5, in["dax"].Reasons[scorecard.ReasonOutcomeIneligible])

	assert.Nil(t, toPersonaDetails(nil), "a nil map converts to nil, not an empty map")
}

func TestPersonasScoreDetailLabels_MatchScorecardsVocabulary(t *testing.T) {
	// internal/personas/list_test.go spells "outcome-ineligible" as a literal
	// because it cannot import internal/scorecard. This is the pin that keeps
	// that literal honest — it fails here, in a package that legally sees both,
	// rather than letting the two drift silently.
	assert.Equal(t, "outcome-ineligible", scorecard.ReasonOutcomeIneligible)
	assert.Contains(t, scorecard.ScoreReasons(), scorecard.ReasonOutcomeIneligible)
}

// A lens that raised nothing and a lens whose every finding went uncorroborated
// both have rate 0 (ratio returns 0 for a zero denominator). They are opposite
// facts, so the row must carry the denominator and must not print 0.0% for the
// lens that was never wrong because it never spoke.
func TestRenderScoredList_ZeroRaisedIsNotAZeroRate(t *testing.T) {
	zero := 0.0
	scored := []personas.ScoredPersona{
		{PersonaMeta: personas.PersonaMeta{Name: "quiet", Version: "built-in", Source: "built-in"},
			Rate: &zero, Detail: &personas.ScoreDetail{Counted: 50}},
		{PersonaMeta: personas.PersonaMeta{Name: "missed", Version: "built-in", Source: "built-in"},
			Rate: &zero, Detail: &personas.ScoreDetail{Counted: 50, Raised: 100}},
		{PersonaMeta: personas.PersonaMeta{Name: "unmeasured", Version: "built-in", Source: "built-in"}},
	}
	var out bytes.Buffer
	require.NoError(t, renderScoredList(&out, scored))

	rows := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n")[1:] {
		f := strings.Split(regexp.MustCompile(`\s{2,}`).ReplaceAllString(line, "\t"), "\t")
		rows[f[0]] = f
	}
	assert.Equal(t, []string{"n/a (raised 0)", "0"}, rows["quiet"][4:6])
	assert.Equal(t, []string{"0.0%", "100"}, rows["missed"][4:6])
	assert.Equal(t, []string{"n/a", "n/a"}, rows["unmeasured"][4:6],
		"no data renders n/a in both cells, never a measured zero")
}

func TestFormatScoreDetail_ZeroExclusionsIsDistinctFromNoData(t *testing.T) {
	// AC 06-04's explicit-zero requirement, stated as the contrast it is about:
	// "measured, excluded nothing" and "never measured" must not look alike.
	measured := formatScoreDetail(&personas.ScoreDetail{Counted: 12})
	noData := formatScoreDetail(nil)

	// 12 is under DefaultTrustMinRuns, so the row also carries the provisional
	// marker. That is additive to the contrast this test is about, and asserting
	// the WHOLE rendered string keeps it honest: a measured-but-thin sample must
	// still be visibly distinct from a never-measured one.
	assert.Equal(t, "12 counted · 0 excluded · provisional (under the 20-case trust floor)", measured)
	assert.Equal(t, "n/a", noData)
	assert.NotEqual(t, measured, noData)
	assert.NotContains(t, noData, "0",
		"the no-data marker must never render a count, or it reads as a measured zero")
}

func TestDocs_PersonasInstallMdDocumentsTheCasesColumn(t *testing.T) {
	// The Phase 5 gate caught docs/personas-install.md still publishing the
	// pre-Phase-5 five-column table while docs/scorecard.md described six — two
	// published docs disagreeing with each other, with no test reading either.
	// This is that test. It lives in cli/ because renderScoredList and
	// formatScoreDetail are here, so the doc is pinned against the RENDERER
	// rather than against prose.
	raw, err := os.ReadFile(filepath.Join("..", "docs", "personas-install.md"))
	require.NoError(t, err)
	doc := string(raw)

	// The header the renderer emits must be the header the doc shows.
	var table bytes.Buffer
	require.NoError(t, renderScoredList(&table, nil))
	header := strings.Fields(strings.SplitN(table.String(), "\n", 2)[0])
	require.Equal(t, []string{"NAME", "VERSION", "SOURCE", "LANGUAGE", "CORROBORATION", "RAISED", "CASES"}, header)
	for _, col := range header {
		assert.Contains(t, doc, col, "docs/personas-install.md must name every --scores column")
	}

	// Every reason label a reader can meet in the cell must be documented, and
	// the source of truth for the list is scorecard's closed vocabulary — not a
	// hand-kept copy here — so a fourth member fails this test automatically.
	for _, reason := range scorecard.ScoreReasons() {
		if !scorecard.ReasonExcludes(reason) {
			continue // annotations render as the word "unlabelled", asserted below
		}
		assert.Contains(t, doc, "`"+reason+"`",
			"docs/personas-install.md must name every exclusion reason the CASES cell can print")
	}

	// The exact strings the renderer produces, DERIVED from the renderer rather
	// than re-typed here — a doc pin that hard-codes its own expectation only
	// proves the doc agrees with the test.
	sample := formatScoreDetail(&personas.ScoreDetail{
		Counted:  12,
		Excluded: 3,
		Reasons: map[string]int{
			scorecard.ReasonNoRecognizedCategory: 4,
			scorecard.ReasonOutcomeIneligible:    3,
		},
	})
	require.Equal(t, "12 counted (4 unlabelled) · 3 excluded (outcome-ineligible) · provisional (under the 20-case trust floor)", sample,
		"guard on the guard: if the cell format changes, the substrings below are re-derived, not silently relaxed")
	for _, fragment := range []string{"counted", "unlabelled", "excluded", "provisional"} {
		assert.Contains(t, sample, fragment)
		assert.Contains(t, doc, fragment,
			"every word the CASES cell prints must appear in the doc that explains it")
	}
	// The cell's SHAPE, not just its words: the doc shows a worked example, so a
	// renderer change that reorders or re-punctuates the cell fails here.
	assert.Contains(t, doc, "· ", "the doc's worked example must use the renderer's separator")
	assert.Contains(t, doc, formatScoreDetail(nil), "the no-data marker must be documented")
	assert.Contains(t, doc, "The excluded figure is always shown, including at `0`",
		"AC 06-04's explicit-zero behaviour must be documented, not only tested")
}

// A below-floor lens must be MARKED, not silently ranked on its rate alone.
//
// `--scores` loads with minRuns=0 (loadPersonasScores), so DefaultTrustMinRuns
// never fires on the production path and every lens with any history at all gets
// a rate. sortScoredPersonas then ranks strictly by that rate with no sample-size
// term, which on the live store puts `mira 100.0% (3 counted, 198 excluded)`
// FIRST and `kai 33.3% (20 counted, 188 excluded)` last — the ordering inverts
// the evidence on the one surface whose question is "can I drop or repoint this
// lens?".
//
// The marker rides the CASES column, beside the count it qualifies, so the
// caveat is on the same row as the rate it applies to. The floor is read from
// scorecard.DefaultTrustMinRuns rather than retyped: a marker that kept saying
// "20" after the floor moved would be worse than no marker.
func TestFormatScoreDetail_MarksBelowFloorSamplesProvisional(t *testing.T) {
	below := formatScoreDetail(&personas.ScoreDetail{Counted: 3, Excluded: 198})
	assert.Contains(t, below, "provisional",
		"a lens measured on fewer cases than the trust floor must say so where its count is rendered")
	assert.Contains(t, below, strconv.Itoa(scorecard.DefaultTrustMinRuns),
		"the marker must name the floor it is below, read from the constant rather than retyped")

	at := formatScoreDetail(&personas.ScoreDetail{Counted: scorecard.DefaultTrustMinRuns, Excluded: 1})
	assert.NotContains(t, at, "provisional",
		"a sample AT the floor is not provisional — the floor is inclusive, as TrustPriors applies it")

	assert.NotContains(t, formatScoreDetail(nil), "provisional",
		"n/a is the no-data marker and must stay the only one; an unmeasured lens is not a weakly-measured one")
}
