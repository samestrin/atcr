package stream

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// v2Body strips the "# atcr-findings/v2" line. DecodeTabular reads the first
// non-blank line as the table header, so the version line must go first.
func v2Body(t *testing.T, out string) string {
	t.Helper()
	head, body, ok := strings.Cut(out, "\n")
	require.True(t, ok, "v2 output has no header line: %q", out)
	require.Equal(t, VersionV2, head)
	return body
}

func decodeV2(t *testing.T, out string) *goaxi.Document {
	t.Helper()
	body := v2Body(t, out)
	require.False(t, strings.HasPrefix(body, `{"axi_format"`), "expected the TOON path, got the JSON envelope: %s", body)
	doc, err := goaxi.DecodeTabular(strings.NewReader(body))
	require.NoError(t, err)
	return doc
}

func writeV2(t *testing.T, findings []Finding) string {
	t.Helper()
	var b strings.Builder
	require.NoError(t, WriteSourceV2(&b, findings))
	return b.String()
}

var v2Columns = []string{"severity", "file_line", "problem", "fix", "category", "est_minutes", "evidence", "reviewer"}

func TestWriteSourceV2_EncodesTOONTable(t *testing.T) {
	findings := []Finding{
		{Severity: "HIGH", File: "a.go", Line: 10, Problem: "p", Fix: "f", Category: "security", EstMinutes: 20, Evidence: "ev", Reviewer: "bruce"},
		{Severity: "LOW", File: "b/c.go", Line: 0, Problem: "q", Fix: "g", Category: "style", EstMinutes: 0, Evidence: "ev2", Reviewer: "kai"},
	}
	doc := decodeV2(t, writeV2(t, findings))

	assert.Equal(t, "findings", doc.Name)
	assert.Equal(t, v2Columns, doc.Fields)
	assert.Equal(t, len(findings), doc.Declared)
	require.Len(t, doc.Rows, 2)
	assert.Equal(t, map[string]string{
		"severity": "HIGH", "file_line": "a.go:10", "problem": "p", "fix": "f",
		"category": "security", "est_minutes": "20", "evidence": "ev", "reviewer": "bruce",
	}, doc.Rows[0])
	// file_line mirrors the v1 FILE:LINE column, ":0" included.
	assert.Equal(t, "b/c.go:0", doc.Rows[1]["file_line"])
}

func TestWriteSourceV2_EmptyAndNilSlices(t *testing.T) {
	want := VersionV2 + "\nfindings[0]:\n"
	assert.Equal(t, want, writeV2(t, nil))
	assert.Equal(t, want, writeV2(t, []Finding{}))

	doc := decodeV2(t, want)
	assert.Empty(t, doc.Rows)
}

func TestWriteSourceV2_EmptyFieldsStayEmptyStrings(t *testing.T) {
	out := writeV2(t, []Finding{{Severity: "LOW", File: "a.go", Line: 1, Reviewer: "otto"}})
	assert.Contains(t, out, `""`, "an empty cell is written as a quoted empty string")

	doc := decodeV2(t, out)
	require.Len(t, doc.Rows, 1)
	for _, k := range []string{"problem", "fix", "category", "evidence"} {
		v, ok := doc.Rows[0][k]
		assert.True(t, ok, "column %q is missing", k)
		assert.Equal(t, "", v, "column %q", k)
	}
}

