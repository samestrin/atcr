package payload

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// fileSectionStarts returns the byte offsets at which each per-file section
// begins, covering the whole input contiguously so the sections partition
// diffText with no loss (the guarantee behind round-trip identity). It detects
// `git diff` format (boundaries on `diff --git ` lines) when any such line is
// present, else loose format (boundaries on a `--- `/`+++ `/`@@ ` header triple,
// which a removed/added hunk line cannot spoof — a hunk header never appears
// mid-hunk). The first section must start at offset 0; leading preamble is an
// error rather than silently-dropped bytes.
func fileSectionStarts(diff string) ([]int, error) {
	lines, offsets := splitLinesWithOffsets(diff)

	gitMode := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, gitDiffMarker) {
			gitMode = true
			break
		}
	}

	var starts []int
	if gitMode {
		for i, ln := range lines {
			if strings.HasPrefix(ln, gitDiffMarker) {
				starts = append(starts, offsets[i])
			}
		}
	} else {
		for i := 0; i+2 < len(lines); i++ {
			if strings.HasPrefix(lines[i], oldFileMarker) &&
				strings.HasPrefix(lines[i+1], newFileMarker) &&
				strings.HasPrefix(lines[i+2], hunkMarker) {
				starts = append(starts, offsets[i])
			}
		}
	}

	if len(starts) == 0 {
		return nil, fmt.Errorf("diff ingestion: no file sections found (expected a `diff --git` line or a `--- `/`+++ `/`@@ ` header triple)")
	}
	if starts[0] != 0 {
		return nil, fmt.Errorf("diff ingestion: unexpected content before the first file section (would be lost on round-trip)")
	}
	return starts, nil
}

// splitLinesWithOffsets splits s on '\n' into lines (newline excluded) and the
// byte offset where each line begins. A trailing newline yields a final empty
// line at offset len(s), which matches no marker.
func splitLinesWithOffsets(s string) (lines []string, offsets []int) {
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
	for _, ln := range strings.Split(section, "\n") {
		switch {
		case gitHeader == "" && strings.HasPrefix(ln, gitDiffMarker):
			gitHeader = ln
		case newPath == "" && strings.HasPrefix(ln, newFileMarker):
			newPath = parseDiffPathField(ln[len(newFileMarker):])
		case oldPath == "" && strings.HasPrefix(ln, oldFileMarker):
			oldPath = parseDiffPathField(ln[len(oldFileMarker):])
		}
	}
	if newPath != "" && newPath != devNull {
		return newPath, nil
	}
	if oldPath != "" && oldPath != devNull {
		return oldPath, nil
	}
	if gitHeader != "" {
		if p := headPathFromGitHeader(gitHeader); p != "" {
			return p, nil
		}
	}
	return "", fmt.Errorf("diff ingestion: cannot determine file path for section: %s", firstLine(section))
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
// line by taking the segment after the last ` b/` token — the same head-side key
// the splitter uses, sufficient for the header-only (binary/mode) sections that
// carry no `+++` line.
func headPathFromGitHeader(header string) string {
	const sep = " b/"
	if i := strings.LastIndex(header, sep); i >= 0 {
		return header[i+len(sep):]
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
// make the ingestion path read an arbitrary file.
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
