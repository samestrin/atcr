package report

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// HomeViewAXI is the token-dense analogue of the human home view (axi.md
// Principle 8, "Content First"): the executable path, atcr's one-line
// description, and the current review identity/status. It carries no findings
// list, so — exactly like ReviewSummaryAXI — it is a single-row TOON payload
// sharing this package's one TOON encoder (toonQuote/axiDelim) rather than a
// second, divergent serializer.
type HomeViewAXI struct {
	ExecPath     string
	Description  string
	ReviewID     string // "" when no review has run yet
	ReviewStatus string // "none" when no review has run yet
}

// homeViewAXIHeader is the fixed column order of the home-view payload. Kept as
// one slice so the header line and the row are guaranteed the same width and
// order — the same defensive invariant renderAXI/RenderReviewSummaryAXI enforce.
var homeViewAXIHeader = []string{"exec_path", "description", "review_id", "review_status"}

// RenderHomeViewAXI writes s as a single-row TOON tabular array
// (home[1|]{...}:) reusing the axi findings encoder's pipe delimiter and
// must-quote rules (toonQuote), so the home-view payload carries the same
// no-ANSI / no-Markdown structural guarantee as renderAXI and stays a faithful,
// agent-consumable re-encoding. All four fields are free text and go through
// toonQuote (an empty review_id becomes a quoted "" so a consumer reads it back
// as an empty string, not null).
func RenderHomeViewAXI(w io.Writer, s HomeViewAXI) error {
	var b bytes.Buffer
	quotedHeader := make([]string, len(homeViewAXIHeader))
	for i, h := range homeViewAXIHeader {
		quotedHeader[i] = toonQuote(h)
	}
	fmt.Fprintf(&b, "home[1%c]{%s}:\n", axiDelim, strings.Join(quotedHeader, string(axiDelim)))

	row := []string{
		toonQuote(s.ExecPath),
		toonQuote(s.Description),
		toonQuote(s.ReviewID),
		toonQuote(s.ReviewStatus),
	}
	// Same defensive width invariant renderAXI/RenderReviewSummaryAXI enforce: the
	// row must carry exactly as many columns as the header declares. It cannot trip
	// on valid input (both are fixed at 4) but fails deterministically if a future
	// edit adds a header column without a matching row cell (or vice versa) rather
	// than silently emitting a misaligned payload.
	if len(row) != len(homeViewAXIHeader) {
		return fmt.Errorf("axi encoder: home view row has %d columns, header declares %d", len(row), len(homeViewAXIHeader))
	}
	b.WriteString("  ")
	b.WriteString(strings.Join(row, string(axiDelim)))
	b.WriteByte('\n')
	_, err := w.Write(b.Bytes())
	return err
}
