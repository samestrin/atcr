package stream

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	goaxi "github.com/samestrin/go-axi"
)

// VersionV2 is the header of the lossless findings file (findings.toon). The
// body is a go-axi document: a TOON table by default, or the
// {"axi_format":"json",...} envelope when TOON would lose data.
//
// v2 lives in its own file, apart from the v1 pipe writer, so retiring v1 later
// is mostly a delete. Nothing here may call escapeField or fieldReplacer: v2
// substitutes nothing, so '|', quotes, and line breaks survive. The one rewrite
// is go-axi's sanitizer, which runs on both paths and strips control bytes
// (other than tab, LF, CR), U+2028/U+2029, and invalid UTF-8.
const VersionV2 = "# atcr-findings/v2"

// v2Row is one per-source finding in the v2 body. The json tags equal the toon
// tags because go-axi's JSON fallback marshals the row with encoding/json, which
// ignores toon tags; matching them keeps the envelope's keys identical to the
// TOON header's column names.
type v2Row struct {
	Severity   string `toon:"severity" json:"severity"`
	FileLine   string `toon:"file_line" json:"file_line"`
	Problem    string `toon:"problem" json:"problem"`
	Fix        string `toon:"fix" json:"fix"`
	Category   string `toon:"category" json:"category"`
	EstMinutes int    `toon:"est_minutes" json:"est_minutes"`
	Evidence   string `toon:"evidence" json:"evidence"`
	Reviewer   string `toon:"reviewer" json:"reviewer"`
}

type v2Payload struct {
	Findings []v2Row `toon:"findings" json:"findings"`
}

// WriteSourceV2 writes per-source findings (single REVIEWER) as a v2 document.
// There is no reconciled v2 shape: nothing re-reads reconciled/findings.txt,
// and a reviewer list would need its own delimiter rule.
func WriteSourceV2(w io.Writer, findings []Finding) error {
	rows := make([]v2Row, len(findings))
	for i, f := range findings {
		rows[i] = v2Row{
			Severity:   f.Severity,
			FileLine:   fmt.Sprintf("%s:%d", f.File, f.Line),
			Problem:    f.Problem,
			Fix:        f.Fix,
			Category:   f.Category,
			EstMinutes: f.EstMinutes,
			Evidence:   f.Evidence,
			Reviewer:   f.Reviewer,
		}
	}
	return encodeV2(w, v2Payload{Findings: rows})
}

// encodeV2 is the single encode chokepoint for v2 output. It takes any payload
// so a test can drive go-axi's JSON fallback, which real []Finding input never
// reaches.
//
// The TOON-vs-JSON choice is made once for the WHOLE document, not per field:
// one value TOON cannot carry flips every row to the JSON envelope. That is
// go-axi's contract, not a bug, and it is why a reader routes on the literal
// {"axi_format prefix of the body rather than on a leading '{' or '['.
func encodeV2(w io.Writer, payload any) error {
	if _, err := fmt.Fprintln(w, VersionV2); err != nil {
		return fmt.Errorf("writing v2 findings header: %w", err)
	}
	if err := goaxi.EncodeOrJSON(w, payload); err != nil {
		return fmt.Errorf("encoding v2 findings: %w", err)
	}
	return nil
}

// envelopePrefix is how a v2 body announces go-axi's JSON envelope. Readers
// route on this literal, never on a bare leading '{' or '[': several TOON
// shapes begin with '['.
const envelopePrefix = `{"axi_format`

// parseV2Body decodes the body under a "# atcr-findings/v2" header. Unlike
// ParseModelOutput it recovers nothing: this file is written by atcr or by the
// host skill, so any deviation is an error, never a partial or empty result.
func parseV2Body(body string) (ParseResult, error) {
	if strings.HasPrefix(strings.TrimLeft(body, " \t\r\n"), envelopePrefix) {
		return parseV2Envelope(body)
	}
	doc, err := goaxi.DecodeTabular(strings.NewReader(body))
	if err != nil {
		return ParseResult{}, fmt.Errorf("decoding v2 findings table: %w", err)
	}
	if doc.Name != "findings" {
		return ParseResult{}, fmt.Errorf("decoding v2 findings table: table is %q, want \"findings\"", doc.Name)
	}
	// A missing or misspelled column would otherwise read as an empty field
	// (TD-024). An empty table may declare no columns at all.
	if want := v2TableColumns(); (len(doc.Fields) > 0 || len(doc.Rows) > 0) && !slices.Equal(doc.Fields, want) {
		return ParseResult{}, fmt.Errorf("decoding v2 findings table: columns are %v, want %v", doc.Fields, want)
	}
	// Fewer rows than declared means the file was cut on a row boundary, which
	// DecodeTabular reports rather than rejects.
	if doc.Declared != len(doc.Rows) {
		return ParseResult{}, fmt.Errorf("decoding v2 findings table: header declares %d row(s), found %d", doc.Declared, len(doc.Rows))
	}
	res := ParseResult{Findings: make([]Finding, 0, len(doc.Rows))}
	for _, r := range doc.Rows {
		file, line := splitFileLine(r["file_line"])
		res.Findings = append(res.Findings, Finding{
			Severity:   r["severity"],
			File:       file,
			Line:       line,
			Problem:    r["problem"],
			Fix:        r["fix"],
			Category:   r["category"],
			EstMinutes: atoiOrZero(r["est_minutes"]),
			Evidence:   r["evidence"],
			Reviewer:   r["reviewer"],
		})
	}
	return res, nil
}

