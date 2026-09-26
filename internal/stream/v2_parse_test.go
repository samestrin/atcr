package stream

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The signature is a contract: fanout's engine and artifacts call it directly.
var _ func([]byte) []Finding = ParseModelOutput

// jsonBlock wraps body in a ```json fence, the shape personas are told to emit.
func jsonBlock(body string) string { return "```json\n" + body + "\n```\n" }

const (
	objA = `{"severity":"HIGH","file_line":"a.go:10","problem":"os.O_CREATE | os.O_WRONLY","fix":"use \"x\"\nthen y","category":"correctness","est_minutes":15,"evidence":"a | b"}`
	objB = `{"severity":"LOW","file_line":"b.go:2","problem":"pb","fix":"fb","category":"style","est_minutes":2,"evidence":"eb"}`
	objC = `{"severity":"MEDIUM","file_line":"c.go:3","problem":"pc","fix":"fc","category":"security","est_minutes":5,"evidence":"ec"}`
)

var (
	findA = Finding{Severity: "HIGH", File: "a.go", Line: 10, Problem: "os.O_CREATE | os.O_WRONLY", Fix: "use \"x\"\nthen y", Category: "correctness", EstMinutes: 15, Evidence: "a | b"}
	findB = Finding{Severity: "LOW", File: "b.go", Line: 2, Problem: "pb", Fix: "fb", Category: "style", EstMinutes: 2, Evidence: "eb"}
	findC = Finding{Severity: "MEDIUM", File: "c.go", Line: 3, Problem: "pc", Fix: "fc", Category: "security", EstMinutes: 5, Evidence: "ec"}
)

func TestParseModelOutput_JSONBlock(t *testing.T) {
	content := "Here is my review.\n\n" + jsonBlock("["+objA+",\n"+objB+"]") + "Done.\n"
	assert.Equal(t, []Finding{findA, findB}, ParseModelOutput([]byte(content)))
}

func TestParseModelOutput_JSONFenceInfoString(t *testing.T) {
	for _, opener := range []string{"```json", "```JSON", "  ```json", "````json", "``` json", "```json  ", "```json\r"} {
		t.Run(opener, func(t *testing.T) {
			content := opener + "\n[" + objB + "]\n```\n"
			assert.Equal(t, []Finding{findB}, ParseModelOutput([]byte(content)))
		})
	}
	// Any other fence is a quoted example, never output.
	for _, opener := range []string{"```", "```text", "```jsonc", "```js"} {
		t.Run(opener, func(t *testing.T) {
			content := opener + "\n[" + objB + "]\n```\n"
			assert.Empty(t, ParseModelOutput([]byte(content)))
		})
	}
}

func TestParseModelOutput_UnionsEveryBlockInOrder(t *testing.T) {
	// Chunked reviews newline-join each chunk's output, so blocks arrive back to back.
	content := jsonBlock("["+objA+"]") + "chunk two\n" + jsonBlock("["+objB+"]") + jsonBlock("["+objC+"]")
	assert.Equal(t, []Finding{findA, findB, findC}, ParseModelOutput([]byte(content)))
}

func TestParseModelOutput_TruncationRecovery(t *testing.T) {
	cut := objC[:strings.Index(objC, `"fix"`)+8] // mid-string, inside the last object

	cases := []struct {
		name    string
		content string
		want    []Finding
	}{
		{"cut inside the final object keeps every complete one", "```json\n[" + objA + ",\n" + objB + ",\n" + cut, []Finding{findA, findB}},
		{"cut right after a complete object", "```json\n[" + objA + ",\n" + objB, []Finding{findA, findB}},
		{"cut after the trailing comma", "```json\n[" + objA + ",", []Finding{findA}},
		{"truncated first block, clean second block", "```json\n[" + objA + ",\n" + cut + "\n```\n" + jsonBlock("["+objB+"]"), []Finding{findA, findB}},
		{"a cut-off block is closed by the next chunk's opener", "```json\n[" + objA + ",\n" + cut + "\nChunk two prose.\n" + jsonBlock("["+objC+"]"), []Finding{findA, findC}},
		{"unterminated fence with a closed array", "```json\n[" + objA + "]\n", []Finding{findA}},
		{"braces and brackets inside strings are not structure", "```json\n[" + `{"severity":"LOW","file_line":"d.go:1","problem":"}] {[ \"}\"","fix":"","category":"","est_minutes":1,"evidence":""}` + ",\n" + cut, []Finding{{Severity: "LOW", File: "d.go", Line: 1, Problem: `}] {[ "}"`, EstMinutes: 1}}},
		{"interior syntax error keeps the elements before it", jsonBlock("[" + objA + ",\n" + `{"severity":"LOW","file_line":"x.go:1",}` + ",\n" + objB + "]"), []Finding{findA}},
		{"nothing recoverable", jsonBlock("[{\"severity\":"), nil},
		{"not JSON at all", jsonBlock("this is not json"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, ParseModelOutput([]byte(c.content)))
		})
	}
}

