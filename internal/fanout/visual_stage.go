package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
)

// ErrVisualNeedsRange rejects a VisualReviewer on a baseline (--all/--scope)
// or --diff-file review: felix authors its spec from a change set, and those
// paths have none to show it.
var ErrVisualNeedsRange = errors.New("the visual review (--preview-url) applies to git-range reviews only; it cannot be combined with a baseline (--all/--scope) or --diff-file review")

// visualSkipWarning is logged when felix is in the roster but no visual stage
// was built: without --preview-url there is no page to open.
const visualSkipWarning = "felix is in the roster but --preview-url is not set, so the visual review is skipped; pass --preview-url to run it"

// The lines that tell felix which of its two calls a request is; the persona
// says each request names it.
const (
	visualAuthorCallNote = "This is the spec-authoring call: reply with the one ```ts fence described under Spec Authoring."
	visualReviewCallNote = "This is the screenshot-review call: report findings as described under Screenshot Review."
)

// visualPayloadLabel names the diff text bound for felix's call 1 in a
// secret_guard warning or block error.
const visualPayloadLabel = "visual spec payload"

// VisualReviewer is the optional visual-review stage of a git-range review
// (Epic 36.0.2). It runs after the reviewers, when felix is in the roster, and
// its result is written as felix's. The CLI builds it from --preview-url, as
// it builds the ThreatMapper; VisualStage is the production implementation.
type VisualReviewer interface {
	RunVisual(ctx context.Context, in VisualStageInput) (VisualReviewResult, error)
}

// VisualStageInput is what the review hands the visual stage.
type VisualStageInput struct {
	// RepoDir is the absolute root of the repo under review; the spec is
	// written under it and the browser runs it there.
	RepoDir string
	// RunID is the review id; visualSpecRunID turns it into the spec name.
	RunID string
	// Prompt is felix's render context: the reviewers' diff payload for its
	// configured payload mode.
	Prompt payload.PayloadContext
	// Changed is the patch's grounding data.
	Changed payload.ChangedLines
	// Truncation records the files shed to fit felix's own model window;
	// felix's status.json carries it.
	Truncation payload.Truncation
}

// VisualStage runs call 1 (Author writes e2e/felix-<run id>.spec.ts), the
// browser (Browser runs that spec against PreviewURL), then call 2 (Vision,
// as Agent, reviews the screenshots). A failing step stops the stage.
type VisualStage struct {
	Author     SpecAuthor
	Browser    BrowserBackend
	Vision     VisionChatter
	Agent      VisionAgent
	Settings   registry.Settings
	PreviewURL string
}

var _ VisualReviewer = (*VisualStage)(nil)

// RunVisual implements VisualReviewer. The diff bound for felix's own provider
// passes guardSecrets first, as every reviewer payload does; call 2 guards its
// own text in RunVisualReview.
func (s *VisualStage) RunVisual(ctx context.Context, in VisualStageInput) (VisualReviewResult, error) {
	pc := in.Prompt
	if pc.AgentName == "" {
		pc.AgentName = VisualPersonaName
	}
	guarded, err := guardSecrets(secretGuardPolicy(&ReviewConfig{Settings: s.Settings}), VisualPersonaName,
		guardSegment{text: pc.Payload, file: visualPayloadLabel})
	if err != nil {
		return VisualReviewResult{}, err
	}
	diff := guarded[0]

	// Render call 2's prompt first, so a persona that cannot render fails
	// before any model call or browser run.
	reviewCtx := pc
	reviewCtx.Payload = visualReviewCallNote + "\n\n" + diff
	prompt, err := payload.RenderPrompt(s.Author.Persona(), reviewCtx)
	if err != nil {
		return VisualReviewResult{}, fmt.Errorf("visual review: render persona: %w", err)
	}
	authorCtx := pc
	authorCtx.Payload = visualAuthorCallNote + "\n\n" + diff
	spec, err := AuthorVisualSpec(ctx, s.Author, VisualAuthorRequest{RepoDir: in.RepoDir, RunID: visualSpecRunID(in.RunID), Prompt: authorCtx})
	if err != nil {
		return VisualReviewResult{}, err
	}
	shots, err := s.Browser.Run(ctx, BrowserRun{RepoDir: in.RepoDir, Spec: spec, PreviewURL: s.PreviewURL})
	if err != nil {
		return VisualReviewResult{}, fmt.Errorf("visual review: browser %s: %w", s.Browser.Name(), err)
	}
	return RunVisualReview(ctx, s.Vision, s.Agent, s.Settings, VisualReviewRequest{Prompt: prompt, SpecPath: spec, Browser: shots, Changed: in.Changed})
}

