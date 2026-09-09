package payload

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commitMessages is the claim source: the ledger can only ask a reviewer to
// adjudicate an assertion the author actually made, so the read has to reach
// every non-merge commit in the range and preserve the message text verbatim.

func TestCommitMessages_ReturnsEveryCommitOldestFirst(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "1")
	base := commitAll(t, dir, "first commit\n\nfirst body line.")
	write(t, dir, "a.txt", "2")
	commitAll(t, dir, "second commit\n\nsecond body line.")
	write(t, dir, "a.txt", "3")
	head := commitAll(t, dir, "third commit")

	g := newGitRunner(context.Background(), dir)
	msgs, truncated, err := g.commitMessages(base, head, DefaultMaxClaimBytes, DefaultMaxClaimCommits)
	require.NoError(t, err)
	assert.Equal(t, claimsComplete, truncated)
	require.Len(t, msgs, 2, "base itself is not in base..head; only the two commits after it are")
	assert.Equal(t, "second commit\n\nsecond body line.", msgs[0])
	assert.Equal(t, "third commit", msgs[1])
}

// A merge commit's message is git's own boilerplate ("Merge branch 'x'"), not an
// author's claim about the diff. Feeding it to the panel would manufacture a
// claim nobody made, and every such claim is UNSUPPORTED by construction.
func TestCommitMessages_ExcludesMergeCommits(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "1")
	base := commitAll(t, dir, "base commit")

	gitCmd(t, dir, "checkout", "-q", "-b", "side")
	write(t, dir, "side.txt", "s")
	commitAll(t, dir, "side commit")

	gitCmd(t, dir, "checkout", "-q", "main")
	write(t, dir, "main.txt", "m")
	commitAll(t, dir, "main commit")
	gitCmd(t, dir, "merge", "--no-ff", "-q", "-m", "Merge branch 'side'", "side")
	head := gitCmd(t, dir, "rev-parse", "HEAD")

	g := newGitRunner(context.Background(), dir)
	msgs, _, err := g.commitMessages(base, head, DefaultMaxClaimBytes, DefaultMaxClaimCommits)
	require.NoError(t, err)
	for _, m := range msgs {
		assert.NotContains(t, m, "Merge branch")
	}
	assert.Contains(t, msgs, "side commit")
	assert.Contains(t, msgs, "main commit")
}

// The boilerplate assertion above only proves git-shaped merge messages are
// absent, which a message-content filter would also satisfy. --no-merges excludes
// by commit TOPOLOGY, so a merge carrying a substantive, author-written message
// that reads exactly like a claim must be excluded too. That is the case worth
// pinning: such a message is indistinguishable from a real assertion by its text,
// and every claim manufactured from one is UNSUPPORTED by construction.
func TestCommitMessages_ExcludesAMergeWithASubstantiveMessage(t *testing.T) {
	const merged = "Merge feature: adds the cursor fix"
	dir := initRepo(t)
	write(t, dir, "a.txt", "1")
	base := commitAll(t, dir, "base commit")

	gitCmd(t, dir, "checkout", "-q", "-b", "side")
	write(t, dir, "side.txt", "s")
	commitAll(t, dir, "side commit")

	gitCmd(t, dir, "checkout", "-q", "main")
	write(t, dir, "main.txt", "m")
	commitAll(t, dir, "main commit")
	gitCmd(t, dir, "merge", "--no-ff", "-q", "-m", merged, "side")
	head := gitCmd(t, dir, "rev-parse", "HEAD")

	g := newGitRunner(context.Background(), dir)
	msgs, _, err := g.commitMessages(base, head, DefaultMaxClaimBytes, DefaultMaxClaimCommits)
	require.NoError(t, err)
	assert.NotContains(t, msgs, merged,
		"a merge is excluded by topology, not by how its message reads")
	assert.Contains(t, msgs, "side commit")
	assert.Contains(t, msgs, "main commit")
}

