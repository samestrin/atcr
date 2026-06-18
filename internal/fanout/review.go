package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
)

// ErrPayloadFullyDropped is returned by buildPayloads when a non-empty input
// has every file shed by the byte budget. A too-small --byte-budget silently
// produced zero findings before this guard; it now fails loudly so callers
// can surface a clear diagnostic rather than firing the reviewer pool at an
// empty payload.
var ErrPayloadFullyDropped = errors.New("payload fully dropped by byte budget: every changed file exceeds the configured --byte-budget")

// ErrNoReviewableContent reports a resolved range whose commits changed no
// reviewable files (e.g. only merge or empty commits), so every payload mode
// built empty. gitrange.ErrEmptyRange catches zero-commit ranges earlier;
// this is the complementary guard for commit-bearing ranges with no file
// changes. PrepareReview returns it before scaffolding, so a vacuous review
// never creates a directory, repoints .atcr/latest, or reaches the provider
// pool.
var ErrNoReviewableContent = errors.New("no reviewable content in range")

// ReviewConfig bundles the loaded configuration a review needs. Built by
// LoadReviewConfig so both the CLI and the MCP server discover config the same way.
type ReviewConfig struct {
	Registry    *registry.Registry
	Project     *registry.ProjectConfig
	Settings    registry.Settings
	PersonaDirs registry.PersonaDirs
}

// ReviewRange is the resolved git range as plain provenance fields. The engine
// package cannot import gitrange (package-boundary rule), so the caller resolves
// the range and passes the result in.
type ReviewRange struct {
	Base          string
	Head          string
	DetectionMode string
	DefaultBranch string
	CommitCount   int
}

// ReviewRequest is everything RunReview needs beyond the config: the repo/range,
// the branch + date used to derive the review id, the collision suffix, the run
// start time, and an optional id override.
type ReviewRequest struct {
	Repo       string // git work tree to diff
	Root       string // where .atcr lives (usually == Repo)
	Range      ReviewRange
	Branch     string
	Date       string // YYYY-MM-DD
	TimeSuffix string // HHMMSS collision suffix
	StartedAt  time.Time
	IDOverride string
	// OutputDir, when non-empty, redirects the whole review tree to this
	// (absolute) path instead of .atcr/reviews/<id>/, and suppresses the
	// .atcr/latest update. The id is still derived (for provenance/output) but
	// is not used for path construction. Mutually exclusive with IDOverride,
	// enforced by the CLI before the request is built.
	//
	// Security: arbitrary absolute paths (including outside the repo root) are
	// accepted by design; --output-dir is intended for trusted orchestrators that
	// own their output destination. PrepareReview rejects paths inside ReviewsRoot
	// to prevent invisible half-state reviews. Untrusted callers must validate the
	// path before populating this field.
	OutputDir string
}

// ReviewResult is the outcome of a completed review run.
type ReviewResult struct {
	ID      string
	Dir     string
	Summary Summary
}

// LoadReviewConfig loads the registry and project config under root, validates
// the roster against the registry, resolves shared settings with the CLI
// overlays, and computes the persona search dirs.
func LoadReviewConfig(root string, cli registry.CLIOverrides) (*ReviewConfig, error) {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return nil, err
	}
	// Merge the optional project registry overlay (.atcr/registry.yaml) onto the
	// user registry; the merged loader enforces the project-provider trust gate.
	reg, err := registry.LoadMergedRegistry(regPath, root)
	if err != nil {
		return nil, err
	}
	proj, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath(root))
	if err != nil {
		return nil, err
	}
	if err := proj.ValidateAgainst(reg); err != nil {
		return nil, err
	}
	settings, err := registry.ResolveSettings(cli, proj, reg)
	if err != nil {
		return nil, err
	}
	// Defense-in-depth: every tier validates >= 0 at load time; re-check the
	// resolved value here so a future tier can never smuggle a negative budget
	// into ApplyByteBudget (AC 06-03 Error Scenario 1).
	if err := payload.ValidateBudget(settings.PayloadByteBudget); err != nil {
		return nil, err
	}
	return &ReviewConfig{
		Registry: reg,
		Project:  proj,
		Settings: settings,
		PersonaDirs: registry.PersonaDirs{
			Project:  filepath.Join(root, ".atcr", "personas"),
			Registry: filepath.Join(filepath.Dir(regPath), "personas"),
		},
	}, nil
}

