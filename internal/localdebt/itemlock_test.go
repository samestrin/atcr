package localdebt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/gitexec"
)

// itemLockGit runs a fixture git command in dir and returns its trimmed stdout.
func itemLockGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitexec.FixtureCommandContext(context.Background(), dir, args...).CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// itemLockRemote creates a bare remote and n clones of it, and returns the
// remote's path and the clones' store directories (<clone>/.atcr/debt, created).
func itemLockRemote(t *testing.T, n int) (string, []string) {
	t.Helper()
	// A non-bare remote: FixtureCommandContext pins GIT_WORK_TREE, which --bare
	// refuses, and the locks touch only refs/atcr/, never a checked-out branch.
	remote := t.TempDir()
	itemLockGit(t, remote, "init", "-q")
	var stores []string
	for i := 0; i < n; i++ {
		clone := t.TempDir()
		itemLockGit(t, clone, "init", "-q")
		itemLockGit(t, clone, "remote", "add", "origin", remote)
		store := DefaultDir(clone)
		require.NoError(t, os.MkdirAll(store, 0o755))
		stores = append(stores, store)
	}
	return remote, stores
}

func fixedNow() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }

func TestItemLockRef_HexEncodesID(t *testing.T) {
	require.Equal(t, "refs/atcr/debt-locks/54442d303031", ItemLockRef("TD-001"))
	// A slash, a space or a dot-dot cannot reach the ref name.
	require.Equal(t, "refs/atcr/debt-locks/612f2e2e2062", ItemLockRef("a/.. b"))
}

// The task's success criterion: two clones locking one item produce one winner.
func TestItemLock_TwoClonesOneItemOneWinner(t *testing.T) {
	_, stores := itemLockRemote(t, 2)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, store := range stores {
		wg.Add(1)
		go func(i int, store string) {
			defer wg.Done()
			l := ItemLocker{Dir: store, Holder: []string{"alice", "bob"}[i], Now: fixedNow}
			_, errs[i] = l.Lock(context.Background(), []string{"TD-001"})
		}(i, store)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		require.ErrorIs(t, err, ErrItemLocked)
	}
	require.Equal(t, 1, wins, "errors: %v", errs)
}

func TestItemLock_SecondLockFailsAndNamesHolder(t *testing.T) {
	_, stores := itemLockRemote(t, 2)
	got, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "alice", got[0].Holder)
	require.Equal(t, fixedNow(), got[0].At)
	require.Equal(t, ItemLockRef("TD-001"), got[0].Ref)

	// The same holder from the same clone is refused too: a second lock is never
	// a quiet success.
	for _, store := range stores {
		_, err = ItemLocker{Dir: store, Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
		require.ErrorIs(t, err, ErrItemLocked)
		require.Contains(t, err.Error(), "TD-001")
		require.Contains(t, err.Error(), "alice")
	}
}

// AC1: a lock blocks only its own item.
func TestItemLock_OtherItemsStayLockable(t *testing.T) {
	_, stores := itemLockRemote(t, 2)
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
	_, err = ItemLocker{Dir: stores[1], Holder: "bob", Now: fixedNow}.Lock(context.Background(), []string{"TD-002"})
	require.NoError(t, err)
}

// Several ids lock in one atomic push: if one is held, none is taken.
func TestItemLock_ManyIDsAllOrNothing(t *testing.T) {
	remote, stores := itemLockRemote(t, 2)
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-002"})
	require.NoError(t, err)

	_, err = ItemLocker{Dir: stores[1], Holder: "bob", Now: fixedNow}.Lock(context.Background(), []string{"TD-001", "TD-002", "TD-003"})
	require.ErrorIs(t, err, ErrItemLocked)
	require.Contains(t, err.Error(), "TD-002")
	refs := itemLockGit(t, remote, "for-each-ref", "--format=%(refname)", "refs/atcr/")
	require.Equal(t, ItemLockRef("TD-002"), refs)

	got, err := ItemLocker{Dir: stores[1], Holder: "bob", Now: fixedNow}.Lock(context.Background(), []string{"TD-001", "TD-003"})
	require.NoError(t, err)
	require.Len(t, got, 2)
}