func TestParseModelOutput_JSONFieldRules(t *testing.T) {
	cases := []struct {
		name string
		obj  string
		want []Finding
	}{
		{"extra keys are ignored", `{"severity":"LOW","file_line":"a.go:1","problem":"p","confidence":"HIGH","nested":{"a":[1]}}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1, Problem: "p"}}},
		{"a model-supplied reviewer never reaches Finding.Reviewer (TD-016)", `{"severity":"LOW","file_line":"a.go:1","reviewer":"forged-agent"}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1}}},
		{"severity case is normalized", `{"severity":" high ","file_line":"a.go:1"}`, []Finding{{Severity: "HIGH", File: "a.go", Line: 1}}},
		{"est_minutes as a string", `{"severity":"LOW","file_line":"a.go:1","est_minutes":"15"}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1, EstMinutes: 15}}},
		{"est_minutes as a float", `{"severity":"LOW","file_line":"a.go:1","est_minutes":7.5}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1, EstMinutes: 7}}},
		{"est_minutes null or junk", `{"severity":"LOW","file_line":"a.go:1","est_minutes":null},{"severity":"LOW","file_line":"b.go:1","est_minutes":"soon"}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1}, {Severity: "LOW", File: "b.go", Line: 1}}},
		{"null string fields stay empty", `{"severity":"LOW","file_line":"a.go:1","fix":null}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1}}},
		{"unknown severity is dropped", `{"severity":"INFO","file_line":"a.go:1"}`, nil},
		{"missing location is dropped", `{"severity":"HIGH","file_line":" "}`, nil},
		{"a location with no line keeps line 0", `{"severity":"LOW","file_line":"pkg/x.go"}`, []Finding{{Severity: "LOW", File: "pkg/x.go"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseModelOutput([]byte(jsonBlock("[" + c.obj + "]")))
			assert.Equal(t, c.want, got)
			for _, f := range got {
				assert.Empty(t, f.Reviewer)
			}
		})
	}
}

func TestParseModelOutput_EmptyArrayAndSentinel(t *testing.T) {
	assert.Empty(t, ParseModelOutput([]byte(jsonBlock("[]"))))
	assert.Empty(t, ParseModelOutput([]byte(NoFindingsSentinel)))
	assert.True(t, IsNoFindings(NoFindingsSentinel))
}

func TestParseModelOutput_UnfencedArrayFallback(t *testing.T) {
	content := "See [the docs](https://example.com) first.\n" +
		"[\n" + objA + ",\n" + objB + "\n]\n" +
		"That is all.\n"
	assert.Equal(t, []Finding{findA, findB}, ParseModelOutput([]byte(content)))

	// A cut-off bare array recovers the same way a fenced one does.
	assert.Equal(t, []Finding{findA}, ParseModelOutput([]byte("["+objA+",\n"+objB[:20])))

	// A chunked review can mix a chunk that forgot the fence with one that did
	// not; both are read, in Content order (TD-016, live run 2026-09-25).
	withFence := "[" + objC + "]\n" + jsonBlock("["+objB+"]")
	assert.Equal(t, []Finding{findC, findB}, ParseModelOutput([]byte(withFence)))

	// Recovering a cut-off bare array stops at the next fence: a quoted example
	// below it is never read as a finding.
	quoted := "[" + objA + ",\n\nExample format:\n```text\n" + objC + "\n```\n"
	assert.Equal(t, []Finding{findA}, ParseModelOutput([]byte(quoted)))

	// Unclosed "[" lines are bounded, not quadratic: this must stay fast.
	assert.Empty(t, ParseModelOutput([]byte(strings.Repeat("[\n", 20000))))

	// A bare array quoted inside a non-json fence is an example.
	assert.Empty(t, ParseModelOutput([]byte("```\n["+objA+"]\n```\n")))
}

