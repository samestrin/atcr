package stream

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is the required first non-blank line of every findings file. Unknown
// versions are a hard error so a consumer never silently parses incompatible
// data.
const Version = "# atcr-findings/v1"

// versionPrefix matches any atcr-findings version header so a wrong version can
// be reported distinctly from a missing one.
const versionPrefix = "# atcr-findings/"

// versionTokenRe matches a well-formed version token (e.g. "v1", "v10") after
// versionPrefix. Only a well-formed token earns ErrUnknownVersion; a garbage
// suffix ("v1x", "v1.2") is a malformed header, not an unsupported version.
var versionTokenRe = regexp.MustCompile(`^v[0-9]+$`)

// Column counts for the two stream shapes.
const (
	PerSourceColumns  = 8 // ...|EVIDENCE|REVIEWER
	ReconciledColumns = 9 // ...|EVIDENCE|REVIEWERS|CONFIDENCE
)

// severityRe anchors a finding line at a valid severity prefix. Lines that do
// not match — comments, blanks, model prose mentioning "HIGH" mid-sentence —
// are skipped. This is the format's core contract: prose never becomes a row.
var severityRe = regexp.MustCompile(`^(CRITICAL|HIGH|MEDIUM|LOW)\|`)

// Sentinel errors for header problems (the only fatal parse failures; malformed
// rows are skipped, not fatal, per AC 01-05).
var (
	ErrMissingHeader  = errors.New("missing version header")
	ErrUnknownVersion = errors.New("unknown findings version")
)

// Finding is one normalized finding. Reviewer holds the per-source 8th column;
// Reviewers and Confidence hold the reconciled 8th/9th columns. A given Finding
// is populated by whichever parser produced it.
type Finding struct {
	Severity   string
	File       string
	Line       int
	Problem    string
	Fix        string
	Category   string
	EstMinutes int
	Evidence   string
	Reviewer   string   // per-source 8th column
	Reviewers  []string // reconciled 8th column
	Confidence string   // reconciled 9th column
}

// SkippedRow records a line skipped as malformed (wrong column count), with its
// 1-based line number, so callers can warn without failing the whole parse.
type SkippedRow struct {
	Line    int
	Content string
	Reason  string
}

// ParseResult carries the findings plus any malformed rows that were skipped.
type ParseResult struct {
	Findings []Finding
	Skipped  []SkippedRow
}

// ModelColumns is the column count a reviewer model emits: the per-source shape
// minus the trailing REVIEWER, which the engine appends from the agent name.
const ModelColumns = 7 // SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE

// ParseModelOutput extracts findings from a model's raw review text. Unlike
// ParseSource it requires no version header — models emit finding rows inline
// among prose — and it reads exactly the 7 persona columns (SEVERITY..EVIDENCE).
// Any 8th-or-later field a model emits is dropped, so a model can never
// self-attribute a REVIEWER: the engine sets Finding.Reviewer from the agent
// name afterward (TD-016). Non-severity-prefixed lines, blanks, and comments are
// skipped; short rows are padded. The returned findings have an empty Reviewer.
func ParseModelOutput(data []byte) []Finding {
	var out []Finding
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !severityRe.MatchString(line) {
			continue // prose
		}
		fields := strings.Split(line, "|")
		// A real finding has at least SEVERITY|FILE:LINE|PROBLEM with a location;
		// drop degenerate severity-prefixed noise like a bare "HIGH|".
		if len(fields) < 3 || strings.TrimSpace(fields[1]) == "" {
			continue
		}
		// Any overflow past the 7 persona columns — a model-supplied REVIEWER or
		// an unescaped pipe inside EVIDENCE — is folded back into the EVIDENCE
		// field rather than dropped. This keeps evidence text intact AND makes
		// REVIEWER forgery impossible: a model can never land a value in the
		// REVIEWER slot, which the engine fills from the agent name.
		if len(fields) > ModelColumns {
			fields[ModelColumns-1] = strings.Join(fields[ModelColumns-1:], "/")
			fields = fields[:ModelColumns]
		}
		// Pad to the per-source width so the REVIEWER slot exists but stays empty
		// until the engine fills it.
		for len(fields) < PerSourceColumns {
			fields = append(fields, "")
		}
		out = append(out, fieldsToFinding(fields, PerSourceColumns))
	}
	return out
}

