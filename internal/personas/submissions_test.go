package personas

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AC 03-01: `submitted` is not a fourth Source value ----------------------

// sourceSet is the closed set of provenance values PersonaMeta.Source may take.
// "submitted" must NEVER appear here — it is an orthogonal status axis.
var sourceSet = map[string]bool{"built-in": true, "community": true, "project": true}

// TestSubmissionStatus_NotASourceValue covers AC 03-01 Scenarios 1 & 2: with a
// `submitted` marker present on disk (in its own storage dir), every Source
// reported by List/ListTiers is still exactly built-in/community/project — the
// marker's existence never shifts a persona's provenance to a fourth value.
func TestSubmissionStatus_NotASourceValue(t *testing.T) {
	projectDir := t.TempDir()
	communityDir := t.TempDir()
	subDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bruce.md"), []byte("# project bruce\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(communityDir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "security", "owasp.yaml"), []byte(validPersonaYAML), 0o644))

	// A submitted marker exists for the community persona.
	require.NoError(t, WriteSubmissionMarker(subDir, SubmissionStatus{
		Persona: "security/owasp", Version: "1.0.0", Submitter: "octocat",
		FixturePassed: true, SubmittedAt: time.Now().UTC(),
	}))

	metas, err := ListTiers(projectDir, communityDir)
	require.NoError(t, err)
	require.NotEmpty(t, metas)
	for _, m := range metas {
		assert.Truef(t, sourceSet[m.Source], "persona %q has out-of-set Source %q", m.Name, m.Source)
		assert.NotEqual(t, "submitted", m.Source, "submitted must never be a Source value")
	}
}

// TestList_IdenticalWithAndWithoutMarkers covers AC 03-01 Edge Cases 1 & 2 and
// AC 03-03 Scenario 2: List/ListTiers return byte-for-byte identical slices
// whether or not `submitted` markers (including a malformed one) exist on disk.
func TestList_IdenticalWithAndWithoutMarkers(t *testing.T) {
	projectDir := t.TempDir()
	communityDir := t.TempDir()
	subDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bruce.md"), []byte("# project bruce\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(communityDir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "security", "owasp.yaml"), []byte(validPersonaYAML), 0o644))

	before, err := ListTiers(projectDir, communityDir)
	require.NoError(t, err)

	// Write one valid and one malformed marker in the separate storage dir.
	require.NoError(t, WriteSubmissionMarker(subDir, SubmissionStatus{
		Persona: "security/owasp", Version: "1.0.0", Submitter: "octocat", FixturePassed: true, SubmittedAt: time.Now().UTC(),
	}))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "garbage.yaml"), []byte("::: not: valid: yaml"), 0o600))

	after, err := ListTiers(projectDir, communityDir)
	require.NoError(t, err)
	assert.Equal(t, before, after, "List output must not change because markers exist")
}

// --- AC 03-02: attribution + atomic persistence ------------------------------

// TestWriteSubmissionMarker_Attribution covers AC 03-02 Scenario 1: the marker
// round-trips with submitter identity, persona name+version, timestamp, and the
// fixture-pass flag.
func TestWriteSubmissionMarker_Attribution(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC().Truncate(time.Second)
	in := SubmissionStatus{Persona: "sasha", Version: "2.1.0", Submitter: "octocat", FixturePassed: true, SubmittedAt: ts}
	require.NoError(t, WriteSubmissionMarker(dir, in))

	got, ok, err := ReadSubmission(dir, "sasha")
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, "sasha", got.Persona)
	assert.Equal(t, "2.1.0", got.Version)
	assert.Equal(t, "octocat", got.Submitter)
	assert.True(t, got.FixturePassed)
	assert.True(t, ts.Equal(got.SubmittedAt), "timestamp round-trips")
}

// TestWriteSubmissionMarker_CreatesDirOnFirstRun covers AC 03-02 Scenario 3: the
// storage directory is created (MkdirAll) on the first-ever submission.
func TestWriteSubmissionMarker_CreatesDirOnFirstRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")
	require.NoError(t, WriteSubmissionMarker(dir, SubmissionStatus{Persona: "sasha", Submitter: "octocat", FixturePassed: true, SubmittedAt: time.Now().UTC()}))
	_, err := os.Stat(filepath.Join(dir, "sasha.yaml"))
	require.NoError(t, err, "marker written after creating the missing storage dir")
}

// TestWriteSubmissionMarker_Perms0700Dir confirms the created storage dir is 0700.
func TestWriteSubmissionMarker_Perms0700Dir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "submissions")
	require.NoError(t, WriteSubmissionMarker(dir, SubmissionStatus{Persona: "sasha", Submitter: "octocat", FixturePassed: true, SubmittedAt: time.Now().UTC()}))
	fi, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
}

