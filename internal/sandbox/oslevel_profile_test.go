package sandbox

import (
	"fmt"
	"path/filepath"
	"regexp"
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
	// Pinned, not inherited from DefaultOSLevelConfig: that value comes from
	// os.UserCacheDir(), so leaving it would make every profile assertion depend
	// on the running user's home path — and on a host where UserCacheDir fails,
	// the carve-out would silently vanish from the fixture entirely.
	cfg.ModuleCacheDir = "/private/var/folders/atcr-modcache-abc123"
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

// writeAllowSubpaths extracts the subpath of every ALLOW rule granting
// file-write in the profile. Deny rules are excluded deliberately: this is the
// set of paths the profile makes writable, which is the set the invariants
// below are about.
func writeAllowSubpaths(t *testing.T, profile string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(profile, "\n") {
		if !strings.HasPrefix(line, "(allow ") || !strings.Contains(line, "file-write") {
			continue
		}
		start := strings.Index(line, `(subpath "`)
		if start < 0 {
			continue // a (literal "...") rule, e.g. /dev/null
		}
		rest := line[start+len(`(subpath "`):]
		end := strings.Index(rest, `"`)
		require.GreaterOrEqual(t, end, 0, "malformed subpath clause: %s", line)
		out = append(out, rest[:end])
	}
	return out
}

// assertPathNotWritable is the invariant form of "the snapshot is read-only":
// no write-allow rule may cover it, whether by naming it or by naming any
// ancestor of it. The earlier string-matching version of this check inspected
// only lines containing the snapshot path, so a `/private/tmp` write rule
// covering a `/private/tmp/...` snapshot was invisible to it — and that is
// where os.MkdirTemp actually puts snapshots.
// A snapshot under /tmp IS covered by the unconditional /tmp write-allow, and
// that cannot be avoided without refusing the most common snapshot location
// there is. What makes it read-only anyway is sandbox-exec's last-match-wins
// evaluation: the trailing deny is the final rule, so it overrides every
// covering allow above it. The invariant is therefore about ORDER, not about
// the absence of a covering allow — verified against the real binary, where a
// write to a /private/tmp snapshot fails with "Operation not permitted" while a
// write to the scratch dir succeeds.
func assertPathNotWritable(t *testing.T, profile, path string) {
	t.Helper()
	assert.Contains(t, profile, `(deny file-write* (subpath "`+path+`"))`,
		"the trailing deny is what re-asserts read-only under last-match-wins")
	lastDeny := strings.LastIndex(profile, `(deny file-write* (subpath "`+path+`"))`)
	for _, granted := range writeAllowSubpaths(t, profile) {
		// Anchor on "(allow ": when the snapshot IS a writable root (a snapshot
		// at /private/tmp itself), the deny line also contains
		// `file-write* (subpath "…")`, and a bare substring search would find
		// the deny and compare it against itself.
		idx := strings.LastIndex(profile, `(allow file-read* file-write* (subpath "`+granted+`")`)
		assert.Less(t, idx, lastDeny,
			"sandbox-exec applies the last matching rule, so the snapshot deny must come after every write allow")
	}
}

func TestSandboxExecProfile_SnapshotIsReadOnlyForBothWritableValues(t *testing.T) {
	// The corrected AC 02-01 Scenario 1 (2026-08-08): RunSpec.Writable is the
	// single source of truth for writability, and Writable:false means the
	// snapshot is READ-ONLY — DockerBackend mounts it -v snap:/work:ro
	// (docker.go:172-189) and sandbox.go:9 makes it a package MUST. The original
	// scenario granted file-write* on the snapshot for Writable:false, which
	// would have let model-authored code mutate the operator's work tree.
	//
	// Parameterised over where a snapshot REALLY lands, not just a tidy fixture:
	// os.MkdirTemp("", …) returns a path under /var/folders on a normal macOS
	// host and under /tmp whenever TMPDIR is unset (launchd, cron, CI), and both
	// of those sit inside a writable root.
	cfg, base := profileFixture(t)

	snapshots := map[string]string{
		"home-style fixture": "/Users/dev/atcr-snapshot-abc123",
		"under /tmp":         "/tmp/atcr-snapshot-abc123",
		"under /private/tmp": "/private/tmp/atcr-snapshot-abc123",
		"under TMPDIR alias": "/var/folders/qq/T/atcr-snapshot-abc123",
	}
	for name, snapshot := range snapshots {
		for _, writable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/writable=%t", name, writable), func(t *testing.T) {
				spec := base
				spec.SnapshotDir = snapshot
				spec.Writable = writable
				profile, err := sandboxExecProfile(cfg, spec)
				require.NoError(t, err)

				// The emitted path is the RESOLVED one: sandbox-exec matches
				// resolved paths, so a rule naming /var/... or /tmp/... matches
				// nothing at all and the carve-out silently does not exist.
				resolved := snapshot
				for prefix, target := range map[string]string{"/tmp": "/private/tmp", "/var": "/private/var"} {
					if resolved == prefix {
						resolved = target
					} else if strings.HasPrefix(resolved, prefix+"/") {
						resolved = target + strings.TrimPrefix(resolved, prefix)
					}
				}
				assert.Contains(t, profile, `(allow file-read* (subpath "`+resolved+`"))`,
					"the snapshot rule must name the resolved path or it grants nothing")
				assertPathNotWritable(t, profile, resolved)
			})
		}
	}
}

func TestSandboxExecProfile_NoRuleIsRootedAtTheFilesystemRoot(t *testing.T) {
	// (subpath "/") grants the whole filesystem and, under last-match-wins,
	// overrides the deny-default entirely. This asserts the invariant against
	// the inputs that can actually produce it — the previous version asserted it
	// only against a fixture that never could, so the guard was untested and in
	// fact broken: ScratchDir="/" passed the nesting check and emitted exactly
	// this rule.
	cfg, base := profileFixture(t)

	for _, scratch := range []string{"/", "//", "/.", "/usr", "/usr/bin", "/private"} {
		t.Run("scratch="+scratch, func(t *testing.T) {
			cfg := cfg
			cfg.ScratchDir = scratch
			profile, err := sandboxExecProfile(cfg, base)
			require.Error(t, err, "a writable root at or above the system tier must be rejected")
			assert.Empty(t, profile)
		})
	}

	// And the invariant itself, on the accepted path.
	profile, err := sandboxExecProfile(cfg, base)
	require.NoError(t, err)
	for _, granted := range writeAllowSubpaths(t, profile) {
		assert.NotEqual(t, "/", granted, "no write rule may be rooted at /")
	}
	assert.NotContains(t, profile, `(subpath "/")`,
		"the root must be granted as a (literal \"/\"), which is the directory entry alone, never as a subtree")
}

func TestSandboxExecProfile_ResolvesSymlinkedSystemPrefixes(t *testing.T) {
	// sandbox-exec matches resolved paths. On a normal macOS host TMPDIR is
	// /var/folders/... and /var is a symlink to /private/var, so an unresolved
	// rule grants nothing — Preflight would still go green while the snapshot
	// carve-out was dead. Prefix normalization is pure (no filesystem access),
	// which is what keeps the generator assertable without a real host.
	cfg, base := profileFixture(t)
	for input, want := range map[string]string{
		"/tmp/snap":            "/private/tmp/snap",
		"/var/folders/qq/snap": "/private/var/folders/qq/snap",
		"/etc/snap":            "/private/etc/snap",
		"/private/tmp/snap":    "/private/tmp/snap",
		"/Users/dev/snap":      "/Users/dev/snap",
	} {
		t.Run(input, func(t *testing.T) {
			spec := base
			spec.SnapshotDir = input
			profile, err := sandboxExecProfile(cfg, spec)
			require.NoError(t, err)
			assert.Contains(t, profile, `(allow file-read* (subpath "`+want+`"))`)
		})
	}
}