// visualSpecRunID maps a review id (<date>_<slug>, which may carry "_", "."
// and upper case) onto the spec-name pattern: lower case, every other
// character run collapsed to one "-", trimmed, and capped so felix-<id> fits
// in 64 characters. An id with nothing left becomes "review".
func visualSpecRunID(id string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if max := 64 - len(VisualPersonaName) - 1; len(out) > max {
		out = strings.TrimRight(out[:max], "-")
	}
	if out == "" {
		return "review"
	}
	return out
}

// visualInRoster reports whether felix is in the project roster.
func visualInRoster(p *registry.ProjectConfig) bool {
	return p != nil && slices.Contains(rosterNames(p), VisualPersonaName)
}

// prepareVisual decides the visual stage at prepare time: nil, with the skip
// warning, when felix is in the roster and no stage was built; nil when felix
// is not in the roster; otherwise the stage and its input.
func prepareVisual(ctx context.Context, cfg *ReviewConfig, req ReviewRequest, payloads map[string]modePayload, changed payload.ChangedLines) (VisualReviewer, VisualStageInput, error) {
	if !visualInRoster(cfg.Project) {
		return nil, VisualStageInput{}, nil
	}
	if req.Visual == nil {
		log.FromContext(ctx).Warn(visualSkipWarning)
		return nil, VisualStageInput{}, nil
	}
	repoDir, err := filepath.Abs(req.Repo)
	if err != nil {
		return nil, VisualStageInput{}, fmt.Errorf("visual review: %w", err)
	}
	ac := cfg.Registry.Agents[VisualPersonaName]
	mode := ac.EffectivePayloadMode(cfg.Settings)
	text, count, trunc, err := visualPayload(cfg, ac, payloads[mode], mode)
	if err != nil {
		return nil, VisualStageInput{}, err
	}
	return req.Visual, VisualStageInput{
		RepoDir: repoDir,
		Prompt: payload.PayloadContext{
			AgentName:   VisualPersonaName,
			BaseRef:     req.Range.Base,
			HeadRef:     req.Range.Head,
			FileCount:   count,
			PayloadMode: mode,
			Payload:     text,
			ScopeRule:   payload.ScopeRuleForPayload(payload.PayloadMode(mode), text),
		},
		Changed:    changed,
		Truncation: trunc,
	}, nil
}

// visualPayload sizes felix's diff to felix's own model window, as buildSlots
// sizes every reviewer's: felix has no slot, so the shared payload text would
// otherwise reach it unsized. The pre-budget entries are re-shed against the
// effective budget (capped by payload_byte_budget), less the call note each
// request prepends outside the budget, as a slot reserves its prepended scope
// constraint. A zero budget is an overflow, as on the bulk path: on_overflow
// fail or fallback refuses pre-dispatch, and otherwise felix is sent the
// smallest non-empty file. When nothing fits a non-zero budget, the smallest
// file is sent too; when everything fits, the shared text is sent whole.
func visualPayload(cfg *ReviewConfig, ac registry.AgentConfig, mp modePayload, mode string) (string, int, payload.Truncation, error) {
	if len(mp.Entries) == 0 {
		return mp.Text, mp.FileCount, mp.Truncation, nil
	}
	maxTokens := maxTokensFor(cfg, ac)
	agentBudget := payload.EffectiveByteBudget(ac.Model, ac.ContextWindowTokens, payload.SizingOutputTokens(false, maxTokens))
	budget := appliedByteBudget(agentBudget, cfg.Settings.PayloadByteBudget, visualLongestCallNote())
	var kept []payload.FileEntry
	var trunc payload.Truncation
	if budget <= 0 {
		if p := cfg.Settings.OnOverflow; p == OverflowFail || p == OverflowFallback {
			if _, err := applyOverflowPolicy(p, "", 0, mp.Entries, budget); err != nil {
				return "", 0, payload.Truncation{}, refuseOverflow(VisualPersonaName, payload.ContextWindowTokens(ac.Model, ac.ContextWindowTokens), maxTokens, p, 0, err)
			}
		}
		trunc.AllDropped = true
	} else {
		kept, trunc = payload.ApplyByteBudgetPreferEscalated(mp.Entries, budget, payload.PayloadMode(mode))
	}
	if trunc.AllDropped {
		var ok bool
		if kept, trunc, ok = keepSmallestEntry(mp.Entries); !ok {
			return mp.Text, mp.FileCount, mp.Truncation, nil
		}
	}
	if !trunc.Truncated {
		return mp.Text, mp.FileCount, mp.Truncation, nil
	}
	var b strings.Builder
	for _, e := range kept {
		b.WriteString(e.Body)
	}
	return b.String(), payload.ReviewableCount(kept), trunc, nil
}

