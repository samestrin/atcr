package sast

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSemgrepScript stands in for the semgrep binary. It records its argv and
// working directory, then behaves per FAKE_SEMGREP_MODE: print the fixture
// SARIF (default), exit non-zero, print non-SARIF, hang past the timeout, or
// hang in a forked child that keeps stdout open after the shell is killed.
const fakeSemgrepScript = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_SEMGREP_DIR/args"
pwd > "$FAKE_SEMGREP_DIR/cwd"
case "$FAKE_SEMGREP_MODE" in
  fail) echo "app/db.go: secret-looking stderr" >&2; exit 2 ;;
  badjson) echo "not sarif"; exit 0 ;;
  hang) exec sleep 30 ;;
  hangchild) sleep 30 & wait ;;
esac
cat "$FAKE_SEMGREP_SARIF"
`

// installFakeSemgrep puts a fake semgrep first on PATH and returns the
// directory it records into. /bin and /usr/bin stay on PATH for sh, cat,
// pwd and sleep; the fake still shadows any real semgrep.
func installFakeSemgrep(t *testing.T, mode string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake semgrep is a POSIX shell script")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "semgrep"), []byte(fakeSemgrepScript), 0o755))
	sarif, err := filepath.Abs(filepath.Join("testdata", "semgrep.sarif"))
	require.NoError(t, err)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin"+string(os.PathListSeparator)+"/usr/bin")
	t.Setenv("FAKE_SEMGREP_DIR", dir)
	t.Setenv("FAKE_SEMGREP_MODE", mode)
	t.Setenv("FAKE_SEMGREP_SARIF", sarif)
	return dir
}

// shrinkSemgrepTimeout keeps the hang cases fast.
func shrinkSemgrepTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := semgrepTimeout
	semgrepTimeout = d
	t.Cleanup(func() { semgrepTimeout = prev })
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func TestSemgrep_ImplementsRunner(t *testing.T) {
	var r Runner = Semgrep{}
	assert.Equal(t, "semgrep", r.Name())
}

func TestSemgrep_FakeBinaryYieldsFixtureFindings(t *testing.T) {
	dir := installFakeSemgrep(t, "")
	root := fixtureRoot(t)

	findings, err := Semgrep{}.Run(context.Background(), root, []string{"app/db.go", "app/handler v2.go"})
	require.NoError(t, err)

	// The fixture's escaping results are dropped by the SARIF reader.
	assert.Equal(t, []Finding{
		{File: "app/db.go", Line: 12, Rule: "go.lang.security.audit.sqli.string-formatted-query", Message: "String-formatted SQL query"},
		{File: "app/handler v2.go", Line: 40, Rule: "go.lang.security.audit.xss.no-direct-write", Message: "Direct write to ResponseWriter"},
	}, findings)

	args := readLines(t, filepath.Join(dir, "args"))
	assert.Contains(t, args, "--sarif")
	assert.Contains(t, args, "--metrics=off")
	assert.Contains(t, args, DefaultSemgrepConfig)
	// Targets follow "--" so a file named like a flag is never parsed as one.
	require.GreaterOrEqual(t, len(args), 3)
	assert.Equal(t, []string{"--", "app/db.go", "app/handler v2.go"}, args[len(args)-3:])

	realRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	assert.Equal(t, []string{realRoot}, readLines(t, filepath.Join(dir, "cwd")))
}

func TestSemgrep_NoFilesScansWholeRoot(t *testing.T) {
	dir := installFakeSemgrep(t, "")
	_, err := Semgrep{}.Run(context.Background(), fixtureRoot(t), nil)
	require.NoError(t, err)
	args := readLines(t, filepath.Join(dir, "args"))
	assert.Equal(t, []string{"--", "."}, args[len(args)-2:])
}

func TestSemgrep_CustomConfig(t *testing.T) {
	dir := installFakeSemgrep(t, "")
	_, err := Semgrep{Config: "rules/local.yml"}.Run(context.Background(), fixtureRoot(t), nil)
	require.NoError(t, err)
	args := readLines(t, filepath.Join(dir, "args"))
	assert.Contains(t, args, "rules/local.yml")
	assert.NotContains(t, args, DefaultSemgrepConfig)
}

// assertUnavailable checks err is the typed unavailable error, names semgrep,
// and carries no scanner stderr.
func assertUnavailable(t *testing.T, err error) *UnavailableError {
	t.Helper()
	require.Error(t, err)
	var ue *UnavailableError
	require.True(t, errors.As(err, &ue), "err = %v, want *UnavailableError", err)
	assert.True(t, errors.Is(err, ErrUnavailable))
	assert.Equal(t, "semgrep", ue.Scanner)
	assert.NotEmpty(t, ue.Reason)
	assert.NotContains(t, err.Error(), "secret-looking")
	return ue
}

func TestSemgrep_MissingBinaryIsUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	shrinkSemgrepTimeout(t, 300*time.Millisecond)

	start := time.Now()
	findings, err := Semgrep{}.Run(context.Background(), fixtureRoot(t), nil)
	elapsed := time.Since(start)

	assert.Nil(t, findings)
	ue := assertUnavailable(t, err)
	assert.Contains(t, ue.Reason, "not found")
	assert.Less(t, elapsed, 300*time.Millisecond, "a missing binary must not wait out the timeout")
}

func TestSemgrep_FailuresAreUnavailable(t *testing.T) {
	cases := []struct {
		name   string
		mode   string
		script string
		reason string
	}{
		{name: "non-zero exit", mode: "fail", reason: "status 2"},
		{name: "output is not SARIF", mode: "badjson", reason: "SARIF"},
		{name: "hung binary", mode: "hang", reason: "timed out"},
		{name: "hung child holding stdout", mode: "hangchild", reason: "timed out"},
		// Run fails without an ExitError: the kernel cannot start it.
		{name: "cannot start", script: "#!/nonexistent/interpreter\n", reason: "could not run"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := installFakeSemgrep(t, tc.mode)
			if tc.script != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "semgrep"), []byte(tc.script), 0o755))
			}
			const timeout = 300 * time.Millisecond
			shrinkSemgrepTimeout(t, timeout)

			start := time.Now()
			findings, err := Semgrep{}.Run(context.Background(), fixtureRoot(t), nil)
			elapsed := time.Since(start)

			assert.Nil(t, findings)
			ue := assertUnavailable(t, err)
			assert.Contains(t, ue.Reason, tc.reason)
			// The timeout bounds the run; WaitDelay bounds the pipe drain
			// after the kill. Generous slack for a loaded CI host.
			assert.Less(t, elapsed, timeout+3*time.Second, "the fixed timeout did not bound the run")
		})
	}
}

func TestSemgrep_CallerCancelIsNotUnavailable(t *testing.T) {
	installFakeSemgrep(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Semgrep{}.Run(ctx, fixtureRoot(t), nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, errors.Is(err, ErrUnavailable), "a cancelled review is not a scanner outage")
}

func TestSemgrep_BadRootIsAnError(t *testing.T) {
	installFakeSemgrep(t, "")
	_, err := Semgrep{}.Run(context.Background(), filepath.Join(t.TempDir(), "missing"), nil)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrUnavailable), "a bad scan root is a caller bug, not a scanner outage")
}

func TestUnavailableError_Message(t *testing.T) {
	err := &UnavailableError{Scanner: "semgrep", Reason: "timed out after 2m0s"}
	assert.Equal(t, "sast: semgrep unavailable: timed out after 2m0s", err.Error())
}