// clauseOperands returns the operands of kind ("subpath" or "literal") on the
// single profile line containing marker.
//
// The whole file-read-metadata clause is emitted as ONE line, so a per-line
// assert.Contains(line, "(subpath ") is satisfied by any single scoped entry on
// it — appending (subpath "/Users") to the same clause keeps such a check green
// while restoring exactly the `stat ~/.ssh/id_ed25519` capability the clause was
// scoped to remove. Reading the operands back as a SET is what lets the test
// assert containment of the whole clause instead of the presence of one entry.
func clauseOperands(t *testing.T, profile, marker, kind string) []string {
	t.Helper()
	var line string
	for _, l := range strings.Split(profile, "\n") {
		if strings.Contains(l, marker) {
			require.Empty(t, line, "more than one %s clause: the set assertion below would only cover the first", marker)
			line = l
		}
	}
	require.NotEmpty(t, line, "no %s clause in the profile", marker)

	var out []string
	for _, m := range regexp.MustCompile(`\(`+kind+` "([^"]*)"\)`).FindAllStringSubmatch(line, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestSandboxExecProfile_ScopesMetadataReadsAwayFromUserData(t *testing.T) {
	// An unscoped (allow file-read-metadata) let a workload stat
	// ~/.ssh/id_ed25519 and read back its size and mtime. No content, but enough
	// to enumerate which secrets an operator holds — and the code comment
	// claimed the opposite ("nothing here covers user data").
	cfg, spec := profileFixture(t)
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	// Exhaustive: every subtree the metadata clause scopes to must be one the
	// generator is entitled to grant. A presence check stays green when an EXTRA,
	// wider scope is added, which is the regression that actually matters here —
	// the clause grants stat() over whatever it names.
	// The fixture's two directories are already in canonical spelling, so they
	// travel into the clause unchanged; the fixed lists are emitted verbatim.
	permitted := map[string]bool{"/dev": true, spec.SnapshotDir: true, cfg.ScratchDir: true, cfg.ModuleCacheDir: true}
	for _, d := range append(append([]string{}, darwinSystemReadDirs...), darwinTmpDirs...) {
		permitted[d] = true
	}

	granted := clauseOperands(t, profile, "file-read-metadata", "subpath")
	require.NotEmpty(t, granted, "metadata reads must be path-scoped, not global")
	for _, g := range granted {
		assert.NotEqual(t, "/", g, "a root subtree scope is not a scope")
		assert.True(t, permitted[g],
			"metadata clause scopes to a subtree outside the permitted set: %q", g)
	}
	// The user-data trees the scoping exists to exclude, named explicitly so the
	// intent survives a future widening of `permitted`.
	for _, forbidden := range []string{"/Users", "/Users/dev", "/private/var/root", "/private/etc"} {
		assert.NotContains(t, granted, forbidden, "the metadata clause must not cover user data")
	}
}

func TestSandboxExecProfile_MetadataLiteralsAreEmittedOnce(t *testing.T) {
	// The clause's dedup justifies itself by profile size in ONE argv element, so
	// "deduped" has to mean every literal in the clause, not only the ancestor
	// chains deduped against each other. Swept over inputs whose ancestor chains
	// overlap each other and overlap the symlink-alias and root literals emitted
	// ahead of them.
	for _, tc := range []struct{ name, snapshot, scratch string }{
		{"shared /private/tmp prefix", "/tmp/atcr-snap", "/private/tmp/atcr-scratch"},
		{"shared /var/folders prefix", "/var/folders/qq/snap", "/var/folders/qq/scratch"},
		{"alias-spelled inputs", "/etc/x/snap", "/tmp/scratch"},
		{"disjoint trees", "/Users/dev/snap", "/private/var/folders/scratch"},
		{"no scratch carve-out", "/tmp/atcr-snap", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultOSLevelConfig()
			cfg.ScratchDir = tc.scratch
			profile, err := sandboxExecProfile(cfg, RunSpec{Command: []string{"true"}, SnapshotDir: tc.snapshot})
			require.NoError(t, err)

			seen := map[string]int{}
			for _, lit := range clauseOperands(t, profile, "file-read-metadata", "literal") {
				seen[lit]++
			}
			require.NotEmpty(t, seen, "the clause must carry the root and ancestor literals")
			for lit, n := range seen {
				assert.Equal(t, 1, n, "literal %q emitted %d times in the metadata clause", lit, n)
			}
		})
	}
}

func TestSandboxExecProfile_GrantsTheDevicesAToolchainNeeds(t *testing.T) {
	// Without /dev/null a shell redirect fails outright and `go vet` aborts
	// before running — a sandbox that cannot run the project's validate commands
	// is broken, not conservative. /dev/null takes file-write-data (writing into
	// the device) rather than file-write* (creating or unlinking device nodes).
	cfg, spec := profileFixture(t)
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	assert.Contains(t, profile, `(allow file-read* file-write-data (literal "/dev/null"))`)
	assert.Contains(t, profile, `(allow file-read* (literal "/dev/urandom"))`)
	assert.NotContains(t, profile, `(subpath "/dev")`+"\n", "the whole /dev tree is not a device allowlist")
}

func TestSandboxExecProfile_GrantsTheToolchainPrefixesReadOnly(t *testing.T) {
	// Measured: without these, `git`, `make`, `python3` and `go vet` all fail
	// under the profile (`unable to load libxcrun`). They are read-only — being
	// able to READ /opt/homebrew is what lets the workload run its tools, while
	// the absence of a write rule keeps a compromised run from modifying the
	// operator's toolchain.
	cfg, spec := profileFixture(t)
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	for _, dir := range []string{"/Library/Developer/CommandLineTools", "/opt/homebrew", "/usr/local"} {
		assert.Contains(t, profile, `(allow file-read* (subpath "`+dir+`"))`)
		for _, granted := range writeAllowSubpaths(t, profile) {
			assert.False(t, pathContains(granted, dir),
				"write-allow on %q covers the toolchain path %q", granted, dir)
		}
	}
}

func TestSandboxExecProfile_TheScratchDirIsTheOnlyWritableRoot(t *testing.T) {
	// Every file-write* rule must name the per-run scratch dir and nothing else.
	// Asserted as an exhaustive sweep rather than a presence check: a presence
	// check stays green when an EXTRA, wider write rule is added, which is the
	// regression that actually matters.
	//
	// The host /tmp used to be an unconditional writable root here, which handed
	// every run read AND write over a world-writable directory shared with every
	// other process on the machine — the predictable-path and symlink-race
	// surface, plus visibility of other processes' temp files. The Linux side
	// never had it (bwrapArgs mounts a fresh --tmpfs /tmp), so this also closes
	// a real cross-platform posture gap rather than merely narrowing macOS.
	// sandboxEnv points HOME/TMPDIR/GOTMPDIR/GOCACHE/XDG_CACHE_HOME at the
	// scratch tree, so a toolchain following the environment has everywhere it
	// needs; one that hardcodes bare /tmp now fails closed, which is the safe
	// direction and is measured against the real binary in the integration
	// suite rather than assumed here.
	cfg, spec := profileFixture(t)
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	granted := writeAllowSubpaths(t, profile)
	assert.Equal(t, []string{cfg.ScratchDir}, granted,
		"the scratch carve-out must be the one and only writable subtree")
	for _, tmp := range darwinTmpDirs {
		assert.NotContains(t, granted, tmp, "the host temp roots must not be writable")
	}

	// The only non-subtree write is the /dev/null device, and it is write-DATA
	// (writing into the device), never write* (creating or unlinking nodes).
	assert.Contains(t, profile, `(allow file-read* file-write-data (literal "/dev/null"))`)
	for _, line := range strings.Split(profile, "\n") {
		if strings.HasPrefix(line, "(allow ") && strings.Contains(line, "(literal ") && strings.Contains(line, "file-write") {
			assert.Contains(t, line, "/dev/null", "the only literal write target is /dev/null: %s", line)
			assert.NotContains(t, line, "file-write*", "a device literal must not carry file-write*: %s", line)
		}
	}
}

func TestProfiles_ModuleCacheIsReadOnlyAndGuardedOnBothPlatforms(t *testing.T) {
	// The module cache is the ONE carve-out that outlives the run, so it is also
	// the one whose misconfiguration persists. Two properties matter and both are
	// asserted on each platform: it is never writable (a persistent directory the
	// workload can write is a cache-poisoning primitive that feeds every later
	// build), and it clears the same path guards the read-only snapshot does.

	t.Run("darwin read-only", func(t *testing.T) {
		cfg, spec := profileFixture(t)
		profile, err := sandboxExecProfile(cfg, spec)
		require.NoError(t, err)

		assert.Contains(t, profile, `(allow file-read* (subpath "`+cfg.ModuleCacheDir+`"))`,
			"the module cache must be readable or GOMODCACHE resolves to nothing")
		assert.NotContains(t, writeAllowSubpaths(t, profile), cfg.ModuleCacheDir,
			"the module cache must never be writable: a poisoned entry would outlive the run")
	})

	t.Run("linux read-only", func(t *testing.T) {
		cfg, spec := bwrapFixture(t)
		argv, err := bwrapArgs(cfg, spec)
		require.NoError(t, err)

		assert.Contains(t, argvPairs(t, argv, "--ro-bind"), [2]string{cfg.ModuleCacheDir, cfg.ModuleCacheDir},
			"the module cache must be bound at its own path so GOMODCACHE resolves identically inside and out")
		for _, pair := range argvPairs(t, argv, "--bind") {
			assert.NotEqual(t, cfg.ModuleCacheDir, pair[0],
				"the module cache must never be bound writable: %v", pair)
		}
	})

	t.Run("guards reject a dangerous cache path", func(t *testing.T) {
		// Same standard as SnapshotDir: not the root, not a system tree, not a
		// home TREE, and disjoint from the run's own two directories.
		darwinCfg, darwinSpec := profileFixture(t)
		linuxCfg, linuxSpec := bwrapFixture(t)

		// The home-container spelling is platform-specific by design (/Users on
		// darwin, /home on linux), so each list is driven with its own.
		for _, bad := range []string{"/", "/usr", "/Users"} {
			t.Run("darwin "+bad, func(t *testing.T) {
				cfg := darwinCfg
				cfg.ModuleCacheDir = bad
				_, err := sandboxExecProfile(cfg, darwinSpec)
				require.Error(t, err, "ModuleCacheDir %q must be refused", bad)
			})
		}
		for _, bad := range []string{"/", "/usr", "/home", "/var/home"} {
			t.Run("linux "+bad, func(t *testing.T) {
				cfg := linuxCfg
				cfg.ModuleCacheDir = bad
				_, err := bwrapArgs(cfg, linuxSpec)
				require.Error(t, err, "ModuleCacheDir %q must be refused", bad)
			})
		}

		// Overlapping the snapshot or the scratch dir is refused too: the cache
		// is read-only, so nesting it inside the writable scratch tree would
		// hand the workload write access to it by the other rule.
		t.Run("darwin overlaps scratch", func(t *testing.T) {
			cfg := darwinCfg
			cfg.ModuleCacheDir = filepath.Join(cfg.ScratchDir, "modcache")
			_, err := sandboxExecProfile(cfg, darwinSpec)
			require.Error(t, err)
		})
		t.Run("linux overlaps scratch", func(t *testing.T) {
			cfg := linuxCfg
			cfg.ModuleCacheDir = filepath.Join(cfg.ScratchDir, "modcache")
			_, err := bwrapArgs(cfg, linuxSpec)
			require.Error(t, err)
		})
		t.Run("darwin overlaps snapshot", func(t *testing.T) {
			cfg := darwinCfg
			cfg.ModuleCacheDir = filepath.Join(darwinSpec.SnapshotDir, "modcache")
			_, err := sandboxExecProfile(cfg, darwinSpec)
			require.Error(t, err)
		})
		t.Run("linux overlaps snapshot", func(t *testing.T) {
			cfg := linuxCfg
			cfg.ModuleCacheDir = filepath.Join(linuxSpec.SnapshotDir, "modcache")
			_, err := bwrapArgs(cfg, linuxSpec)
			require.Error(t, err)
		})
	})

	t.Run("empty disables the carve-out entirely", func(t *testing.T) {
		// The pre-existing behavior must remain reachable, and "disabled" must
		// mean no rule at all rather than a rule naming "".
		cfg, spec := profileFixture(t)
		cfg.ModuleCacheDir = ""
		profile, err := sandboxExecProfile(cfg, spec)
		require.NoError(t, err)
		assert.NotContains(t, profile, `(subpath "")`)

		lcfg, lspec := bwrapFixture(t)
		lcfg.ModuleCacheDir = ""
		argv, err := bwrapArgs(lcfg, lspec)
		require.NoError(t, err)
		for _, flag := range []string{"--bind", "--ro-bind", "--ro-bind-try"} {
			for _, pair := range argvPairs(t, argv, flag) {
				assert.NotEmpty(t, pair[0], "an empty module cache must emit no mount at all")
			}
		}
	})
}

func TestSandboxExecProfile_HostTempRootsKeepTheirNonWriteRoles(t *testing.T) {
	// Removing the /tmp WRITE rule must not quietly remove the three other jobs
	// darwinTmpDirs does. Each is a separate guarantee and each has its own
	// failure mode if it goes with the write rule:
	//
	//   - metadata scope: path resolution through /private/tmp
	//   - darwinSymlinkAliases: the (literal "/tmp") symlink NODE, without which
	//     every access spelled /tmp/... fails at the first component even when
	//     the resolved target is granted
	//   - the guards: a snapshot or scratch that IS a host temp root
	cfg, spec := profileFixture(t)
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	metadata := clauseOperands(t, profile, "file-read-metadata", "subpath")
	for _, tmp := range darwinTmpDirs {
		assert.Contains(t, metadata, tmp,
			"the host temp roots must keep metadata scope for path resolution")
	}
	assert.Contains(t, clauseOperands(t, profile, "file-read-metadata", "literal"), "/tmp",
		"the /tmp symlink NODE literal is now the only thing resolving that spelling and must survive")

	// A scratch dir that IS a host temp root is still refused: it would grant
	// write over the whole shared directory, which is what this change removed.
	for _, tmp := range darwinTmpDirs {
		cfg := cfg
		cfg.ScratchDir = tmp
		_, err := sandboxExecProfile(cfg, spec)
		require.Error(t, err, "ScratchDir %q must still be refused", tmp)
	}
	// And a snapshot that IS one is still refused.
	for _, tmp := range darwinTmpDirs {
		spec := spec
		spec.SnapshotDir = tmp
		_, err := sandboxExecProfile(cfg, spec)
		require.Error(t, err, "SnapshotDir %q must still be refused", tmp)
	}
	// A scratch INSIDE one stays ordinary — that is where os.MkdirTemp puts it.
	cfg.ScratchDir = "/private/tmp/atcr-oslevel-scratch-xyz"
	_, err = sandboxExecProfile(cfg, spec)
	require.NoError(t, err, "a scratch dir inside a temp root is the ordinary case")
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

// --- AC 02-02: Linux bwrap argv generation (shape) --------------------------

// argvPairs returns every (flag, value) pair for flag in the argv, so a test can
// assert on the bind SET rather than on positional indices. bwrap takes its
// bind arguments positionally (--ro-bind SRC DEST), so a pair extractor is the
// honest way to read them back.
func argvPairs(t *testing.T, argv []string, flag string) [][2]string {
	t.Helper()
	var out [][2]string
	for i := 0; i < len(argv); i++ {
		if argv[i] != flag {
			continue
		}
		require.Less(t, i+2, len(argv), "%s must be followed by SRC and DEST: %v", flag, argv)
		out = append(out, [2]string{argv[i+1], argv[i+2]})
		i += 2
	}
	return out
}

// bwrapFixture is deliberately separate from profileFixture: the macOS fixture
// supplies /Users and /private/var paths, and driving a Linux generator with
// them made three "never expose the host root or a home" assertions vacuous —
// no argv built from those inputs can ever equal "/" or start with /home. The
// CRITICALs that reached review lived precisely in the inputs the fixture could
// not produce.
func bwrapFixture(t *testing.T) (OSLevelConfig, RunSpec) {
	t.Helper()
	cfg := DefaultOSLevelConfig()
	cfg.ScratchDir = "/var/tmp/atcr-scratch-abc123"
	// Pinned for the same reason profileFixture pins it: DefaultOSLevelConfig
	// derives this from os.UserCacheDir(), which is the running user's home.
	cfg.ModuleCacheDir = "/var/cache/atcr-modcache"
	return cfg, RunSpec{
		Command:     []string{"go", "test", "./..."},
		SnapshotDir: "/srv/atcr-snapshot-abc123",
	}
}

func TestBwrapArgs_TerminatesItsOwnOptions(t *testing.T) {
	// The workload argv is appended directly after these arguments by
	// osLevelRunArgs, and it is MODEL-AUTHORED. Without a terminator bwrap
	// parses those tokens as its own options: a review appended
	// `--bind / /host --share-net` to the generated prefix on a real bubblewrap
	// 0.9.0, then read ~/.ssh and wrote into the operator's home from inside the
	// "sandbox". With the terminator the same argv fails closed with
	// `bwrap: execvp --bind: No such file or directory`.
	cfg, spec := bwrapFixture(t)
	for _, writable := range []bool{false, true} {
		spec := spec
		spec.Writable = writable
		argv, err := bwrapArgs(cfg, spec)
		require.NoError(t, err)
		require.NotEmpty(t, argv)
		assert.Equal(t, "--", argv[len(argv)-1],
			"the containment argv must end with the option terminator, or the workload becomes bwrap flags")
	}

	// And end to end through the real assembly path: a hostile command must
	// land after the terminator, never as a flag bwrap would honour.
	orig := osLevelContainmentArgs
	osLevelContainmentArgs = func(_ string, c OSLevelConfig, s RunSpec) ([]string, error) { return bwrapArgs(c, s) }
	t.Cleanup(func() { osLevelContainmentArgs = orig })

	hostile := []string{"--bind", "/", "/host", "--share-net", "/bin/sh"}
	args, err := osLevelRunArgs("linux", cfg, RunSpec{Command: hostile, SnapshotDir: spec.SnapshotDir})
	require.NoError(t, err)
	term := -1
	for i, a := range args {
		if a == "--" {
			term = i
			break
		}
	}
	require.GreaterOrEqual(t, term, 0, "no terminator in the assembled argv: %v", args)
	assert.Equal(t, hostile, args[term+1:], "every workload token must sit after the terminator")
}

func TestBwrapArgs_RejectsASnapshotThatWouldExposeTheHost(t *testing.T) {
	// RunSpec.validate only checks absolute and colon-free, so nothing else
	// refuses SnapshotDir="/". A review passed exactly that, got
	// `--ro-bind / /work`, and read the operator's SSH private key out of the
	// sandbox. A snapshot INSIDE a home directory is the normal case for a
	// developer's repo and must stay allowed.
	cfg, base := bwrapFixture(t)

	// /var/home is the ostree spelling (Fedora Silverblue/Kinoite, openSUSE
	// MicroOS): there /home is a SYMLINK to /var/home, so a guard that knows only
	// the /home spelling is bypassed by naming the real container directly.
	for _, bad := range []string{"/", "//", "/.", "/home", "/var/home", "/root", "/usr", "/etc", "/proc"} {
		t.Run("reject "+bad, func(t *testing.T) {
			spec := base
			spec.SnapshotDir = bad
			argv, err := bwrapArgs(cfg, spec)
			require.Error(t, err, "a snapshot at %q would expose the host filesystem", bad)
			assert.Nil(t, argv)
		})
	}
	// The container itself is what is refused; a repo INSIDE one is the ordinary
	// developer case on every spelling, and over-refusing it would make the
	// backend unusable on an ostree host rather than safer.
	for _, ok := range []string{"/home/dev/project", "/var/home/dev/project", "/root/project", "/srv/snap", "/tmp/atcr-snapshot-x"} {
		t.Run("allow "+ok, func(t *testing.T) {
			spec := base
			spec.SnapshotDir = ok
			argv, err := bwrapArgs(cfg, spec)
			require.NoError(t, err, "a snapshot inside a home or temp dir is ordinary")
			assert.Contains(t, argvPairs(t, argv, "--ro-bind"), [2]string{ok, "/work"})
		})
	}
}

func TestBwrapArgs_RejectsAScratchThatSubvertsAMount(t *testing.T) {
	// Three separate holes a review reached through this one parameter.
	cfg, spec := bwrapFixture(t)

	bad := map[string]string{
		"is the host /tmp, replacing the ephemeral tmpfs": "/tmp",
		"is /proc, restoring the host process table":      "/proc",
		"is /dev":                           "/dev",
		"is inside the read-only toolchain": "/usr/local/atcr-scratch",
		"is inside /etc":                    "/etc/atcr",
		"is the filesystem root":            "/",
	}
	for name, scratch := range bad {
		t.Run(name, func(t *testing.T) {
			cfg := cfg
			cfg.ScratchDir = scratch
			argv, err := bwrapArgs(cfg, spec)
			require.Error(t, err)
			assert.Nil(t, argv)
		})
	}

	// Inside /tmp is fine and must stay fine: that is where os.MkdirTemp puts
	// it, and the bind lands inside the fresh tmpfs rather than replacing it.
	cfg.ScratchDir = "/tmp/atcr-oslevel-scratch-xyz"
	argv, err := bwrapArgs(cfg, spec)
	require.NoError(t, err, "a scratch dir nested in /tmp is the normal case")
	assert.Contains(t, argvPairs(t, argv, "--bind"), [2]string{"/tmp/atcr-oslevel-scratch-xyz", "/tmp/atcr-oslevel-scratch-xyz"})
}

func TestBwrapArgs_IsolatesTheUserAndCgroupNamespaces(t *testing.T) {
	// bwrap's user-namespace unshare is only "automatically implied if not
	// setuid". A review ran the generated argv on a real host and found the
	// workload sharing the HOST user namespace while net and pid were fresh, so
	// the implication cannot be relied on — it is requested explicitly.
	cfg, spec := bwrapFixture(t)
	argv, err := bwrapArgs(cfg, spec)
	require.NoError(t, err)

	assert.Contains(t, argv, "--unshare-user",
		"the try form is not enough: --disable-userns refuses to start without the hard unshare")
	assert.Contains(t, argv, "--unshare-cgroup-try")
	assert.Contains(t, argv, "--disable-userns", "a nested userns is the usual route to a kernel LPE primitive")
	assert.Contains(t, argv, "--new-session")
	assert.Contains(t, argv, "--unshare-ipc")
	assert.Contains(t, argv, "--unshare-uts")
	assert.Contains(t, argvPairs2(t, argv, "--proc"), "/proc")
	assert.Contains(t, argvPairs2(t, argv, "--dev"), "/dev")
}

func TestBwrapArgs_RejectsAShadowingArgvItWasHandedDirectly(t *testing.T) {
	// The generator's own output is checked by assertNoBindShadowing, so a test
	// that re-derives the same loop over that output is a tautology. This drives
	// the guard directly with argvs the generator would not produce, which is
	// the only way to prove it can fail at all.
	for name, argv := range map[string][]string{
		"writable bind over an earlier ro-bind": {"--ro-bind", "/usr", "/usr", "--bind", "/x", "/usr"},
		"writable bind over a parent":           {"--ro-bind", "/usr/lib", "/usr/lib", "--bind", "/x", "/usr"},
		"writable bind over the private proc":   {"--proc", "/proc", "--bind", "/x", "/proc"},
		"writable bind over the tmpfs":          {"--tmpfs", "/tmp", "--bind", "/x", "/tmp"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, assertNoBindShadowing(argv))
		})
	}

	for name, argv := range map[string][]string{
		"writable bind nested inside is not shadowing": {"--ro-bind", "/usr", "/usr", "--bind", "/x", "/tmp/x"},
		"read-only after writable is not shadowing":    {"--bind", "/x", "/x", "--ro-bind", "/usr", "/usr"},
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, assertNoBindShadowing(argv))
		})
	}

	for name, argv := range map[string][]string{
		"bind missing an operand":  {"--ro-bind", "/usr"},
		"tmpfs missing a operand":  {"--tmpfs"},
		"proc missing its operand": {"--proc"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, assertNoBindShadowing(argv), "a truncated argv must be an error, never a panic")
		})
	}
}

