package stream

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Version is the required first non-blank line of every findings file. Unknown
// versions are a hard error so a consumer never silently parses incompatible
// data.
const Version = "# atcr-findings/v1"

// NoFindingsSentinel is what a reviewer emits when it has nothing to report.
//
// It exists so a clean review is a POSITIVE signal rather than silence. The
// fanout engine treats an empty reviewer response as a dead call and fails the
// slot over to its backup — a provider returning a null completion without
// setting finish_reason=length is otherwise recorded as a clean review, which
// on a leaderboard is the opposite of what happened. That gate is only safe if
// a genuinely clean review looks different from silence, which is what this
// sentinel provides. Every persona prompt instructs it.
const NoFindingsSentinel = "NO FINDINGS"

// bareFenceRe matches a code-fence marker line that carries nothing but an
// optional info word.
var bareFenceRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})[A-Za-z0-9_-]*\\s*$")

// IsNoFindings reports whether a reviewer response says "clean" and nothing
// else. The sentinel is model-produced, so the shapes a model slips into are
// accepted too: any case, surrounding whitespace, trailing '.', ':' or '!', a
// code fence around it, and an empty JSON array or {"findings":[]} (the clean
// reply under a json_object response format). A response may repeat these, as
// a chunked review does, but must hold nothing else: any other text means the
// model said something no parser read, which is exactly what this check must
// not call clean.
func IsNoFindings(content string) bool {
	var kept []string
	for _, l := range strings.Split(content, "\n") {
		// Only a bare marker line (```, ```json) is dropped; text sharing a
		// fence line is content like any other.
		if !bareFenceRe.MatchString(strings.TrimRight(l, "\r")) {
			kept = append(kept, l)
		}
	}
	s := strings.TrimSpace(strings.Join(kept, "\n"))
	seen := false
	for s != "" {
		if n := len(NoFindingsSentinel); len(s) >= n && strings.EqualFold(s[:n], NoFindingsSentinel) {
			s = strings.TrimLeft(s[n:], ".:!")
		} else if n := emptyFindingsValue(s); n > 0 {
			s = s[n:]
		} else {
			return false
		}
		if s != "" && !unicode.IsSpace(rune(s[0])) {
			return false // "NO FINDINGSX", "[]x"
		}
		s = strings.TrimSpace(s)
		seen = true
	}
	return seen
}

// versionPrefix matches any atcr-findings version header so a wrong version can
// be reported distinctly from a missing one.
const versionPrefix = "# atcr-findings/"

// versionTokenRe matches a well-formed version token (e.g. "v1", "v10") after
// versionPrefix. Only a well-formed token earns ErrUnknownVersion; a garbage
// suffix ("v1x", "v1.2") is a malformed header, not an unsupported version.
var versionTokenRe = regexp.MustCompile(`^v[0-9]+$`)

// Column counts for the two stream shapes.
const (
	PerSourceColumns  = 8 // ...|EVIDENCE|REVIEWER
	ReconciledColumns = 9 // ...|EVIDENCE|REVIEWERS|CONFIDENCE
)

// severityRe anchors a finding line at a valid severity prefix. Lines that do
// not match — comments, blanks, model prose mentioning "HIGH" mid-sentence —
// are skipped. This is the format's core contract: prose never becomes a row.
var severityRe = regexp.MustCompile(`^(CRITICAL|HIGH|MEDIUM|LOW)\|`)

// Sentinel errors for header problems (the only fatal parse failures; malformed
// rows are skipped, not fatal, per AC 01-05).
var (
	ErrMissingHeader  = errors.New("missing version header")
	ErrUnknownVersion = errors.New("unknown findings version")
)

