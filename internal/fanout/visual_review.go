package fanout

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
)

// ErrVisionAgentUnset means no visual_review.agent is configured. It is a
// usage error: the operator asked for a visual review and the engine will not
// guess which agent may see the screenshots.
var ErrVisionAgentUnset = errors.New("the visual review needs a vision agent: set visual_review.agent in .atcr/config.yaml (or ~/.config/atcr/registry.yaml) to an agents: entry that declares supports_vision")

// ErrVisionAgentRefused means the configured agent may not receive
// screenshots: it is not in the registry, does not declare supports_vision, or
// sits on a non-loopback host without the cloud opt-in.
var ErrVisionAgentRefused = errors.New("vision agent refused")

// VisionAgent is the agent selected to review screenshots. Loopback is false
// only when allowCloud admitted a non-loopback provider, which means the
// screenshot pixels leave the machine.
type VisionAgent struct {
	Name     string
	Agent    registry.AgentConfig
	Provider registry.Provider
	Loopback bool
}

// SelectVisionAgent returns the visual_review.agent if it may receive
// screenshots: it must declare supports_vision, and its provider base_url must
// be a loopback host unless allowCloud is true. An empty base_url is the
// client's remote default, so it counts as non-loopback. An unset key returns
// ErrVisionAgentUnset; every refusal wraps ErrVisionAgentRefused. Both are
// raised before any provider call.
func SelectVisionAgent(reg *registry.Registry, settings registry.Settings, allowCloud bool) (VisionAgent, error) {
	name := settings.VisualReview.Agent
	if name == "" {
		return VisionAgent{}, ErrVisionAgentUnset
	}
	if reg == nil {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q: no registry", ErrVisionAgentRefused, name)
	}
	ac, ok := reg.Agents[name]
	if !ok {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q not found in registry", ErrVisionAgentRefused, name)
	}
	if !ac.SupportsVision {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q does not declare supports_vision", ErrVisionAgentRefused, name)
	}
	prov, ok := reg.Providers[ac.Provider]
	if !ok {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q: provider %q not found in registry", ErrVisionAgentRefused, name, ac.Provider)
	}
	loopback := prov.IsLoopback()
	if !loopback && !allowCloud {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q: provider %q is not on a loopback host, and screenshots go only to a local model unless --allow-cloud-vision is passed", ErrVisionAgentRefused, name, ac.Provider)
	}
	return VisionAgent{Name: name, Agent: ac, Provider: prov, Loopback: loopback}, nil
}

// ErrNoScreenshots means the browser run returned no screenshot bytes, so the
// screenshot review has nothing to look at and no request is sent.
var ErrNoScreenshots = errors.New("the browser run returned no screenshots to review")

// Labels for the captured text segments in a secret_guard warning or block
// error.
const (
	visualPromptLabel      = "visual review prompt"
	visualScreenshotsLabel = "screenshot names"
	visualConsoleLabel     = "captured console text"
	visualNetworkLabel     = "captured failed-request text"
	visualTestsLabel       = "failed test messages"
	visualSpecLabel        = "generated spec path"
)

// VisionChatter is the one client call the screenshot review makes.
// *llmclient.Client satisfies it.
type VisionChatter interface {
	Chat(ctx context.Context, inv llmclient.Invocation, messages []llmclient.Message, toolDefs []llmclient.ToolDef) (*llmclient.ChatResponse, error)
}

// VisualReviewRequest is the input to call 2 of the visual review.
type VisualReviewRequest struct {
	// Prompt is the persona's rendered screenshot-review prompt.
	Prompt string
	// SpecPath is the generated spec, e2e/<name>.spec.ts, that findings cite.
	SpecPath string
	// Browser is what the browser run of that spec brought back.
	Browser BrowserResult
	// Changed is the patch's grounding data for groundFindings.
	Changed payload.ChangedLines
}

// VisualReviewResult is the outcome of call 2. Result carries the reply and
// ImagesSentOffMachine for the agent's status.json; Findings are the parsed
// findings that passed the grounding gate, and Ungrounded counts the rest.
type VisualReviewResult struct {
	Result     Result
	Findings   []stream.Finding
	Ungrounded int
}

