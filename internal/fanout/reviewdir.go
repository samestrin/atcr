package fanout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/payload"
)

// reviewIDRe is the positive allowlist for a review id: it must be a single
// path component starting with an alphanumeric. This rejects in one rule every
// escape vector — ".", "..", "", a leading "-" (flag injection), "/" and "\"
// separators, and absolute paths — without the brittle ".." substring heuristic,
// which both over-rejected legitimate ids (release-1..2) and under-rejected ".".
var reviewIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// manifestFile is the per-review provenance file at the review-dir root.
const manifestFile = "manifest.json"

// reviewSubdirs are the directories `atcr review` scaffolds. The per-agent
// sources/pool/raw/agent/<name>/ tree is created later by the fan-out engine
// (WritePool); scaffolding creates only the top-level trio (AC 01-03 Note).
var reviewSubdirs = []string{"payload", "sources", "reconciled"}

// branchPrefixes are stripped from a branch before slugifying so a review id is
// derived from the meaningful tail (feature/JIRA-123 → JIRA-123).
var branchPrefixes = []string{"feature/", "fix/", "bugfix/", "hotfix/", "release/", "chore/"}

// ReviewID derives the review id. An explicit override wins verbatim after a
// path-traversal check; otherwise the id is "<date>_<slug>" where slug is the
// sanitized branch ("detached" for a detached HEAD / empty branch, "review" when
// the branch sanitizes to nothing). When exists reports a collision, the
// HHMMSS-style collisionSuffix is appended (AC 01-03 Edge Case 1). exists may be
// nil to skip the collision probe.
func ReviewID(override, branch, date, collisionSuffix string, exists func(id string) bool) (string, error) {
	if s := strings.TrimSpace(override); s != "" {
		if err := validateReviewID(s); err != nil {
			return "", err
		}
		return s, nil
	}
	slug := slugifyBranch(branch)
	switch {
	case strings.TrimSpace(branch) == "":
		slug = "detached"
	case slug == "":
		slug = "review"
	}
	id := date + "_" + slug
	// Defense-in-depth: validate the computed id, not just user overrides — a
	// degenerate date or slug must never yield an unsafe component.
	if err := validateReviewID(id); err != nil {
		return "", err
	}
	if exists != nil {
		id = resolveCollision(id, collisionSuffix, exists)
	}
	return id, nil
}

// collisionCandidate returns the nth candidate in the collision sequence: the
// base id for n==0, then id-suffix, then id-suffix-2, id-suffix-3, ...
func collisionCandidate(id, suffix string, n int) string {
	switch n {
	case 0:
		return id
	case 1:
		return id + "-" + suffix
	default:
		return fmt.Sprintf("%s-%s-%d", id, suffix, n)
	}
}

// resolveCollision returns the first non-colliding id, appending the suffix then
// an incrementing counter so two reviews of the same branch within the same
// second never scaffold into one another's directory. The loop is bounded by a
// generous cap to avoid spinning on a pathological exists predicate.
func resolveCollision(id, suffix string, exists func(string) bool) string {
	for n := 0; n < 10000; n++ {
		if candidate := collisionCandidate(id, suffix, n); !exists(candidate) {
			return candidate
		}
	}
	return collisionCandidate(id, suffix, 10000)
}

// validateReviewID rejects ids that could escape the reviews directory. The
// message is AC 01-03 Edge Case 4 verbatim.
func validateReviewID(id string) error {
	if !reviewIDRe.MatchString(id) {
		return fmt.Errorf("invalid review id: must not contain path separators or '..'")
	}
	return nil
}

// ValidateReviewID is the exported guard the CLI applies to a bare review-id
// anchor argument (so "..", "/...", or a leading dash can never resolve to a
// directory outside .atcr/reviews/).
func ValidateReviewID(id string) error { return validateReviewID(id) }

