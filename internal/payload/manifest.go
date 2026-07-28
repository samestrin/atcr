package payload

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// Manifest is the per-review provenance record written to manifest.json so
// every downstream tool reads what reviewers saw from disk rather than
// re-deriving it. It records the resolved range, the default payload mode, the
// per-agent payload map (who saw what), the roster, timestamps, and the
// partial-success flag.
type Manifest struct {
	Base            string            `json:"base"`
	Head            string            `json:"head"`
	DetectionMode   string            `json:"detection_mode"`
	DefaultBranch   string            `json:"default_branch,omitempty"`
	CommitCount     int               `json:"commit_count"`
	PayloadMode     string            `json:"payload_mode"`
	PerAgentPayload map[string]string `json:"per_agent_payload"`
	Roster          []string          `json:"roster"`
	StartedAt       time.Time         `json:"started_at"`
	CompletedAt     time.Time         `json:"completed_at,omitempty"`
	Partial         bool              `json:"partial"`

	// NoIgnore records that the review ran with the .gitignore/.atcrignore payload
	// filter disabled (--no-ignore). It is persisted so a resume recovers the flag
	// from on-disk state rather than the resume request, locking pending agents to
	// the same filtering the completed agents saw — exactly as the range, roster,
	// and scope are locked. omitempty keeps a normal (filtered) run's manifest
	// byte-identical to the pre-field shape so older readers are unaffected.
	NoIgnore bool `json:"no_ignore,omitempty"`

	// Baseline is true when the review is a full-repository / directory scan
	// (`atcr review --all`/`--dir`, Sprint 35.0) rather than a git-range diff. It is
	// the durable marker the resume path keys on: a baseline review has an empty
	// Base/Head, so ValidateResumeRange must skip git-range validation and
	// PrepareResume must rebuild the payload from the tracked repository (the repo
	// walker) instead of a git range — otherwise a resumed baseline review would fail
	// ErrRangeChanged or review the wrong (diff) content. omitempty keeps a diff
	// review's manifest byte-identical to the pre-field shape so older readers are
	// unaffected.
	Baseline bool `json:"baseline,omitempty"`

	// Dir records the --dir <path> subtree scope of a baseline directory scan
	// (Sprint 35.0, Story 2): a slash-normalized, repo-root-relative directory (or
	// "" for a whole-repository --all scan). Like NoIgnore and Baseline it is
	// persisted so a resume recovers the scope from on-disk state rather than the
	// resume request, locking pending agents to the exact subtree the completed
	// agents reviewed — otherwise a resumed --dir scan would rebuild the whole-repo
	// payload and review out-of-scope files. omitempty keeps a whole-repo/diff
	// review's manifest byte-identical to the pre-field shape so older readers are
	// unaffected.
	Dir string `json:"dir,omitempty"`

	// Interrupted is true when the fan-out was cut short by an external signal
	// (SIGINT/SIGTERM cancelling the root context), as opposed to running to
	// completion or hitting its own timeout (epic 4.1). It is the durable marker
	// that ReadReviewStatus derives the "interrupted" run state from, so partial
	// results from a Ctrl-C are distinguishable from a clean completion. omitempty
	// keeps a never-interrupted manifest byte-identical to the pre-4.1 shape and
	// lets older readers ignore it.
	Interrupted bool `json:"interrupted,omitempty"`

	// Stages records which review stages ran. Reserved for the agentic stages
	// (Epics 3.0–5.0): 1.x records ["review"]; later runs append "verify",
	// "debate", etc. Optional so a manifest written without it parses cleanly.
	Stages []string `json:"stages,omitempty"`

	// PerFilePayload records the payload mode each file was ACTUALLY rendered in
	// (Epic 35.1), keyed by repo-relative head path. It is written only for files
	// the escalation heuristic promoted above the run's configured mode, so a
	// review where nothing escalated omits the field entirely and the manifest is
	// byte-identical to what earlier versions produced. PayloadMode and
	// PerAgentPayload still record what was CONFIGURED; this records what a
	// reviewer saw.
	PerFilePayload map[string]string `json:"per_file_payload,omitempty"`

	// EscalationDegraded is true ONLY when a git-range review's change set exceeded
	// the escalation file cap, so the per-file escalation and skeleton passes were
	// skipped wholesale. Its ABSENCE (false/omitted) carries no information: it
	// reads identically whether nothing was complex enough to escalate, the feature
	// was disabled with max_files: 0 (builder.go guards degradation on MaxFiles > 0),
	// there was no RangeBuilder (the --all/--dir baseline and --diff-file paths force
	// it false), or a files-only roster never ran the pass at all. Do NOT read a
	// false/absent value as "escalation ran and found nothing to promote".
	EscalationDegraded bool `json:"escalation_degraded,omitempty"`

	// Review is the enriched record of the review stage's tool-using agents
	// (Epic 2.0, AC 05-04). It is a sibling of Stages (which stays the ordered
	// stage-name list, unchanged from 1.x) rather than nested inside it, because
	// Stages is a string array and reshaping it would break the 1.x on-disk
	// contract. Present only when at least one agent ran with tools enabled, so a
	// pure 1.x roster omits it entirely and older readers are unaffected.
	Review *ReviewStage `json:"review,omitempty"`

	// MaxParallel and TimeoutSecs are the effective fan-out settings recorded
	// for post-hoc diagnosis: a throttled run can be identified by max_parallel
	// in the manifest without replaying the registry precedence chain.
	// MaxParallel has no omitempty so 0 (explicitly unbounded) serializes and is
	// distinguishable from an older manifest that never carried the field.
	MaxParallel int `json:"max_parallel"`
	TimeoutSecs int `json:"timeout_secs,omitempty"`
}

