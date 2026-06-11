package reconcile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Adjudication artifact filenames written/read in the reconciled dir.
const (
	// AdjudicationJSON is the Skill-authored decisions file consumed on a
	// reconcile re-invocation (atcr applies it; it does not write it).
	AdjudicationJSON = "adjudication.json"
	// OriginalAmbiguousJSON preserves the pre-adjudication ambiguous.json so the
	// audit chain survives a re-invocation.
	OriginalAmbiguousJSON = "ambiguous.original.json"
)

// Adjudication decision verbs (AC 05-04). Any other value is rejected.
const (
	DecisionMerge    = "merge"
	DecisionDistinct = "distinct"
	DecisionSkipped  = "skipped"
)

// Decision is one Skill adjudication of an ambiguous cluster. ClusterID matches
// an AmbiguousCluster.ID from ambiguous.json. HostModel/Timestamp form the audit
// trail; they are recorded verbatim and not interpreted by atcr.
type Decision struct {
	ClusterID string `json:"cluster_id"`
	Decision  string `json:"decision"`
	Rationale string `json:"rationale,omitempty"`
	HostModel string `json:"host_model,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// Adjudication is the adjudication.json document: a flat list of decisions.
type Adjudication struct {
	Decisions []Decision `json:"decisions"`
}

// AmbiguousID is the stable content-addressed id for a gray-zone pair. It is
// derived from the location and the two PROBLEM texts (order-independent), so
// the id is byte-identical on the initial reconcile and on the adjudicated
// re-invocation of the same sources — the property that lets the Skill reference
// a cluster across runs without atcr persisting a counter.
func AmbiguousID(file string, line int, problemA, problemB string) string {
	lo, hi := problemA, problemB
	if hi < lo {
		lo, hi = hi, lo
	}
	h := sha256.Sum256([]byte(file + "\x00" + strconv.Itoa(line) + "\x00" + lo + "\x00" + hi))
	// 128 bits: a collision would alias two distinct gray pairs to one id and let
	// one merge decision collapse the wrong pair — the one outcome the design
	// forbids — so spend the bytes.
	return "amb-" + hex.EncodeToString(h[:16])
}

// LoadAdjudication reads and validates an adjudication.json document. A
// zero-byte or malformed file is an error (AC 05-04 Error Scenario 1); an
// unknown decision verb is rejected (Error Scenario 3). It does not yet check
// cluster ids — that needs the prior ambiguous set and is done by the caller.
func LoadAdjudication(path string) (*Adjudication, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("adjudication.json is empty (malformed)")
	}
	var adj Adjudication
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&adj); err != nil {
		return nil, fmt.Errorf("parsing adjudication.json: %w", err)
	}
	for _, d := range adj.Decisions {
		switch d.Decision {
		case DecisionMerge, DecisionDistinct, DecisionSkipped:
		default:
			return nil, fmt.Errorf("invalid adjudication decision %q for cluster %s: must be merge, distinct, or skipped", d.Decision, d.ClusterID)
		}
	}
	return &adj, nil
}

// ValidateDecisions rejects any decision referencing a cluster id absent from
// known (the ids present in the ambiguous.json being adjudicated), per AC 05-04
// Edge Case 5. known is the set of valid AmbiguousCluster.ID values.
func ValidateDecisions(adj *Adjudication, known map[string]bool) error {
	for _, d := range adj.Decisions {
		if !known[d.ClusterID] {
			return fmt.Errorf("adjudication references unknown cluster id: %s", d.ClusterID)
		}
	}
	return nil
}

// MergeSet returns the set of cluster ids the Skill decided to merge. Distinct
// and skipped clusters are absent (left unmerged — the conservative default).
func (a *Adjudication) MergeSet() map[string]bool {
	out := map[string]bool{}
	for _, d := range a.Decisions {
		if d.Decision == DecisionMerge {
			out[d.ClusterID] = true
		}
	}
	return out
}

// AmbiguousIDsFromFile reads an ambiguous.json sidecar and returns the set of
// cluster ids it contains — the valid targets for adjudication decisions.
func AmbiguousIDsFromFile(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var clusters []AmbiguousCluster
	if err := json.Unmarshal(data, &clusters); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", AmbiguousJSON, err)
	}
	ids := make(map[string]bool, len(clusters))
	for _, c := range clusters {
		ids[c.ID] = true
	}
	return ids, nil
}

// preserveOriginalAmbiguous copies reconDir/ambiguous.json to
// ambiguous.original.json before an adjudicated re-emit overwrites it, so the
// pre-adjudication gray-zone clusters remain on disk for audit. It is a no-op
// when the original is already preserved (a second adjudication re-run must NOT
// clobber the true original with the already-merged, shrunken sidecar) or when
// there is no ambiguous.json to preserve.
func preserveOriginalAmbiguous(reconDir string) error {
	if _, err := os.Stat(filepath.Join(reconDir, OriginalAmbiguousJSON)); err == nil {
		return nil // true original already preserved on a prior adjudication
	}
	data, err := os.ReadFile(filepath.Join(reconDir, AmbiguousJSON))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading ambiguous.json to preserve: %w", err)
	}
	// writeFileAtomic (temp + rename), like every other reconciled/ artifact: a
	// crash mid-write must never leave a truncated baseline — the stat guard
	// above would then block it from ever being repaired from the true original.
	if err := writeFileAtomic(filepath.Join(reconDir, OriginalAmbiguousJSON), data); err != nil {
		return fmt.Errorf("writing %s: %w", OriginalAmbiguousJSON, err)
	}
	return nil
}
