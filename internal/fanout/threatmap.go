package fanout

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
)

// ThreatMapper is the optional threat-model mapping step of a git-range review
// (Epic 37.0, --threat-map): given every changed path it returns the subset the
// review panel should see. *threatmap.Agent wrapped by threatmap.Map is the
// production implementation; the CLI builds it, so this package never imports
// internal/threatmap. Any error means "keep every file" (fail open).
type ThreatMapper interface {
	Map(ctx context.Context, paths []string) ([]string, error)
}

// ErrThreatMapNeedsRange rejects a ThreatMapper on a baseline (--all/--scope)
// or --diff-file preparation. The phase is scoped to git-range reviews, and
// ignoring the mapper there would review every file while the caller believes
// the run was scoped.
var ErrThreatMapNeedsRange = errors.New("threat-map scoping applies to git-range reviews only; it cannot be combined with a baseline (--all/--scope) or --diff-file review")

// threatScope is the threat-map input to buildPayloads: a live mapper on a
// fresh run, or the manifest's record on a resume. Both nil means the run does
// not map, and buildPayloads is unchanged.
type threatScope struct {
	mapper   ThreatMapper
	recorded *payload.ThreatMapRecord
}

// resolve decides the keep set for a run whose reviewable paths are paths. It
// returns the record to persist (nil when the run does not map) and the keep
// set to trim with (nil = keep every file). A resume reuses the recorded keep
// set and never calls the mapper. A mapper error is logged and recorded, and
// every file is kept.
//
// The one error returned is an interrupt: a run cancelled while it maps is not
// a failed mapper, and recording it as one would make a resume review every
// file unscoped, so the preparation aborts instead.
func (s threatScope) resolve(ctx context.Context, paths []string) (*payload.ThreatMapRecord, map[string]bool, error) {
	if s.recorded != nil {
		return &payload.ThreatMapRecord{Keep: s.recorded.Keep, Failed: s.recorded.Failed}, keepSet(s.recorded.Keep), nil
	}
	if s.mapper == nil || len(paths) == 0 {
		return nil, nil, nil
	}
	keep, err := s.mapper.Map(ctx, paths)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, fmt.Errorf("threat map interrupted: %w", ctxErr)
		}
		log.FromContext(ctx).Warn("threat map failed; reviewing every changed file", "files", len(paths), "err", err)
		return &payload.ThreatMapRecord{Failed: err.Error()}, nil, nil
	}
	kept := append([]string(nil), keep...)
	sort.Strings(kept)
	return &payload.ThreatMapRecord{Keep: kept}, keepSet(kept), nil
}

func keepSet(paths []string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	return set
}

// reviewablePaths is the sorted, de-duplicated set of real file paths across
// every mode's entries. The synthetic sections (claim ledger, Context
// Definitions) are not files and are never shown to the mapper.
func reviewablePaths(byMode map[string][]payload.FileEntry) []string {
	seen := map[string]bool{}
	for _, entries := range byMode {
		for _, p := range payload.ReviewablePaths(entries) {
			seen[p] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// stampThreatMap copies the run's threat-map drops onto each merged result, so
// every status.json and summary.json record lists them.
func stampThreatMap(results []Result, dropped []string) {
	if len(dropped) == 0 {
		return
	}
	for i := range results {
		results[i].ThreatMapDropped = dropped
	}
}

// threatMapDropped is the record's drop list, nil for a run that did not map.
func threatMapDropped(r *payload.ThreatMapRecord) []string {
	if r == nil {
		return nil
	}
	return r.Dropped
}
