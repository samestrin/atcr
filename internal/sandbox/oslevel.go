package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/samestrin/atcr/internal/log"
)

// osLevelBackendName identifies the backend in diagnostics and the evidence
// trail. It MUST stay equal to the `sandbox.fallback: os-level` config value
// (registry.SandboxFallbackOSLevel) — that string is how an operator connects
// what they configured to the backend named in a failure message.
const osLevelBackendName = "os-level"

// Platform sandboxing binaries. macOS ships sandbox-exec in the base system;
// Linux uses bubblewrap, which provides mount/network/PID namespace isolation
// in one tool (seccomp alone was considered and rejected during /refine-epic:
// it is a syscall filter with no filesystem or network isolation of its own).
const (
	darwinSandboxTool = "sandbox-exec"
	linuxSandboxTool  = "bwrap"
)

// osLevelToolFor maps a GOOS to its native sandboxing binary.
//
// Platform selection is a runtime switch rather than build-tagged files on
// purpose. .goreleaser.yaml ships windows/amd64 and windows/arm64 while CI runs
// on ubuntu-latest only, so a GOOS the build-tagged layout forgot would stay
// invisible until a release tag — and Phase 4 will wire NewOSLevelBackend into
// internal/verify and cli/, so that gap would then break their builds too. One
// file that compiles everywhere cannot have that hole, and taking the GOOS as a
// parameter makes every platform's decision assertable from any host.
//
// An unsupported platform fails closed: it returns an error rather than a
// best-effort passthrough. A backend that "succeeded" without a sandbox would
// run model-authored code with no containment at all.
func osLevelToolFor(goos string) (string, error) {
	switch goos {
	case "darwin":
		return darwinSandboxTool, nil
	case "linux":
		return linuxSandboxTool, nil
	default:
		return "", fmt.Errorf("sandbox: os-level sandbox is not supported on %s", goos)
	}
}

// OSLevelConfig parameterizes the OS-level backend. Zero values are not safe;
// use DefaultOSLevelConfig and override fields as needed.
type OSLevelConfig struct {
	// ToolPath is the platform sandboxing binary to invoke (sandbox-exec on
	// macOS, bwrap on Linux). A non-empty value MUST be an absolute path.
	//
	// Empty means Preflight resolves the platform default from a fixed list of
	// system directories — never from $PATH, which the operator's environment
	// controls — and pins the result; Run refuses until that has happened.
	// Tests inject a fake shim here, mirroring DockerConfig.DockerPath.
	ToolPath string
	// Timeout is the default per-run wall-clock budget when RunSpec.Timeout is 0.
	Timeout time.Duration
	// MaxOutputBytes truncates captured combined stdout+stderr.
	MaxOutputBytes int
	// ScratchDir is the per-run writable directory the containment profile/argv
	// carves out — the only place outside /tmp a run may write, and where
	// sandboxEnv points HOME/TMPDIR/GOCACHE. It MUST be absolute and MUST NOT
	// overlap RunSpec.SnapshotDir, or the writable carve-out would subsume the
	// read-only snapshot guarantee; the generators reject both cases.
	//
	// It lives on the config rather than RunSpec because it is a property of the
	// backend's execution environment, not of the caller's request: Run creates
	// one per run and passes a copy of the config carrying it, so the generators
	// stay pure functions of (cfg, spec). Empty means no writable carve-out
	// beyond /tmp — more restrictive, not less.
	ScratchDir string
	// MaxConcurrent bounds the number of sandboxed processes running at once
	// across this backend, mirroring DockerConfig.MaxConcurrent: a review
	// verifies findings concurrently and each skeptic may run many tools, so
	// without this cap a large finding set could exhaust the host.
	MaxConcurrent int
}

// DefaultOSLevelConfig returns a conservative default configuration. ToolPath is
// left empty so Preflight resolves the running platform's tool from the trusted
// system directories.
func DefaultOSLevelConfig() OSLevelConfig {
	return OSLevelConfig{
		Timeout:        60 * time.Second,
		MaxOutputBytes: 64 * 1024,
		MaxConcurrent:  4,
	}
}

// osLevelBackend runs commands and scripts under the platform's native process
// sandbox — sandbox-exec on macOS, bwrap on Linux — as a second Backend
// implementation alongside DockerBackend, for hosts without a Docker daemon.
//
// It is unexported so no sandbox-exec/bwrap-specific type crosses the Backend
// boundary; consumers hold it as a sandbox.Backend. NewOSLevelBackend returns
// the concrete type (mirroring NewDockerBackend) purely so in-package tests can
// reach cfg — see TD-002 if a cross-package assertion is ever needed.
type osLevelBackend struct {
	cfg OSLevelConfig
	// sem bounds concurrent sandbox spawns to cfg.MaxConcurrent (buffered slots).
	// NewOSLevelBackend allocates it; a struct-literal backend leaves it nil.
	sem chan struct{}
	// semOnce lazily allocates sem on first Run so a struct-literal backend
	// (bypassing NewOSLevelBackend) still enforces the cap instead of failing
	// open, as DockerBackend does at docker.go:237-245.
	semOnce sync.Once
	// goos overrides the platform the fault classifier reasons about. Empty
	// means runtime.GOOS. It exists so both platforms' classification can be
	// exercised from either host: CI is ubuntu-only and development is on
	// macOS, so without it each platform's branch would be dead code on the
	// machine that could have tested it.
	goos string
	// prerequisiteCheck verifies a host-specific precondition beyond the binary
	// being present (on Linux, that unprivileged user namespaces are usable).
	// It is an injectable seam because a real userns failure cannot be forced
	// through a shell shim. Nil means the platform default.
	prerequisiteCheck func(context.Context) error
	// procRoot overrides the filesystem root the Linux prerequisite check reads
	// sysctls from. Empty means "/". Injectable so the check is assertable from
	// a macOS dev box, not only on whichever Linux host CI happens to use.
	procRoot string
	// trustedDirs overrides the directories the platform default binary is
	// resolved from. Nil means trustedToolDirs. Injectable so the
	// trusted-directory control is assertable from a fixture rather than
	// depending on whether the host happens to have the real tool installed.
	trustedDirs []string

	// mu guards pinnedTool. Preflight and Run may be called concurrently — the
	// type is explicitly built for concurrent use (see MaxConcurrent/sem) — and
	// an unsynchronized write here would be a data race on the decision of
	// WHICH binary is spawned to contain untrusted code.
	mu sync.Mutex
	// pinnedTool is the absolute binary path a successful Preflight resolved.
	// It is deliberately NOT written back into cfg.ToolPath: cfg.ToolPath means
	// "the operator chose this binary", so overwriting it would turn a resolved
	// default into an apparent override, and a retried Preflight would then
	// re-validate a stale pin instead of resolving afresh.
	pinnedTool string
}

// platform reports the GOOS this backend classifies against.
func (b *osLevelBackend) platform() string {
	if b.goos != "" {
		return b.goos
	}
	return runtime.GOOS
}

var _ Backend = (*osLevelBackend)(nil)

// NewOSLevelBackend constructs an OS-level backend from cfg, flooring
// non-positive values to their defaults. It mirrors NewDockerBackend
// (docker.go:91-105): a caller must not have to pre-populate every field, and a
// zero or negative timeout would be an unbounded run — the same
// resource-exhaustion risk the Docker backend floors.
func NewOSLevelBackend(cfg OSLevelConfig) *osLevelBackend {
	def := DefaultOSLevelConfig()
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = def.MaxOutputBytes
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = def.MaxConcurrent
	}
	return &osLevelBackend{cfg: cfg, sem: make(chan struct{}, cfg.MaxConcurrent)}
}

