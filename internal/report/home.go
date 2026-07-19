package report

import (
	"fmt"
	"io"
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

// RenderHomeViewAXI writes s as a single-row TOON tabular array. STUB (T3 RED):
// emits only the array header, no data row.
func RenderHomeViewAXI(w io.Writer, s HomeViewAXI) error {
	_, err := fmt.Fprintln(w, "home[1|]{}:")
	return err
}