// PreparedReview is a scaffolded-but-not-yet-executed review: the review
// directory exists with its payload artifacts, manifest (Partial=false,
// finalized by ExecuteReview), and .atcr/latest pointer written, and the roster
// is assembled into runnable slots. It is the handoff between the two review
// phases so the MCP server can scaffold synchronously (returning the id/dir/
// agent-count to the client immediately) and run the fan-out in the background,
// while the CLI runs both phases inline. The fields the executor needs are
// exported; manifest is finalized in place by ExecuteReview.
type PreparedReview struct {
	ID          string
	Dir         string
	Slots       []Slot
	TimeoutSec  int
	MaxParallel int
	// Repo and Head locate the read-only snapshot the tool harness reads (Epic
	// 2.0). Set from the request; ExecuteReview builds the snapshot→jail→dispatcher
	// only when a slot is tool-enabled. An empty Head leaves the harness unwired,
	// so a tool agent degrades to single-shot.
	Repo     string
	Head     string
	manifest *payload.Manifest
}

// AgentCount is the number of reviewer slots the prepared review will run.
func (p *PreparedReview) AgentCount() int { return len(p.Slots) }

// PrepareReview runs phase one of a review: build per-mode payloads, assemble
// the roster into parallel/serial slots (with fallback chains), derive the
// review id, scaffold the review directory, and write the payload artifacts, an
// in-progress manifest, and the .atcr/latest pointer. No agent runs here, so it
// returns quickly; ExecuteReview performs the fan-out.
//
// An empty roster is rejected before scaffolding so a no-op run never creates a
// review directory or repoints .atcr/latest. (LoadReviewConfig also rejects
// this earlier; PrepareReview is defended for direct/MCP callers.)
func PrepareReview(ctx context.Context, cfg *ReviewConfig, req ReviewRequest) (*PreparedReview, error) {
	if len(rosterNames(cfg.Project)) == 0 {
		return nil, ErrEmptyRoster
	}
	if req.OutputDir != "" && req.IDOverride != "" {
		return nil, fmt.Errorf("--output-dir and --id are mutually exclusive")
	}
	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
	if err != nil {
		return nil, err
	}
	// Only a roster that resolved to payload modes can be "empty": a roster of
	// unknown agents builds zero modes and must keep its "not found in
	// registry" diagnostic from buildSlots below.
	empty := len(payloads) > 0
	for _, mp := range payloads {
		if mp.FileCount > 0 {
			empty = false
			break
		}
	}
	if empty {
		return nil, fmt.Errorf("%w: the range contains commits but no changed files (only merge or empty commits?); review a range that changes files", ErrNoReviewableContent)
	}
	slots, perAgentMode, err := buildSlots(cfg, payloads, req.Range)
	if err != nil {
		return nil, err
	}

	// Derive the id unconditionally: for --output-dir the id is provenance-only
	// (written to the manifest and PreparedReview.ID but not used for the path),
	// while for --id and the default derived case the id IS the path component.
	id, err := ReviewID(req.IDOverride, req.Branch, req.Date, req.TimeSuffix, nil)
	if err != nil {
		return nil, err
	}
	var dir string
	switch {
	case req.OutputDir != "":
		// --output-dir redirects the whole tree to an explicit path. The id is
		// still derived above (for provenance/output) but never used for the
		// path, and .atcr/latest is left untouched below.
		if err = validateOutputDirRoot(req.OutputDir, req.Root); err != nil {
			return nil, err
		}
		dir, err = ScaffoldOutputDir(req.OutputDir)
	case req.IDOverride != "":
		// Explicit overrides keep their exact id, but the scaffold is exclusive:
		// a pre-existing directory (e.g. a client retrying atcr_review with the
		// same id while the first run is in flight) is rejected rather than
		// scaffolded into, so two fan-outs never share one review dir.
		dir, err = ScaffoldReviewDir(req.Root, id)
	default:
		// Derived ids claim their directory atomically: creation is the
		// collision check, so two reviews of the same branch in the same second
		// get distinct dirs instead of interleaving writes in one.
		id, dir, err = claimReviewDir(req.Root, id, req.TimeSuffix)
	}
	if err != nil {
		return nil, err
	}
	if err := writePayloadArtifacts(dir, payloads); err != nil {
		return nil, err
	}

	m := &payload.Manifest{
		Base:            req.Range.Base,
		Head:            req.Range.Head,
		DetectionMode:   req.Range.DetectionMode,
		DefaultBranch:   req.Range.DefaultBranch,
		CommitCount:     req.Range.CommitCount,
		PayloadMode:     cfg.Settings.PayloadMode,
		MaxParallel:     cfg.Settings.MaxParallel,
		TimeoutSecs:     cfg.Settings.TimeoutSecs,
		PerAgentPayload: perAgentMode,
		Roster:          rosterNames(cfg.Project),
		StartedAt:       req.StartedAt,
		Partial:         false,              // finalized by ExecuteReview once outcomes are known
		Stages:          []string{"review"}, // 1.x runs only the review stage (Epic 1.1 reserved field)
	}
	if err := WriteManifest(dir, m); err != nil {
		return nil, err
	}
	// Point .atcr/latest at the review before fan-out so `atcr status` can find an
	// in-progress run started by the non-blocking MCP handler. Skipped for
	// --output-dir: the pointer tracks interactive runs under .atcr/reviews/, and
	// an external orchestrator owns (and already knows) its output path.
	if req.OutputDir == "" {
		if err := WriteLatest(req.Root, id); err != nil {
			return nil, err
		}
	}
	return &PreparedReview{ID: id, Dir: dir, Slots: slots, TimeoutSec: cfg.Settings.TimeoutSecs, MaxParallel: cfg.Settings.MaxParallel, Repo: req.Repo, Head: req.Range.Head, manifest: m}, nil
}