// v2TableColumns is the v2 table header, read from v2Row's toon tags so the writer
// and the reader cannot drift apart.
func v2TableColumns() []string {
	t := reflect.TypeOf(v2Row{})
	cols := make([]string, t.NumField())
	for i := range cols {
		cols[i] = t.Field(i).Tag.Get("toon")
	}
	return cols
}

// v2EnvelopeRow reads one envelope row. It is modelFinding plus the reviewer,
// which an on-disk file carries and model output must never supply.
type v2EnvelopeRow struct {
	modelFinding
	Reviewer string `json:"reviewer"`
}

// parseV2Envelope decodes go-axi's {"axi_format":"json","axi_notice":...,
// "data":{"findings":[...]}} envelope. axi_notice is optional (a host-written
// file may leave it empty); findings outside data are not accepted. Unknown
// keys are errors (TD-025): a host typing "file-line" or "reviewr" by hand
// must fail loudly, not decode that field as empty.
func parseV2Envelope(body string) (ParseResult, error) {
	var env struct {
		Format string `json:"axi_format"`
		Notice string `json:"axi_notice"`
		Data   *struct {
			Findings *[]v2EnvelopeRow `json:"findings"`
		} `json:"data"`
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		return ParseResult{}, fmt.Errorf("decoding v2 findings envelope: %w", err)
	}
	// Anything after the envelope (a second envelope, prose, a copied code
	// fence) is a deviation, not something to ignore.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ParseResult{}, errors.New("decoding v2 findings envelope: unexpected data after the envelope")
	}
	if env.Format != "json" {
		return ParseResult{}, fmt.Errorf("decoding v2 findings envelope: axi_format is %q, want \"json\"", env.Format)
	}
	if env.Data == nil || env.Data.Findings == nil {
		return ParseResult{}, errors.New("decoding v2 findings envelope: missing data.findings")
	}
	rows := *env.Data.Findings
	res := ParseResult{Findings: make([]Finding, 0, len(rows))}
	for _, r := range rows {
		file, line := splitFileLine(r.FileLine)
		res.Findings = append(res.Findings, Finding{
			Severity:   r.Severity,
			File:       file,
			Line:       line,
			Problem:    r.Problem,
			Fix:        r.Fix,
			Category:   r.Category,
			EstMinutes: int(r.EstMinutes),
			Evidence:   r.Evidence,
			Reviewer:   r.Reviewer,
		})
	}
	return res, nil
}

// modelFinding is one finding object a reviewer model emits. It has no
// reviewer field on purpose: encoding/json drops unknown keys, so a
// model-supplied "reviewer" can never reach Finding.Reviewer (TD-016).
type modelFinding struct {
	Severity   string  `json:"severity"`
	FileLine   string  `json:"file_line"`
	Problem    string  `json:"problem"`
	Fix        string  `json:"fix"`
	Category   string  `json:"category"`
	EstMinutes flexInt `json:"est_minutes"`
	Evidence   string  `json:"evidence"`
}

// flexInt is EST_MINUTES as a model writes it: a number, a numeric string, or
// junk. Like atoiOrZero it is best-effort and never fails the finding.
type flexInt int

func (n *flexInt) UnmarshalJSON(b []byte) error {
	var num json.Number
	if json.Unmarshal(b, &num) == nil {
		if i, err := num.Int64(); err == nil {
			*n = flexInt(i)
		} else if f, err := num.Float64(); err == nil {
			*n = flexInt(int(f))
		}
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		*n = flexInt(atoiOrZero(s))
	}
	return nil
}

