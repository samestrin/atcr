// Package sandbox provides an opt-in, container-isolated executor for running
// untrusted, model-authored commands and scripts during a review (Epic 11.0).
//
// Execution is the last and most dangerous rung of the review ladder: it runs
// code an LLM wrote on the operator's machine. Containment is therefore not a
// feature but the precondition. Every Backend MUST guarantee, for every Run:
//
//   - no network (the run cannot exfiltrate or call out),
//   - a read-only view of the snapshot (the run cannot mutate the work tree),
//   - a bounded wall-clock duration and a cap on concurrent runs,
//   - the minimum privilege its isolation mechanism can express.
//
// Those are the universal MUSTs. The following are guaranteed by DockerBackend
// and NOT by every backend, because they depend on a mechanism the host may not
// provide:
//
//   - memory, CPU, and PID caps,
//   - a non-root user, dropped capabilities, and no-new-privileges.
//
// OSLevelBackend (oslevel.go) is the exception: it wraps the platform's native
// process sandbox — macOS sandbox-exec, Linux bwrap — which isolates the
// filesystem and network but shares the host kernel and has no image, no
// rootfs remount, and no capability set to drop. bwrap supplies a PID
// namespace. Neither tool caps resources itself, so the backend adds the
// memory and PID caps outside the tool: on linux a per-run cgroup v2
// (memory.max, pids.max) with an RLIMIT_DATA fallback for memory, and on
// darwin nothing, because XNU rejects lowering RLIMIT_AS and RLIMIT_DATA and
// RLIMIT_NPROC counts the whole user. Preflight refuses an operator-set cap
// the host cannot enforce. CPU is capped on neither platform, and the run
// keeps the invoking user's identity.
// A caller that needs the full list must require DockerBackend specifically
// rather than any Backend. This distinction is stated here rather than left
// implicit because the OS-level backend is operator-selectable, and an operator
// choosing it is accepting a genuinely weaker containment posture than Docker's
// in exchange for not needing a daemon.
//
// The package never enables itself: callers opt in via `--exec` and a backend
// that passes Preflight. Bare-metal execution is intentionally unsupported.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// RunSpec describes a single sandboxed execution request. Exactly one of Command
// or Script must be set.
type RunSpec struct {
	// Command is an argv executed directly inside the sandbox. No shell
	// interpolation is performed. Mutually exclusive with Script.
	Command []string
	// Script is a shell script body fed to `/bin/sh -s` over stdin and executed
	// in the writable scratch overlay. It is never interpolated into argv.
	// Mutually exclusive with Command.
	Script string
	// Timeout bounds the wall-clock duration of the run. Zero uses the backend
	// default.
	Timeout time.Duration
	// SnapshotDir is the host directory mounted read-only as the working tree.
	SnapshotDir string
	// Writable opts into an ephemeral writable working tree. It defaults to
	// false, preserving the read-only behavior every current caller relies on.
	//
	// The universal guarantee, on every backend: when true, the snapshot itself
	// stays read-only and the writable tree the workload mutates is an
	// ephemeral copy — it (and every write into it) dies with the run, so no
	// host file is ever mutated. The package-level read-only-snapshot MUST is
	// thereby narrowed, not weakened.
	//
	// Working-tree path, which differs by platform under OSLevelBackend: on
	// Linux bwrap reuses Docker's mount points (copy at /work, snapshot at /src,
	// --chdir /work), so a workload sees the same paths as under DockerBackend.
	// On darwin sandbox-exec applies a policy to host paths and cannot remap
	// them, so the run executes in the copy at its host path
	// (<scratch>/work) and there is no /work and no /src. A validate_command
	// that hardcodes /work therefore works under Docker and Linux but fails on
	// darwin; a portable command uses relative paths from its working
	// directory.
	//
	// Everything below is DockerBackend-specific mechanics; OSLevelBackend
	// provides the same guarantee differently (a host-side copy seeded into the
	// per-run scratch tree before the spawn — no shell wrapper, no image
	// requirement; see oslevel.go's seedWritableCopy).
	//
	// DockerBackend mounts the snapshot at /src instead of /work, and /work is
	// backed by an ephemeral tmpfs seeded by a `tar` pipe from /src that skips
	// .git, node_modules, vendor and .terraform (docker.go's writableSeedCopy).
	// The copy step is injected differently per mode. Command mode wraps the argv as
	// `/bin/sh -c '<seed copy>; cd /work && exec "$@"' -- <command...>`,
	// passing the original command positionally so no token is interpolated into the
	// shell text. Script mode instead prepends `<seed copy>; cd /work\n` to
	// the script body before it is streamed to `sh -s` over stdin, so the copy step
	// travels as stdin data and the Script-mode argv is unchanged. A failed copy
	// exits 125 in both modes.
	//
	// Image requirement (DockerBackend only): both paths need a POSIX shell, so
	// the run image must ship /bin/sh and a `tar` supporting --exclude (true for
	// alpine/golang-family images, false for distroless/scratch).
	Writable bool
	// Collect lists files to read back out of the writable tree after the run,
	// as paths relative to the tree root (/work under Docker). Ignored unless
	// Writable. Entries that are empty, absolute, or escape the tree ("..") are
	// dropped; at most 64 are honoured. Only regular files come back — a symlink,
	// or a path through a symlinked directory, is skipped — each truncated to
	// CollectMaxFileBytes and all together to CollectMaxTotalBytes.
	//
	// The writable tree dies with the run, so collection happens before it does:
	// OSLevelBackend copies from the scratch tree before cleanup; DockerBackend
	// (whose /work is a tmpfs in a --rm container) runs an in-container
	// `tar | base64` pass after the command and parses it off the end of the
	// output on the host. That pass needs `tar`, `head`, `base64` and `dirname`
	// in the run image (true for alpine/busybox), and for Script mode it is an
	// EXIT trap, so a script that installs its own EXIT trap disables it. No
	// extra mount is added.
	Collect []string
	// LoopbackPorts lists the TCP ports the workload may bind, accept on and
	// connect to on loopback, for a run that starts its own server and talks to
	// it (a dev server and a browser). Empty, the default, means no network of
	// any kind. Each port must be 1-65535, listed once, at most
	// maxLoopbackPorts in all.
	//
	// DockerBackend (--network none) and the Linux OS-level backend
	// (--unshare-net) already give every run its own private loopback, so the
	// field changes nothing there. The darwin OS-level backend has no network
	// namespace: its loopback IS the host's, so the field opens exactly these
	// ports on it (oslevel_profile.go's loopbackRules) and Run refuses with
	// ErrOSLevelLoopbackPortHeld when a host process already listens on one.
	// UDP, unix sockets, every other port and every non-loopback address stay
	// denied.
	LoopbackPorts []int
}