// Name implements Backend. It returns a constant, so a struct-literal backend
// reports the same identifier as a constructed one.
func (b *osLevelBackend) Name() string { return osLevelBackendName }

// Timeout reports the backend's default per-run wall-clock budget — the value
// applied when RunSpec.Timeout is zero. Exposed for the same reason
// DockerBackend.Timeout is (docker.go:115): the value is a context deadline and
// never appears in the spawned argv, so a resolver's propagation of an
// operator's configured timeout cannot otherwise be asserted.
func (b *osLevelBackend) Timeout() time.Duration { return b.cfg.Timeout }

// Preflight implements Backend. It verifies, in strict order and
// short-circuiting on the first failure:
//
//  1. the platform is supported and its sandboxing binary is present and
//     executable (cheapest, so it runs first),
//  2. the host-specific prerequisite holds (on Linux, that unprivileged user
//     namespaces are usable — bwrap cannot isolate without them),
//  3. a trivial no-op Run actually completes through the real Run path.
//
// Step 3 matters: reporting success on binary presence alone would let the CLI
// enable --exec against a sandbox that cannot actually contain anything, which
// is DockerBackend.Preflight's reason for spawning a trivial container
// (docker.go:382-397). Every failure is wrapped so the cause stays reachable.
func (b *osLevelBackend) Preflight(ctx context.Context) error {
	// 1. Binary present and executable.
	//
	//    There is deliberately no separate containment pre-check here. The gate
	//    lives in osLevelRunArgs, on the real argv path, so step 3's trivial run
	//    inherits the refusal (ErrOSLevelNoContainment surfaces through the
	//    wrap) — and, unlike a check in this function, it also covers a caller
	//    that skips Preflight entirely. A pre-check here would additionally have
	//    to invent a RunSpec, and Phase 2's generators are spec-dependent
	//    (bwrap binds SnapshotDir; the sandbox-exec profile names it), so a
	//    zero-value probe would break the moment they land.
	resolved, err := b.resolveToolPath()
	if err != nil {
		return fmt.Errorf("sandbox preflight: os-level sandbox binary not usable: %w", err)
	}

	// 2. Host prerequisite, checked after the cheap binary test and before any
	//    spawn, so an unmet precondition fails fast with a specific message
	//    rather than surfacing as an opaque trivial-run failure.
	check := b.prerequisiteCheck
	if check == nil {
		check = func(ctx context.Context) error { return checkHostPrerequisite(ctx, b.platform(), b.procRoot) }
	}
	if err := check(ctx); err != nil {
		return fmt.Errorf("sandbox preflight: host prerequisite unmet: %w", err)
	}

	// 3. A trivial command actually runs through the real Run path AND proves
	//    the workload itself executed. Asserting only "exit 0" would pass for
	//    any binary that swallows its argv and exits 0 — including a shim that
	//    never runs the command — so the probe echoes a nonce that must come
	//    back in the output.
	tmpDir, err := os.MkdirTemp("", "atcr-oslevel-preflight-*")
	if err != nil {
		return fmt.Errorf("sandbox preflight: cannot create throwaway snapshot dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Both shapes are probed, because a green Preflight is what a resolver acts
	// on. RunSandboxedValidation hardcodes Writable:true for every --auto-fix
	// run (internal/verify/sandboxvalidate.go:72), so probing only the read-only
	// shape would report the backend ready while the writable argv, the scratch
	// partition and the seeding step had never been exercised — and the failure
	// would then land after --auto-fix had already applied its patch.
	for _, writable := range []bool{false, true} {
		nonce := "atcr-preflight-" + randHex(8)
		runCtx, cancel := context.WithTimeout(ctx, preflightRunTimeout)
		res, err := b.runWith(runCtx, resolved, RunSpec{
			Command:     []string{"/bin/sh", "-c", "echo " + nonce},
			SnapshotDir: tmpDir,
			Writable:    writable,
		}, true)
		cancel()
		if err != nil {
			// The tool's own stderr travels with the error. Without it the caller
			// sees only "bwrap failed to set up the sandbox (exit 1)" — bwrap
			// exits 1 for every one of its failures, so that message cannot
			// distinguish "this host forbids user namespaces" (an operator
			// problem with a known remedy) from "our argv is wrong" (our bug).
			// Both the operator's diagnostics and the integration suite's
			// skip-vs-fail decision depend on this text.
			return fmt.Errorf("sandbox preflight: trivial run failed (writable=%t): %w (tool output: %s)",
				writable, err, strings.TrimSpace(res.Output))
		}
		if res.TimedOut || res.ExitCode != 0 {
			return fmt.Errorf("sandbox preflight: trivial run exited %d (timed_out=%t, writable=%t, tool output: %s): %w",
				res.ExitCode, res.TimedOut, writable, strings.TrimSpace(res.Output), errPreflightTrivialRun)
		}
		if !strings.Contains(res.Output, nonce) {
			return fmt.Errorf("sandbox preflight: trivial run exited 0 but never executed the command "+
				"(nonce absent from output — %s may not be a sandboxing tool): %w",
				resolved, errPreflightTrivialRun)
		}
	}

	// Pin only here, on the success path, so a failed Preflight leaves the
	// backend's configuration exactly as the operator set it. Every later Run
	// then spawns this exact binary instead of re-resolving a name, closing the
	// window between preflight and spawn (TD-003).
	b.mu.Lock()
	b.pinnedTool = resolved
	b.mu.Unlock()
	return nil
}

// ErrOSLevelNoContainment is returned when the platform containment generator
// produced no arguments at all, so a spawn would provide no isolation. It is a
// sentinel so a caller can branch on "this backend cannot contain anything"
// with errors.Is rather than by matching message text. The generators are wired
// (osLevelContainmentArgs), so on a supported platform this now marks a
// regression rather than the expected state.
var ErrOSLevelNoContainment = errors.New("os-level sandbox provides no containment yet")

// errPreflightTrivialRun marks a preflight failure at the trivial-run step, so a
// caller can tell "the sandbox could not run" from "the binary was missing"
// without matching message text.
var errPreflightTrivialRun = errors.New("trivial sandbox run did not complete cleanly")

// osLevelContainmentArgs returns the tool arguments that actually establish
// containment — the deny-by-default sandbox-exec profile flags on macOS, the
// bind-mount and namespace flags on Linux.
//
// This is the single seam between the backend and the pure generators in
// oslevel_profile.go, and it dispatches rather than deriving: re-deriving
// `{"-p", profile, "--"}` here would put a second copy of the argv shape on the
// path that decides containment. While it returned nothing, EVERY generator in
// that file was unreachable from Run — osLevelRunArgs' empty-argv gate refused,
// Preflight inherited the refusal, and the feature would have shipped inert with
// every phase gate green (the generators are unit-tested directly). Filling it
// in is what flips Preflight from refusing to probing; there is no separate flag.
//
// An unsupported platform returns an error rather than an empty slice. Empty
// would still be refused by the gate, but as a generic "no containment"
// message that hides which platform was actually asked for.
//
// It is a variable, not a plain function, solely so tests can exercise Run and
// Preflight against a fake shim without either real binary installed;
// production code never reassigns it.
var osLevelContainmentArgs = func(goos string, cfg OSLevelConfig, spec RunSpec) ([]string, error) {
	switch goos {
	case "darwin":
		return sandboxExecArgs(cfg, spec)
	case "linux":
		return bwrapArgs(cfg, spec)
	default:
		return nil, fmt.Errorf("sandbox: os-level sandbox is not supported on %s", goos)
	}
}

// A run's scratch tree is PARTITIONED, not shared. Both subdirectories live
// under the per-run scratch root the containment profile/argv carves out, so
// the split costs nothing in the generators.
//
//	<scratch>/home  HOME, TMPDIR, GOTMPDIR and the XDG/Go caches
//	<scratch>/work  the ephemeral copy of the snapshot, for a Writable run
//
// Pointing both at one directory is not a tidiness question. It makes the tree
// under review the run's $HOME, so every dotfile in the repository becomes the
// toolchain's own configuration: a review measured `.gitconfig` in a snapshot
// taking effect as `--global` config, `Library/Application Support/go/env`
// setting GOFLAGS and GOPROXY, and `.profile` being EXECUTED by `sh -lc`. The
// argv is the operator's trusted validate_command, so that is repository
// content silently redirecting the operator's own validation — including the
// power to make the tree's validation pass. DockerBackend avoids it by having
// separate mount points (/scratch vs /work, docker.go:165-169); this backend
// has to create them.
const (
	osLevelScratchHomeSubdir = "home"
	osLevelScratchWorkSubdir = "work"
)

// osLevelMaxWritableCopyBytes and osLevelMaxWritableCopyEntries bound the
// ephemeral copy a Writable run seeds into its scratch directory.
//
// An unbounded copy is a host disk-exhaustion vector on the very machine atcr
// is running on: the snapshot is the tree under review, and a workload from a
// previous run (or a repository containing a large fixture) decides its size.
// The Docker backend gets this bound for free from the container's own storage
// limits; here it has to be stated.
//
// The numbers are sized against a real working tree rather than picked round:
// atcr's own checkout measured 486 MiB / 16.9k entries excluding .git, and a
// working tree carries build output, caches and vendored dependencies that a
// git clone does not. An earlier 512 MiB bound put this very repository at 91%
// of the limit, and crossing it is not a degraded run — it is a fault, so the
// caller reports "cannot even validate". Nothing is excluded from the count:
// .git is deliberately copied, because an operator's validate_command may
// legitimately shell out to git.
//
// They are variables rather than constants so the bound itself is testable
// without writing a multi-gigabyte fixture.
var (
	osLevelMaxWritableCopyBytes   int64 = 2 << 30 // 2 GiB
	osLevelMaxWritableCopyEntries       = 500_000
)

// seedWritableCopy copies the snapshot tree into a run's writable scratch
// directory, so a Writable run mutates a throwaway copy and never the host
// snapshot (sandbox.go:59-67).
//
// The copy is performed host-side, in Go, before the spawn — NOT with a `cp -R`
// injected into the workload the way DockerBackend does it
// (writableSetupExec/writableSetupPrefix, docker.go:124-130). Docker has no
// other way to reach inside a container; this backend spawns a child process on
// the same filesystem, so no shell needs to enter the picture and RunSpec's
// injection-safety guarantee (sandbox.go:51-56) is left intact rather than
// re-implemented on a new surface.
//
// Symlinks are never dereferenced. The snapshot is model-adjacent input, so a
// dereferencing copy would pull an arbitrary host file — ~/.ssh/id_ed25519 is
// the standing example in this package — INTO the writable, workload-readable
// scratch tree, re-opening the exact exfiltration path the containment profile
// closes. A symlink that stays inside the snapshot is recreated as a symlink;
// one that escapes (absolute, or climbing out via ..) is dropped. Dropping is
// safe in the direction that matters: the workload sees a missing path rather
// than a host secret. Irregular entries (devices, sockets, FIFOs) are dropped
// for the same reason — they are not source code and cannot be meaningfully
// copied.
// It returns the number of entries it could not read and therefore skipped. A
// permission error on an individual entry is NOT fatal: the copy is a throwaway
// working tree, and aborting the whole run because one file in a repository is
// unreadable would make --auto-fix unusable on ordinary checkouts. A failure to
// read the snapshot ROOT is still fatal, since that is a copy of nothing.
func seedWritableCopy(src, dst string) (skipped int, err error) {
	// WalkDir Lstats the walk root and does NOT dereference it, so a SnapshotDir
	// whose final component is a symlink to a directory would visit exactly one
	// entry and seed an EMPTY copy with skipped=0/err=nil — and the workload
	// would run against an empty tree. Resolve the root once up front.
	resolved, resolveErr := filepath.EvalSymlinks(src)
	if resolveErr != nil {
		return 0, fmt.Errorf("sandbox: cannot resolve snapshot dir %q: %w", src, resolveErr)
	}
	src = resolved
	var copiedBytes int64
	var entries int
	walkErr := filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == src {
				return walkErr
			}
			// Unreadable directory or entry: skip it and keep going.
			skipped++
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		entries++
		if entries > osLevelMaxWritableCopyEntries {
			return fmt.Errorf("sandbox: snapshot is too large to copy into the writable scratch tree "+
				"(more than %d entries)", osLevelMaxWritableCopyEntries)
		}
		target := filepath.Join(dst, rel)

		switch {
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				skipped++
				return filepath.SkipDir
			}
			// |0o700 rather than the source mode verbatim. A read-only (0555)
			// directory in the snapshot would otherwise be recreated read-only
			// and then reject its own children, faulting every Writable run on a
			// repository containing an extracted archive or a module cache. The
			// copy is throwaway and only this process writes into it, so the
			// owner bits are widened; group/other bits are left as the source
			// had them.
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case d.Type()&os.ModeSymlink != 0:
			if err := copySymlinkIfContained(src, path, dst, target); err != nil {
				// A link that vanished (or turned unreadable) between readdir
				// and readlink is the live-working-tree case below: skip it.
				if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
					skipped++
					return nil
				}
				return err
			}
			return nil
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				skipped++
				return nil
			}
			// The bound is checked BEFORE the bytes are written, so crossing it
			// costs one stat rather than one full file copy.
			copiedBytes += info.Size()
			if copiedBytes > osLevelMaxWritableCopyBytes {
				return fmt.Errorf("sandbox: snapshot is too large to copy into the writable scratch tree "+
					"(exceeds %d bytes)", osLevelMaxWritableCopyBytes)
			}
			if err := copyRegularFile(path, target, info.Mode().Perm()); err != nil {
				// os.ErrNotExist alongside os.ErrPermission: the snapshot is
				// the operator's LIVE working tree, and a file that vanished
				// between readdir and open (.git pack rewrite, build temp file,
				// editor swap) reports ENOENT — one vanishing file must not
				// abort the run any more than one unreadable file may.
				if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
					skipped++
					return nil
				}
				return err
			}
			return nil
		default:
			// Device, socket, FIFO: dropped deliberately. See the doc comment.
			return nil
		}
	})
	if walkErr == nil && entries == 0 {
		// Post-condition: a non-empty source must never seed an empty copy.
		// Unreachable through the walk alone once the root is resolved — it is
		// the guard against the next silent-empty-copy shape, and turns it into
		// the fault runWith's seed-error path already knows how to report.
		if dirEntries, readErr := os.ReadDir(src); readErr == nil && len(dirEntries) > 0 {
			return skipped, fmt.Errorf("sandbox: writable copy of %q seeded zero entries from a non-empty snapshot", src)
		}
	}
	return skipped, walkErr
}