func TestAssertNoBindShadowing_TracksEveryWritableMountVariant(t *testing.T) {
	// The guard's comment claimed the switch covered "every writable bind
	// variant bwrap accepts" — it did not: overlay, dir, file, symlink and
	// bind-fd all establish mounts that can shadow an earlier read-only one,
	// and an untracked variant both escaped the check and left its operands to
	// be re-scanned as if they were flags.
	ro := []string{"--ro-bind", "/snapshot", "/work"}
	for name, variant := range map[string][]string{
		"overlay":     {"--overlay", "/src", "/rwsrc", "/overlay-work", "/"},
		"tmp-overlay": {"--tmp-overlay", "/"},
		"ro-overlay":  {"--ro-overlay", "/"},
		"bind-fd":     {"--bind-fd", "3", "/"},
		"file":        {"--file", "3", "/work"},
		"dir":         {"--dir", "/"},
		"symlink":     {"--symlink", "/x", "/"},
	} {
		t.Run(name, func(t *testing.T) {
			argv := append(append([]string{}, ro...), variant...)
			err := assertNoBindShadowing(argv)
			if name == "ro-overlay" {
				assert.NoError(t, err, "a read-only overlay does not shadow anything writably")
				return
			}
			assert.Error(t, err, "%s establishes a writable mount shadowing the read-only mount and must be refused", name)
		})
	}
}

