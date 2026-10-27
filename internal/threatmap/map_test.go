package threatmap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/require"
)

// fakeCompleter is the mapper agent as Map sees it: a fixed persona and a
// scripted reply. It records every prompt so a test can count the calls and
// read what was sent.
type fakeCompleter struct {
	persona string
	reply   string
	err     error
	block   bool // wait for ctx to end, then return its error
	prompts []string
}

func (f *fakeCompleter) Persona() string { return f.persona }

func (f *fakeCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.reply, f.err
}

const testPersona = "map these {{.FileCount}} files for {{.AgentName}}:\n{{.Payload}}\n"

func TestMap_ReplyIsCutToTheInputPaths(t *testing.T) {
	fc := &fakeCompleter{persona: testPersona, reply: `{"keep": ["auth.go", "evil.go"]}`}

	keep, err := Map(context.Background(), fc, []string{"auth.go", "style.css", "README.md"})

	require.NoError(t, err)
	require.Equal(t, []string{"auth.go"}, keep, "an unknown path is ignored, never added")
	require.Len(t, fc.prompts, 1, "exactly one call to the mapper agent")
}

func TestMap_PromptIsTheRenderedPersona(t *testing.T) {
	fc := &fakeCompleter{persona: testPersona, reply: `{"keep": ["a.go"]}`}

	_, err := Map(context.Background(), fc, []string{"a.go", "b.css"})

	require.NoError(t, err)
	require.Equal(t, []string{"map these 2 files for nora:\na.go\nb.css\n"}, fc.prompts,
		"the prompt is the persona rendered over the path list and nothing else")
}

func TestMap_KeepsInputOrderAndDropsDuplicates(t *testing.T) {
	fc := &fakeCompleter{persona: testPersona, reply: `{"keep": ["c.go", "a.go", "c.go"]}`}

	keep, err := Map(context.Background(), fc, []string{"a.go", "b.go", "c.go"})

	require.NoError(t, err)
	require.Equal(t, []string{"a.go", "c.go"}, keep)
}

func TestMap_AcceptsOneJSONFence(t *testing.T) {
	fc := &fakeCompleter{persona: testPersona, reply: "```json\n{\"keep\": [\"auth.go\"]}\n```\n"}

	keep, err := Map(context.Background(), fc, []string{"auth.go", "style.css"})

	require.NoError(t, err)
	require.Equal(t, []string{"auth.go"}, keep)
}

