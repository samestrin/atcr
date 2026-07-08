package personas

import (
	"fmt"
	"os"
	"strings"

	"github.com/samestrin/atcr/internal/payload"
	builtins "github.com/samestrin/atcr/personas"
	"gopkg.in/yaml.v3"
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

	// If a personas directory is configured, prefer an installed on-disk unit
	// (the production resolver chain) over the embedded library copy. The fixture
	// patch stays embedded-only because InstallUnit never writes testdata patches
	// to disk.
	if r.PersonasDir != nil {
		dir, perr := r.PersonasDir()
		if perr == nil {
			text, model, found, ferr := loadInstalledPersona(dir, name)
			if ferr != nil {
				return FixtureOutcome{}, ferr
			}
			if found {
				if err := assertBoundModel(name, model); err != nil {
					return FixtureOutcome{}, err
				}
				patchContent, err := builtins.CommunityFixture(name)
				if err != nil {
					return FixtureOutcome{HasFixture: false}, nil
				}
				return renderFixture(name, text, patchContent)
			}
		}
	}

	// Community-library persona: resolve its co-located template + fixture from
	// the embedded community layout. A name with no embedded community template
	// (an arbitrary/namespaced name) is not a library persona → HasFixture: false.
	text, err := builtins.CommunityGet(name)
	if err != nil {
		return FixtureOutcome{HasFixture: false}, nil
	}

	// The embedded .md resolved, so this IS a library persona. AC 06-03 (the AC7
	// authoring-contract gate): a library persona MUST bind a non-empty model in
	// its structured metadata. Enforce this immediately — BEFORE the fixture
	// lookup — so a missing/absent fixture cannot silently suppress the
	// model-binding contract (a resolved library persona with no .yaml is a broken
	// authoring state and hard-fails here). Built-ins are exempt: they resolve
	// through the isBuiltin branch above and carry no provider/model
	// (model-agnostic per C2). This check is purely structural: no network, no LLM.
	model, err := builtins.CommunityModel(name)
	if err != nil {
		return FixtureOutcome{}, fmt.Errorf("resolve community persona %q metadata: %w", name, err)
	}
	if err := assertBoundModel(name, model); err != nil {
		return FixtureOutcome{}, err
	}

	patchContent, err := builtins.CommunityFixture(name)
	if err != nil {
		return FixtureOutcome{HasFixture: false}, nil
	}
	return renderFixture(name, text, patchContent)
}

// loadInstalledPersona reads an installed persona unit from dir. It returns the
// .md template text, the bound model extracted from the co-located .yaml, a
// found flag, and any error. If either file is missing, found is false and err
// is nil so the caller can fall back to the embedded library copy.
func loadInstalledPersona(dir, name string) (text string, model string, found bool, err error) {
	yamlPath, perr := personaPath(dir, name)
	if perr != nil {
		return "", "", false, perr
	}
	mdPath := strings.TrimSuffix(yamlPath, ".yaml") + ".md"

	mdData, merr := os.ReadFile(mdPath)
	if merr != nil {
		if os.IsNotExist(merr) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("read installed persona prompt %q: %w", name, merr)
	}
	yamlData, yerr := os.ReadFile(yamlPath)
	if yerr != nil {
		if os.IsNotExist(yerr) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("read installed persona metadata %q: %w", name, yerr)
	}

	var meta struct {
		Model string `yaml:"model"`
	}
	if uerr := yaml.Unmarshal(yamlData, &meta); uerr != nil {
		return "", "", false, fmt.Errorf("parse installed persona metadata %q: %w", name, uerr)
	}
	return string(mdData), meta.Model, true, nil
}

// assertBoundModel enforces AC 06-03: a community/library persona must carry a
// non-empty bound model in its structured metadata. A blank model fails with a
// clear, attributable error naming the persona and the missing field, distinct
// from the template-unrendered failure path. Built-in personas are model-agnostic
// (C2) and never reach this check.
func assertBoundModel(name, model string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("persona %q: bound model missing from structured metadata", name)
	}
	return nil
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
	// RenderPrompt already errors on parse/execute failures and missing keys,
	// so reaching here means the template rendered successfully. The AgentName
	// check catches templates that contain no actionable substitutions at all.
	rendered := strings.Contains(out, ctx.AgentName)
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