// TestWriteSourceV2_AdversarialRoundTrip is the core promise of v2: no field is
// rewritten on the way to disk. Each field is compared on its own so a failure
// names the field.
func TestWriteSourceV2_AdversarialRoundTrip(t *testing.T) {
	long := strings.Repeat("line with a | pipe and \"quotes\"\n", 200) // > 5,000 chars, multiline
	require.Greater(t, len(long), 5000)

	cases := []struct {
		name string
		f    Finding
	}{
		{"literal pipe", Finding{Problem: "`os.O_CREATE | os.O_WRONLY`"}},
		{"double quotes", Finding{Fix: "Use `strconv.Quote(\"x\")`"}},
		{"CRLF multiline", Finding{Evidence: "-old line\r\n+new line\r\n context"}},
		{"LF multiline", Finding{Evidence: "-old line\n+new line\n context"}},
		{"bitwise OR", Finding{Problem: "flags := os.O_RDWR | os.O_CREATE | os.O_TRUNC"}},
		{"markdown table", Finding{Fix: "| a | b |\n|---|---|\n| 1 | 2 |"}},
		{"regex alternation and shell pipe", Finding{Fix: "grep -E '(foo|bar)' x | sort"}},
		{"TOON-looking text", Finding{Evidence: "findings[2]{a,b}:\n  x,y\n- item: 1"}},
		{"long multiline", Finding{Evidence: long}},
		{"several adversarial fields", Finding{Problem: "a | b", Fix: "one\ntwo", Evidence: "| h |\n|---|"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.f
			in.Severity, in.File, in.Line, in.Category, in.EstMinutes, in.Reviewer = "HIGH", "a.go", 3, "correctness", 7, "bruce"

			doc := decodeV2(t, writeV2(t, []Finding{in}))
			require.Len(t, doc.Rows, 1)
			row := doc.Rows[0]
			assert.Equal(t, in.Problem, row["problem"], "problem")
			assert.Equal(t, in.Fix, row["fix"], "fix")
			assert.Equal(t, in.Evidence, row["evidence"], "evidence")
			assert.Equal(t, strconv.Itoa(in.EstMinutes), row["est_minutes"])
			assert.Equal(t, "a.go:3", row["file_line"])
			assert.Equal(t, "bruce", row["reviewer"])
		})
	}
}

// TestWriteSourceV2_SanitizeBoundary pins go-axi's sanitizer as the ONLY
// rewrite on the v2 path: each stripped class is removed, nothing else is
// touched.
func TestWriteSourceV2_SanitizeBoundary(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"ESC", "a\x1bb | c", "ab | c"},
		{"NUL", "a\x00b", "ab"},
		{"form feed and vertical tab", "a\fb\vc", "abc"},
		{"DEL", "a\x7fb", "ab"},
		{"C1 control", "a\u0085b", "ab"},
		{"line and paragraph separators", "a\u2028b\u2029c", "abc"},
		{"invalid UTF-8", "a\xffb", "ab"},
		{"tab, LF, and CR are kept", "a\tb\nc\r\nd", "a\tb\nc\r\nd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := decodeV2(t, writeV2(t, []Finding{{Severity: "LOW", File: "a.go", Line: 1, Problem: c.in}}))
			require.Len(t, doc.Rows, 1)
			assert.Equal(t, c.want, doc.Rows[0]["problem"])
		})
	}
}

func TestWriteSourceV2_TruncatedBody(t *testing.T) {
	out := writeV2(t, []Finding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "first finding", Reviewer: "r"},
		{Severity: "LOW", File: "b.go", Line: 2, Problem: "second finding text", Reviewer: "r"},
	})
	body := v2Body(t, out)

	// Cut inside a quoted cell of the last row: a hard decode error.
	cut := strings.LastIndex(body, "second finding") + len("second")
	_, err := goaxi.DecodeTabular(strings.NewReader(body[:cut]))
	assert.Error(t, err)

	// Cut on a row boundary: fewer physical rows than declared, reported, not an error.
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	require.Len(t, lines, 3)
	doc, err := goaxi.DecodeTabular(strings.NewReader(strings.Join(lines[:2], "\n") + "\n"))
	require.NoError(t, err)
	assert.Greater(t, doc.Declared, len(doc.Rows))
}

// textish implements encoding.TextMarshaler, which toon-go silently drops, so
// go-axi routes any payload carrying one to the JSON envelope.
type textish struct{ s string }

func (x textish) MarshalText() ([]byte, error) { return []byte(x.s), nil }

