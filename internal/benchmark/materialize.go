package benchmark

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/gitexec"
)

// MaterializedCase is a repo-state case turned into a real git repository: the
// base tree as one commit, the change applied on top as a second commit carrying
// the case's commit message verbatim.
//
// It is a REPOSITORY and not just a working tree because that is what the review
// path this tier measures requires. fanout.PrepareReviewFromDiff — the ingestion
// entry standard-v1 uses — builds no RangeBuilder, and both the claim ledger
// (epic 35.16.7) and context-aware pre-fetching (epic 35.16.8) live there. A tier
// whose purpose is measuring those two features cannot run on the path that omits
// them, so the case has to present a real base..head range.
type MaterializedCase struct {
	// Root is the repository working tree, in the HEAD state. Expected findings'
	// line numbers are head-state, so this is the tree their line numbers index.
	Root string
	// BaseSHA and HeadSHA bound the review range. Both are deterministic for a
	// given case — see the fixed identity and dates in commitAll.
	BaseSHA string
	HeadSHA string
}

// benchmarkCommitIdentity is the fixed author/committer every materialized case
// commits under. Real identities are not available (GIT_CONFIG_GLOBAL is
// /dev/null under gitexec hardening, so there is no user.name to inherit) and are
// not wanted: a host-dependent identity would change the commit SHA per machine.
const (
	benchmarkCommitName  = "atcr benchmark"
	benchmarkCommitEmail = "benchmark@atcr.invalid"
	// benchmarkCommitDate is fixed so two materializations of one case produce
	// byte-identical commits. A wall-clock date would make every run a different
	// repository, which defeats the run-to-run comparability the whole suite exists
	// for — the same reason executeBenchmarkRun injects generatedAt rather than
	// calling time.Now.
	benchmarkCommitDate = "2026-01-01T00:00:00+00:00"
)

// MaterializeCase builds c's repository under dest and returns the range the
// review is computed over.
//
// dest must be an existing empty directory; the caller owns its lifetime, exactly
// as executeBenchmarkRun owns the per-run temp directory it already creates.
// Both halves are VERIFIED, not assumed: a stray file in dest would be swept
// into the base commit by git add -A (base tree, range and working tree then
// disagree), and a missing dest would be silently created by the copy.
func MaterializeCase(ctx context.Context, c RepoStateCase, dest string) (*MaterializedCase, error) {
	if entries, err := os.ReadDir(dest); err != nil {
		return nil, fmt.Errorf("case %q: dest %s must be an existing directory: %w", c.ID, dest, err)
	} else if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return nil, fmt.Errorf("case %q: dest %s must be empty; found %d entr(y|ies): %s",
			c.ID, dest, len(entries), strings.Join(names, ", "))
	}
	files, err := copyBaseTree(filepath.Join(c.Dir, c.BaseTree), dest)
	if err != nil {
		return nil, fmt.Errorf("case %q base tree: %w", c.ID, err)
	}
	// A base tree with no files is always a broken case, and git reports it as
	// "nothing to commit", which names neither the case nor the reason. This tier
	// exists to plant defects reachable through UNCHANGED code; an empty base tree
	// has no unchanged code, so the case could not carry one.
	if files == 0 {
		return nil, fmt.Errorf("case %q base tree %q is empty; a repo-state case plants defects reachable through unchanged code, so it needs a tree to reach", c.ID, c.BaseTree)
	}
	if err := runGit(ctx, dest, "init", "-q"); err != nil {
		return nil, fmt.Errorf("case %q: %w", c.ID, err)
	}
	baseSHA, err := commitAll(ctx, dest, []string{"-m", "base"})
	if err != nil {
		return nil, fmt.Errorf("case %q base commit: %w", c.ID, err)
	}

	// ABSOLUTE, because every git call here runs with `-C dest`. A relative case
	// path is resolved against git's working directory, not the caller's, so a
	// suite loaded from a relative --suite-path would send git looking for the patch
	// underneath the materialized tree. The same applies to the message file below.
	diffPath, err := filepath.Abs(filepath.Join(c.Dir, c.Diff))
	if err != nil {
		return nil, fmt.Errorf("case %q: resolving %s: %w", c.ID, c.Diff, err)
	}
	// --whitespace=nowarn: an authored case's diff may legitimately carry trailing
	// whitespace (a hand-written tree is not required to be lint-clean), and git
	// apply's default warning is noise on a path that is not reviewing whitespace.
	// It does NOT relax what applies — a diff that does not apply still fails, which
	// is the point of the check below.
	if err := runGit(ctx, dest, "apply", "--whitespace=nowarn", diffPath); err != nil {
		// Fail LOUDLY. A diff that silently did not apply leaves the head tree equal
		// to the base tree, so every reviewer scores zero and the case reads as
		// extremely hard rather than broken — the precise silent failure this tier
		// exists to detect in others.
		return nil, fmt.Errorf("case %q: applying %s: %w", c.ID, c.Diff, err)
	}

	// --cleanup=verbatim, because commit-message.txt is the case's CLAIM SOURCE.
	// git's default cleanup strips comment lines and trailing blank lines; FORMAT.md
	// states the file is used verbatim "including trailing whitespace and trailers",
	// so a case exercising trailer handling would otherwise be measured against a
	// message its author never wrote.
	msgPath, err := filepath.Abs(filepath.Join(c.Dir, c.CommitMessage))
	if err != nil {
		return nil, fmt.Errorf("case %q: resolving %s: %w", c.ID, c.CommitMessage, err)
	}
	headSHA, err := commitAll(ctx, dest, []string{"-F", msgPath, "--cleanup=verbatim"})
	if err != nil {
		return nil, fmt.Errorf("case %q head commit: %w", c.ID, err)
	}
	return &MaterializedCase{Root: dest, BaseSHA: baseSHA, HeadSHA: headSHA}, nil
}

