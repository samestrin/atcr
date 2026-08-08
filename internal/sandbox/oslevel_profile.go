package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
)

// This file holds the pure, I/O-free containment generators the OS-level
// backend feeds to its platform tool: a sandbox-exec profile string on macOS
// and (task 2.8) a bwrap argument list on Linux. Both mirror dockerRunArgs
// (docker.go:135) — spec.validate() first, no filesystem access, no spawn — so
// the containment boundary is assertable in a unit test without either binary
// installed.
//
// SCOPE OF PROOF (32.3/Q7): being able to assert the output does not mean the
// output contains anything. A perfectly-shaped profile can still fail to
// isolate. Only the //go:build integration tests (AC 02-03/02-04), which drive
// the real binary and attempt a forbidden write and an outbound connection,
// prove enforcement.

// sandbox-exec profile clauses that are not path-scoped.
//
// Every rule here was MEASURED against the real binary on macOS 25.2, not
// inferred from the profile-language documentation — the same discipline the
// fault taxonomy in oslevel.go was rebuilt on after an inferred version proved
// wrong in the fail-open direction. A (deny default) profile carrying only the
// snapshot and /tmp carve-outs cannot start ANY binary: dyld aborts the process
// with SIGABRT (exit 134) before the workload's first instruction, so Preflight
// would fail on every macOS host.
//
// Measured minimum for `sandbox-exec -p <profile> /bin/sh -c echo`:
//
//	(allow process-exec*) (allow process-fork)  — or nothing can be exec'd at all
//	(allow sysctl-read)                          — dyld reads kern.* at startup
//	(allow file-map-executable)                  — mapping the shared cache pages
//	(allow file-read-metadata)                   — path resolution along the way
//	(allow file-read* (literal "/"))             — the loader reads the root entry
//
// The root rule is a `literal`, NOT a `subpath`: it grants the root directory
// entry itself and nothing beneath it. `(subpath "/")` would grant the entire
// filesystem and silently defeat the deny-default — it is the single most
// dangerous typo available in this file, which is why the two forms are called
// out here and asserted apart in the tests.
const (
	profileVersion       = "(version 1)"
	profileDenyDefault   = "(deny default)"
	profileDenyNetwork   = "(deny network*)"
	profileAllowRootRead = `(allow file-read* (literal "/"))`
)

var profileRuntimeAllows = []string{
	"(allow process-exec*)",
	"(allow process-fork)",
	"(allow sysctl-read)",
	"(allow file-map-executable)",
	"(allow file-read-metadata)",
}

// darwinSystemReadDirs is the read-only system tier a workload needs to execute
// at all. It deliberately mirrors trustedToolDirs (oslevel.go:339) plus the
// library and dyld-cache paths the loader maps, rather than granting the whole
// /System or /Library trees — the same "limited to what execution requires, not
// a blanket" standard AC 02-02 Scenario 3 sets for the Linux binds.
//
// Nothing here is writable, and nothing here covers user data: $HOME stays
// unreadable, which is what makes ~/.ssh unreachable for a prompt-injected
// Fixer agent. Verified on macOS 25.2 that a profile carrying exactly this tier
// runs /bin/sh while `touch $HOME/x` fails with "Operation not permitted".
var darwinSystemReadDirs = []string{
	"/usr/lib",
	"/usr/bin",
	"/bin",
	"/usr/sbin",
	"/sbin",
	// The dyld shared cache. Both locations are listed because the path moved:
	// macOS 13+ serves it from the Cryptexes mount, older releases from
	// /private/var/db/dyld. A missing path in a read-only allow costs nothing.
	"/System/Volumes/Preboot/Cryptexes/OS/usr/lib",
	"/private/var/db/dyld",
}

// darwinTmpDirs are the writable temp roots. /tmp is a symlink to /private/tmp
// on macOS and sandbox-exec matches on the RESOLVED path, so a rule naming only
// /tmp matches nothing and the carve-out silently does not exist. Both are
// emitted.
var darwinTmpDirs = []string{"/tmp", "/private/tmp"}