// slugifyBranch strips a known git-flow prefix then collapses every run of
// characters outside [A-Za-z0-9._-] into a single '-', preserving case and
// existing separators (feature/JIRA-123-add-auth → JIRA-123-add-auth). Leading
// and trailing '-' are trimmed.
func slugifyBranch(branch string) string {
	b := strings.TrimSpace(branch)
	for _, p := range branchPrefixes {
		if strings.HasPrefix(b, p) {
			b = b[len(p):]
			break
		}
	}
	var sb strings.Builder
	prevDash := false
	for _, r := range b {
		if isSlugChar(r) {
			sb.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			sb.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(sb.String(), "-")
	// A slug that is only dots ("." / "..") would form an unsafe component; treat
	// it as empty so the caller falls back to "review".
	if strings.Trim(slug, ".") == "" {
		return ""
	}
	return slug
}

func isSlugChar(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	default:
		return false
	}
}

// ReviewsRoot returns .atcr/reviews under root.
func ReviewsRoot(root string) string {
	return filepath.Join(root, ".atcr", "reviews")
}

// ReviewExists reports whether a review directory with id already exists under
// root. It is an advisory probe only — derived-id collision handling claims the
// directory atomically via claimReviewDir rather than relying on this check.
func ReviewExists(root, id string) bool {
	_, err := os.Stat(filepath.Join(ReviewsRoot(root), id))
	return err == nil
}

// claimReviewDir atomically claims a review directory for a derived id: the
// directory creation itself (os.Mkdir, which fails on an existing dir) is the
// claim, so two reviews racing the same id can never scaffold into one
// another's directory — the loser sees EEXIST and retries with the next
// collision candidate. This replaces the Stat-probe-then-MkdirAll sequence,
// whose check/use window let concurrent runs interleave writes in one dir.
// Returns the claimed id and its review-dir path.
func claimReviewDir(root, id, suffix string) (string, string, error) {
	if err := os.MkdirAll(ReviewsRoot(root), 0o755); err != nil {
		return "", "", fmt.Errorf("failed to create review directory: %w", err)
	}
	for n := 0; n < 10000; n++ {
		candidate := collisionCandidate(id, suffix, n)
		dir := filepath.Join(ReviewsRoot(root), candidate)
		err := os.Mkdir(dir, 0o755)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", "", fmt.Errorf("failed to create review directory: %w", err)
		}
		for _, sub := range reviewSubdirs {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
				return "", "", fmt.Errorf("failed to create review directory: %w", err)
			}
		}
		return candidate, dir, nil
	}
	return "", "", fmt.Errorf("failed to create review directory: too many collisions for id %q", id)
}

// ReviewDirExistsError is returned by ScaffoldReviewDir when an explicit id's
// review directory already exists. Its Error() names the CLI recovery flags
// (--resume/--force) for the common CLI path; non-CLI callers (the MCP handler)
// detect it with errors.As and substitute a flag-neutral message, since MCP
// clients have no such flags.
type ReviewDirExistsError struct{ ID string }

func (e *ReviewDirExistsError) Error() string {
	return fmt.Sprintf("review %s already exists; use --resume %s to continue it or --force to overwrite", e.ID, e.ID)
}

// ScaffoldReviewDir creates .atcr/reviews/<id>/ and its top-level subdirs (0755),
// returning the review-dir path. Parent directories are created as needed
// (AC 01-03 Edge Case 3). A creation failure carries the AC 01-03 message.
// Creation is exclusive: an id whose review directory already exists is
// rejected, so a retried explicit override (e.g. an MCP client re-sending
// atcr_review while the first run shows running) can never launch a second
// fan-out into a directory another run is writing. Derived ids go through
// claimReviewDir instead, which makes creation the atomic collision claim.
func ScaffoldReviewDir(root, id string) (string, error) {
	if err := os.MkdirAll(ReviewsRoot(root), 0o755); err != nil {
		return "", fmt.Errorf("failed to create review directory: %w", err)
	}
	dir := filepath.Join(ReviewsRoot(root), id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", &ReviewDirExistsError{ID: id}
		}
		return "", fmt.Errorf("failed to create review directory: %w", err)
	}
	for _, sub := range reviewSubdirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", fmt.Errorf("failed to create review directory: %w", err)
		}
	}
	return dir, nil
}