// runEngine wires the optional read-only tool harness for p's tool-enabled slots
// (a head snapshot → path jail → dispatcher, shared across the run, plus a
// per-agent transcript writer under poolDir), runs the fan-out under p's timeout,
// and returns the per-agent results together with the manifest review-stage entry
// (snapshot provenance already stamped). Best-effort harness setup: a snapshot or
// jail failure logs and degrades tool agents to single-shot rather than failing
// the review. Extracted from ExecuteReview so ExecuteResume runs the identical
// engine setup; the two differ only in how they persist the results.
func runEngine(ctx context.Context, completer Completer, p *PreparedReview, poolDir string) ([]Result, *payload.ReviewStage) {
	runCtx := ctx
	if p.TimeoutSec > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutSec)*time.Second)
		defer cancel()
	}

	// Snapshot provenance for the manifest review stage (AC 03-02 / 03-03). Zero
	// unless a snapshot actually runs and succeeds below.
	var snapMode, snapHeadSHA, snapWorktreePath string
	// Seed the engine with the review_id-correlated context logger so every agent
	// log line is greppable by review (AC9 + AC10). FromContext returns a never-nil
	// discard logger if none is set.
	opts := []EngineOption{WithMaxParallel(p.MaxParallel), WithLogger(log.FromContext(ctx))}
	if anyToolAgent(p.Slots) && p.Head != "" {
		if root, cleanup, err := tools.NewSnapshotManager(p.Repo).SnapshotFor(p.Head); err != nil {
			log.FromContext(ctx).Warn("tool harness disabled (snapshot); tool agents degrade to single-shot", "head", p.Head, "err", err)
			snapMode = "failed" // snapshot attempted but failed; distinguishable from no-snapshot-attempted
		} else {
			defer cleanup()
			// A successful SnapshotFor call fixes the mode/head/path the tool harness
			// reviewed at (AC 03-02 Scenario 5), recorded even if the jail below fails.
			// Resolve the head to a full SHA for the manifest even if the caller passed
			// a symbolic ref or short SHA (e.g., tests constructing PreparedReview directly).
			// A resolution failure is logged but does not abort the review; the original
			// value is preserved as a best-effort fallback.
			headSHA := p.Head
			if resolved, err := resolveHeadSHA(p.Repo, p.Head); err == nil {
				headSHA = resolved
			} else {
				log.FromContext(ctx).Warn("could not resolve head SHA for manifest", "err", err)
			}
			snapMode, snapHeadSHA, snapWorktreePath = snapshotManifestFields(root, p.Repo, headSHA)
			if jail, jerr := tools.NewJail(root); jerr != nil {
				log.FromContext(ctx).Warn("tool harness disabled (jail); tool agents degrade to single-shot", "err", jerr)
			} else {
				disp := tools.NewDispatcher(jail, tools.DefaultLimits())
				rawBase := filepath.Join(poolDir, poolRawAgentDir)
				opts = append(opts, WithDispatcher(disp), WithTranscript(func(agent string) *tools.Transcript {
					dir := filepath.Join(rawBase, transcriptAgentDir(agent))
					if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
						log.FromContext(ctx).Warn("transcript dir creation failed", "agent", agent, "err", mkErr)
					}
					return tools.OpenTranscript(filepath.Join(dir, "transcript.jsonl"), agent)
				}))
			}
		}
	}

	results := NewEngine(completer, opts...).Run(runCtx, p.Slots)

	// Classify the run into the manifest's review-stage entry and stamp the
	// snapshot provenance (nil when no agent ran with tools).
	stage := reviewStageFor(results)
	if stage != nil {
		stage.SnapshotMode = snapMode
		stage.HeadSHA = snapHeadSHA
		stage.SnapshotWorktreePath = snapWorktreePath
	}
	return results, stage
}

