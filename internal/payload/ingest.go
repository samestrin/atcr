package payload

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DefaultMaxDiffBytes is the default size cap for diff-file reads, mirroring
// benchmark.MaxDiffBytes (10 MiB): a hostile or accidental multi-GB diff in
// externally-sourced input must not cause unbounded allocation. A maxBytes <= 0
// passed to BuildEntriesFromDiffFile means unlimited.
const DefaultMaxDiffBytes int64 = 10 * 1024 * 1024

// Section-boundary and file-header markers. A `git diff` patch delimits files
// with a `diff --git ` line; a loose unified diff (the suite-fixture format)
// has no such line and is delimited by the `--- `/`+++ `/`@@ ` header triple.
const (
	gitDiffMarker = "diff --git "
	oldFileMarker = "--- "
	newFileMarker = "+++ "
	hunkMarker    = "@@ "
	devNull       = "/dev/null"
)

// BuildEntriesFromDiff parses unified diff text into per-file FileEntry values —
// the same []FileEntry shape BuildEntries(ModeDiff, ...) returns. The mapping is
// round-trip byte-identical: concatenating the returned bodies (as joinEntries
// does) reproduces diffText exactly, so an ingested diff reviews on the same
// modePayload path a git-sourced one does.
//
// It accepts both full `git diff` patches (split on `diff --git ` boundaries)
// and loose `--- `/`+++ `/`@@ ` unified diffs. Bodies are preserved verbatim (no
// trailing-newline normalization, unlike the git-sourced builder) so the
// round-trip is exact. An empty or whitespace-only diff yields zero entries; the
// caller (the fanout ingestion entry) maps that to ErrNoReviewableContent.
func BuildEntriesFromDiff(diffText string) ([]FileEntry, error) {
	if strings.TrimSpace(diffText) == "" {
		return []FileEntry{}, nil
	}
	starts, err := fileSectionStarts(diffText)
	if err != nil {
		return nil, err
	}
	entries := make([]FileEntry, 0, len(starts))
	for k, s := range starts {
		end := len(diffText)
		if k+1 < len(starts) {
			end = starts[k+1]
		}
		body := diffText[s:end]
		path, err := diffSectionPath(body)
		if err != nil {
			return nil, err
		}
		entries = append(entries, FileEntry{Path: path, Size: int64(len(body)), Body: body})
	}
	return entries, nil
}

