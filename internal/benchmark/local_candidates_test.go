package benchmark

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// localCandidatesPath is the epic 113 candidate-profile file. It is benchmark
// input, not quickstart behavior, so it lives under benchmarks/ and the CLI
// keeps only the picks (refine answer Q10, EST-6493).
const localCandidatesPath = "../../benchmarks/local-candidates.yaml"

// thinkingMaxTokensFloor is the least max_tokens a thinking candidate may run
// with. The thinking tokens count against the output cap, and the review
// default (registry.DefaultMaxTokens, 8192) starves a thinking model into
// near-empty replies (plan 113, Why).
const thinkingMaxTokensFloor = 16384

// The two hardware tiers of plan 113.
var localCandidateTiers = []string{"gpu-32gb", "unified-64gb"}

type localCandidate struct {
	Name          string   `yaml:"name"`
	OllamaTag     string   `yaml:"ollama_tag"`
	HFID          string   `yaml:"hf_id"`
	Architecture  string   `yaml:"architecture"`
	ParamsB       float64  `yaml:"params_b"`
	ActiveParamsB float64  `yaml:"active_params_b"`
	Quantization  string   `yaml:"quantization"`
	SizeGB        float64  `yaml:"size_gb"`
	MinOllama     string   `yaml:"min_ollama"`
	Tiers         []string `yaml:"tiers"`
	Baseline      bool     `yaml:"baseline"`
	ChatTemplate  string   `yaml:"chat_template"`
	StopTokens    string   `yaml:"stop_tokens"`
	ThinkingModel bool     `yaml:"thinking_model"`
	ThinkingStyle string   `yaml:"thinking_style"`
	Thinking      string   `yaml:"thinking"`
	ThinkingLevel string   `yaml:"thinking_level"`
	MaxTokens     int      `yaml:"max_tokens"`
	Notes         string   `yaml:"notes"`
}

type localCandidatesFile struct {
	Version    int              `yaml:"version"`
	Candidates []localCandidate `yaml:"candidates"`
}

// decodeLocalCandidates decodes strictly, so a misspelt key (thinking_stlye)
// fails instead of reading as an absent field.
func decodeLocalCandidates(data []byte) (localCandidatesFile, error) {
	var f localCandidatesFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err := dec.Decode(&f)
	return f, err
}

// checkLocalCandidate returns every fault in one profile. A thinking candidate
// is one whose model thinks (thinking_model) or whose profile turns thinking on;
// it must declare its thinking fields and run with at least
// thinkingMaxTokensFloor output tokens. The thinking and max_tokens fields
// must also load as a registry agent, so a profile cannot carry a value the
// review would reject.
func checkLocalCandidate(c localCandidate) []string {
	var faults []string
	add := func(format string, a ...any) {
		faults = append(faults, fmt.Sprintf("candidate %q: ", c.Name)+fmt.Sprintf(format, a...))
	}
	if c.Name == "" {
		add("name is empty")
	}
	if c.OllamaTag == "" && c.HFID == "" {
		add("needs an ollama_tag or an hf_id")
	}
	if c.Architecture != "dense" && c.Architecture != "moe" {
		add("architecture %q must be dense or moe", c.Architecture)
	}
	if c.Architecture == "moe" && (c.ActiveParamsB <= 0 || c.ActiveParamsB >= c.ParamsB) {
		add("an moe candidate needs active_params_b between 0 and params_b")
	}
	if c.ParamsB <= 0 {
		add("params_b is missing")
	}
	if c.Quantization == "" {
		add("quantization is empty")
	}
	if c.SizeGB <= 0 {
		add("size_gb at the stated quantization is missing")
	}
	if len(c.Tiers) == 0 {
		add("names no tier")
	}
	for _, tier := range c.Tiers {
		if !slices.Contains(localCandidateTiers, tier) {
			add("unknown tier %q: must be one of %s", tier, strings.Join(localCandidateTiers, ", "))
		}
	}
	if strings.TrimSpace(c.ChatTemplate) == "" {
		add("chat_template note is empty")
	}
	if strings.TrimSpace(c.StopTokens) == "" {
		add("stop_tokens note is empty")
	}
	thinkingOn := registry.ThinkingEnabled(c.Thinking, c.ThinkingLevel)
	if c.ThinkingModel {
		if c.ThinkingStyle == "" {
			add("is a thinking model but has no thinking_style")
		}
		if !registry.ThinkingDeclared(c.Thinking, c.ThinkingLevel) {
			add("is a thinking model but declares neither thinking nor thinking_level")
		}
	}
	if (c.ThinkingModel || thinkingOn) && c.Thinking != registry.ThinkingOff && c.MaxTokens < thinkingMaxTokensFloor {
		add("thinks but max_tokens is %d, below %d", c.MaxTokens, thinkingMaxTokensFloor)
	}
	if !c.ThinkingModel && thinkingOn {
		add("turns thinking on but thinking_model is false")
	}
	if c.MaxTokens <= 0 {
		add("max_tokens is missing")
	}
	if err := registry.ValidateAgentYAML(c.Name, c.agentYAML()); err != nil {
		add("does not load as a registry agent: %v", err)
	}
	return faults
}