// maxLoopbackPorts bounds RunSpec.LoopbackPorts. A Playwright config starts one
// web server per entry, and each grant is a hole in the darwin host's loopback,
// so the list is kept short.
const maxLoopbackPorts = 8

func (s RunSpec) validate() error {
	hasCmd := len(s.Command) > 0
	hasScript := s.Script != ""
	switch {
	case hasCmd && hasScript:
		return errors.New("sandbox: RunSpec must set exactly one of Command or Script, not both")
	case !hasCmd && !hasScript:
		return errors.New("sandbox: RunSpec must set one of Command or Script")
	}
	if s.SnapshotDir == "" {
		return errors.New("sandbox: RunSpec.SnapshotDir is required")
	}
	// The snapshot dir is interpolated into the `-v <dir>:/work:ro` mount spec.
	// A path containing ':' could inject extra mount options (e.g. strip :ro), so
	// reject it; require an absolute path so the mount source is unambiguous.
	if !filepath.IsAbs(s.SnapshotDir) {
		return fmt.Errorf("sandbox: RunSpec.SnapshotDir must be absolute, got %q", s.SnapshotDir)
	}
	if strings.ContainsRune(s.SnapshotDir, ':') {
		return fmt.Errorf("sandbox: RunSpec.SnapshotDir must not contain ':' (mount-spec injection), got %q", s.SnapshotDir)
	}
	if len(s.LoopbackPorts) > maxLoopbackPorts {
		return fmt.Errorf("sandbox: RunSpec.LoopbackPorts lists %d ports, at most %d are allowed", len(s.LoopbackPorts), maxLoopbackPorts)
	}
	seen := make(map[int]bool, len(s.LoopbackPorts))
	for _, p := range s.LoopbackPorts {
		// Port 0 is refused too: it asks for an ephemeral port, which a profile
		// rule cannot name in advance without a wildcard.
		if p < 1 || p > 65535 {
			return fmt.Errorf("sandbox: RunSpec.LoopbackPorts entry %d is not a port in 1-65535", p)
		}
		if seen[p] {
			return fmt.Errorf("sandbox: RunSpec.LoopbackPorts lists port %d twice", p)
		}
		seen[p] = true
	}
	return nil
}

