package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
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
//	(allow file-read-metadata …)                 — path resolution along the way
//	(allow file-read* (literal "/"))             — the loader reads the root entry
//
// The root rule is a `literal`, NOT a `subpath`: it grants the root directory
// entry itself and nothing beneath it. `(subpath "/")` would grant the entire
// filesystem and silently defeat the deny-default — it is the single most
// dangerous typo available in this file, and it is guarded by
// assertUsableWritableRoot rather than by care alone, because an earlier
// revision of that guard let ScratchDir="/" through and a review reproduced a
// full read of ~/.ssh under the resulting profile.
//
// Two of these allows are deliberately unscoped, and the comment says so
// instead of claiming a narrowness the rules do not have:
//
//   - process-exec*/file-map-executable let the workload exec any binary on the
//     host, including prefixes with no read allow. That is by design: this
//     backend confines what code can READ, WRITE, and REACH, not which binaries
//     it may start. A review verified it is not an escape — a nested
//     sandbox-exec is refused (`sandbox_apply: Operation not permitted`),
//     setuid exec is refused, and the deny-default's mach-lookup denial blocks
//     `open`/`osascript`/`launchctl`/`security`. Narrowing it is TD-012.
//   - sysctl-read leaks host fingerprint (`kern.hostname`, `hw.model`) to
//     model-authored code. dyld needs only a handful of names; narrowing to
//     them is TD-013.
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
}

// darwinSystemReadDirs is the read-only tier a workload needs to execute at
// all. It is NOT the minimum for `/bin/sh` — an earlier revision measured only
// that, and a review then showed `git`, `make`, `python3` and `go vet` all
// failing under it (`unable to load libxcrun`, `open /dev/null: operation not
// permitted`). A sandbox that cannot run the project's own validate commands is
// not a conservative sandbox, it is a broken one, so the tier covers the
// toolchain prefixes those commands load from.
//
// It stays a list of named prefixes rather than the whole /System and /Library
// trees, matching the "limited to what execution requires, not a blanket"
// standard AC 02-02 Scenario 3 sets for the Linux binds. Nothing here is
// writable and nothing here is user data: $HOME is not readable, which is what
// keeps ~/.ssh out of reach of a prompt-injected Fixer agent.
var darwinSystemReadDirs = []string{
	"/usr/lib",
	"/usr/bin",
	"/bin",
	"/usr/sbin",
	"/sbin",
	"/usr/share",
	// `sh` resolves its own variant through here; without it the shell starts
	// with "Error opening /private/var/select/sh".
	"/private/var/select",
	// The dyld shared cache. Both locations are listed because the path moved:
	// macOS 13+ serves it from the Cryptexes mount, older releases from
	// /private/var/db/dyld. A missing path in a read-only allow costs nothing.
	"/System/Volumes/Preboot/Cryptexes/OS/usr/lib",
	"/private/var/db/dyld",
	// Toolchain prefixes. Read-only: /opt/homebrew and /usr/local are
	// user-writable locations, so being able to READ them is what lets the
	// workload run its tools, while the absence of any write rule keeps a
	// compromised run from modifying the operator's toolchain.
	"/Library/Developer/CommandLineTools",
	"/opt/homebrew",
	"/usr/local",
}

// darwinDeviceReads are the character devices a normal toolchain needs. Without
// /dev/null a shell redirect fails outright (`ls > /dev/null` →
// "Operation not permitted") and `go vet` aborts before running.
//
// /dev/null gets file-write-data rather than file-write*: writing INTO the
// device is required, creating or unlinking device nodes is not.
var darwinDeviceReads = []string{"/dev/urandom", "/dev/random", "/dev/zero"}

// darwinTmpDirs are the writable temp roots. /tmp is a symlink to /private/tmp
// on macOS and sandbox-exec matches on the RESOLVED path, so a rule naming only
// /tmp matches nothing and the carve-out silently does not exist. Both are
// emitted.
var darwinTmpDirs = []string{"/tmp", "/private/tmp"}