// TestWriteSubmissionMarker_RefusesSymlink covers AC 03-02 Edge Case 1: a
// pre-planted symlink at the marker destination is refused (writeFileAtomic's
// Lstat guard), and nothing is written through it.
func TestWriteSubmissionMarker_RefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "evil.yaml")
	require.NoError(t, os.WriteFile(target, []byte("pre-existing\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "sasha.yaml")))

	err := WriteSubmissionMarker(dir, SubmissionStatus{Persona: "sasha", Submitter: "octocat", FixturePassed: true, SubmittedAt: time.Now().UTC()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
	data, rerr := os.ReadFile(target)
	require.NoError(t, rerr)
	assert.Equal(t, "pre-existing\n", string(data), "no data written through the symlink")
}

// TestWriteSubmissionMarker_RefusesSymlinkedIntermediate covers the 3.2.A LOW fix:
// for a namespaced name, a symlink pre-planted at an INTERMEDIATE directory
// component is refused (mirroring writePersonaUnit), so the marker write cannot be
// redirected outside the storage dir even though the leaf Lstat alone would not
// catch it.
func TestWriteSubmissionMarker_RefusesSymlinkedIntermediate(t *testing.T) {
	dir := t.TempDir()
	elsewhere := t.TempDir()
	// Plant a symlink at the intermediate "team" component the namespaced name needs.
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(dir, "team")))

	err := WriteSubmissionMarker(dir, SubmissionStatus{Persona: "team/reviewer", Submitter: "octocat", FixturePassed: true, SubmittedAt: time.Now().UTC()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlinked path component")
	// Nothing was written through the symlink into the outside directory.
	_, statErr := os.Stat(filepath.Join(elsewhere, "reviewer.yaml"))
	assert.True(t, os.IsNotExist(statErr), "no marker written through the intermediate symlink")
}

// TestWriteSubmissionMarker_ResubmitOverwrites covers AC 03-02 Edge Case 2: a
// re-submission atomically replaces the prior marker with a refreshed timestamp.
func TestWriteSubmissionMarker_ResubmitOverwrites(t *testing.T) {
	dir := t.TempDir()
	first := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	second := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, WriteSubmissionMarker(dir, SubmissionStatus{Persona: "sasha", Version: "1.0.0", Submitter: "octocat", FixturePassed: true, SubmittedAt: first}))
	require.NoError(t, WriteSubmissionMarker(dir, SubmissionStatus{Persona: "sasha", Version: "2.0.0", Submitter: "octocat", FixturePassed: true, SubmittedAt: second}))

	got, ok, err := ReadSubmission(dir, "sasha")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "2.0.0", got.Version, "re-submission replaces the marker")
	assert.True(t, second.Equal(got.SubmittedAt), "timestamp refreshed")
}

// TestWriteSubmissionMarker_InvalidNameRejected confirms a path-traversal persona
// name is rejected before any write, mirroring the install/submit name guard.
func TestWriteSubmissionMarker_InvalidNameRejected(t *testing.T) {
	dir := t.TempDir()
	err := WriteSubmissionMarker(dir, SubmissionStatus{Persona: "../escape", Submitter: "octocat", FixturePassed: true, SubmittedAt: time.Now().UTC()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid persona name")
}

// --- AC 03-03: storage outside community tree + extension point --------------

// TestSubmissionsDir_OutsideCommunityTree covers AC 03-03 Scenario 1 / Error
// Scenario 2: the submissions storage dir is a sibling of PersonasDir() and never
// resolves under personas/community/.
func TestSubmissionsDir_OutsideCommunityTree(t *testing.T) {
	subDir, err := SubmissionsDir()
	require.NoError(t, err)
	personasDir, err := PersonasDir()
	require.NoError(t, err)

	assert.Equal(t, "submissions", filepath.Base(subDir))
	assert.Equal(t, filepath.Dir(personasDir), filepath.Dir(subDir), "submissions sits beside personas")

	community := filepath.Join(personasDir, "community")
	rel, err := filepath.Rel(community, subDir)
	require.NoError(t, err)
	assert.True(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)),
		"submissions dir %q must not resolve under community tree %q", subDir, community)
}

// TestMarkerPathNeverUnderCommunity covers AC 03-03 Scenario 1 & Edge Case 2: for
// flat and namespaced names the written marker stays within the storage dir and
// outside a sibling community tree.
func TestMarkerPathNeverUnderCommunity(t *testing.T) {
	base := t.TempDir()
	subDir := filepath.Join(base, "submissions")
	community := filepath.Join(base, "personas", "community")
	require.NoError(t, os.MkdirAll(community, 0o755))

	for _, name := range []string{"sasha", "team/reviewer", "a/b/c"} {
		require.NoError(t, WriteSubmissionMarker(subDir, SubmissionStatus{Persona: name, Submitter: "octocat", FixturePassed: true, SubmittedAt: time.Now().UTC()}))
		markerPath := filepath.Join(subDir, filepath.FromSlash(name)+".yaml")
		_, err := os.Stat(markerPath)
		require.NoErrorf(t, err, "marker for %q written within storage dir", name)

		rel, err := filepath.Rel(subDir, markerPath)
		require.NoError(t, err)
		assert.False(t, strings.HasPrefix(rel, ".."), "marker for %q stays within storage dir", name)

		relToCommunity, err := filepath.Rel(community, markerPath)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(relToCommunity, ".."), "marker for %q must be outside community tree", name)
	}
}

// TestReadSubmission_NotSubmitted covers AC 03-03 Edge Case 1: querying a persona
// with no marker returns a clear zero-value (nil, false) result, never an error.
func TestReadSubmission_NotSubmitted(t *testing.T) {
	got, ok, err := ReadSubmission(t.TempDir(), "never-submitted")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, got)
}

