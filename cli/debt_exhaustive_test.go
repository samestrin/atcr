package cli

import (
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
func localdebtStatusConstants(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "internal", "localdebt")
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
					if !strings.HasPrefix(ident.Name, "Status") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
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
