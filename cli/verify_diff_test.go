package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/gitexec"
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
// stdout must stay payload-only, with the clean-input notes on stderr.
func runSmell(t *testing.T, in string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runSmellContext(context.Background(), t, in, args...)
}

// runSmellContext is runSmell with an explicit context, so a test can cancel
// the scan the way runMain's SIGINT/SIGTERM handler cancels the root context.
func runSmellContext(ctx context.Context, t *testing.T, in string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(append([]string{"verify", "diff"}, args...))
	root.SetIn(strings.NewReader(in))
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	err := root.ExecuteContext(ctx)
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
// gitSmellEnv is the hardened, identity-pinned environment every test-side git
// subprocess runs with — the same config surface gitexec pins in production
// (GIT_CONFIG_GLOBAL/SYSTEM=/dev/null), so a test asserting the CLI and a raw
// git call agree byte-for-byte never reads the runner's global git config on
// one side only.
func gitSmellEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
}

func gitSmell(t *testing.T, args ...string) {
	t.Helper()
	wd := requireTempWorkdir(t)
	cmd := exec.Command("git", append([]string{"-C", wd}, args...)...)
	cmd.Env = gitSmellEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// gitSmellOutput is gitSmell for callers that need stdout back — same -C
// scoping and hardened env.
func gitSmellOutput(t *testing.T, args ...string) string {
	t.Helper()
	wd := requireTempWorkdir(t)
	cmd := exec.Command("git", append([]string{"-C", wd}, args...)...)
	cmd.Env = gitSmellEnv()
	out, err := cmd.Output()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// stageTestDeletion builds a repo whose STAGED change deletes a committed test
// file while editing an implementation file — the HARD case, reached through
// real git rather than a literal diff string.
func stageTestDeletion(t *testing.T) {
	t.Helper()
	// Guard BEFORE writing anything: the writes below are joined against this
	// dir, so a caller that forgot isolate(t) fails here instead of littering
	// foo.go/foo_test.go into the package directory and breaking go vet for
	// every session sharing the working tree.
	wd := requireTempWorkdir(t)
	require.NoError(t, os.WriteFile(filepath.Join(wd, "foo.go"), []byte("package p\n\nfunc Foo() int { return 0 }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wd, "foo_test.go"), []byte("package p\n\nfunc TestFoo(t *testing.T) { _ = Foo() }\n"), 0o644))
	gitSmell(t, "add", "foo.go", "foo_test.go")
	gitSmell(t, "commit", "-q", "-m", "add foo")

	gitSmell(t, "rm", "-q", "foo_test.go")
	require.NoError(t, os.WriteFile(filepath.Join(wd, "foo.go"), []byte("package p\n\nfunc Foo() int { return 1 }\n"), 0o644))
	gitSmell(t, "add", "foo.go")
}

// TestStageTestDeletion_GuardsBeforeWriting runs stageTestDeletion in a
// subprocess whose cwd is NOT a temp dir (the forget-isolate(t) case): the
// requireTempWorkdir guard must fire before anything is written, so no
// foo.go/foo_test.go appears and the helper fails.
func TestStageTestDeletion_GuardsBeforeWriting(t *testing.T) {
	if os.Getenv("ATCR_STAGE_GUARD_HELPER") == "1" {
		stageTestDeletion(t)
		return
	}
	// A scratch dir inside the repo (NOT under os.TempDir()) so the guard
	// fires; any pre-guard writes land here and are removed with it.
	dir := filepath.Join("..", ".planning", ".temp", "stage-guard")
	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(abs, 0o755))
	defer os.RemoveAll(abs)

	cmd := exec.Command(os.Args[0], "-test.run=^TestStageTestDeletion_GuardsBeforeWriting$")
	cmd.Env = append(os.Environ(), "ATCR_STAGE_GUARD_HELPER=1")
	cmd.Dir = abs
	out, runErr := cmd.CombinedOutput()
	require.Error(t, runErr, "the helper must fail outside a temp workdir, got: %s", out)
	require.NoFileExists(t, filepath.Join(abs, "foo.go"), "guard must fire before any write")
	require.NoFileExists(t, filepath.Join(abs, "foo_test.go"), "guard must fire before any write")
}

// --- verdict reporting ----------------------------------------------------

func TestVerifyDiffCmd_CleanDiffReportsClean(t *testing.T) {
	code, stdout, _ := runSmell(t, smellCleanDiff, "--diff", "-")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "clean")
}

func TestVerifyDiffCmd_HardDiffReportsHardAndNamesSmell(t *testing.T) {
	code, stdout, _ := runSmell(t, smellHardDiff, "--diff", "-")
	// No --fail-on, so reporting a HARD verdict must NOT change the exit code:
	// gating is opt-in, preserving upstream's always-zero behavior (AC3).
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "hard")
	require.Contains(t, stdout, "test_deleted")
	require.Contains(t, stdout, "foo_test.go")
}

func TestVerifyDiffCmd_SoftDiffReportsSoftOnly(t *testing.T) {
	code, stdout, _ := runSmell(t, smellSoftDiff, "--diff", "-")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "soft_only")
	require.Contains(t, stdout, "suppression")
}

// The text renderer cites file:line when the smell has one (T2), so a consuming
// gate can copy the location straight into a technical-debt row.
func TestVerifyDiffCmd_TextRendererCitesFileLine(t *testing.T) {
	code, stdout, _ := runSmell(t, smellSoftDiff, "--diff", "-")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "foo.go:2")
}

