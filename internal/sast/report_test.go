package sast

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReport_WriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Report{Scanner: "semgrep", Findings: []Finding{
		{File: "app/db.go", Line: 12, Rule: "sqli", Message: "String-formatted SQL query"},
		{File: "app/handler v2.go", Line: 0, Rule: "xss", Message: "file-level"},
	}}
	require.NoError(t, WriteReport(dir, want))

	got, err := ReadReport(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestReport_EmptyFindingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteReport(dir, Report{Scanner: "semgrep"}))
	got, err := ReadReport(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "semgrep", got.Scanner)
	assert.Empty(t, got.Findings)
}

func TestReadReport_MissingFileIsNil(t *testing.T) {
	got, err := ReadReport(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestWriteReport_RejectsBadScannerName(t *testing.T) {
	for _, name := range []string{"", "Semgrep", "sem grep", "a,b", "a|b", "../x"} {
		err := WriteReport(t.TempDir(), Report{Scanner: name})
		assert.Errorf(t, err, "scanner %q", name)
	}
}

// ReadReport re-checks what WriteReport guarantees: the sidecar sits in the
// review dir, outside the scan root's confinement, so a hand-edited file must
// not smuggle an escaping path or a forged reviewer name past this layer.
func TestReadReport_RejectsUnsafeContent(t *testing.T) {
	cases := map[string]string{
		"malformed":     `{"scanner":`,
		"bad scanner":   `{"scanner":"a,b","findings":[]}`,
		"dot-dot":       `{"scanner":"semgrep","findings":[{"file":"../x.go","line":1}]}`,
		"absolute":      `{"scanner":"semgrep","findings":[{"file":"/etc/passwd","line":1}]}`,
		"backslash":     `{"scanner":"semgrep","findings":[{"file":"a\\b.go","line":1}]}`,
		"empty file":    `{"scanner":"semgrep","findings":[{"file":"","line":1}]}`,
		"unclean":       `{"scanner":"semgrep","findings":[{"file":"a//b.go","line":1}]}`,
		"negative line": `{"scanner":"semgrep","findings":[{"file":"a.go","line":-1}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, ReportFile), []byte(body), 0o644))
			_, err := ReadReport(dir)
			assert.Error(t, err)
		})
	}
}

func TestWriteReport_RejectsUnsafePath(t *testing.T) {
	err := WriteReport(t.TempDir(), Report{Scanner: "semgrep", Findings: []Finding{{File: "../x.go", Line: 1}}})
	assert.Error(t, err)
}