// The shapes a model slips into, and the only one a json_object response
// format allows, read like the fenced array (TD-022; vera, live run 2026-09-25).
func TestParseModelOutput_ObjectShapes(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []Finding
	}{
		{"wrapper in a fence", jsonBlock(`{"findings":[` + objA + "," + objB + `]}`), []Finding{findA, findB}},
		{"bare wrapper", `{"findings":[` + objA + `]}`, []Finding{findA}},
		{"bare single object", objA, []Finding{findA}},
		{"pretty-printed single object", "{\n" + objA[1:len(objA)-1] + "\n}", []Finding{findA}},
		{"single object in a fence", jsonBlock(objA), []Finding{findA}},
		{"bare object chunk beside a fenced chunk", objA + "\n" + jsonBlock("["+objB+"]"), []Finding{findA, findB}},
		{"cut-off wrapper keeps complete objects", `{"findings":[` + objA + "," + objB[:20], []Finding{findA}},
		{"an array is not re-read object by object", "[\n" + objA + ",\n" + objB + "\n]", []Finding{findA, findB}},
		{"empty wrapper", `{"findings":[]}`, nil},
		{"object with no severity is prose", `{"note":"hello"}`, nil},
		{"split file and line keys", `[{"severity":"HIGH","file":"a.go","line":"7","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e"}]`,
			[]Finding{{Severity: "HIGH", File: "a.go", Line: 7, Problem: "p", Fix: "f", Category: "c", EstMinutes: 1, Evidence: "e"}}},
		{"file_line wins over split keys", `[{"severity":"HIGH","file_line":"b.go:2","file":"a.go","line":7}]`,
			[]Finding{{Severity: "HIGH", File: "b.go", Line: 2}}},
		{"split keys without a file are dropped", `[{"severity":"HIGH","line":7}]`, nil},
		{"file already carrying a line", `[{"severity":"HIGH","file":"a.go:7","line":7}]`, []Finding{{Severity: "HIGH", File: "a.go", Line: 7}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, ParseModelOutput([]byte(c.content)))
		})
	}
}

// Every clean shape a model slipped into on the live panel, and the ones a
// json_object response format produces, count as a clean review; anything
// with other text does not.
func TestIsNoFindings_AcceptsCleanSlips(t *testing.T) {
	for _, in := range []string{
		"NO FINDINGS:\nNO FINDINGS",                     // greta, run 3
		"```json\n[\n]\n```\n```json\nNO FINDINGS\n```", // greta, run 1
		"NO FINDINGS.", "NO FINDINGS!", "[]", " [ ] ", `{"findings":[]}`,
		"```\nNO FINDINGS\n```", "NO FINDINGS\n\nNO FINDINGS",
	} {
		assert.True(t, IsNoFindings(in), "%q is a clean review", in)
	}
	for _, in := range []string{
		"NO FINDINGS HERE", "NO FINDINGSX", "[]x", "[1]", `{"findings":[{}]}`, `{"findings":[],"x":1}`,
		"No findings are present; all claims are verified.", "NO FINDINGS\nbut see line 3", "```json\n```", "{}",
		"```\nNO FINDINGS\n``` but a.go:3 has a nil deref", "```HIGH|a.go:1|nil deref|f\nNO FINDINGS",
	} {
		assert.False(t, IsNoFindings(in), "%q must not count as clean", in)
	}
}

func TestParseModelOutput_MixedJSONAndPipeRowsInContentOrder(t *testing.T) {
	content := "HIGH|p1.go:1|first pipe|f|c|1|e\n" +
		jsonBlock("["+objB+"]") +
		"LOW|p2.go:2|second pipe|f|c|2|e\n"
	got := ParseModelOutput([]byte(content))
	require.Len(t, got, 3)
	assert.Equal(t, "p1.go", got[0].File)
	assert.Equal(t, findB, got[1])
	assert.Equal(t, "p2.go", got[2].File)
}