// --- clean-not-error inputs (AC5) ----------------------------------------

func TestVerifyDiffCmd_EmptyInputIsCleanWithStderrNote(t *testing.T) {
	code, stdout, stderr := runSmell(t, "", "--diff", "-")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "clean")
	require.Contains(t, stderr, "empty diff")
}

// Non-diff content is reported clean with a note rather than refused, keeping
// drop-in parity with upstream diff-smell, which never exits nonzero on content.
func TestVerifyDiffCmd_NonDiffInputIsCleanWithStderrNote(t *testing.T) {
	for _, in := range []string{
		"add a nil check to Foo",
		"+ add a nil check to Foo",
		"package p\n\nfunc Foo() int { return 1 }\n",
	} {
		code, stdout, stderr := runSmell(t, in, "--diff", "-")
		require.Equal(t, 0, code, "non-diff input %q must not fail", in)
		require.Contains(t, stdout, "clean")
		require.Contains(t, stderr, "not a unified diff")
	}
}

func TestVerifyDiffCmd_CleanNoteStaysOffStdoutUnderJSON(t *testing.T) {
	code, stdout, stderr := runSmell(t, "not a diff at all", "--diff", "-", "--json")
	require.Equal(t, 0, code)
	require.Contains(t, stderr, "not a unified diff")

	// AC5: stdout under --json contains ONLY the JSON document.
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &m), "stdout must be JSON only, got: %q", stdout)
	require.Equal(t, "clean", m["summary"].(map[string]any)["verdict"])
}

// --- JSON contract (AC1) --------------------------------------------------

func TestVerifyDiffCmd_JSONPayloadOnlyOnStdout(t *testing.T) {
	code, stdout, _ := runSmell(t, smellHardDiff, "--diff", "-", "--json")
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
			Line     int    `json:"line"`
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
	require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout must be JSON only, got: %q", stdout)

	require.Equal(t, "hard", got.Summary.Verdict)
	require.Positive(t, got.Summary.Hard)
	require.Contains(t, got.Summary.ByType, "test_deleted")
	require.Contains(t, got.Files.Test, "foo_test.go")
	require.Contains(t, got.Files.Impl, "foo.go")
	require.NotEmpty(t, got.Smells)
}

// AC1: the three top-level keys are exactly files/smells/summary, and the
// verdict is drawn from the closed set.
func TestVerifyDiffCmd_JSONTopLevelKeysAreExact(t *testing.T) {
	_, stdout, _ := runSmell(t, smellHardDiff, "--diff", "-", "--json")
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &m))

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	require.ElementsMatch(t, []string{"files", "smells", "summary"}, keys)
	require.Contains(t, []string{"clean", "soft_only", "hard"},
		m["summary"].(map[string]any)["verdict"])
}

func TestVerifyDiffCmd_JSONCleanHasEmptyArraysNotNull(t *testing.T) {
	code, stdout, _ := runSmell(t, smellCleanDiff, "--diff", "-", "--json")
	require.Equal(t, 0, code)
	require.NotContains(t, stdout, "null")
}

// --- gate (AC3) -----------------------------------------------------------

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
		{"hard diff, gate none", smellHardDiff, []string{"--fail-on", "none"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := runSmell(t, tc.diff, append([]string{"--diff", "-"}, tc.failOn...)...)
			require.Equal(t, tc.want, code)
		})
	}
}

