package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/spf13/cobra"
)

// Fit verdicts `atcr benchmark fit` prints, one per (persona, model) pair.
const (
	fitVerdictFit   = "fit"
	fitVerdictWarn  = "fit (warning)"
	fitVerdictUnfit = "unfit"
)

// newBenchmarkFitCmd builds `atcr benchmark fit --in <run-result.json>`: read the
// run-result's reviewer_fit rows (`benchmark run --replicates N`) and print one
// health verdict per (persona, model) pair (Epic 35.16.11.2.2.8). Read-only, and
// advisory: the operator decides whether to repoint, so the exit code does not
// depend on the verdicts.
func newBenchmarkFitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fit",
		Short: "Judge each persona + model pair in a benchmark run-result on call health",
		Long: "Read a benchmark run-result and print one row per (persona, model) pair: its\n" +
			"calls, its truncated, unparseable, failed and timed-out calls, its silent\n" +
			"chunks, its findings and output tokens per replicate, and a verdict.\n\n" +
			"The verdict is health-only; findings are reported, never gated. A pair is\n" +
			"unfit when any call was truncated, unparseable or failed (a timeout is a\n" +
			"failure), or was silent on every chunk it reviewed. A silent chunk beside a\n" +
			"productive one is a warning, and the pair stays fit.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runBenchmarkFit,
	}
	cmd.Flags().String("in", "", "path to a benchmark run-result JSON file (produced by atcr benchmark run, ideally with --replicates)")
	_ = cmd.MarkFlagRequired("in")
	return cmd
}

func runBenchmarkFit(cmd *cobra.Command, _ []string) error {
	// Cobra GetString error is unreachable: "in" is registered above and
	// MarkFlagRequired. Project-wide convention (27 sites).
	in, _ := cmd.Flags().GetString("in")

	data, err := readRunResultLimited(in)
	if err != nil {
		return err
	}
	var rr benchmark.RunResult
	if err := json.Unmarshal(data, &rr); err != nil {
		return fmt.Errorf("parsing run-result %s: %w", in, err)
	}
	if len(rr.Fit) == 0 {
		return fmt.Errorf("run-result %s has no reviewer_fit rows: it was written by an atcr without `benchmark run --replicates`, or by a full replay of a checkpoint that predates it; re-run the suite to judge fit", in)
	}
	rows, err := buildFitReport(rr.Fit)
	if err != nil {
		return fmt.Errorf("run-result %s: %w", in, err)
	}
	return writeFitReport(cmd, rows)
}

// fitPairRow is one (persona, model) pair of the fit report.
type fitPairRow struct {
	Persona, Model string
	Verdict        string
	Reasons        []string

	Calls       int
	Truncated   int
	Unparseable int
	// Failed counts failed calls, timed-out ones included; TimedOut is the subset
	// that hit the timeout.
	Failed   int
	TimedOut int
	// WhollySilent counts calls silent on every chunk they reviewed.
	WhollySilent int
	SilentChunks int
	Chunks       int

	// FindingsByReplicate and TokensOutByReplicate are summed over the suite's
	// cases, indexed by replicate number - 1. ObservedReplicates marks the
	// replicates the pair has a row for: a legacy checkpoint entry resumed with
	// --replicates writes none for replicate 1, and its 0 was never measured.
	FindingsByReplicate  []int
	TokensOutByReplicate []int
	ObservedReplicates   []bool
}

// buildFitReport groups reviewer_fit rows by (persona, model) and judges each pair.
// A row whose outcome is outside the vocabulary fails the report rather than being
// read as healthy: it was written by a newer atcr, and judging a call this binary
// cannot classify would print a verdict nothing supports. So does a row no run
// could have written (see checkFitRow): the file may be hand-edited.
func buildFitReport(fits []benchmark.ReviewerFit) ([]fitPairRow, error) {
	type key struct{ persona, model string }
	byPair := map[key]*fitPairRow{}
	for _, f := range fits {
		if err := checkFitRow(f, len(fits)); err != nil {
			return nil, err
		}
		k := key{f.Persona, f.Model}
		row := byPair[k]
		if row == nil {
			row = &fitPairRow{Persona: f.Persona, Model: f.Model}
			byPair[k] = row
		}
		row.add(f)
	}

	out := make([]fitPairRow, 0, len(byPair))
	for _, row := range byPair {
		row.judge()
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Persona != out[j].Persona {
			return out[i].Persona < out[j].Persona
		}
		return out[i].Model < out[j].Model
	})
	return out, nil
}

