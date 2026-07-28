package payload

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/astgroup"
)

// Skeleton block markers. They deliberately avoid every prefix the rendered-
// payload splitter recognizes as a new file section (`diff --git`, `=== FILE: `,
// `[deleted file: `, `[binary file changed: `, `diff --cc`, `diff --combined`)
// and every prefix the diff path scanner reads (`---`, `+++`, `@@`), so a
// skeleton folds into the file body it belongs to instead of starting a section.
const (
	skeletonStart = ">>> SKELETON (HEAD) <<<"
	skeletonEnd   = ">>> END SKELETON <<<"

	// maxSkeletonHeaderBytes caps a single rendered declaration header. A
	// generated single-line declaration — a go-bindata `var _bindata =
	// []byte("…")`, an embedded asset table, minified generated Go — yields a
	// header of arbitrary length that astgroup.declHeader slices whole. Without
	// this cap the MaxSkeletonLines entry limit bounds the line COUNT but not the
	// bytes, so a handful of such entries could prepend megabytes to a one-line
	// diff, in every mode payload. Truncation is disclosed inline, never silent.
	maxSkeletonHeaderBytes = 512
	// maxSkeletonBlockBytes bounds the whole rendered skeleton block. It is the
	// backstop the per-header cap cannot provide once an operator raises
	// MaxSkeletonLines far above the default: even capped headers, enough of them,
	// still add up. Rendering stops when the block reaches this size and the
	// remainder is disclosed with the elision notice.
	maxSkeletonBlockBytes = 8 * 1024
	// skeletonHeaderTruncSuffix marks a header clipped by maxSkeletonHeaderBytes.
	skeletonHeaderTruncSuffix = "…(truncated)"
)

// skeletonEntry is one rendered declaration header. It mirrors
// astgroup.SkeletonEntry's rendering-relevant fields so the formatting helpers
// stay testable without standing up a wasm host.
type skeletonEntry struct {
	StartLine int
	Header    string
}

// renderSkeleton formats a file's extracted declaration headers as a payload
// block, or returns "" when there is nothing structural to show.
//
// Each header is anchored with its HEAD line number. The anchor doubles as a
// safety property: because every content line begins with "L<digits>: ", no
// rendered line can start with a payload section marker even if the source
// declaration somehow did.
func renderSkeleton(entries []skeletonEntry, maxLines int) string {
	if len(entries) == 0 || maxLines <= 0 {
		return ""
	}
	elided := 0
	if len(entries) > maxLines {
		elided = len(entries) - maxLines
		entries = entries[:maxLines]
	}
	var b strings.Builder
	b.WriteString(skeletonStart)
	b.WriteByte('\n')
	rendered := 0
	for _, e := range entries {
		// Stop before the block outgrows the ceiling. The line-count cap bounds the
		// number of entries but not their bytes; this bounds the bytes so a run
		// that raised MaxSkeletonLines cannot prepend an unbounded block.
		if b.Len() >= maxSkeletonBlockBytes {
			break
		}
		b.WriteByte('L')
		b.WriteString(strconv.Itoa(e.StartLine))
		b.WriteString(": ")
		// Collapse whitespace at the render boundary, not just upstream: a header
		// carrying an embedded newline + a payload marker would otherwise emit a
		// column-0 marker that splits the payload into a spoofed section. Folding
		// runs of whitespace to single spaces removes every newline, so a rendered
		// line can never begin with a marker regardless of the header's origin.
		b.WriteString(truncateHeader(strings.Join(strings.Fields(e.Header), " ")))
		b.WriteByte('\n')
		rendered++
	}
	// Fold any block-ceiling remainder into the same disclosed count as the
	// line-cap elision, so a partial map is never mistaken for a complete one.
	elided += len(entries) - rendered
	if elided > 0 {
		// Disclosed, never silent: a reviewer must know the map is partial rather
		// than conclude the file has no further declarations.
		b.WriteString("... ")
		b.WriteString(strconv.Itoa(elided))
		b.WriteString(" more declaration(s) elided\n")
	}
	b.WriteString(skeletonEnd)
	b.WriteByte('\n')
	return b.String()
}

// truncateHeader bounds a single declaration header to maxSkeletonHeaderBytes,
// appending skeletonHeaderTruncSuffix when it clips. The cut lands on a rune
// boundary so the result is always valid UTF-8. The rendered line still begins
// with "L<digits>: ", so truncation cannot make a line start with a payload
// section marker regardless of what the header contained.
func truncateHeader(h string) string {
	if len(h) <= maxSkeletonHeaderBytes {
		return h
	}
	cut := maxSkeletonHeaderBytes
	for cut > 0 && !utf8.RuneStart(h[cut]) {
		cut--
	}
	return h[:cut] + skeletonHeaderTruncSuffix
}

// injectSkeleton splices skel into body immediately after body's first line.
//
// After, not before: the rendered-payload splitter decides where a file section
// begins by testing a line against isRenderedEntryStart, and it reads the path
// from that section's FIRST line. Keeping the original marker line at index 0
// leaves both behaviours untouched, so the skeleton is invisible to the
// round-trip parser and visible to the reviewer.
func injectSkeleton(body, skel string) string {
	if skel == "" || body == "" {
		return body
	}
	nl := strings.IndexByte(body, '\n')
	if nl < 0 {
		return body + "\n" + skel
	}
	return body[:nl+1] + skel + body[nl+1:]
}