// ExecuteReview runs phase two: fan out the prepared roster under the global
// timeout, then write per-agent artifacts, the merged pool, summary.json, and
// the finalized manifest (Partial reflecting the outcome). The completer is
// injected so the CLI uses the real HTTP client and tests use a fake/httptest.
//
// Artifacts are always persisted, even when every agent fails; in that case the
// populated *ReviewResult is still returned alongside the wrapped
// ErrAllAgentsFailed so the caller can map it to exit 1 while the on-disk review
// remains for inspection. The background MCP path discards the error (status is
// read from disk) while the CLI maps it to the process exit code.
//
// Graceful-shutdown note: cooperative shutdown preserves agents that finished
// before the signal; in-flight agents share the cancelled parent ctx and are cut
// off (classified as timeout). Truly completing in-flight work would require
// running them on an uncancelled child ctx — a deliberate engine change out of
// scope here.
func ExecuteReview(ctx context.Context, completer Completer, p *PreparedReview) (*ReviewResult, error) {
	poolDir := filepath.Join(p.Dir, "sources", "pool")

	results, stage := runEngine(ctx, completer, p, poolDir)

	// Detect an external interrupt (SIGINT/SIGTERM cancelled the root context) so
	// the manifest can record it. The check is on the PARENT ctx, not runCtx: a
	// review timeout cancels only the child runCtx (DeadlineExceeded), while a
	// signal cancels the parent (Canceled). The engine has already collapsed both
	// into StatusTimeout per-agent, so the parent ctx is the only signal that still
	// distinguishes a user interrupt from an exhausted time budget.
	// Contract: callers must cancel the parent ctx only via a signal handler;
	// any other cancellation would be misreported as interrupted in the manifest.
	interrupted := errors.Is(ctx.Err(), context.Canceled)

	sum, err := WritePool(poolDir, results)
	if err != nil {
		// Persistence failed after the fan-out ran. Write a best-effort failure
		// marker so the status reader reports `failed` rather than leaving the
		// review stuck in_progress forever (Epic 1.5); if even this cannot be
		// written, stale inference covers it once the timeout elapses.
		writeFailureSummary(poolDir, results)
		// Stamp CompletedAt so the manifest is distinguishable from an unfinished
		// scaffold on disk; the failure-marker summary.json is the authoritative
		// outcome signal, but a zero CompletedAt left duration/partial-deriving
		// tools unable to tell a failed review from one still in flight.
		// Nil guard: PreparedReview may be constructed directly in tests without a manifest.
		if p.manifest != nil {
			p.manifest.CompletedAt = time.Now().UTC()
			p.manifest.Interrupted = interrupted
			_ = WriteManifest(p.Dir, p.manifest) // best-effort; stale inference covers the `failed` outcome but manifest.Interrupted is lost if this write also fails
		}
		return nil, err
	}

	// Finalize the manifest into a local copy. p.manifest is only updated on a
	// successful write so a caller that retries with the same PreparedReview does
	// not observe stale completion data from a previous failed attempt.
	m := *p.manifest
	m.Partial = sum.Partial
	m.CompletedAt = time.Now().UTC()
	m.Interrupted = interrupted
	// Record the review-stage entry listing the tool-using agents (Epic 2.0, AC
	// 05-04), with snapshot provenance already stamped by runEngine. nil when no
	// agent ran with tools, so a pure 1.x roster's manifest is unchanged.
	m.Review = stage
	if err := WriteManifest(p.Dir, &m); err != nil {
		return nil, err
	}
	p.manifest = &m

	res := &ReviewResult{ID: p.ID, Dir: p.Dir, Summary: sum}
	// The all-agents-failed gate runs after artifacts are on disk; the result is
	// returned regardless so the caller knows where to look.
	if _, outErr := Outcome(results); outErr != nil {
		return res, outErr
	}
	return res, nil
}