// A tripped gate still prints the report — the consumer must be able to read
// WHY it failed from the same invocation, not have to re-run without the gate.
func TestVerifyDiffCmd_GateFailureStillReports(t *testing.T) {
	code, stdout, stderr := runSmell(t, smellHardDiff, "--diff", "-", "--fail-on", "hard")
	require.Equal(t, 1, code)
	require.Contains(t, stdout, "test_deleted")
	require.Contains(t, stderr, "hard")
}

func TestVerifyDiffCmd_GateFailureUnderJSONKeepsStdoutParseable(t *testing.T) {
	code, stdout, _ := runSmell(t, smellHardDiff, "--diff", "-", "--json", "--fail-on", "hard")
	require.Equal(t, 1, code)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &m), "stdout must stay JSON-only when the gate trips")
}

func TestVerifyDiffCmd_InvalidFailOnIsUsageError(t *testing.T) {
	// Severities belong to `atcr review`/`reconcile`; this command takes verdicts.
	// An empty/whitespace value is deliberately NOT in this set — it is unset, per
	// TestVerifyDiffCmd_EmptyFailOnIsUnset below.
	for _, v := range []string{"bogus", "HIGH", "critical"} {
		code, _, _ := runSmell(t, smellCleanDiff, "--diff", "-", "--fail-on", v)
		require.Equal(t, 2, code, "--fail-on %q must be a usage error", v)
	}
}

// TestVerifyDiffCmd_EmptyFailOnIsUnset pins an explicitly-empty --fail-on as
// UNSET rather than a usage error, matching the two sibling threshold readers:
// verifyMinSeverity (cli/verify.go) and gateFlagValue (cli/reconcile.go) both
// trim and treat whitespace-only as unset, and verifyMinSeverity's comment states
// that as the shared convention.
//
// The shape this protects is exactly the one the `--fail-on none` affordance was
// added for: `atcr verify diff --fail-on "$LEVEL"` with LEVEL unset. Treating it
// as a usage error hard-fails CI here (exit 2) while being a silent no-op in
// every sibling command.
func TestVerifyDiffCmd_EmptyFailOnIsUnset(t *testing.T) {
	// A HARD diff proves the gate is genuinely OFF, not merely that nothing tripped.
	for _, v := range []string{"", " ", "\t "} {
		code, stdout, _ := runSmell(t, smellHardDiff, "--diff", "-", "--fail-on", v)
		require.Equal(t, 0, code, "--fail-on %q must be unset (gate off), not a usage error", v)
		require.Contains(t, stdout, "verdict: hard", "the scan must still run and report")
	}
	// The relaxation must not swallow a genuinely bad value.
	code, _, _ := runSmell(t, smellCleanDiff, "--diff", "-", "--fail-on", "bogus")
	require.Equal(t, 2, code, "--fail-on bogus must still be a usage error")
}

// --- repo-local config hardening ------------------------------------------

// plantTextconvDriver arms the repo in the current working directory with a
// repo-local textconv diff driver that EXECUTES a program, and returns the path
// of the canary that program creates when it runs.
//
// This is a repo-local vector by construction: gitexec pins GIT_CONFIG_GLOBAL and
// GIT_CONFIG_SYSTEM to /dev/null, and its package doc names repo-local config as
// the surface it does NOT close. `--repo <path>` explicitly invites pointing the
// scanner at a tree the operator does not control, so the command must neutralize
// the driver itself.
func plantTextconvDriver(t *testing.T) string {
	t.Helper()
	wd := requireTempWorkdir(t)
	canary := filepath.Join(wd, "TEXTCONV_RAN")
	script := filepath.Join(wd, "evil.sh")
	require.NoError(t, os.WriteFile(script,
		[]byte("#!/bin/sh\n: > \""+canary+"\"\necho converted\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wd, ".gitattributes"), []byte("*.bin diff=evil\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wd, "data.bin"), []byte("payload-one\n"), 0o644))
	gitSmell(t, "config", "diff.evil.textconv", script)
	gitSmell(t, "add", "-A")
	gitSmell(t, "commit", "-q", "-m", "arm textconv driver")

	require.NoError(t, os.WriteFile(filepath.Join(wd, "data.bin"), []byte("payload-two\n"), 0o644))
	gitSmell(t, "add", "data.bin")
	return canary
}

