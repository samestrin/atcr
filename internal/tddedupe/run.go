package tddedupe

import (
	"fmt"
	"strconv"
	"strings"

	reconcile "github.com/samestrin/atcr/reconcile"
)

// Stream is one reviewer stream: its source tag (the source label, and the
// reviewer of a row that names none) and its contents.
type Stream struct {
	Tag  string
	Data []byte
}

// Options are td-dedupe's knobs: the line window that clusters findings on one
// file, and the source tags whose findings alone are LOW confidence.
type Options struct {
	Tolerance int
	Untrusted []string
}

// Member is one finding in a cluster of 2+, with its own values rather than
// the cluster's pooled ones, so a model that splits the cluster can emit one
// correctly-cited finding per member.
type Member struct {
	Reviewer   string `json:"reviewer"`
	Severity   string `json:"severity"`
	FileLine   string `json:"file_line"`
	Problem    string `json:"problem"`
	Fix        string `json:"fix"`
	Category   string `json:"category"`
	EstMinutes int    `json:"est_minutes"`
}

// Row is one merged cluster, in td-dedupe's shape.
type Row struct {
	Severity     string   `json:"severity"`
	FileLine     string   `json:"file_line"`
	Problem      string   `json:"problem"`
	Fix          string   `json:"fix"`
	Category     string   `json:"category"`
	EstMinutes   int      `json:"est_minutes"`
	Source       string   `json:"source"`
	Reviewers    string   `json:"reviewers"`
	Confidence   string   `json:"confidence"`
	Disagreement string   `json:"disagreement,omitempty"`
	ClusterSize  int      `json:"cluster_size"`
	NeedsReview  bool     `json:"needs_review"`
	Members      []Member `json:"members,omitempty"`
}

// Summary is td-dedupe's summary, key for key. Warnings names each stream read
// without a version header.
type Summary struct {
	InputRows        int      `json:"input_rows"`
	Sources          int      `json:"sources"`
	Clusters         int      `json:"clusters"`
	MergedRows       int      `json:"merged_rows"`
	NeedsReviewCount int      `json:"needs_review_count"`
	Skipped          int      `json:"skipped"`
	Warnings         []string `json:"warnings,omitempty"`
}

// Result is the `atcr dedupe` payload: {merged, summary}.
type Result struct {
	Merged  []Row   `json:"merged"`
	Summary Summary `json:"summary"`
}

// clusterWithin is reconcile.ClusterWithin, a variable so a test can break the
// clustering and prove the accounting check catches it.
var clusterWithin = reconcile.ClusterWithin

// Run reads each stream with ReadStream, clusters the rows with
// reconcile.ClusterWithin and merges each cluster with reconcile.Merge.
// PROBLEM and FIX are pooled with reconcile.PoolText rather than Merge's
// longest value, so no remedy is dropped. A cluster whose distinct reviewers
// are fewer than two and whose every row comes from an Untrusted source is LOW,
// as td-dedupe ranks it. Every cluster of 2+ is needs_review with its members:
// the "same issue?" call stays with the model. A clustering that does not
// account for every row read exactly once is a FATAL error.
func Run(streams []Stream, opts Options) (Result, error) {
	untrusted := map[string]bool{}
	for _, t := range opts.Untrusted {
		if t = strings.TrimSpace(t); t != "" {
			untrusted[t] = true
		}
	}

	var (
		rows    []reconcile.Finding
		sources []string // sources[i] is the tag of the stream rows[i] came from
		sum     = Summary{Sources: len(streams)}
	)
	for _, s := range streams {
		findings, skipped, warning, err := ReadStream(s.Tag, s.Data)
		if err != nil {
			return Result{}, err
		}
		sum.Skipped += skipped
		if warning != "" {
			sum.Warnings = append(sum.Warnings, warning)
		}
		for _, f := range findings {
			rows = append(rows, f)
			sources = append(sources, s.Tag)
		}
	}
	sum.InputRows = len(rows)

	clusters, err := clusterIndexes(rows, opts.Tolerance)
	if err != nil {
		return Result{}, err
	}

	merged := make([]Row, 0, len(clusters))
	for _, idx := range clusters {
		group := make([]reconcile.Finding, len(idx))
		allUntrusted := true
		for i, n := range idx {
			group[i] = rows[n]
			allUntrusted = allUntrusted && untrusted[sources[n]]
		}
		row := mergeRow(group, sources[idx[0]], allUntrusted)
		if row.NeedsReview {
			sum.NeedsReviewCount++
		}
		merged = append(merged, row)
	}
	sum.Clusters = len(clusters)
	sum.MergedRows = len(merged)
	return Result{Merged: merged, Summary: sum}, nil
}