// Every failure returns an error and no paths: the caller reads an error as
// "keep every file", so none of these may trim anything.
func TestMap_FailuresReturnAnError(t *testing.T) {
	paths := []string{"auth.go", "style.css"}
	cases := []struct {
		name string
		fc   *fakeCompleter
	}{
		{"completer error", &fakeCompleter{err: errors.New("provider down")}},
		{"timeout", &fakeCompleter{block: true}},
		{"empty reply", &fakeCompleter{reply: ""}},
		{"blank reply", &fakeCompleter{reply: " \n\t"}},
		{"prose", &fakeCompleter{reply: "auth.go looks risky"}},
		{"prose around the json", &fakeCompleter{reply: `Sure: {"keep": ["auth.go"]}`}},
		{"trailing data", &fakeCompleter{reply: `{"keep": ["auth.go"]} {"keep": []}`}},
		{"unknown key", &fakeCompleter{reply: `{"keep": ["auth.go"], "drop": ["style.css"]}`}},
		{"keep is not a list", &fakeCompleter{reply: `{"keep": "auth.go"}`}},
		{"keep holds a non-string", &fakeCompleter{reply: `{"keep": ["auth.go", 7]}`}},
		{"keep missing", &fakeCompleter{reply: `{}`}},
		{"keep null", &fakeCompleter{reply: `{"keep": null}`}},
		{"bare array", &fakeCompleter{reply: `["auth.go"]`}},
		{"unclosed fence", &fakeCompleter{reply: "```json\n{\"keep\": [\"auth.go\"]}\n"}},
		{"empty keep", &fakeCompleter{reply: `{"keep": []}`}},
		{"only unknown paths", &fakeCompleter{reply: `{"keep": ["evil.go", "../auth.go", "./auth.go"]}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.fc.persona = testPersona
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			keep, err := Map(ctx, tc.fc, paths)

			require.Error(t, err)
			require.Nil(t, keep)
		})
	}
}

func TestMap_NoPathsMakesNoCall(t *testing.T) {
	fc := &fakeCompleter{persona: testPersona, reply: `{"keep": ["a.go"]}`}

	keep, err := Map(context.Background(), fc, nil)

	require.Error(t, err)
	require.Nil(t, keep)
	require.Empty(t, fc.prompts)
}

func TestMap_BadPersonaMakesNoCall(t *testing.T) {
	for name, persona := range map[string]string{
		"empty":            " \n",
		"unparseable":      "{{.Payload",
		"unknown variable": "{{.Nope}}",
	} {
		t.Run(name, func(t *testing.T) {
			fc := &fakeCompleter{persona: persona, reply: `{"keep": ["a.go"]}`}

			keep, err := Map(context.Background(), fc, []string{"a.go"})

			require.Error(t, err)
			require.Nil(t, keep)
			require.Empty(t, fc.prompts)
		})
	}
}

// fakeLLM stands in for *llmclient.Client behind Agent.
type fakeLLM struct {
	reply       string
	block       bool
	invs        []llmclient.Invocation
	hadDeadline bool
}

func (f *fakeLLM) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	f.invs = append(f.invs, inv)
	_, f.hadDeadline = ctx.Deadline()
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.reply, nil
}

func intp(v int) *int { return &v }

func testRegistry() *registry.Registry {
	return &registry.Registry{
		Providers: map[string]registry.Provider{
			"cheap": {APIKeyEnv: "CHEAP_KEY", BaseURL: "https://cheap.example/v1"},
		},
		Agents: map[string]registry.AgentConfig{
			"mapper":        {Provider: "cheap", Model: "small-1", Persona: personas.ThreatMapperName, MaxTokens: intp(512)},
			"threat_mapper": {Provider: "cheap", Model: "small-2"},
			"custom":        {Provider: "cheap", Model: "small-3", Persona: "my_mapper"},
			"orphan":        {Provider: "missing", Model: "small-4", Persona: personas.ThreatMapperName},
		},
	}
}

func embeddedMapper(t *testing.T) string {
	t.Helper()
	text, err := personas.ThreatMapper()
	require.NoError(t, err)
	return text
}

func TestNewAgent_UsesTheEmbeddedPersona(t *testing.T) {
	llm := &fakeLLM{}
	a, err := NewAgent("mapper", testRegistry(), registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{}, llm)

	require.NoError(t, err)
	require.Equal(t, embeddedMapper(t), a.Persona())
}

// An agent named threat_mapper with no persona key uses the mapper persona,
// never the shared _base.md reviewer template, even when a project _base.md
// exists: the reviewer template asks for findings, which never parse here.
func TestNewAgent_NeverFallsBackToBase(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "_base.md"), []byte("reviewer base {{.Payload}}"), 0o644))

	a, err := NewAgent("threat_mapper", testRegistry(), registry.Settings{TimeoutSecs: 30},
		registry.PersonaDirs{Project: project}, &fakeLLM{})

	require.NoError(t, err)
	require.Equal(t, embeddedMapper(t), a.Persona())
}

// A persona key still naming the pre-Epic 83.0 mapper key resolves to the
// embedded persona (now nora) instead of failing as an unknown persona.
func TestNewAgent_LegacyPersonaKeyUsesTheEmbeddedPersona(t *testing.T) {
	reg := testRegistry()
	reg.Agents["legacy"] = registry.AgentConfig{Provider: "cheap", Model: "small-5", Persona: personas.LegacyThreatMapperName}

	a, err := NewAgent("legacy", reg, registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{}, &fakeLLM{})

	require.NoError(t, err)
	require.Equal(t, embeddedMapper(t), a.Persona())
}

func TestNewAgent_LegacyKeyFlagsTheAliasOnly(t *testing.T) {
	reg := testRegistry()
	reg.Agents["legacy"] = registry.AgentConfig{Provider: "cheap", Model: "small-5", Persona: personas.LegacyThreatMapperName}

	a, err := NewAgent("legacy", reg, registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{}, &fakeLLM{})
	require.NoError(t, err)
	require.True(t, a.UsedLegacyKey())

	a, err = NewAgent("threat_mapper", testRegistry(), registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{}, &fakeLLM{})
	require.NoError(t, err)
	require.True(t, a.UsedLegacyKey(), "an agent named threat_mapper with no persona key uses the alias")

	a, err = NewAgent("mapper", testRegistry(), registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{}, &fakeLLM{})
	require.NoError(t, err)
	require.False(t, a.UsedLegacyKey())
}

// An existing threat_mapper.md override is tried before the alias maps, so it
// keeps winning, and it is not a deprecated-alias use.
func TestNewAgent_LegacyOverrideFileStillWins(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "threat_mapper.md"), []byte("old override {{.Payload}}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "nora.md"), []byte("nora override {{.Payload}}"), 0o644))
	reg := testRegistry()
	reg.Agents["legacy"] = registry.AgentConfig{Provider: "cheap", Model: "small-5", Persona: personas.LegacyThreatMapperName}

	a, err := NewAgent("legacy", reg, registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{Project: project}, &fakeLLM{})

	require.NoError(t, err)
	require.Equal(t, "old override {{.Payload}}", a.Persona())
	require.False(t, a.UsedLegacyKey())
}

// With no threat_mapper.md, the alias lands on a nora.md override.
func TestNewAgent_LegacyKeyReachesNoraOverride(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "nora.md"), []byte("nora override {{.Payload}}"), 0o644))
	reg := testRegistry()
	reg.Agents["legacy"] = registry.AgentConfig{Provider: "cheap", Model: "small-5", Persona: personas.LegacyThreatMapperName}

	a, err := NewAgent("legacy", reg, registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{Project: project}, &fakeLLM{})

	require.NoError(t, err)
	require.Equal(t, "nora override {{.Payload}}", a.Persona())
	require.True(t, a.UsedLegacyKey())
}

func TestNewAgent_ProjectPersonaFileWins(t *testing.T) {
	project := t.TempDir()
	override := "edited mapper {{.Payload}}"
	require.NoError(t, os.WriteFile(filepath.Join(project, personas.ThreatMapperName+".md"), []byte(override), 0o644))

	a, err := NewAgent("mapper", testRegistry(), registry.Settings{TimeoutSecs: 30},
		registry.PersonaDirs{Project: project}, &fakeLLM{})

	require.NoError(t, err)
	require.Equal(t, override, a.Persona())
}

func TestNewAgent_CustomPersonaRef(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "my_mapper.md"), []byte("mine {{.Payload}}"), 0o644))

	a, err := NewAgent("custom", testRegistry(), registry.Settings{TimeoutSecs: 30},
		registry.PersonaDirs{Project: project}, &fakeLLM{})
	require.NoError(t, err)
	require.Equal(t, "mine {{.Payload}}", a.Persona())

	_, err = NewAgent("custom", testRegistry(), registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{}, &fakeLLM{})
	require.ErrorIs(t, err, registry.ErrPersonaNotFound, "a named persona with no file is an error, not a silent fallback")
}

func TestNewAgent_Errors(t *testing.T) {
	_, err := NewAgent("nobody", testRegistry(), registry.Settings{}, registry.PersonaDirs{}, &fakeLLM{})
	require.ErrorContains(t, err, `"nobody"`)

	_, err = NewAgent("orphan", testRegistry(), registry.Settings{}, registry.PersonaDirs{}, &fakeLLM{})
	require.ErrorContains(t, err, `"missing"`)

	_, err = NewAgent("mapper", nil, registry.Settings{}, registry.PersonaDirs{}, &fakeLLM{})
	require.Error(t, err)
}

func TestAgent_CompleteSendsTheAgentsInvocation(t *testing.T) {
	llm := &fakeLLM{reply: "ok"}
	a, err := NewAgent("mapper", testRegistry(), registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{}, llm)
	require.NoError(t, err)

	got, err := a.Complete(context.Background(), "the prompt")

	require.NoError(t, err)
	require.Equal(t, "ok", got)
	require.Len(t, llm.invs, 1)
	inv := llm.invs[0]
	require.Equal(t, "https://cheap.example/v1", inv.BaseURL)
	require.Equal(t, "CHEAP_KEY", inv.APIKeyEnv)
	require.Equal(t, "small-1", inv.Model)
	require.Equal(t, "the prompt", inv.Prompt)
	require.NotNil(t, inv.MaxTokens)
	require.Equal(t, 512, *inv.MaxTokens)
	require.True(t, llm.hadDeadline, "every mapper call carries a deadline")
}

func TestNewAgent_MaxTokensOrder(t *testing.T) {
	cases := []struct {
		name     string
		agentCap *int
		runCap   int
		want     int
	}{
		{"run-wide override wins", intp(512), 2048, 2048},
		{"agent cap", intp(512), 0, 512},
		{"zero agent cap reads as unset", intp(0), 0, registry.DefaultMaxTokens},
		{"no cap", nil, 0, registry.DefaultMaxTokens},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := testRegistry()
			ac := reg.Agents["mapper"]
			ac.MaxTokens = tc.agentCap
			reg.Agents["mapper"] = ac
			llm := &fakeLLM{reply: "ok"}
			a, err := NewAgent("mapper", reg, registry.Settings{TimeoutSecs: 30, MaxTokens: tc.runCap}, registry.PersonaDirs{}, llm)
			require.NoError(t, err)

			_, err = a.Complete(context.Background(), "p")

			require.NoError(t, err)
			require.Equal(t, tc.want, *llm.invs[0].MaxTokens)
		})
	}
}

func TestNewAgent_RejectsATraversingPersonaRef(t *testing.T) {
	reg := testRegistry()
	reg.Agents["sneaky"] = registry.AgentConfig{Provider: "cheap", Model: "m", Persona: "../outside"}

	_, err := NewAgent("sneaky", reg, registry.Settings{}, registry.PersonaDirs{Project: t.TempDir()}, &fakeLLM{})

	require.ErrorContains(t, err, "invalid persona name")
}

func TestAgent_CompleteTimesOut(t *testing.T) {
	llm := &fakeLLM{block: true}
	reg := testRegistry()
	ac := reg.Agents["mapper"]
	ac.TimeoutSecs = intp(1)
	reg.Agents["mapper"] = ac
	a, err := NewAgent("mapper", reg, registry.Settings{TimeoutSecs: 600}, registry.PersonaDirs{}, llm)
	require.NoError(t, err)

	start := time.Now()
	_, err = a.Complete(context.Background(), "p")

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 5*time.Second, "the agent's own timeout_secs bounds the call")
}

func TestAgent_ZeroTimeoutStillHasADeadline(t *testing.T) {
	llm := &fakeLLM{reply: "ok"}
	a, err := NewAgent("mapper", testRegistry(), registry.Settings{}, registry.PersonaDirs{}, llm)
	require.NoError(t, err)

	_, err = a.Complete(context.Background(), "p")

	require.NoError(t, err)
	require.True(t, llm.hadDeadline)
}

// End to end through the real embedded persona: it renders over a path list,
// and its reply contract is the one Map parses.
func TestMap_WithTheEmbeddedPersona(t *testing.T) {
	llm := &fakeLLM{reply: `{"keep": ["internal/auth/login.go", "evil.go"]}`}
	a, err := NewAgent("mapper", testRegistry(), registry.Settings{TimeoutSecs: 30}, registry.PersonaDirs{}, llm)
	require.NoError(t, err)
	paths := []string{"internal/auth/login.go", "web/style.css", "README.md"}

	keep, err := Map(context.Background(), a, paths)

	require.NoError(t, err)
	require.Equal(t, []string{"internal/auth/login.go"}, keep)
	require.Len(t, llm.invs, 1)
	prompt := llm.invs[0].Prompt
	for _, p := range paths {
		require.Contains(t, prompt, p)
	}
	require.Contains(t, prompt, `"keep"`, "the persona states the reply shape Map parses")
	require.False(t, strings.Contains(prompt, "{{"), "no template action is left unrendered")
}

// loadRegistry writes yaml to a registry file and loads it the way the CLI
// does, so applyDefaults runs: an agent with no persona key comes back with
// Persona set to its own name.
func loadRegistry(t *testing.T, yaml string) *registry.Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))
	reg, err := registry.LoadRegistry(path)
	require.NoError(t, err)
	return reg
}

const scoutRegistry = `providers:
  cheap:
    base_url: https://cheap.example/v1
    api_key_env: CHEAP_KEY
agents:
  scout:
    provider: cheap
    model: small-1
`

// A mapper entry with no persona key uses the embedded threat_mapper persona
// whatever the agent is called, as docs/payload-modes.md says — even though
// the registry load defaults its persona to the agent's own name.
func TestNewAgent_UnsetPersonaDefaultsToThreatMapper(t *testing.T) {
	reg := loadRegistry(t, scoutRegistry)
	require.Equal(t, "scout", reg.Agents["scout"].Persona, "precondition: the load defaults persona to the agent name")

	a, err := NewAgent("scout", reg, registry.Settings{TimeoutSecs: 30},
		registry.PersonaDirs{Project: t.TempDir(), Registry: t.TempDir()}, &fakeLLM{})

	require.NoError(t, err)
	require.Equal(t, embeddedMapper(t), a.Persona())
}

// An edited threat_mapper.md still overrides the embedded default for a
// mapper with no persona key.
func TestNewAgent_UnsetPersonaUsesAnEditedThreatMapper(t *testing.T) {
	project := t.TempDir()
	override := "edited mapper {{.Payload}}"
	require.NoError(t, os.WriteFile(filepath.Join(project, personas.ThreatMapperName+".md"), []byte(override), 0o644))

	a, err := NewAgent("scout", loadRegistry(t, scoutRegistry), registry.Settings{TimeoutSecs: 30},
		registry.PersonaDirs{Project: project}, &fakeLLM{})

	require.NoError(t, err)
	require.Equal(t, override, a.Persona())
}

// A <name>.md persona file named after the agent still wins, in either dir,
// so an agent whose persona key was left to default to its own name keeps it.
func TestNewAgent_OwnNamePersonaFileWins(t *testing.T) {
	for _, tier := range []string{"project", "registry"} {
		t.Run(tier, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "scout.md"), []byte("scout {{.Payload}}"), 0o644))
			dirs := registry.PersonaDirs{Project: dir}
			if tier == "registry" {
				dirs = registry.PersonaDirs{Registry: dir}
			}

			a, err := NewAgent("scout", loadRegistry(t, scoutRegistry), registry.Settings{TimeoutSecs: 30}, dirs, &fakeLLM{})

			require.NoError(t, err)
			require.Equal(t, "scout {{.Payload}}", a.Persona())
		})
	}
}
