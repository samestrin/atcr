//go:build integration

// Integration tests for the repo-root install.sh wrapper.
//
// The happy-path test runs a REAL, unstubbed
// `go install github.com/samestrin/atcr/cmd/atcr@latest` via install.sh, per the
// project's recorded convention (.planning/.knowledge/kb-2026-07-11-9e4bcd.md): no
// mocked Go environment for the happy path.
//
// PRECONDITION / SKIP-GUARD (Sprint 33.2, Phase 1): until the repo is public
// (Phase 3) and the reconcile `replace` directive is dropped (Phase 4), a true
// `@latest` install cannot succeed — the module is private/untagged and the fetched
// go.mod still carries a filesystem `replace` directive that Go refuses to install.
// So this test runs the real install and calls t.Skip ONLY when the failure matches
// a known pre-public blocking signature (private/unreachable or replace-directive
// refusal). Any OTHER failure is a genuine test failure. Post-Phase-4 the same test
// exercises the real install for real — no env-var gate to remember to flip.
//
// Run with: go test -tags=integration ./cmd/atcr/...

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// exact stderr line install.sh must print when the Go toolchain is absent.
const wantPreflightMsg = "error: go toolchain not found. Install Go 1.25+ from https://go.dev/dl/ and re-run this script."

// modulePath must co-occur with a signature for a failure to count as a known
// pre-public blocker. This scopes the skip-guard to *this* module's unreachability,
// so a generic post-public install regression (a 404 on some dependency, a typo'd
// import path) fails loudly instead of being silently skipped as a false pass.
const modulePath = "github.com/samestrin/atcr"

// Substrings (lower-cased) identifying an expected pre-public install failure that
// should skip rather than fail. Captured empirically against the current repo state
// and deliberately narrow — a generic fragment like "not found:" alone is not enough.
var knownBlockerSignatures = []string{
	"replace directives",                              // Go's refusal: fetched go.mod has a replace directive
	"could not read username for 'https://github.com", // private repo, no git credentials
	"terminal prompts disabled",                       // git ls-remote auth prompt suppressed
	"not found: github.com/samestrin/atcr",            // proxy/VCS not-found for THIS module (not a transitive dep)
}

func matchesKnownBlocker(output string) bool {
	low := strings.ToLower(output)
	// The modulePath guard is load-bearing, not redundant: three of the four
	// signatures ("replace directives", the two auth prompts) do not embed the
	// module path, so without this guard a transitive dependency's failure with
	// the same signature would be misclassified as a known pre-public blocker.
	// It is redundant only for the "not found: github.com/samestrin/atcr"
	// signature, which already embeds the path.
	if !strings.Contains(low, modulePath) {
		return false
	}
	for _, sig := range knownBlockerSignatures {
		if strings.Contains(low, sig) {
			return true
		}
	}
	return false
}

// installScriptPath resolves the repo-root install.sh relative to this test file.
func installScriptPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatalf("resolving install.sh path: %v", err)
	}
	return p
}