func TestAssertNoBindShadowing_StopsAtTheTerminatorAndRefusesUnknownFlags(t *testing.T) {
	// osLevelRunArgs appends the MODEL-AUTHORED workload immediately after
	// "--"; scanning past the terminator reads workload tokens as mount flags,
	// and a workload spelling of "--bind /work /work" would be refused as
	// shadowing — a false positive on the run's own command line.
	err := assertNoBindShadowing([]string{"--ro-bind", "/snapshot", "/work", "--", "--bind", "/work", "/work"})
	assert.NoError(t, err, "workload arguments after the terminator are not bwrap's")

	// The default arm must refuse, not ignore: an untracked bwrap flag is a
	// containment decision the guard cannot vouch for.
	err = assertNoBindShadowing([]string{"--unshare-net", "--brand-new-bwrap-flag", "--", "true"})
	assert.Error(t, err, "an untracked bwrap flag must be a refusal, not a silent pass")
}

// argvPairs2 returns the destination operand of every single-operand mount
// flag (--proc, --dev, --tmpfs) in the argv.
func argvPairs2(t *testing.T, argv []string, flag string) []string {
	t.Helper()
	var out []string
	for i := 0; i < len(argv); i++ {
		if argv[i] != flag {
			continue
		}
		require.Less(t, i+1, len(argv), "%s is missing its destination: %v", flag, argv)
		out = append(out, argv[i+1])
		i++
	}
	return out
}