// Finding is one normalized finding. Reviewer holds the per-source 8th column;
// Reviewers and Confidence hold the reconciled 8th/9th columns. A given Finding
// is populated by whichever parser produced it.
type Finding struct {
	Severity   string
	File       string
	Line       int
	Problem    string
	Fix        string
	Category   string
	EstMinutes int
	Evidence   string
	Reviewer   string   // per-source 8th column
	Reviewers  []string // reconciled 8th column
	Confidence string   // reconciled 9th column

	// PathValid and PathWarning record the result of file-existence validation
	// (Epic 5.0). They are NOT part of the wire format — no findings.txt column
	// carries them — and stay at their zero values until ValidatePath stamps a
	// finding. PathWarning is the authoritative display signal: an empty
	// PathWarning means "no warning" regardless of PathValid, so an unvalidated
	// finding (PathValid defaults to false) is never falsely flagged.
	PathValid   bool   // true once validated and the file exists
	PathWarning string // e.g. "file not found"; empty when valid or unvalidated

	// PathSuggestion is the candidate-index correction for a hallucinated path
	// (Epic 5.4): the real tracked file a flagged finding most likely meant. It
	// is suggest-only — File is never rewritten — and stays empty when the path
	// is valid, when no confident single candidate exists, or when no index is
	// available (non-git repo). Set only alongside a non-empty PathWarning.
	PathSuggestion string // e.g. "internal/auth/validate.go"; empty when none

	// FallbackModel records the model that SERVED the SOURCE slot this finding came
	// from when a litellm fallback substituted for the configured primary (Epic
	// 19.10 F5). Like PathValid/PathWarning it is NOT part of the wire format — no
	// findings.txt column carries it — and stays empty until reconcile stamps it
	// post-parse from the source's AgentStatus (status.json fallback_model, written
	// by fanout's statusFor). A non-empty value is the substituted-TO net model;
	// empty means the slot ran on its own configured model (an independent voice).
	// Reconcile's distinct-reviewer independence count collapses reviewers sharing
	// the same non-empty FallbackModel into a single voice, so one net model backing
	// multiple personas is not counted as multiple distinct reviewers. Keying on the
	// served MODEL (not the per-persona substituted-from name, which is unique and
	// would never collapse) is what makes the de-weighting actually fire.
	//
	// json:"-" — this is an in-memory-only stamp (built at discovery, consumed by
	// stampFallbackProvenance), never a serialized column. The tag keeps it out of
	// the ambiguous.json wire layout (which marshals raw stream.Finding values with
	// PascalCase keys), preserving the Epic 8.0 byte-identical baseline that a new
	// serialized field would otherwise break.
	FallbackModel string `json:"-"` // served fallback model; empty when not a fallback
}

// SkippedRow records a line skipped as malformed (wrong column count), with its
// 1-based line number, so callers can warn without failing the whole parse.
type SkippedRow struct {
	Line    int
	Content string
	Reason  string
}

// ParseResult carries the findings plus any malformed rows that were skipped.
type ParseResult struct {
	Findings []Finding
	Skipped  []SkippedRow
}

// ModelColumns is the column count a reviewer model emits: the per-source shape
// minus the trailing REVIEWER, which the engine appends from the agent name.
const ModelColumns = 7 // SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE

// ParseModelOutput extracts findings from a model's raw review text. Unlike
// ParseSource it requires no version header — models emit findings inline among
// prose — and it never lets a model self-attribute a REVIEWER: the engine sets
// Finding.Reviewer from the agent name afterward (TD-016). The returned findings
// have an empty Reviewer.
//
// Two shapes are read, in Content order, in one forward pass:
//
//   - A fenced ```json block holding an array of finding objects — the shape
//     persona prompts ask for. A chunked review joins several chunk outputs, so
//     every such block is read and the findings are unioned. A cut-off block
//     keeps every complete object and drops only the partial one (see
//     decodeJSONValue). A bare, unfenced value outside any fence is read too,
//     for a model (or one chunk) that forgot the fence. A value may also be a
//     {"findings":[...]} wrapper or one finding object, the shapes a model
//     slips into and the only shape a json_object response format allows.
//   - A legacy 7-column pipe row, for custom personas that have not migrated:
//     exactly the 7 persona columns (SEVERITY..EVIDENCE). An 8th-or-later field
//     is folded into EVIDENCE, never dropped. Non-severity-prefixed lines,
//     blanks, and comments are skipped; short rows are padded.
func ParseModelOutput(data []byte) []Finding {
	out, _ := scanModelOutput(data)
	return out
}

// LineSpan is an inclusive range of line indexes into a text split on "\n".
type LineSpan struct{ First, Last int }

// BareValueSpans returns the lines of every unfenced JSON value ParseModelOutput
// reads as findings, from its opening line through the last line the value
// consumed. It is the same scan, so internal/reconcile can bound these values in
// a narrative exactly where the parser read them.
func BareValueSpans(data []byte) []LineSpan {
	_, spans := scanModelOutput(data)
	return spans
}

