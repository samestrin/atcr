package personas

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
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

		ruleLine, ruleErr := predicateRuleLineUnderFocus(string(body))
		if ruleErr != nil {
			t.Errorf("built-in persona %s does not carry the predicate-exhaustiveness rule — %v", path, ruleErr)
			return nil
		}
		if !strings.Contains(ruleLine, predicateFilingAnchor) {
			t.Errorf("built-in persona %s states the predicate-exhaustiveness rule but not how to "+
				"file what it finds — %q must appear verbatim on the SAME ## Focus bullet as the "+
				"lens anchor, or a correct finding about an unchanged sibling branch is discarded "+
				"by the grounding gate", path, predicateFilingAnchor)
		}
		return nil
	})
	require.NoError(t, err, "walking built-in personas")

	if checked == 0 {
		t.Fatal("no embedded built-in persona files were checked — the walk found nothing to pin")
	}
}

// stripTemplateActions removes any leading {{...}} template actions from line
// and returns the remainder, whitespace-trimmed. An unterminated action yields
// "". Mirrors internal/registry/persona_base_reach_test.go:81, and for the same
// reason: persona headings are routinely prefixed by a template action
// ({{if .ToolsEnabled}}## Tool-Assisted Review, {{end}}## Severity Rubric), so a
// literal "## " scan runs straight past them and swallows the rest of the file.
func stripTemplateActions(line string) string {
	line = strings.TrimSpace(line)
	for strings.HasPrefix(line, "{{") {
		stop := strings.Index(line, "}}")
		if stop < 0 {
			return ""
		}
		line = strings.TrimSpace(line[stop+2:])
	}
	return line
}

// opensSection reports whether line starts a new "## " section heading,
// ignoring any leading template actions. "###" and deeper are not sections.
func opensSection(line string) bool {
	stripped := stripTemplateActions(line)
	return strings.HasPrefix(stripped, "##") && !strings.HasPrefix(stripped, "###")
}

// predicateRuleLineUnderFocus returns the single line inside text's "## Focus"
// section that carries predicateRuleAnchor.
//
// Checking each anchor as a substring of the WHOLE file was the original guard
// and it pinned nothing structural: a persona carrying the lens under ## Focus
// and the filing anchor buried in a fenced ## Output Format example satisfied
// both checks, splitting a rule whose whole point is that the lens is
// unreportable without the filing mechanic attached to it. Resolving the rule
// line within the ## Focus span and requiring both anchors ON IT is what fixed
// that, and it keeps the rule outside the {{if .ToolsEnabled}} block, which opens
// two sections later — so single-shot agents still receive it.
//
// WHAT IT DOES NOT CHECK: that the line is NUMBERED. The span and the two anchors
// are the whole predicate; a rule on an unnumbered line under ## Focus passes. The
// shipped built-ins all number it `6.` and docs/personas-authoring.md tells authors
// to, but that is an authoring convention this guard does not enforce, so neither
// this comment nor the error below claims it does.
func predicateRuleLineUnderFocus(text string) (string, error) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if stripTemplateActions(line) == "## Focus" {
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no \"## Focus\" heading — the rule has nowhere to live")
	}
	for _, line := range lines[start+1:] {
		if opensSection(line) {
			break
		}
		if strings.Contains(line, predicateRuleAnchor) {
			return line, nil
		}
	}
	return "", fmt.Errorf("the lens anchor %q is absent from the ## Focus section — it must be on a "+
		"bullet there, in prose adapted to this persona's "+
		"voice, not in another section and not inside a fenced example", predicateRuleAnchor)
}

// TestPredicateRuleLineUnderFocus_MessageClaimsOnlyWhatItChecks pins the "WHAT IT
// DOES NOT CHECK" paragraph above against the failure message below it. The guard
// resolves the rule line by span plus anchor and never inspects numbering, so an
// unnumbered bullet under ## Focus is a PASS — the first assertion states that
// directly. The second is the one that drifted: the message named the built-ins'
// `6.` inside the sentence stating the requirement, so an author reading only the
// failure would take numbering for an enforced invariant and hunt for a violation
// the predicate cannot see. A message may describe the requirement it enforces;
// this one must not describe a convention it does not.
func TestPredicateRuleLineUnderFocus_MessageClaimsOnlyWhatItChecks(t *testing.T) {
	unnumbered := "## Focus\n\n- " + predicateRuleAnchor + ", then " + predicateFilingAnchor + ".\n"
	line, err := predicateRuleLineUnderFocus(unnumbered)
	require.NoError(t, err, "an unnumbered bullet under ## Focus must resolve — the guard checks span and anchor, never numbering")
	assert.Contains(t, line, predicateRuleAnchor)

	_, missErr := predicateRuleLineUnderFocus("## Focus\n\n- nothing relevant here.\n")
	require.Error(t, missErr)
	assert.NotContains(t, missErr.Error(), "number it",
		"the failure message must not cite the built-ins' numbering convention: the predicate does not check it, so naming it here sends the author after a violation the guard cannot see")
}

// TestEveryBuiltinPersona_PredicateRuleStaysInItsOwnVoice pins the heterogeneity
// constraint the authoring guide states (docs/personas-authoring.md): word the
// prose around the two anchors in your persona's own voice, not another
// persona's sentence. The anchor checks above cannot see a pasted bullet — an
// identical rule line satisfies them in every file — so distinctness needs its
// own guard. Strip both anchors from each file's rule line and fail if any
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
		// Same resolution as the class guard above — a rule line found anywhere
		// in the file would let a stray copy outside ## Focus supply the
		// remainder that gets compared for distinctness.
		ruleLine, ruleErr := predicateRuleLineUnderFocus(string(body))
		if ruleErr != nil {
			t.Errorf("%s: %v — "+
				"TestEveryBuiltinPersona_CarriesThePredicateExhaustivenessRule should have failed already", path, ruleErr)
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
	ctx := renderContext("<sample diff>")
	ctx.ToolsEnabled = tools
	return ctx
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

	// Walk a SORTED key list and assert (not require) on the anchors: prompts is
	// a map, so Go randomises iteration order, and a t.FailNow inside the loop
	// would abort on the first offender and leave the rest unrendered — a
	// maintainer fixes one file, re-runs, and is handed a different name with no
	// idea how many remain. require stays on the render error itself, which is a
	// broken template rather than a missing rule. Neighbouring class guards make
	// the same choice: internal/payload/tools_persona_test.go:41-48 and
	// personas/clean_review_marker_test.go:46-59.
	promptFiles := make([]string, 0, len(prompts))
	for file := range prompts {
		promptFiles = append(promptFiles, file)
	}
	sort.Strings(promptFiles)

	for _, file := range promptFiles {
		for _, tools := range []bool{true, false} {
			out, err := payload.RenderPrompt(prompts[file], predicateRuleCtx(tools))
			require.NoErrorf(t, err, "%s: render with ToolsEnabled=%v", file, tools)
			ruleLine, ruleErr := predicateRuleLineUnderFocus(out)
			if ruleErr != nil {
				assert.Failf(t, "predicate rule absent from rendered prompt",
					"%s: RENDERED with ToolsEnabled=%v — the predicate-exhaustiveness rule must "+
						"live under ## Focus, outside the {{if .ToolsEnabled}} block: %v", file, tools, ruleErr)
				continue
			}
			assert.Containsf(t, ruleLine, predicateFilingAnchor,
				"%s: %q absent from the rendered ## Focus rule line with ToolsEnabled=%v — the "+
					"filing mechanic must travel on the same bullet as the lens",
				file, predicateFilingAnchor, tools)
		}
	}
}
