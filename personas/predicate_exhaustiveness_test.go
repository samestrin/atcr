package personas

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
)

// TestEveryBuiltinPersona_CarriesThePredicateExhaustivenessRule pins the class,
// not the current roster.
//
// The defect this rule exists for is a multi-branch predicate fixed on the branch
// the reviewer was pointed at: the FlushResult-to-FlushResult arm was widened to
// all four fields while the dict arm still gated on two, so equality stayed
// non-transitive on the very field the change called authoritative. Partial
// coverage disguises that — the object is more broken after the partial fix than
// before it. Nothing else in the panel directs a reviewer to enumerate a
// predicate's full branch and field set, and the natural reading behaviour (look
// at what changed) is exactly the behaviour that misses it.
//
// Enumeration walks the embedded built-in filesystem rather than Names() or a
// literal list, because AC2 requires enumerating from the embedded filesystem —
// and the walk pays for itself twice over: it names the offending path in each
// failure, and the checked == 0 fatal below gives the guard a floor no vacuous
// pass can clear. It is NOT preferred for independence from personas.go's
// init(), which panics unless the embedded .md set equals names plus _base.md
// and runs before any test in this package: the only tree that would
// distinguish the two enumerations panics before either test runs, so that
// advantage is unreachable. Community prompts (communityFiles) are deliberately
// NOT walked — they are out of scope for the epic that added this rule.
func TestEveryBuiltinPersona_CarriesThePredicateExhaustivenessRule(t *testing.T) {
	var checked int

	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		// checked++ BEFORE the read, and an unreadable file aborts the walk
		// rather than being skipped: counting only files that read cleanly
		// lets the checked == 0 floor below stay satisfied by the other nine
		// while one built-in escapes both anchor assertions entirely.
		checked++
		body, readErr := fs.ReadFile(files, path)
		if readErr != nil {
			return readErr
		}

		if !strings.Contains(string(body), predicateRuleAnchor) {
			t.Errorf("built-in persona %s does not carry the predicate-exhaustiveness rule — "+
				"it must contain the anchor phrase %q verbatim, in prose adapted to this "+
				"persona's voice, as a numbered bullet under ## Focus", path, predicateRuleAnchor)
		}
		if !strings.Contains(string(body), predicateFilingAnchor) {
			t.Errorf("built-in persona %s states the predicate-exhaustiveness rule but not how to "+
				"file what it finds — it must also contain %q verbatim, or a correct finding about "+
				"an unchanged sibling branch is discarded by the grounding gate", path, predicateFilingAnchor)
		}
		return nil
	})
	require.NoError(t, err, "walking built-in personas")

	if checked == 0 {
		t.Fatal("no embedded built-in persona files were checked — the walk found nothing to pin")
	}
}

// TestEveryBuiltinPersona_PredicateRuleStaysInItsOwnVoice pins the heterogeneity
// constraint the authoring guide states (docs/personas-authoring.md): word the
// prose around the two anchors in your persona's own voice, not another
// persona's sentence. The two Contains checks above cannot see a pasted bullet
// — an identical rule line satisfies them in every file — so distinctness needs
// its own guard. Strip both anchors from each file's rule line and fail if any
// two remainders are byte-identical: each pasted copy makes the next look like
// house style, and correlated findings inflate the reconciler's CONFIDENCE =
// HIGH (2+ distinct reviewers) without adding independent evidence.
func TestEveryBuiltinPersona_PredicateRuleStaysInItsOwnVoice(t *testing.T) {
	remainders := map[string]string{} // file -> rule line with both anchors stripped

	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, readErr := fs.ReadFile(files, path)
		if readErr != nil {
			return readErr
		}
		var ruleLine string
		for _, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, predicateRuleAnchor) {
				ruleLine = line
				break
			}
		}
		if ruleLine == "" {
			t.Errorf("%s: no line carries the rule anchor — "+
				"TestEveryBuiltinPersona_CarriesThePredicateExhaustivenessRule should have failed already", path)
			return nil
		}
		remainders[path] = strings.TrimSpace(
			strings.ReplaceAll(strings.ReplaceAll(ruleLine, predicateRuleAnchor, ""), predicateFilingAnchor, ""))
		return nil
	})
	require.NoError(t, err, "walking built-in personas")

	seen := map[string]string{} // remainder -> first file carrying it
	for path, remainder := range remainders {
		if first, dup := seen[remainder]; dup {
			t.Errorf("built-in personas %s and %s carry a byte-identical predicate-exhaustiveness "+
				"bullet outside the two anchors — the prose around the anchors must be each persona's "+
				"own voice (docs/personas-authoring.md); pasted copies make reviewers converge and "+
				"inflate reconcile CONFIDENCE without adding independent evidence", first, path)
		} else {
			seen[remainder] = path
		}
	}
}

