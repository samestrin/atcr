package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

// feedbackKindList renders the accepted --kind values for help and error text.
var feedbackKindList = strings.Join([]string{
	localdebt.FeedbackWrongMerge, localdebt.FeedbackWrongSplit, localdebt.FeedbackWrongCategory,
}, "|")

// newDebtReclusterCmd builds `atcr debt recluster <id>` (Epic 36.1): it records a
// developer's verdict on the reconciler's decision behind a finding — a wrong
// merge, a wrong split, or a wrong category — as a localdebt.ReconcilerFeedback
// annotation. It is deliberately a sibling of `debt resolve`, not a --status
// value there: a status is the verdict on the finding, which the quality signal
// reads as reviewer ground truth, while this is a verdict on the clustering and
// must leave the finding's status alone.
func newDebtReclusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recluster <id>",
		Short: "Record feedback on how reconcile grouped a finding",
		Long: "atcr debt recluster records that the reconciler grouped a finding wrongly:\n" +
			"  wrong-merge     it merged distinct issues into this finding\n" +
			"  wrong-split     it split one issue, so this finding and another should be one;\n" +
			"                  name the other with --with <finding-id>\n" +
			"  wrong-category  it filed this finding under the wrong category\n" +
			"The feedback is appended to the local store as an annotation on the finding\n" +
			"(the id is the leading column of `atcr debt list`). It does not change the\n" +
			"finding's status: close or dismiss the finding itself with `atcr debt resolve`.",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: runDebtRecluster,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("kind", "", "what the reconciler got wrong ("+feedbackKindList+")")
	cmd.Flags().String("with", "", "the partner finding's id; only with --kind "+localdebt.FeedbackWrongSplit)
	cmd.Flags().String("reason", "", "why the grouping is wrong (required)")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err) })
	return cmd
}

func runDebtRecluster(cmd *cobra.Command, args []string) error {
	dir := debtStoreDir(cmd)
	id := strings.TrimSpace(args[0])
	if id == "" {
		return usageError(fmt.Errorf("atcr debt recluster requires a non-empty id"))
	}
	kind := mustFlag(cmd, "kind")
	if !localdebt.IsKnownFeedbackKind(kind) {
		return usageError(fmt.Errorf("invalid --kind %q: expected %s", kind, feedbackKindList))
	}
	with := mustFlag(cmd, "with")
	if cmd.Flags().Changed("with") {
		switch {
		case !localdebt.FeedbackKindTakesPartner(kind):
			return usageError(fmt.Errorf("--with names a partner finding and applies only to --kind %s, not %s",
				localdebt.FeedbackWrongSplit, kind))
		case !localdebt.IsWellFormedFindingID(with):
			return usageError(fmt.Errorf("invalid --with %q: expected a finding id (16 lowercase hex characters)", with))
		case with == id:
			return usageError(fmt.Errorf("--with %q names the finding itself", with))
		}
	}
	reason := strings.TrimSpace(mustFlag(cmd, "reason"))
	if reason == "" {
		return usageError(fmt.Errorf("atcr debt recluster requires --reason <why>"))
	}
	if len(reason) > maxReasonBytes {
		return usageError(fmt.Errorf("--reason too long: %d bytes exceeds the %d-byte limit", len(reason), maxReasonBytes))
	}

	ids := []string{id}
	if with != "" {
		ids = append(ids, with)
	}
	recs, err := collectDebtIDRecords(dir, ids, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt recluster: failed to read local debt store: %w", err)
	}
	var effective *localdebt.Record
	var partnerFound bool
	for _, f := range localdebt.FoldRecords(recs) {
		switch f.ID {
		case id:
			r := f
			effective = &r
		case with:
			partnerFound = true
		}
	}
	if effective == nil {
		return usageError(fmt.Errorf("no technical-debt item with id %q in the local store", id))
	}
	if with != "" && !partnerFound {
		return usageError(fmt.Errorf("no technical-debt item with --with id %q in the local store", with))
	}

	// The annotated record is a copy of the finding's EFFECTIVE record, status and
	// justification untouched, so the fold still reports the same status for the
	// id. It is a marker, not an occurrence: the copied aggregate counters live on
	// the effective record alone (the rule buildDebtResolution follows), and
	// localdebt does not count an annotated record as a sighting.
	now := time.Now().UTC().Format(time.RFC3339)
	rec := *effective
	rec.Timestamp = now
	rec.RunID = now + "-recluster"
	rec.Occurrences = 0
	rec.FirstSeen = ""
	rec.CountedThrough = ""
	rec.ReconcilerFeedback = &localdebt.ReconcilerFeedback{Kind: kind, With: with, Reason: reason}

	// Bound the encoded record the way appendDebtResolution does: a line over the
	// read cap would be written and then skipped on every read.
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("atcr debt recluster: failed to encode feedback: %w", err)
	}
	if len(line)+1 > localdebt.MaxRecordBytes {
		return usageError(fmt.Errorf(
			"feedback record is too large to store: %d bytes encoded, cap is %d (the store skips longer lines on read)",
			len(line)+1, localdebt.MaxRecordBytes))
	}
	if err := localdebt.Append(dir, rec); err != nil {
		return fmt.Errorf("atcr debt recluster: failed to record feedback: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Recorded %s feedback on %s.\n", kind, id)
	return nil
}
