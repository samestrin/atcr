package payload

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// gendeclHeaderName is what lets a const/var/type-only change contribute a
// symbol at all: a gendecl node carries no Name and no named children, so the
// declared identifier survives only in the header text FileSkeleton sliced.
//
// Its happy path is covered end to end by
// TestExtractChangedSymbols_ResolvesASingleGendeclByItsHeader. All three of its
// REJECTION arms shipped uncovered - blocks (430,432), (435,436) and (438,440).
// The last of those is the one that matters most: it is the gate that stops a
// name parsed out of repository source flowing unvalidated into a `git grep`
// argv, which the commit that added it (fcade85f) states outright. A security
// filter with no test is the shape that quietly stops filtering.
//
// The accepted cases are included so the table is not one-sided: a function
// returning false unconditionally would satisfy every rejection below.
func TestGendeclHeaderName(t *testing.T) {
	cases := []struct {
		name     string
		header   string
		wantName string
		wantOK   bool
		why      string
	}{
		{
			name:   "empty header",
			header: "",
			why:    "no fields at all — the fewer-than-two arm, reached before the keyword switch",
		},
		{
			name:   "keyword with no name",
			header: "const",
			why:    "a bare keyword has one field; there is no identifier to attribute the change to",
		},
		{
			name:   "grouped opener carries no single name",
			header: "const (",
			why:    "FileSkeleton drops these already, so the lookup must also refuse the shape rather than grep for '('",
		},
		{
			name:   "import is not a declaration this resolves",
			header: `import "fmt"`,
			why:    "the keyword switch's default arm — only const/var/type name a symbol worth retrieving",
		},
		{
			name:   "func is handled by the named-node path, not here",
			header: "func Alpha(p string) error {",
			why:    "the parser emits named func nodes, so reaching this resolver with one means the caller mis-routed",
		},
		{
			name:   "single-character name is not worth a grep slot",
			header: "const X = 1",
			why:    "validGrepSymbol rejects a one-character name — it matches too much to be worth a lookup",
		},
		{
			name:   "leading digit is a literal, not a symbol",
			header: "var 1abc = 2",
			why:    "validGrepSymbol's digit rule; a numeric token would spend a grep pattern on noise",
		},
		{
			name:   "argv-unsafe name is refused",
			header: "const $(rm -rf /) = 1",
			why:    "THE argv boundary: this name would otherwise be handed to a git grep subprocess",
		},
		{
			name:     "const with a real name resolves",
			header:   "const MaxThing = 1",
			wantName: "MaxThing",
			wantOK:   true,
			why:      "the accepted case — without it every rejection above is satisfied by a constant false",
		},
		{
			name:     "var with a real name resolves",
			header:   "var Registry = map[string]int{}",
			wantName: "Registry",
			wantOK:   true,
			why:      "var is a sibling keyword of const and must behave identically",
		},
		{
			name:     "type with a real name resolves",
			header:   "type Widget struct {",
			wantName: "Widget",
			wantOK:   true,
			why:      "type is the third accepted keyword; a trailing brace must not reach the name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := gendeclHeaderName(tc.header)
			require.Equal(t, tc.wantOK, ok, tc.why)
			require.Equal(t, tc.wantName, got,
				"a refused header must yield the empty name, never a partial one that could still reach a grep argv")
		})
	}
}