// RunVisualReview is call 2 of the visual review: it sends the screenshots as
// image parts, with the prompt and the captured console, failed-request and
// failed-test text, in one user message to the vision agent, and parses the
// reply into findings. Every text bound for the provider passes guardSecrets
// under settings.SecretGuard first, so block returns ErrSecretGuardBlocked
// before any request and redact sends the redacted text. Screenshot pixels
// are not scanned, which is why the agent is loopback unless allowCloud
// admitted it; when it is not, Result.ImagesSentOffMachine records that the
// pixels left the machine. Findings go through groundFindings: the persona
// files them as CATEGORY out-of-scope on SpecPath, which the gate keeps.
func RunVisualReview(ctx context.Context, client VisionChatter, agent VisionAgent, settings registry.Settings, req VisualReviewRequest) (VisualReviewResult, error) {
	start := time.Now()
	var shots []BrowserScreenshot
	for _, sc := range req.Browser.Screenshots {
		if len(sc.Data) > 0 {
			shots = append(shots, sc)
		}
	}
	if len(shots) == 0 {
		return VisualReviewResult{}, fmt.Errorf("vision agent %q: %w", agent.Name, ErrNoScreenshots)
	}
	cfg := &ReviewConfig{Settings: settings}
	segs := []guardSegment{
		{text: req.Prompt, file: visualPromptLabel},
		{text: consoleText(req.Browser.Console), file: visualConsoleLabel},
		{text: networkText(req.Browser.NetworkFailures), file: visualNetworkLabel},
		{text: failedTestsText(req.Browser.FailedTests), file: visualTestsLabel},
		{text: req.SpecPath, file: visualSpecLabel},
	}
	for i, sc := range shots {
		segs = append(segs, guardSegment{text: screenshotLabel(i, sc), file: visualScreenshotsLabel})
	}
	guarded, err := guardSecrets(secretGuardPolicy(cfg), agent.Name, segs...)
	if err != nil {
		return VisualReviewResult{}, err
	}
	labels := guarded[len(segs)-len(shots):]

	parts := make([]llmclient.ContentPart, 0, 2*len(shots)+1)
	for i, sc := range shots {
		parts = append(parts,
			llmclient.TextPart(labels[i]),
			llmclient.ImageURLPart("data:"+screenshotMIME(sc)+";base64,"+base64.StdEncoding.EncodeToString(sc.Data)))
	}
	var b strings.Builder
	b.WriteString(guarded[0])
	fmt.Fprintf(&b, "\n\nGenerated spec: %s\n", guarded[4])
	writeCaptured(&b, "Console messages", guarded[1])
	writeCaptured(&b, "Failed requests", guarded[2])
	writeCaptured(&b, "Failed tests", guarded[3])
	parts = append(parts, llmclient.TextPart(b.String()))

	if !agent.Loopback {
		log.FromContext(ctx).Warn("sending screenshots to a non-loopback vision endpoint; the pixels leave this machine and are not scanned by secret_guard",
			"agent", agent.Name, "screenshots", len(shots))
	}
	ac := agent.Agent
	maxTokens := maxTokensFor(cfg, ac)
	inv := llmclient.Invocation{
		BaseURL:          agent.Provider.BaseURL,
		APIKeyEnv:        agent.Provider.APIKeyEnv,
		Model:            ac.Model,
		Temperature:      ac.Temperature,
		MaxTokens:        &maxTokens,
		ResponseFormat:   ac.ResponseFormat,
		Thinking:         ac.Thinking,
		ThinkingLevel:    ac.ThinkingLevel,
		ThinkingStyle:    ac.ThinkingStyle,
		PreserveThinking: ac.PreserveThinking,
		SupportsVision:   ac.SupportsVision,
	}
	resp, err := client.Chat(ctx, inv, []llmclient.Message{{Role: "user", Parts: parts}}, nil)
	if err != nil {
		return VisualReviewResult{}, fmt.Errorf("vision agent %q: %w", agent.Name, err)
	}
	content := ""
	if resp.Message.Content != nil {
		content = *resp.Message.Content
	}
	answer, _ := llmclient.SplitThink(content)
	findings := stream.ParseModelOutput([]byte(answer))
	for i := range findings {
		findings[i].Reviewer = agent.Name
	}
	kept, ungrounded := groundFindings(findings, req.Changed)
	return VisualReviewResult{
		Result: Result{
			Agent:                agent.Name,
			Content:              content,
			Status:               StatusOK,
			DurationMS:           time.Since(start).Milliseconds(),
			ResponseTruncated:    resp.Truncated,
			ImagesSentOffMachine: !agent.Loopback,
			Model:                ac.Model,
			TokensIn:             resp.Usage.PromptTokens,
			TokensOut:            resp.Usage.CompletionTokens,
			CallRecords:          resp.CallRecords,
		},
		Findings:   kept,
		Ungrounded: ungrounded,
	}, nil
}

// screenshotLabel names screenshot i in the request, with the runner's name
// and path flattened to one line.
func screenshotLabel(i int, sc BrowserScreenshot) string {
	name := sc.Name
	if sc.Path != "" {
		name += " (" + sc.Path + ")"
	}
	return strings.Join(strings.Fields(fmt.Sprintf("Screenshot %d: %s", i+1, name)), " ")
}

// screenshotMIME is the image type for a data URL: from the file extension,
// else sniffed from the bytes, else PNG (Playwright's default).
func screenshotMIME(sc BrowserScreenshot) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(sc.Path))); strings.HasPrefix(t, "image/") {
		return t
	}
	if t := http.DetectContentType(sc.Data); strings.HasPrefix(t, "image/") {
		return t
	}
	return "image/png"
}

func consoleText(entries []ConsoleEntry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "- [%s] %s", e.Level, e.Text)
		if e.Location != "" {
			fmt.Fprintf(&b, " (%s)", e.Location)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func networkText(failures []NetworkFailure) string {
	var b strings.Builder
	for _, f := range failures {
		fmt.Fprintf(&b, "- %s %s", f.Method, f.URL)
		if f.Status != 0 {
			fmt.Fprintf(&b, " -> HTTP %d", f.Status)
		}
		if f.Error != "" {
			fmt.Fprintf(&b, " -> %s", f.Error)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func failedTestsText(tests []BrowserTestFailure) string {
	var b strings.Builder
	for _, t := range tests {
		fmt.Fprintf(&b, "- %s (%s)", t.Title, t.Status)
		if t.Error != "" {
			fmt.Fprintf(&b, ": %s", t.Error)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeCaptured appends one captured-text section, "(none)" when empty.
func writeCaptured(b *strings.Builder, title, text string) {
	if strings.TrimSpace(text) == "" {
		text = "(none)\n"
	}
	fmt.Fprintf(b, "\n%s:\n%s", title, text)
}
