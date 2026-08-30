package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/internal/validation"
	"github.com/spf13/cobra"
)

// newLeaderboardCmd builds `atcr leaderboard`: aggregate stored scorecard records
// across runs into a table ranked by corroboration rate, with optional --since,
// --model, and --persona filters.
func newLeaderboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "leaderboard",
		Short: "Aggregate scorecard records across runs, ranked by corroboration rate",
		Long: "Aggregate the local scorecard store across runs into a leaderboard ranked\n" +
			"by corroboration rate. Records are grouped by (reviewer, model). Filters\n" +
			"compose with AND semantics. Read-only.\n\n" +
			"With --export, emit an anonymized, versioned public submission JSON\n" +
			"document instead of the table (run_id and any path/host/key strings are\n" +
			"stripped); --output writes it to a file instead of stdout.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runLeaderboard,
	}
	cmd.Flags().String("since", "30d", `time window: Nd (days), Nw (weeks), Nm (months); "all" disables the window`)
	cmd.Flags().String("model", "", "filter to a model id (substring, case-insensitive)")
	cmd.Flags().String("persona", "", "filter to an exact reviewer/persona name")
	// The version is named here, not only in the docs, because the bump was made for
	// a BENCHMARK-side change (coverage in the suite envelope) while this envelope
	// gained no fields — and board acceptance of the new number is an unverified
	// coordination item. A production submitter reads this help and nothing else, so
	// omitting it would leave the one group affected by the risk uninformed. The
	// number is formatted from the constant so one bump updates every surface.
	cmd.Flags().Bool("export", false, fmt.Sprintf("emit anonymized public submission JSON instead of the table. The envelope stamps submission_schema %d; its field set is unchanged from 1, but a board pinned to an earlier version must be updated to accept it", scorecard.SubmissionSchema))
	cmd.Flags().String("output", "", "with --export: write JSON to this file instead of stdout (atomically replaces the target; a symlink at the path is replaced, not followed)")
	return cmd
}

