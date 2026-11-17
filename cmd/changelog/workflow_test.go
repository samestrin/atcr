package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const changelogWorkflow = "../../.github/workflows/changelog-check.yml"

// baseRef is the only ref the checker may be built from: the PR's base commit,
// which the PR cannot edit.
const baseRef = "${{ github.event.pull_request.base.sha }}"

type workflowStep struct {
	Name             string            `yaml:"name"`
	Uses             string            `yaml:"uses"`
	Run              string            `yaml:"run"`
	WorkingDirectory string            `yaml:"working-directory"`
	With             map[string]string `yaml:"with"`
	Env              map[string]string `yaml:"env"`
}

var buildOutputRE = regexp.MustCompile(`go build\b.*\s-o\s+("[^"]+"|\S+)`)

// checkerBuiltFromBase reports why a changelog-check.yml would let a PR's own
// cmd/changelog decide whether that PR passes: the checker must be compiled
// from a checkout of the base commit and the check must run that binary, never
// `go run` from the PR checkout.
func checkerBuiltFromBase(src []byte) error {
	var wf struct {
		Jobs map[string]struct {
			Steps []workflowStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(src, &wf); err != nil {
		return fmt.Errorf("parsing workflow: %w", err)
	}
	job, ok := wf.Jobs["fragment"]
	if !ok {
		return errors.New("no fragment job")
	}
	baseDir, built, checkAt := "", "", -1
	buildAt := -1
	for i, s := range job.Steps {
		if strings.Contains(s.Run, "${{") {
			return fmt.Errorf("step %q expands ${{ }} inside run:; untrusted values must arrive through env:", s.Name)
		}
		if strings.Contains(s.Run, "go run") {
			return fmt.Errorf("step %q uses go run, which compiles the PR's own code", s.Name)
		}
		inBaseBuild := baseDir != "" && s.WorkingDirectory == baseDir && strings.Contains(s.Run, "go build")
		if strings.Contains(s.Run, "cmd/changelog") && !inBaseBuild {
			return fmt.Errorf("step %q touches cmd/changelog outside the base build", s.Name)
		}
		switch {
		case strings.HasPrefix(s.Uses, "actions/checkout@") && s.With["ref"] == baseRef:
			if s.With["path"] == "" {
				return fmt.Errorf("step %q checks out the base ref over the workspace root, where the PR head also lives", s.Name)
			}
			baseDir = s.With["path"]
		case strings.Contains(s.Run, "go build") && strings.Contains(s.Run, "./cmd/changelog"):
			if baseDir == "" || s.WorkingDirectory != baseDir {
				return fmt.Errorf("step %q builds ./cmd/changelog outside the base checkout (working-directory %q, base checkout %q)", s.Name, s.WorkingDirectory, baseDir)
			}
			if strings.Contains(s.Run, "cd ") || strings.Contains(s.Run, " -C ") {
				return fmt.Errorf("step %q changes directory before building; the build must run in the base checkout", s.Name)
			}
			m := buildOutputRE.FindStringSubmatch(s.Run)
			if m == nil {
				return fmt.Errorf("step %q builds the checker without -o, so the check cannot name the binary", s.Name)
			}
			built, buildAt = m[1], i
		case strings.Contains(s.Run, " check --base "):
			checkAt = i
			if built == "" {
				return fmt.Errorf("step %q runs the check before any step builds the checker from the base ref", s.Name)
			}
			if !strings.HasPrefix(strings.TrimSpace(s.Run), built+" check ") {
				return fmt.Errorf("step %q does not run the base-built binary %s", s.Name, built)
			}
			if s.WorkingDirectory == baseDir {
				return fmt.Errorf("step %q runs in the base checkout; the check must read the PR head", s.Name)
			}
			if s.Env["BASE_SHA"] != baseRef {
				return fmt.Errorf("step %q: BASE_SHA is %q, want %q", s.Name, s.Env["BASE_SHA"], baseRef)
			}
		}
	}
	if baseDir == "" {
		return fmt.Errorf("no actions/checkout step with ref %s", baseRef)
	}
	if buildAt < 0 {
		return errors.New("no step builds ./cmd/changelog")
	}
	if checkAt < 0 {
		return errors.New("no step runs the changelog check")
	}
	return nil
}

func TestChangelogWorkflowBuildsCheckerFromBase(t *testing.T) {
	if err := checkerBuiltFromBase([]byte(readRepoFile(t, changelogWorkflow))); err != nil {
		t.Fatalf("%s: %v\nA PR must not be able to edit cmd/changelog to pass its own gate.", changelogWorkflow, err)
	}
}

// The check must reject the workflow as it stood before the fix, and each way
// back to running PR-supplied code.
func TestCheckerBuiltFromBaseRejects(t *testing.T) {
	good := readRepoFile(t, changelogWorkflow)
	for _, tc := range []struct {
		name, wf string
	}{
		{"go run from the PR checkout", `
jobs:
  fragment:
    steps:
      - name: Check out code
        uses: actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5 # v4
        with:
          fetch-depth: 0
      - name: Check
        env:
          BASE_SHA: ${{ github.event.pull_request.base.sha }}
          HEAD_SHA: ${{ github.event.pull_request.head.sha }}
        run: go run ./cmd/changelog check --base "$BASE_SHA" --head "$HEAD_SHA"
`},
		{"built from the head checkout", strings.Replace(good, "working-directory: base", "working-directory: head", 1)},
		{"base checkout of the head ref", strings.Replace(good, "ref: "+baseRef, "ref: ${{ github.event.pull_request.head.sha }}", 1)},
		{"check runs a different binary", strings.Replace(good, `"$RUNNER_TEMP/changelog" check`, `go run ./cmd/changelog check`, 1)},
		{"build changes into the head checkout", strings.Replace(good, `go build -o`, `cd ../head && go build -o`, 1)},
		{"build with go -C", strings.Replace(good, `go build -o "$RUNNER_TEMP/changelog" ./cmd/changelog`, `go -C ../head build -o "$RUNNER_TEMP/changelog" ./cmd/changelog`, 1)},
		{"untrusted expansion in run", strings.Replace(good, `--head "$HEAD_SHA"`, `--head "${{ github.event.pull_request.head.sha }}"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wf == good {
				t.Fatal("mutation did not apply; the workflow changed shape, update this test to follow it")
			}
			if err := checkerBuiltFromBase([]byte(tc.wf)); err == nil {
				t.Fatal("checkerBuiltFromBase accepted a workflow that runs PR-supplied checker code")
			}
		})
	}
}

// readRepoFile reads a repo file by path relative to this package, failing the
// test rather than the package if it has moved: a renamed workflow must read as
// "update this test", not as a panic.
func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: if the file moved, update this drift test to follow it: %v", path, err)
	}
	return string(b)
}