// copySymlinkIfContained recreates a symlink only when it resolves inside the
// snapshot; otherwise it drops it. The link is recreated with its ORIGINAL
// (relative) target rather than a rewritten one, so the copy keeps the same
// internal shape the snapshot had.
//
// It is a variable, not a plain function, solely so tests can inject the
// vanished-between-readdir-and-readlink failure the live-working-tree skip
// logic exists for (mirroring the osLevelContainmentArgs seam); production
// code never reassigns it.
var copySymlinkIfContained = func(src, path, dst, target string) error {
	linkTarget, err := os.Readlink(path)
	if err != nil {
		return err
	}
	// Resolve against the link's own directory, exactly as the kernel would,
	// then check containment against the SNAPSHOT root. An absolute target is
	// resolved as-is and will fail this check unless it genuinely points back
	// inside the snapshot.
	resolved := linkTarget
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(path), resolved)
	}
	if !pathContainsFold(src, filepath.Clean(resolved), false) {
		return nil // escapes the snapshot: drop it
	}
	if filepath.IsAbs(linkTarget) {
		// An absolute in-snapshot target would still point at the ORIGINAL
		// read-only snapshot from inside the copy. Rewrite it to the equivalent
		// path inside the copy so the link stays self-consistent.
		rel, err := filepath.Rel(src, filepath.Clean(resolved))
		if err != nil {
			return nil
		}
		linkTarget = filepath.Join(dst, rel)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.Symlink(linkTarget, target)
}