func TestBwrapArgs_AlwaysUnsharesNetworkAndPID(t *testing.T) {
	// AC 02-02 Security Considerations. bwrap has no "deny network" flag to
	// assert against — the ABSENCE of --unshare-net IS the containment failure,
	// so its presence is asserted directly, for every Writable value. The
	// network namespace blocks all socket families, not just TCP, which is
	// stronger in scope than Docker's --network none.
	cfg, spec := bwrapFixture(t)

	for _, writable := range []bool{false, true} {
		spec := spec
		spec.Writable = writable
		argv, err := bwrapArgs(cfg, spec)
		require.NoError(t, err)

		assert.Contains(t, argv, "--unshare-net", "no network namespace means no containment of egress")
		assert.Contains(t, argv, "--unshare-pid")
		assert.Contains(t, argv, "--die-with-parent",
			"a workload that outlives the sandbox process is not contained by it")
	}
}

func TestBwrapArgs_BindsAreScopedAndNeverExposeTheHostRoot(t *testing.T) {
	// bwrap's model is allow-list — nothing is visible unless bound in — so the
	// equivalent of deny-default is that every bind source is one the generator
	// is entitled to expose, and nothing else.
	//
	// Asserted as an exhaustive membership sweep, not as prefix bans. An earlier
	// revision banned sources under /home/ and /root, which contradicts the
	// documented policy TestBwrapArgs_RejectsASnapshotThatWouldExposeTheHost
	// pins: a snapshot INSIDE a home directory is the ordinary developer case and
	// is ALLOWED. Those bans passed only because bwrapFixture supplies /srv and
	// /var/tmp paths that can never trip them, and fixing the fixture to a
	// realistic /home/dev/project snapshot made them fail on correct behavior.
	// Membership catches what the prefix bans were reaching for — an EXTRA bind
	// of some unrelated host tree — without encoding an invariant the generator
	// does not hold.
	cfg, spec := bwrapFixture(t)
	for _, snapshot := range []string{spec.SnapshotDir, "/home/dev/project", "/root/project", "/tmp/atcr-snap-x"} {
		for _, writable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/writable=%v", snapshot, writable), func(t *testing.T) {
				spec := spec
				spec.SnapshotDir = snapshot
				spec.Writable = writable
				argv, err := bwrapArgs(cfg, spec)
				require.NoError(t, err)

				// The only sources the generator may name: the run's own two
				// directories (plus the work copy carved out of the scratch dir)
				// and the fixed system allowlists.
				permitted := map[string]bool{
					snapshot:           true,
					cfg.ScratchDir:     true,
					cfg.ModuleCacheDir: true,
					filepath.Join(cfg.ScratchDir, osLevelScratchWorkSubdir): true,
				}
				for _, p := range linuxRequiredRoots {
					permitted[p] = true
				}
				for _, p := range linuxOptionalRoots {
					permitted[p] = true
				}
				for _, p := range linuxOptionalFiles {
					permitted[p] = true
				}

				var sources int
				for _, flag := range []string{"--bind", "--ro-bind", "--ro-bind-try"} {
					for _, pair := range argvPairs(t, argv, flag) {
						sources++
						assert.NotEqual(t, "/", pair[0], "%s must never take the host root as its source: %v", flag, pair)
						assert.NotEqual(t, "/", pair[1], "%s must never mount over the sandbox root: %v", flag, pair)
						assert.True(t, permitted[pair[0]],
							"%s exposes a host path outside the permitted set: %v", flag, pair)
					}
				}
				// A sweep over an empty bind set proves nothing; the generator
				// always emits at least the required roots and the snapshot.
				assert.GreaterOrEqual(t, sources, len(linuxRequiredRoots)+1,
					"the generator must emit the required roots and the snapshot")
			})
		}
	}
}