// RunResult is the captured outcome of a sandboxed execution. A non-zero program
// exit is reported via ExitCode, never as a Go error.
type RunResult struct {
	// Command is a human-readable rendering of what was executed, suitable for
	// the evidence_exec block and the report.
	Command string
	// ExitCode is the container's exit status (the program's, since the entry
	// process is the program). 124 is the conventional timeout code.
	ExitCode int
	// Output is the combined stdout+stderr, truncated to the backend budget.
	Output string
	// TimedOut is true when the run exceeded its deadline and was killed.
	TimedOut bool
	// Files holds the contents of RunSpec.Collect, keyed by the cleaned relative
	// path. It is a non-nil (possibly empty) map whenever Collect was non-empty
	// — including when Writable is false, where it stays empty — and nil
	// otherwise. Files that were missing, not regular, or over budget are absent.
	Files map[string][]byte
}

// Backend is a pluggable sandbox executor. DockerBackend (docker.go) and
// OSLevelBackend (oslevel.go) implement it; the interface keeps Podman (or a
// remote runner) a drop-in later. See the package doc for which containment
// guarantees are universal and which are Docker-only.
type Backend interface {
	// Name identifies the backend for diagnostics and the evidence trail.
	Name() string
	// Preflight verifies the backend is usable: its mechanism is installed and
	// reachable, its prerequisites are met, and a trivial workload runs to
	// completion under it. (For Docker that is the daemon, the base image, and a
	// throwaway container; for the OS-level backend, the sandboxing binary, the
	// host's namespace support, and a probe run.) It MUST pass before Run is
	// used; the CLI refuses `--exec` otherwise.
	Preflight(ctx context.Context) error
	// Run executes spec in isolation and returns the captured result. err is
	// reserved for backend faults (spawn failure, malformed spec); a non-zero
	// program exit is NOT an error.
	Run(ctx context.Context, spec RunSpec) (RunResult, error)
}

// timeoutExitCode is the conventional exit status for a killed-on-timeout run,
// matching coreutils `timeout`.
const timeoutExitCode = 124

var _ Backend = (*DockerBackend)(nil)

// truncate shortens s to at most limit bytes (without splitting a UTF-8 rune),
// appending a marker when it cut anything.
func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	// Build the marker first so we can reserve its bytes in the budget.
	keep := limit
	marker := fmt.Sprintf("\n[...truncated %d bytes...]", len(s)-keep)
	keep = limit - len(marker)
	if keep < 0 {
		return marker[:limit]
	}
	for keep > 0 && (s[keep]&0xC0) == 0x80 { // back up off a UTF-8 continuation byte
		keep--
	}
	marker = fmt.Sprintf("\n[...truncated %d bytes...]", len(s)-keep)
	return s[:keep] + marker
}
