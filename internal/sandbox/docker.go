package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/log"
)

// DockerConfig parameterizes the Docker backend. Zero values are not safe; use
// DefaultDockerConfig and override fields as needed.
type DockerConfig struct {
	// DockerPath is the docker binary to invoke. "docker" is resolved on PATH;
	// tests inject a fake shim here.
	DockerPath string
	// Image is the base image runs execute in. It must already be present
	// locally (Preflight verifies this) and must carry whatever toolchain
	// run_tests needs.
	Image string
	// Memory, CPUs, PidsLimit are the resource caps (docker --memory/--cpus/
	// --pids-limit).
	Memory    string
	CPUs      string
	PidsLimit int
	// User is the non-root uid:gid the container runs as (docker --user).
	User string
	// ScratchSize bounds the writable tmpfs scratch overlay (docker --tmpfs size).
	ScratchSize string
	// Timeout is the default per-run wall-clock budget when RunSpec.Timeout is 0.
	Timeout time.Duration
	// MaxOutputBytes truncates captured combined stdout+stderr.
	MaxOutputBytes int
	// MaxConcurrent bounds the number of containers running at once across this
	// backend. A review verifies findings concurrently and each skeptic may run
	// many tools, so without this cap a large finding set could fork enough
	// containers to exhaust the host (the Critical-rated resource-abuse risk).
	MaxConcurrent int
}

// DefaultDockerConfig returns a hardened, conservative default configuration.
func DefaultDockerConfig() DockerConfig {
	return DockerConfig{
		DockerPath:     "docker",
		Image:          "alpine:3.20",
		Memory:         "512m",
		CPUs:           "1.0",
		PidsLimit:      256,
		User:           "65534:65534", // nobody:nogroup
		ScratchSize:    "64m",
		Timeout:        60 * time.Second,
		MaxOutputBytes: 64 * 1024,
		MaxConcurrent:  4,
	}
}

// DockerBackend runs commands and scripts inside an ephemeral, network-isolated,
// resource-capped, non-root container with the snapshot mounted read-only.
type DockerBackend struct {
	cfg DockerConfig
	// sem bounds concurrent container spawns to cfg.MaxConcurrent (buffered slots).
	sem chan struct{}
	// semOnce lazily allocates sem on first Run so a backend built as a struct
	// literal (bypassing NewDockerBackend) still enforces the cap instead of
	// failing open. Safe under concurrent Run.
	semOnce sync.Once
}

// NewDockerBackend constructs a Docker backend from cfg.
func NewDockerBackend(cfg DockerConfig) *DockerBackend {
	if cfg.DockerPath == "" {
		cfg.DockerPath = "docker"
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 64 * 1024
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 4
	}
	return &DockerBackend{cfg: cfg, sem: make(chan struct{}, cfg.MaxConcurrent)}
}

// Name implements Backend.
func (b *DockerBackend) Name() string { return "docker" }

// dockerRunArgs builds the `docker run` argv for spec. It is pure (no I/O) so the
// hardening flags can be asserted in a unit test without a daemon. The script
// body is NOT included here: it is streamed over stdin to `sh -s` by Run.
func dockerRunArgs(cfg DockerConfig, spec RunSpec) ([]string, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	args := []string{
		"run", "--rm",
		"--network", "none",
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--user", cfg.User,
		"--memory", cfg.Memory,
		"--cpus", cfg.CPUs,
		"--pids-limit", strconv.Itoa(cfg.PidsLimit),
		// /scratch is mounted exec (not noexec) on purpose: go test compiles and
		// executes test binaries from GOCACHE/GOTMPDIR, which point here. noexec
		// would be defense theater anyway — run_script already pipes arbitrary sh
		// into the container, so the real containment is --network none, --cap-drop
		// ALL, --security-opt no-new-privileges, and the read-only rootfs.
		"--tmpfs", "/scratch:rw,exec,size=" + cfg.ScratchSize,
		// Point HOME, temp, and common build caches at the writable scratch tmpfs so
		// runners that need to write (go test's build cache, mktemp, pip, etc.) work
		// under the read-only rootfs + read-only /work. Harmless for runners that
		// ignore them (e.g. pytest).
		"-e", "HOME=/scratch",
		"-e", "TMPDIR=/scratch",
		"-e", "XDG_CACHE_HOME=/scratch/.cache",
		"-e", "GOCACHE=/scratch/.gocache",
		"-e", "GOTMPDIR=/scratch",
		"--workdir", "/work",
		"-v", spec.SnapshotDir + ":/work:ro",
	}
	if spec.Script != "" {
		// Feed the script over stdin: `docker run -i <image> /bin/sh -s`.
		args = append(args, "-i", cfg.Image, "/bin/sh", "-s")
	} else {
		args = append(args, cfg.Image)
		args = append(args, spec.Command...)
	}
	return args, nil
}