func TestBwrapArgs_SnapshotIsReadOnlyAtWorkAndWritableSplitsToSrc(t *testing.T) {
	// AC 02-02 Scenario 1/2. The mount points mirror dockerRunArgs exactly
	// (docker.go:172-189): Writable:false binds the snapshot read-only at /work;
	// Writable:true binds it read-only at /src and puts the writable scratch at
	// /work. Reusing Docker's mount points is what lets a workload that
	// hardcodes /work behave the same on either backend.
	cfg, spec := bwrapFixture(t)

	t.Run("read-only", func(t *testing.T) {
		argv, err := bwrapArgs(cfg, spec)
		require.NoError(t, err)
		assert.Contains(t, argvPairs(t, argv, "--ro-bind"), [2]string{spec.SnapshotDir, "/work"})
		for _, pair := range argvPairs(t, argv, "--bind") {
			assert.NotEqual(t, spec.SnapshotDir, pair[0], "the snapshot must never be bound writable")
		}
		assert.Contains(t, argv, "--chdir")
	})

	t.Run("writable", func(t *testing.T) {
		spec := spec
		spec.Writable = true
		argv, err := bwrapArgs(cfg, spec)
		require.NoError(t, err)
		assert.Contains(t, argvPairs(t, argv, "--ro-bind"), [2]string{spec.SnapshotDir, "/src"},
			"the snapshot stays read-only; only an ephemeral copy is writable")
		// The COPY subdirectory, not the scratch root: binding the root at /work
		// would put the snapshot copy and the run's $HOME in one directory, so
		// the tree under review would supply the toolchain's dotfiles.
		assert.Contains(t, argvPairs(t, argv, "--bind"),
			[2]string{filepath.Join(cfg.ScratchDir, osLevelScratchWorkSubdir), "/work"})
		assert.NotContains(t, argvPairs(t, argv, "--bind"), [2]string{cfg.ScratchDir, "/work"},
			"the scratch ROOT must never be the writable work tree")
		for _, pair := range argvPairs(t, argv, "--bind") {
			assert.NotEqual(t, spec.SnapshotDir, pair[0], "the snapshot must never be bound writable")
		}
	})
}

func TestBwrapArgs_TmpIsAnEphemeralTmpfsNotTheHostTmp(t *testing.T) {
	// Decided in the Phase 2 clarifications: --tmpfs /tmp, not a host bind. A
	// shared host /tmp exposes the workload to other processes' temp files and
	// to predictable-path/symlink race preconditions that an ephemeral mount
	// closes by construction, and it continues DockerBackend's own ephemeral
	// --tmpfs /scratch precedent.
	cfg, spec := bwrapFixture(t)
	argv, err := bwrapArgs(cfg, spec)
	require.NoError(t, err)

	// Assert the DESTINATION, not merely that a --tmpfs appears somewhere: a
	// tmpfs mounted anywhere else would leave /tmp unmounted while this test
	// stayed green.
	assert.Equal(t, []string{"/tmp"}, argvPairs2(t, argv, "--tmpfs"))
	for _, flag := range []string{"--bind", "--ro-bind", "--ro-bind-try"} {
		for _, pair := range argvPairs(t, argv, flag) {
			assert.NotEqual(t, "/tmp", pair[0], "the host /tmp must not be bound into the sandbox: %v", pair)
		}
	}
}

func TestBwrapArgs_ToolchainBindsAreReadOnlyAndNarrow(t *testing.T) {
	// AC 02-02 Scenario 3. The interpreter and toolchain must be visible or
	// nothing executes inside the namespaces, but they are read-only and named
	// rather than a blanket --ro-bind / /.
	cfg, spec := bwrapFixture(t)
	argv, err := bwrapArgs(cfg, spec)
	require.NoError(t, err)

	ro := append(argvPairs(t, argv, "--ro-bind"), argvPairs(t, argv, "--ro-bind-try")...)
	var haveUsr bool
	for _, pair := range ro {
		if pair[0] == "/usr" {
			haveUsr = true
			assert.Equal(t, "/usr", pair[1], "a toolchain path must be mounted at its own location")
		}
	}
	assert.True(t, haveUsr, "/usr must be visible read-only or no binary can be executed")

	// Nothing under a toolchain path may also be writable.
	for _, w := range argvPairs(t, argv, "--bind") {
		for _, r := range ro {
			assert.False(t, pathContains(w[1], r[1]),
				"writable bind at %q covers the read-only toolchain mount at %q", w[1], r[1])
		}
	}
}

func TestBwrapArgs_RejectsScratchOverlappingSnapshot(t *testing.T) {
	// AC 02-02 Edge Case 2, the same decision as the macOS profile: reject, do
	// not silently relocate. On Linux the overlap is what MAKES the shadowing
	// above possible, so refusing it at the source is the cheaper guarantee.
	cfg, spec := bwrapFixture(t)
	for name, scratch := range map[string]string{
		"nested":                     spec.SnapshotDir + "/scratch",
		"identical":                  spec.SnapshotDir,
		"snapshot nested in scratch": "/srv",
		"filesystem root":            "/",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := cfg
			cfg.ScratchDir = scratch
			argv, err := bwrapArgs(cfg, spec)
			require.Error(t, err)
			assert.Nil(t, argv, "no partial argv may be returned alongside an error")
		})
	}
}

func TestBwrapArgs_PathsAreDiscreteArgvElements(t *testing.T) {
	// AC 02-02 Edge Case 1. bwrap is spawned via os/exec with this argv and no
	// shell, so a path with a space or a metacharacter is safe as long as this
	// generator never joins paths into one string. Asserted with a path that
	// would be re-tokenized by any shell.
	cfg, base := bwrapFixture(t)
	cfg.ScratchDir = "/var/tmp/atcr scratch; rm -rf /"
	spec := base
	spec.SnapshotDir = "/home-less/dev/atcr snapshot $(whoami)"

	argv, err := bwrapArgs(cfg, spec)
	require.NoError(t, err, "a path with spaces is not an error — it is an argv element")

	assert.Contains(t, argvPairs(t, argv, "--ro-bind"), [2]string{spec.SnapshotDir, "/work"},
		"the path must survive as ONE element, spaces and all")
	for _, a := range argv {
		assert.NotContains(t, a, "--ro-bind "+spec.SnapshotDir,
			"a flag and its operand must never be joined into a single element")
	}
}

func TestBwrapArgs_ValidatesSpecFirst(t *testing.T) {
	// AC 02-02 Error Scenario 1, mirroring dockerRunArgs (docker.go:136-138).
	cfg, _ := bwrapFixture(t)
	for name, spec := range map[string]RunSpec{
		"neither command nor script": {SnapshotDir: "/srv/snap"},
		"both command and script":    {Command: []string{"true"}, Script: "echo hi", SnapshotDir: "/srv/snap"},
		"missing snapshot dir":       {Command: []string{"true"}},
		"relative snapshot dir":      {Command: []string{"true"}, SnapshotDir: "relative/snap"},
	} {
		t.Run(name, func(t *testing.T) {
			argv, err := bwrapArgs(cfg, spec)
			require.Error(t, err)
			assert.Nil(t, argv)
		})
	}
}

func TestBwrapArgs_ZeroConfigStillProducesAContainedArgv(t *testing.T) {
	// AC 02-02 Edge Case 3. A zero config has no scratch dir; the argv must
	// still unshare the network and bind the snapshot read-only rather than
	// degrade into something permissive or malformed.
	_, spec := bwrapFixture(t)
	argv, err := bwrapArgs(OSLevelConfig{}, spec)
	require.NoError(t, err)

	assert.Contains(t, argv, "--unshare-net")
	assert.Contains(t, argvPairs(t, argv, "--ro-bind"), [2]string{spec.SnapshotDir, "/work"})
	for _, pair := range argvPairs(t, argv, "--bind") {
		assert.NotEmpty(t, pair[0], "an empty scratch dir must be omitted, never emitted as an empty bind")
		assert.NotEmpty(t, pair[1])
	}
}