// copyRegularFile copies one file's contents, preserving its permission bits.
// It is a variable for the same test-seam reason as copySymlinkIfContained.
var copyRegularFile = func(path, target string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// preflightRunTimeout bounds the trivial verification run, matching the 30s
// budget DockerBackend.Preflight gives its trivial container (docker.go:394).
const preflightRunTimeout = 30 * time.Second

// resolveToolPath returns the absolute path of the sandboxing binary, verifying
// it exists and is executable. Unlike toolPath it performs I/O, so it belongs to
// Preflight rather than the pure argv path.
func (b *osLevelBackend) resolveToolPath() (string, error) {
	tool, err := b.platformToolName()
	if err != nil {
		return "", err
	}
	if b.cfg.ToolPath != "" {
		if !filepath.IsAbs(b.cfg.ToolPath) {
			return "", fmt.Errorf("sandbox: os-level tool_path must be absolute, got %q", b.cfg.ToolPath)
		}
		if err := verifyExecutable(b.cfg.ToolPath); err != nil {
			return "", err
		}
		return b.cfg.ToolPath, nil
	}
	// Resolve the platform default from a fixed list of system directories
	// rather than the inherited $PATH. exec.LookPath honours $PATH verbatim —
	// including an empty element, which Go maps to "." — and during a review the
	// working directory is the repository under review, so an executable named
	// "bwrap" committed to that repo could otherwise become "the sandbox" and
	// contain nothing. An operator who needs a different location sets an
	// absolute tool_path, which is a deliberate act rather than an inherited
	// environment value.
	dirs := b.trustedDirs
	if dirs == nil {
		dirs = trustedToolDirs
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, tool)
		if err := verifyExecutable(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found in %v (is it installed? set an absolute tool_path to override)",
		tool, dirs)
}

// trustedToolDirs are the system directories the platform default binary may be
// resolved from. /usr/local/bin is deliberately absent: it is the conventional
// admin/user-managed prefix (user-owned under Intel-macOS Homebrew), so trusting
// it would reintroduce the substitution this list exists to prevent. An operator
// whose tool lives elsewhere sets an absolute tool_path — a deliberate act
// rather than an inherited environment value.
var trustedToolDirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}

// verifyExecutable reports whether path is a regular, executable, non-symlink
// file that is not group- or world-writable.
//
// Lstat rather than Stat: os.Stat follows symlinks, so a symlink in a trusted
// directory pointing at an attacker-controlled file elsewhere would pass. The
// writability check is what makes "trusted directory" mean something — a
// world-writable binary in /usr/bin is not more trustworthy than one in /tmp.
func verifyExecutable(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to resolve the sandbox binary through one", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is group- or world-writable", path)
	}
	return nil
}

// checkHostPrerequisite verifies the platform-specific precondition the
// sandboxing tool needs beyond simply being installed.
//
// On Linux that is unprivileged user namespaces: bwrap builds its mount,
// network, and PID isolation out of them, so without them it cannot contain
// anything — it fails with "setting up uid map: Permission denied" (measured on
// a host with kernel.apparmor_restrict_unprivileged_userns=1). Checking the
// sysctls here turns that into a specific, actionable message instead of an
// opaque trivial-run failure.
//
// macOS needs no equivalent: sandbox-exec is part of the base system and has no
// separately-disablable kernel feature behind it.
func checkHostPrerequisite(ctx context.Context, goos, procRoot string) error {
	if goos != "linux" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if procRoot == "" {
		procRoot = "/"
	}
	// Both knobs are optional — unprivileged_userns_clone is Debian/Ubuntu-
	// specific and absent on mainline kernels — so a MISSING file is not a
	// failure. A file that exists but cannot be read or parsed IS: an unreadable
	// gate is not evidence that the gate is open, and the hosts most likely to
	// restrict /proc are the same ones most likely to restrict namespaces.
	for _, knob := range []struct{ path, sysctl string }{
		{filepath.Join(procRoot, "proc/sys/user/max_user_namespaces"), "user.max_user_namespaces"},
		{filepath.Join(procRoot, "proc/sys/kernel/unprivileged_userns_clone"), "kernel.unprivileged_userns_clone"},
	} {
		raw, err := os.ReadFile(knob.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("cannot read %s to verify unprivileged user namespaces: %w", knob.sysctl, err)
		}
		text := strings.TrimSpace(string(raw))
		n, convErr := strconv.Atoi(text)
		if convErr != nil {
			return fmt.Errorf("cannot parse %s (%q) to verify unprivileged user namespaces: %w",
				knob.sysctl, text, convErr)
		}
		if n <= 0 {
			return fmt.Errorf("unprivileged user namespaces are disabled (%s=%d); "+
				"%s cannot isolate without them", knob.sysctl, n, linuxSandboxTool)
		}
	}
	// AppArmor's restriction (Ubuntu 24.04+) does not block namespace creation
	// outright — an AppArmor-profiled bwrap still works — so it cannot be
	// rejected from the sysctl alone. An unprofiled binary fails at uid-map
	// time, which the step-3 trivial run catches.
	return nil
}

// osLevelRunArgs builds the argv for the platform sandboxing tool: the tool's
// own containment arguments, then the workload.
//
// The containment arguments come from osLevelContainmentArgs, which dispatches
// to the generators in oslevel_profile.go — the deny-by-default sandbox-exec
// profile on macOS, the bind-mount/namespace argument list on Linux. The
// empty-argv gate below stays regardless: it is what stops a future generator
// regression from producing a spawn with no isolation arguments at all.
//
// Like dockerRunArgs, this is pure (no I/O) so the argv can be asserted in a
// unit test without either binary installed. The script body is NOT included
// here: it is streamed over stdin to `sh -s` by Run, never interpolated into
// argv, preserving RunSpec's injection-safety guarantee (sandbox.go:51-56).
func osLevelRunArgs(goos string, cfg OSLevelConfig, spec RunSpec) ([]string, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	// The containment arguments come first and the workload last. The generator
	// is wired (osLevelContainmentArgs dispatches on GOOS), so this is the real
	// containment argv, not a placeholder.
	contain, err := osLevelContainmentArgs(goos, cfg, spec)
	if err != nil {
		return nil, fmt.Errorf("sandbox: cannot build containment arguments: %w", err)
	}
	// Refuse to build an argv that contains nothing. This is the gate, and it
	// lives here rather than in Preflight so it covers EVERY spawn: a caller
	// that set tool_path and skipped Preflight would otherwise run
	// model-authored code with the sandboxing binary present but no isolation
	// arguments at all — the binary would simply exec the workload.
	if len(contain) == 0 {
		return nil, fmt.Errorf("%w (platform %s)", ErrOSLevelNoContainment, goos)
	}
	// Copy before appending. A Phase 2 generator that returns a slice with spare
	// capacity — a cached base profile, a make([]string, 0, N) builder — would
	// otherwise have two concurrent runs append into the same backing array,
	// and argv is what decides containment.
	args := make([]string, 0, len(contain)+len(spec.Command)+2)
	args = append(args, contain...)
	if spec.Script != "" {
		return append(args, "/bin/sh", "-s"), nil
	}
	return append(args, spec.Command...), nil
}