// RunReview is the full synchronous review flow used by the CLI: prepare the
// review directory then execute the fan-out inline. The completer is injected so
// the CLI uses the real HTTP client and tests use a fake/httptest.
//
// Artifacts are always persisted, even when every agent fails; in that case the
// populated *ReviewResult is still returned alongside the wrapped
// ErrAllAgentsFailed so the caller can map it to exit 1 while the on-disk review
// remains for inspection.
func RunReview(ctx context.Context, completer Completer, cfg *ReviewConfig, req ReviewRequest) (*ReviewResult, error) {
	p, err := PrepareReview(ctx, cfg, req)
	if err != nil {
		return nil, err
	}
	return ExecuteReview(ctx, completer, p)
}

// modePayload is one payload mode's built content shared by every agent using it.
type modePayload struct {
	Text       string
	FileCount  int
	Truncation payload.Truncation
}

// buildPayloads builds each distinct payload mode the roster uses exactly once.
func buildPayloads(ctx context.Context, cfg *ReviewConfig, repo, base, head string) (map[string]modePayload, error) {
	out := map[string]modePayload{}
	for _, mode := range neededModes(cfg) {
		entries, err := payload.BuildEntries(ctx, payload.PayloadMode(mode), repo, base, head)
		if err != nil {
			return nil, fmt.Errorf("building %s payload: %w", mode, err)
		}
		kept, trunc := payload.ApplyByteBudget(entries, cfg.Settings.PayloadByteBudget)
		if trunc.AllDropped {
			return nil, fmt.Errorf("%w (mode %s, dropped %d file(s))", ErrPayloadFullyDropped, mode, len(trunc.FilesDropped))
		}
		var b strings.Builder
		for _, e := range kept {
			b.WriteString(e.Body)
		}
		// FileCount reflects what the reviewer actually saw (post-truncation), not
		// the pre-budget total — the dropped files are recorded in trunc.
		out[mode] = modePayload{Text: b.String(), FileCount: len(kept), Truncation: trunc}
	}
	return out, nil
}

// neededModes returns the distinct payload modes across the whole roster.
func neededModes(cfg *ReviewConfig) []string {
	seen := map[string]bool{}
	var modes []string
	for _, name := range rosterNames(cfg.Project) {
		if ac, ok := cfg.Registry.Agents[name]; ok {
			m := ac.EffectivePayloadMode(cfg.Settings)
			if !seen[m] {
				seen[m] = true
				modes = append(modes, m)
			}
		}
	}
	return modes
}

