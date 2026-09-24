package report

import "io"

// HomeViewAXI is the token-dense analogue of the human home view (axi.md
// Principle 8, "Content First"): the executable path, atcr's one-line
// description, and the current review identity/status. It carries no findings
// list, so — exactly like ReviewSummaryAXI — it is a single-row TOON payload
// sharing this package's one TOON encoder (encodeAXI) rather than a second,
// divergent serializer.
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

// RenderHomeViewAXI writes s as a single-row standard TOON tabular array
// (home[1]{...}:) through the same go-axi encoder as the findings payload, so the
// home view carries the same no-ANSI / no-Markdown structural guarantee. All four
// fields are strings; an empty review_id encodes as a quoted "" so a consumer
// reads it back as an empty string, not null.
func RenderHomeViewAXI(w io.Writer, s HomeViewAXI) error {
	doc, err := singleRowAXI("home", homeViewAXIHeader, []any{s.ExecPath, s.Description, s.ReviewID, s.ReviewStatus})
	if err != nil {
		return err
	}
	return encodeAXI(w, doc)
}
