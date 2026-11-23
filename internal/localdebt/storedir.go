package localdebt

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/validation"
)

// StoreDir is the ONE resolver for where the local debt store of the repository
// at root lives (Epic 92 T4). Debt commands (below an explicit --store, which
// the CLI applies first), PersistForReconcile, the quality-signal payload and
// the next-step suggestions all call it, so a configured store cannot be read by
// one path and missed by another.
//
// Precedence: project .atcr/config.yaml debt.store > user
// ~/.config/atcr/registry.yaml debt.store > DefaultDir(root). With neither set
// the answer is DefaultDir(root), byte for byte the behavior before the key
// existed.
//
// The two tiers are trusted differently. The project file is committed, so
// anyone who can land a commit in the repository writes it; a value there must
// be a clean repo-relative path with no "..", and must still be inside the
// repository once symlinks are followed. Only the user's own file (and --store)
// may name a store outside the repository, such as a sibling private planning
// repo: a relative user value resolves against root, so "../planning/debt" is
// that sibling.
//
// An invalid or unreadable value is an error, never a fall-through to the next
// tier: acting on the next tier would turn a visible mistake into a silent
// write to a different store (the rule ResolveStoreRoot follows too). A blank
// value is unset.
func StoreDir(root string) (string, error) {
	proj, err := registry.LoadProjectDebtStore(root)
	if err != nil {
		return "", err
	}
	if v := strings.TrimSpace(proj); v != "" {
		return projectStoreDir(root, v)
	}
	user, err := registry.LoadUserDebtStore()
	if err != nil {
		return "", err
	}
	if v := strings.TrimSpace(user); v != "" {
		return userStoreDir(root, v)
	}
	return DefaultDir(root), nil
}

// projectStoreDir applies the project tier's trust rule to v. Error messages
// quote only v (repo-relative by construction), never the absolute root.
func projectStoreDir(root, v string) (string, error) {
	slashed := filepath.ToSlash(v)
	reject := func(why string) (string, error) {
		return "", fmt.Errorf(".atcr/config.yaml debt.store %q: %s; a store outside the repository can be set only in ~/.config/atcr/registry.yaml or with --store", v, why)
	}
	if filepath.IsAbs(v) || filepath.VolumeName(v) != "" || strings.HasPrefix(slashed, "/") {
		return reject("must be a path relative to the repository root")
	}
	if strings.HasPrefix(slashed, "~") {
		return reject(`must be a path relative to the repository root ("~" is not expanded here)`)
	}
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".." {
			return reject(`must not contain ".."`)
		}
	}
	if path.Clean(slashed) != slashed {
		return reject(`must be a clean path (no "./", "//" or trailing "/")`)
	}
	if slashed == "." {
		return reject("must name a directory inside the repository, not the repository itself")
	}
	dir := filepath.Join(root, filepath.FromSlash(slashed))
	if err := containedIn(root, dir); err != nil {
		return reject(err.Error())
	}
	projectStores.Store(filepath.Clean(dir), root)
	return dir, nil
}

// projectStores maps each directory projectStoreDir has handed out (cleaned) to
// the repository root it must stay inside, so withLock can re-check containment
// at write time without every caller passing the root along.
var projectStores sync.Map

// afterContainmentCheck runs between withLock's pre-lock containment check and
// taking the lock. It is a var so a test can swap a link inside that window;
// production leaves it empty.
var afterContainmentCheck = func() {}

// checkStoreContainment re-runs containedIn for a store that StoreDir resolved
// from the project tier; any other directory (the user tier, --store, the
// default) carries no containment rule and passes. withLock calls it before it
// creates anything in the store and again once the lock is held, before fn, so a
// link swapped in after resolve, or while the lock was awaited, is refused
// rather than written through. What remains is the window between the last check
// and each open inside fn, which only an os.Root anchored at the repository root
// would close.
func checkStoreContainment(dir string) error {
	root, ok := projectStores.Load(filepath.Clean(dir))
	if !ok {
		return nil
	}
	if err := containedIn(root.(string), dir); err != nil {
		return fmt.Errorf("localdebt store %s: %w", filepath.Base(dir), err)
	}
	return nil
}

// containedIn reports whether dir, with every symlink on its EXISTING prefix
// followed, is still inside root (also resolved, so a repository that itself
// sits behind a link is not an escape). The part of dir that does not exist yet
// holds no link to follow; Append will create it as plain directories.
//
// The check is made when the store is resolved, and again by withLock before
// the first write (checkStoreContainment), so a link swapped in between is
// caught at write time too.
func containedIn(root, dir string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("cannot resolve the repository root: %v", basePathErr(err))
	}
	existing := dir
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect the store path: %v", basePathErr(err))
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	realDir, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("cannot resolve the store path (dangling link?): %v", basePathErr(err))
	}
	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("resolves outside the repository through a symlink")
	}
	return nil
}

// userStoreDir resolves the user tier's value: "~" and "~/..." expanded to the
// home directory, absolute as given, relative against root. It gets the same system-directory guard --store gets
// (validation.FilePath on the absolute form), but no containment rule.
func userStoreDir(root, v string) (string, error) {
	expanded, err := validation.ExpandHome(v)
	if err != nil {
		return "", fmt.Errorf("registry.yaml debt.store %q: %w", v, err)
	}
	dir := filepath.Clean(expanded)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("registry.yaml debt.store %q: %w", v, err)
	}
	if err := validation.FilePath(abs); err != nil {
		return "", fmt.Errorf("registry.yaml debt.store %q: %w", v, err)
	}
	return dir, nil
}