func runLeaderboard(cmd *cobra.Command, _ []string) error {
	since, _ := cmd.Flags().GetString("since")
	// Map the no-window sentinels to an empty string before building FilterOpts.
	// scorecard.ApplyFilters already treats empty Since as "no window"; this mapping
	// keeps ParseSince's strict contract untouched (it lives in internal/scorecard).
	if since == "all" || since == "0" {
		since = ""
	}
	model, _ := cmd.Flags().GetString("model")
	persona, _ := cmd.Flags().GetString("persona")
	export, _ := cmd.Flags().GetBool("export")
	output, _ := cmd.Flags().GetString("output")

	// --output only routes the export document; without --export the table view
	// has nothing to write, so a bare --output is a usage error (exit 2) rather
	// than a silent no-op that leaves the user's expected file unwritten.
	if output != "" && !export {
		return usageError(errors.New("--output requires --export"))
	}

	// Validate --output at the input layer for parity with review: the absolute
	// path is rejected (exit 2) when it references a system directory or contains
	// path traversal, before any export work begins. Like review (and unlike
	// report), symlinks are NOT resolved here — writeExportFile deliberately
	// follows a symlink at the target as a documented design choice for a local
	// CLI, so validating filepath.Abs keeps that posture while still blocking a
	// literal system-dir or traversal path.
	if output != "" {
		abs, aerr := filepath.Abs(output)
		if aerr != nil {
			return usageError(fmt.Errorf("resolving --output: %w", aerr))
		}
		if verr := validation.FilePath(abs); verr != nil {
			return usageError(verr)
		}
		output = abs
	}

	dir, err := scorecard.DefaultDir()
	if err != nil {
		return fmt.Errorf("cannot determine scorecard store path: %w", err)
	}
	readOpts := scorecard.ReadOpts{Writer: cmd.ErrOrStderr()}

	// One now for the table path: it anchors the windowed read below and the
	// day-precision filter further down, which would otherwise call time.Now()
	// again. (The export path keeps its own UTC anchor — see
	// runLeaderboardExport.) Month-file selection makes the skew immaterial in
	// production, but a single anchor is what makes the boundary deterministic
	// under test.
	now := time.Now()

	// Size the read window from --since. The value is parsed here only to bound
	// the I/O, so a parse failure is deliberately NOT reported at this point: a
	// zero window reads all history, and ApplyFilters below stays the single
	// source of the invalid --since error. That keeps the empty-store check's
	// precedence intact — `--since abc` against an empty store reports the
	// graceful empty state (exit 0) exactly as it did before the window existed.
	//
	// The "all"/"0" sentinels already became an empty string above, which
	// ParseSince rejects by construction; skipping the parse for it is what keeps
	// a no-window query an all-history read instead of an error.
	var window time.Duration
	if !export && since != "" {
		if d, perr := scorecard.ParseSince(since); perr == nil {
			window = d
		}
	}

	// --export reads all history: the `!export` guard on the window computation
	// above forces window to 0 on the export path, and a zero-window ReadSince
	// delegates to an all-history read (store.go:394). Keeping the export read
	// surface unwindowed preserves the distinction between an empty store and a
	// no-match filter: windowing it here would swap the empty-store error for the
	// no-match one. The table view takes the real windowed read, so displaying
	// one month of leaderboard data no longer opens every month file the store
	// has ever written.
	//
	// Two consequences follow from selecting files instead of filtering records,
	// and both are accepted rather than overlooked:
	//
	//   - An unreadable or corrupt month file OUTSIDE the window no longer fails
	//     the command WHILE THE WINDOW IS NON-EMPTY. When the window comes back
	//     empty, the probe below runs ReadAll and may open that file and surface
	//     its failure; `--since all` still surfaces it, as does any window that
	//     reaches it.
	//   - A record stamped in a FUTURE calendar month (host clock skew, an
	//     imported record) drops out of the table. ReadSince's upper edge is
	//     deliberately fail-closed (see monthOverlapsWindow), whereas the filter
	//     alone only ever dropped records BEFORE the cutoff. Widening that edge
	//     would be a change to the store's windowing contract, which this call
	//     site is not the place to make.
	var records []scorecard.Record
	records, err = scorecard.ReadSince(dir, window, now, readOpts)
	if err != nil {
		return fmt.Errorf("failed to read scorecard store: %w", err)
	}

	out := cmd.OutOrStdout()
	filters := scorecard.FilterOpts{Since: since, Model: model, Persona: persona}

	// --export takes its own path: it emits anonymized JSON and treats an empty
	// store or a no-match filter as a failure (exit 1) — unlike the table view,
	// where an empty store is a graceful exit-0 state.
	if export {
		return runLeaderboardExport(cmd, records, filters, output)
	}

	if len(records) == 0 && window > 0 {
		// The window came back empty, which the windowed read alone cannot explain:
		// an empty store and a store whose every record predates the window look
		// identical once out-of-window files are never opened — yet they are
		// different outcomes with different exit codes. Probe the whole store to
		// tell them apart. The cost lands only here, on a query that already found
		// nothing, never on the populated-window path this read exists to bound.
		//
		// Reading all history back into `records` also keeps the two branches below
		// byte-identical to the unbounded implementation: an empty store still
		// falls through to the graceful message, while out-of-window data now
		// reaches ApplyFilters and is rejected by it, producing the no-match error.
		//
		// The probe's only job is the boolean "is the store non-empty", so its read
		// discards diagnostics; the windowed read above already reported any file it
		// opened, and re-emitting them here would double the noise and re-expose the
		// absolute store path in user-visible output.
		records, err = scorecard.ReadAll(dir, scorecard.ReadOpts{Writer: io.Discard})
		if err != nil {
			return fmt.Errorf("failed to read scorecard store: %w", err)
		}
	}

	if len(records) == 0 {
		// No data at all is a graceful empty state, not an error (exit 0).
		_, err := fmt.Fprintln(out, "No scorecard data found. Run 'atcr reconcile' to generate scorecard records.")
		return err
	}

	filtered, err := scorecard.ApplyFilters(records, filters, now)
	if err != nil {
		// A bad --since value parses at runtime (not by cobra); per the sprint
		// contract it is a runtime error (exit 1) carrying actionable guidance.
		return err
	}
	if len(filtered) == 0 {
		// Data exists but no record survived the filters: a real "nothing to
		// show" outcome (exit 1), distinct from the empty-store state above. The
		// active window is named so data hidden purely by the default 30d --since
		// is not mistaken for a bad --model/--persona.
		windowClause := "last " + since
		if since == "" {
			windowClause = "all time"
		}
		return fmt.Errorf("no records match filters (window: %s). Try a wider --since or removing --model/--persona", windowClause)
	}

	return renderLeaderboard(out, scorecard.Aggregate(filtered))
}