// Run implements Backend. It reproduces DockerBackend.Run's three-way outcome
// taxonomy: a normal exit (zero or not) is a RunResult with a nil error, a
// timeout or parent cancellation is TimedOut/124 with a nil error, and a
// genuine backend fault is a wrapped error.
func (b *osLevelBackend) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	tool, err := b.toolPath()
	if err != nil {
		return RunResult{}, err
	}
	return b.runWith(ctx, tool, spec, false)
}

// runWith is Run against an already-resolved binary. Preflight uses it so its
// verification run spawns the exact path it just validated — toolPath has not
// been pinned at that point, and must not be until the verification succeeds.
//
// probe marks Preflight's verification runs. A probe bypasses the MaxConcurrent
// semaphore: Preflight is the readiness gate a resolver blocks on, so queueing
// it behind in-flight workload runs would report a busy backend as an unhealthy
// one. Probes are rare (one backend setup, not per finding) and bounded by
// preflightRunTimeout, so exempting them does not open the resource hole the
// semaphore closes.
func (b *osLevelBackend) runWith(ctx context.Context, tool string, spec RunSpec, probe bool) (res RunResult, err error) {
	// Validate up front. The argv is no longer built here — it needs the scratch
	// directory, which is created below — so without this a malformed spec would
	// take a concurrency slot and create a temp tree before being refused.
	if err := spec.validate(); err != nil {
		return RunResult{}, err
	}

	// Bound concurrent sandbox spawns; block until a slot frees or ctx is done.
	// Lazily allocate the semaphore so a struct-literal backend (nil sem) still
	// enforces MaxConcurrent rather than silently spawning without limit,
	// mirroring docker.go:237-251. Probes skip the acquire entirely (see the
	// doc comment).
	if !probe {
		b.semOnce.Do(func() {
			if b.sem == nil {
				n := b.cfg.MaxConcurrent
				if n <= 0 {
					n = DefaultOSLevelConfig().MaxConcurrent
				}
				b.sem = make(chan struct{}, n)
			}
		})
		select {
		case b.sem <- struct{}{}:
			defer func() { <-b.sem }()
		case <-ctx.Done():
			// A cancellation while queued is still a cancellation, not a backend
			// fault — report it the same way a cancellation after spawn is reported,
			// so a caller keying on err != nil does not treat a user's ctrl-C on a
			// queued run as a sandbox failure.
			return RunResult{
				Command:  renderCommand(spec),
				ExitCode: timeoutExitCode,
				TimedOut: true,
			}, nil
		}
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = b.cfg.Timeout
	}
	if timeout <= 0 {
		timeout = DefaultOSLevelConfig().Timeout
	}
	logger := log.FromContext(ctx)
	cmdStr := renderCommand(spec)
	// A Preflight probe is labeled as one at every step, so the evidence trail
	// separates a readiness check from an executed workload without
	// command-string matching.
	startMsg, auditMsg := "sandbox exec start", "sandbox exec"
	if probe {
		startMsg, auditMsg = "sandbox preflight probe start", "sandbox preflight probe"
	}
	logger.Info(startMsg, "backend", osLevelBackendName, "tool", tool, "command", cmdStr)
	// Exactly one terminal record per start line, emitted from a defer so it
	// reports the FINAL outcome. Registered immediately after the start line, so
	// every path that announced a run also accounts for it — including the ones
	// that fail before the process is created. Only the refusals that genuinely
	// precede this point produce no record at all: spec validation and a
	// cancellation while queued for a concurrency slot. The argv build moved
	// BELOW this line (it needs the scratch dir), so a containment-generator
	// refusal and a failed writable copy now do announce a start and are
	// accounted for with fault=true — which is the right way round, since both
	// are failures worth seeing in the evidence trail.
	//
	// DockerBackend logs its equivalent line inline (docker.go:289-294) before the
	// timeout and exit-code branches run, so its trail records
	// exit_code=0/timed_out=false for every run including the killed and faulted
	// ones. The evidence trail for model-authored code should not carry that.
	defer func() { auditRun(logger, tool, res, err, auditMsg) }()

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The scratch directory is created BEFORE the argv is built, and travels to
	// the generators on a per-run COPY of the config. Both halves matter:
	//
	//   - Before: the generators carve out cfg.ScratchDir, and sandboxEnv points
	//     HOME/TMPDIR/GOCACHE at it. Creating it afterwards (as this function
	//     originally did) left cfg.ScratchDir empty on every real run, so the
	//     profile permitted nothing for the toolchain to write to while the
	//     environment still pointed there — a Go build cannot run with an
	//     unwritable HOME/GOCACHE. That was TD-014.
	//   - A copy: the directory is per-RUN, and this backend is explicitly built
	//     for concurrent use. Writing it into b.cfg would have two concurrent
	//     runs each permitting the other's scratch tree.
	scratchDir, err := os.MkdirTemp("", "atcr-oslevel-scratch-*")
	if err != nil {
		return RunResult{Command: cmdStr}, fmt.Errorf("os-level sandbox run: cannot create scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratchDir) }()
	homeDir := filepath.Join(scratchDir, osLevelScratchHomeSubdir)
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return RunResult{Command: cmdStr}, fmt.Errorf("os-level sandbox run: cannot create scratch home: %w", err)
	}

	// Build the argv BEFORE seeding anything. osLevelRunArgs is where the
	// generators' guards live — assertUsableSource, assertNotAHomeTree,
	// assertDisjointPaths — and they are what refuse a SnapshotDir of "/", of a
	// home tree, or of a protected mount. Seeding first would copy up to the
	// full bound out of exactly those trees before the guard that exists to
	// refuse them ever ran, and a SIGKILL inside that window would leave the
	// copy on disk.
	runCfg := b.cfg
	runCfg.ScratchDir = scratchDir
	args, err := osLevelRunArgs(b.platform(), runCfg, spec)
	if err != nil {
		return RunResult{Command: cmdStr}, err
	}

	// Run inside the snapshot. RunSpec.validate already requires SnapshotDir to
	// be absolute, and the Docker backend mounts it as the working directory —
	// leaving cmd.Dir unset here would instead run model-authored code in atcr's
	// own working directory, i.e. the operator's live repo.
	workDir := spec.SnapshotDir
	if spec.Writable {
		// Seed the throwaway copy the run may mutate, so the host snapshot stays
		// read-only (sandbox.go:59-67). It goes in its OWN subdirectory, never
		// the scratch root, so the tree under review does not become $HOME.
		// A failed seed is a FAULT: falling through would run the workload
		// against a partial or empty tree and report its exit code as a
		// validation verdict.
		copyDir := filepath.Join(scratchDir, osLevelScratchWorkSubdir)
		if err := os.MkdirAll(copyDir, 0o700); err != nil {
			return RunResult{Command: cmdStr},
				fmt.Errorf("os-level sandbox run: cannot create writable copy dir: %w", err)
		}
		skipped, seedErr := seedWritableCopy(spec.SnapshotDir, copyDir)
		if seedErr != nil {
			return RunResult{Command: cmdStr},
				fmt.Errorf("os-level sandbox run: writable copy failed: %w", seedErr)
		}
		if skipped > 0 {
			// Surfaced rather than swallowed: a validation verdict produced over
			// an incomplete tree is worth knowing about, even though refusing the
			// run over one unreadable file would be worse.
			logger.Warn("sandbox exec writable copy skipped unreadable entries",
				"backend", osLevelBackendName, "command", cmdStr, "skipped", skipped)
		}
		if b.platform() != "linux" {
			// On darwin the run must execute IN the copy: sandbox-exec applies a
			// policy to paths and cannot remap them, so there is no way to make
			// the copy appear at the snapshot's path. On Linux bwrapArgs already
			// emits --ro-bind snap /src + --bind <scratch>/work /work + --chdir
			// /work, so cmd.Dir stays the host snapshot for the bwrap process
			// itself and the remap happens inside the mount namespace.
			workDir = copyDir
		}
	}

	cmd := exec.CommandContext(runCtx, tool, args...)
	cmd.Dir = workDir
	// Hand the workload an explicit, minimal environment. Inheriting os.Environ()
	// would pass every credential in the operator's shell (LITELLM_API_KEY,
	// GITHUB_TOKEN, cloud tokens) straight to LLM-generated code. Docker gets
	// this for free from a fresh container plus an -e allowlist (docker.go:165-169);
	// neither sandbox-exec nor bwrap scrubs the environment on its own, so the
	// allowlist has to be built here — and here is the ONLY place it is built.
	// bwrap's --clearenv is deliberately not used: bwrap inherits exactly this
	// cmd.Env and passes it through, so --clearenv would wipe the allowlist
	// rather than reinforce it, and every var would have to be re-stated as a
	// --setenv pair. The scrub is cmd.Env's alone; a future caller that leaves
	// cmd.Env nil would leak the whole parent environment.
	//
	// HOME is the scratch tree's home SUBDIRECTORY, never its root and never the
	// snapshot copy — see osLevelScratchHomeSubdir. It sits inside the carve-out
	// created above, so the directory the containment profile/argv permits and
	// the directory HOME/TMPDIR/GOCACHE point at agree by construction rather
	// than by coincidence.
	cmd.Env = sandboxEnv(homeDir)
	// Put the sandbox in its own process group and make cancellation kill the
	// whole group, so a workload that forked is reaped rather than left running
	// past the deadline. WaitDelay is the backstop for a grandchild that escaped
	// the group and still holds the output pipes: without it cmd.Wait would block
	// forever on an already-expired deadline. Both mirror the same pattern in
	// internal/verify/localvalidate.go:100-105.
	configureSandboxProcessGroup(cmd)
	cmd.WaitDelay = osLevelWaitGrace
	if spec.Script != "" {
		// The script is stdin DATA to `sh -s`, never argv and never inside a
		// `sh -c "..."` string, so the script body IS the program source rather
		// than an argument — there is no shell-injection vector.
		cmd.Stdin = strings.NewReader(spec.Script)
	}
	var buf bytes.Buffer
	// Cap the captured buffer so a chatty workload cannot exhaust host memory
	// before truncation, with headroom for rune-boundary backup.
	lw := &limitedWriter{w: &buf, n: int64(b.cfg.MaxOutputBytes) + 4096}
	// Both streams ALSO feed a dedicated tail buffer for fault classification.
	// The display buffer keeps the HEAD of the combined capture, so a workload
	// flooding output before the tool reports its own failure would push the
	// diagnostic past the capture cap — and with it the one signal that
	// separates a tool fault from a workload exit. The tail keeps the capture's
	// final bytes, which is where a failing tool's diagnostic lives. One shared
	// writer value for both streams keeps os/exec's single-goroutine drain
	// (same-writer fast path), so interleave order is unchanged and the buffer
	// needs no locking.
	capture := &combinedCapture{lw: lw}
	cmd.Stdout = capture
	cmd.Stderr = capture

	runErr := cmd.Run()
	res = RunResult{
		Command: cmdStr,
		Output:  truncate(buf.String(), b.cfg.MaxOutputBytes),
	}

	// A cancellation-class end (deadline exceeded OR parent cancellation) is
	// folded into TimedOut so it is never misreported as a spurious non-zero
	// program exit or a backend fault, mirroring docker.go:301. runErr is
	// consulted first so a run that genuinely finished microseconds before the
	// deadline keeps its real exit code instead of being relabelled a timeout.
	if runCtxDone(runCtx) && !ranToCompletion(runErr) {
		res.TimedOut = true
		res.ExitCode = timeoutExitCode
		// A non-nil, non-ESRCH error on this path means the group kill itself
		// failed (e.g. EPERM because a group member dropped privileges). Log it
		// so a failed reap stays observable instead of being masked by TimedOut.
		if runErr != nil && !errors.Is(runErr, syscall.ESRCH) && !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
			logger.Warn("sandbox exec kill-on-timeout returned unexpected error",
				"backend", osLevelBackendName, "command", cmdStr, "error", runErr)
		}
		logger.Warn("sandbox exec timed out", "backend", osLevelBackendName, "command", cmdStr, "timeout", timeout)
		return res, nil
	}
	if errors.Is(runErr, exec.ErrWaitDelay) {
		// The workload exited but a lingering child held its output pipes open
		// past the wait grace: completed uncleanly. Fail closed as a timeout
		// rather than reporting a success we cannot vouch for
		// (localvalidate.go:147-153).
		res.TimedOut = true
		res.ExitCode = timeoutExitCode
		logger.Warn("sandbox exec exceeded wait grace", "backend", osLevelBackendName, "command", cmdStr)
		return res, nil
	}
	if runErr != nil {
		// The capture TAIL is handed to the classifier, not res.Output and not
		// the head-capped display buffer: a small MaxOutputBytes or an output
		// flood would push the tool's diagnostic out of the retained head and
		// silently turn a containment failure into a reported workload exit.
		// Truncation is a display budget and must not decide a security question.
		return b.classifyRunError(ctx, res, tool, cmdStr, capture.Tail(), runErr)
	}
	return res, nil
}

