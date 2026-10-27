// Package threatmap is the optional threat-model mapping step (Epic 37.0): one
// call to a cheap mapper agent that names which changed files are security
// relevant, so the expensive review panel sees only those. The mapper can only
// remove files. Its reply is cut to the paths it was shown, and every failure
// is an error the caller reads as "keep every file" (fail open).
package threatmap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/personas"
)

// Completer is the mapper agent as Map sees it. Persona returns the agent's
// persona template, so the prompt comes from the persona file and not from Go
// code; Complete sends the rendered prompt and returns the reply. *Agent is the
// production implementation.
type Completer interface {
	Persona() string
	Complete(ctx context.Context, prompt string) (string, error)
}

// Map makes one call to the mapper and returns the paths it kept, in the order
// of paths. A reply path that is not in paths is ignored, never added. Any
// failure (no paths, a bad persona, a call error or timeout, a reply that is
// empty, malformed, or keeps nothing it was shown) returns a nil slice and an
// error; the caller then keeps every file.
func Map(ctx context.Context, completer Completer, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("threatmap: no paths to map")
	}
	persona := completer.Persona()
	if strings.TrimSpace(persona) == "" {
		return nil, errors.New("threatmap: mapper persona is empty")
	}
	// The path list goes in as data through {{.Payload}}; only the persona text
	// is parsed as a template, so a path holding "{{" is never executed.
	prompt, err := payload.RenderPrompt(persona, payload.PayloadContext{
		AgentName: personas.ThreatMapperName,
		FileCount: len(paths),
		Payload:   strings.Join(paths, "\n"),
	})
	if err != nil {
		return nil, fmt.Errorf("threatmap: render mapper persona: %w", err)
	}
	reply, err := completer.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("threatmap: mapper call: %w", err)
	}
	named, err := parseReply(reply)
	if err != nil {
		return nil, fmt.Errorf("threatmap: mapper reply: %w", err)
	}
	keep := make([]string, 0, len(named))
	for _, p := range paths {
		if named[p] {
			keep = append(keep, p)
			delete(named, p) // a path listed twice in paths is kept once
		}
	}
	if len(keep) == 0 {
		// Trimming every file would leave the panel nothing to review; a mapper
		// that keeps nothing it was shown is treated as a failed mapper.
		return nil, errors.New("threatmap: mapper reply keeps none of the input paths")
	}
	return keep, nil
}

// reply is the one shape the mapper persona asks for: {"keep": [...]}.
type reply struct {
	Keep *[]string `json:"keep"`
}

// parseReply strictly decodes the mapper reply: one JSON object, bare or in a
// single ``` fence, with only the "keep" key holding a list of strings, and
// nothing after it. It returns the named paths as a set.
func parseReply(text string) (map[string]bool, error) {
	body, err := unfence(strings.TrimSpace(text))
	if err != nil {
		return nil, err
	}
	if body == "" {
		return nil, errors.New("empty reply")
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var r reply
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("not a {\"keep\": [...]} object: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON object")
	}
	if r.Keep == nil {
		return nil, errors.New(`missing "keep" list`)
	}
	named := make(map[string]bool, len(*r.Keep))
	for _, p := range *r.Keep {
		named[p] = true
	}
	return named, nil
}

// unfence strips one surrounding ``` or ```json fence. Text that opens a fence
// without closing it is malformed; text with no fence is returned unchanged.
func unfence(text string) (string, error) {
	if !strings.HasPrefix(text, "```") {
		return text, nil
	}
	open, rest, found := strings.Cut(text, "\n")
	if !found || (open != "```" && open != "```json") || !strings.HasSuffix(rest, "```") {
		return "", errors.New("malformed code fence")
	}
	return strings.TrimSpace(strings.TrimSuffix(rest, "```")), nil
}

// LLM is the provider call an Agent makes. *llmclient.Client satisfies it.
type LLM interface {
	Complete(ctx context.Context, inv llmclient.Invocation) (string, error)
}

// Agent is the mapper agent: the normal agents: entry that
// settings.threat_map.agent names, resolved to its persona text and its LLM
// call. It implements Completer.
type Agent struct {
	persona string
	legacy  bool
	inv     llmclient.Invocation
	timeout time.Duration
	llm     LLM
}