// TestAuthoringDoc_QuotesTheEnforcedAnchors pins docs/personas-authoring.md
// against the two constants above. The doc quotes both phrases verbatim — in the
// panel-wide rule paragraph and in the release checklist — as the contract
// contributors must satisfy; a phrase edited in the constants leaves authoring
// instructions that produce personas which fail the suite. The guard lives in
// THIS package, beside the constants, so the doc cannot drift from the strings
// it documents (doc-content precedent: internal/personas/personas_test.go).
func TestAuthoringDoc_QuotesTheEnforcedAnchors(t *testing.T) {
	body, err := os.ReadFile("../docs/personas-authoring.md")
	require.NoError(t, err, "docs/personas-authoring.md must exist")
	for _, anchor := range []string{predicateRuleAnchor, predicateFilingAnchor} {
		require.Containsf(t, string(body), anchor,
			"docs/personas-authoring.md no longer quotes the enforced anchor %q — the "+
				"authoring instructions now describe a contract the suite does not check; "+
				"update the doc and the constants together", anchor)
	}
}

// predicateRuleCtx is the render context for the rule-reachability check. It
// differs from renderContext (personas_test.go) only in exposing ToolsEnabled,
// which is the whole point of the assertion below.
func predicateRuleCtx(tools bool) payload.PayloadContext {
	return payload.PayloadContext{
		AgentName:    "tester",
		BaseRef:      "main",
		HeadRef:      "feature",
		FileCount:    1,
		PayloadMode:  string(payload.ModeBlocks),
		Payload:      "<sample diff>",
		ScopeRule:    payload.ScopeRule(payload.ModeBlocks),
		ToolsEnabled: tools,
	}
}

// TestPredicateExhaustivenessRule_RendersWithToolsEitherWay asserts the rule
// reaches the RENDERED prompt, not merely the file on disk.
//
// Placement is the trap: the {{if .ToolsEnabled}} block spans _base.md's
// Tool-Assisted Review and Reasoning Budget sections, and every per-agent file
// repeats that layout. A rule appended near Reasoning Budget would satisfy the
// file-level walk above and still render for tool-enabled agents only — silently
// exempting every single-shot agent. ## Focus sits outside that block, so the
// rule survives both settings, and this test fails if it ever moves inside.
func TestPredicateExhaustivenessRule_RendersWithToolsEitherWay(t *testing.T) {
	prompts := map[string]string{}

	base, err := Base()
	require.NoError(t, err)
	prompts["_base.md"] = base

	for _, name := range Names() {
		text, err := Get(name)
		require.NoErrorf(t, err, "Get(%q)", name)
		prompts[name+".md"] = text
	}

	for file, text := range prompts {
		for _, tools := range []bool{true, false} {
			out, err := payload.RenderPrompt(text, predicateRuleCtx(tools))
			require.NoErrorf(t, err, "%s: render with ToolsEnabled=%v", file, tools)
			for _, anchor := range []string{predicateRuleAnchor, predicateFilingAnchor} {
				require.Containsf(t, out, anchor,
					"%s: %q absent from the RENDERED prompt with ToolsEnabled=%v — the "+
						"predicate-exhaustiveness rule must live under ## Focus, outside the "+
						"{{if .ToolsEnabled}} block", file, anchor, tools)
			}
		}
	}
}
