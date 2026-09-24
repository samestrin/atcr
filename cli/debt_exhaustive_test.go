package cli

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// debt_exhaustive_test.go is the guard AC 01-03 exists for.
//
// The localdebt.Status* constants are referenced NOWHERE outside
// internal/localdebt/, and no test tied any CLI vocabulary to them. That gap is
// not theoretical: a status could be added to the enum, pass every existing
// test, and still be unwritable through `debt resolve` and unfilterable through
// `debt list` — a ground-truth signal that stays permanently empty for a reason
// nothing on screen explains.
//
// The fix is to derive the expected set from the SOURCE rather than from a
// hand-typed literal list, the same rationale internal/reconcile's
// metrics_doc_drift_test.go gives for deriving its counter set from source: a
// literal list drifts the moment a constant is added and the list is not, so the
// guard silently stops guarding the very next status.

// localdebtStatusConstants returns every Status* constant the localdebt package
// declares, as const-name -> value, read from the package's own sources.
func localdebtStatusConstants(t testing.TB) map[string]string {
	t.Helper()
	return scanStatusConstants(t, filepath.Join("..", "internal", "localdebt"))
}

// scanStatusConstants is the directory-scanning core of localdebtStatusConstants,
// split out so a fixture directory can drive it: a Status constant whose value is
// not a plain string literal has no reason to exist in internal/localdebt today,
// so the loud-failure contract below can only be exercised against synthetic
// source.
func scanStatusConstants(t testing.TB, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "the localdebt package sources must be readable from cli/")

	out := map[string]string{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		require.NoError(t, err, "parsing %s", name)
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range vs.Names {
					if !strings.HasPrefix(ident.Name, "Status") {
						continue
					}
					// A Status-prefixed constant the scanner cannot read a plain string
					// literal from used to be dropped with a silent `continue` — three
					// arms: no explicit value (implicit const-block repetition), a
					// computed value or typed conversion, and a non-STRING literal. The
					// guard then covered only the readable constants while the enum
					// carried more. Fail loudly instead; the fix is to spell the value
					// as a string literal so the guard can read it.
					if i >= len(vs.Values) {
						t.Errorf("%s: Status-prefixed constant has no explicit value (implicit const-block repetition); spell its string value so this guard can read it", ident.Name)
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok {
						t.Errorf("%s: Status-prefixed constant value is not a literal (computed expression or conversion); spell its string value so this guard can read it", ident.Name)
						continue
					}
					if lit.Kind != token.STRING {
						t.Errorf("%s: Status-prefixed constant value is a non-STRING literal; spell its string value so this guard can read it", ident.Name)
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					out[ident.Name] = v
				}
			}
		}
	}
	return out
}

// errorRecorder is a testing.TB whose Errorf is recorded instead of failing the
// running test, so the loud-failure contract of scanStatusConstants can be
// asserted on rather than merely observed.
type errorRecorder struct {
	testing.TB
	errors []string
}

func (e *errorRecorder) Errorf(format string, args ...any) {
	e.errors = append(e.errors, fmt.Sprintf(format, args...))
}