func scanModelOutput(data []byte) ([]Finding, []LineSpan) {
	text := string(data)
	lines := strings.Split(text, "\n")
	var out []Finding
	var spans []LineSpan
	inFence, inJSON := false, false
	openMarker := "" // the open fence's opener, which only a closesFence line ends
	fences := fenceOffsets(lines, len(text))
	bareScanned := 0 // bytes scanned by failed bare-value attempts
	bareEnd := 0     // byte offset just past the last bare value read
	jsonStart := 0   // byte offset of the current ```json block's first content line
	offset := 0      // byte offset of the current line
	for i, raw := range lines {
		lineStart := offset
		offset += len(raw) + 1
		line := strings.TrimRight(raw, "\r")
		// A markdown code fence toggles "inside a fenced block" state. Rows a model
		// quotes inside a fence — e.g. a sample findings table it shows while
		// explaining the format — are examples, never real findings, yet they carry
		// a leading severity token and would otherwise parse as findings and inflate
		// the count with rows whose cited files do not exist. Skip everything between
		// fences. Mirrors internal/verify/syntaxguard's fence handling; a fence
		// marker is a line whose first non-space content is a run of >=3 backticks
		// or tildes.
		// The one exception is a ```json fence: that is the output itself. As in
		// CommonMark, a fence closes only on a marker of its own character (` or ~)
		// at least as long as its opener, so a ```json example quoted inside a
		// ````md or ~~~ fence stays quoted (TD-019, TD-048).
		if isFenceMarker(line) && (!inJSON && !inFence || closesFence(line, openMarker)) {
			switch {
			case inJSON:
				// A "```json" line here is the NEXT chunk's opener, not this block's
				// closer: a chunk cut off inside its block never wrote a closer, and
				// reading the opener as one would turn the next chunk's array into
				// prose. It closes this block and opens the next.
				out = append(out, decodeJSONFindings(text[jsonStart:lineStart])...)
				inJSON, jsonStart, openMarker = isJSONFence(line), offset, line
			case inFence:
				inFence = false
			case isJSONFence(line):
				inJSON, jsonStart, openMarker = true, offset, line
			default:
				inFence, openMarker = true, line
			}
			continue
		}
		if inJSON || inFence {
			continue
		}
		if lineStart < bareEnd {
			spans[len(spans)-1].Last = i
			continue // inside a bare value already read
		}
		if t := strings.TrimSpace(line); bareScanned < maxBareScanFactor*len(text) && (strings.HasPrefix(t, "[") || strings.HasPrefix(t, "{")) {
			// A prose line like "[x](y)" decodes to nothing and is passed over. The
			// candidate ends at the next fence marker, so recovering a cut-off value
			// never reaches into a quoted example below it. Failed attempts are
			// charged the bytes they could scan (maxBareScanFactor).
			end := max(fences[i], lineStart)
			if found, n := decodeJSONValue(text[lineStart:end]); len(found) > 0 {
				out = append(out, found...)
				bareEnd = lineStart + n
				spans = append(spans, LineSpan{First: i, Last: i})
				continue
			}
			bareScanned += end - lineStart
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !severityRe.MatchString(line) {
			continue // prose
		}
		fields := strings.Split(line, "|")
		// A real finding has at least SEVERITY|FILE:LINE|PROBLEM with a location;
		// drop degenerate severity-prefixed noise like a bare "HIGH|".
		if len(fields) < 3 || strings.TrimSpace(fields[1]) == "" {
			continue
		}
		// Any overflow past the 7 persona columns — a model-supplied REVIEWER or
		// an unescaped pipe inside EVIDENCE — is folded back into the EVIDENCE
		// field rather than dropped. This keeps evidence text intact AND makes
		// REVIEWER forgery impossible: a model can never land a value in the
		// REVIEWER slot, which the engine fills from the agent name.
		if len(fields) > ModelColumns {
			fields[ModelColumns-1] = strings.Join(fields[ModelColumns-1:], "/")
			fields = fields[:ModelColumns]
		}
		// Pad to the per-source width so the REVIEWER slot exists but stays empty
		// until the engine fills it.
		for len(fields) < PerSourceColumns {
			fields = append(fields, "")
		}
		out = append(out, fieldsToFinding(fields, PerSourceColumns))
	}
	// An unterminated ```json fence runs to the end of Content: a response cut off
	// mid-block still contributes its complete findings.
	if inJSON {
		out = append(out, decodeJSONFindings(text[min(jsonStart, len(text)):])...)
	}
	return out, spans
}

// isFenceMarker reports whether line opens or closes a markdown code fence: its
// first non-space content is a run of three or more backticks or tildes
// (```lang, ```, ~~~, or a longer CommonMark fence). Used by ParseModelOutput to
// toggle fenced-block state so quoted example rows are not parsed as findings.
func isFenceMarker(line string) bool {
	_, n := fenceRun(line)
	return n >= 3
}

// fenceRun returns the character (` or ~) and length of the run that begins a
// line after leading spaces and tabs; n is 0 when the line starts with neither.
func fenceRun(line string) (c byte, n int) {
	t := strings.TrimLeft(line, " \t")
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return 0, 0
	}
	c = t[0]
	for n < len(t) && t[n] == c {
		n++
	}
	return c, n
}

