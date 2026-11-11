// Package tddedupe reads the reviewer streams that `atcr dedupe` merges (epic
// 110), the replacement for `llm-support td-dedupe`. It reads; the clustering
// and merging stay in the reconcile library.
package tddedupe

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
	reconcile "github.com/samestrin/atcr/reconcile"
)

// versionPrefix starts every atcr-findings version header. A first line that
// carries it goes to stream.ParseSource, which reads v1 and v2 and rejects any
// other version.
const versionPrefix = "# atcr-findings/"

// legacyWarning is td-dedupe's text for a stream with no version header,
// prefixed with the stream's tag.
const legacyWarning = "no `" + stream.Version + "` header: parsing as a legacy stream. " +
	"Add the header so a consumer never silently reads incompatible data."

// minColumns is the narrowest row a headerless stream may carry:
// SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY.
const minColumns = 5

// ReadStream reads one td-stream file into reconcile findings.
//
// A stream whose first non-blank line is an atcr-findings v1 or v2 header is
// read by stream.ParseSource; any other version is an error. A stream with no
// header is read by the column names in its `# Format:` comment when it has one
// (a name with no home in a finding, such as ORIGIN, RISK_CLASS or BLOCKING, is
// dropped, never shifted into a neighbour), and otherwise by its 5, 6, 8 or 10
// column width; either way warning names the tag. A row with fewer than 5
// columns, or with an empty severity or file, is counted in skipped. An empty
// reviewer becomes tag, and every category goes through
// stream.CanonicalCategory (epic 110 D2).
func ReadStream(tag string, data []byte) (findings []reconcile.Finding, skipped int, warning string, err error) {
	lines := strings.Split(string(data), "\n")
	if strings.HasPrefix(firstNonBlank(lines), versionPrefix) {
		return readVersioned(tag, data)
	}

	warning = fmt.Sprintf("stream %q: %s", tag, legacyWarning)
	cols := formatColumns(lines)
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		var (
			f  reconcile.Finding
			ok bool
		)
		if cols != nil {
			f, ok = rowByName(t, cols)
		} else {
			f, ok = rowByWidth(t)
		}
		if !ok {
			skipped++
			continue
		}
		findings = append(findings, finish(f, f.Category, tag))
	}
	return findings, skipped, warning, nil
}

// readVersioned reads a v1 or v2 stream through stream.ParseSource.
func readVersioned(tag string, data []byte) ([]reconcile.Finding, int, string, error) {
	res, err := stream.ParseSource(data)
	if err != nil {
		return nil, 0, "", fmt.Errorf("stream %q: %w", tag, err)
	}
	skipped := len(res.Skipped)
	var out []reconcile.Finding
	for _, sf := range res.Findings {
		if sf.Severity == "" || sf.File == "" {
			skipped++
			continue
		}
		out = append(out, finish(fromStream(sf), sf.Category, tag))
	}
	return out, skipped, "", nil
}

// fromStream copies a per-source stream finding's wire fields. It is the same
// map as adapter.ToFinding, kept here so this package does not import
// internal/reconcile.
func fromStream(f stream.Finding) reconcile.Finding {
	return reconcile.Finding{
		Severity:   f.Severity,
		File:       f.File,
		Line:       f.Line,
		Problem:    f.Problem,
		Fix:        f.Fix,
		Category:   f.Category,
		EstMinutes: f.EstMinutes,
		Evidence:   f.Evidence,
		Reviewer:   f.Reviewer,
	}
}

// finish canonicalizes the category and fills an empty reviewer with the tag.
func finish(f reconcile.Finding, rawCategory, tag string) reconcile.Finding {
	f.Category = stream.CanonicalCategory(rawCategory)
	if f.Reviewer == "" {
		f.Reviewer = tag
	}
	return f
}

