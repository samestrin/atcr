package sandbox

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SCOPE OF PROOF (32.3/Q7) — read this before adding anything here.
//
// Every test in this file asserts the SHAPE of a generated profile string or
// argv: which clauses are present, in what order, scoped to which paths. Shape
// is not enforcement. Nothing here runs sandbox-exec or bwrap, so nothing here
// can show that the kernel actually refused a write or a connect — a profile
// can be perfectly shaped and still contain nothing.
//
// The enforcement proof lives ONLY in the //go:build integration tests
// (AC 02-03/02-04), which drive the real binary and actively attempt a
// forbidden write and an outbound connection. A green run of this file is not
// containment evidence and must never be accepted as sign-off for Story 2.
// ---------------------------------------------------------------------------

// profileFixture returns a spec/cfg pair with the two directories the generators
// need, kept distinct and non-nesting so the collision guards are not tripped
// incidentally by the fixture itself.
func profileFixture(t *testing.T) (OSLevelConfig, RunSpec) {
	t.Helper()
	cfg := DefaultOSLevelConfig()
	cfg.ScratchDir = "/private/var/folders/atcr-scratch-abc123"
	return cfg, RunSpec{
		Command:     []string{"go", "test", "./..."},
		SnapshotDir: "/Users/dev/atcr-snapshot-abc123",
	}
}

func TestSandboxExecProfile_DenyDefaultPrecedesEveryAllow(t *testing.T) {
	// AC 02-01 Security Considerations. An allow clause that lands before the
	// deny-default is not a style problem: sandbox-exec applies the last matching
	// rule, so a leading allow with a trailing deny is a containment failure that
	// still "looks like" a deny-default profile to a casual reader.
	cfg, spec := profileFixture(t)

	for name, writable := range map[string]bool{"read-only": false, "writable": true} {
		t.Run(name, func(t *testing.T) {
			spec := spec
			spec.Writable = writable
			profile, err := sandboxExecProfile(cfg, spec)
			require.NoError(t, err)

			denyIdx := strings.Index(profile, "(deny default)")
			require.GreaterOrEqual(t, denyIdx, 0, "the profile must deny by default")
			allowIdx := strings.Index(profile, "(allow ")
			require.GreaterOrEqual(t, allowIdx, 0, "a profile with no allow rule cannot execute anything")
			assert.Less(t, denyIdx, allowIdx, "(deny default) must precede every allow clause")
			assert.True(t, strings.HasPrefix(strings.TrimSpace(profile), "(version 1)"),
				"the profile must open with the version declaration sandbox-exec requires")
		})
	}
}

func TestSandboxExecProfile_BlocksNetworkEgress(t *testing.T) {
	// Belt-and-braces: the absence of an allow under (deny default) is already
	// the enforcement, and the explicit deny is what makes a future refactor that
	// introduces a network allow fail loudly instead of silently opening egress.
	cfg, spec := profileFixture(t)

	for _, writable := range []bool{false, true} {
		spec := spec
		spec.Writable = writable
		profile, err := sandboxExecProfile(cfg, spec)
		require.NoError(t, err)

		assert.Contains(t, profile, "(deny network*)", "the no-egress posture must be explicit in the profile")
		assert.NotContains(t, profile, "(allow network", "no network allow clause of any kind may be emitted")
	}
}

func TestSandboxExecProfile_SnapshotIsReadOnlyForBothWritableValues(t *testing.T) {
	// The corrected AC 02-01 Scenario 1 (2026-08-08): RunSpec.Writable is the
	// single source of truth for writability, and Writable:false means the
	// snapshot is READ-ONLY — DockerBackend mounts it -v snap:/work:ro
	// (docker.go:172-189) and sandbox.go:9 makes it a package MUST. The original
	// scenario granted file-write* on the snapshot for Writable:false, which
	// would have let model-authored code mutate the operator's work tree.
	cfg, spec := profileFixture(t)

	for name, writable := range map[string]bool{"read-only": false, "writable": true} {
		t.Run(name, func(t *testing.T) {
			spec := spec
			spec.Writable = writable
			profile, err := sandboxExecProfile(cfg, spec)
			require.NoError(t, err)

			assert.Contains(t, profile, `(allow file-read* (subpath "`+spec.SnapshotDir+`"))`,
				"the snapshot must be readable")
			for _, line := range strings.Split(profile, "\n") {
				if strings.Contains(line, spec.SnapshotDir) {
					assert.NotContains(t, line, "file-write",
						"no rule may grant write access to the snapshot: %s", line)
				}
			}
		})
	}
}