func TestParseModelOutput_PipeRowsInsideAJSONFenceAreNotPipeFindings(t *testing.T) {
	content := "```json\nHIGH|a.go:1|pipe row in a json fence|f|c|1|e\n```\n"
	assert.Empty(t, ParseModelOutput([]byte(content)))
}

// TD-011: legacy pipe output that the new JSON scan also looks at — a `[`-led
// prose line and a non-json fence — parses exactly as it always did.
func TestParseModelOutput_LegacyPipeOutputUnchangedByJSONScan(t *testing.T) {
	plain := "HIGH|a.go:1|p|f|c|1|e\nLOW|b.go:2|q\n"
	withNoise := "[x](y) is the link I mean.\n" +
		"HIGH|a.go:1|p|f|c|1|e\n" +
		"```text\n[1, 2]\nCRITICAL|ex.go:1|example|f|c|1|e\n```\n" +
		"LOW|b.go:2|q\n"
	assert.Equal(t, ParseModelOutput([]byte(plain)), ParseModelOutput([]byte(withNoise)))
}

// TD-008: every overflow field past the seventh folds into EVIDENCE, joined by
// '/', and REVIEWER stays empty.
func TestParseModelOutput_MultiFieldOverflowFoldsIntoEvidence(t *testing.T) {
	got := ParseModelOutput([]byte("HIGH|a.go:1|p|f|c|1|e1|e2|mallory\n"))
	require.Len(t, got, 1)
	assert.Equal(t, "e1/e2/mallory", got[0].Evidence)
	assert.Empty(t, got[0].Reviewer)
}

// ---- ParseSource: the on-disk v2 file ----

func TestParseSource_V2TOONRoundTrip(t *testing.T) {
	in := []Finding{
		{Severity: "HIGH", File: "a.go", Line: 3, Problem: "flags := os.O_RDWR | os.O_CREATE", Fix: "line1\r\nline2", Category: "correctness", EstMinutes: 7, Evidence: "| h |\n|---|\n\"q\"", Reviewer: "bruce"},
		{Severity: "LOW", File: "pkg/x.go", Line: 0, Reviewer: "kai"},
	}
	var b strings.Builder
	require.NoError(t, WriteSourceV2(&b, in))

	res, err := ParseSource([]byte(b.String()))
	require.NoError(t, err)
	assert.Equal(t, in, res.Findings)
	assert.Empty(t, res.Skipped)
}

func TestParseSource_V2Empty(t *testing.T) {
	var b strings.Builder
	require.NoError(t, WriteSourceV2(&b, nil))
	res, err := ParseSource([]byte(b.String()))
	require.NoError(t, err)
	assert.Empty(t, res.Findings)
}

func TestParseSource_V2JSONEnvelope(t *testing.T) {
	var b strings.Builder
	require.NoError(t, encodeV2(&b, lossyPayload{Findings: []lossyRow{
		{Severity: "HIGH", FileLine: "a.go:1", Problem: "a | b", Fix: "x\ny", Category: "c", EstMinutes: 3, Evidence: "e", Reviewer: "bruce", Extra: textish{"t"}},
	}}))
	require.Contains(t, b.String(), `{"axi_format"`)

	res, err := ParseSource([]byte(b.String()))
	require.NoError(t, err)
	assert.Equal(t, []Finding{{Severity: "HIGH", File: "a.go", Line: 1, Problem: "a | b", Fix: "x\ny", Category: "c", EstMinutes: 3, Evidence: "e", Reviewer: "bruce"}}, res.Findings)
}

func TestParseSource_V2HostShapedEnvelope(t *testing.T) {
	data := VersionV2 + "\n" +
		`{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file_line":"a.go:3","problem":"p | q","fix":"f","category":"c","est_minutes":5,"evidence":"e","reviewer":"host"}]}}` + "\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, []Finding{{Severity: "HIGH", File: "a.go", Line: 3, Problem: "p | q", Fix: "f", Category: "c", EstMinutes: 5, Evidence: "e", Reviewer: "host"}}, res.Findings)
}

// v2Row1 is one valid envelope row, for error cases that hide it behind a bad key.
const v2Row1 = `{"severity":"HIGH","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e","reviewer":"host"}`