// clusterIndexes clusters rows and returns each cluster as indexes into rows.
// ClusterWithin reads only a finding's file and line, so it clusters stand-ins
// that carry those and the row's index, and each cluster maps back to its full
// rows (and their sources) by index.
func clusterIndexes(rows []reconcile.Finding, tolerance int) ([][]int, error) {
	stand := make([]reconcile.Finding, len(rows))
	for i, f := range rows {
		stand[i] = reconcile.Finding{File: f.File, Line: f.Line, Evidence: strconv.Itoa(i)}
	}
	seen := make([]bool, len(rows))
	clustered := 0
	var out [][]int
	for _, c := range clusterWithin(stand, tolerance) {
		idx := make([]int, 0, len(c))
		for _, f := range c {
			n, err := strconv.Atoi(f.Evidence)
			if err != nil || n < 0 || n >= len(rows) || seen[n] {
				return nil, fmt.Errorf("FATAL: dedupe accounting mismatch: row %q clustered twice or unknown", f.Evidence)
			}
			seen[n] = true
			idx = append(idx, n)
		}
		clustered += len(idx)
		if len(idx) > 0 {
			out = append(out, idx)
		}
	}
	if clustered != len(rows) {
		return nil, fmt.Errorf("FATAL: dedupe accounting mismatch: rows=%d clustered=%d", len(rows), clustered)
	}
	return out, nil
}

// mergeRow merges one cluster with reconcile.Merge and pools its PROBLEM and
// FIX. The location and source are the first member's. allUntrusted lowers a
// cluster with fewer than two distinct reviewers to LOW.
func mergeRow(group []reconcile.Finding, source string, allUntrusted bool) Row {
	m := reconcile.Merge(group)
	problems := make([]string, len(group))
	fixes := make([]string, len(group))
	for i, f := range group {
		problems[i], fixes[i] = f.Problem, f.Fix
	}
	row := Row{
		Severity:     m.Severity,
		FileLine:     fileLine(group[0]),
		Problem:      reconcile.PoolText(problems),
		Fix:          reconcile.PoolText(fixes),
		Category:     m.Category,
		EstMinutes:   m.EstMinutes,
		Source:       source,
		Reviewers:    strings.Join(m.Reviewers, ","),
		Confidence:   m.Confidence,
		Disagreement: m.Disagreement,
		ClusterSize:  len(group),
		NeedsReview:  len(group) >= 2,
	}
	if allUntrusted && len(m.Reviewers) < 2 {
		row.Confidence = reconcile.ConfLow
	}
	if row.NeedsReview {
		for _, f := range group {
			row.Members = append(row.Members, Member{
				Reviewer:   f.Reviewer,
				Severity:   f.Severity,
				FileLine:   fileLine(f),
				Problem:    f.Problem,
				Fix:        f.Fix,
				Category:   f.Category,
				EstMinutes: f.EstMinutes,
			})
		}
	}
	return row
}

// fileLine is FILE:LINE, or the bare file for a finding with no line.
func fileLine(f reconcile.Finding) string {
	if f.Line <= 0 {
		return f.File
	}
	return f.File + ":" + strconv.Itoa(f.Line)
}