func TestSandboxExecProfile_WritableRootsAreScratchAndTmpOnly(t *testing.T) {
	// Every file-write* rule must name one of the permitted writable roots. This
	// is asserted as an exhaustive sweep rather than a presence check: a
	// presence check stays green when an EXTRA, wider write rule is added, which
	// is the regression that actually matters.
	cfg, spec := profileFixture(t)
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	permitted := []string{cfg.ScratchDir, "/tmp", "/private/tmp"}
	var writeRules int
	for _, line := range strings.Split(profile, "\n") {
		if !strings.Contains(line, "file-write") {
			continue
		}
		writeRules++
		var ok bool
		for _, root := range permitted {
			if strings.Contains(line, `(subpath "`+root+`")`) {
				ok = true
				break
			}
		}
		assert.True(t, ok, "write rule outside the permitted roots %v: %s", permitted, line)
	}
	assert.GreaterOrEqual(t, writeRules, 2, "the scratch dir and /tmp must both be writable")

	// /tmp is a symlink to /private/tmp on macOS, so a (subpath "/tmp") rule
	// alone resolves to nothing and the carve-out silently does not exist.
	assert.Contains(t, profile, `(subpath "/private/tmp")`,
		"the /tmp carve-out must name the resolved path or it matches nothing")
}

func TestSandboxExecProfile_SystemReadTierIsNarrowAndReadOnly(t *testing.T) {
	// AC 02-01 Scenario 1b. A (deny default) profile cannot exec /bin/sh without
	// these, so Preflight's probe would fail on every macOS host — but the tier
	// must stay the narrow trustedToolDirs-style list, not the whole /System and
	// /Library trees, matching AC 02-02 Scenario 3's standard for Linux.
	cfg, spec := profileFixture(t)
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	for _, dir := range []string{"/usr/lib", "/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		assert.Contains(t, profile, `(allow file-read* (subpath "`+dir+`"))`,
			"%s is required to execute anything under a deny-default profile", dir)
	}
	assert.Contains(t, profile, "dyld", "the dyld shared cache must be readable or no binary can start")

	// The blanket trees, and any path that would expose user data.
	assert.NotContains(t, profile, `(subpath "/System")`, "the whole /System tree is a blanket, not a toolchain path")
	assert.NotContains(t, profile, `(subpath "/Library")`, "the whole /Library tree is a blanket, not a toolchain path")
	assert.NotContains(t, profile, `(subpath "/")`, "a root-level allow defeats the deny-default entirely")
	assert.NotContains(t, profile, "/Users/dev/.ssh")
	assert.NotContains(t, profile, `(subpath "/Users")`, "no rule may cover a user's home tree")
}

func TestSandboxExecProfile_PolicyIsIdenticalAcrossModesAndWritable(t *testing.T) {
	// AC 02-01 Scenario 3. Command vs Script is an argv/stdin distinction that
	// Run makes; it must not move the containment boundary. Writable likewise
	// changes only WHERE the workload is started (Run's concern) — under the
	// corrected Scenario 1 the snapshot is read-only either way, so the policy is
	// the same profile. Asserting byte-equality here means a future edit that
	// makes containment depend on the caller's mode fails immediately.
	cfg, spec := profileFixture(t)

	command, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	script := spec
	script.Command = nil
	script.Script = "go test ./..."
	scriptProfile, err := sandboxExecProfile(cfg, script)
	require.NoError(t, err)
	assert.Equal(t, command, scriptProfile, "Command and Script mode must encode the same policy")

	writable := spec
	writable.Writable = true
	writableProfile, err := sandboxExecProfile(cfg, writable)
	require.NoError(t, err)
	assert.Equal(t, command, writableProfile,
		"sandbox-exec cannot remap paths, so Writable changes the working directory, not the policy")
}

func TestSandboxExecProfile_RejectsProfileDSLMetacharacters(t *testing.T) {
	// AC 02-01 Edge Case 1 / Error Scenario 2. Rejection, not escaping: the
	// profile IS the security boundary, and a hand-written escaper on that
	// boundary is an easy-to-get-subtly-wrong encoder, whereas a path containing
	// a quote or paren is pathological for a snapshot dir. Same severity class
	// as SQL or shell injection.
	cfg, base := profileFixture(t)

	breakouts := map[string]string{
		"double quote":      `/Users/dev/snap"))(allow default)(deny nothing`,
		"open paren":        `/Users/dev/snap(evil`,
		"close paren":       `/Users/dev/snap)evil`,
		"backslash":         `/Users/dev/snap\evil`,
		"newline":           "/Users/dev/snap\nevil",
		"semicolon comment": `/Users/dev/snap;(allow default)`,
	}
	for name, path := range breakouts {
		t.Run("snapshot/"+name, func(t *testing.T) {
			spec := base
			spec.SnapshotDir = path
			profile, err := sandboxExecProfile(cfg, spec)
			require.Error(t, err, "a path that can break out of a (subpath \"...\") literal must be rejected")
			assert.Empty(t, profile, "no partial profile may be returned alongside an error")
		})
		t.Run("scratch/"+name, func(t *testing.T) {
			// The scratch path is interpolated into the same literal and is the
			// one an operator config could reach, so it gets the same treatment.
			cfg := cfg
			cfg.ScratchDir = path
			profile, err := sandboxExecProfile(cfg, base)
			require.Error(t, err)
			assert.Empty(t, profile)
		})
	}
}

