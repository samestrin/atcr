package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	goaxi "github.com/samestrin/go-axi"
	"github.com/spf13/cobra"
	toon "github.com/toon-format/toon-go"

	"github.com/samestrin/atcr/internal/localdebt"
)

// Mode thresholds, in est_minutes, as `llm-support td-filter` defines them:
// quick-wins is est < 30, backlog is 30 <= est < 2880, all is unbounded.
const (
	filterModeQuickWins = "quick-wins"
	filterModeBacklog   = "backlog"
	filterModeAll       = "all"

	filterQuickWinsCeil = 30
	filterBacklogCeil   = 2880
)

// debtFilterSummary is the `summary` half of the {items, summary} document. Its
// keys are td-filter's, minus the group and attempts counters (the store carries
// neither); total_unchecked is named total here because "unchecked" is README
// vocabulary — it counts the records in the chosen --status.
type debtFilterSummary struct {
	Total                int    `json:"total"`
	Matched              int    `json:"matched"`
	Max                  int    `json:"max"`
	Mode                 string `json:"mode"`
	ExcludedBySeverity   int    `json:"excluded_by_severity"`
	ExcludedByConfidence int    `json:"excluded_by_confidence"`
}

type debtFilterDoc struct {
	Items   []localdebt.Record `json:"items"`
	Summary debtFilterSummary  `json:"summary"`
}

// newDebtFilterCmd is the deterministic selection step td-filter was: status,
// mode, severity, confidence, then the --max cap. It is a command of its own, not
// flags on `debt list`, so list's defaults (no cap, a table) stay untouched; it
// shares debtFilter and selectDebtForList with it.
func newDebtFilterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filter",
		Short: "Select technical-debt items by severity, confidence, status and mode; emits {items, summary}",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runDebtFilter,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("severity", "", "comma list of severities to keep (CRITICAL|HIGH|MEDIUM|LOW; empty = all)")
	cmd.Flags().String("confidence", "", "comma list of confidences to keep (low|medium|high; empty = all)")
	cmd.Flags().String("status", statusOpen, "status to select (exact: "+debtListStatusList()+")")
	cmd.Flags().String("mode", filterModeQuickWins, "quick-wins (est < 30) | backlog (30 <= est < 2880) | all")
	cmd.Flags().Int("max", 10, "cap on how many items are returned, most severe first (0 = no cap)")
	cmd.Flags().Bool("json", false, "emit {items, summary} as JSON (the default output)")
	// The root PersistentPreRunE propagates this into the command context, as it
	// does for `atcr review`; there is no legacy pipe encoder for this command.
	cmd.Flags().Bool("axi", false, "emit {items, summary} as a token-dense TOON payload for agent consumption")
	// Registered because every --axi command must carry it (TestAxiImpliesLegacyPipeFlag),
	// but rejected: the pipe encoder is frozen and deprecated, and this command is new.
	cmd.Flags().Bool("legacy-pipe", false, "not supported by this command; rejected")
	_ = cmd.Flags().MarkHidden("legacy-pipe")
	cmd.Annotations = map[string]string{noLegacyPipeAnnotation: "true"}
	return cmd
}

// noLegacyPipeAnnotation marks a command whose --axi has no legacy pipe encoding,
// so the root does not announce a deprecated encoder that will never run.
const noLegacyPipeAnnotation = "atcr.noLegacyPipe"

func runDebtFilter(cmd *cobra.Command, _ []string) error {
	if legacy, _ := cmd.Flags().GetBool("legacy-pipe"); legacy {
		return usageError(fmt.Errorf("--legacy-pipe is not supported by debt filter; --axi emits standard TOON"))
	}
	mode := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "mode")))
	var floor, ceil int
	switch mode {
	case filterModeQuickWins:
		ceil = filterQuickWinsCeil
	case filterModeBacklog:
		floor, ceil = filterQuickWinsCeil, filterBacklogCeil
	case filterModeAll:
	default:
		return usageError(fmt.Errorf("invalid --mode %q: expected quick-wins|backlog|all", mustFlag(cmd, "mode")))
	}
	for _, s := range splitFilterList(mustFlag(cmd, "severity")) {
		if !resolveSeverities[strings.ToUpper(s)] {
			return usageError(fmt.Errorf("invalid --severity %q: expected CRITICAL|HIGH|MEDIUM|LOW", s))
		}
	}
	for _, c := range splitFilterList(mustFlag(cmd, "confidence")) {
		if l := strings.ToLower(c); l != "low" && l != "medium" && l != "high" {
			return usageError(fmt.Errorf("invalid --confidence %q: expected low|medium|high", c))
		}
	}
	if st := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status"))); !debtListStatuses[st] {
		return usageError(fmt.Errorf("invalid --status %q: expected %s", mustFlag(cmd, "status"), debtListStatusList()))
	}
	limit, _ := cmd.Flags().GetInt("max")
	if limit < 0 {
		return usageError(fmt.Errorf("invalid --max %d: expected a non-negative number", limit))
	}

	f := debtFilter{
		Severity:   mustFlag(cmd, "severity"),
		Status:     mustFlag(cmd, "status"),
		Confidence: mustFlag(cmd, "confidence"),
		EstFloor:   floor,
		EstCeil:    ceil,
	}
	summary, err := summarizeDebtFilter(cmd, f)
	if err != nil {
		return err
	}
	summary.Max, summary.Mode = limit, mode

	recs, err := selectDebtForList(cmd, f, sortKeySeverity, limit)
	if err != nil {
		return err
	}
	if recs == nil {
		recs = []localdebt.Record{}
	}
	doc := debtFilterDoc{Items: recs, Summary: summary}

	out := cmd.OutOrStdout()
	if axiFromContext(cmd.Context()) {
		return renderDebtFilterAXI(out, doc)
	}
	return renderDebtFilterJSON(out, doc)
}