// auditRun emits the single structured record of a sandbox run: which backend
// and which binary contained it, what was executed, and how it ended.
//
// It is the evidence trail for code an LLM wrote, so every field is always
// present. Two of them are load-bearing rather than decorative:
//
//   - fault distinguishes "the workload ran under containment and exited 0" from
//     "containment was never established, so nothing about this run is vouched
//     for". Without it the two render identically: RunResult.ExitCode stays 0 on
//     every fault path (a fault has no program exit status to report, and the
//     non-nil error is the caller's signal — see TD-005), so exit_code alone
//     reads a containment failure as a clean success. A fault is also logged at
//     ERROR, so the distinction survives a reader who scans levels rather than
//     attributes.
//   - tool names the binary that actually did the containing. Which binary runs
//     is the security decision in this file (see toolPath), and an operator's
//     tool_path override and the Preflight-pinned default are indistinguishable
//     from the workload command alone.
//
// msg is "sandbox exec" for a workload run and "sandbox preflight probe" for a
// Preflight verification run, so the two are separable without command-string
// matching.
func auditRun(logger *slog.Logger, tool string, res RunResult, runErr error, msg string) {
	attrs := []any{
		"backend", osLevelBackendName,
		"tool", tool,
		"command", res.Command,
		"exit_code", res.ExitCode,
		"timed_out", res.TimedOut,
		"fault", runErr != nil,
	}
	if runErr != nil {
		logger.Error(msg, append(attrs, "error", runErr)...)
		return
	}
	logger.Info(msg, attrs...)
}

// osLevelWaitGrace bounds how long cmd.Wait blocks after the sandboxed process
// exits while a lingering grandchild still holds its output pipes open.
const osLevelWaitGrace = 5 * time.Second

