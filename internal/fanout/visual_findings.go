package fanout

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
	reclib "github.com/samestrin/atcr/reconcile"
)

// The EVIDENCE prefixes of the findings the browser run's own capture yields
// (Epic 36.0.3).
const (
	visualConsolePrefix = "ui-console:"
	visualNetworkPrefix = "ui-network:"
)

// visualFindingMsgMax caps the captured text quoted in a finding's PROBLEM
// and EVIDENCE.
const visualFindingMsgMax = 300

// visualBrowserFindings renders res as 7-column finding lines: one MEDIUM
// out-of-scope finding per distinct console error (de-duplicated by message)
// and per distinct failed request (by URL), filed at line 1 of spec, which
// the grounding gate keeps whatever the patch holds. Each EVIDENCE starts
// with its ui-console: or ui-network: prefix and names the evidence files in
// art, so a report can link them.
func visualBrowserFindings(spec string, res BrowserResult, art VisualArtifacts) []string {
	// Each finding lists its own log first, which is the one the reconciler
	// reads an excerpt from.
	refsFor := func(first, second string) string {
		var refs []string
		refs = append(refs, art.Screenshots...)
		for _, p := range []string{first, second} {
			if p != "" {
				refs = append(refs, p)
			}
		}
		if len(refs) == 0 {
			return ""
		}
		return " [" + strings.Join(refs, ", ") + "]"
	}
	consoleFiles, networkFiles := refsFor(art.Console, art.Network), refsFor(art.Network, art.Console)
	row := func(problem, fix, evidence, files string) string {
		return strings.Join([]string{"MEDIUM", spec + ":1", problem, fix, reclib.CategoryOutOfScope, "0", evidence + files}, "|")
	}

	var lines []string
	seen := map[string]bool{}
	for _, e := range res.Console {
		if e.Level != "error" {
			continue
		}
		msg := visualFindingText(e.Text)
		if msg == "" || seen["c:"+msg] {
			continue
		}
		seen["c:"+msg] = true
		ev := visualConsolePrefix + " " + msg
		if loc := visualFindingText(e.Location); loc != "" {
			ev += " (" + loc + ")"
		}
		lines = append(lines, row("The page logged a console error: "+msg,
			"Fix the cause of the console error the page logs.", ev, consoleFiles))
	}
	for _, f := range res.NetworkFailures {
		url := visualFindingText(f.URL)
		if url == "" || seen["n:"+url] {
			continue
		}
		seen["n:"+url] = true
		what := "no response"
		switch {
		case f.Status != 0:
			what = fmt.Sprintf("HTTP %d", f.Status)
		case f.Error != "":
			what = visualFindingText(f.Error)
		}
		ev := fmt.Sprintf("%s %s %s -> %s", visualNetworkPrefix, visualFindingText(f.Method), url, what)
		lines = append(lines, row("A request the page made failed: "+url+" ("+what+")",
			"Fix the failing request or the endpoint it calls.", ev, networkFiles))
	}
	return lines
}

// visualFindingText flattens captured text onto one line without the column
// separator and caps it, so it cannot add a column or a row.
func visualFindingText(s string) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "|", "/")), " ")
	if len(s) > visualFindingMsgMax {
		s = strings.ToValidUTF8(s[:visualFindingMsgMax], "") + "..."
	}
	return s
}

// appendVisualFindings adds lines to the reply content, ahead of the reply so
// a reply cut off inside a code fence cannot swallow them. A bare "no
// findings" reply is dropped, since it would no longer be true.
func appendVisualFindings(content string, lines []string) string {
	if len(lines) == 0 {
		return content
	}
	if stream.IsNoFindings(content) {
		content = ""
	}
	return strings.Join(lines, "\n") + "\n" + content
}

// guardBrowserText passes the captured text that becomes finding text through
// guardSecrets under policy, as persistVisualEvidence does for the saved
// files, and returns a copy of res with it replaced: redact swaps in the
// redacted text, block returns ErrSecretGuardBlocked.
func guardBrowserText(ctx context.Context, policy string, res BrowserResult) (BrowserResult, error) {
	var segs []guardSegment
	for _, e := range res.Console {
		segs = append(segs, guardSegment{text: e.Text, file: visualConsoleLabel}, guardSegment{text: e.Location, file: visualConsoleLabel})
	}
	for _, f := range res.NetworkFailures {
		segs = append(segs, guardSegment{text: f.URL, file: visualNetworkLabel}, guardSegment{text: f.Error, file: visualNetworkLabel})
	}
	guarded, err := guardSecrets(ctx, policy, VisualPersonaName, segs...)
	if err != nil {
		return BrowserResult{}, err
	}
	res.Console = slices.Clone(res.Console)
	for i := range res.Console {
		res.Console[i].Text, res.Console[i].Location, guarded = guarded[0], guarded[1], guarded[2:]
	}
	res.NetworkFailures = slices.Clone(res.NetworkFailures)
	for i := range res.NetworkFailures {
		res.NetworkFailures[i].URL, res.NetworkFailures[i].Error, guarded = guarded[0], guarded[1], guarded[2:]
	}
	return res, nil
}