// sandboxExecProfile builds the macOS sandbox-exec profile for spec: deny by
// default, an explicit network denial, a read-only view of the snapshot and of
// the system/toolchain tier, and write access confined to cfg.ScratchDir and
// /tmp.
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
//
// The read-only snapshot is enforced twice, and the second time is the one that
// holds. sandbox-exec applies the LAST matching rule, so the profile ends with
// an explicit `(deny file-write* (subpath <snapshot>))`. That matters because a
// snapshot routinely lives INSIDE a writable root: os.MkdirTemp("", …) — which
// is where both the real snapshot (internal/tools/snapshot.go) and Preflight's
// probe dir come from — returns a path under /tmp whenever TMPDIR is unset, as
// it is under launchd, cron and CI. Without the trailing deny, the /tmp
// carve-out silently makes the snapshot writable; a review reproduced exactly
// that, overwriting a snapshot file from inside the sandbox.
func sandboxExecProfile(cfg OSLevelConfig, spec RunSpec) (string, error) {
	if err := spec.validate(); err != nil {
		return "", err
	}
	snapshot, err := profileSafePath("SnapshotDir", spec.SnapshotDir)
	if err != nil {
		return "", err
	}
	// An empty scratch dir is valid: it yields no scratch carve-out. That is no
	// ADDITIONAL access beyond the unconditional /tmp roots — not zero writable
	// access, which the previous wording implied.
	var scratch string
	if cfg.ScratchDir != "" {
		if scratch, err = profileSafePath("ScratchDir", cfg.ScratchDir); err != nil {
			return "", err
		}
		if err := assertUsableWritableRoot("ScratchDir", scratch); err != nil {
			return "", err
		}
		if err := assertDisjointPaths(snapshot, scratch); err != nil {
			return "", err
		}
	}

	metadataScope := append([]string{}, darwinSystemReadDirs...)
	metadataScope = append(metadataScope, darwinTmpDirs...)
	metadataScope = append(metadataScope, "/dev", snapshot)
	if scratch != "" {
		metadataScope = append(metadataScope, scratch)
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
	// file-read-metadata is scoped rather than global. Unscoped, it let a
	// workload stat ~/.ssh/id_ed25519 and read back its size and mtime — no
	// content, but enough to enumerate an operator's secrets. Measured: scoping
	// it to the same directories still satisfies dyld's path resolution.
	b.WriteString("(allow file-read-metadata " + profileLiteral("/") + " " +
		strings.Join(profileSubpaths(metadataScope), " ") + ")\n")
	b.WriteString(profileAllowRootRead + "\n")
	for _, dir := range darwinSystemReadDirs {
		b.WriteString(profileReadRule(dir) + "\n")
	}
	for _, dev := range darwinDeviceReads {
		b.WriteString("(allow file-read* " + profileLiteral(dev) + ")\n")
	}
	b.WriteString("(allow file-read* file-write-data " + profileLiteral("/dev/null") + ")\n")
	b.WriteString(profileReadRule(snapshot) + "\n")
	if scratch != "" {
		b.WriteString(profileWriteRule(scratch) + "\n")
	}
	for _, dir := range darwinTmpDirs {
		b.WriteString(profileWriteRule(dir) + "\n")
	}
	// LAST, so last-match-wins re-asserts it over every carve-out above,
	// including a /tmp rule covering a snapshot that lives under /tmp.
	b.WriteString("(deny file-write* " + profileSubpath(snapshot) + ")\n")
	return b.String(), nil
}

func profileReadRule(dir string) string {
	return "(allow file-read* " + profileSubpath(dir) + ")"
}

func profileWriteRule(dir string) string {
	return "(allow file-read* file-write* " + profileSubpath(dir) + ")"
}

// profileSubpath/profileLiteral build the two path clause forms.
//
// They concatenate the validated bytes rather than using %q. %q re-escapes
// invalid UTF-8 and non-printable runes with backslashes, so it could reinsert
// the very metacharacter profileSafePath rejects — an escaper smuggled back
// into the one place this file refuses to have one. Concatenation makes the
// emitted bytes provably identical to the validated bytes.
func profileSubpath(dir string) string { return `(subpath "` + dir + `")` }

func profileLiteral(path string) string { return `(literal "` + path + `")` }

func profileSubpaths(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, profileSubpath(d))
	}
	return out
}

// profileDSLMetacharacters are the characters that can break out of a
// `(subpath "...")` string literal or change how the profile parses around it:
// a quote closes the literal, the parens close or open a clause, a backslash
// starts an escape, a semicolon begins a comment, and a newline ends the line.
const profileDSLMetacharacters = "\"()\\;\n\r\t"

// darwinSymlinkedPrefixes are the macOS system paths that are symlinks into
// /private. sandbox-exec matches on the RESOLVED path, so a rule naming an
// unresolved one matches nothing at all — a snapshot at /var/folders/…, which
// is exactly what os.MkdirTemp returns on a normal macOS host, would get a
// carve-out that grants precisely zero access while Preflight still reported
// success.
var darwinSymlinkedPrefixes = map[string]string{
	"/tmp": "/private/tmp",
	"/var": "/private/var",
	"/etc": "/private/etc",
}