// BuildEntriesFromDiffFile reads a diff file (bounded by maxBytes, rejecting
// absolute paths and `..` traversal — mirroring the suite manifest's
// isSafeRelPath guard) and delegates to BuildEntriesFromDiff. A maxBytes <= 0
// disables the cap. Callers holding an absolute path (e.g. a `git diff` redirected
// to /tmp) read the bytes themselves and call BuildEntriesFromDiff directly; the
// path guard here is intentionally strict for the relative-path ingestion case.
func BuildEntriesFromDiffFile(path string, maxBytes int64) ([]FileEntry, error) {
	if !isSafeDiffPath(path) {
		return nil, fmt.Errorf("diff ingestion: refusing unsafe diff path %q: must be a relative path within the working tree (no absolute paths, no .. traversal)", path)
	}
	// isSafeDiffPath is purely lexical: a relative, in-tree path can still be (or
	// traverse) a symlink pointing outside the working tree. Resolve symlinks and
	// reject an escape before os.Open follows the link to an external file.
	if err := rejectDiffSymlinkEscape(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("diff ingestion: opening diff file: %w", err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("diff ingestion: stat diff file %q: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("diff ingestion: diff file %q is not a regular file", path)
	}
	if maxBytes > 0 && fi.Size() > maxBytes {
		return nil, fmt.Errorf("diff ingestion: diff file %q size %d exceeds max %d bytes", path, fi.Size(), maxBytes)
	}
	var r io.Reader = f
	if maxBytes > 0 {
		// Bound the read independently of the pre-read Stat so a file that grows
		// between Stat and read still cannot exceed the cap (TOCTOU defense).
		r = io.LimitReader(f, maxBytes+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("diff ingestion: reading diff file %q: %w", path, err)
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("diff ingestion: diff file %q exceeds max %d bytes", path, maxBytes)
	}
	return BuildEntriesFromDiff(string(data))
}

// hunkCountsRe captures the old-side and new-side line counts from a unified-diff
// hunk header `@@ -a,b +c,d @@`. Group 1 is b (old count), group 2 is d (new
// count); each defaults to 1 when its `,count` is absent.
var hunkCountsRe = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

// fileSectionStarts returns the byte offsets at which each per-file section
// begins, covering the whole input contiguously so the sections partition
// diffText with no loss (the guarantee behind round-trip identity). It detects
// `git diff` format (boundaries on column-0 `diff --git ` lines, unspoofable
// because every hunk-body line carries a +/-/space prefix) when any such line is
// present, else loose format (delegated to looseSectionStarts, which counts hunk
// body lines so a `--- `/`+++ ` body line cannot be mistaken for a header). The
// first section must start at offset 0; leading preamble is an error rather than
// silently-dropped bytes.
func fileSectionStarts(diff string) ([]int, error) {
	lines, offsets := splitLinesWithOffsets(diff)

	gitMode := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, gitDiffMarker) {
			gitMode = true
			break
		}
	}

	if !gitMode {
		return looseSectionStarts(lines, offsets)
	}

	var starts []int
	for i, ln := range lines {
		if strings.HasPrefix(ln, gitDiffMarker) {
			starts = append(starts, offsets[i])
		}
	}
	if len(starts) == 0 {
		return nil, fmt.Errorf("diff ingestion: no file sections found (expected a `diff --git` line)")
	}
	if starts[0] != 0 {
		return nil, fmt.Errorf("diff ingestion: unexpected content before the first file section (would be lost on round-trip)")
	}
	return starts, nil
}

// looseSectionStarts finds the per-file section offsets in a loose diff (no
// `diff --git` line) by walking it structurally: each section is a `--- `/`+++ `
// header pair followed by one or more `@@ ` hunks, and each hunk body is consumed
// by its declared old/new line counts. Counting the body is what makes a removed
// line rendering `--- X` or an added line `+++ Y` — even one sitting immediately
// before the next hunk's `@@` header — get consumed as body and never mistaken
// for the next file's header. The first line must be a header (so starts[0] == 0,
// preserving round-trip identity); anything else is an error.
func looseSectionStarts(lines []string, offsets []int) ([]int, error) {
	var starts []int
	n := len(lines)
	i := 0
	for i < n {
		// Tolerate trailing empty lines produced by final newline(s): a diff
		// ending in `\n` leaves one empty line after splitting, `\n\n` leaves two,
		// and so on. When only empty lines remain, the sections are complete.
		if lines[i] == "" && allEmpty(lines, i) {
			break
		}
		if !looseHeaderAt(lines, i) {
			if len(starts) == 0 {
				return nil, fmt.Errorf("diff ingestion: no file sections found (expected a `--- `/`+++ `/`@@ ` header triple)")
			}
			return nil, fmt.Errorf("diff ingestion: unexpected content at line %d (not a file header or hunk)", i+1)
		}
		starts = append(starts, offsets[i])
		i += 2 // consume the `--- ` and `+++ ` header lines

		for i < n && strings.HasPrefix(lines[i], hunkMarker) {
			oldRem, newRem := hunkLineCounts(lines[i])
			hunkLine := i
			i++ // past the `@@ ` header
			for i < n && (oldRem > 0 || newRem > 0) {
				// A bare `@@ ` line is never a hunk-body line (every body line is
				// prefixed -/+/space/\), so reaching one while body budget remains
				// means this hunk header over-claimed its line count — a malformed or
				// hostile loose diff inflating a count to swallow the following hunk
				// or file. Reject it loudly: a silent merge would still round-trip
				// byte-for-byte, hiding the corruption from the contract test. (A
				// count tuned to land exactly on a following `--- `/`+++ ` pair
				// without crossing a bare `@@ ` is an unresolved residual; the bare
				// `@@ ` boundary is the only marker unambiguously not body content.)
				if strings.HasPrefix(lines[i], hunkMarker) {
					return nil, fmt.Errorf("diff ingestion: hunk at line %d claims more body lines than the section contains (malformed or hostile diff)", hunkLine+1)
				}
				switch {
				case strings.HasPrefix(lines[i], "-"):
					oldRem--
				case strings.HasPrefix(lines[i], "+"):
					newRem--
				case strings.HasPrefix(lines[i], `\`):
					// "\ No newline at end of file" — counts toward neither side.
				default:
					// Context line (space-prefixed, or a blank line): both sides.
					oldRem--
					newRem--
				}
				i++
			}
			// A `\ No newline at end of file` marker trails its hunk's counted
			// body lines (it counts toward neither side, so the budget loop above
			// has already exited). Consume any such markers into this hunk so they
			// are not mistaken for content after the section — which would abort an
			// otherwise valid loose diff, or merge it with the next file.
			for i < n && strings.HasPrefix(lines[i], `\`) {
				i++
			}
		}
	}
	if len(starts) == 0 {
		return nil, fmt.Errorf("diff ingestion: no file sections found (expected a `--- `/`+++ `/`@@ ` header triple)")
	}
	return starts, nil
}

// allEmpty reports whether every line from index i onward is empty — the
// trailing blank lines a final newline (or several) leaves after splitting.
func allEmpty(lines []string, i int) bool {
	for ; i < len(lines); i++ {
		if lines[i] != "" {
			return false
		}
	}
	return true
}

// looseHeaderAt reports whether a loose-diff file header (`--- `/`+++ `/`@@ `
// triple) begins at line i.
func looseHeaderAt(lines []string, i int) bool {
	n := len(lines)
	return strings.HasPrefix(lines[i], oldFileMarker) &&
		i+1 < n && strings.HasPrefix(lines[i+1], newFileMarker) &&
		i+2 < n && strings.HasPrefix(lines[i+2], hunkMarker)
}

// hunkLineCounts returns the old-side and new-side body line counts declared by a
// `@@ -a,b +c,d @@` hunk header, each defaulting to 1 when its count is absent. A
// header that does not parse defaults to (1, 1): real diffs always carry a
// well-formed hunk header, and the loose walk only reaches here on a line already
// beginning with `@@ `.
func hunkLineCounts(header string) (oldCount, newCount int) {
	oldCount, newCount = 1, 1
	m := hunkCountsRe.FindStringSubmatch(header)
	if m == nil {
		return oldCount, newCount
	}
	if m[1] != "" {
		oldCount, _ = strconv.Atoi(m[1])
	}
	if m[2] != "" {
		newCount, _ = strconv.Atoi(m[2])
	}
	return oldCount, newCount
}

// splitLinesWithOffsets splits s on '\n' into lines (newline excluded) and the
// byte offset where each line begins. A trailing newline yields a final empty
// line at offset len(s), which matches no marker.
func splitLinesWithOffsets(s string) (lines []string, offsets []int) {
	// Pre-size both slices to the line count (newline count + 1) so a large
	// multi-file diff fills them without repeated grow/realloc. strings.Count is
	// allocation-free (unlike bytes.Count, which would copy s).
	n := strings.Count(s, "\n") + 1
	lines = make([]string, 0, n)
	offsets = make([]int, 0, n)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			offsets = append(offsets, start)
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	offsets = append(offsets, start)
	return lines, offsets
}

// diffSectionPath derives the head-side path for one file section. It prefers
// the new path (`+++ b/<path>`); for a deletion (`+++ /dev/null`) it falls back
// to the old path (`--- a/<path>`); for a header-only git section with no
// `---`/`+++` lines (binary/mode-only) it parses the `b/` path from the
// `diff --git ` line. The first `--- `/`+++ ` lines are the headers (they precede
// any hunk body), so a later `--- `/`+++ ` removed/added line cannot shadow them.
func diffSectionPath(section string) (string, error) {
	var oldPath, newPath, gitHeader string
	// Walk the section line-by-line with IndexByte rather than strings.Split, which
	// would allocate a slice sized to the whole section just to read its header.
	for rest := section; rest != ""; {
		ln := rest
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			ln, rest = rest[:nl], rest[nl+1:]
		} else {
			rest = ""
		}
		if strings.HasPrefix(ln, hunkMarker) {
			break // the hunk body begins here; the git/old/new headers all precede it
		}
		switch {
		case gitHeader == "" && strings.HasPrefix(ln, gitDiffMarker):
			gitHeader = ln
		case newPath == "" && strings.HasPrefix(ln, newFileMarker):
			newPath = parseDiffPathField(ln[len(newFileMarker):])
		case oldPath == "" && strings.HasPrefix(ln, oldFileMarker):
			oldPath = parseDiffPathField(ln[len(oldFileMarker):])
		}
	}
	var path string
	switch {
	case newPath != "" && newPath != devNull:
		path = newPath
	case oldPath != "" && oldPath != devNull:
		path = oldPath
	case gitHeader != "":
		path = headPathFromGitHeader(gitHeader)
	}
	if path == "" {
		return "", fmt.Errorf("diff ingestion: cannot determine file path for section: %s", firstLine(section))
	}
	// The path comes from untrusted diff content; reject one that escapes the
	// working tree so a hostile `+++ b/../../etc/passwd` header never reaches a
	// FileEntry a downstream consumer might resolve. (Provenance only today, but
	// validated at the boundary as defense-in-depth.)
	if !isSafeDiffContentPath(path) {
		return "", fmt.Errorf("diff ingestion: refusing unsafe path %q extracted from diff content (absolute path or .. traversal)", path)
	}
	return path, nil
}

// isSafeDiffContentPath reports whether a path extracted from untrusted diff
// content stays within the working tree: not absolute, and with no leading `..`
// segment that escapes the root. Purely lexical — the path is provenance and is
// never opened, so (unlike isSafeDiffPath, which guards a path that IS opened) it
// has no symlink concern.
func isSafeDiffContentPath(p string) bool {
	if filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(p)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// parseDiffPathField extracts the path from a `--- `/`+++ ` header value: it
// drops a trailing tab-delimited timestamp (some diff tools append one), trims a
// trailing CR, and strips the conventional a/ or b/ prefix (absent under
// `git diff --no-prefix`). It does not trim surrounding spaces — diff paths may
// contain them.
func parseDiffPathField(field string) string {
	if tab := strings.IndexByte(field, '\t'); tab >= 0 {
		field = field[:tab]
	}
	field = strings.TrimSuffix(field, "\r")
	if field == devNull {
		return devNull
	}
	for _, pfx := range []string{"a/", "b/"} {
		if strings.HasPrefix(field, pfx) {
			return field[len(pfx):]
		}
	}
	return field
}

// headPathFromGitHeader extracts the new path from a `diff --git a/<old> b/<new>`
// line, for the header-only (binary/mode) sections that carry no `+++` line. The
// header is ambiguous when a path itself contains the literal " b/" token, so a
// naive last-" b/"-token split mis-truncates such paths. For the overwhelmingly
// common modification case (old == new) the header is the symmetric form
// `a/<P> b/<P>`, which splits unambiguously at its midpoint even when <P> contains
// " b/". Renames and `--no-prefix` headers stay genuinely ambiguous for spaced
// paths and fall back to the last " b/" token (correct for unspaced paths).
func headPathFromGitHeader(header string) string {
	const sep = " b/"
	body := strings.TrimPrefix(header, gitDiffMarker) // "a/<old> b/<new>"
	if !strings.HasPrefix(body, "a/") {
		// --no-prefix: `diff --git <old> <new>` with no a/ b/ markers. The common
		// modification/binary case is symmetric (`<P> <P>`); recover it via a
		// single-space midpoint even when <P> contains spaces. Renames or
		// asymmetric spaced paths stay ambiguous (documented limitation) -> "".
		return symmetricMidpoint(body, " ")
	}
	body = body[len("a/"):] // "<old> b/<new>"
	if p := symmetricMidpoint(body, sep); p != "" {
		return p
	}
	return lastBToken(body, sep)
}

// symmetricMidpoint returns the second half of body when it has the symmetric
// form `<P><sep><P>` (the common same-path git header, where old == new), even
// when <P> itself contains sep. It returns "" for an asymmetric (rename) or
// genuinely ambiguous spaced header.
func symmetricMidpoint(body, sep string) string {
	if len(body) < len(sep) || (len(body)-len(sep))%2 != 0 {
		return ""
	}
	half := (len(body) - len(sep)) / 2
	if body[half:half+len(sep)] == sep && body[:half] == body[half+len(sep):] {
		return body[half+len(sep):]
	}
	return ""
}

// lastBToken returns the segment after the last ` b/` token of s, or "" when
// absent — the unspaced-path fallback for headPathFromGitHeader.
func lastBToken(s, sep string) string {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[i+len(sep):]
	}
	return ""
}

// firstLine returns the first line of s for use in diagnostics.
func firstLine(s string) string {
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		return s[:nl]
	}
	return s
}

// isSafeDiffPath rejects absolute paths and any path that, once cleaned, escapes
// the working tree (a leading ".." segment) — the diff-file path-traversal guard,
// mirroring the suite manifest's isSafeRelPath so a hostile path argument cannot
// make the ingestion path read an arbitrary file. Like isSafeRelPath, it is a
// lexical check: it does NOT resolve symlinks, so a relative in-tree path whose
// component is a symlink pointing outside the tree is out of this guard's scope —
// callers ingesting untrusted suite trees should pre-resolve paths if that matters.
func isSafeDiffPath(p string) bool {
	if filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(p)
	if clean == "." {
		return false
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// rejectDiffSymlinkEscape resolves the symlinks in a (lexically-validated) diff
// path and rejects it when the real target lands outside the working tree — the
// filesystem-aware companion to isSafeDiffPath's lexical guard. A path that does
// not yet resolve (EvalSymlinks errors) is left to the subsequent os.Open, and a
// failure to determine the working-tree root fails open (the lexical guard still
// applies). The root is itself symlink-resolved so the comparison holds on
// platforms whose temp/working dirs sit behind a symlink (e.g. macOS /var ->
// /private/var).
func rejectDiffSymlinkEscape(path string) error {
	// Resolve to an absolute path BEFORE following symlinks so every component
	// (including the working-tree root itself, e.g. macOS /var -> /private/var) is
	// resolved consistently — EvalSymlinks on a bare relative path leaves it
	// relative and Abs would then join the UNresolved cwd, spuriously diverging
	// from the resolved root below.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil // does not resolve; os.Open reports the concrete failure
	}
	root, err := os.Getwd()
	if err != nil {
		return nil
	}
	if resolvedRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = resolvedRoot
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("diff ingestion: refusing diff path %q: resolves outside the working tree via a symlink", path)
	}
	return nil
}