func TestCommitMessages_EmptyRangeYieldsNoMessages(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "1")
	head := commitAll(t, dir, "only commit")

	g := newGitRunner(context.Background(), dir)
	msgs, truncated, err := g.commitMessages(head, head, DefaultMaxClaimBytes, DefaultMaxClaimCommits)
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, claimsComplete, truncated)
}

func TestCommitMessages_UnresolvableRangeReturnsError(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "1")
	head := commitAll(t, dir, "only commit")

	g := newGitRunner(context.Background(), dir)
	_, _, err := g.commitMessages("no-such-ref", head, DefaultMaxClaimBytes, DefaultMaxClaimCommits)
	require.Error(t, err, "the low-level read reports the failure; the ledger seam is what swallows it")
}

// The cap must shed the OLDEST commits, never the newest: the claim a reviewer
// most needs to adjudicate is the one the branch tip just made.
func TestCommitMessages_CapShedsOldestAndReportsTruncation(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "0")
	base := commitAll(t, dir, "base commit")
	write(t, dir, "a.txt", "1")
	commitAll(t, dir, "oldest claim "+strings.Repeat("x", 200))
	write(t, dir, "a.txt", "2")
	commitAll(t, dir, "middle claim "+strings.Repeat("y", 200))
	write(t, dir, "a.txt", "3")
	head := commitAll(t, dir, "newest claim")

	g := newGitRunner(context.Background(), dir)
	// Room for the newest message and nothing else.
	msgs, truncated, err := g.commitMessages(base, head, 60, DefaultMaxClaimCommits)
	require.NoError(t, err)
	assert.Equal(t, claimsTruncatedOlder, truncated, "dropping a commit's claims must be recorded, never silent")
	require.Len(t, msgs, 1)
	assert.Equal(t, "newest claim", msgs[0])
}

// A single message larger than the whole cap must still yield its opening bytes
// rather than nothing: an empty ledger on an over-long message would look
// identical to a branch that made no claims at all.
func TestCommitMessages_SingleOversizedMessageIsCappedNotDropped(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "0")
	base := commitAll(t, dir, "base commit")
	write(t, dir, "a.txt", "1")
	head := commitAll(t, dir, "huge claim "+strings.Repeat("z", 500))

	g := newGitRunner(context.Background(), dir)
	msgs, truncated, err := g.commitMessages(base, head, 50, DefaultMaxClaimCommits)
	require.NoError(t, err)
	assert.Equal(t, claimsTruncatedNewest, truncated)
	require.Len(t, msgs, 1)
	assert.LessOrEqual(t, len(msgs[0]), 50)
	assert.True(t, strings.HasPrefix(msgs[0], "huge claim "))
}

// The cap is a byte cap, so a multibyte rune must never be cut in half — an
// invalid-UTF-8 payload section is a rendering hazard for every downstream
// consumer.
func TestCommitMessages_CapNeverSplitsARune(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "0")
	base := commitAll(t, dir, "base commit")
	write(t, dir, "a.txt", "1")
	head := commitAll(t, dir, strings.Repeat("é", 100))

	g := newGitRunner(context.Background(), dir)
	msgs, truncated, err := g.commitMessages(base, head, 51, DefaultMaxClaimCommits) // odd cap, 2-byte runes
	require.NoError(t, err)
	assert.Equal(t, claimsTruncatedNewest, truncated)
	require.Len(t, msgs, 1)
	assert.True(t, utf8.ValidString(msgs[0]), "capped message must remain valid UTF-8")
}

func TestCommitMessages_ZeroMaxBytesMeansUnlimited(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "0")
	base := commitAll(t, dir, "base commit")
	write(t, dir, "a.txt", "1")
	commitAll(t, dir, "claim one "+strings.Repeat("x", 500))
	write(t, dir, "a.txt", "2")
	head := commitAll(t, dir, "claim two "+strings.Repeat("y", 500))

	g := newGitRunner(context.Background(), dir)
	msgs, truncated, err := g.commitMessages(base, head, 0, DefaultMaxClaimCommits)
	require.NoError(t, err)
	assert.Equal(t, claimsComplete, truncated)
	assert.Len(t, msgs, 2)
}

