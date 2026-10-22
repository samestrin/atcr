package localdebt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tdFixture = `# Technical Debt

## Stats

| Severity | Open | Deferred | Resolved | Unreproducible |
|----------|------|----------|----------|----------------|
| [ ] | 1 | 0 | 0 | 0 |

### [2026-10-22] From: some-branch

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |
|-------|---|----------|------|---------|-----|----------|-------------|--------|----------|
| 1 | [x] | LOW | a.go:1 | fixed thing debt:00000000000000aa | fix | c | 5 | paperclip-builder | 0 |
| 1 | [X] | LOW | a.go:2 | fixed upper | fix | c | 5 | paperclip-builder debt:00000000000000BB | 0 |
| 1 | [-] | LOW | a.go:3 | could not repro debt:00000000000000cc | fix | c | 5 | x | 0 |
| 1 | [ ] | LOW | a.go:4 | open debt:00000000000000dd | fix | c | 5 | x | 0 |
| 1 | [/] | LOW | a.go:5 | deferred debt:00000000000000ee | fix | c | 5 | x | 0 |
| 1 | [x] | LOW | a.go:6 | no token here | fix | c | 5 | x | 0 |
| 1 | [x] | LOW | a.go:7 | token only in fix | debt:00000000000000ff | c | 5 | x | 0 |
| 1 | [x] | LOW | a.go:8 | two debt:0000000000000001 and debt:0000000000000002 | fix | c | 5 | x | 0 |
| 1 | [x] | LOW | a.go:9 | short debt:abc | fix | c | 5 | x | 0 |
| 1 | [?] | LOW | a.go:10 | odd marker debt:0000000000000003 | fix | c | 5 | x | 0 |
| 1 | [x] | LOW | a.go:11 | escaped \| pipe debt:0000000000000004 | fix | c | 5 | x | 0 |
`

func TestParseTDRows_ReadsMarkersAndTokensFromProblemAndSource(t *testing.T) {
	rows, err := ParseTDRows(strings.NewReader(tdFixture))
	require.NoError(t, err)

	// The Stats table has a checkbox cell but no Problem column: it is not an item
	// table, and its row must not surface as an untokened item.
	require.Len(t, rows, 11)
	for _, r := range rows {
		assert.Equal(t, "### [2026-10-22] From: some-branch", r.Section)
	}

	byLine := map[int]TDRow{}
	for _, r := range rows {
		byLine[r.Line] = r
	}
	assert.Equal(t, "[x]", byLine[13].Marker)
	assert.Equal(t, []string{"00000000000000aa"}, byLine[13].IDs)
	assert.Equal(t, []string{"00000000000000bb"}, byLine[14].IDs, "Source cell is read; hex is lowercased")
	assert.Equal(t, []string{"00000000000000cc"}, byLine[15].IDs)
	assert.Equal(t, "[ ]", byLine[16].Marker)
	assert.Equal(t, "[/]", byLine[17].Marker)
	assert.Empty(t, byLine[18].IDs)
	assert.Empty(t, byLine[19].IDs, "a token outside the Problem and Source cells is not read")
	assert.Equal(t, []string{"0000000000000001", "0000000000000002"}, byLine[20].IDs)
	assert.Empty(t, byLine[21].IDs)
	assert.Equal(t, []string{"debt:abc"}, byLine[21].BadTokens)
	assert.Equal(t, "[?]", byLine[22].Marker)
	assert.Equal(t, []string{"0000000000000004"}, byLine[23].IDs, "an escaped pipe does not split the cell")
	assert.Contains(t, byLine[23].Problem, `escaped \| pipe`)
}

func TestParseTDRows_HeaderlessTableUsesTheCanonicalLayout(t *testing.T) {
	in := "| 1 | [x] | LOW | a.go:1 | p debt:00000000000000aa | f | c | 5 | src debt:00000000000000bb | 0 |\n"
	rows, err := ParseTDRows(strings.NewReader(in))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, []string{"00000000000000aa", "00000000000000bb"}, rows[0].IDs)
}

func TestParseTDRows_HeaderWithExtraColumnsIsReadByName(t *testing.T) {
	in := "| Group | | Severity | File | Problem | Fix | Category | Est | Source | Model | Attempts |\n" +
		"|---|---|---|---|---|---|---|---|---|---|---|\n" +
		"| 1 | [x] | LOW | a.go:1 | p | f | c | 5 | code-review | debt:00000000000000aa | 0 |\n" +
		"| 1 | [x] | LOW | a.go:1 | p | f | c | 5 | debt:00000000000000bb | m | 0 |\n"
	rows, err := ParseTDRows(strings.NewReader(in))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Empty(t, rows[0].IDs, "the Model column is not the Source column")
	assert.Equal(t, []string{"00000000000000bb"}, rows[1].IDs)
}

func TestTDMarkerStatus_FollowsTheTDLegend(t *testing.T) {
	cases := []struct {
		marker   string
		status   string
		terminal bool
		known    bool
	}{
		{"[x]", StatusResolved, true, true},
		{"[X]", StatusResolved, true, true},
		{"[-]", StatusUnreproducible, true, true},
		{"[ ]", "", false, true},
		{"[/]", StatusDeferred, false, true},
		{"[?]", "", false, false},
	}
	for _, c := range cases {
		status, terminal, known := TDMarkerStatus(c.marker)
		assert.Equal(t, c.status, status, c.marker)
		assert.Equal(t, c.terminal, terminal, c.marker)
		assert.Equal(t, c.known, known, c.marker)
	}
}