func TestSandboxExecProfile_RejectsScratchNestedInSnapshot(t *testing.T) {
	// AC 02-01 Edge Case 2. Decided as reject, not silently relocate: a scratch
	// path under the snapshot means the writable allow rule covers part of the
	// tree the read-only guarantee protects, so the guarantee is subsumed rather
	// than narrowed. Silently moving it would change where the workload's output
	// lands without telling the caller.
	cfg, spec := profileFixture(t)

	for name, scratch := range map[string]string{
		"nested":                     spec.SnapshotDir + "/scratch",
		"identical":                  spec.SnapshotDir,
		"trailing slash":             spec.SnapshotDir + "/",
		"snapshot nested in scratch": "/Users/dev",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := cfg
			cfg.ScratchDir = scratch
			profile, err := sandboxExecProfile(cfg, spec)
			require.Error(t, err, "a scratch path overlapping the snapshot must be rejected")
			assert.Empty(t, profile)
		})
	}

	t.Run("sibling with a shared prefix is allowed", func(t *testing.T) {
		// A string-prefix check would wrongly reject this: "/Users/dev/snap-2"
		// starts with "/Users/dev/snap" without being inside it. Path
		// containment is a component-wise question, not a substring one.
		cfg := cfg
		cfg.ScratchDir = spec.SnapshotDir + "-scratch"
		profile, err := sandboxExecProfile(cfg, spec)
		require.NoError(t, err, "a sibling directory sharing a name prefix is not nested")
		assert.NotEmpty(t, profile)
	})
}

func TestSandboxExecProfile_RejectsRelativeScratch(t *testing.T) {
	// The scratch path is interpolated into a (subpath "...") literal, which
	// sandbox-exec matches against resolved absolute paths; a relative one would
	// silently scope the only writable carve-out to nothing. An EMPTY scratch dir
	// is a different case and is accepted — it omits the carve-out entirely,
	// which is more restrictive, not less (AC 02-01 Edge Case 3, covered by
	// TestSandboxExecProfile_ZeroConfigStillProducesASafeProfile).
	cfg, spec := profileFixture(t)
	for _, bad := range []string{"relative/scratch", "./scratch"} {
		cfg := cfg
		cfg.ScratchDir = bad
		profile, err := sandboxExecProfile(cfg, spec)
		require.Error(t, err, "scratch dir %q must be rejected", bad)
		assert.Empty(t, profile)
	}
}

func TestSandboxExecProfile_ValidatesSpecFirst(t *testing.T) {
	// AC 02-01 Error Scenario 1. validate() runs before anything is built, so no
	// partial profile is ever returned, exactly as dockerRunArgs does
	// (docker.go:136-138).
	cfg, _ := profileFixture(t)
	for name, spec := range map[string]RunSpec{
		"neither command nor script": {SnapshotDir: "/Users/dev/snap"},
		"both command and script":    {Command: []string{"true"}, Script: "echo hi", SnapshotDir: "/Users/dev/snap"},
		"missing snapshot dir":       {Command: []string{"true"}},
		"relative snapshot dir":      {Command: []string{"true"}, SnapshotDir: "relative/snap"},
	} {
		t.Run(name, func(t *testing.T) {
			profile, err := sandboxExecProfile(cfg, spec)
			require.Error(t, err)
			assert.Empty(t, profile)
		})
	}
}

func TestSandboxExecProfile_ZeroConfigStillProducesASafeProfile(t *testing.T) {
	// AC 02-01 Edge Case 3. A zero-value config must not silently degrade the
	// policy — the only thing it lacks is a scratch dir, and a profile with no
	// writable carve-out is more restrictive, not less. It must still be a
	// well-formed deny-default profile rather than a malformed or empty one.
	_, spec := profileFixture(t)
	profile, err := sandboxExecProfile(OSLevelConfig{}, spec)
	require.NoError(t, err, "a zero config has no scratch dir, which is restrictive, not invalid")

	assert.Contains(t, profile, "(deny default)")
	assert.Contains(t, profile, "(deny network*)")
	assert.Contains(t, profile, `(allow file-read* (subpath "`+spec.SnapshotDir+`"))`)
	assert.NotContains(t, profile, `(subpath "")`, "an empty scratch dir must be omitted, never emitted as an empty subpath")
}