// ParseSource parses a per-source (8-column) findings file.
func ParseSource(data []byte) (ParseResult, error) {
	return parse(data, PerSourceColumns)
}

// ParseReconciled parses a reconciled (9-column) findings file. Nothing in the
// pipeline re-ingests the reconciled findings.txt today; annotations folded
// into EVIDENCE at write time stay folded (see Finding.AsReconciled).
func ParseReconciled(data []byte) (ParseResult, error) {
	return parse(data, ReconciledColumns)
}

// parse validates the version header then extracts finding rows. Short rows are
// padded to cols with empty strings; rows with MORE than cols columns are
// recorded as skipped (an unescaped pipe leaked a column) rather than silently
// misaligning fields. Comment and prose lines are skipped.
func parse(data []byte, cols int) (ParseResult, error) {
	var res ParseResult
	headerSeen := false

	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !headerSeen {
			switch {
			case strings.TrimSpace(line) == Version:
				headerSeen = true
				continue
			case strings.HasPrefix(line, versionPrefix) && versionTokenRe.MatchString(strings.TrimSpace(strings.TrimPrefix(line, versionPrefix))):
				return res, fmt.Errorf("%w: %q (want %q)", ErrUnknownVersion, strings.TrimSpace(line), Version)
			default:
				return res, fmt.Errorf("%w: first line must be %q", ErrMissingHeader, Version)
			}
		}
		if strings.HasPrefix(line, "#") {
			continue // comment
		}
		if !severityRe.MatchString(line) {
			continue // prose
		}
		fields := strings.Split(line, "|")
		// A trailing pipe yields an extra empty column; treat trailing empties
		// as padding so a valid finding written with a trailing '|' is not lost.
		for len(fields) > cols && fields[len(fields)-1] == "" {
			fields = fields[:len(fields)-1]
		}
		if len(fields) > cols {
			res.Skipped = append(res.Skipped, SkippedRow{
				Line:    i + 1,
				Content: line,
				Reason:  fmt.Sprintf("expected %d columns, got %d", cols, len(fields)),
			})
			continue
		}
		for len(fields) < cols {
			fields = append(fields, "")
		}
		res.Findings = append(res.Findings, fieldsToFinding(fields, cols))
	}

	if !headerSeen {
		return res, fmt.Errorf("%w: first line must be %q", ErrMissingHeader, Version)
	}
	return res, nil
}

// fieldsToFinding maps a padded column slice to a Finding. cols selects the
// per-source vs reconciled tail (REVIEWER vs REVIEWERS+CONFIDENCE).
func fieldsToFinding(f []string, cols int) Finding {
	file, line := splitFileLine(f[1])
	fnd := Finding{
		Severity:   f[0],
		File:       file,
		Line:       line,
		Problem:    f[2],
		Fix:        f[3],
		Category:   f[4],
		EstMinutes: atoiOrZero(f[5]),
		Evidence:   f[6],
	}
	if cols == ReconciledColumns {
		fnd.Reviewers = splitReviewers(f[7])
		fnd.Confidence = f[8]
	} else {
		fnd.Reviewer = f[7]
	}
	return fnd
}

// splitFileLine splits a FILE:LINE column on the last colon. A missing or
// non-numeric line yields line 0 with the whole column kept as the file, so a
// path that happens to contain a colon is never lost.
func splitFileLine(s string) (string, int) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return s, 0
	}
	suffix := s[idx+1:]
	if suffix == "" {
		return s[:idx], 0 // bare trailing colon: drop it, line 0
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return s, 0
	}
	return s[:idx], n
}

// splitReviewers splits and trims a comma-joined REVIEWERS column, dropping
// empty entries.
func splitReviewers(s string) []string {
	var out []string
	for _, r := range strings.Split(s, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// atoiOrZero parses an integer, defaulting to 0 (EST_MINUTES is best-effort).
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
