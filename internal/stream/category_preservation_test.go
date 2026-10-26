package stream_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
	reclib "github.com/samestrin/atcr/reconcile"
)

// Epic 51.0 AC3/AC4: canonicalization at the parse boundary must not change what
// the three category-driven decision sites decide. Each site branches on
// `out-of-scope` read from the raw Finding.Category:
//
//   - internal/reconcile/gate.go IsFailing — CI pass/fail; out-of-scope never
//     fails, ahead of any verdict.
//   - internal/fanout/grounding.go isGrounded — out-of-scope is exempt from the
//     grounding discard.
//   - internal/localdebt/reconcile.go PersistForReconcile — out-of-scope never
//     persists to technical debt.
//
// decisionSites pins each site's exemption expression as written. The tests
// below evaluate those exact expressions, so a site that starts reading
// CanonicalCategory, or changes how it normalizes, fails here first.
var decisionSites = []struct {
	file, fn, expr string
	exempt         func(category string) bool
}{
	{
		"internal/reconcile/gate.go", "IsFailing",
		"strings.ToLower(strings.TrimSpace(category)) == CategoryOutOfScope",
		func(c string) bool { return strings.ToLower(strings.TrimSpace(c)) == reclib.CategoryOutOfScope },
	},
	{
		"internal/fanout/grounding.go", "isGrounded",
		"strings.EqualFold(strings.TrimSpace(f.Category), reclib.CategoryOutOfScope)",
		func(c string) bool { return strings.EqualFold(strings.TrimSpace(c), reclib.CategoryOutOfScope) },
	},
	{
		"internal/localdebt/reconcile.go", "PersistForReconcile",
		"strings.ToLower(strings.TrimSpace(f.Category)) == reclib.CategoryOutOfScope",
		func(c string) bool { return strings.ToLower(strings.TrimSpace(c)) == reclib.CategoryOutOfScope },
	},
}

// categoryVariants is every raw CATEGORY the preservation test parses: the
// token itself, spellings the sites already treat as the token (case and
// surrounding whitespace), near-misses they do not, and every vocabulary and
// merge-table word.
func categoryVariants() []string {
	v := []string{
		"out-of-scope", "OUT-OF-SCOPE", "Out-Of-Scope", " out-of-scope", "out-of-scope ", "\tout-of-scope",
		"out of scope", "Out Of Scope", "outofscope", "OutOfScope", "out_of_scope", "out-of-scopes",
		"Out-Of-Scopes", "out-of-scope.", "out–of–scope", "out-of-ſcope", "scope", "oos", "out", "in-scope",
	}
	for w := range reclib.CategoryMerges() {
		v = append(v, w, strings.ToUpper(w), w+"s")
	}
	for _, m := range reclib.Categories() {
		v = append(v, m, strings.ToUpper(m)+" ", m+"s")
	}
	return v
}

// parsed is one parse path's output beside the raw category of each row.
type parsed struct {
	raw []string
	got []stream.Finding
}

// parsePaths runs the variants through every parse path that builds a
// stream.Finding and returns the findings in input order.
func parsePaths(t *testing.T, cats []string) map[string]parsed {
	t.Helper()
	out := make(map[string]parsed)
	quoted := func(s string) string {
		b, err := json.Marshal(s)
		require.NoError(t, err)
		return string(b)
	}
	// Pipe rows cannot carry a tab or a pipe inside a cell; skip only those.
	pipeSafe := func(c string) bool { return !strings.ContainsAny(c, "|\t\n") }

	var src, rec, model strings.Builder
	src.WriteString(stream.Version + "\n")
	rec.WriteString(stream.Version + "\n")
	var pipeCats []string
	for _, c := range cats {
		if !pipeSafe(c) {
			continue
		}
		pipeCats = append(pipeCats, c)
		src.WriteString("HIGH|a.go:1|p|f|" + c + "|5|e|bruce\n")
		rec.WriteString("HIGH|a.go:1|p|f|" + c + "|5|e|bruce,kai|high\n")
		model.WriteString("HIGH|a.go:1|p|f|" + c + "|5|e\n")
	}
	res, err := stream.ParseSource([]byte(src.String()))
	require.NoError(t, err)
	out["v1 per-source"] = parsed{pipeCats, res.Findings}
	res, err = stream.ParseReconciled([]byte(rec.String()))
	require.NoError(t, err)
	out["v1 reconciled"] = parsed{pipeCats, res.Findings}
	out["model output pipe rows"] = parsed{pipeCats, stream.ParseModelOutput([]byte(model.String()))}

	var js strings.Builder
	js.WriteString("[")
	for i, c := range cats {
		if i > 0 {
			js.WriteString(",")
		}
		js.WriteString(`{"severity":"HIGH","file_line":"a.go:1","problem":"p","fix":"f","category":` + quoted(c) + `,"est_minutes":5,"evidence":"e"}`)
	}
	js.WriteString("]")
	out["model output JSON"] = parsed{cats, stream.ParseModelOutput([]byte(js.String()))}

	in := make([]stream.Finding, len(cats))
	for i, c := range cats {
		in[i] = stream.Finding{Severity: "HIGH", File: "a.go", Line: 1, Category: c, Reviewer: "bruce"}
	}
	var toon strings.Builder
	require.NoError(t, stream.WriteSourceV2(&toon, in))
	res, err = stream.ParseSource([]byte(toon.String()))
	require.NoError(t, err)
	out["v2 TOON"] = parsed{cats, res.Findings}

	env, err := stream.EncodeV2EnvelopeForTest(cats)
	require.NoError(t, err)
	res, err = stream.ParseSource([]byte(env))
	require.NoError(t, err)
	out["v2 JSON envelope"] = parsed{cats, res.Findings}

	for name, p := range out {
		require.Len(t, p.got, len(p.raw), "%s: a row was dropped or added", name)
	}
	return out
}