// TestListSubmissions_ExtensionPoint confirms the separately-named extension point
// surfaces submitted status without touching List/ListTiers.
func TestListSubmissions_ExtensionPoint(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteSubmissionMarker(dir, SubmissionStatus{Persona: "sasha", Version: "1.0.0", Submitter: "octocat", FixturePassed: true, SubmittedAt: time.Now().UTC()}))
	require.NoError(t, WriteSubmissionMarker(dir, SubmissionStatus{Persona: "security/owasp", Version: "2.0.0", Submitter: "hubot", FixturePassed: true, SubmittedAt: time.Now().UTC()}))

	subs, err := ListSubmissions(dir)
	require.NoError(t, err)
	byName := map[string]SubmissionStatus{}
	for _, s := range subs {
		byName[s.Persona] = s
	}
	require.Contains(t, byName, "sasha")
	require.Contains(t, byName, "security/owasp")
	assert.Equal(t, "octocat", byName["sasha"].Submitter)
	assert.Equal(t, "hubot", byName["security/owasp"].Submitter)
}

// TestListSubmissions_EmptyDir returns no rows and no error when the storage dir
// does not exist yet (no submissions have ever been made).
func TestListSubmissions_EmptyDir(t *testing.T) {
	subs, err := ListSubmissions(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	assert.Empty(t, subs)
}

// --- Wiring: marker fires only after a successful PR -------------------------

// TestSubmit_WritesMarkerAfterPR covers the Story 3 wiring: on a successful PR,
// Submit records a `submitted` marker whose submitter is the fork owner (from the
// push head) and whose version comes from the resolved persona unit.
func TestSubmit_WritesMarkerAfterPR(t *testing.T) {
	personasDir := t.TempDir()
	subDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(personasDir, "sasha.yaml"), []byte(validPersonaYAML), 0o600))
	s := &stubSubmitter{pushHead: "octocat:persona-submit/sasha", prURL: "https://github.com/samestrin/atcr/pull/42"}

	url, err := Submit(context.Background(), s, personasDir, subDir, "sasha")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/samestrin/atcr/pull/42", url)

	got, ok, err := ReadSubmission(subDir, "sasha")
	require.NoError(t, err)
	require.True(t, ok, "a successful PR records a submitted marker")
	assert.Equal(t, "octocat", got.Submitter, "submitter is the fork owner from the push head")
	assert.Equal(t, "1.0.0", got.Version, "version read from the resolved persona unit")
	assert.True(t, got.FixturePassed)
	assert.False(t, got.SubmittedAt.IsZero(), "timestamp stamped")
}

// TestSubmit_NoMarkerWhenPRFails covers AC 03-02 CONTRACT (no orphan marker): a
// PR-create failure leaves no submitted marker on disk.
func TestSubmit_NoMarkerWhenPRFails(t *testing.T) {
	subDir := t.TempDir()
	s := &stubSubmitter{pushHead: "octocat:persona-submit/sasha", prErr: errors.New("could not create pull request")}

	_, err := Submit(context.Background(), s, t.TempDir(), subDir, "sasha")
	require.Error(t, err)
	_, ok, rerr := ReadSubmission(subDir, "sasha")
	require.NoError(t, rerr)
	assert.False(t, ok, "no marker written when PR creation failed")
}

// TestSubmit_MarkerFailureIncludesPRURL covers the recorded Phase 3 clarification:
// a marker-write failure AFTER a successful PR returns a non-nil error (exit
// non-zero per AC 03-02), but the error embeds the already-created PR URL so a
// live PR is not misread as total failure.
func TestSubmit_MarkerFailureIncludesPRURL(t *testing.T) {
	personasDir := t.TempDir()
	subDir := t.TempDir()
	// Plant a symlink at the marker destination so the atomic write is refused.
	target := filepath.Join(t.TempDir(), "target.yaml")
	require.NoError(t, os.WriteFile(target, []byte("x\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(subDir, "sasha.yaml")))
	s := &stubSubmitter{pushHead: "octocat:persona-submit/sasha", prURL: "https://github.com/samestrin/atcr/pull/42"}

	url, err := Submit(context.Background(), s, personasDir, subDir, "sasha")
	require.Error(t, err)
	assert.Empty(t, url, "no URL returned on error, but the message carries it")
	assert.Contains(t, err.Error(), "https://github.com/samestrin/atcr/pull/42", "error surfaces the created PR URL")
	assert.Contains(t, err.Error(), "symlink", "underlying marker-write cause preserved")
}