// TestVerifyDiffCmd_TextconvDriverIsNeutralized asserts a poisoned repo-local
// textconv driver never runs. --no-ext-diff closes `diff.external`; textconv is a
// SEPARATE execution vector it does not touch, so both git-backed sources must
// pass --no-textconv.
//
// Verified by construction: with only `--no-ext-diff --no-color`, running this
// fixture's diff creates the canary.
func TestVerifyDiffCmd_TextconvDriverIsNeutralized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the textconv driver fixture is a /bin/sh script")
	}
	t.Run("staged", func(t *testing.T) {
		isolate(t)
		initGitRepo(t)
		canary := plantTextconvDriver(t)
		wd := requireTempWorkdir(t)
		code, _, _ := runSmell(t, "", "--staged", "--repo", wd)
		require.Equal(t, 0, code)
		require.NoFileExists(t, canary, "a repo-local textconv driver must not be executed by --staged")
	})

	t.Run("rev", func(t *testing.T) {
		isolate(t)
		initGitRepo(t)
		canary := plantTextconvDriver(t)
		wd := requireTempWorkdir(t)
		gitSmell(t, "commit", "-q", "-m", "change payload")
		_ = os.Remove(canary) // clear anything the commit itself may have triggered
		code, _, _ := runSmell(t, "", "--rev", "HEAD", "--repo", wd)
		require.Equal(t, 0, code)
		require.NoFileExists(t, canary, "a repo-local textconv driver must not be executed by --rev")
	})
}

// --- merge commits --------------------------------------------------------

// mergeWithTestDeletion builds a repo whose HEAD is a MERGE commit bringing in a
// feature branch that deleted a test file while editing an implementation file.
// The reward hack is on the feature side, so it is invisible to a merge diff
// taken against all parents — which is exactly the shape CI presents, since
// refs/pull/N/merge checkouts and post-merge branches routinely have a merge at
// HEAD and --rev defaults to HEAD.
func mergeWithTestDeletion(t *testing.T) string {
	t.Helper()
	isolate(t)
	initGitRepo(t)
	wd := requireTempWorkdir(t)

	require.NoError(t, os.WriteFile(filepath.Join(wd, "foo.go"), []byte("package p\n\nfunc Foo() int { return 0 }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wd, "foo_test.go"), []byte("package p\n\nfunc TestFoo(t *testing.T) { _ = Foo() }\n"), 0o644))
	gitSmell(t, "add", "foo.go", "foo_test.go")
	gitSmell(t, "commit", "-q", "-m", "add foo")

	base := strings.TrimSpace(gitSmellOutput(t, "rev-parse", "HEAD"))
	gitSmell(t, "checkout", "-q", "-b", "feature")
	gitSmell(t, "rm", "-q", "foo_test.go")
	require.NoError(t, os.WriteFile(filepath.Join(wd, "foo.go"), []byte("package p\n\nfunc Foo() int { return 1 }\n"), 0o644))
	gitSmell(t, "commit", "-q", "-am", "delete the failing test")

	gitSmell(t, "checkout", "-q", base)
	gitSmell(t, "checkout", "-q", "-B", "mainline")
	require.NoError(t, os.WriteFile(filepath.Join(wd, "other.txt"), []byte("unrelated\n"), 0o644))
	gitSmell(t, "add", "other.txt")
	gitSmell(t, "commit", "-q", "-m", "unrelated mainline work")
	gitSmell(t, "merge", "-q", "--no-ff", "feature", "-m", "merge feature")

	// Precondition: HEAD really is a merge (rev-list --parents prints sha + parents).
	require.Len(t, strings.Fields(gitSmellOutput(t, "rev-list", "--parents", "-n1", "HEAD")), 3,
		"fixture must produce a merge commit at HEAD")
	return wd
}

// TestVerifyDiffCmd_MergeCommitIsScanned pins the gate against a merge at HEAD.
//
// `git show <merge>` prints NO diff by default, so the empty-input branch
// reported `verdict: clean`, exit 0 — under `--fail-on hard`, a silent no-op
// exactly where the gate is meant to run. `--first-parent -m` asks the question
// the gate actually cares about: what is this merge introducing to the branch it
// lands on?
func TestVerifyDiffCmd_MergeCommitIsScanned(t *testing.T) {
	wd := mergeWithTestDeletion(t)
	code, stdout, _ := runSmell(t, "", "--rev", "HEAD", "--repo", wd, "--fail-on", "hard")
	require.Equal(t, 1, code, "a merge that lands a deleted test must trip --fail-on hard, not report clean")
	require.Contains(t, stdout, "verdict: hard")
	require.Contains(t, stdout, "test_deleted")
}

// TestVerifyDiffCmd_NonMergeRevUnchanged is the negative control: the merge
// handling must not alter the ordinary single-parent case.
func TestVerifyDiffCmd_NonMergeRevUnchanged(t *testing.T) {
	wd := mergeWithTestDeletion(t)
	code, stdout, _ := runSmell(t, "", "--rev", "feature", "--repo", wd, "--fail-on", "hard")
	require.Equal(t, 1, code)
	require.Contains(t, stdout, "verdict: hard")
	require.Contains(t, stdout, "test_deleted")

	// And a commit that changes nothing suspicious still reports clean.
	code, stdout, _ = runSmell(t, "", "--rev", "mainline~0^1", "--repo", wd)
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "verdict: ")
}

// --- input size cap -------------------------------------------------------

// oversizeDiff builds a syntactically valid unified diff of at least n bytes.
// The added/removed line mix is what makes it expensive: smellRelocated compares
// every added line against every removed line in the same file, so scan cost is
// O(added × removed) and grows quadratically with input size. Measured on the
// unbounded reader: 1.46 MB took 1.82s, 0.72 MB took 0.55s — clean quadratic.
func oversizeDiff(n int) string {
	var b strings.Builder
	b.WriteString("diff --git a/big_test.go b/big_test.go\n--- a/big_test.go\n+++ b/big_test.go\n@@ -1,1 +1,1 @@\n")
	for b.Len() < n {
		b.WriteString("-old line filler filler filler filler filler\n")
		b.WriteString("+new line filler filler filler filler filler\n")
	}
	return b.String()
}

// TestVerifyDiffCmd_OversizeInputIsRefused pins a byte cap on all three diff
// sources. The in-process caller of the SAME analyzer refuses input over
// maxFixBytes (internal/verify/fixreview.go), so leaving this surface unbounded
// reinstates exactly the exposure that cap exists to prevent — made worse by the
// analyzer's quadratic relocation scan, which turns a large diff into minutes of
// CPU rather than a large allocation.
func TestVerifyDiffCmd_OversizeInputIsRefused(t *testing.T) {
	big := oversizeDiff(3 << 20) // 3 MiB, comfortably over the cap

	t.Run("stdin", func(t *testing.T) {
		code, stdout, stderr := runSmell(t, big, "--diff", "-")
		require.Equal(t, 2, code, "an oversize diff must be a usage error, not a multi-second scan")
		require.Contains(t, stderr+stdout, "too large to scan")
	})

	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "big.diff")
		require.NoError(t, os.WriteFile(p, []byte(big), 0o644))
		code, stdout, stderr := runSmell(t, "", "--diff", p)
		require.Equal(t, 2, code)
		require.Contains(t, stderr+stdout, "too large to scan")
	})

	t.Run("git", func(t *testing.T) {
		isolate(t)
		initGitRepo(t)
		wd := requireTempWorkdir(t)
		var b strings.Builder
		for b.Len() < 3<<20 {
			b.WriteString("// filler filler filler filler filler filler filler\n")
		}
		require.NoError(t, os.WriteFile(filepath.Join(wd, "big.go"), []byte("package p\n"+b.String()), 0o644))
		gitSmell(t, "add", "big.go")
		code, stdout, stderr := runSmell(t, "", "--staged", "--repo", wd)
		require.Equal(t, 2, code)
		require.Contains(t, stderr+stdout, "too large to scan")
	})
}