// Every decision site sees the reviewer's raw word after parsing, and no
// variant the sites did not already exempt is exempted through the canonical
// field: if a consumer were switched to CanonicalCategory, the outcome would be
// unchanged for every variant tested.
func TestCategoryDecisionSites_SeeUnchangedInputs(t *testing.T) {
	for name, p := range parsePaths(t, categoryVariants()) {
		t.Run(name, func(t *testing.T) {
			for i, f := range p.got {
				raw := p.raw[i]
				assert.Equal(t, raw, f.Category, "raw category must reach the decision sites byte-identical")
				for _, s := range decisionSites {
					if !s.exempt(raw) {
						assert.False(t, s.exempt(f.Category), "%s: raw %q newly exempt via Category", s.fn, raw)
						assert.False(t, s.exempt(f.CanonicalCategory), "%s: raw %q newly exempt via canonical %q", s.fn, raw, f.CanonicalCategory)
					}
				}
				// The gate itself, not only its pinned expression: a HIGH
				// finding is failing at LOW threshold exactly when it is not
				// out-of-scope, whichever field is read.
				gate := reconcile.IsFailing(f.Severity, f.Category, nil, "LOW", false)
				assert.Equal(t, !decisionSites[0].exempt(raw), gate, "IsFailing on raw %q", raw)
				if gate {
					assert.True(t, reconcile.IsFailing(f.Severity, f.CanonicalCategory, nil, "LOW", false),
						"raw %q: canonical %q would un-gate a failing finding", raw, f.CanonicalCategory)
				}
			}
		})
	}
}

// The variant list must include near-misses the sites do not exempt, or the
// "newly exempt" assertions above check nothing.
func TestCategoryVariants_IncludeNonExemptNearMisses(t *testing.T) {
	var near int
	for _, v := range categoryVariants() {
		if strings.Contains(strings.ToLower(v), "scope") && !decisionSites[0].exempt(v) {
			near++
		}
	}
	assert.GreaterOrEqual(t, near, 10)
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found")
		dir = parent
	}
}

// Each decision site still holds its exemption expression, reading the raw
// field, inside the function the plan names.
func TestCategoryDecisionSites_ReadRawCategory(t *testing.T) {
	root := moduleRoot(t)
	for _, s := range decisionSites {
		t.Run(s.fn, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(root, s.file), nil, 0)
			require.NoError(t, err)
			var body *ast.BlockStmt
			for _, d := range file.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == s.fn && fd.Recv == nil {
					body = fd.Body
				}
			}
			require.NotNil(t, body, "%s: func %s not found", s.file, s.fn)
			var found bool
			ast.Inspect(body, func(n ast.Node) bool {
				e, ok := n.(ast.Expr)
				if !ok || found {
					return !found
				}
				var b bytes.Buffer
				require.NoError(t, printer.Fprint(&b, fset, e))
				found = b.String() == s.expr
				return !found
			})
			assert.True(t, found, "%s %s no longer contains %q", s.file, s.fn, s.expr)
		})
	}
}

// canonicalSelectors returns every `x.CanonicalCategory` selector in src: a
// read or write of the field, or a call of the canonicalizer from outside
// package stream. Inside package stream the canonicalizer's own calls and the
// composite-literal keys that set the field are plain identifiers, not
// selectors, so the parse boundary itself is not counted.
func canonicalSelectors(t *testing.T, fset *token.FileSet, path string, src any) []string {
	t.Helper()
	file, err := parser.ParseFile(fset, path, src, 0)
	require.NoError(t, err)
	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "CanonicalCategory" {
			hits = append(hits, fset.Position(sel.Pos()).String())
		}
		return true
	})
	return hits
}

// AC4: the canonical field has zero non-test consumers in this epic. Stated as
// a property, not a census, so it cannot drift.
func TestCanonicalCategory_ZeroNonTestConsumers(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	var hits []string
	var scanned int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		hits = append(hits, canonicalSelectors(t, fset, path, nil)...)
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, scanned, 100, "walk found too few Go files; is the module root right?")
	assert.Empty(t, hits, "CanonicalCategory has a non-test consumer; epic 51.0 AC4 forbids migrating any consumer")
}

// The scanner sees the shapes a migrated consumer would take.
func TestCanonicalSelectors_DetectsConsumers(t *testing.T) {
	src := `package p
func a(f stream.Finding) bool { return f.CanonicalCategory == "out-of-scope" }
func b(raw string) string { return stream.CanonicalCategory(raw) }
func c(f *stream.Finding) { f.CanonicalCategory = "x" }
func d() string { return CanonicalCategory("x") }
`
	hits := canonicalSelectors(t, token.NewFileSet(), "p.go", src)
	assert.Len(t, hits, 3)
}