// renderCommand produces the human-readable command string stored as evidence.
func renderCommand(spec RunSpec) string {
	if spec.Script != "" {
		return "/bin/sh -s <<'EOF'\n" + spec.Script + "\nEOF"
	}
	return strings.Join(spec.Command, " ")
}

// Run implements Backend.
func (b *DockerBackend) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	args, err := dockerRunArgs(b.cfg, spec)
	if err != nil {
		return RunResult{}, err
	}
	// Name the container so a timeout can target it directly. exec.CommandContext
	// only SIGKILLs the `docker run` CLI client on timeout, not the container the
	// daemon runs; without an explicit kill the workload keeps consuming CPU/mem/
	// PIDs until it self-exits. The name is generated before the spawn so it is
	// known regardless of daemon timing (a --cidfile would race a slow start).
	containerName := "atcr-sbx-" + randHex(8)
	named := make([]string, 0, len(args)+2)
	named = append(named, args[0], "--name", containerName) // args[0] == "run"
	args = append(named, args[1:]...)
	// Bound concurrent container spawns; block until a slot frees or ctx is done.
	// Lazily allocate the semaphore so a struct-literal backend (nil sem) still
	// enforces MaxConcurrent — the mitigation for the Critical-rated resource-abuse
	// risk — rather than silently spawning unlimited containers.
	b.semOnce.Do(func() {
		if b.sem == nil {
			n := b.cfg.MaxConcurrent
			if n <= 0 {
				n = 4
			}
			b.sem = make(chan struct{}, n)
		}
	})
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
	case <-ctx.Done():
		return RunResult{}, ctx.Err()
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = b.cfg.Timeout
	}
	logger := log.FromContext(ctx)
	cmdStr := renderCommand(spec)
	logger.Info("sandbox exec start", "backend", "docker", "command", cmdStr)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, b.cfg.DockerPath, args...)
	if spec.Script != "" {
		// The script is delivered as stdin DATA to `sh -s` (see dockerRunArgs), never
		// interpolated into argv or a `sh -c "..."` string, so there is no shell-
		// injection vector: the script body IS the program source, not an argument.
		cmd.Stdin = strings.NewReader(spec.Script)
	}
	var buf bytes.Buffer
	// Cap the captured buffer so a chatty workload cannot exhaust host memory
	// before truncation. Allow a small headroom for rune-boundary backup.
	maxBuf := int64(b.cfg.MaxOutputBytes) + 4096
	lw := &limitedWriter{w: &buf, n: maxBuf}
	cmd.Stdout = lw
	cmd.Stderr = lw

	runErr := cmd.Run()
	res := RunResult{
		Command: cmdStr,
		Output:  truncate(buf.String(), b.cfg.MaxOutputBytes),
	}
	logger.Info("sandbox exec",
		"backend", b.Name(),
		"command", res.Command,
		"exit_code", res.ExitCode,
		"timed_out", res.TimedOut,
	)

	// Distinguish a timeout (deadline exceeded) from a real program exit.
	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = timeoutExitCode
		// The killed CLI does not stop the container; kill it explicitly so its
		// caps are reclaimed. --rm (in args) removes it once it stops. Use a fresh
		// background context so the kill is not itself cut short by the dead runCtx.
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if killErr := exec.CommandContext(killCtx, b.cfg.DockerPath, "kill", containerName).Run(); killErr != nil {
			logger.Warn("sandbox exec kill-on-timeout failed", "backend", "docker", "container", containerName, "error", killErr)
		}
		killCancel()
		logger.Warn("sandbox exec timed out", "backend", "docker", "command", cmdStr, "timeout", timeout)
		return res, nil
	}
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			ec := ee.ExitCode()
			// Docker reserved exit codes (125-127) and signal deaths (128+N, including
			// 137 SIGKILL/OOM-kill) originate from the runtime or kernel, not the
			// workload. Treat them as backend faults so they are not misreported as a
			// program result.
			if ec >= 128 {
				logger.Error("sandbox exec container killed", "backend", "docker", "command", cmdStr, "exit_code", ec, "signal", ec-128, "error", runErr)
				return res, fmt.Errorf("docker run: container killed by signal %d (OOM or daemon kill, exit %d): %w", ec-128, ec, runErr)
			}
			if ec >= 125 {
				logger.Error("sandbox exec runtime error", "backend", "docker", "command", cmdStr, "exit_code", ec, "error", runErr)
				return res, fmt.Errorf("docker run: runtime error (exit %d): %w", ec, runErr)
			}
			res.ExitCode = ec
			logger.Info("sandbox exec done", "backend", "docker", "command", cmdStr, "exit_code", res.ExitCode)
			return res, nil
		}
		// Not an exit status: spawn failure, binary missing, etc. — a backend fault.
		logger.Error("sandbox exec backend fault", "backend", "docker", "command", cmdStr, "error", runErr)
		return res, fmt.Errorf("docker run: %w", runErr)
	}
	logger.Info("sandbox exec done", "backend", "docker", "command", cmdStr, "exit_code", res.ExitCode)
	return res, nil
}