// isJSONFence reports whether a fence marker line opens a ```json block: its
// info string, after the backticks, is "json" in any case.
// internal/reconcile's isJSONFenceOpener restates this rule and is pinned to it.
func isJSONFence(line string) bool {
	t := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(t, "```") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(strings.TrimLeft(t, "`")), "json")
}

// maxBareAttempts bounds how many "["- or "{"-led lines ParseModelOutput tries
// as an unfenced value. Model output is untrusted, and each attempt can scan to
// the next fence, so an unbounded count is quadratic.
const maxBareAttempts = 16

// nextFenceOffset returns the byte offset of the first fence marker line after
// line i (which starts at offset start), or end, the length of Content.
func nextFenceOffset(lines []string, i, start, end int) int {
	off := start
	for j := i; j < len(lines); j++ {
		if j > i && isFenceMarker(strings.TrimRight(lines[j], "\r")) {
			return off
		}
		off += len(lines[j]) + 1
	}
	return min(off, end)
}

// decodeJSONFindings reads the JSON value at the start of a ```json block.
func decodeJSONFindings(text string) []Finding {
	found, _ := decodeJSONValue(text)
	return found
}

// findingsWrapperRe matches the opening of a {"findings":[...]} wrapper, so a
// cut-off wrapper can still recover the complete objects inside it.
var findingsWrapperRe = regexp.MustCompile(`^\{\s*"findings"\s*:\s*\[`)

// decodeJSONValue reads the JSON value at the start of text (after whitespace)
// and returns its valid finding objects plus the bytes it consumed. The value is
// an array of finding objects, a {"findings":[...]} wrapper, or one finding
// object. When it does not decode — the response was cut off, or an element is
// malformed — it keeps every complete element before the damage and drops the
// rest (recoverElements), and consumes all of text. Objects with an unknown
// severity or no location are dropped, as the pipe path drops degenerate rows.
func decodeJSONValue(text string) ([]Finding, int) {
	body := strings.TrimLeft(text, " \t\r\n")
	lead := len(text) - len(body)
	consumed := len(text)
	var elems []json.RawMessage
	switch {
	case strings.HasPrefix(body, "["):
		dec := json.NewDecoder(strings.NewReader(body))
		if err := dec.Decode(&elems); err != nil {
			elems = recoverElements(body)
		} else {
			consumed = lead + int(dec.InputOffset())
		}
	case strings.HasPrefix(body, "{"):
		dec := json.NewDecoder(strings.NewReader(body))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == nil {
			consumed = lead + int(dec.InputOffset())
			var wrap struct {
				Findings *[]json.RawMessage `json:"findings"`
			}
			if json.Unmarshal(raw, &wrap) == nil && wrap.Findings != nil {
				elems = *wrap.Findings
			} else {
				elems = []json.RawMessage{raw}
			}
		} else if loc := findingsWrapperRe.FindStringIndex(body); loc != nil {
			elems = recoverElements(body[loc[1]-1:])
		}
	default:
		return nil, 0
	}
	var out []Finding
	for _, e := range elems {
		var m modelFinding
		if json.Unmarshal(e, &m) != nil {
			continue // valid JSON, wrong shape (e.g. a number where text belongs)
		}
		sev := NormalizeSeverity(m.Severity)
		loc := strings.TrimSpace(m.FileLine)
		if loc == "" {
			loc = splitLocation(e)
		}
		if _, ok := SeverityRank[sev]; !ok || loc == "" {
			continue
		}
		file, line := splitFileLine(loc)
		out = append(out, Finding{
			Severity:   sev,
			File:       file,
			Line:       line,
			Problem:    m.Problem,
			Fix:        m.Fix,
			Category:   m.Category,
			EstMinutes: int(m.EstMinutes),
			Evidence:   m.Evidence,
		})
	}
	return out, consumed
}

// splitLocation reads a location a model split into "file" and "line" keys
// instead of file_line (vera, live run 2026-09-25) and returns it as FILE:LINE.
// It is model-output only: the on-disk envelope keeps its strict key set.
func splitLocation(e json.RawMessage) string {
	var alt struct {
		File string  `json:"file"`
		Line flexInt `json:"line"`
	}
	if json.Unmarshal(e, &alt) != nil || strings.TrimSpace(alt.File) == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", strings.TrimSpace(alt.File), int(alt.Line))
}