// TestVerifyDiffCmd_AtCapStillScans is the negative control: the cap must refuse
// only what is genuinely oversize, not shrink the useful working range.
func TestVerifyDiffCmd_AtCapStillScans(t *testing.T) {
	code, stdout, _ := runSmell(t, oversizeDiff(256<<10), "--diff", "-")
	require.Equal(t, 0, code, "a 256 KiB diff is an ordinary large commit and must still scan")
	require.Contains(t, stdout, "verdict: ")
}

// AC3: an invalid --fail-on is rejected BEFORE any git process is spawned, so a
// bad flag in a non-repo directory still exits 2 rather than failing on git.
func TestVerifyDiffCmd_InvalidFailOnRejectedBeforeGit(t *testing.T) {
	isolate(t) // a temp dir that is NOT a git repo
	code, _, stderr := runSmell(t, "", "--staged", "--fail-on", "bogus")
	require.Equal(t, 2, code)
	require.Contains(t, stderr, "--fail-on")
	require.NotContains(t, stderr, "git diff", "the gate must be validated before git runs")
}

// --- input sources (AC2) --------------------------------------------------

func TestVerifyDiffCmd_DiffFlagReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patch.diff")
	require.NoError(t, os.WriteFile(path, []byte(smellHardDiff), 0o644))

	code, stdout, _ := runSmell(t, "", "--diff", path)
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "test_deleted")
}