// closesFence reports whether marker line may close the fence opener opened:
// same character, and a run at least as long (CommonMark).
func closesFence(line, opener string) bool {
	c, n := fenceRun(line)
	oc, on := fenceRun(opener)
	return c == oc && n >= on
}

// ParseSource parses a per-source findings file: a v1 8-column pipe stream, or
// a v2 document (TOON table or go-axi JSON envelope), chosen by the header.
func ParseSource(data []byte) (ParseResult, error) {
	return parse(data, PerSourceColumns)
}

// ParseReconciled parses a reconciled (9-column) findings file. Nothing in the
// pipeline re-ingests the reconciled findings.txt today; annotations folded
// into EVIDENCE at write time stay folded (see Finding.AsReconciled).
func ParseReconciled(data []byte) (ParseResult, error) {
	return parse(data, ReconciledColumns)
}

// parse validates the version header then extracts finding rows. Short rows are
// padded to cols with empty strings; rows with MORE than cols columns are
// recorded as skipped (an unescaped pipe leaked a column) rather than silently
// misaligning fields. Comment and prose lines are skipped.
func parse(data []byte, cols int) (ParseResult, error) {
	var res ParseResult
	headerSeen := false

	lines := strings.Split(string(data), "\n")
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !headerSeen {
			switch {
			case strings.TrimSpace(line) == Version:
				headerSeen = true
				continue
			case strings.TrimSpace(line) == VersionV2 && cols == PerSourceColumns:
				return parseV2Body(strings.Join(lines[i+1:], "\n"))
			case strings.HasPrefix(line, versionPrefix) && versionTokenRe.MatchString(strings.TrimSpace(strings.TrimPrefix(line, versionPrefix))):
				return res, fmt.Errorf("%w: %q (want %s)", ErrUnknownVersion, strings.TrimSpace(line), wantHeaders(cols))
			default:
				return res, fmt.Errorf("%w: first line must be %s", ErrMissingHeader, wantHeaders(cols))
			}
		}
		if strings.HasPrefix(line, "#") {
			continue // comment
		}
		if !severityRe.MatchString(line) {
			continue // prose
		}
		fields := strings.Split(line, "|")
		// A trailing pipe yields an extra empty column; treat trailing empties
		// as padding so a valid finding written with a trailing '|' is not lost.
		for len(fields) > cols && fields[len(fields)-1] == "" {
			fields = fields[:len(fields)-1]
		}
		if len(fields) > cols {
			res.Skipped = append(res.Skipped, SkippedRow{
				Line:    i + 1,
				Content: line,
				Reason:  fmt.Sprintf("expected %d columns, got %d", cols, len(fields)),
			})
			continue
		}
		for len(fields) < cols {
			fields = append(fields, "")
		}
		res.Findings = append(res.Findings, fieldsToFinding(fields, cols))
	}

	if !headerSeen {
		return res, fmt.Errorf("%w: first line must be %s", ErrMissingHeader, wantHeaders(cols))
	}
	return res, nil
}

// wantHeaders names the headers a parser accepts, for error messages. Only the
// per-source shape has a v2 form.
func wantHeaders(cols int) string {
	if cols == PerSourceColumns {
		return fmt.Sprintf("%q or %q", Version, VersionV2)
	}
	return fmt.Sprintf("%q", Version)
}

// fieldsToFinding maps a padded column slice to a Finding. cols selects the
// per-source vs reconciled tail (REVIEWER vs REVIEWERS+CONFIDENCE).
func fieldsToFinding(f []string, cols int) Finding {
	file, line := splitFileLine(f[1])
	fnd := Finding{
		Severity:   f[0],
		File:       file,
		Line:       line,
		Problem:    f[2],
		Fix:        f[3],
		Category:   f[4],
		EstMinutes: atoiOrZero(f[5]),
		Evidence:   f[6],
	}
	if cols == ReconciledColumns {
		fnd.Reviewers = splitReviewers(f[7])
		fnd.Confidence = f[8]
	} else {
		fnd.Reviewer = f[7]
	}
	return fnd
}

// splitFileLine splits a FILE:LINE column on the last colon. A missing or
// non-numeric line yields line 0 with the whole column kept as the file, so a
// path that happens to contain a colon is never lost.
func splitFileLine(s string) (string, int) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return s, 0
	}
	suffix := s[idx+1:]
	if suffix == "" {
		return s[:idx], 0 // bare trailing colon: drop it, line 0
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return s, 0
	}
	return s[:idx], n
}

// splitReviewers splits and trims a comma-joined REVIEWERS column, dropping
// empty entries.
func splitReviewers(s string) []string {
	var out []string
	for _, r := range strings.Split(s, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// atoiOrZero parses an integer, defaulting to 0 (EST_MINUTES is best-effort).
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