// ScaffoldOutputDir creates the review tree at an explicit --output-dir path
// (used verbatim — it is the orchestrator's own output target, not under
// .atcr/reviews/). Parent directories are created as needed; the path may be
// non-existent or an empty directory. It returns the path so callers mirror the
// ScaffoldReviewDir signature.
//
// Trust boundary: arbitrary absolute paths — including paths outside the repo
// root — are accepted by design. atcr is a developer tool and --output-dir is
// intended for external orchestrators that own their output location. Callers
// are responsible for supplying trusted, user-controlled paths; paths inside
// ReviewsRoot are rejected by PrepareReview to avoid confusing half-state.
func ScaffoldOutputDir(dir string) (string, error) {
	// Reject symlinks up front: os.Lstat does not follow the link, so a dangling
	// symlink (whose target is absent) is caught here before ReadDir would see
	// ErrNotExist and silently fall through to MkdirAll.
	if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("output directory %q is a symlink: refusing to scaffold into a symlink target", dir)
	}
	// Create all parent components, then claim the leaf atomically with os.Mkdir
	// (not MkdirAll) to match the codebase's atomic-claim discipline: the syscall
	// either succeeds (exclusive create — definitionally empty) or returns
	// ErrExist, eliminating the check/use window in the old ReadDir+MkdirAll
	// sequence where two concurrent callers could both pass the empty check.
	//
	// Concurrency contract: this differs from ScaffoldReviewDir, which treats
	// ErrExist as a hard error. ScaffoldOutputDir intentionally accepts an
	// empty pre-existing directory because --output-dir is for external
	// orchestrators that may pre-create their output path. Callers are expected
	// to use unique paths per run; two concurrent callers on the same
	// pre-existing empty path are not protected against each other.
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("failed to create review directory: %w", err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("failed to create review directory: %w", err)
		}
		// Leaf exists: verify it is empty before writing into it. os.ReadDir
		// surfaces every entry (hidden files included).
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return "", fmt.Errorf("failed to create review directory: %w", readErr)
		}
		if len(entries) > 0 {
			// Name only the leaf, not the full resolved path: the collision error is
			// surfaced to MCP clients that never see the server's filesystem layout,
			// and the caller already supplied the path so the basename is enough to
			// identify the target without leaking the parent directory structure.
			return "", fmt.Errorf("output directory %q already exists and is not empty; use --force to overwrite (or point --output-dir at a new or empty path)", filepath.Base(dir))
		}
	}
	for _, sub := range reviewSubdirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", fmt.Errorf("failed to create review directory: %w", err)
		}
	}
	return dir, nil
}