// Every Status-prefixed constant the scanner cannot read a plain string literal
// from — implicit const-block repetition, a computed value, a typed conversion, a
// non-STRING literal — used to be dropped with a silent `continue`. The guard then
// covered only the readable constants while the enum carried more, so a new status
// could ship unaccounted-for with the whole suite green. Each unreadable name must
// be reported loudly instead.
func TestScanStatusConstantsErrorsOnUnreadableValues(t *testing.T) {
	dir := t.TempDir()
	src := `package localdebt

const (
	StatusGood = "good"
	StatusImplicit
	StatusComputed = statusPrefix + "x"
	StatusTyped    = Status("x")
	StatusInt      = 3
)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o600))

	rec := &errorRecorder{TB: t}
	got := scanStatusConstants(rec, dir)
	assert.Equal(t, map[string]string{"StatusGood": "good"}, got,
		"only the plain-string-literal constant is collected")
	assert.Len(t, rec.errors, 4,
		"each Status-prefixed constant without a plain string literal is reported, not dropped")
	for _, name := range []string{"StatusImplicit", "StatusComputed", "StatusTyped", "StatusInt"} {
		found := false
		for _, msg := range rec.errors {
			if strings.Contains(msg, name) {
				found = true
			}
		}
		assert.True(t, found, "%s must be named in the scanner's report", name)
	}

	// The real package still scans clean: its constants are all plain literals,
	// so the loud-failure arms stay dormant there.
	clean := &errorRecorder{TB: t}
	_ = scanStatusConstants(clean, filepath.Join("..", "internal", "localdebt"))
	assert.Empty(t, clean.errors, "the real localdebt package has nothing for the scanner to complain about")
}

// debtAddExcluded records, per status VALUE, why `debt add` does not accept it.
// An entry here is a documented narrowing; a status that is neither accepted nor
// listed here is an oversight, which is exactly what this file catches.
//
// The rationale is the one cli/debt_add.go already gives for wontfix: add
// collects no --reason, and for every status below the reason IS the payload —
// a permanent dismissal's rationale, or the investigation trail behind a
// determination. Filing a finding and closing one are also not the same act.
var debtAddExcluded = map[string]string{
	localdebt.StatusWontfix:           "permanent suppression needs a recorded --reason, which add does not collect",
	localdebt.StatusUnreproducible:    "the investigation trail is the --reason, which add does not collect",
	localdebt.StatusAttemptsExhausted: "the attempt trail is the --reason, which add does not collect",
}

// resolveExcluded records, per status VALUE, why `debt resolve` does not accept
// it as a mark action.
var resolveExcluded = map[string]string{
	localdebt.StatusDeferred: "deferral is written by other paths, not by an explicit resolve",
}

// listExcluded records, per status VALUE, why `debt list --status` does not
// accept it. It is empty on purpose: every status a record can carry must stay
// viewable, or a closed item becomes invisible rather than closed.
var listExcluded = map[string]string{}

// TestDebtVocabularies_ExhaustiveOverStatusConstants is the AC 01-03 guard: each
// of the three hand-written CLI vocabularies must account for every
// localdebt.Status* constant — by accepting it, or by naming it in the matching
// documented-exclusion map above.
func TestDebtVocabularies_ExhaustiveOverStatusConstants(t *testing.T) {
	constants := localdebtStatusConstants(t)
	require.NotEmpty(t, constants, "the source scan must find the Status* constants; an empty result means the scan broke, not that the enum is empty")

	// Sanity-pin the two Story 01 additions so a broken scan cannot make this
	// whole file pass vacuously.
	assert.Equal(t, "unreproducible", constants["StatusUnreproducible"])
	assert.Equal(t, "attempts-exhausted", constants["StatusAttemptsExhausted"])

	for _, tc := range []struct {
		name     string
		vocab    map[string]bool
		excluded map[string]string
	}{
		{"debtAddStatuses", debtAddStatuses, debtAddExcluded},
		{"resolveStatuses", resolveStatuses, resolveExcluded},
		{"debtListStatuses", debtListStatuses, listExcluded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for constName, value := range constants {
				if tc.vocab[value] {
					continue
				}
				reason, documented := tc.excluded[value]
				assert.True(t, documented,
					"%s accounts for neither %s (%q) nor documents why it is excluded", tc.name, constName, value)
				if documented {
					assert.NotEmpty(t, strings.TrimSpace(reason),
						"%s's exclusion of %s must carry a rationale, not an empty string", tc.name, constName)
				}
			}
		})
	}
}

// TestDebtVocabularies_ExcludedEntriesAreRealStatuses is the inverse guard: an
// exclusion map must not accumulate stale entries for statuses that no longer
// exist, which would let a vocabulary silently under-cover while this file still
// reported green.
func TestDebtVocabularies_ExcludedEntriesAreRealStatuses(t *testing.T) {
	constants := localdebtStatusConstants(t)
	values := map[string]bool{}
	for _, v := range constants {
		values[v] = true
	}

	for name, excluded := range map[string]map[string]string{
		"debtAddExcluded": debtAddExcluded,
		"resolveExcluded": resolveExcluded,
		"listExcluded":    listExcluded,
	} {
		for value := range excluded {
			assert.True(t, values[value],
				"%s documents an exclusion for %q, which is not a localdebt.Status* constant", name, value)
		}
	}
}

// TestDebtListStatuses_CoversEveryPresentationBucket ties the list vocabulary to
// what debtStatusBucket can actually render. A status that buckets to a value
// `debt list --status` refuses is filterable-in-theory only: cli/debt.go:461
// compares the filter against the BUCKET, so the two sets must agree or a
// visible item becomes unreachable by filter.
func TestDebtListStatuses_CoversEveryPresentationBucket(t *testing.T) {
	for constName, value := range localdebtStatusConstants(t) {
		bucket := debtStatusBucket(value)
		assert.True(t, debtListStatuses[bucket],
			"%s (%q) renders as bucket %q, which `debt list --status` does not accept", constName, value, bucket)
	}
	assert.True(t, debtListStatuses["open"], "the open bucket must stay filterable")
}
