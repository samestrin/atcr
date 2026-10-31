package gitexec

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A fixture commit made with no identity of its own carries FixtureEmail, never
// whatever identity the surrounding environment exports.
func TestFixtureCommandContextSetsTheSafeIdentity(t *testing.T) {
	t.Setenv("GIT_AUTHOR_EMAIL", "someone@example.com")
	t.Setenv("GIT_COMMITTER_EMAIL", "someone@example.com")
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "-q"},
		{"commit", "-q", "--allow-empty", "-m", "x"},
	} {
		if out, err := FixtureCommandContext(ctx, dir, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	out, err := FixtureCommandContext(ctx, dir, "log", "-1", "--format=%an <%ae>|%cn <%ce>").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := FixtureName + " <" + FixtureEmail + ">"
	if got := strings.TrimSpace(string(out)); got != want+"|"+want {
		t.Fatalf("fixture commit identity = %q, want %q for author and committer", got, want)
	}
}

func TestFixtureEmailIsReserved(t *testing.T) {
	// RFC 2606: .invalid can never be a real mailbox, so no GitHub account can claim it.
	if !strings.HasSuffix(FixtureEmail, ".invalid") {
		t.Fatalf("FixtureEmail %q is not under the reserved .invalid TLD", FixtureEmail)
	}
}

// A fixture must never run against an atcr checkout: that is how a test identity
// reached the public history (2026-10-30).
func TestFixtureCommandContextRefusesAnAtcrCheckout(t *testing.T) {
	root := moduleRoot(t)
	for _, dir := range []string{root, filepath.Join(root, "internal", "gitexec"), ""} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("FixtureCommandContext(%q) did not refuse a dir inside the atcr checkout", dir)
				}
			}()
			FixtureCommandContext(context.Background(), dir, "status")
		}()
	}
}

func TestFixtureCommandContextAllowsAForeignGoModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	FixtureCommandContext(context.Background(), dir, "status") // must not panic
}

// Every git identity written in this repo's Go files is under .invalid. A literal
// such as t@t.t belongs to a real GitHub account and showed it as a contributor.
var identityLiteral = regexp.MustCompile(`(?:user\.email[=", ]+|_EMAIL=)([^"\s,)]+)`)

func TestEveryGitIdentityInTheRepoIsReserved(t *testing.T) {
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || exists(filepath.Join(path, ".git"))) {
				return filepath.SkipDir // dot dirs and nested worktrees are not this checkout's source
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range identityLiteral.FindAllStringSubmatch(string(src), -1) {
			email := m[1]
			if strings.Contains(email, "@") && !strings.HasSuffix(email, ".invalid") {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s: git identity %q is not under .invalid; use gitexec.FixtureEmail", rel, email)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil || !exists(filepath.Join(root, "go.mod")) {
		t.Fatalf("module root not found from %s", root)
	}
	return root
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
