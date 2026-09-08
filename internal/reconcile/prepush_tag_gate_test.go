package reconcile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pinGateEcho is the line .githooks/pre-push prints immediately before it runs
// `GOWORK=off go build ./...`. The gate is a build, so the echo — not an exit
// code — is what tells us whether the gate ran at all.
const pinGateEcho = "pre-push: GOWORK=off go build"

// TestPrePush_SkipsPinGateForReconcileTagOnlyPush pins the release-process
// deadlock out of existence.
//
// docs/release-process.md step 2 publishes the reconcile subtree by pushing a
// `reconcile/vX.Y.Z` tag cut from the branch tip, and only step 4 — after that
// tag exists — bumps the root `go.mod` pin. Between the two, the root module by
// construction does NOT build against the pinned reconcile. A pre-push hook that
// runs the pin gate on every push therefore blocks the very push the documented
// procedure requires, and the only way through is `git push --no-verify` — which
// disables gofmt, golangci-lint, go test and the reconcile module checks too.
//
// The gate must therefore be scoped: skipped when every ref being pushed is a
// `refs/tags/reconcile/*` tag, and run in every other case, INCLUDING a push
// that carries a branch alongside such a tag.
func TestPrePush_SkipsPinGateForReconcileTagOnlyPush(t *testing.T) {
	tests := []struct {
		name     string
		stdin    string
		wantGate bool
	}{
		{
			name:     "reconcile tag only — the documented step 2 push",
			stdin:    "refs/tags/reconcile/v0.9.0 1111111111111111111111111111111111111111 refs/tags/reconcile/v0.9.0 0000000000000000000000000000000000000000\n",
			wantGate: false,
		},
		{
			name:     "two reconcile tags, still tag-only",
			stdin:    "refs/tags/reconcile/v0.9.0 1111111111111111111111111111111111111111 refs/tags/reconcile/v0.9.0 0000000000000000000000000000000000000000\nrefs/tags/reconcile/v0.9.1 2222222222222222222222222222222222222222 refs/tags/reconcile/v0.9.1 0000000000000000000000000000000000000000\n",
			wantGate: false,
		},
		{
			name:     "ordinary branch push",
			stdin:    "refs/heads/main 1111111111111111111111111111111111111111 refs/heads/main 0000000000000000000000000000000000000000\n",
			wantGate: true,
		},
		{
			name:     "branch pushed alongside a reconcile tag — the branch still needs the gate",
			stdin:    "refs/heads/main 1111111111111111111111111111111111111111 refs/heads/main 0000000000000000000000000000000000000000\nrefs/tags/reconcile/v0.9.0 2222222222222222222222222222222222222222 refs/tags/reconcile/v0.9.0 0000000000000000000000000000000000000000\n",
			wantGate: true,
		},
		{
			name:     "app tag, a different namespace entirely",
			stdin:    "refs/tags/v1.2.3 1111111111111111111111111111111111111111 refs/tags/v1.2.3 0000000000000000000000000000000000000000\n",
			wantGate: true,
		},
		{
			name:     "empty stdin — a delete or an unknown shape, never assume it is safe to skip",
			stdin:    "",
			wantGate: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := runPrePushWithStubbedTools(t, tc.stdin)
			if tc.wantGate {
				assert.Contains(t, out, pinGateEcho,
					"the pin gate must run for this push shape")
			} else {
				assert.NotContains(t, out, pinGateEcho,
					"the pin gate must be skipped for a reconcile-tag-only push — it cannot pass until step 4 bumps the pin, so running it here forces --no-verify")
			}
		})
	}
}

// TestReleaseProcessDoc_DocumentsThePrePushTagException keeps the procedure and
// the hook from drifting apart again. The deadlock was reported precisely
// because the document told the operator to make a push the hook refused, and
// said nothing about it.
func TestReleaseProcessDoc_DocumentsThePrePushTagException(t *testing.T) {
	root := repoRootDir(t)
	body, err := os.ReadFile(filepath.Join(root, "docs", "release-process.md"))
	require.NoError(t, err)

	doc := string(body)
	assert.Contains(t, doc, "pre-push",
		"release-process.md must name the pre-push hook where it tells the operator to push the tag")
	assert.Contains(t, doc, "refs/tags/reconcile/",
		"release-process.md must state which refs the hook exempts from the pin gate, so the exemption is auditable from the procedure")
}

// runPrePushWithStubbedTools executes .githooks/pre-push with go, gofmt and
// golangci-lint stubbed out to instant no-ops. The hook's real gates are
// verified by CI; what is under test here is only WHICH gates it decides to run
// for a given set of pushed refs.
func runPrePushWithStubbedTools(t *testing.T, stdin string) string {
	t.Helper()

	root := repoRootDir(t)
	hook := filepath.Join(root, ".githooks", "pre-push")
	require.FileExists(t, hook)

	stubDir := t.TempDir()
	// `go env GOVERSION` reports an old toolchain so the wasm parser-module
	// block short-circuits; every other invocation succeeds silently.
	writeStub(t, stubDir, "go", "#!/bin/sh\nif [ \"$1\" = \"env\" ]; then echo go1.20; fi\nexit 0\n")
	writeStub(t, stubDir, "gofmt", "#!/bin/sh\nexit 0\n")
	writeStub(t, stubDir, "golangci-lint", "#!/bin/sh\nexit 0\n")

	cmd := exec.Command("sh", hook, "origin", "git@example.invalid:samestrin/atcr.git")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "the hook must exit 0 with every gate stubbed green:\n%s", out)
	return string(out)
}

func writeStub(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755))
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(repoRootFromHere)
	require.NoError(t, err)
	return abs
}

// TestPrePush_PinGateRunsWhenAReconcileTagTargetsSomethingElse covers the
// refspec whose LOCAL side is a reconcile tag but whose remote side is not.
// Exempting on the local ref alone would let a reconcile tag be written onto a
// branch with the pin gate skipped.
func TestPrePush_PinGateRunsWhenAReconcileTagTargetsSomethingElse(t *testing.T) {
	out := runPrePushWithStubbedTools(t,
		"refs/tags/reconcile/v0.9.0 1111111111111111111111111111111111111111 refs/heads/main 0000000000000000000000000000000000000000\n")
	assert.Contains(t, out, pinGateEcho,
		"a reconcile tag pushed onto a branch is not the documented step-2 push and must still be gated")
}