// backupExisting moves path aside to path+".bak" so a --force re-run preserves
// the prior review tree instead of destroying it, leaving the path vacant for a
// fresh scaffold. --force keeps exactly one generation of backup —
// garbage-collecting older state is the user's responsibility (Epic 4.7: no
// automatic .bak GC). Returns the backup path.
//
// Crash-safe swap (Epic 4.7.1): the prior path+".bak" is renamed aside to
// path+".bak.old" (not destroyed) before path is renamed onto path+".bak"; the
// old generation is removed only after the swap succeeds, and is restored on any
// failure — so an interrupted swap never leaves the user with neither the new
// backup nor the prior one. When the path→.bak rename crosses a filesystem
// boundary (EXDEV — possible when path is itself a mountpoint, so its parent-dir
// sibling .bak is on a different mount), it falls back to copy-into-staging +
// same-fs rename-swap + RemoveAll(path), replicating the move's vacate-path
// postcondition. Stale atcr-owned staging artifacts (.bak.old/.bak.new) from a
// prior crashed run are reconciled away at entry, so retries start clean and the
// one-generation contract holds across a crash-then-retry sequence.
func backupExisting(path string) (string, error) {
	backup := path + ".bak"
	backupOld := path + ".bak.old"
	backupNew := path + ".bak.new"

	// Reconcile stragglers a prior crashed swap may have left, so a retry starts
	// from a clean slate and no atcr-owned staging artifact accumulates.
	// .bak.tmp-* sibling names are produced only by atomicfs.BackupToDotBak, not
	// by backupExisting — only .bak.old and .bak.new need clearing here.
	if err := os.RemoveAll(backupOld); err != nil {
		return "", fmt.Errorf("clearing stale staging backup %q: %w", backupOld, err)
	}
	if err := os.RemoveAll(backupNew); err != nil {
		return "", fmt.Errorf("clearing stale staging backup %q: %w", backupNew, err)
	}

	// Stage the prior generation aside instead of destroying it: a failed swap
	// below restores it from .bak.old.
	priorStaged := false
	if _, err := os.Lstat(backup); err == nil {
		if err := os.Rename(backup, backupOld); err != nil {
			return "", fmt.Errorf("staging prior backup %q aside: %w", backup, err)
		}
		priorStaged = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("checking prior backup %q: %w", backup, err)
	}

	// Swap the live tree into place as the new backup.
	if err := renameFn(path, backup); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			// path is on a different filesystem from its .bak sibling (path is a
			// mountpoint): replicate the move with a same-fs copy + vacate.
			if cerr := backupCrossDevice(path, backup, backupNew); cerr != nil {
				restorePriorBackup(priorStaged, backupOld, backup)
				return "", cerr
			}
		} else {
			restorePriorBackup(priorStaged, backupOld, backup)
			return "", fmt.Errorf("backing up %q: %w", path, err)
		}
	}

	// Swap succeeded — the prior generation staged in .bak.old is now superseded.
	if priorStaged {
		if err := os.RemoveAll(backupOld); err != nil {
			return "", fmt.Errorf("removing superseded backup %q: %w", backupOld, err)
		}
	}
	return backup, nil
}

// renameFn is the swap primitive backupExisting uses, indirected through a
// package var so fault-injection tests can drive the failed-swap and
// cross-filesystem (EXDEV) branches deterministically — the EXDEV branch cannot
// be reached by real-fs tricks without a cross-mount in CI. In production it is
// os.Rename.
var renameFn = os.Rename

// copyPathFn is the copy primitive backupCrossDevice uses, indirected so tests
// can inject copy failures without a real cross-mount in CI.
var copyPathFn = atomicfs.CopyPath

// removePathFn is the vacate primitive backupCrossDevice uses for the final
// os.RemoveAll(path) step, indirected so tests can inject vacate failures.
var removePathFn = os.RemoveAll

// restorePriorBackup moves the staged prior generation (.bak.old) back to .bak
// after a failed swap, so a failure leaves the user with the prior backup intact.
// Best-effort: a restore failure cannot un-fail the swap, but the prior data
// still survives under .bak.old for manual recovery, so the error is not
// propagated over the swap failure that is the caller's real concern.
func restorePriorBackup(staged bool, backupOld, backup string) {
	if !staged {
		return
	}
	_ = os.Rename(backupOld, backup)
}

// backupCrossDevice replicates backupExisting's move across a filesystem boundary
// (hit when path is a mountpoint, so os.Rename(path, backup) returns EXDEV): it
// copies path's tree into a fresh same-fs staging sibling (backupNew, next to
// backup on the parent filesystem), renames that over backup as an atomic same-fs
// swap, then removes path to leave it vacant — matching os.Rename(path, backup)'s
// postcondition. The prior .bak (if any) has already been staged to .bak.old by
// the caller, so backup is absent here; a failure leaves backupNew for the
// entry-time reconcile to clean on the next run.
func backupCrossDevice(path, backup, backupNew string) error {
	if err := os.RemoveAll(backupNew); err != nil {
		return fmt.Errorf("clearing staging backup %q: %w", backupNew, err)
	}
	// CopyPath skips symlinks and non-regular entries — this fallback is lossy
	// if the live tree contains symlinks. Review trees hold only regular files
	// (WritePool never creates symlinks), so the divergence is immaterial for
	// managed reviews; --output-dir callers with symlinks in their tree will
	// silently lose them on this path.
	if err := copyPathFn(path, backupNew); err != nil {
		return fmt.Errorf("backing up %q across filesystems: %w", path, err)
	}
	if err := os.Rename(backupNew, backup); err != nil {
		return fmt.Errorf("swapping staged backup %q into place: %w", backupNew, err)
	}
	if err := removePathFn(path); err != nil {
		return fmt.Errorf("vacating %q after cross-device backup: %w", path, err)
	}
	return nil
}

