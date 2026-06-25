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
// fixture without an LLM call. For built-in personas it reads the embedded
// fixture and renders the template, then asserts no unresolved `{{` markers
// survive. For community personas it returns HasFixture: false (fixture metadata
// wiring in community YAML is a future enhancement).
type TemplateFixtureRunner struct {
	// PersonasDir is reserved for future community fixture support.
	PersonasDir func() (string, error)
}

func (r TemplateFixtureRunner) RunFixture(name string) (FixtureOutcome, error) {
	if !isBuiltin(name) {
		return FixtureOutcome{HasFixture: false}, nil
	}
	patchContent, err := builtins.Fixture(name)
	if err != nil {
		// Not all built-ins have fixtures; treat missing fixture as HasFixture: false.
		return FixtureOutcome{HasFixture: false}, nil
	}
	text, err := builtins.Get(name)
	if err != nil {
		return FixtureOutcome{}, fmt.Errorf("load built-in persona %q: %w", name, err)
	}
	out, err := payload.RenderPrompt(text, fixtureCtx(patchContent))
	if err != nil {
		return FixtureOutcome{}, fmt.Errorf("render persona %q: %w", name, err)
	}
	if strings.Contains(out, "{{") {
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