// Preflight implements Backend. It verifies, in order: the docker binary is
// runnable, the daemon is reachable, the base image is present, and a trivial
// network-isolated container runs to completion.
func (b *DockerBackend) Preflight(ctx context.Context) error {
	// 1. Daemon reachable (also proves the binary runs). `docker version` talks
	//    to the daemon; a non-zero status means it is down or unreachable.
	if err := b.dockerCmd(ctx, 15*time.Second, "version"); err != nil {
		return fmt.Errorf("sandbox preflight: docker daemon unreachable (is Docker running?): %w", err)
	}
	// 2. Base image present locally (runs are network-isolated, so the image
	//    cannot be pulled on demand).
	if err := b.dockerCmd(ctx, 15*time.Second, "image", "inspect", b.cfg.Image); err != nil {
		return fmt.Errorf("sandbox preflight: base image %q not found locally — run `docker pull %s`: %w", b.cfg.Image, b.cfg.Image, err)
	}
	// 2.5. Resource caps must fit the host the daemon enforces against. `docker
	//      info` reports MemTotal/NCPU in the daemon's own units (VM-scoped on
	//      macOS, where Docker runs in a Linux VM) — the exact ceiling --memory and
	//      --cpus are enforced against — so validate the configured caps here,
	//      before any container spawn, to fail fast instead of OOM-killing the host.
	if err := b.validateHostCaps(ctx); err != nil {
		return err
	}
	// 3. A trivial hardened container actually runs, using the SAME docker run
	//    args as real executions so malformed caps (memory/cpus/pids-limit) are
	//    caught here instead of failing mid-review.
	tmpDir, err := os.MkdirTemp("", "atcr-preflight-*")
	if err != nil {
		return fmt.Errorf("sandbox preflight: cannot create throwaway snapshot dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	args, err := dockerRunArgs(b.cfg, RunSpec{Command: []string{"true"}, SnapshotDir: tmpDir})
	if err != nil {
		return fmt.Errorf("sandbox preflight: cannot build run args: %w", err)
	}
	if err := b.dockerCmd(ctx, 30*time.Second, args...); err != nil {
		return fmt.Errorf("sandbox preflight: trivial container failed to run: %w", err)
	}
	return nil
}

// limitedWriter wraps w and discards writes after n bytes. It bounds the
// memory a single sandbox run can consume while capturing output.
type limitedWriter struct {
	w io.Writer
	n int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.n <= 0 {
		return len(p), nil
	}
	if int64(len(p)) > lw.n {
		if _, err := lw.w.Write(p[:lw.n]); err != nil {
			return 0, err
		}
		lw.n = 0
		return len(p), nil
	}
	n, err := lw.w.Write(p)
	lw.n -= int64(n)
	return n, err
}

// validateHostCaps fails preflight when the configured memory/cpu caps exceed
// what the docker daemon reports for the host (MemTotal/NCPU). A non-numeric cap
// value is left for the trivial-run step to reject — this check is only the
// fits-the-host comparison. A daemon that reports zero/unknown for a field skips
// that field's check rather than guessing.
func (b *DockerBackend) validateHostCaps(ctx context.Context) error {
	out, err := b.dockerOutput(ctx, 15*time.Second, "info", "--format", "{{json .}}")
	if err != nil {
		return fmt.Errorf("sandbox preflight: cannot query docker info: %w", err)
	}
	var info struct {
		MemTotal int64 `json:"MemTotal"`
		NCPU     int   `json:"NCPU"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &info); err != nil {
		return fmt.Errorf("sandbox preflight: cannot parse docker info: %w", err)
	}
	if b.cfg.Memory != "" && info.MemTotal > 0 {
		if want, perr := parseDockerMemory(b.cfg.Memory); perr == nil && want > info.MemTotal {
			return fmt.Errorf("sandbox preflight: configured memory %s (%d bytes) exceeds host MemTotal (%d bytes)", b.cfg.Memory, want, info.MemTotal)
		}
	}
	if b.cfg.CPUs != "" && info.NCPU > 0 {
		if want, perr := strconv.ParseFloat(b.cfg.CPUs, 64); perr == nil && want > float64(info.NCPU) {
			return fmt.Errorf("sandbox preflight: configured cpus %s exceeds host NCPU (%d)", b.cfg.CPUs, info.NCPU)
		}
	}
	return nil
}

// parseDockerMemory parses a docker --memory value (a positive integer with an
// optional b/k/m/g suffix, 1024-based) into bytes. An empty string is 0 bytes.
func parseDockerMemory(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	mult := int64(1)
	numPart := s
	switch s[len(s)-1] {
	case 'b', 'B':
		numPart = s[:len(s)-1]
	case 'k', 'K':
		mult, numPart = 1024, s[:len(s)-1]
	case 'm', 'M':
		mult, numPart = 1024*1024, s[:len(s)-1]
	case 'g', 'G':
		mult, numPart = 1024*1024*1024, s[:len(s)-1]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(numPart), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory value %q: %w", s, err)
	}
	return n * mult, nil
}

// randHex returns n random bytes hex-encoded, used to make a unique container
// name. On the (vanishingly unlikely) crypto/rand failure it falls back to a
// nanosecond timestamp — uniqueness here is best-effort labeling, not security.
func randHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// dockerOutput runs a docker subcommand with a timeout and returns its captured
// stdout, wrapping a failure (with stderr context) as an error.
func (b *DockerBackend) dockerOutput(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, b.cfg.DockerPath, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s timed out: %w", strings.Join(args, " "), cctx.Err())
		}
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return out.String(), nil
}

// dockerCmd runs a docker subcommand with a timeout, discarding output and
// returning a wrapped error on failure.
func (b *DockerBackend) dockerCmd(ctx context.Context, timeout time.Duration, args ...string) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, b.cfg.DockerPath, args...)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("%s timed out: %w", strings.Join(args, " "), cctx.Err())
		}
		if cctx.Err() != nil {
			return fmt.Errorf("%s canceled: %w", strings.Join(args, " "), cctx.Err())
		}
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
