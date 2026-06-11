package stream

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// WriteSource writes findings in the per-source 8-column format (REVIEWER) with
// the v1 version header. Literal pipes inside any field are replaced with '/'
// so the column count stays stable.
func WriteSource(w io.Writer, findings []Finding) error {
	return write(w, findings, PerSourceColumns)
}

// WriteReconciled writes findings in the reconciled 9-column format (REVIEWERS
// joined + CONFIDENCE) with the v1 version header.
func WriteReconciled(w io.Writer, findings []Finding) error {
	return write(w, findings, ReconciledColumns)
}

func write(w io.Writer, findings []Finding, cols int) error {
	if _, err := fmt.Fprintln(w, Version); err != nil {
		return fmt.Errorf("writing findings header: %w", err)
	}
	for _, f := range findings {
		row := fieldsFor(f, cols)
		for i, c := range row {
			row[i] = escapeField(c)
		}
		if _, err := fmt.Fprintln(w, strings.Join(row, "|")); err != nil {
			return fmt.Errorf("writing finding: %w", err)
		}
	}
	return nil
}

// fieldsFor renders a Finding to its column slice for the chosen shape. Commas
// inside reviewer names are neutralized before joining so the comma-delimited
// REVIEWERS column cannot be forged into extra reviewers.
func fieldsFor(f Finding, cols int) []string {
	base := []string{
		f.Severity,
		fmt.Sprintf("%s:%d", f.File, f.Line),
		f.Problem,
		f.Fix,
		f.Category,
		strconv.Itoa(f.EstMinutes),
		f.Evidence,
	}
	if cols == ReconciledColumns {
		reviewers := make([]string, len(f.Reviewers))
		for i, r := range f.Reviewers {
			reviewers[i] = strings.ReplaceAll(r, ",", "/")
		}
		return append(base, strings.Join(reviewers, ","), f.Confidence)
	}
	return append(base, f.Reviewer)
}

// fieldReplacer neutralizes the characters that would break the one-finding-
// per-line, pipe-delimited contract: pipes become '/', and CR/LF become a
// space so an embedded newline can never split a finding across physical lines.
var fieldReplacer = strings.NewReplacer("|", "/", "\r\n", " ", "\r", " ", "\n", " ")

// escapeField makes s safe to write as a single pipe-delimited field (lossy but
// structurally stable).
func escapeField(s string) string {
	return fieldReplacer.Replace(s)
}

// AsReconciled migrates a per-source Finding (single Reviewer) into a reconciled
// Finding (Reviewers + Confidence), carrying the location and detail fields
// across. The reconciler uses this when collapsing a cluster so a finding's
// attribution is never silently dropped between the 8-col and 9-col shapes.
func (f Finding) AsReconciled(reviewers []string, confidence string) Finding {
	out := f
	out.Reviewer = ""
	out.Reviewers = reviewers
	out.Confidence = confidence
	return out
}
