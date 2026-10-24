package sast

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureRoot builds a scan root holding the files the fixture SARIF names,
// plus a symlink "link" that points at a directory outside the root.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "app"), 0o755))
	require.NoError(t, os.MkdirAll(outside, 0o755))
	for _, f := range []string{"app/db.go", "app/handler v2.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, f), []byte("package app\n"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.go"), []byte("package x\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))
	return root
}

func openFixture(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "semgrep.sarif"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestParseSARIF_FixtureFindingsAndEscapesDropped(t *testing.T) {
	root := fixtureRoot(t)

	findings, dropped, err := ParseSARIF(openFixture(t), root)
	require.NoError(t, err)

	assert.Equal(t, []Finding{
		{File: "app/db.go", Line: 12, Rule: "go.lang.security.audit.sqli.string-formatted-query", Message: "String-formatted SQL query"},
		{File: "app/handler v2.go", Line: 40, Rule: "go.lang.security.audit.xss.no-direct-write", Message: "Direct write to ResponseWriter"},
	}, findings)
	// dot-dot, encoded dot-dot, absolute, file URI, symlink, no location.
	assert.Equal(t, 6, dropped)
}

func TestParseSARIF_RejectsWrongVersion(t *testing.T) {
	_, _, err := ParseSARIF(strings.NewReader(`{"version":"2.0.0","runs":[]}`), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2.1.0")
}

func TestParseSARIF_RejectsMalformedJSON(t *testing.T) {
	_, _, err := ParseSARIF(strings.NewReader(`{"version":`), t.TempDir())
	require.Error(t, err)
}

func TestParseSARIF_RequiresRoot(t *testing.T) {
	_, _, err := ParseSARIF(strings.NewReader(`{"version":"2.1.0","runs":[]}`), "")
	require.Error(t, err)
}

func TestParseSARIF_EmptyRunsIsNoFindings(t *testing.T) {
	findings, dropped, err := ParseSARIF(strings.NewReader(`{"version":"2.1.0","runs":[{"results":[]}]}`), t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Zero(t, dropped)
}

func TestParseSARIF_RuleFallbacks(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), nil, 0o644))
	doc := `{"version":"2.1.0","runs":[{"tool":{"driver":{"rules":[{"id":"from-index"}]}},"results":[
		{"rule":{"id":"from-ref"},"message":{"text":"m1"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.go"}}}]},
		{"ruleIndex":5,"message":{"text":"m2"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"./a.go"},"region":{"startLine":2}}}]}
	]}]}`
	findings, dropped, err := ParseSARIF(strings.NewReader(doc), root)
	require.NoError(t, err)
	assert.Zero(t, dropped)
	assert.Equal(t, []Finding{
		{File: "a.go", Line: 0, Rule: "from-ref", Message: "m1"},
		{File: "a.go", Line: 2, Rule: "", Message: "m2"},
	}, findings)
}

func TestParseSARIF_DropsUnsafeURIs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), nil, 0o644))
	for _, uri := range []string{
		"",
		"https://example.com/a.go",
		`..\\a.go`,
		"a.go%00",
		"missing.go",
		"%zz",
		".",
	} {
		t.Run(uri, func(t *testing.T) {
			doc := `{"version":"2.1.0","runs":[{"results":[{"ruleId":"r","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"` + uri + `"}}}]}]}]}`
			findings, dropped, err := ParseSARIF(strings.NewReader(doc), root)
			require.NoError(t, err)
			assert.Empty(t, findings)
			assert.Equal(t, 1, dropped)
		})
	}
}

// fakeRunner proves the Runner interface is implementable by a test double,
// which is how the pipeline stage will be tested without a real scanner.
type fakeRunner struct{ out []Finding }

func (f fakeRunner) Name() string { return "fake" }
func (f fakeRunner) Run(_ context.Context, _ string, _ []string) ([]Finding, error) {
	return f.out, nil
}

func TestRunnerInterface(t *testing.T) {
	var r Runner = fakeRunner{out: []Finding{{File: "a.go", Line: 1, Rule: "r", Message: "m"}}}
	got, err := r.Run(context.Background(), t.TempDir(), nil)
	require.NoError(t, err)
	assert.Equal(t, "fake", r.Name())
	assert.Len(t, got, 1)
}

func TestParseSARIF_SymlinkedRootAndInRootLinkKept(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	require.NoError(t, os.MkdirAll(filepath.Join(real, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(real, "pkg", "a.go"), nil, 0o644))
	// An in-root link to an in-root file is safe and must be kept.
	require.NoError(t, os.Symlink(filepath.Join(real, "pkg", "a.go"), filepath.Join(real, "alias.go")))
	// The scan root itself is reached through a symlink.
	rootLink := filepath.Join(base, "root-link")
	require.NoError(t, os.Symlink(real, rootLink))

	doc := `{"version":"2.1.0","runs":[{"results":[
		{"ruleId":"r","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"pkg/a.go"},"region":{"startLine":3}}}]},
		{"ruleId":"r","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"alias.go"},"region":{"startLine":4}}}]}
	]}]}`
	findings, dropped, err := ParseSARIF(strings.NewReader(doc), rootLink)
	require.NoError(t, err)
	assert.Zero(t, dropped)
	assert.Equal(t, []Finding{
		{File: "pkg/a.go", Line: 3, Rule: "r", Message: "m"},
		{File: "alias.go", Line: 4, Rule: "r", Message: "m"},
	}, findings)
}

func TestParseSARIF_MissingRootIsError(t *testing.T) {
	_, _, err := ParseSARIF(strings.NewReader(`{"version":"2.1.0","runs":[]}`), filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}