func TestParseSource_V2Errors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed envelope", `{"axi_format":"json","data":{"findings":[`},
		{"wrong axi_format", `{"axi_format":"toon","axi_notice":"","data":{"findings":[]}}`},
		{"findings outside data", `{"axi_format":"json","axi_notice":"","findings":[]}`},
		{"data without findings", `{"axi_format":"json","axi_notice":"","data":{}}`},
		{"a bare object is not an envelope", `{"findings":[]}`},
		{"neither table nor envelope", "hello world"},
		{"empty body", ""},
		{"wrong table name", "rows[1]{severity,file_line}:\n  HIGH,\"a.go:1\""},
		{"fewer rows than declared", "findings[2]{severity,file_line}:\n  HIGH,\"a.go:1\""},
		// TD-024/TD-044: a table must carry all eight v2 columns. A misspelled
		// one fails because the real column is then missing.
		{"missing columns", "findings[1]{severity,file_line}:\n  HIGH,\"a.go:1\""},
		{"misspelled column", "findings[1]{severity,file_line,problem,fix,category,est_minutes,evidence,reviewr}:\n  HIGH,\"a.go:1\",p,f,c,1,e,r"},
		// TD-025/TD-044: a host-written envelope with a typo key fails instead
		// of decoding that field as empty, because the real key is missing.
		{"misspelled row key", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file-line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e","reviewer":"host"}]}}`},
		{"row missing a key", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e"}]}}`},
		{"row key in the wrong case", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"Severity":"HIGH","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e","reviewer":"host"}]}}`},
		{"case-variant duplicate overwrites a key", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"LOW","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e","reviewer":"host","SEVERITY":"CRITICAL","Reviewer":"bruce"}]}}`},
		{"row is not an object", `{"axi_format":"json","axi_notice":"","data":{"findings":[1]}}`},
		{"trailing envelope", `{"axi_format":"json","axi_notice":"","data":{"findings":[]}}{"axi_format":"json","axi_notice":"","data":{"findings":[]}}`},
		{"trailing prose", `{"axi_format":"json","axi_notice":"","data":{"findings":[]}}` + "\nthanks"},
		{"trailing fence", `{"axi_format":"json","axi_notice":"","data":{"findings":[]}}` + "\n```"},
		// TD-050/TD-052: encoding/json matches envelope keys case-insensitively,
		// last one wins, so a case variant can silently replace the real key.
		{"top-level findings beside data", `{"axi_format":"json","axi_notice":"","findings":[` + v2Row1 + `],"data":{"findings":[]}}`},
		{"data key in the wrong case", `{"axi_format":"json","axi_notice":"","data":{"findings":[` + v2Row1 + `]},"DATA":{"findings":[]}}`},
		{"axi_format key in the wrong case", `{"AXI_FORMAT":"toon","axi_format":"json","axi_notice":"","data":{"findings":[]}}`},
		{"axi_notice key in the wrong case", `{"axi_format":"json","axi_notice":"","Axi_Notice":"x","data":{"findings":[]}}`},
		{"findings key in the wrong case", `{"axi_format":"json","axi_notice":"","data":{"findings":[` + v2Row1 + `],"FINDINGS":[]}}`},
		// TD-037: atcr never writes these values (ParseModelOutput drops an
		// unknown severity or empty location; the engine stamps every reviewer).
		{"unknown severity", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"BLOCKER","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e","reviewer":"host"}]}}`},
		{"lowercase severity", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"high","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e","reviewer":"host"}]}}`},
		{"empty file_line", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file_line":" ","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e","reviewer":"host"}]}}`},
		{"empty reviewer", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e","reviewer":""}]}}`},
		{"table unknown severity", "findings[1]{severity,file_line,problem,fix,category,est_minutes,evidence,reviewer}:\n  BLOCKER,\"a.go:1\",p,f,c,1,e,r"},
		{"table empty file_line", "findings[1]{severity,file_line,problem,fix,category,est_minutes,evidence,reviewer}:\n  HIGH,\"\",p,f,c,1,e,r"},
		{"table empty reviewer", "findings[1]{severity,file_line,problem,fix,category,est_minutes,evidence,reviewer}:\n  HIGH,\"a.go:1\",p,f,c,1,e,\"\""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := ParseSource([]byte(VersionV2 + "\n" + c.body + "\n"))
			assert.Error(t, err)
			assert.Empty(t, res.Findings)
		})
	}
}