// goModCache resolves the shared module cache path. The subprocess is bounded
// with a timeout: `go env` is local and fast, but an unbounded exec.Command
// could block the test indefinitely in a broken toolchain environment.
func goModCache(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("resolving GOMODCACHE: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestInstallScriptExists confirms install.sh is present and executable.
func TestInstallScriptExists(t *testing.T) {
	p := installScriptPath(t)
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("install.sh not found at %s: %v", p, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("install.sh at %s is not executable (mode %v)", p, info.Mode())
	}
}

// TestInstallScriptRealInstall runs the real go install via install.sh into an
// isolated temp GOPATH, asserting exit 0, the binary landing at GOPATH/bin/atcr, and
// the non-fatal PATH-remediation warning. Skips on a known pre-public blocker.
func TestInstallScriptRealInstall(t *testing.T) {
	script := installScriptPath(t)
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash not found: %v", err)
	}

	// Share the real module cache so t.TempDir() cleanup never fights Go's
	// read-only module cache under a temp GOPATH.
	modCache := goModCache(t)

	tmp := t.TempDir()
	gopath := filepath.Join(tmp, "gopath")
	gobin := filepath.Join(gopath, "bin")

	cmd := exec.Command(bash, script)
	// Isolated GOPATH (binary lands in GOPATH/bin), shared module cache, and a PATH
	// that deliberately excludes GOPATH/bin so the non-fatal warning path is exercised.
	cmd.Env = append(scrubEnv(os.Environ(), "GOPATH", "GOBIN", "GOMODCACHE"),
		"GOPATH="+gopath,
		"GOMODCACHE="+modCache,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()
	combined := stdout.String() + stderr.String()
	code := cmd.ProcessState.ExitCode()

	if code != 0 {
		if matchesKnownBlocker(combined) {
			t.Skipf("skipping real-install assertion: repo not yet publicly installable (Phase 3/4 pending)\n%s", combined)
		}
		t.Fatalf("install.sh exited %d with an unexpected failure:\n%s", code, combined)
	}

	// Success path: keep the full install output for post-mortem debugging
	// (visible with `go test -v`), then check the binary landed.
	t.Logf("install.sh output:\n%s", combined)
	bin := filepath.Join(gobin, "atcr")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("expected installed binary at %s: %v\noutput:\n%s", bin, err, combined)
	}
	// Smoke-check the installed binary actually runs — os.Stat alone would pass on
	// a zero-byte file, a stale leftover, or a broken artifact. `atcr version`
	// prints "atcr version <ver>"; assert exit 0 plus that exact prefix.
	verOut, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("installed binary at %s failed to run `version`: %v\noutput:\n%s", bin, err, verOut)
	}
	if !strings.Contains(string(verOut), "atcr version ") {
		t.Errorf("expected `atcr version` output to contain %q, got:\n%s", "atcr version ", verOut)
	}
	// Non-fatal PATH warning must appear on stderr (install.sh prints it >&2)
	// because GOPATH/bin is not on PATH — and never on stdout.
	if !strings.Contains(stderr.String(), "export PATH=") {
		t.Errorf("expected PATH-remediation warning (export PATH=...) on stderr:\n%s", stderr.String())
	}
	if strings.Contains(stdout.String(), "export PATH=") {
		t.Errorf("PATH-remediation warning must not appear on stdout:\n%s", stdout.String())
	}
}