func TestBwrapArgs_ContainmentArgsDoNotIncludeTheWorkload(t *testing.T) {
	// The generator produces the TOOL's arguments only. osLevelRunArgs appends
	// the workload after them (oslevel.go:461-466), and the script body travels
	// over stdin — so a command token appearing here would mean the workload was
	// being built in two places.
	cfg, spec := bwrapFixture(t)
	argv, err := bwrapArgs(cfg, spec)
	require.NoError(t, err)
	assert.NotContains(t, argv, "go")
	assert.NotContains(t, argv, "test")
	assert.NotContains(t, argv, "./...")
}

// --- Phase 2 gate fixes -----------------------------------------------------

func TestSandboxExecProfile_GrantsTheSymlinkNodesInFrontOfPrivate(t *testing.T) {
	// Normalizing a path onto /private is not enough on its own: sandbox-exec
	// resolves the target but still needs metadata permission on each
	// intermediate symlink node. os.MkdirTemp returns /var/folders/... on a
	// stock macOS host, so without the /var node every snapshot read and every
	// GOCACHE write fails ("mkdir: /var: Operation not permitted"), which would
	// make Phase 3's positive controls impossible and its negative controls pass
	// vacuously.
	cfg, spec := profileFixture(t)
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	var metadataLine string
	for _, line := range strings.Split(profile, "\n") {
		if strings.Contains(line, "file-read-metadata") {
			metadataLine = line
		}
	}
	require.NotEmpty(t, metadataLine)
	for _, alias := range []string{"/var", "/etc", "/tmp"} {
		assert.Contains(t, metadataLine, `(literal "`+alias+`")`,
			"the %s symlink node must be traversable or every aliased path dies at the first component", alias)
	}
}

func TestGenerators_RejectAWritableRootOverAHomeOrSystemTree(t *testing.T) {
	// The writable guard had been handed only the read tier, so a scratch dir at
	// $HOME was accepted — a gate review then read ~/.ssh/id_ed25519 and wrote
	// into the home directory from inside the sandbox, the exact outcome this
	// package exists to prevent. Both platforms are asserted together so they
	// cannot drift apart again.
	t.Run("darwin", func(t *testing.T) {
		cfg, spec := profileFixture(t)
		for _, bad := range []string{"/Users", "/Users/samestrin", "/private/etc", "/etc", "/Applications", "/System", "/Library", "/private/var/root"} {
			cfg := cfg
			cfg.ScratchDir = bad
			profile, err := sandboxExecProfile(cfg, spec)
			require.Error(t, err, "ScratchDir %q must be refused", bad)
			assert.Empty(t, profile)
		}
	})
	t.Run("linux", func(t *testing.T) {
		cfg, spec := bwrapFixture(t)
		for _, bad := range []string{"/home", "/home/dev", "/root", "/etc", "/usr", "/proc"} {
			cfg := cfg
			cfg.ScratchDir = bad
			argv, err := bwrapArgs(cfg, spec)
			require.Error(t, err, "ScratchDir %q must be refused", bad)
			assert.Nil(t, argv)
		}
	})
}

func TestSandboxExecProfile_RejectsTheSameSnapshotValuesAsLinux(t *testing.T) {
	// The two source guards had drifted: Linux refused /etc, /proc and /dev
	// while darwin accepted them, and darwin additionally accepted /private/tmp,
	// where the trailing snapshot deny silently revokes the unconditional /tmp
	// carve-out and leaves the run with nowhere writable at all.
	cfg, base := profileFixture(t)
	for _, bad := range []string{"/", "/Users", "/private/etc", "/dev", "/tmp", "/private/tmp", "/usr/lib"} {
		spec := base
		spec.SnapshotDir = bad
		profile, err := sandboxExecProfile(cfg, spec)
		require.Error(t, err, "SnapshotDir %q must be refused", bad)
		assert.Empty(t, profile)
	}
	// A snapshot INSIDE a home or a temp dir stays ordinary.
	for _, ok := range []string{"/Users/dev/project", "/private/tmp/atcr-snapshot-x", "/var/folders/qq/T/atcr-snapshot-x"} {
		spec := base
		spec.SnapshotDir = ok
		_, err := sandboxExecProfile(cfg, spec)
		require.NoError(t, err, "SnapshotDir %q is the normal case", ok)
	}
}

func TestSandboxExecArgs_WrapsTheProfileAndTerminatesOptions(t *testing.T) {
	// The phase-exit contract: both platforms return a ready-to-splice argv, so
	// Phase 3's integration tests and Phase 4's wiring consume them identically
	// instead of re-deriving the darwin flags at each call site.
	cfg, spec := profileFixture(t)
	argv, err := sandboxExecArgs(cfg, spec)
	require.NoError(t, err)
	require.Len(t, argv, 3)

	assert.Equal(t, "-p", argv[0])
	assert.Contains(t, argv[1], "(deny default)", "the profile travels as one discrete argv element")
	assert.Equal(t, "--", argv[2], "the workload is appended after this and must never be read as an option")

	_, err = sandboxExecArgs(cfg, RunSpec{SnapshotDir: "/Users/dev/snap"})
	require.Error(t, err, "a generator error must propagate rather than yield a partial argv")
}

func TestGenerators_BothRejectWritableWithoutAScratchDir(t *testing.T) {
	// RunSpec.Writable asks for an ephemeral copy to be the writable tree, and
	// with no scratch dir that copy has nowhere to live on either platform. The
	// two generators had disagreed, which would have forced a cross-platform
	// matrix to carry per-platform expectations for one input.
	_, darwinSpec := profileFixture(t)
	darwinSpec.Writable = true
	_, err := sandboxExecProfile(OSLevelConfig{}, darwinSpec)
	require.Error(t, err)

	_, linuxSpec := bwrapFixture(t)
	linuxSpec.Writable = true
	_, err = bwrapArgs(OSLevelConfig{}, linuxSpec)
	require.Error(t, err)
}

func TestSandboxExecProfile_GrantsMetadataOnAncestorsOfTheCarveOuts(t *testing.T) {
	// os.MkdirAll — which every Go build performs on GOCACHE — walks the
	// ancestor chain, so without metadata on each ancestor it fails on the first
	// unreadable one rather than on the target. Measured under the real binary
	// as "mkdir: /var: Operation not permitted" with both the snapshot and
	// scratch subpaths already granted.
	cfg := DefaultOSLevelConfig()
	cfg.ScratchDir = "/private/var/folders/4h/T/atcr-scratch"
	spec := RunSpec{Command: []string{"go", "build"}, SnapshotDir: "/private/var/folders/4h/T/atcr-snap"}

	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	var metadataLine string
	for _, line := range strings.Split(profile, "\n") {
		if strings.Contains(line, "file-read-metadata") {
			metadataLine = line
		}
	}
	require.NotEmpty(t, metadataLine)
	for _, ancestor := range []string{"/private", "/private/var", "/private/var/folders", "/private/var/folders/4h", "/private/var/folders/4h/T"} {
		assert.Contains(t, metadataLine, `(literal "`+ancestor+`")`,
			"an ancestor must be traversable, or mkdir -p fails before reaching the carve-out")
	}
	// An ancestor is granted as a directory ENTRY, never as a subtree — the
	// distinction between (literal) and (subpath) is the whole containment
	// boundary here.
	for _, ancestor := range []string{"/private", "/private/var", "/private/var/folders"} {
		assert.NotContains(t, profile, `(subpath "`+ancestor+`")`,
			"an ancestor must never be granted as a subtree")
	}
}

