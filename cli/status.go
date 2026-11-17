package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/spf13/cobra"

	goaxi "github.com/samestrin/go-axi"
	toon "github.com/toon-format/toon-go"
)

// newStatusCmd builds `atcr status [id-or-path]`: print a review's progress as
// JSON so the Skill's orchestration loop, or a widget, can poll it. Defaults to
// .atcr/latest. The same engine reader backs the atcr_status MCP tool, and the
// command prints its ReviewStatus whole: the six run-level fields plus
// current_stage, completed_agents and the per-persona agents rows
// (docs/status.md). Under --axi the same fields print as one TOON object plus
// the suggestStatus help[] block instead (Epic 104.1 T4).
func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [id-or-path]",
		Short: "Print a review's progress as JSON (counts, stage, per-persona rows)",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE:  runStatus,
	}
	// The root PersistentPreRunE propagates this into the command context; there
	// is no legacy pipe encoder for this command.
	cmd.Flags().Bool("axi", false, "emit the review status and next steps as a token-dense TOON payload on stdout for agent consumption")
	// Registered because every --axi command must carry it (TestAxiImpliesLegacyPipeFlag),
	// but rejected: the pipe encoder is frozen and deprecated.
	cmd.Flags().Bool("legacy-pipe", false, "not supported by this command; rejected")
	_ = cmd.Flags().MarkHidden("legacy-pipe")
	cmd.Annotations = map[string]string{noLegacyPipeAnnotation: "true"}
	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	if legacy, _ := cmd.Flags().GetBool("legacy-pipe"); legacy {
		return usageError(errors.New("--legacy-pipe is not supported by status; --axi emits standard TOON"))
	}
	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	dir, err := anchorDir(arg)
	if err != nil {
		return usageError(err) // no review specified and no latest pointer
	}

	st, err := fanout.ReadReviewStatus(dir, filepath.Base(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return usageError(fmt.Errorf("review not found: %s", filepath.Base(dir)))
		}
		return usageError(err) // corrupt manifest, etc.
	}

	inv := invocation{
		Flags:   invocationFlags(cmd),
		Pending: st.AgentsPending > 0,
	}
	// Under --axi the same ReviewStatus fields print as one TOON object followed
	// by the suggestStatus help[] block on stdout (Epic 104.1 T4), through the
	// shared T1 emitter. No Next: line is written anywhere.
	if axiFromContext(cmd.Context()) {
		return writeAXIHelp(cmd.OutOrStdout(), statusAXIPayload(st), renderSuggestions(suggestStatus, inv))
	}

	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding status: %w", err)
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(out)); err != nil {
		return fmt.Errorf("writing status: %w", err)
	}
	// stdout is the JSON document alone, so the next steps go to stderr for a
	// human or an agent reading both streams (Epic 42.0, AC1).
	writeNextSteps(cmd.ErrOrStderr(), suggestStatus, inv)
	return nil
}

// statusAXIPayload encodes the existing ReviewStatus fields as one TOON object,
// mirroring the JSON document's field names and omitempty rules: current_stage,
// completed_agents and agents appear only when non-empty, and a row's
// last_activity_at only when the persona has written. Text cells are sanitized
// as every other --axi payload's are.
func statusAXIPayload(st *fanout.ReviewStatus) toon.Object {
	fields := []toon.Field{
		{Key: "review_id", Value: goaxi.SanitizeString(st.ReviewID)},
		{Key: "status", Value: goaxi.SanitizeString(st.Status)},
		{Key: "agent_count", Value: st.AgentCount},
		{Key: "agents_done", Value: st.AgentsDone},
		{Key: "agents_pending", Value: st.AgentsPending},
		{Key: "partial", Value: st.Partial},
	}
	if st.CurrentStage != "" {
		fields = append(fields, toon.Field{Key: "current_stage", Value: goaxi.SanitizeString(st.CurrentStage)})
	}
	if len(st.CompletedAgents) > 0 {
		names := make([]string, 0, len(st.CompletedAgents))
		for _, n := range st.CompletedAgents {
			names = append(names, goaxi.SanitizeString(n))
		}
		fields = append(fields, toon.Field{Key: "completed_agents", Value: names})
	}
	if len(st.Agents) > 0 {
		rows := make([]toon.Object, 0, len(st.Agents))
		for _, a := range st.Agents {
			row := []toon.Field{
				{Key: "name", Value: goaxi.SanitizeString(a.Name)},
				{Key: "state", Value: goaxi.SanitizeString(a.State)},
				{Key: "chunks_done", Value: a.ChunksDone},
				{Key: "chunks_total", Value: a.ChunksTotal},
			}
			if a.LastActivityAt != "" {
				row = append(row, toon.Field{Key: "last_activity_at", Value: goaxi.SanitizeString(a.LastActivityAt)})
			}
			rows = append(rows, toon.NewObject(row...))
		}
		fields = append(fields, toon.Field{Key: "agents", Value: rows})
	}
	return toon.NewObject(fields...)
}