func TestVerifyDiffCmd_DiffDashReadsStdin(t *testing.T) {
	code, stdout, _ := runSmell(t, smellHardDiff, "--diff", "-")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "test_deleted")
}

func TestVerifyDiffCmd_MissingFileIsUsageError(t *testing.T) {
	code, _, _ := runSmell(t, "", "--diff", filepath.Join(t.TempDir(), "nope.diff"))
	require.Equal(t, 2, code)
}

func TestVerifyDiffCmd_EmptyDiffFlagIsUsageError(t *testing.T) {
	code, _, _ := runSmell(t, "", "--diff", "  ")
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

// --staged=false assigns the bool to turn the source OFF: cobra still marks the
// flag Changed, so keying the source on Changed misreads an explicit opt-out as
// naming the source. The parsed value, not Changed, decides.
func TestVerifyDiffCmd_StagedFalseIsNotASource(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	stageTestDeletion(t)
	gitSmell(t, "commit", "-q", "-m", "delete the test")

	// Falls through to the --rev HEAD default instead of scanning the (clean) index.
	code, stdout, _ := runSmell(t, "", "--staged=false")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "test_deleted")

	// ...and does not count as a named source for mutual exclusion.
	code, stdout, _ = runSmell(t, "", "--staged=false", "--rev", "HEAD")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "test_deleted")
}

func TestVerifyDiffCmd_StagedOnCleanIndexIsCleanWithNote(t *testing.T) {
	isolate(t)
	initGitRepo(t)

	code, stdout, stderr := runSmell(t, "", "--staged")
	require.Equal(t, 0, code)
	require.Contains(t, stdout, "clean")
	require.Contains(t, stderr, "empty diff")
}

// AC5's specific combination: --staged on a clean index WITH --json — stdout
// must be the JSON document only, with the explanatory note on stderr.
func TestVerifyDiffCmd_StagedCleanIndexJSONStdoutIsJSONOnly(t *testing.T) {
	isolate(t)
	initGitRepo(t)

	code, stdout, stderr := runSmell(t, "", "--staged", "--json")
	require.Equal(t, 0, code)
	require.Contains(t, stderr, "empty diff")

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &m), "stdout must be JSON only, got: %q", stdout)
	require.Equal(t, "clean", m["summary"].(map[string]any)["verdict"])
}

// --rev is the DEFAULT source: a bare `atcr verify diff` scans HEAD.
func TestVerifyDiffCmd_RevIsDefaultSourceAndScansHEAD(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	stageTestDeletion(t)
	gitSmell(t, "commit", "-q", "-m", "delete the test")

	for _, args := range [][]string{nil, {"--rev", "HEAD"}} {
		code, stdout, _ := runSmell(t, "", args...)
		require.Equal(t, 0, code, "args %v", args)
		require.Contains(t, stdout, "test_deleted", "args %v", args)
	}
}

// `git show --format=` must suppress the commit header, or the subject and
// author lines would reach the parser as content. Asserted against the captured
// ARGV — the analyzer ignores header lines outside any @@ hunk, so an
// output-only assertion cannot observe the flag's removal (mutation-proved).
// The gitexec.CommandContextFn package var is the seam, the same pattern as
// verifyRun/newRedactor in cli/verify.go.
func TestVerifyDiffCmd_RevSuppressesCommitHeader(t *testing.T) {
	if os.Getenv("ATCR_GIT_STUB_HELPER") == "1" {
		// Simulate git: with --format= only the diff body is printed; without
		// it the commit header (carrying the "sneaky" canary) precedes it.
		withHeader := true
		for _, a := range os.Args {
			if a == "--format=" {
				withHeader = false
			}
		}
		if withHeader {
			fmt.Print("commit abc123\nAuthor: t <t@t.invalid>\n\n    //nolint:sneaky subject line\n\n")
		}
		fmt.Print(smellHardDiff)
		os.Exit(0)
	}

	var captured [][]string
	stub := gitexec.CommandContextFn
	gitexec.CommandContextFn = func(ctx context.Context, arg ...string) *exec.Cmd {
		captured = append(captured, append([]string(nil), arg...))
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVerifyDiffCmd_RevSuppressesCommitHeader$")
		cmd.Env = append(os.Environ(), "ATCR_GIT_STUB_HELPER=1")
		return cmd
	}
	defer func() { gitexec.CommandContextFn = stub }()

	_, stdout, _ := runSmell(t, "", "--rev", "HEAD", "--json")
	require.NotContains(t, stdout, "sneaky",
		"the commit subject must not be scanned as diff content")

	require.NotEmpty(t, captured, "the --rev path must shell out to git")
	show := captured[len(captured)-1]
	require.Contains(t, show, "show")
	require.Contains(t, show, "--format=", "git show must suppress the commit header")
	// Pinned at the same seam: without these, `git show <merge>` prints nothing
	// and the gate reports clean on every merge commit. The behavioural guard is
	// TestVerifyDiffCmd_MergeCommitIsScanned; this catches a "tidy up the argv"
	// edit directly.
	require.Contains(t, show, "--first-parent", "git show must diff a merge against its first parent")
	require.Contains(t, show, "-m", "git show must emit a diff for merge commits")
}

