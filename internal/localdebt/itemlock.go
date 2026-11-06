package localdebt

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/gitexec"
)

// Per-item locks (Epic 93). A lock on one debt item is one annotated tag object
// pushed to refs/atcr/debt-locks/<hex id> on the store repository's remote.
//
// The push IS the mutex. Two clones share no filesystem, so the per-store .flock
// (withLock, lock_*.go) cannot reach across them; a ref on the shared remote can.
// The remote's receive-pack creates a ref only if it does not exist, so of two
// pushes of one ref exactly one lands and the other is rejected. The tag body
// carries a random nonce, so a second lock never pushes an identical object and
// reads as "up to date" — it is always a rejected push. This is the mechanism
// /workspace/td-own.sh proved for a whole-table lease, scoped down to one item.
//
// There is no local fallback. A store outside a git repository, a repository
// with no remote, or a remote that cannot be reached is an error, because a lock
// only one clone can see protects nothing. Every git call is bounded by
// ItemLockGitTimeout.
//
// Locks are advisory: nothing else in this package reads them. A lock blocks
// only a second lock of the same item.
//
// The holder name is written into the tag on the remote, so anyone who can read
// that remote can read it.

// ItemLockRefPrefix is the ref namespace holding one ref per locked item.
const ItemLockRefPrefix = "refs/atcr/debt-locks/"

// ItemLockGitTimeout bounds each git subprocess a lock or unlock runs.
const ItemLockGitTimeout = 30 * time.Second

// ItemLockHolderEnv names the environment variable that supplies the holder
// when no explicit holder is given.
const ItemLockHolderEnv = "ATCR_DEBT_HOLDER"

const (
	maxItemLockIDBytes     = 100 // hex doubles it; a ref component must stay under 255
	maxItemLockHolderBytes = 256
	itemLockTagName        = "atcr-debt-lock"
)

var (
	// ErrItemLocked reports that an item is already locked on the remote.
	ErrItemLocked = errors.New("item already locked")
	// ErrItemNotLocked reports an unlock of an item with no lock on the remote.
	ErrItemNotLocked = errors.New("item not locked")
	// ErrItemLockHeldByOther reports an unlock by a holder other than the one
	// recorded in the lock.
	ErrItemLockHeldByOther = errors.New("item locked by another holder")
	// ErrItemLockInput reports an invalid id or holder.
	ErrItemLockInput = errors.New("invalid item lock input")
)

// ItemLock describes one lock: the item, who holds it, since when, and where.
type ItemLock struct {
	ID     string
	Holder string
	At     time.Time
	Ref    string
	Remote string
}

// ItemLocker locks and unlocks items of the store at Dir.
type ItemLocker struct {
	// Dir is the store directory. Its repository is found from Dir, or from its
	// nearest existing ancestor when Dir has not been created yet.
	Dir string
	// Holder is recorded in each lock and must match the recorded holder to
	// unlock. See ResolveItemLockHolder.
	Holder string
	// Now stamps the lock time; nil means time.Now.
	Now func() time.Time
	// Timeout bounds each git subprocess; zero means ItemLockGitTimeout.
	Timeout time.Duration
}

// ItemLockRef is the ref that holds the lock for id. The id is hex-encoded, so
// no character in it can change the ref's shape or fail git's ref-name rules.
func ItemLockRef(id string) string {
	return ItemLockRefPrefix + hex.EncodeToString([]byte(id))
}

// ResolveItemLockHolder returns flag when set, else $ATCR_DEBT_HOLDER, else the
// OS user name alone: never user@host, which would publish the host name in a
// ref on the remote. It returns "" when none is available.
func ResolveItemLockHolder(flag string) string {
	if flag != "" {
		return flag
	}
	if v := strings.TrimSpace(os.Getenv(ItemLockHolderEnv)); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil {
		if name := bareUserName(u.Username); name != "" {
			return name
		}
	}
	for _, k := range []string{"USER", "USERNAME"} {
		if name := bareUserName(os.Getenv(k)); name != "" {
			return name
		}
	}
	return ""
}

