package benchmark

import (
	"context"
	"fmt"
	"io"
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
	files, err := copyBaseTree(ctx, filepath.Join(c.Dir, c.BaseTree), dest)
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
	baseSHA, err := commitAll(ctx, dest, files, []string{"-m", "base"})
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
	// `--` before diffPath: the path comes from a case directory, and even though
	// filepath.Abs currently guarantees a leading separator, `--` is the one
	// argument that makes a dash-leading operand impossible to mistake for an
	// option. Defense in depth on the one place a case-controlled string reaches
	// a git argv position.
	if err := runGit(ctx, dest, "apply", "--whitespace=nowarn", "--", diffPath); err != nil {
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
	headSHA, err := commitAll(ctx, dest, -1, []string{"-F", msgPath, "--cleanup=verbatim"})
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
//
// The walk honors ctx: a cancelled run aborts at the next entry instead of
// materializing a tree nobody is waiting for, and every regular file is bounded
// (per file and per tree) so an adversarial or generated base tree cannot OOM
// the process or fill the disk. The per-file cap mirrors MaxDiffBytes (the
// standard-v1 diff cap) — same class of input, same bound.
const (
	maxBaseFileBytes = 10 * 1024 * 1024 // mirrors MaxDiffBytes
	maxBaseTreeBytes = 10 * maxBaseFileBytes
)

func copyBaseTree(ctx context.Context, src, dst string) (int, error) {
	files := 0
	var totalBytes int64
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		// The symlink refusal comes FIRST, before the root's early return: a root
		// that is itself a symlink is a path escape too, and WalkDir (which Lstats
		// the root and never descends into it) would otherwise end the walk with
		// zero files — surfacing as the misleading "empty" error instead of naming
		// the escape.
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("base tree entry %q is a symlink; a case must not reference a path outside its own directory", rel)
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
		// The git-CONSUMED metadata files are refused alongside .git: a vendored
		// .gitignore makes `git add -A` silently drop the files it matches (the
		// working tree and the commit then disagree), a .gitattributes with e.g.
		// `* text=auto` renormalizes content on add (breaking the fixed-SHA
		// determinism contract), and a .gitmodules names submodules the tree does
		// not contain. Names git does NOT consume — .github/, .gitkeep — are
		// ordinary content and stay legal.
		switch d.Name() {
		case ".gitignore", ".gitattributes", ".gitmodules":
			return fmt.Errorf("base tree contains %q; git's add/commit machinery would consume it and make the materialized repository depend on case-authored ignore/attribute state", rel)
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			if info.Size() > maxBaseFileBytes {
				return fmt.Errorf("base tree entry %q is %d bytes; a single base file may not exceed %d", rel, info.Size(), maxBaseFileBytes)
			}
			totalBytes += info.Size()
			if totalBytes > maxBaseTreeBytes {
				return fmt.Errorf("base tree exceeds %d bytes in total; a case's base tree may not exceed %d", totalBytes, maxBaseTreeBytes)
			}
			files++
			return copyRegularFile(d, p, target)
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
//
// The mode comes from the walk's own DirEntry (one syscall, no second TOCTOU
// window after the walk's lstat) and the content is STREAMED rather than slurped:
// a base file's size is bounded by the caller, but the copy must not hold an
// unbounded buffer in memory regardless.
func copyRegularFile(d fs.DirEntry, src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	fi, err := d.Info()
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi.Mode()&0o111 != 0 {
		mode = 0o755
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	// Best-effort: the copy has already finished (or failed) by the time this
	// deferred close runs, and a read-only close error carries no information the
	// caller could act on — silencing it explicitly rather than by omission.
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// commitAll stages everything and commits it under the fixed benchmark identity
// and date, returning the new commit's SHA.
//
// The identity and dates are passed as `-c` overrides and environment rather than
// written into .git/config: gitexec neutralizes system and global config, so there
// is no ambient identity to inherit, and a per-repo config write would be one more
// thing for a future materialization to forget.
//
// The add pins core.excludesFile and core.attributesFile to /dev/null: git's
// default excludes file ($XDG_CONFIG_HOME/git/ignore) is read independently of
// any config GIT_CONFIG_GLOBAL=/dev/null might neutralize, so without the pin a
// host's personal ignore file silently drops matching base-tree files from the
// commit — making the tree, the range and both SHAs host-dependent. With the pin
// in place, expectedFiles (>= 0) asserts the staged set is exactly what
// copyBaseTree placed: a mismatch means something ELSE filtered the add, and the
// error names the case. A negative expectedFiles skips the assertion — the head
// commit's staged set is the applied diff's business, which the apply and range
// tests cover.
func commitAll(ctx context.Context, root string, expectedFiles int, commitArgs []string) (string, error) {
	if err := runGit(ctx, root, "-c", "core.excludesFile=/dev/null", "-c", "core.attributesFile=/dev/null", "add", "-A"); err != nil {
		return "", err
	}
	if expectedFiles >= 0 {
		staged, err := gitCmd(ctx, root, "diff", "--cached", "--name-only").Output()
		if err != nil {
			return "", fmt.Errorf("listing staged files: %w", err)
		}
		out := string(staged)
		count := strings.Count(out, "\n")
		if out != "" && !strings.HasSuffix(out, "\n") {
			count++
		}
		if count != expectedFiles {
			return "", fmt.Errorf("staged %d file(s) but the base tree copied %d; something filtered the add (host ignore/attribute state?)", count, expectedFiles)
		}
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
		return "", fmt.Errorf("git commit: %w: %s", err, truncateGitOutput(strings.TrimSpace(string(out))))
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

// maxGitErrorOutput bounds how much of git's combined output is folded into an
// error. A near-miss apply can emit a long warning chain; the first lines carry
// the diagnosis and the tail is noise that lands in every operator's stderr.
const maxGitErrorOutput = 2 * 1024

// truncateGitOutput bounds s to maxGitErrorOutput with a marker naming what was
// cut, so a truncated error is visibly truncated rather than silently amputated.
func truncateGitOutput(s string) string {
	if len(s) <= maxGitErrorOutput {
		return s
	}
	return s[:maxGitErrorOutput] + fmt.Sprintf("... [%d more bytes truncated]", len(s)-maxGitErrorOutput)
}

// runGit runs a git command for its effect, folding git's own stderr into the
// error: "exit status 1" alone tells an author nothing about why their diff did
// not apply. The folded output is truncated — see maxGitErrorOutput.
func runGit(ctx context.Context, root string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("git: no subcommand given")
	}
	out, err := gitCmd(ctx, root, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, truncateGitOutput(strings.TrimSpace(string(out))))
	}
	return nil
}
