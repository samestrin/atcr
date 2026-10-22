//go:build probe

package scorecard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
)

// TestProbeEraBumpDifferential is the live-store differential for the
// raised_denominator era 4 bump (Epic 35.16.11.3.1 T3). It is a probe, not a
// test: it reads real reconciled reviews and prints a table. Run it with
//
//	PROBE_REVIEWS=/path/to/.atcr/reviews go test -tags probe \
//	    -run TestProbeEraBumpDifferential ./internal/scorecard/ -v
//
// Every review is replayed through EmitForReconcile, which writes era-4
// records. Each is then also mapped to the era-3 record the pre-T2 binary
// would have written (FindingsRaised minus FindingsAmbiguous), which is exact
// (see foldAmbiguousChargeEra). Four stores are compared per lens, as runs
// kept by the trust chain and corroborated/raised:
//
//	before   every run era 3: the store as it stood before the bump
//	nofold   the newest run per lens era 4, the rest era 3, through the
//	         chain WITHOUT foldAmbiguousChargeEra: the blackout
//	fold     the same store through keptForTrust: what this binary does
//	after    every run era 4 through the chain without the fold: what a lens
//	         reads once it holds DefaultTrustMinRuns era-4 runs
func TestProbeEraBumpDifferential(t *testing.T) {
	root := os.Getenv("PROBE_REVIEWS")
	if root == "" {
		t.Skip("PROBE_REVIEWS not set")
	}
	paths, _ := filepath.Glob(filepath.Join(root, "*", "reconciled", "summary.json"))
	store := t.TempDir()
	for _, p := range paths {
		rd := filepath.Dir(filepath.Dir(p))
		var res reconcile.Result
		probeJSON(t, filepath.Join(rd, "reconciled", "findings.json"), &res.Findings, true)
		probeJSON(t, filepath.Join(rd, "reconciled", "unresolved.json"), &res.Unresolved, false)
		probeJSON(t, filepath.Join(rd, "reconciled", "ambiguous.json"), &res.Ambiguous, false)
		probeJSON(t, p, &res.Summary, true)
		EmitForReconcile(rd, res, EmitOpts{Dir: store, Diag: io.Discard})
	}
	era4, err := ReadAll(store, ReadOpts{Writer: io.Discard})
	if err != nil {
		t.Fatal(err)
	}

	toEra3 := func(r Record) Record {
		if r.RecordType != RecordTypeReviewer || r.RaisedDenominator != raisedDenominatorAmbiguousCharged {
			return r
		}
		r.FindingsRaised -= r.FindingsAmbiguous
		r.FindingsSolo -= r.FindingsAmbiguous
		r.FindingsAmbiguous = 0
		r.RaisedDenominator = raisedDenominatorRoutedExShield
		r.CorroborationRate = ratio(r.FindingsCorroborated, r.FindingsRaised)
		return r
	}
	charged := map[string]int{}
	before := make([]Record, len(era4))
	for i, r := range era4 {
		before[i] = toEra3(r)
		if r.RecordType == RecordTypeReviewer {
			charged[normalizeReviewerName(r.Reviewer)] += r.FindingsAmbiguous
		}
	}
	// The newest run per lens AMONG THE RUNS THE CHAIN KEEPS: a run the strict
	// or outcome gate drops never reaches the era link, so re-stamping it
	// would model no upgrade at all.
	newest := map[string]string{}
	for _, r := range keptForTrust(before) {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		if k := normalizeReviewerName(r.Reviewer); r.RunID > newest[k] {
			newest[k] = r.RunID
		}
	}
	upgrade := make([]Record, len(era4))
	for i, r := range era4 {
		upgrade[i] = before[i]
		if r.RecordType == RecordTypeReviewer && r.RunID == newest[normalizeReviewerName(r.Reviewer)] {
			upgrade[i] = r
		}
	}
	noFold := func(records []Record) []Record {
		unions := opportunityUnions(records)
		return opportunitySetRuns(unresolvedEraRuns(mergeRoutedEras(scrubForgedCredit(eligibleOutcomeRuns(strictRuns(records))))), unions)
	}

	type tally struct{ runs, raised, corr int }
	fold := func(kept []Record) map[string]tally {
		out := map[string]tally{}
		for _, r := range kept {
			if r.RecordType != RecordTypeReviewer {
				continue
			}
			k := normalizeReviewerName(r.Reviewer)
			x := out[k]
			x.runs++
			x.raised += r.FindingsRaised
			x.corr += r.FindingsCorroborated
			out[k] = x
		}
		return out
	}
	cols := []struct {
		name string
		got  map[string]tally
	}{
		{"before", fold(keptForTrust(before))},
		{"nofold", fold(noFold(upgrade))},
		{"fold", fold(keptForTrust(upgrade))},
		{"after", fold(noFold(era4))},
	}
	names := map[string]struct{}{}
	for _, c := range cols {
		for n := range c.got {
			names[n] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	// The era mix of each store, as a check that the upgrade store really
	// holds one era-4 run per lens.
	eraCount := map[string]map[int]int{}
	for name, recs := range map[string][]Record{"era4": era4, "before": before, "upgrade": upgrade} {
		eraCount[name] = map[int]int{}
		for _, r := range recs {
			if r.RecordType == RecordTypeReviewer {
				eraCount[name][r.RaisedDenominator]++
			}
		}
	}
	fmt.Printf("eras: %v\n", eraCount)
	fmt.Printf("corpus: %d reviews, %d records, DefaultTrustMinRuns %d\n", len(paths), len(era4), DefaultTrustMinRuns)
	fmt.Printf("%-8s %7s", "lens", "charged")
	for _, c := range cols {
		fmt.Printf(" | %-6s %4s %9s", c.name, "runs", "corr/rais")
	}
	fmt.Println()
	for _, n := range sorted {
		fmt.Printf("%-8s %7d", n, charged[n])
		for _, c := range cols {
			x := c.got[n]
			fmt.Printf(" | %-6s %4d %4d/%-4d", "", x.runs, x.corr, x.raised)
		}
		fmt.Println()
	}
}

func probeJSON(t *testing.T, path string, v any, required bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if required {
			t.Fatal(err)
		}
		return
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