// NewAgent resolves the agents: entry called name into a mapper Agent. Its
// persona is the entry's persona key, or nora when the key is unset.
// The registry load sets an unset key to the agent's name, so that name counts
// as unset unless a <name>.md persona file exists. A <persona>.md in the project or registry personas dir wins; otherwise only
// nora (or its legacy key threat_mapper) has an embedded default. The mapper never falls back to
// _base.md: that is the reviewer template, whose findings reply never parses
// here.
func NewAgent(name string, reg *registry.Registry, settings registry.Settings, dirs registry.PersonaDirs, llm LLM) (*Agent, error) {
	if reg == nil {
		return nil, errors.New("threatmap: no registry")
	}
	ac, ok := reg.Agents[name]
	if !ok {
		return nil, fmt.Errorf("threatmap: mapper agent %q not found in registry", name)
	}
	prov, ok := reg.Providers[ac.Provider]
	if !ok {
		return nil, fmt.Errorf("threatmap: mapper agent %q: provider %q not found in registry", name, ac.Provider)
	}
	persona, legacy, err := resolvePersona(name, ac.Persona, dirs)
	if err != nil {
		return nil, fmt.Errorf("threatmap: mapper agent %q: %w", name, err)
	}
	timeoutSecs := ac.EffectiveTimeoutSecs(settings)
	if timeoutSecs <= 0 {
		timeoutSecs = registry.DefaultTimeoutSecs
	}
	// Same order as the review path: the run-wide --max-tokens, then the
	// agent's own max_tokens, then the built-in default.
	maxTokens := registry.DefaultMaxTokens
	switch {
	case settings.MaxTokens > 0:
		maxTokens = settings.MaxTokens
	case ac.MaxTokens != nil && *ac.MaxTokens > 0:
		maxTokens = *ac.MaxTokens
	}
	return &Agent{
		persona: persona,
		legacy:  legacy,
		inv: llmclient.Invocation{
			BaseURL:          prov.BaseURL,
			APIKeyEnv:        prov.APIKeyEnv,
			Model:            ac.Model,
			Temperature:      ac.Temperature,
			MaxTokens:        &maxTokens,
			ResponseFormat:   ac.ResponseFormat,
			Thinking:         ac.Thinking,
			ThinkingLevel:    ac.ThinkingLevel,
			ThinkingStyle:    ac.ThinkingStyle,
			PreserveThinking: ac.PreserveThinking,
		},
		timeout: time.Duration(timeoutSecs) * time.Second,
		llm:     llm,
	}, nil
}

// resolvePersona loads the mapper persona text for the persona ref ref of the
// agent called name. The registry load defaults an unset persona key to the
// agent's own name, so a ref equal to name with no <name>.md file is treated
// as unset and falls to nora. A ref of the legacy key threat_mapper tries a
// threat_mapper.md override first, so an existing override keeps winning; with
// none, the alias maps it to nora (and its own override or embedded text), and
// legacy is true so the caller can warn.
func resolvePersona(name, ref string, dirs registry.PersonaDirs) (text string, legacy bool, err error) {
	if ref == "" {
		ref = personas.ThreatMapperName
		if name == personas.LegacyThreatMapperName {
			ref = name
		}
	}
	p, ok, err := registry.ResolvePersonaFile(ref, dirs)
	if err != nil {
		return "", false, err
	}
	if ok {
		return p.Text, false, nil
	}
	switch {
	case ref == personas.LegacyThreatMapperName:
		text, _, err = resolvePersona(name, personas.ThreatMapperName, dirs)
		return text, true, err
	case ref == personas.ThreatMapperName:
		text, err = personas.ThreatMapper()
		return text, false, err
	case ref == name:
		return resolvePersona(name, "", dirs)
	}
	return "", false, fmt.Errorf("persona %q not found in %s or %s: %w", ref, dirs.Project, dirs.Registry, registry.ErrPersonaNotFound)
}

// UsedLegacyKey reports whether the persona came from the deprecated
// threat_mapper key through the alias, not from a threat_mapper.md override.
func (a *Agent) UsedLegacyKey() bool { return a.legacy }

// LegacyKeyWarning is the one-per-run deprecation notice for that alias.
const LegacyKeyWarning = "the threat_mapper persona key is deprecated and now resolves to nora; rename persona: threat_mapper to persona: nora (a threat_mapper.md override still wins)"

// Persona returns the resolved persona template.
func (a *Agent) Persona() string { return a.persona }

// Complete sends prompt to the mapper model under the agent's own timeout.
func (a *Agent) Complete(ctx context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	inv := a.inv
	inv.Prompt = prompt
	return a.llm.Complete(ctx, inv)
}

var (
	_ Completer = (*Agent)(nil)
	_ LLM       = (*llmclient.Client)(nil)
)
