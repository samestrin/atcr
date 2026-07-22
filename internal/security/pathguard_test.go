package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsProtectedPath exercises AC3's requirement that IsProtectedPath matches
// across canonical, relative, and traversal path formats for every blocklist
// category, and correctly rejects lookalike (non-protected) paths. Symlink
// traversal is covered separately in TestIsProtectedPath_SymlinkTraversal.
func TestIsProtectedPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		// --- .git/ (dir + bare worktree file) ---
		{".git exact", ".git", true},
		{".git nested config", ".git/config", true},
		{".git nested hook", ".git/hooks/pre-commit", true},
		{".git relative dot-slash", "./.git/config", true},
		{".git traversal into", "foo/../.git/config", true},
		{".git nested submodule", "vendor/lib/.git/config", true},
		{".git case-insensitive", ".GIT/config", true},

		// --- .githooks/ ---
		{".githooks exact", ".githooks", true},
		{".githooks nested", ".githooks/pre-commit", true},
		{".githooks bare relative", "./.githooks/pre-push", true},

		// --- .github/workflows/ (only the workflows subtree) ---
		{"workflows nested", ".github/workflows/ci.yml", true},
		{"workflows exact dir", ".github/workflows", true},
		{"workflows case-insensitive", ".github/Workflows/ci.yml", true},
		{".github actions composite", ".github/actions/build/action.yml", true},
		{".github actions dir", ".github/actions", true},
		{".github actions case-insensitive", ".github/Actions/x/action.yml", true},
		{".github non-workflows not protected", ".github/CODEOWNERS", false},
		{".github dependabot not protected", ".github/dependabot.yml", false},
		{".github issue template not protected", ".github/ISSUE_TEMPLATE/bug.md", false},

		// --- CI definition files ---
		{".gitlab-ci.yml root", ".gitlab-ci.yml", true},
		{".gitlab-ci.yml nested", "subproject/.gitlab-ci.yml", true},
		{".circleci dir blocking", ".circleci", true},
		{".circleci config blocking", ".circleci/config.yml", true},
		{".circleci nested blocking", "subproject/.circleci/config.yml", true},

		// --- .vscode/ and .idea/ ---
		{".vscode exact", ".vscode", true},
		{".vscode tasks", ".vscode/tasks.json", true},
		{".idea exact", ".idea", true},
		{".idea run config", ".idea/runConfigurations/app.xml", true},

		// --- .env* ---
		{".env exact", ".env", true},
		{".env.local", ".env.local", true},
		{".env.production", ".env.production", true},
		{".envrc", ".envrc", true},
		{".env nested dir", "config/.env", true},
		{".env.example.txt matches .env* glob", ".env.example.txt", true},

		// --- .planning/ and .atcr ---
		{".planning exact", ".planning", true},
		{".planning nested", ".planning/sprints/active/x.md", true},
		{".atcr file", ".atcr", true},
		{".atcr as dir", ".atcr/config.yaml", true},

		// --- negatives: lookalikes and normal source files ---
		{".gitignore not protected", ".gitignore", false},
		{".gitattributes not protected", ".gitattributes", false},
		{".gitmodules not protected", ".gitmodules", false},
		{".githubx not protected", ".githubx/foo", false},
		{".vscode-custom not protected", ".vscode-custom/settings.json", false},
		{".environments not protected (not a dotenv secret)", ".environments/config", false},
		{".envoy not protected", ".envoy/config.yaml", false},
		{".envision not protected", ".envision", false},
		{"README.planning.md not protected", "README.planning.md", false},
		{"normal source file", "internal/security/pathguard.go", false},
		{"normal nested file", "cmd/atcr/main.go", false},
		{"empty string", "", false},
		{"dot", ".", false},
		{"traversal not into protected", "../sibling/file.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsProtectedPath(tt.path, ""); got != tt.want {
				t.Errorf("IsProtectedPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestIsProtectedPath_SymlinkTraversal proves AC3's symlink-traversal requirement:
// a symlink whose target resolves into a protected directory is reported protected,
// including for a not-yet-created file inside a symlinked protected directory.
func TestIsProtectedPath_SymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	// Create a real protected directory and a symlink pointing at it.
	realGit := filepath.Join(root, ".git")
	if err := os.MkdirAll(realGit, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	link := filepath.Join(root, "innocent")
	if err := os.Symlink(realGit, link); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	// A symlink that IS a protected dir resolves to true. Paths here are absolute, so
	// root is irrelevant to resolution — pass "".
	if !IsProtectedPath(link, "") {
		t.Errorf("IsProtectedPath(%q) = false, want true (symlink -> .git)", link)
	}
	// An existing file under the symlink resolves to true.
	existing := filepath.Join(realGit, "config")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	viaLink := filepath.Join(link, "config")
	if !IsProtectedPath(viaLink, "") {
		t.Errorf("IsProtectedPath(%q) = false, want true (symlink/config exists)", viaLink)
	}
	// A NOT-yet-created file inside the symlinked protected dir still resolves to
	// true via the deepest-existing-ancestor fallback.
	notYet := filepath.Join(link, "hooks", "pre-commit")
	if !IsProtectedPath(notYet, "") {
		t.Errorf("IsProtectedPath(%q) = false, want true (new file under symlinked .git)", notYet)
	}

	// A symlink to a non-protected directory is NOT protected.
	realSafe := filepath.Join(root, "safe")
	if err := os.MkdirAll(realSafe, 0o755); err != nil {
		t.Fatalf("mkdir safe: %v", err)
	}
	safeLink := filepath.Join(root, "alias")
	if err := os.Symlink(realSafe, safeLink); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if IsProtectedPath(filepath.Join(safeLink, "file.go"), "") {
		t.Errorf("IsProtectedPath(%q) = true, want false (symlink -> safe dir)", safeLink)
	}
}

// TestIsProtectedPath_SymlinkResolvesAgainstRoot proves layer-2 symlink resolution is
// anchored to the supplied working-tree root, not the process CWD (TD-003): a
// repo-relative path whose interior component is a symlink into .git is caught when
// resolved against root, and correctly missed when root is empty (the pre-fix bug,
// where a relative path could only be resolved from an unrelated CWD).
func TestIsProtectedPath_SymlinkResolvesAgainstRoot(t *testing.T) {
	root := t.TempDir()
	realGit := filepath.Join(root, ".git")
	if err := os.MkdirAll(realGit, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	// "innocent" is a symlink into the protected .git dir; the caller sees only the
	// repo-relative path "innocent/config", exactly as apply.go passes e.Path.
	if err := os.Symlink(realGit, filepath.Join(root, "innocent")); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}
	if !IsProtectedPath("innocent/config", root) {
		t.Errorf(`IsProtectedPath("innocent/config", root) = false, want true (symlink resolved against root)`)
	}
	// Without root anchoring the relative path resolves against the test's CWD, where
	// "innocent" does not exist, so layer-2 cannot catch it — the exact miss TD-003 closes.
	if IsProtectedPath("innocent/config", "") {
		t.Errorf(`IsProtectedPath("innocent/config", "") = true, want false (no root to anchor resolution)`)
	}
}

// TestNormalizeWindowsSegment proves the Windows segment-aliasing normalization
// (TD, pathguard.go:128) collapses trailing-dot/space and NTFS ADS forms to the base
// name a Windows host would resolve them to, so ".git."/".git "/".git::$INDEX_ALLOCATION"
// cannot bypass isProtectedSegments on Windows. 8.3 short names are intentionally NOT
// resolved (see the helper doc) — that is a documented limitation, not a bug here.
func TestNormalizeWindowsSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{".git", ".git"},
		{".git.", ".git"},
		{".git ", ".git"},
		{".git. ", ".git"},
		{".git::$INDEX_ALLOCATION", ".git"},
		{".github:extra", ".github"},
		{"config", "config"},
		{"file.txt", "file.txt"}, // interior dot preserved; only trailing stripped
		{"GITHUB~1", "GITHUB~1"}, // 8.3 alias intentionally left as-is
		{".env.local", ".env.local"},
	}
	for _, c := range cases {
		if got := normalizeWindowsSegment(c.in); got != c.want {
			t.Errorf("normalizeWindowsSegment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFlagsForReview exercises the non-blocking advisory check (epic 32.4 Task 6):
// a build-script path touch is flagged with a specific reason, using boundary-safe
// matching so lookalikes are not flagged. Executable-bit changes are intentionally
// NOT flagged — the apply pipeline forces mode 0644 (atomicfs) and the commit forces
// 100644, so an exec-bit change never lands (see FlagsForReview's doc and the AC5
// tech-debt resolution). FlagsForReview therefore evaluates the path only.
func TestFlagsForReview(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantFlagged bool
		wantReason  string
	}{
		// --- build-script path condition ---
		{"Makefile", "Makefile", true, "build-script path"},
		{"nested Makefile", "subdir/Makefile", true, "build-script path"},
		{"lowercase makefile", "makefile", true, "build-script path"},
		{"shell script", "deploy.sh", true, "build-script path"},
		{"nested shell script", "ci/scripts/build.sh", true, "build-script path"},
		{"package.json", "package.json", true, "build-script path"},
		{"nested package.json", "frontend/package.json", true, "build-script path"},
		{"Dockerfile", "Dockerfile", true, "build-script path"},
		{"Jenkinsfile", "Jenkinsfile", true, "build-script path"},
		{"gitlab CI outside github", ".gitlab-ci.yml", true, "build-script path"},
		{"circleci config", ".circleci/config.yml", true, "build-script path"},

		// --- boundary-safe negatives (near-misses) ---
		{"not-a-Makefile.txt", "not-a-Makefile.txt", false, ""},
		{"foo.shell not .sh", "foo.shell", false, ""},
		{"package.json.bak", "nested/package.json.bak", false, ""},
		{"mySh.go not shell", "mySh.go", false, ""},
		{"Dockerfile.md not Dockerfile", "Dockerfile.md", false, ""},
		{"plain source no flags", "cmd/atcr/main.go", false, ""},
		{"empty path", "", false, ""},

		// --- exec-bit is no longer a signal: mode is not consulted at all ---
		{"executable script path still flagged by path", "scripts/deploy.sh", true, "build-script path"},
		{"plain executable (non-build) not flagged", "scripts/tool", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flagged, reason := FlagsForReview(tt.path)
			if flagged != tt.wantFlagged {
				t.Fatalf("FlagsForReview(%q) flagged = %v, want %v (reason=%q)",
					tt.path, flagged, tt.wantFlagged, reason)
			}
			if !tt.wantFlagged {
				if reason != "" {
					t.Errorf("unflagged result must have empty reason, got %q", reason)
				}
				return
			}
			if !strings.Contains(reason, tt.wantReason) {
				t.Errorf("reason %q missing expected substring %q", reason, tt.wantReason)
			}
		})
	}
}

// TestFlagsForReview_NeverErrorsOnOddPaths is a defensive guard: the advisory check
// must never panic, even on unusual path values.
func TestFlagsForReview_NeverErrorsOnOddPaths(t *testing.T) {
	for _, p := range []string{"", ".", "..", "/", "a/../b", strings.Repeat("x/", 100) + "Makefile"} {
		_, _ = FlagsForReview(p)
	}
}
