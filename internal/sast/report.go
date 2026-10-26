package sast

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// ReportFile is the review-dir sidecar that carries one scan's findings from
// the review stage, which runs the scanner, to reconcile, which merges them
// into the reconciled report.
const ReportFile = "sast.json"

// Report is one scan's findings as written to ReportFile. Scanner names the
// runner (Runner.Name) and becomes part of the reviewer name on every merged
// finding, so it is restricted to scannerNamePattern.
type Report struct {
	Scanner  string
	Findings []Finding
}

// scannerNamePattern keeps a scanner name safe as a REVIEWERS cell: no comma,
// pipe, space or path separator can reach the reconciled report through it.
var scannerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type reportWire struct {
	Scanner  string        `json:"scanner"`
	Findings []findingWire `json:"findings"`
}

type findingWire struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// WriteReport writes r to reviewDir/ReportFile through a temp file and rename,
// so reconcile never reads a half-written sidecar. It refuses a report
// ReadReport would reject.
func WriteReport(reviewDir string, r Report) error {
	if err := validateReport(r); err != nil {
		return err
	}
	w := reportWire{Scanner: r.Scanner, Findings: make([]findingWire, 0, len(r.Findings))}
	for _, f := range r.Findings {
		w.Findings = append(w.Findings, findingWire(f))
	}
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return fmt.Errorf("sast: encode report: %w", err)
	}
	tmp, err := os.CreateTemp(reviewDir, ReportFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("sast: write report: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sast: write report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("sast: write report: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(reviewDir, ReportFile)); err != nil {
		return fmt.Errorf("sast: write report: %w", err)
	}
	return nil
}

// ReadReport loads reviewDir/ReportFile. A missing file (no scanner ran, the
// default) is (nil, nil). The sidecar sits outside the scan root, so ParseSARIF's
// confinement cannot vouch for it: every path is re-checked lexically and a
// malformed file, a bad scanner name or an unsafe path is an error, never a
// silently shorter list.
func ReadReport(reviewDir string) (*Report, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, ReportFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("sast: read report: %w", err)
	}
	var w reportWire
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("sast: parse %s: %w", ReportFile, err)
	}
	r := Report{Scanner: w.Scanner}
	for _, f := range w.Findings {
		r.Findings = append(r.Findings, Finding(f))
	}
	if err := validateReport(r); err != nil {
		return nil, fmt.Errorf("sast: %s: %w", ReportFile, err)
	}
	return &r, nil
}

func validateReport(r Report) error {
	if !scannerNamePattern.MatchString(r.Scanner) {
		return fmt.Errorf("invalid scanner name %q", r.Scanner)
	}
	for _, f := range r.Findings {
		if !safeRelPath(f.File) {
			return fmt.Errorf("unsafe finding path %q", f.File)
		}
		if f.Line < 0 {
			return fmt.Errorf("negative line %d for %q", f.Line, f.File)
		}
	}
	return nil
}

// safeRelPath reports whether p is a clean, slash-separated, root-relative
// path with no ".." segment: the shape ParseSARIF's confine returns.
func safeRelPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || path.IsAbs(p) || filepath.IsAbs(p) {
		return false
	}
	if path.Clean(p) != p || p == "." {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}
