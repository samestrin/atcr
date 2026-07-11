package reconcile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
)

const (
	findingsFileName = "findings.txt"
	reconciledDir    = "reconciled"
	// statusFileName is the per-agent status.json sibling of a leaf findings.txt
	// (written by internal/fanout's statusFor). Read for fallback provenance only
	// (Epic 19.10 F5); its full schema stays owned by fanout.
	statusFileName = "status.json"
)

// Source is a discovered reconcile source: the immediate-child name under
// sources/ (e.g. "pool", "host") and the findings parsed from the leaf
// findings.txt files beneath it. Skipped records malformed rows so the caller
// can warn without failing the run. SkippedFiles records whole findings.txt
// files dropped on a read error or bad header, so the run summary can report
// the degradation (skipped_sources in summary.json) instead of losing it to a
// stderr-only warning.
type Source struct {
	Name         string
	Findings     []stream.Finding
	Skipped      []stream.SkippedRow
	SkippedFiles []string
}

// Discover finds reconcile sources under sourcesDir using leaf-preference: each
// immediate child directory (except reconciled/) is a source, and within it the
// findings come from the deepest findings.txt files — a findings.txt is an input
// only when no subdirectory beneath it also has one. This makes the per-agent
// pool/raw/agent/<name>/findings.txt files the pool's inputs while a merged
// findings.txt written at the source root is ignored (never double-counted), and
// reads host/findings.txt directly. allow, when non-empty, restricts which
// immediate children are read (AC 01-05 Scenario 7). reconciled/ is never an
// input. A file with a bad/missing header — or an unreadable subtree, or a
// non-regular findings.txt (symlink/FIFO/device) — is skipped with a warning
// rather than aborting the whole reconcile (sources/ is an open extension point).
// Only immediate-child directories are sources; a findings.txt placed directly
// under sources/ (not inside a child dir) is not a source and is ignored.
func Discover(sourcesDir string, allow []string) ([]Source, error) {
	entries, err := os.ReadDir(sourcesDir)
	if err != nil {
		return nil, fmt.Errorf("reading sources dir: %w", err)
	}
	allowSet := toSet(allow)
	matched := make(map[string]bool, len(allowSet))

	var sources []Source
	for _, e := range entries {
		if !e.IsDir() || e.Name() == reconciledDir {
			continue
		}
		if len(allowSet) > 0 && !allowSet[e.Name()] {
			continue
		}
		matched[e.Name()] = true
		child := filepath.Join(sourcesDir, e.Name())
		leaves, err := leafFindingsFiles(child)
		if err != nil {
			return nil, err
		}
		if len(leaves) == 0 {
			if allowSet[e.Name()] {
				fmt.Fprintf(os.Stderr, "warning: requested source %q has no findings.txt\n", e.Name())
			}
			continue // a child with no findings.txt anywhere is not a source
		}
		src := Source{Name: e.Name()}
		readable := 0
		for _, f := range leaves {
			data, rerr := os.ReadFile(f)
			if rerr != nil {
				// A transient/permission read error on one file must not abort the
				// whole reconcile (open extension point) — warn and skip it.
				fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", f, rerr)
				src.SkippedFiles = append(src.SkippedFiles, f)
				continue
			}
			res, perr := stream.ParseSource(data)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", f, perr)
				src.SkippedFiles = append(src.SkippedFiles, f)
				continue
			}
			readable++
			// Stamp fallback provenance (Epic 19.10 F5) from the leaf's sibling
			// status.json: when the slot that produced this findings.txt was served
			// by a litellm fallback model, mark every one of its findings with that
			// SERVED model so reconcile's distinct-reviewer count can de-weight it.
			// Fail-closed: a missing/unreadable/malformed status.json (or one with
			// fallback_used false) leaves FallbackModel empty — the finding counts as
			// an independent voice, mirroring the PathValid unvalidated default.
			if fbModel := readSourceFallback(f); fbModel != "" {
				for i := range res.Findings {
					res.Findings[i].FallbackModel = fbModel
				}
			}
			src.Findings = append(src.Findings, res.Findings...)
			src.Skipped = append(src.Skipped, res.Skipped...)
		}
		if len(allowSet) > 0 && readable == 0 {
			// The caller asked for this source by name and got nothing readable —
			// warn loudly but continue (v1 favors resilience over fail-closed
			// gating; the degradation is also recorded in summary.json).
			fmt.Fprintf(os.Stderr, "warning: requested source %q yielded zero readable findings files\n", e.Name())
		}
		sources = append(sources, src)
	}
	// A requested name with no matching child directory also yielded nothing —
	// warn (sorted for deterministic output) but continue, same v1 stance.
	for _, name := range sortedUnmatched(allowSet, matched) {
		fmt.Fprintf(os.Stderr, "warning: requested source %q not found under %s\n", name, sourcesDir)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	return sources, nil
}

// sortedUnmatched returns the allowlist names that never matched a child dir.
func sortedUnmatched(allowSet, matched map[string]bool) []string {
	var out []string
	for name := range allowSet {
		if !matched[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// leafFindingsFiles returns the leaf findings.txt paths under root: a
// findings.txt whose directory has no descendant directory that also contains a
// findings.txt. The result is sorted for deterministic ordering.
func leafFindingsFiles(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// One unreadable subtree must not abort discovery of the rest.
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// IsRegular() (not just !IsDir) excludes symlinks, FIFOs, devices, and
		// sockets named findings.txt: a symlink could point outside the review
		// dir (the same exfiltration risk persona resolution refuses), and a
		// device/FIFO would block or error on read.
		if d.Type().IsRegular() && d.Name() == findingsFileName {
			dirs = append(dirs, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}

	var leaves []string
	for _, d := range dirs {
		isLeaf := true
		for _, other := range dirs {
			// The trailing separator is load-bearing: it prevents sibling dirs
			// with a shared name prefix (pool vs pool2) from matching as nested.
			if other != d && strings.HasPrefix(other, d+string(os.PathSeparator)) {
				isLeaf = false
				break
			}
		}
		if isLeaf {
			leaves = append(leaves, filepath.Join(d, findingsFileName))
		}
	}
	sort.Strings(leaves)
	return leaves, nil
}

// readSourceFallback reads the status.json sibling of a leaf findings.txt and
// returns the fallback MODEL that served the slot when it was served by a litellm
// fallback (Epic 19.10 F5), else "". The served model — not the per-persona
// substituted-from name — is the de-weighting collapse key, since two personas on
// one net model are a single independent voice. The status.json layout is written
// by internal/fanout's statusFor; only the provenance fields are decoded here so
// reconcile stays decoupled from the full fanout AgentStatus type (no
// internal/fanout import). Fail-closed on any error: a slot with no readable
// provenance is treated as a non-fallback (independent) voice.
func readSourceFallback(findingsPath string) string {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(findingsPath), statusFileName))
	if err != nil {
		return ""
	}
	var st struct {
		FallbackUsed  bool   `json:"fallback_used"`
		FallbackModel string `json:"fallback_model"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return ""
	}
	if !st.FallbackUsed {
		return ""
	}
	return st.FallbackModel
}

// toSet builds a presence set, ignoring empty entries.
func toSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			set[n] = true
		}
	}
	return set
}

// AllFindings flattens the findings across sources in source order.
func AllFindings(sources []Source) []stream.Finding {
	var out []stream.Finding
	for _, s := range sources {
		out = append(out, s.Findings...)
	}
	return out
}
