package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- fixtures -------------------------------------------------------------

// smellHardDiff deletes a test file outright while editing an implementation
// file: the canonical HARD reward hack.
const smellHardDiff = `diff --git a/foo_test.go b/foo_test.go
--- a/foo_test.go
+++ /dev/null
@@ -1,3 +0,0 @@
-func TestFoo(t *testing.T) {
-	require.Equal(t, 1, Foo())
-}
diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,1 +1,1 @@
-func Foo() int { return 0 }
+func Foo() int { return 1 }
`

// smellSoftDiff adds a lint suppression: SOFT only, no HARD.
const smellSoftDiff = `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,1 +1,2 @@
 func Foo() int {
+	//nolint:errcheck
`

// smellCleanDiff is an ordinary implementation edit.
const smellCleanDiff = `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,1 +1,1 @@
-func Foo() int { return 0 }
+func Foo() int { return 1 }
`

// runSmell executes `atcr verify diff <args...>` with stdin bound to in,
// returning the exit code and stdout/stderr separately. Split streams matter:
// --json must put the payload on stdout with nothing else mixed in.
func runSmell(t *testing.T, in string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(append([]string{"verify", "diff"}, args...))
	root.SetIn(strings.NewReader(in))
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	err := root.ExecuteContext(context.Background())
	if err != nil {
		errBuf.WriteString(err.Error())
	}
	return exitCode(err), outBuf.String(), errBuf.String()
}

// gitSmell runs git with the same hardened, identity-pinned environment
// initGitRepo uses, but EXPLICITLY scoped with `-C <cwd>` rather than inheriting
// the ambient working directory — the rule internal/gitfixtures_test.go enforces
// (its allowlist of ambient-cwd test files is a freeze to shrink, not to join).
//
// The temp-dir precondition is still asserted, since the resolved cwd is what
// gets passed to -C: without it a stray cwd would simply be scoped-to instead of
// inherited, which is no safer (see initGitRepo's incident note). The
// not-already-a-repo half is deliberately not asserted — these calls run INSIDE
// the repo initGitRepo just created.
func gitSmell(t *testing.T, args ...string) {
	t.Helper()
	wd := requireTempWorkdir(t)
	cmd := exec.Command("git", append([]string{"-C", wd}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// stageTestDeletion builds a repo whose STAGED change deletes a committed test
// file while editing an implementation file — the HARD case, reached through
// real git rather than a literal diff string.
func stageTestDeletion(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile("foo.go", []byte("package p\n\nfunc Foo() int { return 0 }\n"), 0o644))
	require.NoError(t, os.WriteFile("foo_test.go", []byte("package p\n\nfunc TestFoo(t *testing.T) { _ = Foo() }\n"), 0o644))
	gitSmell(t, "add", "foo.go", "foo_test.go")
	gitSmell(t, "commit", "-q", "-m", "add foo")

	gitSmell(t, "rm", "-q", "foo_test.go")
	require.NoError(t, os.WriteFile("foo.go", []byte("package p\n\nfunc Foo() int { return 1 }\n"), 0o644))
	gitSmell(t, "add", "foo.go")
}

// --- verdict reporting ----------------------------------------------------

func TestVerifyDiffCmd_CleanDiffReportsClean(t *testing.T) {
	code, stdout, _ := runSmell(t, smellCleanDiff)
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "clean")
}

func TestVerifyDiffCmd_HardDiffReportsHardAndNamesSmell(t *testing.T) {
	code, stdout, _ := runSmell(t, smellHardDiff)
	// No --fail-on, so reporting a HARD verdict must NOT change the exit code:
	// gating is opt-in, matching `atcr review --fail-on`.
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "hard")
	require.Contains(t, stdout, "test_deleted")
	require.Contains(t, stdout, "foo_test.go")
}

func TestVerifyDiffCmd_SoftDiffReportsSoftOnly(t *testing.T) {
	code, stdout, _ := runSmell(t, smellSoftDiff)
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "soft_only")
	require.Contains(t, stdout, "suppression")
}

// Nothing staged / nothing piped is legitimately clean, not an input error:
// `atcr verify diff --staged` on a clean tree must succeed.
func TestVerifyDiffCmd_EmptyInputIsClean(t *testing.T) {
	code, stdout, _ := runSmell(t, "")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "clean")
}

// --- JSON contract --------------------------------------------------------