// emptyFindingsValue returns the length of the empty JSON array or
// {"findings":[]} at the start of s, or 0 when s starts with anything else.
func emptyFindingsValue(s string) int {
	if !strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "{") {
		return 0
	}
	dec := json.NewDecoder(strings.NewReader(s))
	var v any
	if dec.Decode(&v) != nil {
		return 0
	}
	switch t := v.(type) {
	case []any:
		if len(t) != 0 {
			return 0
		}
	case map[string]any:
		f, ok := t["findings"].([]any)
		if len(t) != 1 || !ok || len(f) != 0 {
			return 0
		}
	default:
		return 0
	}
	return int(dec.InputOffset())
}

// recoverElements splits a damaged JSON array (text starts with '[') into its
// complete object/array elements by tracking bracket depth outside string
// literals, so a '}' or ']' inside a string is never read as structure. It stops
// at the first element that is not valid JSON, because the depth count after a
// malformed element cannot be trusted, and at the array's closing bracket. A
// cut-off final element never closes, so it is never returned.
func recoverElements(text string) []json.RawMessage {
	var out []json.RawMessage
	depth, elemStart := 0, -1
	inStr, esc := false, false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
			if depth == 2 {
				elemStart = i
			}
		case '}', ']':
			depth--
			if depth == 1 && elemStart >= 0 {
				elem := text[elemStart : i+1]
				if !json.Valid([]byte(elem)) {
					return out
				}
				out = append(out, json.RawMessage(elem))
				elemStart = -1
			}
			if depth <= 0 {
				return out
			}
		}
	}
	return out
}

// Findings file names in a source directory. atcr dual-writes both; v1 stays
// the wire contract for external consumers until they migrate.
const (
	findingsFileV1 = "findings.txt"
	findingsFileV2 = "findings.toon"
)

// SelectFindingsFile returns the findings file an atcr reader parses in dir:
// findings.toon when it is a regular file, else findings.txt when it exists,
// else an error for which errors.Is(err, fs.ErrNotExist) holds. Every reader
// calls this rather than restating the rule, so no two readers pick different
// files for one directory.
//
// A findings.toon that is a symlink, FIFO, device, or directory counts as
// absent: a symlink could point outside the review tree and a FIFO would block
// the read. findings.txt keeps today's check (plain existence), so a .txt-only
// directory reads exactly as before.
//
// The choice is final. A caller that selected findings.toon must never retry
// findings.txt when the read or parse fails: that would hide a v2 writer bug
// behind lossy data.
func SelectFindingsFile(dir string) (string, error) {
	toon := filepath.Join(dir, findingsFileV2)
	fi, err := os.Lstat(toon)
	if err == nil && fi.Mode().IsRegular() {
		return toon, nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	txt := filepath.Join(dir, findingsFileV1)
	if _, err := os.Stat(txt); err != nil {
		return "", err
	}
	return txt, nil
}

// FindingsParseError reports that the selected findings file was read but did
// not parse. Its message is the parser's own, so a caller that wraps it keeps
// its existing text; errors.As tells it apart from a read error.
type FindingsParseError struct {
	Path string
	Err  error
}

func (e *FindingsParseError) Error() string { return e.Err.Error() }
func (e *FindingsParseError) Unwrap() error { return e.Err }

// ReadPoolFindings reads the findings in dir through SelectFindingsFile and
// ParseFindingsFile, which routes on the file's own version header. It returns the
// selection error when neither file exists (errors.Is(err, fs.ErrNotExist)),
// the OS error when the selected file cannot be read, and a
// *FindingsParseError when it does not parse. It never falls back to
// findings.txt after selecting findings.toon.
func ReadPoolFindings(dir string) (ParseResult, error) {
	path, err := SelectFindingsFile(dir)
	if err != nil {
		return ParseResult{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ParseResult{}, err
	}
	res, err := ParseFindingsFile(path, data)
	if err != nil {
		return ParseResult{}, &FindingsParseError{Path: path, Err: err}
	}
	return res, nil
}

// ParseFindingsFile parses the findings file SelectFindingsFile returned. It is
// ParseSource plus one rule: a findings.toon must carry the v2 header. A v1
// header there means the v2 writer produced lossy bytes, so it is an error
// rather than a clean read of damaged data.
func ParseFindingsFile(path string, data []byte) (ParseResult, error) {
	if filepath.Base(path) == findingsFileV2 {
		if first := firstNonBlankLine(data); first != VersionV2 {
			return ParseResult{}, fmt.Errorf("%w: %s header is %q (want %q)", ErrUnknownVersion, findingsFileV2, first, VersionV2)
		}
	}
	return ParseSource(data)
}

func firstNonBlankLine(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
