package personas

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
)

// predicateRuleAnchor is the one phrase every built-in persona carries VERBATIM.
//
// The rule around it is deliberately re-voiced per persona: nine agents handed a
// byte-identical paragraph converge, and correlated findings inflate reconcile's
// CONFIDENCE = HIGH (2+ distinct reviewers) without adding independent evidence.
// A single short invariant anchor is what makes the class checkable anyway, so
// the prose varies and this phrase does not.
const predicateRuleAnchor = "enumerate every branch of that predicate and every field it is contracted to cover"

// predicateFilingAnchor is the second phrase every built-in persona carries
// verbatim: the rule's filing mechanic, without which the rule is unreportable.
//
// A predicate's sibling branch is normally UNCHANGED code, and the grounding
// gate discards any finding whose FILE:LINE falls outside the changed lines (the
// one escape, CATEGORY out-of-scope, is annotated and never promoted). So a
// reviewer that correctly spots the asymmetry and cites the untouched sibling
// has its finding deleted before it reaches the report — on exactly the defect
// the rule was written for. Anchoring the finding on the edited branch and
// quoting the sibling as EVIDENCE keeps it inside the gate and promotable.
//
// Unlike the lens above, this is a mechanical citation instruction rather than a
// judgement, so sharing its wording across the panel costs no independence.
const predicateFilingAnchor = "file it on the edited branch's changed line"

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
// literal list, so a built-in persona added later cannot silently omit the rule.
// personas.go's init() already panics unless the embedded .md set equals names
// plus _base.md, so today the two enumerations are provably the same set; the
// walk is preferred because it stays correct without depending on that invariant
// continuing to hold. Community prompts (communityFiles) are deliberately NOT
// walked — they are out of scope for the epic that added this rule.
func TestEveryBuiltinPersona_CarriesThePredicateExhaustivenessRule(t *testing.T) {
	var checked int

	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, readErr := fs.ReadFile(files, path)
		if readErr != nil {
			t.Errorf("%s: %v", path, readErr)
			return nil
		}
		checked++

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