// copyBaseTree copies src into dst, refusing anything that would make the
// materialized repository depend on state outside the case directory.
//
// Three refusals, each closing a hole the loader's declared-path check cannot see
// because it inspects strings and this inspects the filesystem:
//
//   - a SYMLINK anywhere in the tree, which is a path escape written on disk
//     rather than in case.json (AC7);
//   - a nested .git at ANY depth, not only the top level the loader checks —
//     FORMAT.md's "the loader creates the repository" holds for subdirectories too;
//   - any entry that is neither a regular file nor a directory (device, socket,
//     fifo), which has no meaning in a source tree and cannot be reviewed.
//
// It returns the number of regular files copied, so the caller can reject an empty
// tree with a diagnostic that names the case.
func copyBaseTree(src, dst string) (int, error) {
	files := 0
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		// The relative path is rebuilt from a real walk, so it cannot contain "..",
		// but checking it keeps the guard aligned with the loader's posture rather
		// than resting on an invariant of WalkDir that a future refactor could break.
		if !isSafeRelPath(rel) {
			return fmt.Errorf("entry %q is not within the base tree", rel)
		}
		if d.Name() == ".git" {
			return fmt.Errorf("base tree contains %q; the case supplies only the working tree and the loader creates the repository", rel)
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			return fmt.Errorf("base tree entry %q is a symlink; a case must not reference a path outside its own directory", rel)
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			files++
			return copyRegularFile(p, target)
		default:
			return fmt.Errorf("base tree entry %q is neither a regular file nor a directory", rel)
		}
	})
	return files, err
}

// copyRegularFile copies one file, preserving only whether it is executable.
// Nothing else about the source mode is carried: a case's value is its content,
// and reproducing an author's umask would make the materialized tree — and so the
// commit SHA — depend on the machine that checked the suite out.
func copyRegularFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi.Mode()&0o111 != 0 {
		mode = 0o755
	}
	return os.WriteFile(dst, data, mode)
}

// commitAll stages everything and commits it under the fixed benchmark identity
// and date, returning the new commit's SHA.
//
// The identity and dates are passed as `-c` overrides and environment rather than
// written into .git/config: gitexec neutralizes system and global config, so there
// is no ambient identity to inherit, and a per-repo config write would be one more
// thing for a future materialization to forget.
func commitAll(ctx context.Context, root string, commitArgs []string) (string, error) {
	if err := runGit(ctx, root, "add", "-A"); err != nil {
		return "", err
	}
	args := append([]string{
		"-c", "user.name=" + benchmarkCommitName,
		"-c", "user.email=" + benchmarkCommitEmail,
		"commit", "-q", "--no-verify",
	}, commitArgs...)
	cmd := gitCmd(ctx, root, args...)
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_DATE="+benchmarkCommitDate,
		"GIT_COMMITTER_DATE="+benchmarkCommitDate,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(string(out)))
	}
	sha, err := gitCmd(ctx, root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(sha)), nil
}

// gitCmd builds a hardened git command rooted at root. Every git subprocess in
// this package goes through gitexec — the package invariant that no bare
// exec.Command("git", ...) exists outside it.
func gitCmd(ctx context.Context, root string, args ...string) *exec.Cmd {
	return gitexec.CommandContextFn(ctx, append([]string{"-C", root}, args...)...)
}

// runGit runs a git command for its effect, folding git's own stderr into the
// error: "exit status 1" alone tells an author nothing about why their diff did
// not apply.
func runGit(ctx context.Context, root string, args ...string) error {
	out, err := gitCmd(ctx, root, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}