// buildSlots assembles the roster into ordered slots (parallel lane first, then
// serial) and returns the per-agent payload-mode map for the manifest. A
// build-time failure (unknown agent/provider, persona resolution, prompt render)
// aborts the whole run fail-fast: these are configuration errors the user must
// fix, not transient per-agent outcomes, so there is nothing useful to preserve
// — unlike the all-agents-failed runtime path, which keeps artifacts on disk.
func buildSlots(cfg *ReviewConfig, payloads map[string]modePayload, rng ReviewRange) ([]Slot, map[string]string, error) {
	perAgentMode := map[string]string{}
	var slots []Slot

	add := func(name string, serial bool) error {
		primary, mode, err := buildAgent(cfg, name, payloads, rng)
		if err != nil {
			return err
		}
		perAgentMode[name] = mode

		var fbs []Agent
		seen := map[string]bool{name: true}
		for fb := cfg.Registry.Agents[name].Fallback; fb != ""; fb = cfg.Registry.Agents[fb].Fallback {
			if seen[fb] {
				break // registry validation guarantees acyclic; defensive stop
			}
			seen[fb] = true
			agent, err := buildFallbackAgent(cfg, primary, fb)
			if err != nil {
				return err
			}
			fbs = append(fbs, agent)
		}
		slots = append(slots, Slot{Primary: primary, Fallbacks: fbs, Serial: serial})
		return nil
	}

	for _, name := range cfg.Project.Agents {
		if err := add(name, false); err != nil {
			return nil, nil, err
		}
	}
	for _, name := range cfg.Project.SerialAgents {
		if err := add(name, true); err != nil {
			return nil, nil, err
		}
	}
	return slots, perAgentMode, nil
}

// defaultMaxTokens is the output-token cap applied to every reviewer call.
// Generous on purpose: reasoning/thinking models spend output budget on
// chain-of-thought before emitting visible content, so a tight cap makes them
// finish mid-reasoning and return an empty review (the doctor self-test warns of
// exactly this). The empty-content case is still caught by the reasoning_content
// fallback in llmclient; this headroom lets the clean Content path win first.
const defaultMaxTokens = 8192

// maxTokensPtr returns a fresh pointer to defaultMaxTokens for an Invocation
// (MaxTokens is a pointer so an explicit value always serializes).
func maxTokensPtr() *int { v := defaultMaxTokens; return &v }

// buildAgent resolves an agent's persona, renders its prompt against the payload
// it sees, and assembles the invocation. It returns the agent and its mode.
func buildAgent(cfg *ReviewConfig, name string, payloads map[string]modePayload, rng ReviewRange) (Agent, string, error) {
	ac, ok := cfg.Registry.Agents[name]
	if !ok {
		return Agent{}, "", fmt.Errorf("agent %q not found in registry", name)
	}
	mode := ac.EffectivePayloadMode(cfg.Settings)
	mp, ok := payloads[mode]
	if !ok {
		// Defensive: payloads is built by neededModes over the same roster, so a
		// miss means the two derivations diverged — fail loudly rather than
		// invoking the agent with an empty payload and a vacuous review.
		return Agent{}, "", fmt.Errorf("agent %q: no payload built for mode %q", name, mode)
	}

	persona, err := registry.ResolvePersona(name, ac.Persona, nil, cfg.PersonaDirs)
	if err != nil {
		return Agent{}, "", err
	}
	prompt, err := payload.RenderPrompt(persona.Text, payload.PayloadContext{
		AgentName:    name,
		BaseRef:      rng.Base,
		HeadRef:      rng.Head,
		PayloadMode:  mode,
		FileCount:    mp.FileCount,
		Payload:      mp.Text,
		ScopeRule:    payload.ScopeRule(payload.PayloadMode(mode)),
		ToolsEnabled: ac.Tools,
	})
	if err != nil {
		return Agent{}, "", fmt.Errorf("agent %q: %w", name, err)
	}
	// Soft per-agent scope focus (Epic 2.2): appended after the persona template
	// renders so it lands in every persona regardless of its template, and feeds
	// both Agent.Prompt and Invocation.Prompt below (a fallback reuses the
	// primary's prompt, so it inherits the focus too). No-op when scope is unset.
	prompt += payload.ScopeFocus(ac.Scope)
	prov, ok := cfg.Registry.Providers[ac.Provider]
	if !ok {
		return Agent{}, "", fmt.Errorf("agent %q references unknown provider %q", name, ac.Provider)
	}
	return Agent{
		Name:            name,
		Prompt:          prompt,
		PayloadMode:     mode,
		Truncation:      mp.Truncation,
		TimeoutSecs:     ac.EffectiveTimeoutSecs(cfg.Settings),
		Tools:           ac.Tools,
		SupportsFC:      ac.SupportsFC,
		MaxTurns:        derefMaxTurns(ac.MaxTurns),
		ToolBudgetBytes: derefInt64(ac.ToolBudgetBytes),
		MinSeverity:     ac.MinSeverity,
		MaxFindings:     ac.MaxFindings,
		Invocation: llmclient.Invocation{
			BaseURL:     prov.BaseURL,
			APIKeyEnv:   prov.APIKeyEnv,
			Model:       ac.Model,
			Temperature: ac.Temperature,
			MaxTokens:   maxTokensPtr(),
			Prompt:      prompt,
		},
	}, mode, nil
}