// lossyRow is the real v2 row's columns plus one field TOON cannot carry. The
// columns are restated, not embedded: go-axi v0.3.1 does not sanitize fields
// reached through an unexported embedded struct (TD-012). Extra also reaches the
// JSON envelope as a ninth key, the shape a newer atcr's additive field takes;
// the envelope reader must ignore it (TD-044).
type lossyRow struct {
	Severity   string `toon:"severity" json:"severity"`
	FileLine   string `toon:"file_line" json:"file_line"`
	Problem    string `toon:"problem" json:"problem"`
	Fix        string `toon:"fix" json:"fix"`
	Category   string `toon:"category" json:"category"`
	EstMinutes int    `toon:"est_minutes" json:"est_minutes"`
	Evidence   string `toon:"evidence" json:"evidence"`
	Reviewer   string `toon:"reviewer" json:"reviewer"`
	Extra      any    `toon:"extra" json:"extra,omitempty"`
}

type lossyPayload struct {
	Findings []lossyRow `toon:"findings" json:"findings"`
}

type envelopeRow struct {
	Severity   string `json:"severity"`
	FileLine   string `json:"file_line"`
	Problem    string `json:"problem"`
	Fix        string `json:"fix"`
	Category   string `json:"category"`
	EstMinutes int    `json:"est_minutes"`
	Evidence   string `json:"evidence"`
	Reviewer   string `json:"reviewer"`
}

// The fixtures above restate v2Row's tags by hand; if v2Row changes, they must
// follow or the fallback test checks a stale shape.
func TestV2Fixtures_MatchV2RowTags(t *testing.T) {
	tags := func(v any) []string {
		var out []string
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Name == "Extra" {
				continue
			}
			out = append(out, f.Name+" "+string(f.Tag))
		}
		return out
	}
	want := tags(v2Row{})
	assert.Equal(t, want, tags(lossyRow{}))

	var jsonOnly []string
	for _, s := range want {
		name, tag, _ := strings.Cut(s, " ")
		jsonOnly = append(jsonOnly, name+" "+tag[strings.Index(tag, "json:"):])
	}
	assert.Equal(t, jsonOnly, tags(envelopeRow{}))
}

