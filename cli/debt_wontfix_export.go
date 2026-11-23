package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/validation"
)

// WontfixRecord is one finding a developer dismissed with
// `atcr debt resolve --status wontfix --reason "<reason>"`, as its effective
// (folded) record in the local debt store.
//
// Every field is a plain type, for the same reason as ModelInvocation's: the
// intended consumer is a separate Go module, for which internal/ types are
// unreachable.
//
// SENSITIVE DATA. Reason is free text a developer typed, and File is a
// repository path. Nothing here leaves the machine on its own; a consumer that
// forwards these fields owns what that exposes.
type WontfixRecord struct {
	// ID is the finding id (`atcr debt list`'s id column).
	ID string
	// Reason is the justification recorded with the dismissal, verbatim.
	Reason string
	// File and Line locate the finding. Line is 0 when none was recorded.
	File string
	Line int
	// Category is the finding's category, as the reconciler or `debt add` filed it.
	Category string
	// Personas are the reviewer personas that raised the finding. The slice is
	// the caller's own copy.
	Personas []string
	// ResolvedAt is when the dismissal was recorded (UTC RFC3339), or "" when
	// the record carries none.
	ResolvedAt string
}

// WontfixExportOptions selects the store ExportWontfixRecords reads. The zero
// value reads the store `atcr debt list` run from the working directory reads.
type WontfixExportOptions struct {
	// Store is the store directory, with the meaning of `atcr debt --store`: it
	// wins over any debt.store setting, a leading "~/" is expanded, and it is
	// validated the same way. Empty means unset; a blank (whitespace-only)
	// value is an error, as `--store ""` is.
	Store string
	// RepoRoot is where the repository-root walk starts when Store is unset.
	// The root found there is the one whose debt.store setting (project
	// .atcr/config.yaml, then the user registry) applies, else
	// <root>/.atcr/debt. Empty starts at the working directory.
	RepoRoot string
	// Diagnostics receives the store's skipped-line warnings. Nil writes them to
	// os.Stderr, as the debt commands do.
	Diagnostics io.Writer
}

// ExportWontfixRecords returns every finding whose effective status in the local
// debt store is wontfix, one record per id, sorted by id.
//
// "Effective" is the fold every debt command shares (`atcr debt list`): a
// finding's records are folded to one by id, and wontfix is the one status that
// survives a later re-detection. A finding dismissed and later reopened by a
// different status, or never dismissed, is not returned. Both store layouts
// (month shards and event files, or a mix) are read.
//
// It reads and never adds, changes or removes a record, and a missing store is
// an empty result, not an error. Like every debt reader, it first completes a
// layout conversion that was committed but left unfinished, so no record is
// read twice.
func ExportWontfixRecords(opts WontfixExportOptions) ([]WontfixRecord, error) {
	dir, err := wontfixStoreDir(opts)
	if err != nil {
		return nil, err
	}
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: opts.Diagnostics})
	if err != nil {
		return nil, fmt.Errorf("read local debt store: %w", err)
	}
	var out []WontfixRecord
	for _, r := range localdebt.FoldRecords(recs) {
		if !localdebt.IsSuppressingStatus(r.Status) {
			continue
		}
		out = append(out, WontfixRecord{
			ID:         r.ID,
			Reason:     r.Justification,
			File:       r.File,
			Line:       r.Line,
			Category:   r.Category,
			Personas:   append([]string(nil), r.Reviewers...),
			ResolvedAt: r.ResolvedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// wontfixStoreDir is debtStoreDir for a caller with no cobra command: an
// explicit Store checked as addDebtStoreFlag's PreRunE checks --store, else
// localdebt.StoreDir under the root debtRepoRoot's walk finds.
func wontfixStoreDir(opts WontfixExportOptions) (string, error) {
	if opts.Store != "" {
		dir := opts.Store
		if strings.TrimSpace(dir) == "" {
			return "", fmt.Errorf("store must not be blank; leave it empty to resolve the configured store or <repo root>/.atcr/debt")
		}
		dir, err := validation.ExpandHome(dir)
		if err != nil {
			return "", fmt.Errorf("store: %w", err)
		}
		if err := validation.FilePath(dir); err != nil {
			return "", fmt.Errorf("store %q: %w", dir, err)
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("store %q: %w", dir, err)
		}
		if err := validation.FilePath(abs); err != nil {
			return "", fmt.Errorf("store %q: %w", dir, err)
		}
		return dir, nil
	}
	start := opts.RepoRoot
	if start == "" {
		start = "."
	}
	start, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve the local debt store: %w", err)
	}
	root := start
	if found, ok := localdebt.FindRepoRoot(start); ok {
		root = found
	}
	store, err := localdebt.StoreDir(root)
	if err != nil {
		return "", fmt.Errorf("resolve the local debt store: %w", err)
	}
	return store, nil
}