// derefMaxTurns resolves the agent's MaxTurns pointer to a value. Registry load
// applies the default (10) when tools=true and it was unset, so a tool agent
// arrives here with a non-nil pointer; a nil pointer (non-tool agent, or direct
// construction) yields 0, which the engine treats as "use the default".
func derefMaxTurns(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// derefInt64 resolves an optional int64 (e.g. ToolBudgetBytes) to its value, with
// nil meaning 0 (unlimited, matching the registry's documented escape hatch).
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// buildFallbackAgent builds a fallback that reviews the SAME persona prompt and
// payload as the primary (AC 01-04: "fallback agent tried (same persona)"), only
// the provider/model/temperature/timeout differ.
func buildFallbackAgent(cfg *ReviewConfig, primary Agent, name string) (Agent, error) {
	ac, ok := cfg.Registry.Agents[name]
	if !ok {
		return Agent{}, fmt.Errorf("fallback agent %q not found in registry", name)
	}
	prov, ok := cfg.Registry.Providers[ac.Provider]
	if !ok {
		return Agent{}, fmt.Errorf("fallback agent %q references unknown provider %q", name, ac.Provider)
	}
	// A fallback answers in the primary's place, so the primary's review
	// constraints (min_severity, max_findings, scope) govern — the fallback's
	// own are intentionally ignored (Epic 2.2). Surface that override so an
	// operator who set these on a fallback-only agent is not silently ignored.
	if ac.MinSeverity != "" || ac.MaxFindings != nil || len(ac.Scope) > 0 {
		fmt.Fprintf(os.Stderr, "warn: fallback agent %q sets its own min_severity/max_findings/scope; these are ignored — the primary lane's constraints govern\n", name)
	}
	return Agent{
		Name:        name,
		Prompt:      primary.Prompt,
		PayloadMode: primary.PayloadMode,
		Truncation:  primary.Truncation,
		TimeoutSecs: ac.EffectiveTimeoutSecs(cfg.Settings),
		// Fallbacks inherit the lane's effective tool settings from the primary,
		// not the fallback's own config (AC 01-05 S4, AC 04-03: "fallbacks inherit
		// the lane's effective tools setting"). Degrade stays per-agent — a
		// fallback whose model cannot do function calling degrades independently
		// (Phase 4), but the requested Tools/MaxTurns/ToolBudgetBytes are the lane's.
		Tools:           primary.Tools,
		MaxTurns:        primary.MaxTurns,
		ToolBudgetBytes: primary.ToolBudgetBytes,
		// SupportsFC is per-agent: the fallback uses its OWN model's capability,
		// NOT the primary's, so the degrade decision is re-evaluated per agent
		// (AC 04-03 EC3 — lane governs Tools, the model governs capability).
		SupportsFC: ac.SupportsFC,
		// Review constraints follow the slot, not the substitute model (Epic 2.2):
		// a fallback answers in the primary's place, so the primary's min_severity
		// and max_findings still govern the output.
		MinSeverity: primary.MinSeverity,
		MaxFindings: primary.MaxFindings,
		Invocation: llmclient.Invocation{
			BaseURL:     prov.BaseURL,
			APIKeyEnv:   prov.APIKeyEnv,
			Model:       ac.Model,
			Temperature: ac.Temperature,
			MaxTokens:   maxTokensPtr(),
			Prompt:      primary.Prompt,
		},
	}, nil
}

// writePayloadArtifacts persists each distinct payload under payload/<mode>.txt
// so the manifest's provenance is backed by what reviewers actually saw.
func writePayloadArtifacts(dir string, payloads map[string]modePayload) error {
	for mode, mp := range payloads {
		path := filepath.Join(dir, "payload", mode+".txt")
		if err := atomicWriteFile(path, []byte(mp.Text)); err != nil {
			return fmt.Errorf("writing payload %s: %w", mode, err)
		}
	}
	return nil
}

// anyToolAgent reports whether any primary slot requested tools, so ExecuteReview
// only pays the snapshot/jail cost when the harness is needed. Fallbacks always
// inherit the lane's effective Tools setting from the primary (AC 01-05 S4), so
// checking fallbacks cannot change the result; the loop is intentionally omitted.
func anyToolAgent(slots []Slot) bool {
	for _, s := range slots {
		if s.Primary.Tools {
			return true
		}
	}
	return false
}

// transcriptAgentDir maps an agent name to the same single-segment directory the
// pool artifacts use (raw/agent/<dir>), so transcript.jsonl lands beside the
// agent's status.json/review.md. An unusable name falls back to a safe constant
// rather than escaping the pool.
func transcriptAgentDir(agent string) string {
	dir, err := agentDirName(agent)
	if err != nil {
		return "transcript-unknown"
	}
	return dir
}

// reviewStageFor classifies fan-out results into the manifest's review-stage
// entry (AC 05-04). An agent is tools-enabled when it requested tools at
// invocation time (ToolsRequested) — preserved across the degrade, budget-trip,
// and provider-error paths, so membership reflects the configured intent, not
// the completion outcome. The degraded subset is the agents that fell back to
// single-shot. Returns nil when no agent ran with tools, so the manifest omits
// the review entry for a pure 1.x roster (Scenario 5).
func reviewStageFor(results []Result) *payload.ReviewStage {
	var enabled, degraded []string
	for _, r := range results {
		if !r.ToolsRequested {
			continue
		}
		enabled = append(enabled, r.Agent)
		if r.ToolsDegraded {
			degraded = append(degraded, r.Agent)
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	// Agents is a distinct copy of ToolsEnabled so the two slices never alias (a
	// later mutation of one must not silently mutate the other).
	return &payload.ReviewStage{Agents: append([]string(nil), enabled...), ToolsEnabled: enabled, ToolsDegraded: degraded}
}

// snapshotManifestFields derives the review-stage snapshot provenance (AC 03-02 /
// 03-03) from the root SnapshotFor returned. root and repo pointing at the same
// directory is the live fast path (head matched HEAD on a clean worktree), so mode
// is "live" and the worktree path is the explicit empty string; any other root is
// a detached worktree at head, so mode is "worktree" and the path is that root.
func snapshotManifestFields(root, repo, head string) (mode, headSHA, worktreePath string) {
	if samePath(root, repo) {
		return "live", head, ""
	}
	return "worktree", head, root
}

// samePath reports whether a and b refer to the same directory, normalizing
// trailing separators and relative vs absolute form so they do not spuriously
// force worktree mode.
func samePath(a, b string) bool {
	absA, err1 := filepath.Abs(a)
	absB, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return absA == absB
}

// resolveHeadSHA resolves a git ref to its full 40-byte SHA. It is a defensive
// guard for callers (including tests) that construct PreparedReview with an
// unresolved head; the production CLI/MCP path already resolves the head through
// gitrange.Resolve before fan-out.
func resolveHeadSHA(repo, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("empty ref")
	}
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// rosterNames returns the full roster (parallel lane then serial lane).
func rosterNames(p *registry.ProjectConfig) []string {
	names := make([]string, 0, len(p.Agents)+len(p.SerialAgents))
	names = append(names, p.Agents...)
	names = append(names, p.SerialAgents...)
	return names
}