// The ref is an annotated tag object that records the id, holder and time.
func TestItemLock_RefIsAnnotatedTagWithHolderAndTime(t *testing.T) {
	remote, stores := itemLockRemote(t, 1)
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
	ref := ItemLockRef("TD-001")
	require.Equal(t, "tag", itemLockGit(t, remote, "cat-file", "-t", ref))
	body := itemLockGit(t, remote, "cat-file", "tag", ref)
	require.Contains(t, body, "\nid: TD-001\n")
	require.Contains(t, body, "\nholder: alice\n")
	require.Contains(t, body, "\nat: 2026-11-06T12:00:00Z\n")
}

func TestItemUnlock_ReleasesAndAllowsRelock(t *testing.T) {
	remote, stores := itemLockRemote(t, 2)
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001", "TD-002"})
	require.NoError(t, err)

	got, err := ItemLocker{Dir: stores[0], Holder: "alice"}.Unlock(context.Background(), []string{"TD-001", "TD-002"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "alice", got[0].Holder)
	require.Empty(t, itemLockGit(t, remote, "for-each-ref", "refs/atcr/"))

	_, err = ItemLocker{Dir: stores[1], Holder: "bob", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
}

// AC2: unlocking an item nobody holds is an error, never a quiet success.
func TestItemUnlock_NotLockedIsError(t *testing.T) {
	_, stores := itemLockRemote(t, 1)
	_, err := ItemLocker{Dir: stores[0], Holder: "alice"}.Unlock(context.Background(), []string{"TD-001"})
	require.ErrorIs(t, err, ErrItemNotLocked)
	require.Contains(t, err.Error(), "TD-001")
}

// Another holder's lock is not released; nothing is released if one id fails.
func TestItemUnlock_OtherHolderIsErrorAndNothingReleased(t *testing.T) {
	remote, stores := itemLockRemote(t, 2)
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
	_, err = ItemLocker{Dir: stores[1], Holder: "bob", Now: fixedNow}.Lock(context.Background(), []string{"TD-002"})
	require.NoError(t, err)

	_, err = ItemLocker{Dir: stores[1], Holder: "bob"}.Unlock(context.Background(), []string{"TD-002", "TD-001"})
	require.ErrorIs(t, err, ErrItemLockHeldByOther)
	require.Contains(t, err.Error(), "alice")
	refs := itemLockGit(t, remote, "for-each-ref", "--format=%(refname)", "refs/atcr/")
	require.Contains(t, refs, ItemLockRef("TD-001"))
	require.Contains(t, refs, ItemLockRef("TD-002"))

	// Naming the recorded holder releases it: the lock is advisory.
	_, err = ItemLocker{Dir: stores[1], Holder: "alice"}.Unlock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
}

func TestItemLock_NoGitRepoIsError(t *testing.T) {
	store := DefaultDir(t.TempDir())
	require.NoError(t, os.MkdirAll(store, 0o755))
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(filepath.Dir(filepath.Dir(store))))
	_, err := ItemLocker{Dir: store, Holder: "alice"}.Lock(context.Background(), []string{"TD-001"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not inside a git repository")
	_, err = ItemLocker{Dir: store, Holder: "alice"}.Unlock(context.Background(), []string{"TD-001"})
	require.Error(t, err)
}

func TestItemLock_NoRemoteIsError(t *testing.T) {
	clone := t.TempDir()
	itemLockGit(t, clone, "init", "-q")
	_, err := ItemLocker{Dir: DefaultDir(clone), Holder: "alice"}.Lock(context.Background(), []string{"TD-001"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no git remote")
}

func TestItemLock_UnreachableRemoteIsError(t *testing.T) {
	clone := t.TempDir()
	itemLockGit(t, clone, "init", "-q")
	itemLockGit(t, clone, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	_, err := ItemLocker{Dir: DefaultDir(clone), Holder: "alice"}.Lock(context.Background(), []string{"TD-001"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrItemLocked)
	_, err = ItemLocker{Dir: DefaultDir(clone), Holder: "alice"}.Unlock(context.Background(), []string{"TD-001"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrItemNotLocked)
}

// A remote that never answers is cut off by the git timeout.
func TestItemLock_HungRemoteTimesOut(t *testing.T) {
	clone := t.TempDir()
	itemLockGit(t, clone, "init", "-q")
	itemLockGit(t, clone, "config", "protocol.ext.allow", "always")
	itemLockGit(t, clone, "remote", "add", "origin", "ext::sh -c sleep% 30")
	start := time.Now()
	_, err := ItemLocker{Dir: DefaultDir(clone), Holder: "alice", Timeout: 300 * time.Millisecond}.Lock(context.Background(), []string{"TD-001"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out")
	require.Less(t, time.Since(start), 15*time.Second)
}

func TestItemLock_DefaultTimeoutIs30s(t *testing.T) {
	require.Equal(t, 30*time.Second, ItemLockGitTimeout)
}

// An inherited GIT_DIR (a hook's environment) must not retarget the lock at
// another repository: the store's own repository is the one that is locked.
func TestItemLock_IgnoresInheritedGitDir(t *testing.T) {
	remote, stores := itemLockRemote(t, 1)
	outer := t.TempDir()
	itemLockGit(t, outer, "init", "-q")
	t.Setenv("GIT_DIR", filepath.Join(outer, ".git"))
	t.Setenv("GIT_WORK_TREE", outer)
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
	require.Equal(t, ItemLockRef("TD-001"), itemLockGit(t, remote, "for-each-ref", "--format=%(refname)", "refs/atcr/"))
}

// The store directory need not exist yet: the repository is found from its
// nearest existing ancestor.
func TestItemLock_MissingStoreDirUsesEnclosingRepo(t *testing.T) {
	_, stores := itemLockRemote(t, 1)
	require.NoError(t, os.RemoveAll(stores[0]))
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
}

func TestItemLock_PicksOriginAmongRemotes(t *testing.T) {
	remote, stores := itemLockRemote(t, 1)
	clone := filepath.Dir(filepath.Dir(stores[0]))
	other := filepath.Join(t.TempDir(), "other.git")
	itemLockGit(t, clone, "remote", "add", "zzz", other)
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
	require.NotEmpty(t, itemLockGit(t, remote, "for-each-ref", "refs/atcr/"))
}

func TestItemLock_SingleNonOriginRemote(t *testing.T) {
	remote, stores := itemLockRemote(t, 1)
	clone := filepath.Dir(filepath.Dir(stores[0]))
	itemLockGit(t, clone, "remote", "rename", "origin", "upstream")
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(context.Background(), []string{"TD-001"})
	require.NoError(t, err)
	require.NotEmpty(t, itemLockGit(t, remote, "for-each-ref", "refs/atcr/"))
}

func TestItemLock_AmbiguousRemotesIsError(t *testing.T) {
	_, stores := itemLockRemote(t, 1)
	clone := filepath.Dir(filepath.Dir(stores[0]))
	itemLockGit(t, clone, "remote", "rename", "origin", "a")
	itemLockGit(t, clone, "remote", "add", "b", filepath.Join(t.TempDir(), "b.git"))
	_, err := ItemLocker{Dir: stores[0], Holder: "alice"}.Lock(context.Background(), []string{"TD-001"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "origin")
}

func TestItemLock_RejectsBadInput(t *testing.T) {
	_, stores := itemLockRemote(t, 1)
	cases := map[string]struct {
		holder string
		ids    []string
	}{
		"no ids":            {"alice", nil},
		"empty id":          {"alice", []string{""}},
		"newline in id":     {"alice", []string{"TD-1\nholder: x"}},
		"duplicate id":      {"alice", []string{"TD-1", "TD-1"}},
		"empty holder":      {"", []string{"TD-1"}},
		"newline in holder": {"a\nb", []string{"TD-1"}},
		"angle in holder":   {"a<b>", []string{"TD-1"}},
		"long holder":       {strings.Repeat("a", 257), []string{"TD-1"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			l := ItemLocker{Dir: stores[0], Holder: c.holder}
			_, err := l.Lock(context.Background(), c.ids)
			require.ErrorIs(t, err, ErrItemLockInput)
			_, err = l.Unlock(context.Background(), c.ids)
			require.ErrorIs(t, err, ErrItemLockInput)
		})
	}
}

func TestDefaultItemLockHolder(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv(ItemLockHolderEnv, "env-name")
		require.Equal(t, "flag-name", ResolveItemLockHolder("flag-name"))
	})
	t.Run("env next", func(t *testing.T) {
		t.Setenv(ItemLockHolderEnv, "env-name")
		require.Equal(t, "env-name", ResolveItemLockHolder(""))
	})
	t.Run("os user name only", func(t *testing.T) {
		t.Setenv(ItemLockHolderEnv, "")
		got := ResolveItemLockHolder("")
		require.NotEmpty(t, got)
		require.NotContains(t, got, "@")
		host, _ := os.Hostname()
		if host != "" {
			require.NotContains(t, got, "@"+host)
		}
	})
	t.Run("domain and host parts are stripped", func(t *testing.T) {
		require.Equal(t, "sam", bareUserName(`CORP\sam`))
		require.Equal(t, "sam", bareUserName("sam@host"))
		require.Equal(t, "sam", bareUserName("sam"))
	})
}

func TestParseItemLockTag(t *testing.T) {
	body := "object 4b825dc642cb6eb9a060e54bf8d69288fbee4904\ntype tree\ntag atcr-debt-lock\ntagger alice <> 1 +0000\n\natcr debt lock\nid: TD-1\nholder: alice\nat: 2026-11-06T12:00:00Z\nnonce: ab\n"
	got, err := parseItemLockTag(body)
	require.NoError(t, err)
	require.Equal(t, "TD-1", got.ID)
	require.Equal(t, "alice", got.Holder)
	require.Equal(t, fixedNow(), got.At)

	_, err = parseItemLockTag("object x\n\nno fields\n")
	require.Error(t, err)
	_, err = parseItemLockTag("object x\n\nid: a\nholder: b\nat: yesterday\n")
	require.Error(t, err)
}

func TestItemLockErrorsAreDistinct(t *testing.T) {
	require.False(t, errors.Is(ErrItemLocked, ErrItemNotLocked))
}

// Epic 93 T2: List reads every lock on the remote, and only reads.
func TestItemLockList_ReadsEveryLockAndFreesNone(t *testing.T) {
	remote, stores := itemLockRemote(t, 2)
	ctx := context.Background()
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(ctx, []string{"TD-001", "a/.. b"})
	require.NoError(t, err)
	_, err = ItemLocker{Dir: stores[1], Holder: "bob", Now: func() time.Time { return fixedNow().Add(time.Hour) }}.Lock(ctx, []string{"TD-002"})
	require.NoError(t, err)
	before := itemLockGit(t, remote, "for-each-ref", "refs/atcr/")

	locks, warnings, err := ItemLocker{Dir: stores[1]}.List(ctx)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, locks, 3)
	byID := map[string]ItemLock{}
	for _, lk := range locks {
		byID[lk.ID] = lk
	}
	require.Equal(t, "alice", byID["TD-001"].Holder)
	require.Equal(t, "alice", byID["a/.. b"].Holder)
	require.Equal(t, fixedNow(), byID["a/.. b"].At)
	require.Equal(t, "bob", byID["TD-002"].Holder)
	require.Equal(t, fixedNow().Add(time.Hour), byID["TD-002"].At)
	require.Equal(t, ItemLockRef("TD-002"), byID["TD-002"].Ref)
	require.Equal(t, "origin", byID["TD-002"].Remote)
	// Oldest first, then by id.
	require.Equal(t, []string{"TD-001", "a/.. b", "TD-002"}, []string{locks[0].ID, locks[1].ID, locks[2].ID})

	require.Equal(t, before, itemLockGit(t, remote, "for-each-ref", "refs/atcr/"), "List must not change any lock")
}

func TestItemLockList_NoLocksIsEmpty(t *testing.T) {
	_, stores := itemLockRemote(t, 1)
	locks, warnings, err := ItemLocker{Dir: stores[0]}.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, locks)
	require.Empty(t, warnings)
}

func TestItemLockList_NoRemoteIsError(t *testing.T) {
	clone := t.TempDir()
	itemLockGit(t, clone, "init", "-q")
	_, _, err := ItemLocker{Dir: DefaultDir(clone)}.List(context.Background())
	require.ErrorContains(t, err, "no git remote")
}

// A remote that cannot be reached at all is still an error: leniency is per
// bad ref, not per unreachable remote.
func TestItemLockList_UnreachableRemoteIsError(t *testing.T) {
	clone := t.TempDir()
	itemLockGit(t, clone, "init", "-q")
	itemLockGit(t, clone, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	_, _, err := ItemLocker{Dir: DefaultDir(clone)}.List(context.Background())
	require.ErrorContains(t, err, "read locks from origin")
}

// A remote whose refs can be listed but whose objects cannot be fetched is
// still an error on both the listing and the mutation path: no lock can be
// read at all. The ref here names an object the remote no longer has.
func TestItemLockList_UnfetchableRemoteIsError(t *testing.T) {
	remote, stores := itemLockRemote(t, 1)
	itemLockGit(t, remote, "commit", "-q", "--allow-empty", "-m", "x")
	itemLockGit(t, remote, "update-ref", ItemLockRef("TD-001"), "HEAD")
	// Empty the remote's object store, keeping the directory so it is still a
	// repository: ls-remote reads refs only, fetch needs the objects.
	objects := filepath.Join(remote, ".git", "objects")
	entries, err := os.ReadDir(objects)
	require.NoError(t, err)
	for _, e := range entries {
		require.NoError(t, os.RemoveAll(filepath.Join(objects, e.Name())))
	}
	_, _, err = ItemLocker{Dir: stores[0]}.List(context.Background())
	require.ErrorContains(t, err, "read locks from origin")
	_, err = ItemLocker{Dir: stores[0], Holder: "alice"}.Unlock(context.Background(), []string{"TD-001"})
	require.ErrorContains(t, err, "read locks from origin")
}

// Epic 122 T4 (owner decision 2026-11-18) replaces TestItemLockList_ForeignRefNameIsError:
// a malformed ref name is a structured warning alongside the valid locks, not an
// error that hides every lock. This is a change of the documented rule, not a
// weakening: the bad ref must still be shown, by name, in the result.
func TestItemLockList_ForeignRefNameIsWarning(t *testing.T) {
	remote, stores := itemLockRemote(t, 1)
	ctx := context.Background()
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(ctx, []string{"TD-001"})
	require.NoError(t, err)
	itemLockGit(t, remote, "commit", "-q", "--allow-empty", "-m", "x")
	for _, name := range []string{"not-hex", "5A44"} {
		itemLockGit(t, remote, "update-ref", ItemLockRefPrefix+name, "HEAD")
	}

	locks, warnings, err := ItemLocker{Dir: stores[0]}.List(ctx)
	require.NoError(t, err)
	require.Len(t, locks, 1, "the valid lock is still listed")
	require.Equal(t, "TD-001", locks[0].ID)
	require.Len(t, warnings, 2)
	// Warnings are ordered by ref, so the output is deterministic.
	require.Equal(t, ItemLockRefPrefix+"5A44", warnings[0].Ref)
	require.Equal(t, ItemLockRefPrefix+"not-hex", warnings[1].Ref)
	for _, w := range warnings {
		require.Equal(t, "origin", w.Remote)
		require.Contains(t, w.Reason, "not an item lock ref")
	}
}

// A ref whose name is a valid hex id but whose tag cannot be read as a lock is
// also a warning, and every other lock is still listed (epic 122 T4).
func TestItemLockList_BadTagIsWarningAndGoodLocksStillListed(t *testing.T) {
	ref := ItemLockRef("TD-002")
	putTag := func(t *testing.T, remote, name, msg string) {
		t.Helper()
		itemLockGit(t, remote, "tag", "-a", name, "-m", msg, "HEAD")
		itemLockGit(t, remote, "update-ref", ref, "refs/tags/"+name)
	}
	cases := map[string]struct {
		setup  func(t *testing.T, remote string)
		reason string
	}{
		"commit not tag": {
			setup: func(t *testing.T, remote string) {
				itemLockGit(t, remote, "update-ref", ref, "HEAD")
			},
			reason: "cat-file",
		},
		"tag without lock fields": {
			setup: func(t *testing.T, remote string) {
				putTag(t, remote, "foreign", "not a lock")
			},
			reason: "not an item lock tag",
		},
		"mismatched id": {
			setup: func(t *testing.T, remote string) {
				putTag(t, remote, "foreign", "atcr debt lock\nid: TD-999\nholder: alice\nat: 2026-11-06T12:00:00Z\nnonce: ab")
			},
			reason: `the tag records id "TD-999"`,
		},
		"bad time": {
			setup: func(t *testing.T, remote string) {
				putTag(t, remote, "foreign", "atcr debt lock\nid: TD-002\nholder: alice\nat: yesterday")
			},
			reason: "not an item lock tag",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			remote, stores := itemLockRemote(t, 1)
			ctx := context.Background()
			_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(ctx, []string{"TD-001"})
			require.NoError(t, err)
			itemLockGit(t, remote, "commit", "-q", "--allow-empty", "-m", "x")
			c.setup(t, remote)
			// A malformed name alongside the bad tag: warnings of both kinds
			// come back together, sorted by ref.
			itemLockGit(t, remote, "update-ref", ItemLockRefPrefix+"not-hex", "HEAD")

			locks, warnings, err := ItemLocker{Dir: stores[0]}.List(ctx)
			require.NoError(t, err)
			require.Len(t, locks, 1)
			require.Equal(t, "TD-001", locks[0].ID)
			require.Len(t, warnings, 2)
			require.Equal(t, ref, warnings[0].Ref)
			require.Equal(t, ItemLockRefPrefix+"not-hex", warnings[1].Ref)
			require.Equal(t, "origin", warnings[0].Remote)
			require.Contains(t, warnings[0].Reason, c.reason)
		})
	}
}

// The listing's leniency must not leak into the mutation paths: unlock (and
// the lock-held read behind Lock) still refuses a ref whose tag cannot be read.
func TestItemUnlock_BadTagIsStillError(t *testing.T) {
	remote, stores := itemLockRemote(t, 1)
	ctx := context.Background()
	itemLockGit(t, remote, "commit", "-q", "--allow-empty", "-m", "x")
	itemLockGit(t, remote, "update-ref", ItemLockRef("TD-001"), "HEAD")
	_, err := ItemLocker{Dir: stores[0], Holder: "alice"}.Unlock(ctx, []string{"TD-001"})
	// Assert the refusal cause, not just the id: ErrItemNotLocked also names
	// the id, so a lenient unlock that drops the unreadable lock must not pass.
	require.ErrorContains(t, err, "read lock TD-001")
	require.NotErrorIs(t, err, ErrItemNotLocked)
	_, err = ItemLocker{Dir: stores[0], Holder: "alice"}.Lock(ctx, []string{"TD-001"})
	require.ErrorIs(t, err, ErrItemLocked)
}

// removeRemoteObject deletes one loose object from the remote's object store,
// leaving its refs intact: ls-remote still lists the ref, but any fetch that
// includes it fails whole.
func removeRemoteObject(t *testing.T, remote, sha string) {
	t.Helper()
	require.NoError(t, os.Remove(filepath.Join(remote, ".git", "objects", sha[:2], sha[2:])))
}

// Row 485c4410f55dc612 (epic 127 T2): one ref whose object the remote can no
// longer serve makes the batch fetch fail whole; the listing must fall back to
// per-ref fetches, warn on that ref alone and still list every valid lock.
func TestItemLockList_UnfetchableRefObjectIsWarningAndGoodLocksStillListed(t *testing.T) {
	badRef := ItemLockRef("TD-003")
	cases := map[string]func(t *testing.T, remote, sha string){
		"missing object": func(t *testing.T, remote, sha string) {
			t.Helper()
			removeRemoteObject(t, remote, sha)
		},
		"corrupt object": func(t *testing.T, remote, sha string) {
			t.Helper()
			// Loose objects are read-only; replace the file with garbage bytes.
			removeRemoteObject(t, remote, sha)
			require.NoError(t, os.WriteFile(filepath.Join(remote, ".git", "objects", sha[:2], sha[2:]), []byte("not zlib"), 0o644))
		},
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			remote, stores := itemLockRemote(t, 1)
			ctx := context.Background()
			_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(ctx, []string{"TD-001", "TD-002"})
			require.NoError(t, err)
			itemLockGit(t, remote, "commit", "-q", "--allow-empty", "-m", "x")
			sha := itemLockGit(t, remote, "rev-parse", "HEAD")
			itemLockGit(t, remote, "update-ref", badRef, sha)
			damage(t, remote, sha)

			locks, warnings, err := ItemLocker{Dir: stores[0]}.List(ctx)
			require.NoError(t, err, "one unfetchable ref must not hide the valid locks")
			require.Len(t, locks, 2)
			require.Equal(t, []string{"TD-001", "TD-002"}, []string{locks[0].ID, locks[1].ID})
			require.Len(t, warnings, 1)
			require.Equal(t, badRef, warnings[0].Ref)
			require.Equal(t, "origin", warnings[0].Remote)
			require.NotEmpty(t, warnings[0].Reason)
		})
	}
}

// When every ref's fetch fails the remote itself cannot be read, and the
// listing is still an error: leniency is per bad ref, not per dead remote
// (row 485c4410f55dc612, plan: "still returning the error when every ref fails").
func TestItemLockList_AllRefsUnfetchableIsError(t *testing.T) {
	remote, stores := itemLockRemote(t, 1)
	// The refs name objects made on the remote and deleted there, so the clone
	// holds no copy: every fetch, batch or per-ref, must fail.
	shas := map[string]string{}
	for _, id := range []string{"TD-001", "TD-002"} {
		itemLockGit(t, remote, "commit", "-q", "--allow-empty", "-m", id)
		shas[id] = itemLockGit(t, remote, "rev-parse", "HEAD")
	}
	for id, sha := range shas {
		itemLockGit(t, remote, "update-ref", ItemLockRef(id), sha)
		removeRemoteObject(t, remote, sha)
	}
	_, _, err := ItemLocker{Dir: stores[0]}.List(context.Background())
	require.ErrorContains(t, err, "read locks from origin")
}

// The per-ref fallback must not leak leniency into the mutation paths: an
// unlock of several ids where one ref cannot be fetched is still an error
// naming that id, and nothing is unlocked.
func TestItemUnlock_OneUnfetchableRefIsStillError(t *testing.T) {
	remote, stores := itemLockRemote(t, 1)
	ctx := context.Background()
	_, err := ItemLocker{Dir: stores[0], Holder: "alice", Now: fixedNow}.Lock(ctx, []string{"TD-001"})
	require.NoError(t, err)
	// TD-002's ref names an object made on the remote and deleted there, so no
	// clone holds a copy and its fetch cannot succeed.
	itemLockGit(t, remote, "commit", "-q", "--allow-empty", "-m", "x")
	sha := itemLockGit(t, remote, "rev-parse", "HEAD")
	itemLockGit(t, remote, "update-ref", ItemLockRef("TD-002"), sha)
	before := itemLockGit(t, remote, "for-each-ref", "--format=%(refname)", "refs/atcr/")
	removeRemoteObject(t, remote, sha)
	_, err = ItemLocker{Dir: stores[0], Holder: "alice"}.Unlock(ctx, []string{"TD-001", "TD-002"})
	require.ErrorContains(t, err, "read lock TD-002")
	require.Equal(t, before, itemLockGit(t, remote, "for-each-ref", "--format=%(refname)", "refs/atcr/"), "nothing was unlocked")
}
