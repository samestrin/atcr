package payload

import "strings"

// Entry-start line prefixes, derived from the format strings that produce them
// so the two can never drift apart. A rendered payload is the concatenation of
// per-file bodies, and every body begins with one of these.
var (
	filesHeaderPrefix  = markerPrefix(fileHeaderFmt)    // "=== FILE: "
	deletedMarkerPfx   = markerPrefix(deletedMarkerFmt) // "[deleted file: "
	binaryMarkerPrefix = markerPrefix(binaryMarkerFmt)  // "[binary file changed: "
)

// markerPrefix returns the literal text a marker format emits before its first
// substituted value.
func markerPrefix(format string) string {
	if i := strings.Index(format, "%s"); i >= 0 {
		return format[:i]
	}
	return format
}

// EntriesFromRenderedPayload recovers the per-file entries that make up an
// already-rendered payload text — the inverse of joining bodies into a flat
// payload.
//
// It exists for the model-invocation observation seam (Epic 35.0): an audit
// consumer needs to know which files a given invocation actually saw, and by
// the time a call reaches the LLM client the payload is one flat string. The
// chunked review strategy splits that string on file boundaries, so re-deriving
// the split from the final per-invocation text is exact, and far cheaper than
// threading structure through the chunker.
//
// It never returns an error and never panics. A payload it cannot attribute to
// files yields nil, and the caller degrades to "no code context" — an audit
// helper must not be able to fail the review it is observing. Bodies are
// substrings of text, not copies, so threading them costs no extra allocation.
//
// Attribution is best-effort by design: a section whose path cannot be
// determined (a combined/merge diff, say) is still returned, with an empty
// Path. An unattributed record is worth more to an auditor than a missing one.
func EntriesFromRenderedPayload(mode PayloadMode, text string) []FileEntry {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if validatePayloadMode(mode) != nil {
		return nil
	}
	if entries := splitMarkedEntries(text); len(entries) > 0 {
		return entries
	}
	// No column-0 marker anywhere: the remaining shape this can legitimately
	// take is a loose `--- `/`+++ `/`@@ ` unified diff, which reaches a payload
	// through diff-file ingestion. The canonical parser owns that grammar.
	entries, err := BuildEntriesFromDiff(text)
	if err != nil {
		return nil
	}
	return entries
}

// splitMarkedEntries splits text on the column-0 markers that begin a per-file
// body, returning nil when it finds none.
func splitMarkedEntries(text string) []FileEntry {
	starts := markedEntryStarts(text)
	if len(starts) == 0 {
		return nil
	}
	entries := make([]FileEntry, 0, len(starts))
	for i, s := range starts {
		end := len(text)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		body := text[s:end]
		entries = append(entries, FileEntry{
			Path: renderedSectionPath(body),
			Size: int64(len(body)),
			Body: body,
		})
	}
	return entries
}

// markedEntryStarts returns the byte offsets of every line that begins a
// per-file body. Content before the first marker is not part of any file's
// contribution and is deliberately excluded.
func markedEntryStarts(text string) []int {
	var starts []int
	for off := 0; off < len(text); {
		nl := strings.IndexByte(text[off:], '\n')
		lineEnd := len(text)
		if nl >= 0 {
			lineEnd = off + nl
		}
		if isRenderedEntryStart(text[off:lineEnd]) {
			starts = append(starts, off)
		}
		if nl < 0 {
			break
		}
		off = lineEnd + 1
	}
	return starts
}

// isRenderedEntryStart reports whether ln begins a new per-file body in any
// payload mode.
func isRenderedEntryStart(ln string) bool {
	return strings.HasPrefix(ln, gitDiffMarker) ||
		strings.HasPrefix(ln, combinedHeaderCC) ||
		strings.HasPrefix(ln, combinedHeader) ||
		strings.HasPrefix(ln, filesHeaderPrefix) ||
		strings.HasPrefix(ln, deletedMarkerPfx) ||
		strings.HasPrefix(ln, binaryMarkerPrefix)
}

// renderedSectionPath extracts the file path a section describes, returning ""
// when it cannot be determined.
func renderedSectionPath(section string) string {
	first := section
	if nl := strings.IndexByte(section, '\n'); nl >= 0 {
		first = section[:nl]
	}
	switch {
	case strings.HasPrefix(first, filesHeaderPrefix):
		return filesModeHeaderPath(first)
	case strings.HasPrefix(first, deletedMarkerPfx):
		return trimMarkerSuffix(first[len(deletedMarkerPfx):], "]")
	case strings.HasPrefix(first, binaryMarkerPrefix):
		return trimMarkerSuffix(first[len(binaryMarkerPrefix):], "]")
	}
	// A diff section: the canonical extractor owns the header grammar and the
	// path-safety check that goes with it. Its error means "unattributable",
	// which is a body with no path rather than a dropped body.
	path, err := diffSectionPath(section)
	if err != nil {
		return ""
	}
	return path
}

// filesModeHeaderPath reads the path out of a `=== FILE: path ===` header,
// including the `(renamed from old)` variant, whose path is the new one.
func filesModeHeaderPath(header string) string {
	inner := trimMarkerSuffix(header[len(filesHeaderPrefix):], " ===")
	if i := strings.Index(inner, " (renamed from "); i >= 0 {
		inner = inner[:i]
	}
	return inner
}

// trimMarkerSuffix strips a marker's closing text and surrounding space.
func trimMarkerSuffix(s, suffix string) string {
	s = strings.TrimSuffix(strings.TrimRight(s, " \t\r"), suffix)
	return strings.TrimSpace(s)
}