// bareUserName drops a Windows DOMAIN\ prefix and any @host or @realm suffix.
func bareUserName(name string) string {
	if i := strings.LastIndexByte(name, '\\'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.IndexByte(name, '@'); i >= 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// Lock takes a lock on every id in one atomic push: either all are taken or
// none is. An id already locked fails the whole call with ErrItemLocked.
func (l ItemLocker) Lock(ctx context.Context, ids []string) ([]ItemLock, error) {
	if err := validateItemLockInput(l.Holder, ids); err != nil {
		return nil, err
	}
	g, remote, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	tree, _, err := g.run(ctx, "", "hash-object", "-t", "tree", "-w", "--stdin")
	if err != nil {
		return nil, err
	}
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	at := now().UTC().Truncate(time.Second)
	locks := make([]ItemLock, 0, len(ids))
	args := []string{"push", "--atomic", "--porcelain", "--no-verify", remote}
	for _, id := range ids {
		nonce := make([]byte, 8)
		if _, err := rand.Read(nonce); err != nil {
			return nil, fmt.Errorf("lock %s: %w", id, err)
		}
		body := fmt.Sprintf("object %s\ntype tree\ntag %s\ntagger %s <> %d +0000\n\natcr debt lock\nid: %s\nholder: %s\nat: %s\nnonce: %s\n",
			tree, itemLockTagName, l.Holder, at.Unix(), id, l.Holder, at.Format(time.RFC3339), hex.EncodeToString(nonce))
		sha, _, err := g.run(ctx, body, "mktag")
		if err != nil {
			return nil, fmt.Errorf("lock %s: %w", id, err)
		}
		ref := ItemLockRef(id)
		args = append(args, sha+":"+ref)
		locks = append(locks, ItemLock{ID: id, Holder: l.Holder, At: at, Ref: ref, Remote: remote})
	}
	out, _, err := g.run(ctx, "", args...)
	status := parsePushPorcelain(out)
	if err == nil {
		for _, lk := range locks {
			if s := status[lk.Ref]; s.flag != '*' {
				return nil, fmt.Errorf("lock %s: push to %s did not create %s (%q)", lk.ID, remote, lk.Ref, s.summary)
			}
		}
		return locks, nil
	}
	// The rejection reason differs with git version and timing ("already
	// exists", "fetch first", "needs force", or "remote rejected" when two
	// pushes race inside the remote), so the remote itself decides: a rejected
	// ref that now exists there is a held lock.
	var rejected []string
	var reasons []string
	for _, lk := range locks {
		if s, ok := status[lk.Ref]; ok && s.flag == '!' && !strings.Contains(s.summary, "atomic push failed") {
			rejected = append(rejected, lk.ID)
			reasons = append(reasons, fmt.Sprintf("%s: %s", lk.ID, s.summary))
		}
	}
	if len(rejected) > 0 {
		present, lerr := listItemLockRefs(ctx, g, remote, rejected)
		if lerr != nil {
			return nil, fmt.Errorf("lock: push to %s rejected (%s), and %w; nothing was locked", remote, strings.Join(reasons, "; "), lerr)
		}
		var held []string
		for _, id := range rejected {
			if _, ok := present[id]; ok {
				held = append(held, id)
			}
		}
		if len(held) > 0 {
			return nil, fmt.Errorf("%w: %s; nothing was locked", ErrItemLocked, describeHeld(ctx, g, remote, held))
		}
		return nil, fmt.Errorf("lock: push to %s rejected (%s); nothing was locked", remote, strings.Join(reasons, "; "))
	}
	return nil, fmt.Errorf("lock: push to %s failed: %w", remote, err)
}

// describeHeld names each held id with its recorded holder and time when the
// lock can be read, and the bare id when it cannot: the push already proved the
// lock exists, so a failed read must not hide that.
func describeHeld(ctx context.Context, g itemLockRunner, remote string, ids []string) string {
	read, _ := readItemLocks(ctx, g, remote, ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if lk, ok := read[id]; ok {
			parts = append(parts, fmt.Sprintf("%s is locked by %s since %s", id, lk.Holder, lk.At.Format(time.RFC3339)))
		} else {
			parts = append(parts, id+" is locked")
		}
	}
	return strings.Join(parts, "; ")
}

// Unlock releases the lock on every id in one atomic push: either all are
// released or none is. An id with no lock fails with ErrItemNotLocked, and an id
// locked under another holder with ErrItemLockHeldByOther. Each delete is leased
// on the lock it read, so a lock re-taken in between is never released.
func (l ItemLocker) Unlock(ctx context.Context, ids []string) ([]ItemLock, error) {
	if err := validateItemLockInput(l.Holder, ids); err != nil {
		return nil, err
	}
	g, remote, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	read, err := readItemLocks(ctx, g, remote, ids)
	if err != nil {
		return nil, fmt.Errorf("unlock: %w", err)
	}
	var missing, others []string
	locks := make([]ItemLock, 0, len(ids))
	for _, id := range ids {
		lk, ok := read[id]
		switch {
		case !ok:
			missing = append(missing, id)
		case lk.Holder != l.Holder:
			others = append(others, fmt.Sprintf("%s is locked by %s, not %s", id, lk.Holder, l.Holder))
		default:
			locks = append(locks, lk.ItemLock)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s on %s; nothing was unlocked", ErrItemNotLocked, strings.Join(missing, ", "), remote)
	}
	if len(others) > 0 {
		return nil, fmt.Errorf("%w: %s; give that holder name to release it; nothing was unlocked", ErrItemLockHeldByOther, strings.Join(others, "; "))
	}
	args := []string{"push", "--atomic", "--porcelain", "--no-verify"}
	for _, id := range ids {
		args = append(args, "--force-with-lease="+ItemLockRef(id)+":"+read[id].sha)
	}
	args = append(args, remote)
	for _, id := range ids {
		args = append(args, ":"+ItemLockRef(id))
	}
	out, _, err := g.run(ctx, "", args...)
	status := parsePushPorcelain(out)
	if err != nil {
		for _, lk := range locks {
			if s := status[lk.Ref]; s.flag == '!' && !strings.Contains(s.summary, "atomic push failed") {
				return nil, fmt.Errorf("unlock %s: push to %s rejected (%s); nothing was unlocked", lk.ID, remote, s.summary)
			}
		}
		return nil, fmt.Errorf("unlock: push to %s failed: %w", remote, err)
	}
	for _, lk := range locks {
		if s := status[lk.Ref]; s.flag != '-' {
			return nil, fmt.Errorf("unlock %s: push to %s did not delete %s (%q)", lk.ID, remote, lk.Ref, s.summary)
		}
	}
	return locks, nil
}

// List reads every lock the store remote holds, oldest first, then by id. It
// only reads: it checks no holder for liveness and never frees a lock (Epic 93
// T2). Holder is not used. A ref under ItemLockRefPrefix whose name is not a
// hex-encoded id, or whose tag is not a lock, is an error rather than skipped,
// so a lock cannot drop out of the list unseen.
func (l ItemLocker) List(ctx context.Context) ([]ItemLock, error) {
	g, remote, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	out, _, err := g.run(ctx, "", "ls-remote", "--refs", remote, ItemLockRefPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("read locks from %s: %w", remote, err)
	}
	found := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasPrefix(ref, ItemLockRefPrefix) {
			continue
		}
		raw, err := hex.DecodeString(strings.TrimPrefix(ref, ItemLockRefPrefix))
		if err != nil || len(raw) == 0 || ItemLockRef(string(raw)) != ref {
			return nil, fmt.Errorf("read locks from %s: %s is not an item lock ref (its name is not an id in lowercase hex)", remote, ref)
		}
		found[string(raw)] = sha
	}
	read, err := fetchItemLocks(ctx, g, remote, found)
	if err != nil {
		return nil, err
	}
	locks := make([]ItemLock, 0, len(read))
	for _, lk := range read {
		locks = append(locks, lk.ItemLock)
	}
	slices.SortFunc(locks, func(a, b ItemLock) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return locks, nil
}

// remoteItemLock is a lock read from the remote, with the tag object it names.
type remoteItemLock struct {
	ItemLock
	sha string
}

// readItemLocks reads the locks the remote holds for ids. An id with no ref on
// the remote is absent from the result; a ref that cannot be read is an error.
func readItemLocks(ctx context.Context, g itemLockRunner, remote string, ids []string) (map[string]remoteItemLock, error) {
	found, err := listItemLockRefs(ctx, g, remote, ids)
	if err != nil {
		return nil, err
	}
	return fetchItemLocks(ctx, g, remote, found)
}

// fetchItemLocks fetches and parses the tag each id's ref names; found maps id
// to tag object as listItemLockRefs returns it.
func fetchItemLocks(ctx context.Context, g itemLockRunner, remote string, found map[string]string) (map[string]remoteItemLock, error) {
	fetch := make([]string, 0, len(found))
	for id := range found {
		fetch = append(fetch, ItemLockRef(id))
	}
	slices.Sort(fetch)
	result := make(map[string]remoteItemLock, len(found))
	if len(fetch) == 0 {
		return result, nil
	}
	if _, _, err := g.run(ctx, "", append([]string{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", remote}, fetch...)...); err != nil {
		return nil, fmt.Errorf("read locks from %s: %w", remote, err)
	}
	for id, sha := range found {
		ref := ItemLockRef(id)
		body, _, err := g.run(ctx, "", "cat-file", "tag", sha)
		if err != nil {
			return nil, fmt.Errorf("read lock %s (%s): %w", id, ref, err)
		}
		lk, err := parseItemLockTag(body)
		if err != nil {
			return nil, fmt.Errorf("read lock %s (%s): %w", id, ref, err)
		}
		if lk.ID != id {
			return nil, fmt.Errorf("read lock %s (%s): the tag records id %q", id, ref, lk.ID)
		}
		lk.Ref, lk.Remote = ref, remote
		result[id] = remoteItemLock{ItemLock: lk, sha: sha}
	}
	return result, nil
}

// listItemLockRefs returns the tag object each id's lock ref names on the
// remote; an id with no ref there is absent.
func listItemLockRefs(ctx context.Context, g itemLockRunner, remote string, ids []string) (map[string]string, error) {
	byRef := make(map[string]string, len(ids))
	args := []string{"ls-remote", "--refs", remote}
	for _, id := range ids {
		ref := ItemLockRef(id)
		byRef[ref] = id
		args = append(args, ref)
	}
	out, _, err := g.run(ctx, "", args...)
	if err != nil {
		return nil, fmt.Errorf("read locks from %s: %w", remote, err)
	}
	found := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		sha, ref, ok := strings.Cut(line, "\t")
		if id, want := byRef[ref]; ok && want {
			found[id] = sha
		}
	}
	return found, nil
}

// parseItemLockTag reads the id, holder and time from a lock tag's message.
func parseItemLockTag(body string) (ItemLock, error) {
	_, msg, ok := strings.Cut(body, "\n\n")
	if !ok {
		return ItemLock{}, errors.New("not an item lock tag: no message")
	}
	var lk ItemLock
	var at string
	for _, line := range strings.Split(msg, "\n") {
		key, val, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		switch key {
		case "id":
			lk.ID = val
		case "holder":
			lk.Holder = val
		case "at":
			at = val
		}
	}
	if lk.ID == "" || lk.Holder == "" || at == "" {
		return ItemLock{}, errors.New("not an item lock tag: id, holder or at missing")
	}
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return ItemLock{}, fmt.Errorf("not an item lock tag: at %q: %w", at, err)
	}
	lk.At = t.UTC()
	return lk, nil
}

// pushRefStatus is one ref's line of `git push --porcelain` output.
type pushRefStatus struct {
	flag    byte
	summary string
}

// parsePushPorcelain maps each destination ref to its status flag and summary.
// Lines look like "<flag>\t<src>:<dst>\t<summary>".
func parsePushPorcelain(out string) map[string]pushRefStatus {
	status := map[string]pushRefStatus{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 || len(parts[0]) != 1 {
			continue
		}
		_, dst, ok := strings.Cut(parts[1], ":")
		if !ok {
			continue
		}
		s := pushRefStatus{flag: parts[0][0]}
		if len(parts) == 3 {
			s.summary = parts[2]
		}
		status[dst] = s
	}
	return status
}

func validateItemLockInput(holder string, ids []string) error {
	if holder == "" {
		return fmt.Errorf("%w: no holder: pass --holder or set %s", ErrItemLockInput, ItemLockHolderEnv)
	}
	if len(holder) > maxItemLockHolderBytes {
		return fmt.Errorf("%w: holder is %d bytes, over the %d-byte limit", ErrItemLockInput, len(holder), maxItemLockHolderBytes)
	}
	if strings.TrimSpace(holder) != holder || strings.ContainsAny(holder, "<>") || !plainText(holder) {
		return fmt.Errorf("%w: holder %q must be plain text without control characters, '<', '>' or surrounding spaces", ErrItemLockInput, holder)
	}
	if len(ids) == 0 {
		return fmt.Errorf("%w: no item ids", ErrItemLockInput)
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || len(id) > maxItemLockIDBytes || strings.TrimSpace(id) != id || !plainText(id) {
			return fmt.Errorf("%w: item id %q must be 1-%d bytes of plain text without control characters or surrounding spaces", ErrItemLockInput, id, maxItemLockIDBytes)
		}
		if seen[id] {
			return fmt.Errorf("%w: item id %q given twice", ErrItemLockInput, id)
		}
		seen[id] = true
	}
	return nil
}

// plainText reports whether s is valid UTF-8 with no control characters, so it
// cannot add a line to the line-oriented tag message.
func plainText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// open finds the store's repository and the remote that holds its locks.
func (l ItemLocker) open(ctx context.Context) (itemLockRunner, string, error) {
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = ItemLockGitTimeout
	}
	dir, err := existingAncestor(l.Dir)
	if err != nil {
		return itemLockRunner{}, "", err
	}
	g := itemLockRunner{dir: dir, timeout: timeout}
	if _, _, err := g.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return itemLockRunner{}, "", fmt.Errorf("store %s is not inside a git repository; item locks live on its remote and have no local fallback: %w", l.Dir, err)
	}
	out, _, err := g.run(ctx, "", "remote")
	if err != nil {
		return itemLockRunner{}, "", err
	}
	var remotes []string
	for _, r := range strings.Split(out, "\n") {
		if r = strings.TrimSpace(r); r != "" {
			remotes = append(remotes, r)
		}
	}
	switch {
	case slices.Contains(remotes, "origin"):
		return g, "origin", nil
	case len(remotes) == 1:
		return g, remotes[0], nil
	case len(remotes) == 0:
		return itemLockRunner{}, "", fmt.Errorf("the repository of store %s has no git remote; item locks live on the remote and have no local fallback", l.Dir)
	default:
		return itemLockRunner{}, "", fmt.Errorf("the repository of store %s has remotes %s and none is named origin; item locks need one remote to hold them", l.Dir, strings.Join(remotes, ", "))
	}
}

// existingAncestor returns dir or its nearest ancestor that exists, as an
// absolute path.
func existingAncestor(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("store %s: %w", dir, err)
	}
	for d := abs; ; d = filepath.Dir(d) {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("store %s: no existing directory above it", dir)
		}
	}
}

