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
	// RunSpec.validate accepts "/" — it only checks absolute and colon-free — so
	// without this the profile would emit (allow file-read* (subpath "/")) and
	// grant a full-filesystem read, exactly the root-subtree grant this file
	// warns about.
	if err := assertUsableSource("SnapshotDir", snapshot,
		append(append([]string{}, darwinSystemReadDirs...), darwinHomeRoots...)); err != nil {
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
		if err := assertUsableWritableRoot("ScratchDir", scratch, darwinSystemReadDirs, nil); err != nil {
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
func assertUsableWritableRoot(field, path string, protected, mountPoints []string) error {
	if path == string(filepath.Separator) {
		return fmt.Errorf("sandbox: %s must not be the filesystem root: a writable carve-out at %q "+
			"grants the whole filesystem and overrides the deny-default", field, path)
	}
	// Bidirectional. Rejecting only "path contains dir" catches ScratchDir=/usr
	// but misses ScratchDir=/usr/local/atcr-scratch — the direction that
	// actually punches a writable hole in the read-only toolchain, and the one a
	// review exercised by writing a file into /usr/local from inside the
	// sandbox while this function returned nil.
	for _, dir := range protected {
		if pathContains(path, dir) || pathContains(dir, path) {
			return fmt.Errorf("sandbox: %s %q overlaps the system directory %q: "+
				"a writable carve-out there would let a run modify the toolchain that validates it",
				field, path, dir)
		}
	}
	// One direction only for the mount points the sandbox establishes itself: a
	// scratch dir INSIDE /tmp is ordinary (that is where os.MkdirTemp puts it,
	// and it lands inside the fresh tmpfs), while a scratch dir that IS /tmp
	// re-binds the host's /tmp over that tmpfs and undoes the isolation it
	// exists for.
	for _, dir := range mountPoints {
		if pathContains(path, dir) {
			return fmt.Errorf("sandbox: %s %q would replace the sandbox's own %q mount with a host bind",
				field, path, dir)
		}
	}
	return nil
}

// assertUsableSource rejects a path that must never be exposed inside the
// sandbox even read-only: the filesystem root, a system tree, or a directory
// holding every user's home.
//
// RunSpec.validate requires SnapshotDir to be absolute and colon-free
// (sandbox.go:97-102) and stops there, so nothing else refuses SnapshotDir="/".
// A review passed exactly that and read the operator's private SSH key out of
// the resulting sandbox at /work.
//
// Containment is one-directional here: the snapshot must not BE or CONTAIN one
// of these trees. A snapshot inside a home directory is the normal case for a
// developer's repository and stays allowed.
func assertUsableSource(field, path string, mustNotContain []string) error {
	if path == string(filepath.Separator) {
		return fmt.Errorf("sandbox: %s must not be the filesystem root: binding %q into the sandbox "+
			"exposes the entire host filesystem to the run", field, path)
	}
	for _, dir := range mustNotContain {
		if pathContains(path, dir) {
			return fmt.Errorf("sandbox: %s %q is or contains %q, which must never be exposed inside the sandbox",
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

// Sandbox mount points, matching dockerRunArgs (docker.go:172-189) exactly:
// the snapshot is the working tree at /work when read-only, and moves to /src
// with a writable copy at /work when RunSpec.Writable is set. Reusing Docker's
// mount points is what lets a workload that hardcodes /work behave identically
// on either backend, which is the point of Backend being a drop-in interface.
const (
	bwrapWorkDir = "/work"
	bwrapSrcDir  = "/src"
)

// linuxRequiredRoots must be visible or nothing can execute inside the
// namespaces — there is no dynamic loader, no shell, and no toolchain without
// /usr. It is bound read-only at its own path.
var linuxRequiredRoots = []string{"/usr"}

// linuxOptionalRoots are bound read-only with --ro-bind-try, which skips a path
// that does not exist instead of aborting the sandbox. Most modern distributions
// symlink /bin, /sbin, /lib and /lib64 into /usr (usrmerge), so on those hosts
// these are absent as real directories and a hard --ro-bind would fail every
// run.
var linuxOptionalRoots = []string{"/bin", "/sbin", "/lib", "/lib64"}

// linuxOptionalFiles are the individual /etc entries a toolchain reads at
// startup. They are named one by one rather than binding /etc wholesale: a
// review measured 296 entries under /etc on a real host, including
// operator-readable service credentials (`/etc/fail2ban/action.d/*token*.conf`),
// and "everything the operator's uid may read" is not the "limited to what
// execution requires, not a blanket" standard this file holds itself to. DAC
// still protects /etc/shadow, but DAC is not this sandbox's contribution.
//
// /etc/resolv.conf is deliberately absent: with the network namespace unshared
// there is nothing to resolve.
var linuxOptionalFiles = []string{
	"/etc/passwd",
	"/etc/group",
	"/etc/nsswitch.conf",
	"/etc/localtime",
	"/etc/ssl/certs",
	"/etc/alternatives",
}

// linuxPseudoMounts are the mount points bwrap establishes itself (--proc,
// --dev, --tmpfs). A bind over one of them replaces it with the host's, which
// is how a scratch dir at /proc turns a private procfs back into the host's
// process table — a review reproduced exactly that, reading
// /proc/1/cmdline and 702 host PIDs from inside the sandbox.
var linuxPseudoMounts = []string{"/proc", "/dev", "/tmp"}

// linuxHomeRoots must never be the snapshot: binding one of them in would
// expose every user's home read-only inside the sandbox. A snapshot INSIDE a
// home directory is ordinary and stays allowed — it is the tree itself that is
// refused.
var linuxHomeRoots = []string{"/home", "/root"}

// darwinHomeRoots is the macOS counterpart of linuxHomeRoots.
var darwinHomeRoots = []string{"/Users", "/private/var/root"}

// sandboxArgvTerminator ends the sandboxing tool's own options so that
// everything after it is the workload, never another flag.
//
// This is load-bearing, not cosmetic. osLevelRunArgs appends RunSpec.Command
// directly after the containment arguments (oslevel.go), and RunSpec.Command is
// model-authored. Without the terminator, bwrap parses those tokens as ITS
// options: a review appended `--bind / /host --share-net` to the generated
// prefix on a real bubblewrap 0.9.0 and read ~/.ssh and wrote into the
// operator's home from inside the "sandbox". With the terminator the same argv
// fails closed — `bwrap: execvp --bind: No such file or directory`.
const sandboxArgvTerminator = "--"

// bwrapArgs builds the bubblewrap argument list for spec: full namespace
// isolation, a read-only view of the toolchain and the snapshot, an ephemeral
// /tmp, and write access confined to the scratch directory.
//
// bwrap's model is the inverse of sandbox-exec's. There is no profile and no
// deny-default clause to write: nothing is visible inside the namespaces unless
// it is explicitly bound in, so the containment IS the bind set. That is why
// the tests assert the PRESENCE of --unshare-net rather than the absence of a
// network allow — there is no "deny network" flag to assert against, and a
// missing --unshare-net is itself the containment failure.
//
// Like sandboxExecProfile this is pure: no filesystem access, no spawn, so the
// argv can be asserted without bwrap installed. Every path is a discrete argv
// element and the list is handed to os/exec with no shell, so a path containing
// spaces or shell metacharacters carries no injection risk.
func bwrapArgs(cfg OSLevelConfig, spec RunSpec) ([]string, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	snapshot := filepath.Clean(spec.SnapshotDir)
	// RunSpec.validate accepts "/" — absolute and colon-free is all it checks —
	// so without this a SnapshotDir of "/" would emit `--ro-bind / /work` and
	// hand the run the entire host filesystem. A review did exactly that and
	// read the operator's SSH private key out of /work.
	if err := assertUsableSource("SnapshotDir", snapshot,
		append(linuxProtectedRoots(), linuxHomeRoots...)); err != nil {
		return nil, err
	}
	scratch := ""
	if cfg.ScratchDir != "" {
		if !filepath.IsAbs(cfg.ScratchDir) {
			return nil, fmt.Errorf("sandbox: ScratchDir must be an absolute path, got %q", cfg.ScratchDir)
		}
		scratch = filepath.Clean(cfg.ScratchDir)
		if err := assertUsableWritableRoot("ScratchDir", scratch, linuxProtectedRoots(), linuxPseudoMounts); err != nil {
			return nil, err
		}
		if err := assertDisjointPaths(snapshot, scratch); err != nil {
			return nil, err
		}
	}
	if spec.Writable && scratch == "" {
		return nil, fmt.Errorf("sandbox: RunSpec.Writable requires a ScratchDir to back the writable %s tree", bwrapWorkDir)
	}

	args := []string{
		// Network isolation. --unshare-net removes every interface but loopback,
		// which blocks all socket families rather than just TCP — stronger in
		// scope than Docker's --network none.
		"--unshare-net",
		// PID/IPC/UTS/cgroup isolation, so the workload cannot see, signal, or
		// reach host processes, shared memory, or the host's cgroup tree.
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--unshare-cgroup-try",
		// The user namespace is requested EXPLICITLY rather than left to bwrap's
		// "may be automatically implied if not setuid" behaviour. A review ran
		// the generated argv on a real host and found the workload sharing the
		// host user namespace (`readlink /proc/self/ns/user` identical to the
		// host's) while net and pid were fresh — so on a setuid installation, or
		// when atcr itself runs as root, the implication does not hold and the
		// run keeps the invoking user's privileges. Stating it in the argv makes
		// the posture a property of what we asked for, not of how the binary
		// happens to be installed.
		// The hard form, not --unshare-user-try: --disable-userns below refuses
		// to start without it ("bwrap: --disable-userns requires
		// --unshare-user", measured on bubblewrap 0.9.0), and a sandbox that
		// cannot guarantee its own user namespace is not one this backend should
		// silently fall back to.
		"--unshare-user",
		// Refuse nested user namespaces, which are the usual route from inside a
		// sandbox to a kernel LPE primitive.
		"--disable-userns",
		// Reap the sandbox if atcr dies, so a workload cannot outlive the
		// process that is supposed to bound it.
		"--die-with-parent",
		// A separate session, so the workload cannot inject keystrokes into the
		// operator's terminal with TIOCSTI.
		"--new-session",
		// A private /proc is required once the PID namespace is unshared —
		// without it the workload sees the host's process table.
		"--proc", "/proc",
		// A minimal device tree: null, zero, random, urandom, tty. Not a bind of
		// the host /dev, so no block device or raw hardware node is reachable.
		"--dev", "/dev",
	}

	// Read-only mounts first. bwrap applies binds in argv order, so everything
	// read-only is established before any writable mount, and the shadowing
	// check below proves no later writable mount covers one of them.
	for _, dir := range linuxRequiredRoots {
		args = append(args, "--ro-bind", dir, dir)
	}
	for _, dir := range linuxOptionalRoots {
		args = append(args, "--ro-bind-try", dir, dir)
	}
	for _, path := range linuxOptionalFiles {
		args = append(args, "--ro-bind-try", path, path)
	}
	// An ephemeral /tmp rather than a bind of the host's. A shared host /tmp
	// exposes the run to other processes' temp files and to the predictable-path
	// and symlink-race preconditions that come with a world-writable directory;
	// a fresh tmpfs closes both by construction and dies with the sandbox. This
	// continues DockerBackend's ephemeral --tmpfs /scratch precedent, and is
	// deliberately stronger than the macOS side, where sandbox-exec can only
	// allow the real /tmp.
	args = append(args, "--tmpfs", "/tmp")

	if spec.Writable {
		args = append(args, "--ro-bind", snapshot, bwrapSrcDir)
	} else {
		args = append(args, "--ro-bind", snapshot, bwrapWorkDir)
	}
	if scratch != "" {
		// Bound at its own host path so the scratch directory is reachable at
		// the same path inside the sandbox as outside it. That is what lets Run
		// point HOME, TMPDIR, GOCACHE and the XDG cache at a host path
		// (sandboxEnv, oslevel.go) — a toolchain with an unreachable HOME fails
		// before it starts. NOTE: Run does not yet populate cfg.ScratchDir with
		// the directory it creates, so this bind is only exercised by callers
		// that set it explicitly; wiring the two together is Phase 3/4 work.
		args = append(args, "--bind", scratch, scratch)
		if spec.Writable && scratch != bwrapWorkDir {
			// And again at /work, which is the writable tree the Docker mount
			// convention promises. Two mounts of one source is how bwrap
			// expresses "visible in both places"; there is no aliasing risk
			// because both are the same writable directory. Skipped when the
			// scratch dir IS /work, which would emit the identical bind twice.
			args = append(args, "--bind", scratch, bwrapWorkDir)
		}
	}
	args = append(args, "--chdir", bwrapWorkDir)
	// Terminate bwrap's options. osLevelRunArgs appends the model-authored
	// workload immediately after these arguments, and without the terminator
	// bwrap parses those tokens as its own flags — a review used that to rebind
	// the host root and re-share the network from a RunSpec.Command.
	args = append(args, sandboxArgvTerminator)

	// The shadowing check runs on the ASSEMBLED argv rather than on the inputs,
	// so it holds however the bind set above is later rearranged. bwrap applies
	// binds in order, so a writable mount covering an earlier read-only one
	// silently widens access — the containment failure AC 02-02 Error Scenario 2
	// requires an error for rather than a silently-wider argv.
	if err := assertNoBindShadowing(args); err != nil {
		return nil, err
	}
	return args, nil
}

// linuxProtectedRoots are the mounts a writable scratch directory must not
// contain: the toolchain the validation command runs from.
func linuxProtectedRoots() []string {
	roots := append([]string{}, linuxRequiredRoots...)
	roots = append(roots, linuxOptionalRoots...)
	roots = append(roots, "/etc")
	// The pseudo-filesystems the sandbox mounts itself. A writable bind over one
	// of them replaces it with the host's — a review bound the host /proc back
	// over the private one and read /proc/1/cmdline and 702 host PIDs.
	return append(roots, "/sys", "/run", "/proc", "/dev")
}

// assertNoBindShadowing rejects an argv in which a writable --bind is applied
// at or above the destination of an earlier read-only mount.
func assertNoBindShadowing(args []string) error {
	type mount struct {
		dest     string
		writable bool
		index    int
	}
	var mounts []mount
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--bind":
			if i+2 >= len(args) {
				return fmt.Errorf("sandbox: malformed bwrap argv: --bind is missing an operand")
			}
			mounts = append(mounts, mount{dest: args[i+2], writable: true, index: i})
			i += 2
		case "--ro-bind", "--ro-bind-try":
			if i+2 >= len(args) {
				return fmt.Errorf("sandbox: malformed bwrap argv: %s is missing an operand", args[i])
			}
			mounts = append(mounts, mount{dest: args[i+2], index: i})
			i += 2
		case "--proc", "--dev", "--tmpfs", "--mqueue":
			// The mounts bwrap makes itself take a single DEST operand. They are
			// tracked because a later writable bind covering one of them
			// replaces it with a host directory — the private /proc becoming the
			// host's process table is the case a review demonstrated, and it was
			// invisible to a check that only knew about bind flags.
			if i+1 >= len(args) {
				return fmt.Errorf("sandbox: malformed bwrap argv: %s is missing its destination", args[i])
			}
			mounts = append(mounts, mount{dest: args[i+1], index: i})
			i++
		}
	}
	for _, later := range mounts {
		if !later.writable {
			continue
		}
		for _, earlier := range mounts {
			if earlier.writable || earlier.index > later.index {
				continue
			}
			if pathContains(later.dest, earlier.dest) {
				return fmt.Errorf("sandbox: writable bind at %q would shadow the read-only mount at %q "+
					"(bwrap applies binds in order, so this silently widens access)", later.dest, earlier.dest)
			}
		}
	}
	return nil
}
