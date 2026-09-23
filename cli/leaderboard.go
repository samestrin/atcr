package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	cmd.Flags().String("persona", "", "filter to one reviewer/persona name (whole name, case-insensitive)")
	// The version is named here, not only in the docs, because the bump was made for
	// a BENCHMARK-side change (coverage in the suite envelope) and board acceptance
	// of the new number is an unverified coordination item. A production submitter
	// reads this help and nothing else, so omitting it would leave the one group
	// affected by the risk uninformed. The number is formatted from the constant so
	// one bump updates every surface.
	//
	// The field set is NO LONGER unchanged from version 1: epic 35.16.6.8 added
	// raised_denominator to each reviewer row, additively. That is stated here
	// rather than left to the docs for the same reason the version number is — the
	// consumer who has to accept the new key is the board this submitter publishes
	// to, and this help is the only notice the submitter reads.
	cmd.Flags().Bool("export", false, fmt.Sprintf("emit anonymized public submission JSON instead of the table. The envelope stamps submission_schema %d and each reviewer row carries raised_denominator (which definition of findings-raised produced its rate); a board pinned to an earlier version must be updated to accept it", scorecard.SubmissionSchema))
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

	// This call deliberately aggregates RAW history: no unresolvedEraRuns era pass,
	// the one PublishedSet and TrustPriors both apply. That is product intent,
	// documented in docs/scorecard.md — the local leaderboard "reports what actually
	// happened across all runs", while export and trust must never blend two
	// raised_denominator definitions. Do not "fix" the asymmetry by wrapping this
	// call.
	return renderLeaderboard(out, scorecard.Aggregate(filtered))
}