func splitFilterList(s string) []string {
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// summarizeDebtFilter counts the filter pipeline stage by stage, as td-filter's
// summary does: total (the status scope), then rows the mode drops, then rows the
// severity filter drops, then rows the confidence filter drops; matched is what
// survives all of them, before the --max cap.
func summarizeDebtFilter(cmd *cobra.Command, f debtFilter) (debtFilterSummary, error) {
	sums, err := localdebt.ReadSummaries(debtStoreDir(cmd), localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return debtFilterSummary{}, fmt.Errorf("read local debt store: %w", err)
	}
	scope := debtFilter{Status: f.Status}
	inMode := debtFilter{Status: f.Status, EstFloor: f.EstFloor, EstCeil: f.EstCeil}
	inSev := inMode
	inSev.Severity = f.Severity
	var s debtFilterSummary
	for _, sum := range localdebt.FoldSummaries(sums) {
		v := viewOfSummary(sum)
		if !scope.matchView(v) {
			continue
		}
		s.Total++
		if !inMode.matchView(v) {
			continue
		}
		if !inSev.matchView(v) {
			s.ExcludedBySeverity++
			continue
		}
		if !f.matchView(v) {
			s.ExcludedByConfidence++
			continue
		}
		s.Matched++
	}
	return s, nil
}

func renderDebtFilterJSON(w io.Writer, doc debtFilterDoc) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// renderDebtFilterAXI emits the same {items, summary} as standard TOON: items as
// a tabular array of the selection fields, summary as an object. Text cells are
// sanitized as every other --axi payload's are.
func renderDebtFilterAXI(w io.Writer, doc debtFilterDoc) error {
	rows := make([]toon.Object, 0, len(doc.Items))
	for _, r := range doc.Items {
		rows = append(rows, toon.NewObject(
			toon.Field{Key: "id", Value: goaxi.SanitizeString(r.ID)},
			toon.Field{Key: "severity", Value: goaxi.SanitizeString(r.Severity)},
			toon.Field{Key: "confidence", Value: goaxi.SanitizeString(r.Confidence)},
			toon.Field{Key: "status", Value: debtStatusBucket(r.Status)},
			toon.Field{Key: "file", Value: goaxi.SanitizeString(r.File)},
			toon.Field{Key: "line", Value: r.Line},
			toon.Field{Key: "category", Value: goaxi.SanitizeString(r.Category)},
			toon.Field{Key: "est_minutes", Value: r.EstMinutes},
			toon.Field{Key: "problem", Value: goaxi.SanitizeString(r.Problem)},
			toon.Field{Key: "fix", Value: goaxi.SanitizeString(r.Fix)},
		))
	}
	s := doc.Summary
	summary := toon.NewObject(
		toon.Field{Key: "total", Value: s.Total},
		toon.Field{Key: "matched", Value: s.Matched},
		toon.Field{Key: "max", Value: s.Max},
		toon.Field{Key: "mode", Value: s.Mode},
		toon.Field{Key: "excluded_by_severity", Value: s.ExcludedBySeverity},
		toon.Field{Key: "excluded_by_confidence", Value: s.ExcludedByConfidence},
	)
	payload := toon.NewObject(
		toon.Field{Key: "items", Value: rows},
		toon.Field{Key: "summary", Value: summary},
	)
	if _, err := goaxi.EncodeChecked(w, payload); err != nil {
		return fmt.Errorf("axi encoder: %w", err)
	}
	return nil
}