// itemLockRunner runs git in one directory with a per-call timeout.
type itemLockRunner struct {
	dir     string
	timeout time.Duration
}

// itemLockScrubbedVars are the repo-locating variables a git hook exports. They
// are dropped so the store's own repository is found from dir; otherwise a
// lock taken under a hook would land on the hook's repository and its remote.
var itemLockScrubbedVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_PREFIX", "GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
}

// run runs git with stdin and returns trimmed stdout and stderr. A call cut off
// by the timeout is reported as timed out.
func (g itemLockRunner) run(ctx context.Context, stdin string, args ...string) (string, string, error) {
	cctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	cmd := gitexec.CommandContextFn(cctx, args...)
	cmd.Dir = g.dir
	cmd.Env = append(slices.DeleteFunc(cmd.Env, func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(itemLockScrubbedVars, k)
	}), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	// A transport child (ssh, a remote helper) can outlive git and hold its
	// pipes open; WaitDelay stops Wait from blocking on it after the kill.
	cmd.WaitDelay = time.Second
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out, errOut := strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String())
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return out, errOut, fmt.Errorf("git %s timed out after %s", args[0], g.timeout)
	}
	if err != nil {
		if errOut != "" {
			return out, errOut, fmt.Errorf("git %s: %w: %s", args[0], err, errOut)
		}
		return out, errOut, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, errOut, nil
}