// TD-044: v2 evolves additively, like v1. A reader requires the eight known
// columns or keys and ignores any others, so a newer atcr can add a field and
// an older atcr still reads the file.
func TestParseSource_V2ToleratesAdditiveFields(t *testing.T) {
	want := []Finding{{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Fix: "f", Category: "c", EstMinutes: 2, Evidence: "e", Reviewer: "r"}}
	cases := []struct {
		name string
		body string
	}{
		{"extra table column", "findings[1]{severity,file_line,problem,fix,category,est_minutes,evidence,reviewer,confidence}:\n  HIGH,\"a.go:1\",p,f,c,2,e,r,0.9"},
		{"extra column first, known columns reordered", "findings[1]{confidence,reviewer,evidence,est_minutes,category,fix,problem,file_line,severity}:\n  0.9,r,e,2,c,f,p,\"a.go:1\",HIGH"},
		{"extra row key", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":2,"evidence":"e","reviewer":"r","confidence":{"score":0.9}}]}}`},
		{"extra envelope and data keys", `{"axi_format":"json","axi_notice":"","extra":1,"data":{"schema":2,"findings":[{"severity":"HIGH","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":2,"evidence":"e","reviewer":"r"}]}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := ParseSource([]byte(VersionV2 + "\n" + c.body + "\n"))
			require.NoError(t, err)
			assert.Equal(t, want, res.Findings)
		})
	}
}

func TestParseReconciled_RejectsV2(t *testing.T) {
	_, err := ParseReconciled([]byte(VersionV2 + "\nfindings[0]:\n"))
	assert.True(t, errors.Is(err, ErrUnknownVersion), "there is no reconciled v2 shape: %v", err)
}

// TD-003: header dispatch, table-driven through both parsers.
func TestParse_HeaderDispatch(t *testing.T) {
	cases := []struct {
		name      string
		data      string
		source    error // nil = accepted
		reconcile error
	}{
		{"v1", Version + "\n", nil, nil},
		{"whitespace-padded v1", "  " + Version + "  \n", nil, nil},
		{"v2", VersionV2 + "\nfindings[0]:\n", nil, ErrUnknownVersion},
		{"unknown version", "# atcr-findings/v99\n", ErrUnknownVersion, ErrUnknownVersion},
		{"malformed version token", "# atcr-findings/v1x\n", ErrMissingHeader, ErrMissingHeader},
		{"empty input", "", ErrMissingHeader, ErrMissingHeader},
		{"comment before header", "# note\n" + Version + "\n", ErrMissingHeader, ErrMissingHeader},
		{"row with no header", "HIGH|a.go:1|p|f|c|1|e|r\n", ErrMissingHeader, ErrMissingHeader},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseSource([]byte(c.data))
			assertHeaderErr(t, c.source, err)
			_, err = ParseReconciled([]byte(c.data))
			assertHeaderErr(t, c.reconcile, err)
		})
	}
}

func assertHeaderErr(t *testing.T, want, got error) {
	t.Helper()
	if want == nil {
		assert.NoError(t, got)
		return
	}
	assert.True(t, errors.Is(got, want), "want %v, got %v", want, got)
}

// ParseSource's header errors name both versions it accepts.
func TestParseSource_HeaderErrorNamesBothVersions(t *testing.T) {
	_, err := ParseSource([]byte("nope\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), Version)
	assert.Contains(t, err.Error(), VersionV2)
}

// TD-019: a fence closes only on a marker at least as long as its opener
// (CommonMark), so a ```json example quoted inside a ````md fence stays quoted.
func TestParseModelOutput_LongerFenceQuotesAShorterJSONBlock(t *testing.T) {
	content := "The format looks like this:\n````md\n```json\n[" + objA + "]\n```\n````\n" + jsonBlock("["+objB+"]")
	assert.Equal(t, []Finding{findB}, ParseModelOutput([]byte(content)))

	// A pipe row quoted the same way is an example too.
	content = "````\n```\nHIGH|ex.go:1|p|f|c|1|e\n```\n````\nLOW|real.go:2|p|f|c|1|e\n"
	got := ParseModelOutput([]byte(content))
	require.Len(t, got, 1)
	assert.Equal(t, "real.go", got[0].File)
}
