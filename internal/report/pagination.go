package report

import (
	"bytes"
	"fmt"
	"io"

	"github.com/samestrin/atcr/internal/reconcile"
)

// AXIMaxLinesDefault is the default physical-line cap applied to an --axi
// payload (AC 03-01). ATCR_AXI_MAX_LINES overrides it; that env resolution lives
// in cmd/atcr (AC 03-03) and is threaded in as maxLines.
const AXIMaxLinesDefault = 500

// PaginateAXI applies the deterministic line cap to an already-rendered AXI
// payload. It treats rendered as opaque text (Story 1's renderer has already
// stripped ANSI/control bytes) and caps it to at most maxLines physical lines.
//
// renderAXI emits exactly one physical line per finding (a row never spans
// lines — see axiRow), so a physical-line cap is a row-boundary cap: the cut
// point always falls between whole rows and no row is split mid-line (AC 03-01
// Edge Case 4). The boundary is inclusive — a payload of exactly maxLines lines
// is NOT truncated (Edge Case 1).
//
// It returns the (possibly capped) payload, whether truncation occurred, and the
// true pre-truncation element count. That true total is what the array header's
// N already declares; it survives capping because the header is line 1 and is
// never dropped (AC 03-02). The cap is a single O(n) pass with no re-parsing or
// backtracking, and is a bounded, unconditionally-succeeding transform — it
// never returns an error and never changes the exit code (AC 03-01 Error
// Scenario 1); the no-error contract is enforced by the signature itself.
//
// maxLines is expected to be >= 1 (the caller's env resolver fails open to
// AXIMaxLinesDefault for any non-positive value, AC 03-03); PaginateAXI does not
// re-validate it.
func PaginateAXI(rendered []byte, maxLines int) (out []byte, truncated bool, total int) {
	// SplitAfter keeps the terminating \n on each piece, so re-joining is exact
	// and the byte-for-byte passthrough below is guaranteed. renderAXI always
	// \n-terminates, leaving a trailing empty segment we drop.
	lines := bytes.SplitAfter(rendered, []byte("\n"))
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 {
		lines = lines[:n-1]
	}
	// The array header is line 1; every remaining physical line is one data row
	// (renderAXI's contract), so the pre-truncation row count is the true total.
	total = len(lines) - 1
	if total < 0 {
		total = 0
	}
	if len(lines) <= maxLines {
		// Under/at the cap: return the input verbatim so the payload is
		// byte-identical to the unwrapped renderer output (AC 03-01 Scenario 1).
		return rendered, false, total
	}
	return bytes.Join(lines[:maxLines], nil), true, total
}

// RenderAXIPaginated is the single shared --axi emission entry point used by the
// CLI (AC 03-04): it renders findings via the FormatAXI encoder, applies the
// maxLines line cap (PaginateAXI), and writes the capped payload followed by a
// `truncated: <bool>` metadata line. Both `atcr report --axi` and any findings
// path of `atcr review --axi` call this rather than reimplementing truncation,
// so the two commands can never diverge in cap behavior.
//
// The `truncated` field is emitted in every payload (AC 03-02): it is the
// "required closing structure" AC 03-01 Scenario 2 permits beyond the maxLines
// content cap, so the content line count stays exactly maxLines when truncated.
// The array header's declared N (the true, pre-truncation total) is preserved by
// PaginateAXI, so a consumer reads the true count from the header and the capped
// state from `truncated`.
//
// This deliberately does NOT alter the base Render(FormatAXI)/renderAXI output or
// the report.axi golden — the un-paginated encoder remains the schema fixture;
// pagination + the truncated flag are the CLI dispatch step wired here.
func RenderAXIPaginated(w io.Writer, findings []reconcile.JSONFinding, maxLines int) error {
	var buf bytes.Buffer
	if err := renderAXI(&buf, findings); err != nil {
		return err
	}
	out, truncated, _ := PaginateAXI(buf.Bytes(), maxLines)
	if _, err := w.Write(out); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "truncated: %t\n", truncated)
	return err
}
