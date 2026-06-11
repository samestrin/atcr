package stream

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_ValidFindings(t *testing.T) {
	data := `# atcr-findings/v1
CRITICAL|src/auth.go:42|Token never expires|Check expiry|security|15|expiresAt unread|greta
HIGH|cmd/main.go:88|Goroutine leak|Add WaitGroup|concurrency|30|no wg.Wait|kai
`
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)
	assert.Empty(t, res.Skipped)

	f := res.Findings[0]
	assert.Equal(t, "CRITICAL", f.Severity)
	assert.Equal(t, "src/auth.go", f.File)
	assert.Equal(t, 42, f.Line)
	assert.Equal(t, "Token never expires", f.Problem)
	assert.Equal(t, 15, f.EstMinutes)
	assert.Equal(t, "greta", f.Reviewer)
}

func TestParser_SkipsProseAndComments(t *testing.T) {
	data := `# atcr-findings/v1
# this is a comment
This line mentions HIGH severity but is prose, not a finding.
LOW|a.go:1|Minor|Fix it|style|5|evidence|bruce

MEDIUM|b.go:2|Thing|Do|correctness|10|because|dax
`
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)
	assert.Equal(t, "LOW", res.Findings[0].Severity)
	assert.Equal(t, "MEDIUM", res.Findings[1].Severity)
}

func TestParser_ShortRowPadded(t *testing.T) {
	// 6 columns: missing EVIDENCE and REVIEWER — padded to 8.
	data := "# atcr-findings/v1\nLOW|a.go:1|Problem|Fix|style|5\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Empty(t, res.Skipped)
	assert.Equal(t, "", res.Findings[0].Evidence)
	assert.Equal(t, "", res.Findings[0].Reviewer)
}

func TestParser_TooManyColumnsSkipped(t *testing.T) {
	// 9 columns in an 8-column file: an unescaped pipe leaked a column.
	data := "# atcr-findings/v1\nHIGH|a.go:1|Problem|Fix|style|5|ev|bruce|extra\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	assert.Empty(t, res.Findings)
	require.Len(t, res.Skipped, 1)
	assert.Contains(t, res.Skipped[0].Reason, "expected 8 columns, got 9")
}

func TestParser_MissingHeader(t *testing.T) {
	data := "CRITICAL|a.go:1|p|f|c|5|e|bruce\n"
	_, err := ParseSource([]byte(data))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMissingHeader)
}

func TestParser_UnknownVersion(t *testing.T) {
	data := "# atcr-findings/v2\nCRITICAL|a.go:1|p|f|c|5|e|bruce\n"
	_, err := ParseSource([]byte(data))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownVersion)
}

func TestParser_EmptyFindings(t *testing.T) {
	data := "# atcr-findings/v1\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	assert.Empty(t, res.Findings)
	assert.Empty(t, res.Skipped)
}

func TestParser_FileLevelLineZero(t *testing.T) {
	data := "# atcr-findings/v1\nLOW|path/to/file.go:0|File-level|Fix|doc|5|ev|otto\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, "path/to/file.go", res.Findings[0].File)
	assert.Equal(t, 0, res.Findings[0].Line)
}

func TestParser_Reconciled(t *testing.T) {
	data := "# atcr-findings/v1\nHIGH|a.go:1|p|f|security|10|ev|greta,kai|HIGH\n"
	res, err := ParseReconciled([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, []string{"greta", "kai"}, res.Findings[0].Reviewers)
	assert.Equal(t, "HIGH", res.Findings[0].Confidence)
}

func TestParser_CRLF(t *testing.T) {
	data := "# atcr-findings/v1\r\nLOW|a.go:1|p|f|style|5|ev|bruce\r\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, "bruce", res.Findings[0].Reviewer)
}