func firstNonBlank(lines []string) string {
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// formatColumns returns the upper-cased names a `# Format:` comment declares in
// the stream's leading comment block, or nil when there is none naming at least
// minColumns columns. Blank lines do not end the block.
func formatColumns(lines []string) []string {
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "#") {
			return nil
		}
		body := strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if !strings.HasPrefix(strings.ToUpper(body), "FORMAT:") {
			continue
		}
		cols := strings.Split(body[len("FORMAT:"):], "|")
		for i := range cols {
			cols[i] = strings.ToUpper(strings.TrimSpace(cols[i]))
		}
		if len(cols) >= minColumns {
			return cols
		}
	}
	return nil
}

func splitRow(line string) []string {
	f := strings.Split(line, "|")
	for i := range f {
		f[i] = strings.TrimSpace(f[i])
	}
	return f
}

// rowByName maps a row onto a finding by the stream's declared column names.
// A row shorter than the declaration leaves the missing columns empty.
func rowByName(line string, cols []string) (reconcile.Finding, bool) {
	f := splitRow(line)
	if len(f) < minColumns {
		return reconcile.Finding{}, false
	}
	var r reconcile.Finding
	for i, name := range cols {
		if i >= len(f) {
			break
		}
		switch name {
		case "SEVERITY":
			r.Severity = strings.ToUpper(f[i])
		case "FILE:LINE", "FILE_LINE", "FILE", "PATH":
			r.File, r.Line = splitFileLine(f[i])
		case "PROBLEM", "DESCRIPTION":
			r.Problem = f[i]
		case "FIX", "RECOMMENDED_FIX":
			r.Fix = f[i]
		case "CATEGORY":
			r.Category = f[i]
		case "EST_MINUTES", "EST":
			r.EstMinutes = estMinutes(f[i])
		case "EVIDENCE":
			r.Evidence = f[i]
		case "REVIEWER", "REVIEWERS":
			r.Reviewer = f[i]
		}
	}
	return r, r.Severity != "" && r.File != ""
}

// rowByWidth maps a row of a headerless stream with no `# Format:` comment.
// 5, 6 and 8 columns share SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY; 6 adds
// REVIEWER, 8 adds EST_MINUTES|EVIDENCE|REVIEWER. 10 is the legacy claude
// layout SEVERITY|ORIGIN|RISK_CLASS|FILE:LINE|PROBLEM|FIX|CATEGORY|EST|EVIDENCE|SOURCE,
// which carries no reviewer.
func rowByWidth(line string) (reconcile.Finding, bool) {
	f := splitRow(line)
	if len(f) < minColumns {
		return reconcile.Finding{}, false
	}
	var r reconcile.Finding
	if len(f) == 10 {
		r.Severity = strings.ToUpper(f[0])
		r.File, r.Line = splitFileLine(f[3])
		r.Problem, r.Fix, r.Category = f[4], f[5], f[6]
		r.EstMinutes = estMinutes(f[7])
		r.Evidence = f[8]
	} else {
		r.Severity = strings.ToUpper(f[0])
		r.File, r.Line = splitFileLine(f[1])
		r.Problem, r.Fix, r.Category = f[2], f[3], f[4]
		switch {
		case len(f) >= 8:
			r.EstMinutes = estMinutes(f[5])
			r.Evidence = f[6]
			r.Reviewer = f[7]
		case len(f) == 6:
			r.Reviewer = f[5]
		}
	}
	return r, r.Severity != "" && r.File != ""
}

// splitFileLine splits FILE:LINE on the last colon, as stream's v1 reader
// does: a missing or non-numeric line gives line 0 and keeps the whole column
// as the file.
func splitFileLine(s string) (string, int) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return s, 0
	}
	if idx == len(s)-1 {
		return s[:idx], 0
	}
	n, err := strconv.Atoi(s[idx+1:])
	if err != nil {
		return s, 0
	}
	return s[:idx], n
}

// maxEstMinutes is one week, the bound stream.ParseSource clamps v1 and v2
// rows to, so a legacy row reads the same as a versioned one.
const maxEstMinutes = 7 * 24 * 60

// estMinutes reads EST_MINUTES leniently, as td-dedupe did: a decimal rounds,
// anything unreadable or negative is 0, and anything over a week is a week.
func estMinutes(s string) int {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || math.IsNaN(v) {
		return 0
	}
	return int(math.Round(min(v, maxEstMinutes)))
}