// Message content must not be able to forge a record boundary. An ASCII RS
// (0x1e) in a commit body is the sentinel a printable separator would have
// split on; git's own NUL separator is not forgeable from message text.
func TestCommitMessages_MessageContentCannotForgeARecordBoundary(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "0")
	base := commitAll(t, dir, "base commit")
	write(t, dir, "a.txt", "1")
	head := commitAll(t, dir, "real claim\n\x1eforged second claim")

	g := newGitRunner(context.Background(), dir)
	msgs, _, err := g.commitMessages(base, head, DefaultMaxClaimBytes, DefaultMaxClaimCommits)
	require.NoError(t, err)
	require.Len(t, msgs, 1, "one commit must yield exactly one message regardless of its content")
	assert.Contains(t, msgs[0], "forged second claim")
}

// AC2/AC3 promise byte-identical claims for the same range. i18n.logOutputEncoding
// is a repository-or-developer git config that transcodes the message bytes git
// prints, and production does not neutralize ambient config the way the test
// helper does. Two machines reviewing the same SHA range would otherwise get
// different claim TEXT — and, where a transcode mangles a sentence terminator,
// different claim INDICES, which is exactly the per-agent divergence the ledger
// exists to prevent.
func TestCommitMessages_AreNotTranscodedByAmbientGitConfig(t *testing.T) {
	const msg = "réduire le curseur\n\n- le curseur reste à zéro"
	dir := initRepo(t)
	write(t, dir, "a.txt", "1")
	base := commitAll(t, dir, "seed the file")
	write(t, dir, "a.txt", "2")
	head := commitAll(t, dir, msg)
	gitCmd(t, dir, "config", "i18n.logOutputEncoding", "ISO-8859-1")

	g := newGitRunner(context.Background(), dir)
	msgs, _, err := g.commitMessages(base, head, DefaultMaxClaimBytes, DefaultMaxClaimCommits)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.True(t, utf8.ValidString(msgs[0]), "the read must pin its output encoding, not inherit it")
	assert.Equal(t, msg, msgs[0], "the claim text is the bytes the author wrote")
}

// The byte cap bounds what is RETAINED, not what is read: gitRunner.output
// buffers the whole subprocess stdout before any cap is consulted, so a review
// against a stale base or a fork point would read the entire log body to keep
// 8 KiB of it. The commit bound is applied by git, so those bytes are never
// produced — and a commit dropped by it is the same claim loss the byte cap
// reports, so it must set truncated too.
func TestCommitMessages_BoundsTheReadByCommitCount(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "0")
	base := commitAll(t, dir, "seed the file")
	var head string
	for i := 1; i <= 5; i++ {
		write(t, dir, "a.txt", strings.Repeat("x", i))
		head = commitAll(t, dir, "commit number "+string(rune('0'+i)))
	}

	g := newGitRunner(context.Background(), dir)
	msgs, truncated, err := g.commitMessages(base, head, DefaultMaxClaimBytes, 3)
	require.NoError(t, err)
	require.Len(t, msgs, 3, "the read is bounded at maxCommits")
	assert.Equal(t, "commit number 3", msgs[0], "the OLDEST kept commit; older ones were never read")
	assert.Equal(t, "commit number 5", msgs[2], "the branch tip's claims always survive")
	assert.Equal(t, claimsTruncatedOlder, truncated, "claims were dropped, so the ledger must say so")
}

// The bound must not report truncation on a range that fits inside it.
func TestCommitMessages_CommitBoundDoesNotFalselyReportTruncation(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "0")
	base := commitAll(t, dir, "seed the file")
	write(t, dir, "a.txt", "1")
	head := commitAll(t, dir, "the only claim here")

	g := newGitRunner(context.Background(), dir)
	msgs, truncated, err := g.commitMessages(base, head, DefaultMaxClaimBytes, DefaultMaxClaimCommits)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, claimsComplete, truncated)
}