// agentYAML renders the profile's registry fields as one agent entry on the
// local Ollama provider, the way the quickstart preset writes it.
func (c localCandidate) agentYAML() []byte {
	agent := map[string]any{
		"provider":   "local",
		"model":      c.OllamaTag,
		"max_tokens": c.MaxTokens,
	}
	for key, v := range map[string]string{
		"thinking_style": c.ThinkingStyle,
		"thinking":       c.Thinking,
		"thinking_level": c.ThinkingLevel,
	} {
		if v != "" {
			agent[key] = v
		}
	}
	out, err := yaml.Marshal(agent)
	if err != nil {
		panic(err)
	}
	return out
}

func loadLocalCandidates(t *testing.T) localCandidatesFile {
	t.Helper()
	data, err := os.ReadFile(localCandidatesPath)
	require.NoError(t, err)
	f, err := decodeLocalCandidates(data)
	require.NoError(t, err)
	return f
}

// TestLocalCandidates_EveryProfileIsComplete is AC1 of plan 113: every
// candidate has a committed profile, and a thinking candidate carries its
// thinking fields and max_tokens >= 16384.
func TestLocalCandidates_EveryProfileIsComplete(t *testing.T) {
	f := loadLocalCandidates(t)
	assert.Equal(t, 1, f.Version)
	require.NotEmpty(t, f.Candidates)
	seen := map[string]bool{}
	for _, c := range f.Candidates {
		assert.Empty(t, checkLocalCandidate(c))
		assert.False(t, seen[c.Name], "duplicate candidate %q", c.Name)
		seen[c.Name] = true
	}
}

// TestLocalCandidates_CoversThePlanCandidates pins the candidate list of plan
// 113 (Proposed Solution 2), so a candidate cannot drop out of the benchmark
// by losing its profile.
func TestLocalCandidates_CoversThePlanCandidates(t *testing.T) {
	f := loadLocalCandidates(t)
	var tags []string
	baselines := 0
	for _, c := range f.Candidates {
		tags = append(tags, c.OllamaTag)
		if c.Baseline {
			baselines++
			assert.Equal(t, "gemma3:27b-it-q4_K_M", c.OllamaTag, "the baseline is today's gerald model")
		}
	}
	assert.ElementsMatch(t, []string{
		"qwen3.8:27b-q4_K_M",
		"gemma4:31b-it-q4_K_M",
		"gemma4:26b-a4b-it-q4_K_M",
		"qwen3.6:35b-a3b-q4_K_M",
		"gpt-oss:20b",
		"nemotron-3-nano:30b-a3b-q4_K_M",
		"gemma3:27b-it-q4_K_M",
	}, tags)
	assert.Equal(t, 1, baselines)
}

// validThinkingCandidate is a complete thinking profile; the cases below break
// one field each.
func validThinkingCandidate() localCandidate {
	return localCandidate{
		Name:          "qwen3.8-27b",
		OllamaTag:     "qwen3.8:27b-q4_K_M",
		HFID:          "Qwen/Qwen3.8-27B",
		Architecture:  "dense",
		ParamsB:       27.3,
		Quantization:  "Q4_K_M",
		SizeGB:        17.74,
		Tiers:         []string{"gpu-32gb", "unified-64gb"},
		ChatTemplate:  "Ollama qwen3.8 renderer",
		StopTokens:    "set by the renderer",
		ThinkingModel: true,
		ThinkingStyle: registry.ThinkingStyleReasoningEffort,
		Thinking:      registry.ThinkingOn,
		ThinkingLevel: registry.ThinkingLevelHigh,
		MaxTokens:     32768,
	}
}