// profileSafePath validates a path before it is interpolated into a profile
// literal, and returns it cleaned and normalized onto its resolved prefix.
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
//
// Normalization is prefix-based and therefore pure — the generator does no I/O,
// which is what lets the containment boundary be asserted without a filesystem.
// It handles the symlinks macOS actually ships. An ARBITRARY symlink (an
// operator symlinking their snapshot dir) is still not resolved here, and the
// residual risk of that is neutralized structurally rather than by detection:
// the profile's trailing `(deny file-write* <snapshot>)` wins under
// last-match-wins no matter which alias some other rule was written against.
func profileSafePath(field, path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("sandbox: %s must be an absolute path, got %q", field, path)
	}
	if i := strings.IndexAny(path, profileDSLMetacharacters); i >= 0 {
		return "", fmt.Errorf("sandbox: %s must not contain %q (sandbox-exec profile injection), got %q",
			field, string(path[i]), path)
	}
	if !utf8.ValidString(path) {
		return "", fmt.Errorf("sandbox: %s must be valid UTF-8 (a profile literal is emitted verbatim), got %q", field, path)
	}
	for _, r := range path {
		if !unicode.IsPrint(r) {
			return "", fmt.Errorf("sandbox: %s must not contain non-printable characters, got %q", field, path)
		}
	}
	clean := filepath.Clean(path)
	for prefix, resolved := range darwinSymlinkedPrefixes {
		if clean == prefix {
			return resolved, nil
		}
		if strings.HasPrefix(clean, prefix+string(filepath.Separator)) {
			return resolved + strings.TrimPrefix(clean, prefix), nil
		}
	}
	return clean, nil
}

// assertUsableWritableRoot rejects a writable root that is the filesystem root
// or a system directory.
//
// `(allow file-read* file-write* (subpath "/"))` grants the entire filesystem
// and, under last-match-wins, overrides the deny-default completely. A review
// reached exactly that by configuring ScratchDir="/" — the nesting guard's
// string-prefix arithmetic produced "//" and passed it through — and then read
// ~/.ssh and wrote into $HOME from inside the sandbox. It is checked explicitly
// rather than left to the disjointness math, because it is the one value whose
// blast radius is total.
func assertUsableWritableRoot(field, path string) error {
	if path == string(filepath.Separator) {
		return fmt.Errorf("sandbox: %s must not be the filesystem root: a writable carve-out at %q "+
			"grants the whole filesystem and overrides the deny-default", field, path)
	}
	for _, dir := range darwinSystemReadDirs {
		if pathContains(path, dir) {
			return fmt.Errorf("sandbox: %s %q must not contain the system directory %q: "+
				"a writable carve-out there would let a run modify the toolchain that validates it",
				field, path, dir)
		}
	}
	return nil
}

// assertDisjointPaths rejects a scratch directory that is the snapshot, sits
// inside it, or contains it.
//
// Rejecting rather than silently relocating is decided (AC 02-01 Edge Case 2,
// AC 02-02 Edge Case 2). An overlap means the writable allow rule covers part
// of the tree the read-only rule is supposed to protect, and on Linux the same
// overlap makes a later writable --bind shadow an earlier --ro-bind. Moving the
// caller's scratch path would change where a workload's output lands without
// telling anyone.
func assertDisjointPaths(snapshot, scratch string) error {
	switch {
	case snapshot == scratch:
		return fmt.Errorf("sandbox: ScratchDir must not be the snapshot directory itself (%q): "+
			"the writable carve-out would cover the read-only snapshot", scratch)
	case pathContains(snapshot, scratch):
		return fmt.Errorf("sandbox: ScratchDir %q must not be inside SnapshotDir %q: "+
			"the writable carve-out would cover part of the read-only snapshot", scratch, snapshot)
	case pathContains(scratch, snapshot):
		return fmt.Errorf("sandbox: SnapshotDir %q must not be inside ScratchDir %q: "+
			"the writable carve-out would cover the whole read-only snapshot", snapshot, scratch)
	}
	return nil
}

// pathContains reports whether child is parent or lies beneath it.
//
// Containment is decided component-wise via filepath.Rel, not by string prefix.
// A prefix test gets this wrong in both directions: it calls "/a/snap-2" nested
// inside "/a/snap" (a false positive on a perfectly safe sibling), and — the
// direction that actually cost something — it misses "/" as a parent, because
// "/" + separator is "//" and no absolute path starts with that. That miss is
// what let a full-filesystem writable rule be generated.
func pathContains(parent, child string) bool {
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