func TestVerifyDiffCmd_JSONPayloadOnlyOnStdout(t *testing.T) {
	code, stdout, _ := runSmell(t, smellHardDiff, "--json")
	require.Equal(t, 0, code)

	var got struct {
		Files struct {
			Test []string `json:"test"`
			Impl []string `json:"impl"`
		} `json:"files"`
		Smells []struct {
			Type     string `json:"type"`
			Severity string `json:"severity"`
			File     string `json:"file"`
			Evidence string `json:"evidence"`
		} `json:"smells"`
		Summary struct {
			TestFiles int            `json:"test_files"`
			ImplFiles int            `json:"impl_files"`
			Hard      int            `json:"hard"`
			Soft      int            `json:"soft"`
			ByType    map[string]int `json:"by_type"`
			Verdict   string         `json:"verdict"`
		} `json:"summary"`
	}
	// Unmarshalling the WHOLE buffer proves stdout carries the payload and
	// nothing else — a stray human line would make this fail.
	require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout must be JSON only, got: %q", stdout)

	require.Equal(t, "hard", got.Summary.Verdict)
	require.Positive(t, got.Summary.Hard)
	require.Contains(t, got.Summary.ByType, "test_deleted")
	require.Contains(t, got.Files.Test, "foo_test.go")
	require.Contains(t, got.Files.Impl, "foo.go")
	require.NotEmpty(t, got.Smells)
}

func TestVerifyDiffCmd_JSONCleanHasEmptyArraysNotNull(t *testing.T) {
	code, stdout, _ := runSmell(t, smellCleanDiff, "--json")
	require.Equal(t, 0, code)
	require.NotContains(t, stdout, "null")

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &m))
	require.Equal(t, "clean", m["summary"].(map[string]any)["verdict"])
}

// --- gate -----------------------------------------------------------------

func TestVerifyDiffCmd_FailOnGate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		diff   string
		failOn []string
		want   int
	}{
		{"hard diff, no gate", smellHardDiff, nil, 0},
		{"hard diff, gate hard", smellHardDiff, []string{"--fail-on", "hard"}, 1},
		{"hard diff, gate soft", smellHardDiff, []string{"--fail-on", "soft"}, 1},
		{"soft diff, gate hard", smellSoftDiff, []string{"--fail-on", "hard"}, 0},
		{"soft diff, gate soft", smellSoftDiff, []string{"--fail-on", "soft"}, 1},
		{"clean diff, gate soft", smellCleanDiff, []string{"--fail-on", "soft"}, 0},
		{"clean diff, gate hard", smellCleanDiff, []string{"--fail-on", "hard"}, 0},
		// `none` is an explicit synonym for unset, so a scripted consumer can
		// always pass the flag and vary only its value.
		{"hard diff, gate none", smellHardDiff, []string{"--fail-on", "none"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := runSmell(t, tc.diff, tc.failOn...)
			require.Equal(t, tc.want, code)
		})
	}
}

// A tripped gate still prints the report — the consumer must be able to read
// WHY it failed from the same invocation, not have to re-run without the gate.
func TestVerifyDiffCmd_GateFailureStillReports(t *testing.T) {
	code, stdout, stderr := runSmell(t, smellHardDiff, "--fail-on", "hard")
	require.Equal(t, 1, code)
	require.Contains(t, stdout, "test_deleted")
	require.Contains(t, stderr, "hard")
}

func TestVerifyDiffCmd_GateFailureUnderJSONKeepsStdoutParseable(t *testing.T) {
	code, stdout, _ := runSmell(t, smellHardDiff, "--json", "--fail-on", "hard")
	require.Equal(t, 1, code)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &m), "stdout must stay JSON-only when the gate trips")
}

func TestVerifyDiffCmd_InvalidFailOnIsUsageError(t *testing.T) {
	for _, v := range []string{"bogus", "HIGH", "critical", "  "} {
		code, _, _ := runSmell(t, smellCleanDiff, "--fail-on", v)
		require.Equal(t, 2, code, "--fail-on %q must be a usage error", v)
	}
}

// --- input sources --------------------------------------------------------

func TestVerifyDiffCmd_ReadsFileArgument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "patch.diff")
	require.NoError(t, os.WriteFile(path, []byte(smellHardDiff), 0o644))

	code, stdout, _ := runSmell(t, "", path)
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "test_deleted")
}

// "-" is the conventional explicit stdin token; it must not be read as a path.
func TestVerifyDiffCmd_DashReadsStdin(t *testing.T) {
	code, stdout, _ := runSmell(t, smellHardDiff, "-")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "test_deleted")
}

func TestVerifyDiffCmd_MissingFileIsUsageError(t *testing.T) {
	code, _, _ := runSmell(t, "", filepath.Join(t.TempDir(), "nope.diff"))
	require.Equal(t, 2, code)
}

func TestVerifyDiffCmd_StagedScansIndex(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	stageTestDeletion(t)

	code, stdout, _ := runSmell(t, "", "--staged")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "hard")
	require.Contains(t, stdout, "test_deleted")
	require.Contains(t, stdout, "foo_test.go")
}

