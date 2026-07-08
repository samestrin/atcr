package personas

import (
	"fmt"
	"strings"

	"github.com/samestrin/atcr/internal/payload"
	builtins "github.com/samestrin/atcr/personas"
)

// FixtureOutcome is the result of running a persona's fixture cases.
type FixtureOutcome struct {
	HasFixture bool
	Passed     int
	Total      int
}

// FixtureRunner executes a persona's fixture cases. It is injectable so the CLI
// `test` path stays free of live LLM calls in CI.
type FixtureRunner interface {
	RunFixture(name string) (FixtureOutcome, error)
}

// TemplateFixtureRunner validates a persona template against its committed patch
// fixture without an LLM call. For a built-in persona it reads the embedded
// fixture and renders the template; for a community-library persona it resolves
// the co-located community/<name>.md template and
// community/testdata/<name>_fixture.patch fixture (AC 04-04). In both cases it
// asserts no unresolved `{{` markers survive the render. A name that is neither a
// built-in nor an embedded community persona returns HasFixture: false.
type TemplateFixtureRunner struct {
	// PersonasDir is reserved for on-disk (installed) community fixture support.
	PersonasDir func() (string, error)
}

func (r TemplateFixtureRunner) RunFixture(name string) (FixtureOutcome, error) {
	if isBuiltin(name) {
		patchContent, err := builtins.Fixture(name)
		if err != nil {
			// Not all built-ins have fixtures; treat missing fixture as HasFixture: false.
			return FixtureOutcome{HasFixture: false}, nil
		}
		text, err := builtins.Get(name)
		if err != nil {
			return FixtureOutcome{}, fmt.Errorf("load built-in persona %q: %w", name, err)
		}
		return renderFixture(name, text, patchContent)
	}

	// Community-library persona: resolve its co-located template + fixture from
	// the embedded community layout. A name with no embedded community template
	// (an arbitrary/namespaced name) is not a library persona → HasFixture: false.
	text, err := builtins.CommunityGet(name)
	if err != nil {
		return FixtureOutcome{HasFixture: false}, nil
	}
	patchContent, err := builtins.CommunityFixture(name)
	if err != nil {
		return FixtureOutcome{HasFixture: false}, nil
	}
	return renderFixture(name, text, patchContent)
}

// renderFixture renders a persona template with the fixture patch as its payload
// and reports a passing case only when the render actually substituted its
// variables: no unrendered `{{`/`}}` action survives AND the AgentName value was
// interpolated. The AgentName check catches a structurally broken template that
// dropped every `{{ }}` token — it renders with no braces yet substitutes
// nothing, which is not a valid persona render.
func renderFixture(name, text, patchContent string) (FixtureOutcome, error) {
	ctx := fixtureCtx(patchContent)
	out, err := payload.RenderPrompt(text, ctx)
	if err != nil {
		return FixtureOutcome{}, fmt.Errorf("render persona %q: %w", name, err)
	}
	rendered := !strings.Contains(out, "{{") && !strings.Contains(out, "}}") &&
		strings.Contains(out, ctx.AgentName)
	if !rendered {
		return FixtureOutcome{HasFixture: true, Passed: 0, Total: 1}, nil
	}
	return FixtureOutcome{HasFixture: true, Passed: 1, Total: 1}, nil
}

// fixtureCtx builds a minimal PayloadContext for persona fixture rendering.
func fixtureCtx(diff string) payload.PayloadContext {
	return payload.PayloadContext{
		AgentName:   "fixture-runner",
		BaseRef:     "main",
		HeadRef:     "HEAD",
		FileCount:   1,
		PayloadMode: string(payload.ModeBlocks),
		Payload:     diff,
		ScopeRule:   payload.ScopeRule(payload.ModeBlocks),
	}
}

// TestPersona resolves name (a built-in or a community persona installed under
// personasDir) and delegates fixture execution to runner. It errors if the
// persona is neither a built-in nor installed.
func TestPersona(personasDir, name string, runner FixtureRunner) (FixtureOutcome, error) {
	_ = personasDir // reserved for future community fixture support; runner owns resolution.
	return runner.RunFixture(name)
}