// ReviewStage records which agents executed the review stage with tools enabled
// (Epic 2.0). ToolsEnabled lists every agent that had tools:true effective at
// invocation time — including agents that later degraded to single-shot, tripped
// a budget, or hit a provider error — because membership is derived from the
// invocation-time flag, not the completion path (AC 05-04). ToolsDegraded is the
// subset that fell back to single-shot. Agents mirrors ToolsEnabled (the agents
// participating in this stage). Slices marshal as [] (never null) so a present
// review entry always has explicit arrays.
type ReviewStage struct {
	Agents        []string `json:"agents"`
	ToolsEnabled  []string `json:"tools_enabled"`
	ToolsDegraded []string `json:"tools_degraded"`

	// SnapshotMode records the filesystem snapshot the tool harness reviewed at:
	// "live" when head matched HEAD on a clean worktree (fast path), "worktree"
	// when a detached git worktree was created (AC 03-02 / 03-03), or "failed"
	// when the snapshot was attempted but could not complete (agents degraded to
	// single-shot). Omitted (via omitempty) when no snapshot was attempted —
	// i.e. no tool agents or an empty Head.
	SnapshotMode string `json:"snapshot_mode,omitempty"`
	// HeadSHA is the resolved head commit the snapshot was taken at (AC 03-02
	// Scenario 5). Omitted when no snapshot ran.
	HeadSHA string `json:"head_sha,omitempty"`
	// SnapshotWorktreePath is the temporary worktree path on the slow path, or
	// the explicit empty string on the live fast path. It is intentionally NOT
	// omitempty: AC 03-03 Scenario 5 requires "snapshot_worktree_path": "" to be
	// present in live mode so a reader distinguishes live from a missing field.
	SnapshotWorktreePath string `json:"snapshot_worktree_path"`
}

// WriteManifest serializes m to path as indented JSON, writing atomically
// (temp file + rename) so a crash never leaves a half-written manifest. A
// write failure is surfaced with the AC-mandated message (06-03 Error
// Scenario 2). A nil PerAgentPayload is normalized to {} so the field
// marshals as an object, never null.
func WriteManifest(path string, m *Manifest) error {
	if m.PerAgentPayload == nil {
		m.PerAgentPayload = map[string]string{}
	}
	if m.Stages == nil {
		m.Stages = []string{"review"}
	}
	if m.Review != nil {
		// Normalize the review-stage slices so they serialize as [] (never null),
		// keeping the tools_enabled/tools_degraded arrays explicit for readers.
		if m.Review.Agents == nil {
			m.Review.Agents = []string{}
		}
		if m.Review.ToolsEnabled == nil {
			m.Review.ToolsEnabled = []string{}
		}
		if m.Review.ToolsDegraded == nil {
			m.Review.ToolsDegraded = []string{}
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to write manifest.json: %w", err)
	}
	if err := atomicfs.WriteFileAtomic(path, append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write manifest.json: %w", err)
	}
	return nil
}