func TestVerifyDiffCmd_StagedOnCleanIndexIsClean(t *testing.T) {
	isolate(t)
	initGitRepo(t)

	code, stdout, _ := runSmell(t, "", "--staged")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "clean")
}

func TestVerifyDiffCmd_RangeScansCommitRange(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	stageTestDeletion(t)
	gitSmell(t, "commit", "-q", "-m", "delete the test")

	code, stdout, _ := runSmell(t, "", "--range", "HEAD~1..HEAD")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "hard")
	require.Contains(t, stdout, "test_deleted")
}

func TestVerifyDiffCmd_RepoFlagTargetsAnotherTree(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	stageTestDeletion(t)
	repo, err := os.Getwd()
	require.NoError(t, err)

	// Run from a DIFFERENT cwd so a dropped --repo would scan the wrong tree.
	t.Chdir(t.TempDir())
	code, stdout, _ := runSmell(t, "", "--staged", "--repo", repo)
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "test_deleted")
}

func TestVerifyDiffCmd_BadRepoIsUsageError(t *testing.T) {
	code, _, _ := runSmell(t, "", "--staged", "--repo", filepath.Join(t.TempDir(), "not-a-repo"))
	require.Equal(t, 2, code)
}

// --- input-source conflicts ----------------------------------------------

func TestVerifyDiffCmd_StagedAndRangeAreMutuallyExclusive(t *testing.T) {
	code, _, _ := runSmell(t, "", "--staged", "--range", "HEAD~1..HEAD")
	require.Equal(t, 2, code)
}

func TestVerifyDiffCmd_FileArgWithGitSourceIsUsageError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "patch.diff")
	require.NoError(t, os.WriteFile(path, []byte(smellCleanDiff), 0o644))

	for _, args := range [][]string{
		{path, "--staged"},
		{path, "--range", "HEAD~1..HEAD"},
	} {
		code, _, _ := runSmell(t, "", args...)
		require.Equal(t, 2, code, "args %v must be a usage error", args)
	}
}

// --range is interpolated into git's argv. A value that starts with "-" would
// be read by git as an OPTION, not a revision range — reject it before exec.
func TestVerifyDiffCmd_RangeRejectsLeadingDash(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	for _, r := range []string{"--output=/tmp/pwned", "-p", "--exec=touch /tmp/x"} {
		code, _, _ := runSmell(t, "", "--range", r)
		require.Equal(t, 2, code, "--range %q must be rejected", r)
	}
}

func TestVerifyDiffCmd_EmptyRangeIsUsageError(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	code, _, _ := runSmell(t, "", "--range", "   ")
	require.Equal(t, 2, code)
}

// --- non-diff input -------------------------------------------------------

// Unlike the in-process fix gate (which treats free-form Finding.Fix as clean
// by construction), a caller of THIS command asserted "here is a diff". Silently
// reporting clean for input that was never scanned would hand a consumer a
// false pass, so non-diff content is a usage error instead.
func TestVerifyDiffCmd_NonDiffInputIsUsageError(t *testing.T) {
	for _, in := range []string{
		"add a nil check to Foo",
		"+ add a nil check to Foo",
		"package p\n\nfunc Foo() int { return 1 }\n",
	} {
		code, _, stderr := runSmell(t, in)
		require.Equal(t, 2, code, "non-diff input %q must be a usage error", in)
		require.Contains(t, stderr, "unified diff")
	}
}

// --- wiring ---------------------------------------------------------------

func TestVerifyDiffCmd_RegisteredUnderVerify(t *testing.T) {
	out, err := execute(t, "verify", "--help")
	require.NoError(t, err)
	require.Contains(t, out, "diff")
}

// The shadowing escape hatch documented on the AddCommand call site: `--`
// terminates cobra's child search, so the parent still receives "diff" as its
// positional id. Exit 2 here is the parent's own "no such review" path, NOT a
// flag error — reaching it at all is the proof.
func TestVerifyDiffCmd_DoubleDashReachesParentWithDiffAsID(t *testing.T) {
	isolate(t)
	code, _, stderr := runSmellParent(t, "--", "diff")
	require.Equal(t, 2, code)
	require.NotContains(t, stderr, "unified diff", "must hit `atcr verify`, not the diff scanner")
}

// runSmellParent invokes `atcr verify <args...>` — the PARENT command — so the
// subcommand-shadowing test can distinguish the two.
func runSmellParent(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(append([]string{"verify"}, args...))
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	err := root.ExecuteContext(context.Background())
	if err != nil {
		errBuf.WriteString(err.Error())
	}
	return exitCode(err), outBuf.String(), errBuf.String()
}