// TestInstallScriptMissingToolchain runs install.sh with a PATH that excludes go and
// asserts a non-zero exit and the exact preflight stderr message before any install.
func TestInstallScriptMissingToolchain(t *testing.T) {
	script := installScriptPath(t)
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash not found: %v", err)
	}

	empty := t.TempDir() // a PATH dir containing neither go nor anything else
	cmd := exec.Command(bash, script)
	cmd.Env = append(scrubEnv(os.Environ(), "PATH"), "PATH="+empty)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()
	code := cmd.ProcessState.ExitCode()

	if code == 0 {
		t.Fatalf("expected non-zero exit when go is absent, got 0:\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	// The preflight message must go to stderr (install.sh prints it >&2), never stdout.
	if !strings.Contains(stderr.String(), wantPreflightMsg) {
		t.Fatalf("expected exact preflight message %q on stderr, got:\n%s", wantPreflightMsg, stderr.String())
	}
	if strings.Contains(stdout.String(), wantPreflightMsg) {
		t.Errorf("preflight message must not appear on stdout:\n%s", stdout.String())
	}
}

// TestInstallScriptPathCheck exercises install.sh's exact colon-segment PATH
// containment check from both sides: an exact GOPATH/bin segment must suppress
// the non-fatal warning, and a partial-collision segment (a superstring of
// gobin that is not equal to it) must NOT — a regression reverting install.sh
// to grep/substring matching would flip the second subtest, and a bug inverting
// the on_path flag would flip the first. Skips on a known pre-public blocker.
func TestInstallScriptPathCheck(t *testing.T) {
	script := installScriptPath(t)
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash not found: %v", err)
	}
	modCache := goModCache(t)

	// run installs into an isolated GOPATH with PATH = extraSegments + the
	// inherited PATH (toolchain stays reachable), returning combined output.
	run := func(t *testing.T, gopath string, extraSegments ...string) string {
		t.Helper()
		segments := append(extraSegments, os.Getenv("PATH"))
		cmd := exec.Command(bash, script)
		cmd.Env = append(scrubEnv(os.Environ(), "GOPATH", "GOBIN", "GOMODCACHE", "PATH"),
			"GOPATH="+gopath,
			"GOMODCACHE="+modCache,
			"PATH="+strings.Join(segments, string(os.PathListSeparator)),
		)
		out, _ := cmd.CombinedOutput()
		combined := string(out)
		if code := cmd.ProcessState.ExitCode(); code != 0 {
			if matchesKnownBlocker(combined) {
				t.Skipf("skipping PATH-check assertion: repo not yet publicly installable (Phase 3/4 pending)\n%s", combined)
			}
			t.Fatalf("install.sh exited %d with an unexpected failure:\n%s", code, combined)
		}
		return combined
	}

	t.Run("exact gobin segment suppresses PATH warning", func(t *testing.T) {
		gopath := filepath.Join(t.TempDir(), "gopath")
		gobin := filepath.Join(gopath, "bin")
		combined := run(t, gopath, gobin)
		if strings.Contains(combined, "export PATH=") {
			t.Errorf("expected no PATH-remediation warning when GOPATH/bin is exactly on PATH, got:\n%s", combined)
		}
	})

	t.Run("partial-collision segment does not suppress PATH warning", func(t *testing.T) {
		gopath := filepath.Join(t.TempDir(), "gopath")
		// A superstring of gobin that is not an exact PATH segment — substring
		// matching would wrongly treat this as "on PATH".
		partial := filepath.Join(gopath, "bin") + "-partial"
		combined := run(t, gopath, partial)
		if !strings.Contains(combined, "export PATH=") {
			t.Errorf("expected PATH-remediation warning when PATH holds only a partial-collision segment, got:\n%s", combined)
		}
	})
}

// scrubEnv returns env with every KEY=... entry for the given keys removed.
func scrubEnv(env []string, keys ...string) []string {
	out := env[:0:0]
	for _, e := range env {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(e, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, e)
		}
	}
	return out
}

// TestScrubEnv covers scrubEnv directly (it was previously exercised only
// indirectly through test helpers): empty input, single key, multiple keys,
// no-match, and the KEY= prefix boundary (GOPATHX must survive a GOPATH scrub).
func TestScrubEnv(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		keys []string
		want []string
	}{
		{"empty input", nil, []string{"GOPATH"}, nil},
		{"single key scrubbed", []string{"GOPATH=/x", "HOME=/h"}, []string{"GOPATH"}, []string{"HOME=/h"}},
		{"multiple keys scrubbed", []string{"GOPATH=/x", "GOBIN=/b", "HOME=/h"}, []string{"GOPATH", "GOBIN"}, []string{"HOME=/h"}},
		{"no match keeps everything", []string{"HOME=/h", "PATH=/p"}, []string{"GOPATH"}, []string{"HOME=/h", "PATH=/p"}},
		{"prefix-similar key not scrubbed", []string{"GOPATHX=/x", "GOPATH=/g"}, []string{"GOPATH"}, []string{"GOPATHX=/x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scrubEnv(tc.env, tc.keys...)
			if !slices.Equal(got, tc.want) {
				t.Errorf("scrubEnv(%v, %v) = %v, want %v", tc.env, tc.keys, got, tc.want)
			}
		})
	}
}