// sandboxExecProfile builds the macOS sandbox-exec profile for spec: deny by
// default, an explicit network denial, a read-only view of the snapshot and of
// the narrow system tier, and write access confined to cfg.ScratchDir and /tmp.
//
// The snapshot is read-only for BOTH values of spec.Writable. RunSpec.Writable
// (sandbox.go:59-79) is the single source of truth for writability and it never
// makes the snapshot itself writable — when true, the snapshot stays read-only
// and an ephemeral COPY becomes the writable tree. sandbox-exec has no mount
// namespace and cannot remap paths, so that copy is a working-directory choice
// Run makes, not a policy difference: the profile is identical either way. (The
// original AC 02-01 Scenario 1 asked for file-write* on the snapshot under
// Writable:false; that contradicted both RunSpec and the package MUST at
// sandbox.go:9 and was corrected on 2026-08-08.)
func sandboxExecProfile(cfg OSLevelConfig, spec RunSpec) (string, error) {
	if err := spec.validate(); err != nil {
		return "", err
	}
	snapshot, err := profileSafePath("SnapshotDir", spec.SnapshotDir)
	if err != nil {
		return "", err
	}
	// An empty scratch dir is valid and simply yields no scratch carve-out —
	// strictly more restrictive, so a zero-value config degrades toward less
	// access rather than more (AC 02-01 Edge Case 3).
	var scratch string
	if cfg.ScratchDir != "" {
		if scratch, err = profileSafePath("ScratchDir", cfg.ScratchDir); err != nil {
			return "", err
		}
		if err := assertDisjointPaths(snapshot, scratch); err != nil {
			return "", err
		}
	}

	var b strings.Builder
	// Denials first. sandbox-exec applies the LAST matching rule, so this
	// ordering is what makes the profile default-deny: every allow below is a
	// deliberate narrowing of it. An allow emitted above the deny-default would
	// be overridden and read as a containment guarantee that is not one.
	b.WriteString(profileVersion + "\n")
	b.WriteString(profileDenyDefault + "\n")
	b.WriteString(profileDenyNetwork + "\n")
	for _, rule := range profileRuntimeAllows {
		b.WriteString(rule + "\n")
	}
	b.WriteString(profileAllowRootRead + "\n")
	for _, dir := range darwinSystemReadDirs {
		b.WriteString(profileReadRule(dir) + "\n")
	}
	b.WriteString(profileReadRule(snapshot) + "\n")
	if scratch != "" {
		b.WriteString(profileWriteRule(scratch) + "\n")
	}
	for _, dir := range darwinTmpDirs {
		b.WriteString(profileWriteRule(dir) + "\n")
	}
	return b.String(), nil
}

func profileReadRule(dir string) string {
	return fmt.Sprintf(`(allow file-read* (subpath %q))`, dir)
}

func profileWriteRule(dir string) string {
	return fmt.Sprintf(`(allow file-read* file-write* (subpath %q))`, dir)
}

// profileDSLMetacharacters are the characters that can break out of a
// `(subpath "...")` string literal or change how the profile parses around it:
// a quote closes the literal, the parens close or open a clause, a backslash
// starts an escape, a semicolon begins a comment, and a newline ends the line.
const profileDSLMetacharacters = "\"()\\;\n\r\t"

// profileSafePath validates a path before it is interpolated into a profile
// literal, and returns it cleaned.
//
// Rejection rather than escaping is a decided point (AC 02-01 Edge Case 1). The
// profile IS the security boundary, so a hand-written escaper for it belongs to
// the same severity class as a SQL or shell escaper — easy to get subtly wrong,
// and wrong means a path value can rewrite the policy that contains untrusted
// code. Rejection is trivially auditable and costs nothing real: a snapshot or
// scratch directory containing a quote or a paren is pathological.
//
// RunSpec.validate already requires SnapshotDir to be absolute and colon-free
// (sandbox.go:97-102); this adds the profile-DSL-specific checks on top and
// applies the same bar to ScratchDir, which validate never sees and which an
// operator config could reach.
func profileSafePath(field, path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("sandbox: %s must be an absolute path, got %q", field, path)
	}
	if i := strings.IndexAny(path, profileDSLMetacharacters); i >= 0 {
		return "", fmt.Errorf("sandbox: %s must not contain %q (sandbox-exec profile injection), got %q",
			field, string(path[i]), path)
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("sandbox: %s must not contain control characters, got %q", field, path)
		}
	}
	return filepath.Clean(path), nil
}

// assertDisjointPaths rejects a scratch directory that is the snapshot, sits
// inside it, or contains it.
//
// Rejecting rather than silently relocating is decided (AC 02-01 Edge Case 2,
// AC 02-02 Edge Case 2). An overlap means the writable allow rule covers part
// of the tree the read-only rule is supposed to protect — the snapshot
// guarantee would be subsumed rather than narrowed — and on Linux the same
// overlap makes a later writable --bind shadow an earlier --ro-bind. Moving the
// caller's scratch path would change where a workload's output lands without
// telling anyone.
//
// Containment is decided component-wise, not by string prefix: "/a/snap-2" has
// "/a/snap" as a string prefix without being inside it, and rejecting that
// sibling would be a false positive on a perfectly safe configuration.
func assertDisjointPaths(snapshot, scratch string) error {
	switch {
	case snapshot == scratch:
		return fmt.Errorf("sandbox: ScratchDir must not be the snapshot directory itself (%q): "+
			"the writable carve-out would cover the read-only snapshot", scratch)
	case strings.HasPrefix(scratch, snapshot+string(filepath.Separator)):
		return fmt.Errorf("sandbox: ScratchDir %q must not be inside SnapshotDir %q: "+
			"the writable carve-out would cover part of the read-only snapshot", scratch, snapshot)
	case strings.HasPrefix(snapshot, scratch+string(filepath.Separator)):
		return fmt.Errorf("sandbox: SnapshotDir %q must not be inside ScratchDir %q: "+
			"the writable carve-out would cover the whole read-only snapshot", snapshot, scratch)
	}
	return nil
}