// forceBackupReviewDir backs up an existing managed review directory for id
// before --force scaffolds a fresh one (Epic 4.7 AC2). A non-existent directory
// is a no-op, so --force is harmless when there is nothing to overwrite. Returns
// the backup path when a backup was created, or "" when there was nothing to
// back up.
func forceBackupReviewDir(root, id string) (string, error) {
	dir := filepath.Join(ReviewsRoot(root), id)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("checking review directory before --force backup: %w", err)
	}
	return backupExisting(dir)
}

// forceBackupOutputDir backs up a non-empty --output-dir before --force scaffolds
// into it (Epic 4.7 AC2). An absent or empty target is a no-op: ScaffoldOutputDir
// already accepts those, so there is nothing to preserve. Returns the backup path
// when a backup was created, or "" when there was nothing to back up.
func forceBackupOutputDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("checking output directory before --force backup: %w", err)
	}
	if len(entries) == 0 {
		return "", nil
	}
	// backupExisting ultimately replaces (and, on success, removes) the prior
	// <dir>.bak generation — and its crash-safe staging also RemoveAll()s any
	// <dir>.bak.old/<dir>.bak.new sibling at entry (Epic 4.7.1). Inside the managed
	// reviews tree those siblings are atcr-owned, but an arbitrary --output-dir may
	// have an unrelated sibling .bak the user owns. Refuse rather than destroy a
	// backup atcr did not create (Epic 4.7: never silently delete user data).
	if err := guardForeignBackup(dir + ".bak"); err != nil {
		return "", err
	}
	return backupExisting(dir)
}

// guardForeignBackup returns an error if backup exists but was not created by
// atcr, so --force on an unmanaged --output-dir cannot silently destroy it. A
// non-existent or empty backup, or one carrying the scaffolded review-tree
// markers (a genuine prior atcr backup), is allowed through to be replaced.
func guardForeignBackup(backup string) error {
	fi, err := os.Lstat(backup)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking backup path %q: %w", backup, err)
	}
	if fi.Mode().IsRegular() {
		return fmt.Errorf("refusing --force: %q is a regular file, not a directory; move or remove it first", backup)
	}
	entries, err := os.ReadDir(backup)
	if err != nil {
		return fmt.Errorf("refusing --force: %q exists and was not created by atcr; move or remove it first", backup)
	}
	if len(entries) == 0 || looksLikeReviewTree(backup) {
		return nil
	}
	return fmt.Errorf("refusing --force: %q already exists and does not look like an atcr backup; move or remove it first", backup)
}

// looksLikeReviewTree reports whether dir carries the atcr provenance markers
// that distinguish an atcr-created tree (or a prior atcr backup) from arbitrary
// user data: every scaffolded review subdirectory AND a manifest.json at the
// root. The subdir names alone are too weak a signal — any directory containing
// payload/, sources/, and reconciled/ would qualify and be eligible for
// destruction by --force. manifest.json is written by every scaffolded review
// (WriteManifest), so a genuine backup always has it while a coincidental
// structural lookalike of user data does not.
func looksLikeReviewTree(dir string) bool {
	for _, sub := range reviewSubdirs {
		fi, err := os.Stat(filepath.Join(dir, sub))
		if err != nil || !fi.IsDir() {
			return false
		}
	}
	fi, err := os.Stat(filepath.Join(dir, manifestFile))
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	return true
}

