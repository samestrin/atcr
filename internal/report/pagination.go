package report

import (
	"bytes"
	"io"

	"github.com/samestrin/atcr/internal/reconcile"
)

// AXIMaxLinesDefault is the default physical-line cap applied to an --axi
// payload (AC 03-01). ATCR_AXI_MAX_LINES overrides it; that env resolution lives
// in cmd/atcr (AC 03-03) and is threaded in as maxLines.
//
// The cap is header-inclusive: the array header consumes line 1, so a cap of N
// emits at most N-1 finding rows. An operator setting ATCR_AXI_MAX_LINES=N gets
// N-1 findings, not N — the knob bounds physical lines, not data rows.
const AXIMaxLinesDefault = 500

// PaginateAXI applies the deterministic line cap to an already-rendered legacy
// pipe AXI payload (RenderPipeAXIPaginated). The standard TOON path caps rows
// before encoding instead (RenderAXIPaginated), so a byte cut can never leave a
// header N a stock decoder would reject. It treats rendered as opaque text (the
// renderer has already stripped ANSI/control bytes) and caps it to at most
// maxLines physical lines.
//
// renderPipeAXI emits exactly one physical line per finding (a row never spans
// lines — see axiRow), so a physical-line cap is a row-boundary cap: the cut
// point always falls between whole rows and no row is split mid-line (AC 03-01
// Edge Case 4). The boundary is inclusive — a payload of exactly maxLines lines
// is NOT truncated (Edge Case 1).
//
// It returns the (possibly capped) payload, whether truncation occurred, and the
// true pre-truncation element count. That true total is what the array header's
// N already declares; it survives capping because the header is line 1 and is
// never dropped (AC 03-02). The returned total is derived from the pre-truncation
// physical row count, which equals the header's N by construction (renderPipeAXI
// emits exactly one line per finding) — it is a convenience for callers/tests,
// the header N remains the authoritative on-wire count. The cap is a single O(n)
// pass with no re-parsing or backtracking, and is a bounded,
// unconditionally-succeeding transform — it never returns an error and never
// changes the exit code (AC 03-01 Error Scenario 1); the no-error contract is
// enforced by the signature itself.
//
// maxLines should be >= 1; a non-positive value is nonsensical as a cap. Rather
// than trust the caller (PaginateAXI is exported and could be reached directly),
// it clamps any maxLines < 1 to AXIMaxLinesDefault — defense in depth mirroring
// the cmd-layer env resolver's fail-open (AC 03-03). This keeps the never-error /
// never-panic contract total for ALL inputs and guarantees the header line
// (line 1, carrying the true N) is never dropped (AC 03-02).
func PaginateAXI(rendered []byte, maxLines int) (out []byte, truncated bool, total int) {
	if maxLines < 1 {
		maxLines = AXIMaxLinesDefault
	}
	// SplitAfter keeps the terminating \n on each piece, so re-joining is exact
	// and the byte-for-byte passthrough below is guaranteed. renderPipeAXI always
	// \n-terminates, leaving a trailing empty segment we drop.
	lines := bytes.SplitAfter(rendered, []byte("\n"))
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 {
		lines = lines[:n-1]
	}
	// The array header is line 1; every remaining physical line is one data row
	// (renderPipeAXI's contract), so the pre-truncation row count is the true total.
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

// RenderAXIPaginated is the single shared --axi findings emission entry point
// used by the CLI (AC 03-04): both `atcr report --format axi` and any findings
// path of `atcr review --axi` call it rather than reimplementing truncation, so
// the two commands can never diverge in cap behavior.
//
// The cap is applied to ROWS, before encoding — never by cutting the encoded
// bytes. maxLines stays header-inclusive (the array header is line 1, so at most
// maxLines-1 rows are emitted) and a non-positive maxLines clamps to
// AXIMaxLinesDefault, mirroring PaginateAXI. Standard TOON escapes newlines
// inside strings, so every row is still exactly one physical line.
//
// CONSUMER CONTRACT (AC8, amending Sprint 31.0 AC 03-02 Edge Case 1): the array
// header declares N = rows actually emitted, so a truncated payload still
// round-trips through a stock TOON decoder. The true pre-truncation count rides
// the sibling `total: <int>` key and the capped state rides `truncated: <bool>`;
// both keys are present in EVERY payload, cut or uncut. The two closing lines
// are required structure beyond the maxLines content cap.
//
// The `truncated` field NAME matches internal/fanout/status.go's Truncated bool
// (json:"truncated") but the SEMANTICS differ: status.go marks byte-budget INPUT
// truncation, whereas this marks OUTPUT row-count capping — the shared name is a
// naming precedent, not a shared signal.
//
// The legacy pipe path keeps the old contract (header N = true total) via
// RenderPipeAXIPaginated; base Render(FormatAXI) stays the uncapped schema
// encoder with no total/truncated keys.
func RenderAXIPaginated(w io.Writer, findings []reconcile.JSONFinding, maxLines int) error {
	return encodeAXI(w, axiPaginatedDoc(findings, maxLines))
}

// axiPaginatedDoc caps findings to maxLines-1 rows (the header is line 1) and
// builds the paginated document. Split out so tests can run goaxi.Check over the
// exact value that is emitted.
func axiPaginatedDoc(findings []reconcile.JSONFinding, maxLines int) axiPaginatedPayload {
	if maxLines < 1 {
		maxLines = AXIMaxLinesDefault
	}
	total := len(findings)
	emitted := findings
	if limit := maxLines - 1; total > limit {
		emitted = findings[:limit]
	}
	return axiPaginatedPayload{
		// Columns are derived from the emitted rows, so a column whose only carrier
		// was cut is not declared as all-empty.
		Findings:  axiRows(emitted),
		Total:     total,
		Truncated: len(emitted) < total,
	}
}