func TestDarwinSourceProtectedDirs_NamesTheProtectedTreesExplicitly(t *testing.T) {
	// /System and /Library were refused only INCIDENTALLY — a
	// darwinSystemReadDirs entry happens to sit under each — while /Volumes
	// and /Applications were accepted outright: SnapshotDir=/Volumes would
	// expose every mounted external disk, Time Machine target and network
	// share read-only to model-authored code. The guard must name the trees,
	// not inherit the refusal from an unrelated list's contents.
	for _, dir := range []string{"/System", "/Library", "/Applications", "/Volumes"} {
		assert.Contains(t, darwinSourceProtectedDirs(), dir,
			"removing a darwinSystemReadDirs entry must not be able to widen the source guard")

		cfg, base := profileFixture(t)
		spec := base
		spec.SnapshotDir = dir
		_, err := sandboxExecProfile(cfg, spec)
		assert.Error(t, err, "SnapshotDir=%s must be refused", dir)
	}
}

func TestGenerators_DarwinGuardsFoldCaseButLinuxGuardsDoNot(t *testing.T) {
	// macOS filesystems are case-insensitive by default, so /users/samestrin and
	// /Users/samestrin are the SAME directory — but filepath.Rel compares bytes.
	// A gate review changed one character of ScratchDir to /users/samestrin, was
	// accepted by every darwin guard, and then read the operator's real
	// ~/.ssh/id_ed25519 and wrote into their real home from inside the sandbox.
	// Every darwin guard folded the same way, so the whole protected list was
	// one keystroke deep.
	t.Run("darwin refuses case variants", func(t *testing.T) {
		cfg, spec := profileFixture(t)
		for _, bad := range []string{
			"/users/dev", "/USERS/dev", "/Users/DEV",
			"/applications", "/SYSTEM", "/library",
			"/private/ETC", "/usr/LIB", "/OPT/homebrew", "/private/TMP",
		} {
			cfg := cfg
			cfg.ScratchDir = bad
			profile, err := sandboxExecProfile(cfg, spec)
			require.Error(t, err, "ScratchDir %q is the same directory as its canonical spelling", bad)
			assert.Empty(t, profile)
		}
	})

	t.Run("darwin normalizes case variants of the symlinked prefixes", func(t *testing.T) {
		cfg, base := profileFixture(t)
		spec := base
		spec.SnapshotDir = "/VAR/folders/qq/T/atcr-snap"
		profile, err := sandboxExecProfile(cfg, spec)
		require.NoError(t, err)
		assert.Contains(t, profile, `(subpath "/private/var/folders/qq/T/atcr-snap")`,
			"a case variant of /var must normalize onto /private/var like any other spelling")
	})

	t.Run("darwin normalizes the APFS firmlink spelling", func(t *testing.T) {
		// /System/Volumes/Data/Users/x IS /Users/x. sandbox-exec canonicalizes,
		// so a rule naming the firmlink spelling grants nothing — and the
		// home-tree guard never sees a /Users to refuse.
		cfg, base := profileFixture(t)
		spec := base
		spec.SnapshotDir = "/System/Volumes/Data/Users/dev/project"
		profile, err := sandboxExecProfile(cfg, spec)
		require.NoError(t, err)
		assert.Contains(t, profile, `(subpath "/Users/dev/project")`)

		spec.SnapshotDir = "/System/Volumes/Data/Users/dev"
		_, err = sandboxExecProfile(cfg, spec)
		require.Error(t, err, "the firmlink spelling of a home directory must be refused like the canonical one")
	})

	t.Run("darwin rewrites every firmlink sub-prefix to a fixed point", func(t *testing.T) {
		// A single rewrite pass maps /System/Volumes/Data/tmp/x to /tmp/x — an
		// alias that matches NOTHING in the generated profile in either
		// direction (measured against the real sandbox-exec: a trailing deny
		// written against /tmp/zz-snap did not stop a write through
		// /private/tmp/zz-snap). Every firmlink sub-prefix must normalize all
		// the way.
		for _, tc := range []struct{ in, want string }{
			{"/System/Volumes/Data/tmp/atcr-snap", "/private/tmp/atcr-snap"},
			{"/System/Volumes/Data/private/tmp/atcr-snap", "/private/tmp/atcr-snap"},
			{"/System/Volumes/Data/var/folders/qq/T/atcr-snap", "/private/var/folders/qq/T/atcr-snap"},
			{"/System/Volumes/Data/etc/whatever", "/private/etc/whatever"},
			{"/System/Volumes/Data/Users/dev/project", "/Users/dev/project"},
			{"/tmp/atcr-snap", "/private/tmp/atcr-snap"},
			{"/var/folders/qq", "/private/var/folders/qq"},
			{"/etc/whatever", "/private/etc/whatever"},
		} {
			got, err := profileSafePath("SnapshotDir", tc.in)
			require.NoError(t, err, "%s", tc.in)
			assert.Equal(t, tc.want, got, "%s must normalize all the way", tc.in)
		}
	})

	t.Run("linux does not fold", func(t *testing.T) {
		// Linux filesystems are genuinely case-sensitive: /Home is not /home,
		// and refusing it would reject a legitimate distinct directory.
		cfg, spec := bwrapFixture(t)
		cfg.ScratchDir = "/Home/dev"
		argv, err := bwrapArgs(cfg, spec)
		require.NoError(t, err, "/Home is a different directory from /home on a case-sensitive filesystem")
		assert.Contains(t, argvPairs(t, argv, "--bind"), [2]string{"/Home/dev", "/Home/dev"})
	})
}

func TestSandboxExecProfile_NeverEmitsAnUnresolvedAlias(t *testing.T) {
	// Generator-wide post-condition, stated as a property so no mirrored oracle
	// can satisfy it: no path in the emitted profile may use a spelling
	// sandbox-exec resolves away — an unresolved alias grants (or denies)
	// NOTHING in either direction. The exceptions are the metadata literals for
	// the symlink NODES themselves (/var, /etc, /tmp), which name the node
	// deliberately: metadata permission on the node is what lets path
	// resolution traverse it.
	cfg, spec := profileFixture(t)
	spec.SnapshotDir = "/System/Volumes/Data/tmp/atcr-snap-property"
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	clauses := regexp.MustCompile(`\((?:subpath|literal) "([^"]+)"\)`).FindAllStringSubmatch(profile, -1)
	require.NotEmpty(t, clauses)
	for _, m := range clauses {
		p := strings.ToLower(m[1])
		for _, banned := range []string{"/tmp/", "/var/", "/etc/", "/system/volumes/data"} {
			assert.False(t, strings.HasPrefix(p, banned),
				"the profile must not name the unresolved alias %q (from %q)", m[1], spec.SnapshotDir)
		}
	}
}

func TestSandboxExecProfile_PinsTheExplicitNetworkDenial(t *testing.T) {
	// The explicit `(deny network*)` clause is defence-in-depth: `(deny default)`
	// already covers network operations, so deleting the explicit line does not
	// open a hole — and consequently the real-binary integration proof cannot
	// detect its removal (measured: with the line deleted, the whole darwin
	// integration suite stayed green, network test included).
	//
	// That is exactly why it needs a shape assertion. Without one the clause
	// reads as "proven by the containment tests" when nothing pins it, and a
	// future edit that relaxes the deny-default posture could drop it in the same
	// breath and lose the network denial with it.
	cfg := DefaultOSLevelConfig()
	cfg.ScratchDir = "/private/tmp/atcr-scratch-xyz"
	profile, err := sandboxExecProfile(cfg, RunSpec{
		Command:     []string{"true"},
		SnapshotDir: "/private/var/folders/xx/atcr-snap-xyz",
	})
	require.NoError(t, err)
	assert.Contains(t, profile, profileDenyNetwork,
		"the explicit network denial must stay in the emitted profile even though (deny default) subsumes it")
}
