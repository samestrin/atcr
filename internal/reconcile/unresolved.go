package reconcile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// routeUnresolved splits a reconcile result's findings into the primary stream
// and the Tier 4 sidecar (Epic 35.16.6.5 T4), given the indices
// validateFindingPaths reported as having exhausted all four tiers with zero
// symbol correspondence anywhere in the tracked tree.
//
// It returns the kept merged findings, the kept JSON records, and the routed
// records. merged and jf MUST be index-aligned on entry (JSONFindings derives
// 1:1 from Findings) and are kept aligned on exit — internal/mcp's
// failingFindings walks res.Findings and indexes res.JSONFindings() with the
// same i, so filtering one without the other would mis-pair every record after
// the first drop.
//
// Routing OUT of the primary stream is what gives AC3 its "no code change to
// internal/verify" property: the skeptic pipeline reads reconciled/findings.json
// (internal/verify/emit_findings.go), so a finding excluded here at emit time is
// already invisible to it, exactly the way consensus-filtered findings have
// always been. It is emphatically NOT a deletion — every routed record is
// written to unresolved.json (AC4).
//
// idx entries must be ascending and in range; an empty idx returns the inputs
// unchanged with a nil sidecar, so the common path allocates nothing.
func routeUnresolved(merged []Merged, jf []JSONFinding, idx []int) (keptMerged []Merged, keptJSON, routed []JSONFinding) {
	if len(idx) == 0 {
		return merged, jf, nil
	}
	drop := make(map[int]struct{}, len(idx))
	for _, i := range idx {
		drop[i] = struct{}{}
	}
	keptMerged = make([]Merged, 0, len(merged)-len(drop))
	keptJSON = make([]JSONFinding, 0, len(jf)-len(drop))
	routed = make([]JSONFinding, 0, len(drop))
	for i := range jf {
		if _, dropped := drop[i]; dropped {
			routed = append(routed, jf[i])
			continue
		}
		keptJSON = append(keptJSON, jf[i])
		if i < len(merged) {
			keptMerged = append(keptMerged, merged[i])
		}
	}
	return keptMerged, keptJSON, routed
}

// countOutOfScope recounts the out-of-scope findings over a post-routing set.
// Summary.OutOfScope is computed by the library over the pre-routing merged
// slice, so it must be recomputed whenever routing drops records — the same
// reason the library recounts it after its own consensus filter runs.
func countOutOfScope(merged []Merged) int {
	n := 0
	for _, m := range merged {
		if m.Category == CategoryOutOfScope {
			n++
		}
	}
	return n
}

// ReadUnresolvedFindings loads reviewDir/reconciled/unresolved.json — the Tier 4
// content-resolution sidecar (Epic 35.16.6.5). A missing or empty file returns
// (nil, nil): every reconciled dir written before this epic has no sidecar, and
// a run that routed nothing is legitimately empty. A present-but-unparseable
// file is an error. This mirrors ReadAmbiguousClusters exactly.
func ReadUnresolvedFindings(reviewDir string) ([]JSONFinding, error) {
	path := filepath.Join(reviewDir, reconciledSubdir, UnresolvedJSON)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var findings []JSONFinding
	if err := json.Unmarshal(data, &findings); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", UnresolvedJSON, err)
	}
	return findings, nil
}
