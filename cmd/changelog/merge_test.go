package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/gitexec"
)

// git runs a real git command against the fixture repo at dir, through
// gitexec.FixtureCommandContext so it can never retarget the real checkout.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)
	if out, err := gitexec.FixtureCommandContext(context.Background(), dir, full...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestFragmentBranches_MergeInEitherOrder proves the point of changelog.d/:
// two branches that each add only a fragment, even in the same category, merge
// into main in both orders without a conflict, and the result assembles.
func TestFragmentBranches_MergeInEitherOrder(t *testing.T) {
	for _, order := range [][2]string{{"a", "b"}, {"b", "a"}} {
		t.Run(order[0]+"-then-"+order[1], func(t *testing.T) {
			dir := setup(t, baseChangelog, map[string]string{"README.md": "how to write a fragment\n"})
			git(t, dir, "init", "-q", "-b", "main")
			git(t, dir, "add", "-A")
			git(t, dir, "commit", "-q", "-m", "base")

			frags := map[string]string{"a": "added-feature-a.md", "b": "added-feature-b.md"}
			for name, file := range frags {
				git(t, dir, "checkout", "-q", "-b", name, "main")
				writeFile(t, filepath.Join(dir, "changelog.d", file), "- feature "+name+". (epic 2)\n")
				git(t, dir, "add", "-A")
				git(t, dir, "commit", "-q", "-m", "fragment "+name)
			}

			git(t, dir, "checkout", "-q", "main")
			for _, name := range order {
				git(t, dir, "merge", "-q", "--no-ff", "-m", "merge "+name, name)
			}

			if code, _, stderr := runIn(dir, "--version", "0.6.0", "--date", "2026-10-28"); code != 0 {
				t.Fatalf("assemble exit %d: %s", code, stderr)
			}
			got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
			for _, want := range []string{"- feature a. (epic 2)", "- feature b. (epic 2)"} {
				if strings.Count(got, want) != 1 {
					t.Errorf("want %q exactly once in:\n%s", want, got)
				}
			}
		})
	}
}