// renderLeaderboard writes the ranked aggregate table via text/tabwriter. Cost
// per corroborated finding renders as a dash for a group with zero corroborated
// findings (undefined). The table is buffered and written once so a flush error
// cannot emit a half table; the single write's error is propagated.
func renderLeaderboard(w io.Writer, rows []scorecard.LeaderboardRow) error {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "REVIEWER\tMODEL\tRUNS\tRAISED\tCORROBORATED\tCORR%\tCOST\tCOST/CORR\tLATENCY")
	for _, r := range rows {
		costPerCorr := "-"
		if r.HasCostPerCorroborated {
			costPerCorr = fmt.Sprintf("$%.4f", r.CostPerCorroborated)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\t$%.4f\t%s\t%dms\n",
			sanitizeCell(r.Reviewer), sanitizeCell(r.Model), r.Runs,
			r.FindingsRaised, r.FindingsCorroborated, formatPercent(r.CorroborationRate),
			r.TotalCostUSD, costPerCorr, r.AvgLatencyMS)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// runLeaderboardExport builds the anonymized v1 submission JSON and routes it to
// stdout or, when output is set, to a file. An empty store returns a "run reconcile"
// error (exit 1) distinct from the filter-no-match error. Both propagate through
// main() to stderr so a success-path `--export | jq` never sees non-JSON on
// stdout. time.Now().UTC() is the envelope timestamp and the --since anchor.
func runLeaderboardExport(cmd *cobra.Command, records []scorecard.Record, filters scorecard.FilterOpts, output string) error {
	if len(records) == 0 {
		return fmt.Errorf("no scorecard data yet; run 'atcr reconcile' to generate records")
	}
	// One timestamp for the selection and the envelope. It is the --since anchor as well
	// as submitted_at, so calling time.Now() twice could select a different record set
	// than the one the guard below inspected.
	now := time.Now().UTC()
	// ONE selection, used by the guard and by the serializer. The export path
	// deliberately reads ALL history (window is forced to 0 above), so selecting twice
	// re-parsed every RunID in an unrotated store through time.Parse for nothing — and,
	// worse, gave the guard a SUPERSET of the envelope: a record ApplyFilters selects
	// but the era pass drops used to hard-fail an export whose envelope was clean.
	//
	// A filter error is returned as-is: it is a usage error (a bad --since), not an
	// identity defect, and it must read the same as it did when Export raised it.
	selected, err := scorecard.PublishedSet(records, filters, now)
	if err != nil {
		return err
	}
	if err := validatePublishableRecordIdentities(selected); err != nil {
		return err
	}
	data, err := scorecard.ExportSelected(selected, now)
	if err != nil {
		if errors.Is(err, scorecard.ErrNoExportRecords) {
			return err
		}
		// A bad --since (or another runtime error) carries its own actionable text.
		return err
	}
	if output == "" {
		_, werr := cmd.OutOrStdout().Write(append(data, '\n'))
		return werr
	}
	return writeExportFile(output, data)
}

// validatePublishableRecordIdentities rejects an export whose published identities
// carry a rune the public envelope cannot show intact.
//
// `benchmark export` asserts this invariant on its own producer
// (validateRunResultForPublication), but leaderboard --export is the SIBLING producer
// into the SAME envelope through the SAME scrubField, and had no guard — so
// "no invisible rune survives into the published document" held for one producer and
// not the other. Epic 35.16.6.2 created that divergence; before it neither checked.
//
// Printability is checked on the RAW identity, before the scrub, for the reason the
// benchmark gate documents: ScrubPublicString provably leaves control (Cc) and format
// (Cf) runes alone, so an invisible rune survives into the envelope and no other arm
// can see it — the value is non-empty on both sides of the scrub.
//
// It takes the ALREADY-SELECTED set — scorecard.PublishedSet, the one definition of
// what the envelope carries — rather than selecting again from every stored record.
// That is what makes it neither too loose nor too strict: a record the operator's own
// --since/--model excluded cannot fail an export it was never part of, and neither can
// one the post-filter unresolvedEraRuns pass drops (the losing half of a reviewer
// spanning the 35.16.6.5 FindingsRaised era boundary), which the earlier ApplyFilters
// call here did reject. It also means the store is walked once on the one path that
// deliberately reads all of it.
func validatePublishableRecordIdentities(filtered []scorecard.Record) error {
	for _, rec := range filtered {
		// Reviewer is the field Export scrubs into the envelope's `persona`; the pair
		// is (persona, model) there, not (reviewer, model).
		for _, f := range []struct{ name, value string }{
			{"model", rec.Model},
			{"reviewer", rec.Reviewer},
		} {
			if r, bad := firstNonPrintingRune(f.value); bad {
				// run_id takes %q for the same reason the offending value does: it is
				// read from the same world-writable store record, so it is untrusted
				// input on a surface an operator reads in a terminal. Printing the
				// locator raw would let the defect being reported reorder the report.
				return fmt.Errorf("scorecard record %q has %s %q, which contains a non-printing rune (U+%04X); "+
					"control and format runes are invisible or reorder text in the published document, "+
					"so a leaderboard row can be misattributed to a model that was never measured — "+
					"edit or remove that record in the scorecard store, then re-run the export",
					rec.RunID, f.name, f.value, r)
			}
		}
	}
	return nil
}

// writeExportFile atomically writes the export to path: it creates parent
// directories, writes a sibling temp file (0600), then renames it over the
// target, so a crash never leaves a partial file and an existing file is
// replaced whole. A directory target is rejected up front with a clear message.
// A symlink at the target is followed by the rename: accepted by design for a
// local CLI writing to a user-chosen path with the user's own permissions (same
// posture as the read path), so the blast radius is the user's own files; the
// --output help notes this so the behavior is not a surprise.
func writeExportFile(path string, data []byte) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf("--output path %s is a directory, not a file", path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".scorecard-export-*.tmp")
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	tmpName := tmp.Name()
	// Remove the temp file if anything below fails; a no-op after a successful
	// rename (the path no longer exists under tmpName).
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting output permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing export: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing export: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("finalizing output file: %w", err)
	}
	return nil
}