// renderLeaderboard writes the ranked aggregate table via text/tabwriter. Cost
// per corroborated finding renders as a dash for a group with zero corroborated
// findings (undefined). The table is buffered and written once so a flush error
// cannot emit a half table; the single write's error is propagated.
//
// The rows it is handed are RAW history — aggregated without the unresolvedEraRuns
// era pass that PublishedSet and TrustPriors apply. That asymmetry is product
// intent and is argued at the caller's scorecard.Aggregate call in runLeaderboard.
func renderLeaderboard(w io.Writer, rows []scorecard.LeaderboardRow) error {
	// RAISED stopped counting doc-shield-routed findings (epic 35.16.6.5), so
	// without this column a row reading RAISED 6 / CORR 100% is indistinguishable
	// from one that raised 10 with 4 shielded — and the shielded count is what
	// `personas list --scores` acts on, so a demotion becomes unexplainable from
	// this table. Conditional, on the SOLO/VERIFIED precedent in renderScorecard:
	// a store with nothing shielded prints exactly the table it printed before.
	hasShielded := false
	for _, r := range rows {
		if r.FindingsDocShielded > 0 {
			hasShielded = true
			break
		}
	}

	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	header := "REVIEWER\tMODEL\tRUNS\tRAISED\tCORROBORATED\tCORR%\tCOST\tCOST/CORR\tLATENCY"
	if hasShielded {
		header += "\tDOC-SHIELDED"
	}
	_, _ = fmt.Fprintln(tw, header)
	for _, r := range rows {
		costPerCorr := "-"
		if r.HasCostPerCorroborated {
			costPerCorr = fmt.Sprintf("$%.4f", r.CostPerCorroborated)
		}
		row := fmt.Sprintf("%s\t%s\t%d\t%d\t%d\t%s\t$%.4f\t%s\t%dms",
			sanitizeCell(r.Reviewer), sanitizeCell(r.Model), r.Runs,
			r.FindingsRaised, r.FindingsCorroborated, formatPercent(r.CorroborationRate),
			r.TotalCostUSD, costPerCorr, r.AvgLatencyMS)
		if hasShielded {
			// A literal 0 rather than a dash: this reviewer HAS a measurement and
			// it is zero, which is a different statement from "not applicable".
			row += fmt.Sprintf("\t%d", r.FindingsDocShielded)
		}
		_, _ = fmt.Fprintln(tw, row)
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
	// One timestamp for the selection and the envelope, resolved HERE and threaded, so a
	// second time.Now() cannot creep back in below. It is the --since anchor as well as
	// submitted_at: two instants would publish a document whose submitted_at disagrees
	// with the window its rows were selected under, and on the boundary would select a
	// different record set than the identity guard inspected.
	return runLeaderboardExportAt(cmd, records, filters, output, time.Now().UTC())
}

// runLeaderboardExportAt is runLeaderboardExport with the instant injected, so a test
// can pin that the selection anchor and the envelope timestamp are the SAME one.
func runLeaderboardExportAt(cmd *cobra.Command, records []scorecard.Record, filters scorecard.FilterOpts, output string, now time.Time) error {
	if len(records) == 0 {
		return fmt.Errorf("no scorecard data yet; run 'atcr reconcile' to generate records")
	}
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
	selected, scrubs, err := selectPublishableRecordIdentities(cmd, selected)
	if err != nil {
		return err
	}
	// ErrNoExportRecords is the ONLY error this call site can observe, now that the
	// selection is made above. ExportSelected applies no --since window of its own,
	// so a bad --since can no longer reach it; what it does do is drop any
	// non-reviewer record it is handed — its own published-shape invariant — which
	// produces ErrNoExportRecords. It carries its own actionable text and main()
	// maps it to exit 1, so it is returned as-is rather than re-wrapped.
	//
	// Its sibling ErrNoCurrentEraRecords is NOT reachable from here. PublishedSet
	// already ran the same era pass and excluded every above-current record, so
	// ExportSelected's own pass can only come out empty when its input was empty —
	// and an empty input is caught first as ErrNoExportRecords. That error exists
	// for a direct embedder of ExportSelected that skipped PublishedSet.
	// The guard above already scrubbed every identity it inspected; handing the memo
	// over means ExportSelected does not re-derive them. scrubField is a fixed-point
	// loop over 7 compiled regexes that breaks on the first unchanged pass, so the
	// second pass cost 7 regex executions per field per record for an identity needing
	// no scrubbing and 14 for one that changes once — across the whole unrotated store.
	// (Up to 56 under the scrubPasses cap, which no current rule reaches.)
	data, err := scorecard.ExportSelectedCached(selected, now, scrubs)
	if err != nil {
		return err
	}
	if output == "" {
		_, werr := cmd.OutOrStdout().Write(append(data, '\n'))
		return werr
	}
	return writeExportFile(output, data)
}

// selectPublishableRecordIdentities returns the subset of the selection whose published
// identities survive the public scrub intact, and rejects the export outright when one
// of them carries a rune the envelope cannot show.
//
// The two arms deliberately have different blast radii, because the shapes they catch do:
//
//   - A control (Cc) or format (Cf) rune is a misattribution vector and is vanishingly
//     rare in an honestly written store, so it HARD-FAILS the whole export. Shrinking the
//     document instead would let a crafted record quietly remove a competitor's row.
//   - Empty ONCE SCRUBBED catches ordinary local-model id shapes — "/models/mistral-7b.gguf"
//     (llama.cpp / LM Studio), "bedrock@us-east-1/claude", "~/models/foo". Those sit in real
//     history, the export path forces window=0 so the window is the whole unrotated store,
//     and hard-failing on one of them took down an export that had succeeded the day before
//     with no escape short of hand-editing a JSONL store. Such a record is DROPPED from the
//     envelope — publishing it as model:"" is still refused — and named on stderr so the
//     operator can find and repair it.
//
// A skip is never silent, and it never empties the document unnoticed: dropping every
// record leaves ExportSelected to raise its own ErrNoExportRecords.
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
func selectPublishableRecordIdentities(cmd *cobra.Command, filtered []scorecard.Record) ([]scorecard.Record, scorecard.ScrubCache, error) {
	kept := make([]scorecard.Record, 0, len(filtered))
	// Every scrub this guard performs is handed back to the caller, which passes it to
	// ExportSelectedCached so the same identity is not scrubbed a second time there.
	scrubs := scorecard.ScrubCache{}
	// Every per-record outcome is BUFFERED and flushed only once the whole pass has
	// returned without error.
	//
	// The printability arm below hard-fails the WHOLE export, and it can trip on any
	// record — including the last. Printed where they are found, the notices for the
	// earlier records survive an abort that produced no document, so the operator is
	// told a record "is kept" or "is skipped" about an export that does not exist. Both
	// messages end in an instruction to go edit the store; acting on them repairs
	// records for a run whose real problem is elsewhere, and the export still fails.
	//
	// Buffering costs nothing in ordering: at most one notice is produced per record
	// (the skip arm breaks the field loop and suppresses the held blank notice), so
	// appending in record order preserves exactly the sequence the interleaved writes
	// produced.
	var notices []string
	for _, rec := range filtered {
		publishable := true
		// The blank-identity notice is HELD rather than printed where it is found, for
		// two reasons.
		//
		// It is one line per RECORD, not per field: the operator repairs the record, and
		// a second line about its other blank identity is noise — the same trade the
		// scrub-casualty report below makes with its `break`. Holding it also means the
		// field loop keeps RUNNING, which a `break` would not: breaking out on a blank
		// model would skip the reviewer field entirely, so a non-printing rune there — a
		// misattribution vector that must HARD-fail — would publish.
		//
		// And it is only printed if the record actually survives. The notice says the
		// record still publishes; a record whose OTHER identity is a scrub casualty is
		// dropped, and printing both lines for it would contradict itself on the one
		// surface the operator acts from.
		blankNotice := ""
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
				return nil, nil, fmt.Errorf("scorecard record %q has %s %q, which contains a non-printing rune (U+%04X); "+
					"control and format runes are invisible or reorder text in the published document, "+
					"so a leaderboard row can be misattributed to a model that was never measured — "+
					"edit or remove that record in the scorecard store, then re-run the export",
					rec.RunID, f.name, f.value, r)
			}
			// THREE mutually exclusive shapes an identity can have here, and what each
			// gets: one ALREADY empty in the store falls through both arms untouched and
			// silent; one blank only after trimming is reported and KEPT; one the scrub
			// empties is reported and DROPPED. The arms are mutually exclusive by their
			// own predicates, so the else-if is readability, not control flow.
			//
			// Already-empty is left silent deliberately. It is a record written without a
			// model — pre-existing and documented — so acting on it is a data decision
			// about existing history, not an identity-printability one, and reporting it
			// would fire on every model-less record in an unrotated store. `f.value != ""`
			// is the term that keeps this scoped to the whitespace shape.
			//
			// "Blank after trimming" is narrower than "whitespace-only", and the gap is a
			// CARVE-OUT rather than an oversight: tab, newline, CR, VT, FF and U+0085 are
			// whitespace AND unicode.IsControl, so firstNonPrintingRune above has already
			// hard-failed the whole export before this chain runs. They reach neither arm.
			// That is correct — a control rune in an identity is the misattribution vector
			// the printability check exists to stop, and being whitespace too does not
			// make it safe — but it is not self-evident from the word "whitespace", and a
			// tab is the likeliest whitespace artifact of a hand-edited store. What
			// actually reaches this arm is Zs-class blankness: a plain space, or a U+00A0
			// the printability arm lets through. Pinned by
			// TestRunLeaderboardExport_ControlClassWhitespaceHardFailsByDesign and stated
			// for operators in docs/scorecard.md.
			//
			// The empty-once-scrubbed arm exists to close a divergence: `benchmark export`
			// applies the same predicate on its producer side, so without it the two
			// sibling producers into the SAME envelope disagreed — benchmark hard-rejected
			// an identity the scrub deletes outright (an email- or path-shaped id) while
			// leaderboard published it as model:"".
			//
			// The scrub-REWRITES arm stays asymmetric on purpose. checkPublishable rejects
			// a value the scrub would change, because the envelope must name the same
			// suite the manifest does; the leaderboard has no manifest to match — the
			// scrub IS its anonymization — so an identity that scrubs to a different
			// string is published under the scrubbed form by design. Aligning that arm
			// would end the anonymization, not close a divergence.
			if trimmed := strings.TrimSpace(f.value); f.value != "" && trimmed == "" {
				// Blank, and therefore KEPT. Scoping the scrub-casualty arm off an
				// already-empty identity is deliberate (see above), but doing it
				// silently was a strict loss: before that scoping such a record
				// hard-failed locally with actionable text, and after it the operator
				// got a clean local export carrying model:"" — precisely the document
				// the sibling message calls one that "would be rejected at the
				// leaderboard" — and learned about it from the board instead.
				//
				// The blank value is NOT printed. `model " "` is what made the old
				// message unactionable, and a U+00A0 renders as nothing at all. The
				// wording is deliberately distinct from the skip report's "empty once
				// scrubbed" so an operator scanning stderr can tell a KEPT record from
				// a DROPPED one without reading to the end of the line.
				if blankNotice == "" {
					// "would be rejected at the leaderboard" is the SAME clause the skip
					// message below uses for the same published shape (an empty
					// identity). Two different consequences printed to one stderr for
					// one shape would leave the operator guessing which is true; the
					// repo states the consequence in exactly one place, so both
					// messages quote it.
					// The remedy clause names what the record's counts are ACTUALLY
					// doing, not what an operator would assume. "to have it counted"
					// was misleading: the record is already counted — just not in a row
					// of its own. ExportSelected keys on
					// key{scrubField(Reviewer), scrubField(Model)}, and a blank identity
					// and a genuinely-empty one both scrub to "", so the two merge into
					// one board row for that persona. Only the blank one is reported
					// here (the already-empty case is silent by design), so an operator
					// told the record is uncounted repairs half the problem and leaves
					// the merged row standing.
					blankNotice = fmt.Sprintf(
						"scorecard record %q: %s is blank after trimming — the record has no %s; "+
							"it is kept, but publishing \"\" would be rejected at the leaderboard, and "+
							"its counts are blended into the empty-%s row for that persona — "+
							"edit or remove that record in the scorecard store to give it a row of its own\n",
						rec.RunID, f.name, f.name, f.name)
				}
			} else if trimmed != "" && scrubs.Scrub(f.value) == "" {
				notices = append(notices, fmt.Sprintf(
					"skipping scorecard record %q: %s %q is empty once scrubbed for publication; "+
						"publishing \"\" would be rejected at the leaderboard — "+
						"edit or remove that record in the scorecard store to include it\n",
					rec.RunID, f.name, f.value))
				publishable = false
				// One report per record, not one per field: the operator repairs the
				// record, and a second line about its other identity is noise. A break
				// is safe HERE and not in the blank arm above, because the record is
				// already dropped — nothing it carries can reach the envelope, so a
				// check skipped on its other field cannot let anything through.
				break
			}
		}
		if publishable {
			if blankNotice != "" {
				notices = append(notices, blankNotice)
			}
			kept = append(kept, rec)
		}
	}
	// The pass completed: every buffered outcome now describes an export that is really
	// going to be produced.
	for _, n := range notices {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), n)
	}
	return kept, scrubs, nil
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