// osLevelCaptureTailBytes caps the tail of the combined output capture that
// fault classification reads. Every measured tool diagnostic (bwrap's die()
// lines, sandbox-exec's usage/syntax errors) is well under 1 KiB; 8 KiB leaves
// generous headroom without giving a chatty workload an unbounded buffer.
const osLevelCaptureTailBytes = 8 << 10

// tailBuffer keeps only the LAST osLevelCaptureTailBytes written to it. Fault
// classification needs the end of the capture — that is where a failing
// sandbox tool prints its diagnostic — not the head the display buffer
// preserves.
type tailBuffer struct {
	data []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.data = append(t.data, p...)
	if len(t.data) > osLevelCaptureTailBytes {
		t.data = t.data[len(t.data)-osLevelCaptureTailBytes:]
	}
	return len(p), nil
}

// combinedCapture fans the workload's combined stdout+stderr into the bounded
// display buffer and the classification tail. A run assigns the SAME value to
// cmd.Stdout and cmd.Stderr, so os/exec drains both pipes from one goroutine
// (its same-writer fast path) and the writes below never race.
type combinedCapture struct {
	lw   *limitedWriter
	tail tailBuffer
}

func (c *combinedCapture) Write(p []byte) (int, error) {
	n, err := c.lw.Write(p)
	_, _ = c.tail.Write(p)
	return n, err
}

// Tail returns the retained trailing bytes of the capture, for fault
// classification.
func (c *combinedCapture) Tail() string { return string(c.tail.data) }

// runCtxDone reports whether the run's context ended in a cancellation class
// (deadline or explicit cancel).
func runCtxDone(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled)
}

// ranToCompletion reports whether the process delivered a real exit status —
// the case where a run finished just before its deadline elapsed and must keep
// its exit code rather than being relabelled a timeout.
func ranToCompletion(runErr error) bool {
	if runErr == nil {
		return true
	}
	var ee *exec.ExitError
	return errors.As(runErr, &ee) && ee.ProcessState != nil && ee.Exited()
}

// sandboxEnv builds the minimal environment handed to a sandboxed workload. It
// is an allowlist, not a filter: anything not named here never reaches
// model-authored code.
//
// HOME/TMPDIR and the Go/XDG cache vars point at an ephemeral scratch directory
// OUTSIDE the snapshot, mirroring docker.go:165-169's /scratch tmpfs. Pointing
// them at the snapshot would aim every toolchain write at the very tree the
// package contract says a run must not mutate (sandbox.go:9) — and would break
// outright once Phase 2 makes the snapshot read-only, since a Go build cannot
// run with an unwritable HOME/GOCACHE.
func sandboxEnv(scratchDir string) []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	return []string{
		"PATH=" + path,
		"HOME=" + scratchDir,
		"TMPDIR=" + scratchDir,
		"XDG_CACHE_HOME=" + filepath.Join(scratchDir, ".cache"),
		"GOCACHE=" + filepath.Join(scratchDir, ".gocache"),
		"GOTMPDIR=" + scratchDir,
		"LANG=C",
	}
}

// Sandbox-tool fault markers. These are MEASURED against the real binaries, not
// inferred from convention — an earlier revision of this file assumed bwrap used
// Docker's 125/126/127 reserved-code scheme and was wrong in the fail-open
// direction, reporting failed containment as a merely-failing workload.
//
// Measured 2026-08-08:
//
//	bubblewrap 0.9.0 (orchestrator.lan)
//	  bwrap --bind / / /bin/sh -c 'exit 7'        -> 7   (workload, passed through)
//	  bwrap ... /bin/sh -c 'nosuchcmd'            -> 127 (workload's sh, passed through)
//	  bwrap --ro-bind /definitely/not/here ...    -> 1   "bwrap: setting up uid map: ..."
//	  bwrap --not-a-real-flag /bin/true           -> 1   "bwrap: Unknown option ..."
//	  bwrap ... /nonexistent-binary-xyz           -> 1   "bwrap: execvp ...: No such file..."
//	  bwrap --bind / /            (no command)    -> 1   "usage: bwrap [OPTIONS...] ..."
//
//	sandbox-exec, macOS 25.2 (this host)
//	  sandbox-exec -p '(version 1)(allow default)' sh -c 'exit 7' -> 7  (passed through)
//	  sandbox-exec -p '<valid>'    (no command)   -> 64  "Usage: sandbox-exec ..."
//	  sandbox-exec -p '<malformed>' /bin/echo hi  -> 65  "sandbox-exec: syntax error: ..."
//	  sandbox-exec -p '(deny default)' /bin/echo  -> 71  "sandbox-exec: execvp() ... failed"
//
// The consequence is that on BOTH platforms the exit code alone cannot separate a
// tool fault from a workload result — bwrap's fault code (1) is the most common
// workload failure code there is. The tool's own stderr diagnostic is the reliable
// signal, so that is what these match.
const (
	// bwrapDiagnosticPrefix prefixes every bubblewrap error (utils.c's die()).
	bwrapDiagnosticPrefix = "bwrap: "
	// bwrapUsagePrefix is what bwrap prints when given no command at all.
	bwrapUsagePrefix = "usage: bwrap"
	// sandboxExecDiagnosticPrefix is how sandbox-exec labels its own failures.
	sandboxExecDiagnosticPrefix = "sandbox-exec:"
	// sandboxExecUsagePrefix is sandbox-exec's marker-free usage error (exit 64),
	// which carries no "sandbox-exec:" token at all.
	sandboxExecUsagePrefix = "Usage: sandbox-exec"
)

// sandboxExecFaultCodes are the sysexits values sandbox-exec returns for its own
// failures (64 EX_USAGE, 65 EX_DATAERR, 71 EX_OSERR). A workload could in
// principle exit with one of these itself and be misclassified as a fault — that
// is the safe direction: a fault is refused, whereas a MISSED fault would report
// an uncontained run as a contained one.
var sandboxExecFaultCodes = map[int]string{
	64: "usage error (profile or command argument rejected)",
	65: "profile could not be read or parsed",
	71: "could not execute the command under the profile",
}

// signalExitBase is the shell/waitpid convention for a signal-killed child:
// 128 + signal number. bwrap propagates it (propagate_exit_status), and Docker
// treats the same range as a fault (docker.go:323-326).
const signalExitBase = 128

// classifyToolExit reports whether a non-zero exit came from the sandboxing tool
// itself rather than from the workload, and why.
//
// Classification keys on the tool's stderr diagnostic rather than its exit code,
// because neither platform reserves exit codes for its own failures (see the
// measurements above). A signal-range exit (>= 128) is also a fault on both
// platforms: it means the kernel killed the process — an OOM kill, an external
// SIGKILL, or once Phase 2 lands, the sandbox policy itself enforcing — none of
// which is a program result the caller can act on.
//
// Any platform without a rule fails closed: an unclassifiable non-zero exit is a
// fault, never a successful run, per the package's containment-first contract
// (sandbox.go:5-14). Ambiguity always resolves toward refusing the run.
//
// output is the tail of the run's captured output (see combinedCapture), never
// the display-truncated string: a small MaxOutputBytes would cut the
// diagnostic off and silently turn a containment failure into a reported
// workload exit.
func classifyToolExit(goos string, exitCode int, output string) (bool, string) {
	if exitCode == 0 {
		return false, ""
	}
	if exitCode >= signalExitBase {
		return true, fmt.Sprintf("process killed by signal %d (exit %d)", exitCode-signalExitBase, exitCode)
	}
	switch goos {
	case "linux":
		if strings.Contains(output, bwrapDiagnosticPrefix) || strings.Contains(output, bwrapUsagePrefix) {
			return true, fmt.Sprintf("%s failed to set up the sandbox (exit %d)", linuxSandboxTool, exitCode)
		}
		// Everything else is bwrap passing the workload's own status through
		// verbatim — including 126/127 from `sh`, the most common outcome for a
		// command referencing an uninstalled tool.
		return false, ""
	case "darwin":
		if reason, ok := sandboxExecFaultCodes[exitCode]; ok {
			return true, fmt.Sprintf("%s %s (exit %d)", darwinSandboxTool, reason, exitCode)
		}
		if strings.Contains(output, sandboxExecDiagnosticPrefix) || strings.Contains(output, sandboxExecUsagePrefix) {
			return true, fmt.Sprintf("%s reported a sandbox error (exit %d)", darwinSandboxTool, exitCode)
		}
		return false, ""
	default:
		return true, fmt.Sprintf("no exit-code classification rule for %s; treating exit %d as a sandbox fault", goos, exitCode)
	}
}