// checkFitRow rejects a reviewer_fit row the report cannot judge. maxReplicate is
// the row count: a pair cannot hold more distinct replicates than there are rows,
// and add sizes its per-replicate accumulators by the replicate number.
func checkFitRow(f benchmark.ReviewerFit, maxReplicate int) error {
	for _, id := range []struct{ name, value string }{{"persona", f.Persona}, {"model", f.Model}} {
		// The identity is printed as a report column, where stripping the rune
		// would show two different identities as one.
		if r, bad := firstNonPrintingRune(id.value); bad {
			return fmt.Errorf("reviewer_fit row has %s %q, which contains a non-printing rune (U+%04X)", id.name, id.value, r)
		}
	}
	row := fmt.Sprintf("reviewer_fit row for %q on %q", f.Persona, stripTerminalControlRunes(f.CaseID))
	if !benchmark.ValidOutcome(f.Outcome) {
		return fmt.Errorf("%s has outcome %q, which this atcr does not know", row, stripTerminalControlRunes(f.Outcome))
	}
	if f.Replicate < 1 {
		return fmt.Errorf("%s has replicate %d; replicates are numbered from 1", row, f.Replicate)
	}
	if f.Replicate > maxReplicate {
		return fmt.Errorf("%s has replicate %d, above the %d rows in the run-result", row, f.Replicate, maxReplicate)
	}
	for _, n := range []struct {
		name  string
		value int
	}{{"findings", f.Findings}, {"tokens_out", f.TokensOut}, {"chunk_count", f.ChunkCount}, {"silent_chunks", f.SilentChunks}} {
		if n.value < 0 {
			return fmt.Errorf("%s has %s %d; counts cannot be negative", row, n.name, n.value)
		}
	}
	// A row with no chunk count reads as one chunk (see add).
	if f.SilentChunks > max(f.ChunkCount, 1) {
		return fmt.Errorf("%s has silent_chunks %d above chunk_count %d", row, f.SilentChunks, f.ChunkCount)
	}
	return nil
}

func (r *fitPairRow) add(f benchmark.ReviewerFit) {
	r.Calls++
	switch {
	case f.TimedOut:
		r.Failed++
		r.TimedOut++
	case f.Outcome == benchmark.OutcomeFailed:
		r.Failed++
	case f.Outcome == benchmark.OutcomeTruncated:
		r.Truncated++
	case f.Outcome == benchmark.OutcomeUnparseable:
		r.Unparseable++
	}
	// An unchunked reviewer reviews one chunk; a row that recorded no chunk count
	// (a failed call writes no status) is read the same way, so a silent chunk on
	// it is never "a minority".
	chunks := max(f.ChunkCount, 1)
	r.Chunks += chunks
	r.SilentChunks += f.SilentChunks
	if f.SilentChunks >= chunks {
		r.WhollySilent++
	}
	for len(r.FindingsByReplicate) < f.Replicate {
		r.FindingsByReplicate = append(r.FindingsByReplicate, 0)
		r.TokensOutByReplicate = append(r.TokensOutByReplicate, 0)
		r.ObservedReplicates = append(r.ObservedReplicates, false)
	}
	r.ObservedReplicates[f.Replicate-1] = true
	r.FindingsByReplicate[f.Replicate-1] += f.Findings
	r.TokensOutByReplicate[f.Replicate-1] += f.TokensOut
}

// judge applies the health-only rule: unfit on any truncated, unparseable or failed
// call, or any call silent on every chunk; a minority of silent chunks is a warning.
func (r *fitPairRow) judge() {
	bad := func(n int, what string) {
		if n > 0 {
			r.Reasons = append(r.Reasons, fmt.Sprintf("%s %d/%d", what, n, r.Calls))
		}
	}
	bad(r.Truncated, "truncated")
	bad(r.Unparseable, "unparseable")
	bad(r.Failed-r.TimedOut, "failed")
	bad(r.TimedOut, "timed out")
	bad(r.WhollySilent, "silent on every chunk")
	if len(r.Reasons) > 0 {
		r.Verdict = fitVerdictUnfit
		return
	}
	if r.SilentChunks > 0 {
		r.Verdict = fitVerdictWarn
		r.Reasons = append(r.Reasons, fmt.Sprintf("%d/%d chunks silent", r.SilentChunks, r.Chunks))
		return
	}
	r.Verdict = fitVerdictFit
}

func writeFitReport(cmd *cobra.Command, rows []fitPairRow) error {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PERSONA\tMODEL\tVERDICT\tCALLS\tTRUNCATED\tUNPARSEABLE\tFAILED\tTIMED_OUT\tSILENT_CHUNKS\tFINDINGS/REPLICATE\tTOKENS_OUT/REPLICATE\tREASON")
	for _, r := range rows {
		reason := strings.Join(r.Reasons, "; ")
		if reason == "" {
			reason = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d/%d\t%s\t%s\t%s\n",
			r.Persona, r.Model, r.Verdict,
			r.Calls, r.Truncated, r.Unparseable, r.Failed, r.TimedOut,
			r.SilentChunks, r.Chunks, joinReplicates(r.FindingsByReplicate, r.ObservedReplicates),
			joinReplicates(r.TokensOutByReplicate, r.ObservedReplicates), reason)
	}
	return tw.Flush()
}

// joinReplicates prints per-replicate values, with "-" for a replicate the pair
// has no row for.
func joinReplicates(ns []int, observed []bool) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = "-"
		if observed[i] {
			parts[i] = strconv.Itoa(n)
		}
	}
	return strings.Join(parts, ",")
}
