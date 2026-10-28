package fanout

import (
	"context"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
)

func TestVisualBrowserFindings(t *testing.T) {
	spec := "e2e/felix-x.spec.ts"
	res := BrowserResult{
		Console: []ConsoleEntry{
			{Level: "error", Text: "Uncaught TypeError | boom"},
			{Level: "error", Text: "Uncaught TypeError | boom"},
			{Level: "warning", Text: "just a warning"},
		},
		NetworkFailures: []NetworkFailure{{URL: "http://x/api", Method: "GET", Status: 500}},
		Screenshots:     []BrowserScreenshot{{Name: "a.png", Data: []byte("x")}},
	}
	art := VisualArtifacts{Screenshots: []string{"visual/a.png"}, Console: "visual/console.txt"}
	lines := visualBrowserFindings(spec, res, art)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), lines)
	}
	got := stream.ParseModelOutput([]byte(strings.Join(lines, "\n")))
	if len(got) != 2 {
		t.Fatalf("round trip: want 2 findings, got %d", len(got))
	}
	prefixes := []string{"ui-console:", "ui-network:"}
	for i, f := range got {
		if f.Severity != "MEDIUM" || f.Category != "out-of-scope" || f.File != spec || f.Line != 1 {
			t.Errorf("finding %d shape: %+v", i, f)
		}
		if !strings.HasPrefix(f.Evidence, prefixes[i]) || !strings.Contains(f.Evidence, "visual/a.png") {
			t.Errorf("finding %d evidence: %q", i, f.Evidence)
		}
	}
	changed := payload.ChangedLines{"src/app.ts": {Ranges: []payload.LineRange{{Start: 1, End: 5}}}}
	if kept, dropped := groundFindings(got, changed); len(kept) != 2 || dropped != 0 {
		t.Fatalf("grounding kept %d dropped %d", len(kept), dropped)
	}
}

func TestAppendVisualFindings(t *testing.T) {
	if got := appendVisualFindings("keep", nil); got != "keep" {
		t.Errorf("no lines changed content: %q", got)
	}
	if got := appendVisualFindings("NO FINDINGS", []string{"L"}); got != "L\n" {
		t.Errorf("sentinel not replaced: %q", got)
	}
	if got := appendVisualFindings("a", []string{"L"}); got != "L\na" {
		t.Errorf("append: %q", got)
	}
}

// The finding text passes secret_guard as the saved evidence does.
func TestGuardBrowserText(t *testing.T) {
	res := BrowserResult{
		Console:         []ConsoleEntry{{Level: "error", Text: "token=" + guardAWSKey, Location: "http://x/a.js:1"}},
		NetworkFailures: []NetworkFailure{{Method: "GET", URL: "http://localhost/?key=" + guardAWSKey, Error: "net::ERR_FAILED"}},
	}
	got, err := guardBrowserText(secretGuardRedact, res)
	require.NoError(t, err)
	for _, l := range visualBrowserFindings("e2e/s.spec.ts", got, VisualArtifacts{}) {
		assert.NotContains(t, l, guardAWSKey)
		assert.Contains(t, l, payload.SecretPlaceholder)
	}
	assert.Contains(t, res.Console[0].Text, guardAWSKey, "the input is not modified")

	_, err = guardBrowserText(secretGuardBlock, res)
	require.ErrorIs(t, err, ErrSecretGuardBlocked)
}

// A network finding cites the network log first, a console finding the
// console log, so the reconciler excerpts the log the finding came from.
func TestVisualBrowserFindings_OwnLogFirst(t *testing.T) {
	lines := visualBrowserFindings("e2e/s.spec.ts", BrowserResult{
		Console:         []ConsoleEntry{{Level: "error", Text: "boom"}},
		NetworkFailures: []NetworkFailure{{Method: "GET", URL: "http://x/api", Status: 500}},
	}, VisualArtifacts{Screenshots: []string{"visual/a.png"}, Console: "visual/console.txt", Network: "visual/network.txt"})
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "[visual/a.png, visual/console.txt, visual/network.txt]")
	assert.Contains(t, lines[1], "[visual/a.png, visual/network.txt, visual/console.txt]")
}

// A reply cut off inside a json fence must not swallow the rows.
func TestAppendVisualFindings_TruncatedFence(t *testing.T) {
	row := "MEDIUM|e2e/s.spec.ts:1|p|f|out-of-scope|0|ui-console: boom"
	got := stream.ParseModelOutput([]byte(appendVisualFindings("```json\n[{\"severity\":\"HIGH\"", []string{row})))
	require.NotEmpty(t, got)
	assert.Equal(t, "ui-console: boom", got[0].Evidence)
}

// runVisualStage appends the browser findings to felix's Content.
func TestRunVisualStage_AppendsBrowserFindings(t *testing.T) {
	v := &browserVisual{res: VisualReviewResult{
		Result:    Result{Agent: "eye", Content: "NO FINDINGS", Status: StatusOK},
		Spec:      "e2e/felix-x.spec.ts",
		Artifacts: VisualArtifacts{Screenshots: []string{"visual/a.png"}},
		Browser: BrowserResult{
			Console:         []ConsoleEntry{{Level: "error", Text: "boom"}},
			NetworkFailures: []NetworkFailure{{Method: "GET", URL: "http://x/api", Status: 500}},
		},
	}}
	r := runVisualStage(context.Background(), &PreparedReview{ID: "r", visual: v})
	assert.Equal(t, VisualPersonaName, r.Agent)
	got := stream.ParseModelOutput([]byte(r.Content))
	require.Len(t, got, 2)
	for _, f := range got {
		assert.Equal(t, "out-of-scope", f.Category)
		assert.Contains(t, f.Evidence, "visual/a.png")
	}
}

type browserVisual struct{ res VisualReviewResult }

func (b *browserVisual) RunVisual(context.Context, VisualStageInput) (VisualReviewResult, error) {
	return b.res, nil
}

type noisyBrowser struct{ *orderBrowser }

func (b noisyBrowser) Run(ctx context.Context, run BrowserRun) (BrowserResult, error) {
	res, err := b.orderBrowser.Run(ctx, run)
	res.Console = []ConsoleEntry{{Level: "error", Text: "token=" + guardAWSKey}}
	res.NetworkFailures = []NetworkFailure{{Method: "GET", URL: "http://localhost/?key=" + guardAWSKey, Status: 500}}
	return res, err
}

// RunVisual hands back the guarded browser result and the spec it ran.
func TestVisualStage_ReturnsGuardedBrowserAndSpec(t *testing.T) {
	stage, _, _, browser, _ := newOrderStage(t)
	stage.Browser = noisyBrowser{browser}
	stage.Settings = registry.Settings{SecretGuard: secretGuardRedact}

	res, err := stage.RunVisual(context.Background(), VisualStageInput{RepoDir: t.TempDir(), RunID: "r1"})
	require.NoError(t, err)
	assert.Equal(t, browser.run.Spec, res.Spec)
	assert.NotEmpty(t, res.Spec)
	require.Len(t, res.Browser.Console, 1)
	require.Len(t, res.Browser.NetworkFailures, 1)
	assert.NotContains(t, res.Browser.Console[0].Text, guardAWSKey)
	assert.Contains(t, res.Browser.Console[0].Text, payload.SecretPlaceholder)
	assert.NotContains(t, res.Browser.NetworkFailures[0].URL, guardAWSKey)
	assert.Contains(t, res.Browser.NetworkFailures[0].URL, payload.SecretPlaceholder)
}
