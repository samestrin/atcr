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
	registerProjectStore(dir, root, filepath.FromSlash(slashed))
	return dir, nil
}

// projectStores maps each directory projectStoreDir has handed out (keyed by
// projectStoreKey and by projectStoreRealKey) to its projectStore, so withLock
// can re-check containment at write time without every caller passing the root
// along. Read it only through projectStoreFor and drop entries through
// forgetProjectStore.
//
// The map has no automatic eviction: re-resolving a store replaces its entries,
// so it grows only with the number of distinct project-store paths a process
// resolves, which is one per repository (two keys) in practice. No pruning is done
// (EST-8389 Q3); tests drop their entries with forgetProjectStore.
var projectStores sync.Map

// projectStore is a projectStores value: the repository root a project-tier
// store was resolved against, the store's path relative to that root, and the
// symlink-resolved key it was also recorded under (see projectStoreRealKey).
type projectStore struct {
	root     string
	realRoot string
	rootInfo os.FileInfo
	rel      string
	realKey  string
}

// projectStoreKey is the primary projectStores key for dir: the cleaned absolute
// path, so a store resolved against a relative root and looked up by its
// absolute spelling (or the reverse) is one entry, not two (row
// 7a03d6351e9f221a). It follows no symlink: a link swapped into the store path
// after resolve must still find the entry, or checkStoreContainment would wave
// the swap through. If the working directory cannot be read the cleaned path is
// used as-is.
func projectStoreKey(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return abs
}

// projectStoreRealKey is the secondary key: projectStoreKey with every symlink
// on its longest resolvable prefix followed, so a store registered through a
// linked parent and reached by its real path (/var vs /private/var on macOS, a
// bind mount), or the reverse, still finds its entry (row 573b54a6bb1024d7). The
// part that does not resolve (not created yet, or under a removed root) is
// joined on as written, so a store whose root has vanished still matches
// through the root's surviving parent.
func projectStoreRealKey(dir string) string {
	abs := projectStoreKey(dir)
	rest := ""
	for p := abs; ; {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return abs
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// registerProjectStore records dir under both keys.
func registerProjectStore(dir, root, rel string) {
	ps := projectStore{root: root, rel: rel, realKey: projectStoreRealKey(dir)}
	projectStores.Store(projectStoreKey(dir), ps)
	projectStores.Store(ps.realKey, ps)
}

// projectStoreFor returns the projectStore recorded for dir and whether there is
// one, trying the primary key first and the symlink-resolved key second. An
// entry of any other type is an error rather than a panic on the write path
// (row 9ffc484fcafdf7c1).
func projectStoreFor(dir string) (projectStore, bool, error) {
	v, ok := projectStores.Load(projectStoreKey(dir))
	if !ok {
		if v, ok = projectStores.Load(projectStoreRealKey(dir)); !ok {
			return projectStore{}, false, nil
		}
	}
	ps, isStore := v.(projectStore)
	if !isStore {
		return projectStore{}, true, fmt.Errorf("localdebt store %s: malformed project-store registry entry (%T)", filepath.Base(dir), v)
	}
	return ps, true, nil
}

// forgetProjectStore drops dir's projectStores entries: both keys it was
// registered under, even when a link has since changed what dir resolves to.
func forgetProjectStore(dir string) {
	key := projectStoreKey(dir)
	if v, ok := projectStores.Load(key); ok {
		if ps, isStore := v.(projectStore); isStore {
			projectStores.Delete(ps.realKey)
		}
	}
	projectStores.Delete(key)
	projectStores.Delete(projectStoreRealKey(dir))
}

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
	ps, ok, err := projectStoreFor(dir)
	if err != nil || !ok {
		return err
	}
	if err := containedIn(ps.root, dir); err != nil {
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
