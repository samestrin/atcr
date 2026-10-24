// Package sast is the deterministic Static Application Security Testing seam:
// a scanner-agnostic Runner interface plus a SARIF 2.1.0 reader that turns a
// scanner's output into repo-relative findings (epic 50.0).
//
// A SARIF file is scanner output, and a scanner run over an untrusted change
// is untrusted input. Every finding path is therefore confined to the scan
// root before it is returned: a path with a ".." segment, an absolute path or
// a non-file URI, and a path that resolves through a symlink to somewhere
// outside the root are all dropped, never rewritten. Downstream stages may
// read the named file into a prompt, so a path that escapes the root must not
// survive this layer. Stdlib-only leaf: imports no internal package.
package sast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SARIFVersion is the only SARIF version ParseSARIF accepts.
const SARIFVersion = "2.1.0"

// Finding is one deterministic scanner result. File is slash-separated and
// relative to the scan root; Line is 1-based, or 0 when the result names no
// region (a file-level finding).
type Finding struct {
	File    string
	Line    int
	Rule    string
	Message string
}

// Runner runs one SAST scanner over files (root-relative; empty means the
// whole root) and returns its findings, already confined to root.
type Runner interface {
	Name() string
	Run(ctx context.Context, root string, files []string) ([]Finding, error)
}

type sarifLog struct {
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool struct {
		Driver struct {
			Rules []struct {
				ID string `json:"id"`
			} `json:"rules"`
		} `json:"driver"`
	} `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifResult struct {
	RuleID    string `json:"ruleId"`
	RuleIndex *int   `json:"ruleIndex"`
	Rule      struct {
		ID string `json:"id"`
	} `json:"rule"`
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
	Locations []struct {
		PhysicalLocation struct {
			ArtifactLocation struct {
				URI string `json:"uri"`
			} `json:"artifactLocation"`
			Region struct {
				StartLine int `json:"startLine"`
			} `json:"region"`
		} `json:"physicalLocation"`
	} `json:"locations"`
}

// ParseSARIF reads a SARIF 2.1.0 log and returns its results as findings
// confined to root, in document order. It takes each result's first location.
// dropped counts the results discarded because they had no location or their
// path is unsafe or does not name a regular file under root. A malformed
// document, a version other than 2.1.0, or an unresolvable root is an error.
func ParseSARIF(r io.Reader, root string) (findings []Finding, dropped int, err error) {
	if root == "" {
		return nil, 0, errors.New("sast: scan root is required")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, 0, fmt.Errorf("sast: resolve scan root: %w", err)
	}
	if realRoot, err = filepath.Abs(realRoot); err != nil {
		return nil, 0, fmt.Errorf("sast: resolve scan root: %w", err)
	}

	var doc sarifLog
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, 0, fmt.Errorf("sast: decode SARIF: %w", err)
	}
	if doc.Version != SARIFVersion {
		return nil, 0, fmt.Errorf("sast: SARIF version %q is not supported, want %s", doc.Version, SARIFVersion)
	}

	for _, run := range doc.Runs {
		for _, res := range run.Results {
			if len(res.Locations) == 0 {
				dropped++
				continue
			}
			loc := res.Locations[0].PhysicalLocation
			rel, ok := confine(realRoot, loc.ArtifactLocation.URI)
			if !ok {
				dropped++
				continue
			}
			line := loc.Region.StartLine
			if line < 0 {
				line = 0
			}
			findings = append(findings, Finding{
				File:    rel,
				Line:    line,
				Rule:    ruleID(run, res),
				Message: res.Message.Text,
			})
		}
	}
	return findings, dropped, nil
}

// ruleID resolves a result's rule: ruleId, then rule.id, then the driver
// rule at ruleIndex. Empty when none is present.
func ruleID(run sarifRun, res sarifResult) string {
	if res.RuleID != "" {
		return res.RuleID
	}
	if res.Rule.ID != "" {
		return res.Rule.ID
	}
	rules := run.Tool.Driver.Rules
	if res.RuleIndex != nil && *res.RuleIndex >= 0 && *res.RuleIndex < len(rules) {
		return rules[*res.RuleIndex].ID
	}
	return ""
}

// confine turns a SARIF artifact URI into a slash-separated path relative to
// realRoot, or reports false when the URI must be dropped. The URI is
// percent-decoded before any check, so "%2e%2e" is caught as "..".
func confine(realRoot, uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" {
		return "", false // includes every file:// URI: always absolute
	}
	p := u.Path
	if p == "" || strings.ContainsAny(p, "\\\x00") || path.IsAbs(p) || filepath.IsAbs(p) {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", false
		}
	}
	clean := path.Clean(p)
	if clean == "." {
		return "", false
	}

	// Symlink escape: the resolved file must still sit under the resolved
	// root. A file that does not exist cannot be checked and is dropped.
	real, err := filepath.EvalSymlinks(filepath.Join(realRoot, filepath.FromSlash(clean)))
	if err != nil {
		return "", false
	}
	relReal, err := filepath.Rel(realRoot, real)
	if err != nil || relReal == ".." || strings.HasPrefix(relReal, ".."+string(filepath.Separator)) {
		return "", false
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return clean, true
}