// visualLongestCallNote is the longer of the two call notes with its
// separator: the most either request prepends to the sized payload.
func visualLongestCallNote() string {
	n := visualAuthorCallNote
	if len(visualReviewCallNote) > len(n) {
		n = visualReviewCallNote
	}
	return n + "\n\n"
}

// reviewerSucceeded reports whether a diff reviewer succeeded: the
// all-agents-failed gate ignores felix, so a visual review cannot pass a run in
// which no reviewer read the diff. A felix-only run is judged on felix.
func reviewerSucceeded(statuses []AgentStatus) bool {
	reviewers := 0
	for _, st := range statuses {
		if st.Agent == VisualPersonaName {
			continue
		}
		reviewers++
		if st.Status == StatusOK {
			return true
		}
	}
	if reviewers > 0 {
		return false
	}
	return slices.ContainsFunc(statuses, func(st AgentStatus) bool { return st.Status == StatusOK })
}

// runVisualStage runs the prepared stage and returns felix's result. A stage
// error is felix's failure, not the review's.
func runVisualStage(ctx context.Context, p *PreparedReview) Result {
	in := p.visualInput
	in.RunID = p.ID
	vr, err := p.visual.RunVisual(ctx, in)
	if err != nil {
		log.FromContext(ctx).Warn("visual review failed", "agent", VisualPersonaName, "err", err)
		return Result{Agent: VisualPersonaName, Status: StatusFailed, Err: err, Truncation: in.Truncation}
	}
	r := vr.Result
	r.Truncation = in.Truncation
	// The vision agent may also sit in the roster as a reviewer; the visual
	// record is felix's, and its model names the vision agent's.
	r.Agent = VisualPersonaName
	return r
}

// reviewerRoster is rosterNames without felix, which never holds a reviewer
// slot (Epic 36.0.2): the manifest roster that status and resume read lists
// only the agents the engine dispatches.
func reviewerRoster(p *registry.ProjectConfig) []string {
	names := rosterNames(p)
	return slices.DeleteFunc(names, func(n string) bool { return n == VisualPersonaName })
}

// visualRebuildRoster is the roster a resume rebuilds the pool from: the
// manifest roster, plus felix when the original run left a felix record, so a
// resume keeps the visual review's findings in the merged pool. The visual
// stage itself is not re-run on a resume.
func visualRebuildRoster(roster []string, poolDir string) []string {
	if slices.Contains(roster, VisualPersonaName) {
		return roster
	}
	if _, err := os.Stat(filepath.Join(poolDir, poolRawAgentDir, VisualPersonaName, "status.json")); err != nil {
		return roster
	}
	return append(slices.Clone(roster), VisualPersonaName)
}

// reviewerResults is results without felix's, for the all-agents-failed gate:
// a visual review cannot pass a run in which no reviewer read the diff. A
// felix-only run keeps felix's result, so it is judged on felix.
func reviewerResults(results []Result) []Result {
	out := slices.DeleteFunc(slices.Clone(results), func(r Result) bool { return r.Agent == VisualPersonaName })
	if len(out) == 0 {
		return results
	}
	return out
}