// TestCheckLocalCandidate_CatchesABrokenThinkingProfile proves the check both
// ways: each broken profile is caught with a fault naming the problem, and
// the complete ones pass.
func TestCheckLocalCandidate_CatchesABrokenThinkingProfile(t *testing.T) {
	assert.Empty(t, checkLocalCandidate(validThinkingCandidate()))

	nonThinking := validThinkingCandidate()
	nonThinking.ThinkingModel = false
	nonThinking.ThinkingStyle, nonThinking.Thinking, nonThinking.ThinkingLevel = "", "", ""
	nonThinking.MaxTokens = registry.DefaultMaxTokens
	assert.Empty(t, checkLocalCandidate(nonThinking), "a non-thinking model may keep the default cap")

	thinkingOff := validThinkingCandidate()
	thinkingOff.ThinkingStyle = registry.ThinkingStyleTemplateKwargs
	thinkingOff.Thinking, thinkingOff.ThinkingLevel = registry.ThinkingOff, ""
	thinkingOff.MaxTokens = registry.DefaultMaxTokens
	assert.Empty(t, checkLocalCandidate(thinkingOff), "a thinking model declared off needs no thinking cap")

	cases := map[string]struct {
		mutate func(*localCandidate)
		want   string
	}{
		// The bench36 Qwen3.8 setup: thinking on at the 8192 default.
		"below the floor":     {func(c *localCandidate) { c.MaxTokens = registry.DefaultMaxTokens }, "below 16384"},
		"one under the floor": {func(c *localCandidate) { c.MaxTokens = thinkingMaxTokensFloor - 1 }, "below 16384"},
		"no thinking_style":   {func(c *localCandidate) { c.ThinkingStyle = "" }, "no thinking_style"},
		"no thinking": {func(c *localCandidate) { c.Thinking, c.ThinkingLevel = "", "" },
			"declares neither thinking nor thinking_level"},
		"thinking on, not a thinking model": {func(c *localCandidate) { c.ThinkingModel = false },
			"thinking_model is false"},
		"thinking on with no floor, not a thinking model": {func(c *localCandidate) {
			c.ThinkingModel = false
			c.MaxTokens = registry.DefaultMaxTokens
		}, "below 16384"},
		"illegal style": {func(c *localCandidate) { c.ThinkingStyle = "harmony" }, "does not load as a registry agent"},
		"style needs a level": {func(c *localCandidate) { c.ThinkingLevel = "" },
			"does not load as a registry agent"},
		"no quantization":           {func(c *localCandidate) { c.Quantization = "" }, "quantization is empty"},
		"no size":                   {func(c *localCandidate) { c.SizeGB = 0 }, "size_gb"},
		"no model id":               {func(c *localCandidate) { c.OllamaTag, c.HFID = "", "" }, "ollama_tag or an hf_id"},
		"no template note":          {func(c *localCandidate) { c.ChatTemplate = " " }, "chat_template"},
		"no stop note":              {func(c *localCandidate) { c.StopTokens = "" }, "stop_tokens"},
		"unknown tier":              {func(c *localCandidate) { c.Tiers = []string{"tier-32gb-dense"} }, "unknown tier"},
		"no tier":                   {func(c *localCandidate) { c.Tiers = nil }, "names no tier"},
		"bad architecture":          {func(c *localCandidate) { c.Architecture = "" }, "architecture"},
		"moe without active params": {func(c *localCandidate) { c.Architecture = "moe" }, "active_params_b"},
		"no params":                 {func(c *localCandidate) { c.ParamsB = 0 }, "params_b is missing"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := validThinkingCandidate()
			tc.mutate(&c)
			faults := checkLocalCandidate(c)
			require.NotEmpty(t, faults)
			assert.Contains(t, strings.Join(faults, "\n"), tc.want)
			assert.Contains(t, faults[0], `candidate "qwen3.8-27b"`, "the fault names the candidate")
		})
	}
}

// TestDecodeLocalCandidates_RejectsAnUnknownKey: a misspelt thinking key must
// fail the decode, not read as a candidate without its thinking fields.
func TestDecodeLocalCandidates_RejectsAnUnknownKey(t *testing.T) {
	_, err := decodeLocalCandidates([]byte("version: 1\ncandidates:\n  - name: x\n    thinking_stlye: qwen\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "thinking_stlye")
}