func TestEncodeV2_FallsBackToJSONEnvelope(t *testing.T) {
	rows := []lossyRow{
		{Severity: "HIGH", FileLine: "a.go:1", Problem: "a | b", Fix: "x\ny", Category: "c", EstMinutes: 3, Evidence: "ctl\x1bbyte", Reviewer: "bruce"},
		// Only this row carries the lossy value, yet the WHOLE document flips:
		// go-axi decides TOON-vs-JSON per document, never per field or row.
		{Severity: "LOW", FileLine: "b.go:2", Problem: "q", Reviewer: "kai", Extra: textish{"t"}},
	}
	var b strings.Builder
	require.NoError(t, encodeV2(&b, lossyPayload{Findings: rows}))

	body := v2Body(t, b.String())
	require.True(t, strings.HasPrefix(body, `{"axi_format"`), "fallback must start with the routable prefix: %s", body)
	assert.NotContains(t, body, "\x1b", "the envelope carries the sanitized value")
	assert.NotContains(t, body, `"Findings"`)
	assert.NotContains(t, body, `"Key"`)

	var env struct {
		Format string `json:"axi_format"`
		Notice string `json:"axi_notice"`
		Data   struct {
			Findings []envelopeRow `json:"findings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &env))
	assert.Equal(t, "json", env.Format)
	assert.NotEmpty(t, env.Notice)
	require.Len(t, env.Data.Findings, 2)
	assert.Equal(t, envelopeRow{Severity: "HIGH", FileLine: "a.go:1", Problem: "a | b", Fix: "x\ny", Category: "c", EstMinutes: 3, Evidence: "ctlbyte", Reviewer: "bruce"}, env.Data.Findings[0])
	assert.Equal(t, "b.go:2", env.Data.Findings[1].FileLine)
}

func TestEncodeV2_CycleIsATypedError(t *testing.T) {
	m := map[string]any{}
	m["self"] = m

	var b strings.Builder
	err := encodeV2(&b, m)
	require.Error(t, err)
	var cyc *goaxi.CycleError
	assert.True(t, errors.As(err, &cyc), "go-axi's typed error must survive wrapping: %v", err)
	assert.True(t, strings.HasPrefix(err.Error(), "encoding v2 findings: "), err.Error())
	assert.Equal(t, VersionV2+"\n", b.String(), "no body bytes after the header")
}

func TestWriteSourceV2_WriteErrorsPropagate(t *testing.T) {
	findings := []Finding{{Severity: "LOW", File: "a.go", Line: 1}}

	err := WriteSourceV2(&failingWriter{failAfter: 0}, findings)
	require.ErrorIs(t, err, errGoldenWrite)
	assert.Equal(t, "writing v2 findings header: disk full", err.Error())

	err = WriteSourceV2(&failingWriter{failAfter: 1}, findings)
	require.ErrorIs(t, err, errGoldenWrite)
	assert.Equal(t, "encoding v2 findings: disk full", err.Error())
}

func TestV2HeaderIsDistinctFromV1(t *testing.T) {
	require.NotEqual(t, Version, VersionV2)
	findings := []Finding{{Severity: "LOW", File: "a.go", Line: 1}}

	var v1 strings.Builder
	require.NoError(t, WriteSource(&v1, findings))
	assert.True(t, strings.HasPrefix(v1.String(), Version+"\n"))
	assert.True(t, strings.HasPrefix(writeV2(t, findings), VersionV2+"\n"))
}

// TestV2_IsolatedFromV1Writer keeps a later v1 deprecation a file delete: v2.go
// never touches the lossy v1 helpers and declares no package-level var other
// than compiled regexps.
func TestV2_IsolatedFromV1Writer(t *testing.T) {
	src, err := os.ReadFile("v2.go")
	require.NoError(t, err)
	f, err := parser.ParseFile(token.NewFileSet(), "v2.go", src, 0)
	require.NoError(t, err)

	// Identifiers only, so a comment naming the helpers is not a violation.
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		switch id.Name {
		case "escapeField", "fieldReplacer":
			t.Errorf("internal/stream/v2.go must not reference escapeField/fieldReplacer (v1-only lossy helpers)")
		case "write", "WriteSource", "WriteReconciled":
			t.Errorf("internal/stream/v2.go must not call the v1 writer %s, which escapes every field", id.Name)
		}
		return true
	})

	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			assert.Len(t, vs.Values, len(vs.Names), "v2.go declares an uninitialized package-level var")
			for _, v := range vs.Values {
				assert.True(t, isMustCompile(v), "v2.go declares a package-level var that is not a compiled regexp")
			}
		}
	}
}

func isMustCompile(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "MustCompile"
}

// TestSelectFindingsFile pins the one .toon-first selection rule every atcr
// reader of a findings directory shares (AC 04-02): findings.toon when it is a
// regular file, else findings.txt when it exists, else an fs.ErrNotExist error.
// A findings.toon that is not a regular file counts as absent.
func TestSelectFindingsFile(t *testing.T) {
	const v1 = Version + "\n"
	const v2 = VersionV2 + "\n"
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  string // file name selected; "" means fs.ErrNotExist
	}{
		{"both present picks toon", func(t *testing.T, dir string) {
			writeFile(t, dir, "findings.toon", v2)
			writeFile(t, dir, "findings.txt", v1)
		}, "findings.toon"},
		{"toon only", func(t *testing.T, dir string) {
			writeFile(t, dir, "findings.toon", v2)
		}, "findings.toon"},
		{"txt only", func(t *testing.T, dir string) {
			writeFile(t, dir, "findings.txt", v1)
		}, "findings.txt"},
		{"neither", func(t *testing.T, dir string) {}, ""},
		{"toon is a directory", func(t *testing.T, dir string) {
			require.NoError(t, os.Mkdir(filepath.Join(dir, "findings.toon"), 0o755))
			writeFile(t, dir, "findings.txt", v1)
		}, "findings.txt"},
		{"toon is a symlink", func(t *testing.T, dir string) {
			target := filepath.Join(t.TempDir(), "elsewhere.toon")
			require.NoError(t, os.WriteFile(target, []byte(v2), 0o644))
			if err := os.Symlink(target, filepath.Join(dir, "findings.toon")); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			writeFile(t, dir, "findings.txt", v1)
		}, "findings.txt"},
		{"txt is a symlink", func(t *testing.T, dir string) {
			target := filepath.Join(t.TempDir(), "elsewhere.txt")
			require.NoError(t, os.WriteFile(target, []byte(v1), 0o644))
			if err := os.Symlink(target, filepath.Join(dir, "findings.txt")); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
		}, ""},
		{"txt is a directory", func(t *testing.T, dir string) {
			require.NoError(t, os.Mkdir(filepath.Join(dir, "findings.txt"), 0o755))
		}, ""},
		{"toon symlink and no txt", func(t *testing.T, dir string) {
			target := filepath.Join(t.TempDir(), "elsewhere.toon")
			require.NoError(t, os.WriteFile(target, []byte(v2), 0o644))
			if err := os.Symlink(target, filepath.Join(dir, "findings.toon")); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			got, err := SelectFindingsFile(dir)
			if tc.want == "" {
				require.Error(t, err)
				assert.True(t, errors.Is(err, fs.ErrNotExist), "want fs.ErrNotExist, got %v", err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(dir, tc.want), got)
		})
	}
}

// TestSelectFindingsFile_MissingDirIsNotExist keeps a review with no
// sources/pool directory on every reader's "missing" branch.
func TestSelectFindingsFile_MissingDirIsNotExist(t *testing.T) {
	_, err := SelectFindingsFile(filepath.Join(t.TempDir(), "no", "such", "dir"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "want fs.ErrNotExist, got %v", err)
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
}

// atcr never writes a non-regular findings.toon, so one is a tampering signal.
// Selection still reads findings.txt, but says why on stderr (TD-031).
func TestSelectFindingsFile_WarnsOnANonRegularToon(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "findings.toon"), 0o755))
	writeFile(t, dir, "findings.txt", Version+"\n")

	var got string
	stderr := captureStderr(t, func() {
		var err error
		got, err = SelectFindingsFile(dir)
		require.NoError(t, err)
	})
	assert.Equal(t, filepath.Join(dir, "findings.txt"), got)
	assert.Contains(t, stderr, filepath.Join(dir, "findings.toon"))
	assert.Contains(t, stderr, "not a regular file")

	clean := t.TempDir()
	writeFile(t, clean, "findings.txt", Version+"\n")
	assert.Empty(t, captureStderr(t, func() { _, _ = SelectFindingsFile(clean) }), "an absent findings.toon is not a warning")
}

// A findings.toon deleted after selection is absent, not a missing pool: the
// read selects again and finds findings.txt (TD-030).
func TestReadPoolFindings_ToonDeletedAfterSelectionReadsTxt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "findings.toon", VersionV2+"\n")
	writeFile(t, dir, "findings.txt", Version+"\n"+"HIGH|a.go:1|p|f|c|1|e|r\n")
	orig := readFindingsFile
	t.Cleanup(func() { readFindingsFile = orig })
	readFindingsFile = func(path string) ([]byte, error) {
		if filepath.Base(path) == "findings.toon" {
			require.NoError(t, os.Remove(path))
		}
		return orig(path)
	}

	res, err := ReadPoolFindings(dir)
	require.NoError(t, err)
	assert.Len(t, res.Findings, 1)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()
	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	return buf.String()
}
