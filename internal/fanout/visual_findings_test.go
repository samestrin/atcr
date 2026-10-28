package fanout

import (
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
	if got := appendVisualFindings("a", []string{"L"}); got != "a\nL\n" {
		t.Errorf("append: %q", got)
	}
}
