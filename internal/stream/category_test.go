package stream

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reclib "github.com/samestrin/atcr/reconcile"
)

func TestCanonicalCategory(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		// Members pass through.
		{"correctness", "correctness"},
		{"error-handling", "error-handling"},
		{"docs", "docs"},
		{"other", "other"},
		// `logic` is a member: canonicalization is not synonym folding, so the
		// CategoryMerges logic->correctness fold is NOT applied.
		{"logic", "logic"},
		// Case and surrounding whitespace.
		{"Concurrency", "concurrency"},
		{"  SECURITY\t", "security"},
		// Singular/plural against a member.
		{"races", "race"},
		{"Dependencies", "dependency"},
		{"doc", "docs"},
		{"leaks", "leak"},
		// The taxonomy's one recorded singular/plural pair (reconcile/category.go
		// "Trivially identical variants"): resource and resources fold to the
		// same member, so both spellings group together.
		{"Resources ", reclib.CategoryResourceLeak},
		{"resource", reclib.CategoryResourceLeak},
		// Unrecognized words route to other — including CategoryMerges synonyms,
		// which are meaning folds, not spelling variants.
		{"frobnicate", "other"},
		{"bug", "other"},
		{"clarity", "other"},
		{"error handling", "other"},
		{"error_handling", "other"},
		{"42", "other"},
		// out-of-scope is reserved: byte-identical in, and never a target.
		{"out-of-scope", "out-of-scope"},
		{"out of scope", "other"},
		{"Out-Of-Scope", "other"},
		{" out-of-scope", "other"},
		{"out-of-scope ", "other"},
		{"out-of-scopes", "other"},
		{"OUT-OF-SCOPE", "other"},
		// No word, no category.
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, CanonicalCategory(c.raw), "raw %q", c.raw)
	}
}

// Every canonical value is a taxonomy member (or empty for no word), so a
// consumer grouping by it never meets an off-vocabulary key.
func TestCanonicalCategory_AlwaysAMember(t *testing.T) {
	members := map[string]bool{"": true}
	for _, c := range reclib.Categories() {
		members[c] = true
	}
	inputs := []string{"Resources ", "frobnicate", "out of scope", "out-of-scope", "RACES", "", "x|y"}
	for _, m := range reclib.Categories() {
		inputs = append(inputs, m, strings.ToUpper(m), m+"s", " "+m+" ")
	}
	for word := range reclib.CategoryMerges() {
		inputs = append(inputs, word)
	}
	for _, in := range inputs {
		assert.True(t, members[CanonicalCategory(in)], "raw %q canonicalized to non-member %q", in, CanonicalCategory(in))
	}
}

// The only route into out-of-scope is the reserved token itself, byte for byte.
func TestCanonicalCategory_OutOfScopeOnlyFromItself(t *testing.T) {
	inputs := []string{"out of scope", "Out-Of-Scope", "out-of-scope\n", "outofscope", "out_of_scope", "out-of-scopes", "scope"}
	for word := range reclib.CategoryMerges() {
		inputs = append(inputs, word)
	}
	for _, m := range reclib.Categories() {
		if m != reclib.CategoryOutOfScope {
			inputs = append(inputs, m)
		}
	}
	for _, in := range inputs {
		assert.NotEqual(t, reclib.CategoryOutOfScope, CanonicalCategory(in), "raw %q", in)
	}
	assert.Equal(t, reclib.CategoryOutOfScope, CanonicalCategory(reclib.CategoryOutOfScope))
}

// The task's examples, end to end through every parse path that builds a
// Finding: the raw Category is untouched and CanonicalCategory sits beside it.
func TestParse_CarriesRawAndCanonicalCategory(t *testing.T) {
	type pair struct{ raw, canon string }
	want := []pair{
		{"Resources ", reclib.CategoryResourceLeak},
		{"frobnicate", "other"},
		{"out of scope", "other"},
		{"out-of-scope", "out-of-scope"},
	}
	check := func(t *testing.T, got []Finding) {
		t.Helper()
		require.Len(t, got, len(want))
		for i, w := range want {
			assert.Equal(t, w.raw, got[i].Category, "raw category must be byte-identical")
			assert.Equal(t, w.canon, got[i].CanonicalCategory)
		}
	}

	t.Run("v1 per-source", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(Version + "\n")
		for _, w := range want {
			b.WriteString("HIGH|a.go:1|p|f|" + w.raw + "|5|e|bruce\n")
		}
		res, err := ParseSource([]byte(b.String()))
		require.NoError(t, err)
		check(t, res.Findings)
	})

	t.Run("v1 reconciled", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(Version + "\n")
		for _, w := range want {
			b.WriteString("HIGH|a.go:1|p|f|" + w.raw + "|5|e|bruce,kai|high\n")
		}
		res, err := ParseReconciled([]byte(b.String()))
		require.NoError(t, err)
		check(t, res.Findings)
	})

	t.Run("model output pipe rows", func(t *testing.T) {
		var b strings.Builder
		for _, w := range want {
			b.WriteString("HIGH|a.go:1|p|f|" + w.raw + "|5|e\n")
		}
		check(t, ParseModelOutput([]byte(b.String())))
	})

	t.Run("model output JSON", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("[")
		for i, w := range want {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"severity":"HIGH","file_line":"a.go:1","problem":"p","fix":"f","category":"` + w.raw + `","est_minutes":5,"evidence":"e"}`)
		}
		b.WriteString("]")
		check(t, ParseModelOutput([]byte(b.String())))
	})

	t.Run("v2 TOON", func(t *testing.T) {
		in := make([]Finding, len(want))
		for i, w := range want {
			in[i] = Finding{Severity: "HIGH", File: "a.go", Line: 1, Category: w.raw, Reviewer: "bruce"}
		}
		var b strings.Builder
		require.NoError(t, WriteSourceV2(&b, in))
		res, err := ParseSource([]byte(b.String()))
		require.NoError(t, err)
		check(t, res.Findings)
	})

	t.Run("v2 JSON envelope", func(t *testing.T) {
		rows := make([]lossyRow, len(want))
		for i, w := range want {
			rows[i] = lossyRow{Severity: "HIGH", FileLine: "a.go:1", Category: w.raw, EstMinutes: 5, Reviewer: "bruce", Extra: textish{"t"}}
		}
		var b strings.Builder
		require.NoError(t, encodeV2(&b, lossyPayload{Findings: rows}))
		require.Contains(t, b.String(), envelopePrefix)
		res, err := ParseSource([]byte(b.String()))
		require.NoError(t, err)
		check(t, res.Findings)
	})
}