// classifyRunError maps a failed *exec.Cmd run onto the Backend.Run contract: a
// program exit becomes RunResult.ExitCode with a nil error, anything else is a
// wrapped backend fault. Platform-specific reserved exit codes are classified in
// task 1.8; this is the exit-vs-fault split only.
func (b *osLevelBackend) classifyRunError(ctx context.Context, res RunResult, tool, cmdStr, rawOutput string, runErr error) (RunResult, error) {
	logger := log.FromContext(ctx)
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		// A signal death (OOM killer, an external SIGKILL, and once Phase 2 lands
		// a sandbox-profile kill) originates from the kernel, not the workload,
		// and ExitCode() reports -1 for it. Reporting -1 as a program result
		// would present an impossible exit status as a real one, so route it to
		// a fault exactly as the Docker backend routes 128+N (docker.go:323-326).
		code := ee.ExitCode()
		if code < 0 {
			logger.Error("sandbox exec killed by signal", "backend", osLevelBackendName, "command", cmdStr, "error", runErr)
			return res, fmt.Errorf("os-level sandbox run: %s: process killed by signal (%s): %w", tool, ee.String(), runErr)
		}
		// The sandboxing tool's own failures must not be folded into ExitCode,
		// where a caller would read them as the workload's result.
		if isFault, reason := classifyToolExit(b.platform(), code, rawOutput); isFault {
			logger.Error("sandbox exec runtime error", "backend", osLevelBackendName, "command", cmdStr, "exit_code", code, "error", runErr)
			return res, fmt.Errorf("os-level sandbox run: %s: runtime error: %s: %w", tool, reason, runErr)
		}
		res.ExitCode = code
		return res, nil
	}
	// Not an exit status: spawn failure, binary vanished, I/O error — a fault.
	logger.Error("sandbox exec backend fault", "backend", osLevelBackendName, "command", cmdStr, "error", runErr)
	return res, fmt.Errorf("os-level sandbox run: %s: %w", tool, runErr)
}

// toolPath resolves the binary this backend invokes.
//
// The platform gate runs FIRST and unconditionally: an unsupported GOOS is
// refused even when cfg.ToolPath is set. Applying the override before the gate
// would let an operator's tool_path re-enable the backend on, say, Windows,
// where setProcessGroup is a no-op and killProcessGroup always fails — a
// spawned workload with neither containment nor a reliable reap. The override
// selects WHICH binary runs on a supported platform; it is not a way to declare
// a platform supported.
//
// The override is both the test-shim seam and the field an operator config
// would populate, so it is the seam through which containment could be replaced
// by a no-op. It must therefore be an absolute path: a relative or bare name
// would be resolved through $PATH at spawn time, letting any user-writable
// $PATH entry shadow the real sandbox binary. Existence and executability are
// Preflight's checks (task 1.11) — this function stays pure so it can be
// asserted without touching the filesystem.
//
// With neither a pin nor an absolute override, this returns
// errOSLevelNotPreflighted rather than a bare name. Returning a name would have
// exec resolve it through the inherited $PATH at spawn time, bypassing
// resolveToolPath's trusted-directory control — so the one decision that picks
// the binary containing untrusted code would be the one with no check on it.
func (b *osLevelBackend) toolPath() (string, error) {
	// The platform gate runs first and unconditionally, so an unsupported GOOS
	// is refused even when tool_path is set.
	if _, err := osLevelToolFor(b.platform()); err != nil {
		return "", err
	}
	// A successful Preflight pinned an absolute, verified path; reuse it so no
	// later Run re-resolves a name.
	b.mu.Lock()
	pinned := b.pinnedTool
	b.mu.Unlock()
	if pinned != "" {
		return pinned, nil
	}
	if b.cfg.ToolPath != "" {
		if !filepath.IsAbs(b.cfg.ToolPath) {
			return "", fmt.Errorf("sandbox: os-level tool_path must be absolute, got %q", b.cfg.ToolPath)
		}
		return b.cfg.ToolPath, nil
	}
	// Neither pinned nor explicitly configured. Returning the bare platform name
	// here would have exec resolve it through the inherited $PATH at spawn time,
	// bypassing resolveToolPath's trusted-directory control entirely — so the
	// one code path that decides which binary contains untrusted code would be
	// the one path with no check on it. Refuse instead: Preflight is where the
	// default is resolved, and Backend.Preflight is documented as mandatory
	// before Run (sandbox.go:108-111). This makes that contract enforced by the
	// type rather than merely stated.
	return "", errOSLevelNotPreflighted
}

// errOSLevelNotPreflighted is returned when Run is reached without a resolved
// binary — either Preflight was never called, or it failed.
var errOSLevelNotPreflighted = errors.New(
	"sandbox: os-level backend has no resolved sandbox binary; Preflight must pass before Run")

// platformToolName returns the bare name of the platform's sandboxing binary,
// for Preflight's trusted-directory resolution. Unlike toolPath it does not
// require a pin, because resolving that name is exactly what it is used for.
func (b *osLevelBackend) platformToolName() (string, error) {
	return osLevelToolFor(b.platform())
}

// CheckSnapshotUsable reports whether snapshotDir would survive the containment
// generators' path guards, WITHOUT spawning anything.
//
// It exists because Preflight cannot answer this question. Preflight probes
// against its own os.MkdirTemp snapshot, while a real run is handed the caller's
// directory — the repository root for --auto-fix. The generators reject a
// snapshot that is a user's home tree, or whose path carries a sandbox-exec
// profile metacharacter, and without this they reject it only at Run: after a
// resolver already selected the backend and, for --auto-fix, after a patch was
// already applied (TD-019).
//
// It is exported solely for that pre-check. The dry-build mirrors a real run's
// shape — including a scratch dir, since the Writable argv is the one --auto-fix
// always takes — and cleans up after itself, so a caller can treat a nil return
// as "the generators will accept this path".
func CheckSnapshotUsable(cfg OSLevelConfig, snapshotDir string, writable bool) error {
	if cfg.ScratchDir == "" {
		// Run creates the scratch dir before building the argv and sets it on the
		// per-run config for BOTH Writable values, so this mirrors it
		// unconditionally. Creating it only for the writable shape would skip the
		// generators' ScratchDir guards on the read-only one, making this helper
		// systematically more permissive than the run it claims to predict.
		scratch, err := os.MkdirTemp("", "atcr-oslevel-precheck-*")
		if err != nil {
			return fmt.Errorf("sandbox: cannot stage an os-level containment check: %w", err)
		}
		defer func() { _ = os.RemoveAll(scratch) }()
		cfg.ScratchDir = scratch
	}
	spec := RunSpec{
		Command:     []string{"true"},
		SnapshotDir: snapshotDir,
		Writable:    writable,
	}
	if _, err := osLevelContainmentArgs(runtime.GOOS, cfg, spec); err != nil {
		return err
	}
	return nil
}