// AC2: the same diff through two different sources yields identical JSON.
func TestVerifyDiffCmd_SourcesAgreeByteForByte(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	stageTestDeletion(t)

	_, viaStaged, _ := runSmell(t, "", "--staged", "--json")

	// Capture the same diff to a file and feed it back through --diff. The raw
	// git call runs under the SAME hardened env as the CLI path (gitexec), so
	// the byte-for-byte comparison never reads the runner's global git config
	// on one side only.
	path := filepath.Join(t.TempDir(), "staged.diff")
	out := gitSmellOutput(t, "diff", "--no-ext-diff", "--no-color", "--cached")
	require.NoError(t, os.WriteFile(path, []byte(out), 0o644))

	_, viaFile, _ := runSmell(t, "", "--diff", path, "--json")
	require.Equal(t, viaStaged, viaFile, "the same diff through two sources must yield identical JSON")
}

// An interrupted scan is not a usage error: runMain cancels the root context
// on SIGINT/SIGTERM, the git child dies, and the command must not report
// exit 2 ("usage or configuration error") for what was simply a cancelled job.
// Evidence text comes from string(b) of raw diff bytes; encoding/json silently
// substitutes U+FFFD for invalid bytes, so an invalid-UTF-8 input would corrupt
// the reported evidence with no diagnostic. The run must warn instead.
func TestVerifyDiffCmd_InvalidUTF8InputWarnsOnStderr(t *testing.T) {
	in := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n" +
		"@@ -1,1 +1,2 @@\n func Foo() int {\n+\t//nolint:errcheck caf\xff\xfe\n"
	code, _, stderr := runSmell(t, in, "--diff", "-")
	require.Equal(t, 0, code)
	require.Contains(t, stderr, "UTF-8")
}