// validateOutputDirRoot returns an error if dir is inside ReviewsRoot(root).
// An --output-dir that resolves into the managed reviews location creates a
// half-state: the review tree is written but WriteLatest is skipped, making
// the review invisible to atcr status while colocated with tracked reviews.
func validateOutputDirRoot(dir, root string) error {
	reviewsRoot := ReviewsRoot(root)
	rel, err := filepath.Rel(reviewsRoot, dir)
	if err != nil {
		return nil // cannot determine relationship on this OS/path — allow
	}
	if !strings.HasPrefix(rel, "..") {
		return fmt.Errorf("--output-dir %q is inside the managed reviews directory %q: use a path outside .atcr/reviews/, or omit --output-dir to use a managed review", dir, reviewsRoot)
	}
	return nil
}

// WriteLatest writes the review id (one line) to .atcr/latest so later commands
// default to it. The .atcr directory is created if absent.
func WriteLatest(root, id string) error {
	dir := filepath.Join(root, ".atcr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating .atcr dir: %w", err)
	}
	return atomicWriteFile(filepath.Join(dir, "latest"), []byte(id+"\n"))
}

// ReadLatest reads and validates the review id recorded in .atcr/latest. An
// empty or malformed pointer is an error rather than a silent "" that would
// resolve to the reviews root downstream.
func ReadLatest(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".atcr", "latest"))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", errors.New("empty .atcr/latest pointer: run 'atcr review' first")
	}
	if err := validateReviewID(id); err != nil {
		return "", fmt.Errorf(".atcr/latest: %w", err)
	}
	return id, nil
}

// WriteManifest writes m into <reviewDir>/manifest.json, centralizing the
// provenance-file path. It delegates the atomic encode to payload.WriteManifest.
func WriteManifest(reviewDir string, m *payload.Manifest) error {
	return payload.WriteManifest(filepath.Join(reviewDir, manifestFile), m)
}

// ReadManifestPartial reads a review's partial flag, treating
// sources/pool/summary.json (the completion signal, and the same source of
// truth ReadReviewStatus uses) as authoritative and falling back to
// manifest.json when no readable summary exists (fan-out still running, or a
// hand-assembled review). Reading the summary first means a WriteManifest
// failure after WritePool can never report partial:false for a partial run.
// EffectivePartial() applies FailureMarker awareness so any WritePool-aborted
// run with roster agents (Total>0) is always treated as partial, regardless of
// Succeeded/Failed counts — a timed-out agent may have flushed findings before
// the fault and still appears as Failed in the summary.
//
// Precondition: callers operating on a fan-out-managed review MUST first call
// EnsureReviewComplete, which validates both files and rejects in-progress or
// corrupt reviews. This function deliberately swallows every read and parse
// error — unreadable files, oversized summaries, corrupt JSON, and
// semantically empty records all collapse to a manifest fallback or false —
// and relies on EnsureReviewComplete to have already ensured the files are
// valid. Calling it on an unvalidated directory silently returns false for any
// read failure, including a corrupt summary that would block ReadReviewStatus.
//
// It is the single best-effort reader shared by the CLI reconcile path and the
// MCP reconcile handler so the two never drift; when neither artifact is
// readable it defaults to false.
func ReadManifestPartial(reviewDir string) bool {
	if sf, err := os.Open(filepath.Join(reviewDir, "sources", "pool", summaryFile)); err == nil {
		raw, readErr := io.ReadAll(io.LimitReader(sf, maxSummaryBytes+1))
		_ = sf.Close()
		if readErr == nil && int64(len(raw)) <= maxSummaryBytes {
			var ps PoolSummary
			if json.Unmarshal(raw, &ps) == nil {
				// Sanity-check the decoded record before trusting it. A zero-value
				// PoolSummary (from {} or null) has Total=0; a corrupt-but-parseable
				// record may violate Total==Succeeded+Failed. Either case falls through
				// to the manifest rather than silently returning partial:false.
				if ps.Total > 0 && ps.Total == ps.Succeeded+ps.Failed {
					return ps.EffectivePartial()
				}
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(reviewDir, manifestFile))
	if err != nil {
		return false
	}
	var m payload.Manifest
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	return m.Partial
}