// fileContext is the result of the per-file measurement and skeleton pass.
type fileContext struct {
	signals  fileSignals
	skeleton string
}

// analyzeFile measures a changed file's escalation signals and extracts its HEAD
// skeleton. ok is false when the file cannot contribute signals at all (deleted,
// or its HEAD blob is unreadable), in which case the caller leaves the file in
// the run's configured mode.
//
// Reading HEAD costs one `git show` per file, memoized per range. This is why
// the whole pass is gated behind EscalationConfig.Enabled: in diff and blocks
// mode the builder otherwise spends a constant number of git processes
// regardless of change-set size.
func (g *gitRunner) analyzeFile(base, head string, f changedFile) (fileContext, bool) {
	if f.kind == kindDeleted {
		return fileContext{}, false
	}
	// A binary file has no reviewable text: it renders as a one-line marker in
	// every mode. Reading its blob to measure churn would pull the whole binary
	// into memory for a decision that cannot change anything.
	if bin, err := g.isBinary(base, head, f.pathspec()...); err != nil || bin {
		g.log().Debug("payload: skipping escalation analysis", "path", f.path, "binary", bin, "error", err)
		return fileContext{}, false
	}
	// Every hunk, deletions included: a rewrite that mostly removes code is
	// exactly the architectural-thrashing case escalation exists to catch, and
	// the head-side ranges alone cannot see it.
	hunks, err := g.allHunkRanges(base, head, f.pathspec()...)
	if err != nil {
		g.log().Debug("payload: skipping escalation analysis, hunk range parse failed", "path", f.path, "error", err)
		return fileContext{}, false
	}
	churn, _, err := g.churnLines(base, head, f.path)
	if err != nil {
		g.log().Debug("payload: skipping escalation analysis, churn lookup failed", "path", f.path, "error", err)
		return fileContext{}, false
	}
	src, err := g.headContentMemo(base, head, f.path)
	if err != nil {
		// A file present in the diff but unreadable at HEAD is not fatal: the
		// review still runs, this file just keeps its configured mode.
		g.logger.Debug("payload: skipping escalation analysis, HEAD blob unreadable", "path", f.path, "error", err)
		return fileContext{}, false
	}

	ctx := fileContext{signals: fileSignals{
		changedLines: churn,
		headLines:    countLines(src),
		hunks:        hunks,
	}}
	// A newly added file's diff already contains every line of the file, so its
	// churn ratio is definitionally 1.0 and carries no information — left in, it
	// would escalate every added file while buying the reviewer nothing. Zeroing
	// headLines suppresses only the churn signal; hunk count, adjacency, and
	// complexity still apply.
	if f.kind == kindAdded {
		ctx.signals.headLines = 0
	}

	// Skeleton extraction and the McCabe signal share one parse. Go only for now:
	// the header-slicing rule is written against Go's func/gendecl shape.
	if astgroup.LanguageForExt(strings.ToLower(filepath.Ext(f.path))) != "go" {
		return ctx, true
	}
	root, err := parseHeadTree(src)
	if err != nil {
		g.logger.Debug("payload: skipping AST signals, parse failed", "path", f.path, "error", err)
		return ctx, true
	}
	// Scope complexity to the functions the change actually TOUCHED, not the
	// file-wide maximum: a one-line edit in a trivial helper must not escalate
	// because some unrelated function elsewhere in the file is branchy (Epic 35.1).
	ctx.signals.cyclomatic = astgroup.MaxFuncCyclomaticInRanges(root, hunkRangePairs(hunks))
	ctx.skeleton = renderSkeleton(toSkeletonEntries(astgroup.FileSkeleton(root, src)), g.escalation.MaxSkeletonLines)
	return ctx, true
}

// hunkRangePairs converts the head-side hunk line ranges into the 1-based
// inclusive [start, end] pairs astgroup.MaxFuncCyclomaticInRanges expects.
func hunkRangePairs(hunks []lineRange) [][2]int {
	if len(hunks) == 0 {
		return nil
	}
	out := make([][2]int, len(hunks))
	for i, h := range hunks {
		out[i] = [2]int{h.start, h.end}
	}
	return out
}

// parseHeadTree parses src with the shared wasm host's Go parser.
func parseHeadTree(src string) (astgroup.Node, error) {
	p, err := astgroup.SharedHost().Parser("go")
	if err != nil {
		return astgroup.Node{}, err
	}
	return p.Parse([]byte(src))
}

func toSkeletonEntries(in []astgroup.SkeletonEntry) []skeletonEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]skeletonEntry, 0, len(in))
	for _, e := range in {
		out = append(out, skeletonEntry{StartLine: e.StartLine, Header: e.Header})
	}
	return out
}

// countLines counts the lines in src, not counting a trailing newline as an
// extra empty line.
func countLines(src string) int {
	if src == "" {
		return 0
	}
	n := strings.Count(src, "\n")
	if !strings.HasSuffix(src, "\n") {
		n++
	}
	return n
}