func TestVerifyDiffCmd_CancelledContextIsNotUsageError(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled when git starts, deterministically
	code, _, _ := runSmellContext(ctx, t, "", "--staged")
	require.NotEqual(t, 2, code, "an interrupted scan must not report a usage error")
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

// A nonexistent --repo must surface the SHARED normalizeRepoFlag wording that
// `atcr verify` and `atcr reconcile` emit for the identical flag, not a raw git
// message — one normalization, one error string (Epic 22.1).
func TestVerifyDiffCmd_BadRepoUsesSharedMessage(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-a-repo")
	code, _, stderr := runSmell(t, "", "--staged", "--repo", missing)
	require.Equal(t, 2, code)
	require.Contains(t, stderr, "does not exist or is not a directory")
	require.Contains(t, stderr, missing)
}

// --- source conflicts (AC4) ----------------------------------------------

func TestVerifyDiffCmd_SourcesAreMutuallyExclusive(t *testing.T) {
	for _, args := range [][]string{
		{"--diff", "-", "--staged"},
		{"--staged", "--rev", "HEAD"},
		{"--diff", "-", "--rev", "HEAD"},
		// Reversed argv order: the message follows diffSourceFlags order, so
		// the first argv flag is NOT necessarily the first flag named.
		{"--rev", "HEAD", "--staged"},
	} {
		code, _, stderr := runSmell(t, "", args...)
		require.Equal(t, 2, code, "args %v must be a usage error", args)
		require.Contains(t, stderr, "mutually exclusive")
		// AC4: the message names BOTH offending flags — derived from the row,
		// not from args[0] (which coincides with diffSourceFlags order in the
		// forward rows and so cannot catch a dropped second name).
		for _, f := range diffSourceFlags {
			for _, a := range args {
				if a == "--"+f {
					require.Contains(t, stderr, a, "args %v: message must name %s", args, a)
					break
				}
			}
		}
	}
}

// --rev is interpolated into git's argv. A value starting with "-" would be
// read as an OPTION — `--output=<path>` alone is enough to make the scan write
// a file of the caller's choosing.
func TestVerifyDiffCmd_RevArgsRejectLeadingDash(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	for _, v := range []string{"--output=/tmp/pwned", "-p", "--exec=touch /tmp/x"} {
		code, _, _ := runSmell(t, "", "--rev", v)
		require.Equal(t, 2, code, "--rev %q must be rejected", v)
	}
}

// TestVerifyDiffCmd_RevIsNotAPathspec pins the rev/pathspec boundary. Without a
// `--` separator git happily reads a path-shaped --rev as a PATHSPEC — `--rev
// foo.go` became `git show HEAD -- foo.go`, which silently narrowed the scan to
// one file and exited 0. A typo'd or stale rev in a CI gate must fail loudly, not
// quietly reduce coverage.
func TestVerifyDiffCmd_RevIsNotAPathspec(t *testing.T) {
	wd := mergeWithTestDeletion(t)
	code, _, stderr := runSmell(t, "", "--rev", "foo.go", "--repo", wd)
	require.Equal(t, 2, code, "a path-shaped --rev must be refused, not reinterpreted as a pathspec")
	require.Contains(t, strings.ToLower(stderr), "revision", "the error must say the value is not a revision")
}

// TestVerifyDiffCmd_RevUsesEndOfOptions pins the hardening at the argv seam. The
// repo standardized on git's own --end-of-options for option injection
// (internal/gitrange/resolver.go, internal/fanout/review.go); the hand-rolled
// leading-dash check stays as defense in depth, but it cannot close rev/pathspec
// ambiguity — only `--` does — so both must be on the wire.
func TestVerifyDiffCmd_RevUsesEndOfOptions(t *testing.T) {
	if os.Getenv("ATCR_GIT_STUB_HELPER") == "1" {
		fmt.Print(smellHardDiff)
		os.Exit(0)
	}
	var captured [][]string
	stub := gitexec.CommandContextFn
	gitexec.CommandContextFn = func(ctx context.Context, arg ...string) *exec.Cmd {
		captured = append(captured, append([]string(nil), arg...))
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVerifyDiffCmd_RevUsesEndOfOptions$")
		cmd.Env = append(os.Environ(), "ATCR_GIT_STUB_HELPER=1")
		return cmd
	}
	defer func() { gitexec.CommandContextFn = stub }()

	_, _, _ = runSmell(t, "", "--rev", "HEAD")
	require.NotEmpty(t, captured, "the --rev path must shell out to git")
	show := captured[len(captured)-1]

	eoo := indexOf(show, "--end-of-options")
	rev := indexOf(show, "HEAD")
	sep := indexOf(show, "--")
	require.GreaterOrEqual(t, eoo, 0, "argv must pass --end-of-options: %v", show)
	require.GreaterOrEqual(t, sep, 0, "argv must pass a `--` separator so a rev is never read as a pathspec: %v", show)
	require.Less(t, eoo, rev, "--end-of-options must precede the revision: %v", show)
	require.Less(t, rev, sep, "the `--` separator must follow the revision: %v", show)
}

// indexOf returns the position of want in argv, or -1.
func indexOf(argv []string, want string) int {
	for i, a := range argv {
		if a == want {
			return i
		}
	}
	return -1
}

func TestVerifyDiffCmd_EmptyRevArgsAreUsageErrors(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	code, _, _ := runSmell(t, "", "--rev", "   ")
	require.Equal(t, 2, code, "--rev with a blank value must be a usage error")
}

func TestVerifyDiffCmd_PositionalArgIsUsageError(t *testing.T) {
	// Sources are named by flag; a stray positional must not be silently ignored.
	code, _, _ := runSmell(t, "", "patch.diff")
	require.Equal(t, 2, code)
}

// --- wiring (AC6) ---------------------------------------------------------

func TestVerifyDiffCmd_RegisteredUnderVerify(t *testing.T) {
	out, err := execute(t, "verify", "--help")
	require.NoError(t, err)
	require.Contains(t, out, "diff")
}

// AC6: the parent command's own flags and args are unchanged.
func TestVerifyDiffCmd_ParentFlagsUnchanged(t *testing.T) {
	out, err := execute(t, "verify", "--help")
	require.NoError(t, err)
	for _, f := range []string{"--fresh", "--thorough", "--min-severity", "--exec", "--repo"} {
		require.Contains(t, out, f, "`atcr verify` must keep its own flag %s", f)
	}
}

// The shadowing escape hatch documented on the AddCommand call site: `--`
// terminates cobra's child search, so the parent still receives "diff" as its
// positional id. Exit 2 here is the parent's own "no such review" path, NOT the
// diff scanner's — reaching it at all is the proof.
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
