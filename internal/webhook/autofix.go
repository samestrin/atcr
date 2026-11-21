package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/samestrin/atcr/internal/log"
)

const (
	// autoFixTokenVar is the variable `atcr review --auto-fix` reads its push
	// token from when --token is not passed. The token is only ever handed over
	// this way: an argument is visible to every user of the host in ps.
	autoFixTokenVar = "GITHUB_TOKEN"
	// autoFixLogChunk is the most bytes of one output line held before it is
	// logged; the rest of that line is dropped.
	autoFixLogChunk = 64 << 10
	// autoFixLogLineRunes caps one logged output line, after redaction.
	autoFixLogLineRunes = 1000
)

// AutoFixUsagePrefix starts the token usage line `atcr review --auto-fix` ends
// with. cli/autofix.go writes the line with this constant and the runner reads
// it back, so the writer and the reader cannot drift apart.
const AutoFixUsagePrefix = "atcr-auto-fix-usage:"

var (
	// ErrNoPushToken is returned when the push-token variable is not named or
	// holds no value. Nothing is checked out.
	ErrNoPushToken = errors.New("webhook: auto-fix push token is not set")
	// ErrNoAutoFixBase is returned for a job with no branch for the fix pull
	// request to target.
	ErrNoAutoFixBase = errors.New("webhook: auto-fix needs a base branch for the fix pull request")
	// ErrAutoFixJobTokenCap is returned for a job whose reported token usage
	// is over AutoFixRunner.JobTokenCap. The job has already run: its spend is
	// charged to the day all the same.
	ErrAutoFixJobTokenCap = errors.New("webhook: auto-fix job token cap exceeded")
	// ErrAutoFixNoUsage is returned, under token accounting, for a job whose
	// child printed no usable usage line. Its spend is unknown and uncharged,
	// so it is a degradation, never a pass.
	ErrAutoFixNoUsage = errors.New("webhook: auto-fix child printed no token usage line")
)

// autoFixKillGrace is how long a cancelled `atcr` child has, after SIGTERM, to
// clean up (its sandbox container, a half-pushed branch notice) before it is
// killed. A var so a test can shorten it.
var autoFixKillGrace = 30 * time.Second

// childRepoVars locate a repository for git. An inherited one would point the
// child's git at another repository than the worktree it runs in.
var childRepoVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_PREFIX", "GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
}

// AutoFixTarget is the pull request one auto-fix job works on.
type AutoFixTarget struct {
	// Repo is the allowlisted `owner/repo`, a key of AutoFixRunner.Repos.
	Repo string
	// Number is the pull request number.
	Number int
	// Base is the branch the fix pull request targets: the linked pull
	// request's head branch, which the worktree's HEAD equals.
	Base string
}

// AutoFixRunner runs `atcr review --auto-fix` as a subprocess in a throwaway
// worktree of a pull request head (WithPRCheckout). The child reads its
// project config, and so its validate_command and sandbox, from the operator's
// allowlisted checkout (--config-dir), never from the pull request, and gets
// the push token in its environment only.
type AutoFixRunner struct {
	// Repos maps an allowlisted `owner/repo` to its local checkout. The
	// checkout is also the child's --config-dir.
	Repos map[string]string
	// Scratch holds the throwaway worktrees; empty is the system temp directory.
	Scratch string
	// Binary is the atcr executable; empty is the running executable.
	Binary string
	// PushTokenEnv names the daemon variable holding the push-capable GitHub
	// token (webhook.linear.auto_fix.push_token_env).
	PushTokenEnv string
	// Secrets are other credential values scrubbed from every logged line.
	Secrets []string
	// Logf receives the child's output, one redacted line per call.
	Logf func(format string, args ...any)
	// JobTokenCap is the most tokens one job may report before Run returns
	// ErrAutoFixJobTokenCap; zero is no cap. The usage is only known once the
	// child exits, so the cap flags the job after the fact and never stops it.
	JobTokenCap int
	// State, when set, is charged with every job's reported token usage, so
	// its DailyTokenCap refuses the next Claim once the day's spend reaches it.
	State *AutoFixState
	// Now is the clock the usage is charged at; nil is time.Now.
	Now func() time.Time
}

// Run checks out t's pull request head and runs `atcr review --auto-fix` in
// it, bounded by ctx: when ctx is done the child gets SIGTERM, then a kill
// after autoFixKillGrace (on unix, of its whole process group, so nothing it
// started outlives it), and the worktree and its branch are removed either
// way. A missing push token or base is refused before anything is checked out.
//
// With token accounting on (JobTokenCap or State set), Run reads the usage line
// the child ends with, whether it passed or failed: the usage is charged to
// State, a job over JobTokenCap returns ErrAutoFixJobTokenCap, and a child with
// no usage line returns ErrAutoFixNoUsage. These join any error of the child's
// own.
func (r *AutoFixRunner) Run(ctx context.Context, t AutoFixTarget) error {
	var token string
	if r.PushTokenEnv != "" {
		token = strings.TrimSpace(os.Getenv(r.PushTokenEnv))
	}
	if token == "" {
		return fmt.Errorf("%w: %q is empty or unset", ErrNoPushToken, r.PushTokenEnv)
	}
	base := strings.TrimSpace(t.Base)
	if base == "" {
		return ErrNoAutoFixBase
	}
	bin := r.Binary
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("webhook: locate the atcr executable: %w", err)
		}
		bin = exe
	}
	label := t.Repo + "#" + strconv.Itoa(t.Number)

	return WithPRCheckout(ctx, r.Repos, t.Repo, t.Number, r.Scratch, func(ctx context.Context, co *Checkout) error {
		// The --flag=value form keeps a value from ever reading as a flag.
		cmd := exec.CommandContext(ctx, bin, "review", "--auto-fix",
			"--auto-fix-base="+base,
			"--config-dir="+r.Repos[t.Repo],
			"--repo="+t.Repo)
		cmd.Dir = co.Dir
		cmd.Env = append(childEnv(os.Environ()), autoFixTokenVar+"="+token)
		out := newAutoFixLogWriter(label, append([]string{token}, r.Secrets...), r.Logf)
		cmd.Stdout, cmd.Stderr = out, out
		reap := configureProcessGroup(cmd)
		cmd.WaitDelay = autoFixKillGrace

		err := cmd.Run()
		reap()
		out.Flush()
		var errs []error
		if cerr := ctx.Err(); cerr != nil {
			errs = append(errs, fmt.Errorf("webhook: auto-fix of %s: %w", label, cerr))
		} else if err != nil {
			errs = append(errs, fmt.Errorf("webhook: auto-fix of %s: atcr review --auto-fix: %w", label, err))
		}
		if r.JobTokenCap > 0 || r.State != nil {
			if uerr := r.chargeUsage(label, out); uerr != nil {
				errs = append(errs, uerr)
			}
		}
		if len(errs) == 1 {
			return errs[0]
		}
		return errors.Join(errs...)
	})
}

// chargeUsage charges the usage out saw to State and checks it against
// JobTokenCap.
func (r *AutoFixRunner) chargeUsage(label string, out *autoFixLogWriter) error {
	in, outTokens, ok := out.Usage()
	if !ok {
		return fmt.Errorf("%w: auto-fix of %s; its spend is not counted against the caps", ErrAutoFixNoUsage, label)
	}
	total := addCapped(in, outTokens)
	var errs []error
	if r.State != nil {
		now := time.Now
		if r.Now != nil {
			now = r.Now
		}
		if err := r.State.AddTokens(total, now()); err != nil {
			errs = append(errs, fmt.Errorf("webhook: auto-fix of %s: charge %d tokens: %w", label, total, err))
		}
	}
	if r.JobTokenCap > 0 && total > r.JobTokenCap {
		errs = append(errs, fmt.Errorf("%w: auto-fix of %s used %d tokens (in %d, out %d), cap %d",
			ErrAutoFixJobTokenCap, label, total, in, outTokens, r.JobTokenCap))
	}
	return errors.Join(errs...)
}

// parseAutoFixUsage reads a usage line,
//
//	atcr-auto-fix-usage: tokens_in=<n> tokens_out=<n> ...
//
// It must start the line and carry tokens_in and tokens_out once each, as
// non-negative integers; other fields are ignored.
func parseAutoFixUsage(line string) (in, out int, ok bool) {
	rest, found := strings.CutPrefix(line, AutoFixUsagePrefix)
	if !found {
		return 0, 0, false
	}
	vals := map[string]int{}
	for _, f := range strings.Fields(rest) {
		k, v, _ := strings.Cut(f, "=")
		if k != "tokens_in" && k != "tokens_out" {
			continue
		}
		n, err := strconv.Atoi(v)
		if _, dup := vals[k]; dup || err != nil || n < 0 {
			return 0, 0, false
		}
		vals[k] = n
	}
	in, inOK := vals["tokens_in"]
	out, outOK := vals["tokens_out"]
	if !inOK || !outOK {
		return 0, 0, false
	}
	return in, out, true
}

// childEnv is env without GITHUB_TOKEN, which the caller replaces with the push
// token, and without any variable that would point git at another repository.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == autoFixTokenVar || slices.Contains(childRepoVars, k) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// signalProcess and killProcess are the calls terminate makes. Vars so a test
// can fail the signal the way Windows does and reach the Kill fallback.
var (
	signalProcess = (*os.Process).Signal
	killProcess   = (*os.Process).Kill
)

// terminate asks p to stop with SIGTERM, which `atcr` turns into a cancelled
// context and a cleanup. Where SIGTERM cannot be sent (Windows) p is killed.
func terminate(p *os.Process) error {
	if p == nil {
		return nil
	}
	err := signalProcess(p, syscall.SIGTERM)
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return killProcess(p)
}

// autoFixLogWriter logs the child's output one line at a time, each with every
// secret redacted and capped at autoFixLogLineRunes. Redaction runs on whole
// lines, so a secret split across two writes is still caught.
type autoFixLogWriter struct {
	prefix string
	redact *log.Redactor
	logf   func(format string, args ...any)

	mu      sync.Mutex
	buf     bytes.Buffer
	discard bool // dropping the rest of an over-long line

	// The last usage line seen (parseAutoFixUsage), read before redaction.
	usageIn, usageOut int
	usageSeen         bool
}

func newAutoFixLogWriter(label string, secrets []string, logf func(string, ...any)) *autoFixLogWriter {
	return &autoFixLogWriter{prefix: "webhook: auto-fix " + label + ": ", redact: log.NewRedactor("", secrets...), logf: logf}
}

func (w *autoFixLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		if !w.discard {
			w.buf.Write(chunk)
		}
		switch {
		case i >= 0:
			if !w.discard {
				w.emit()
			}
			w.discard = false
			p = p[i+1:]
		default:
			p = nil
		}
		if !w.discard && w.buf.Len() > autoFixLogChunk {
			// Only the head of the line is logged, after redaction and the rune
			// cap, so a secret straddling the chunk end is never printed.
			w.emit()
			w.discard = true
		}
	}
	return n, nil
}

// Flush logs a final line that had no newline.
func (w *autoFixLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.discard {
		w.emit()
	}
	w.discard = false
}

// Usage returns the token usage of the last usage line the child printed.
// Call it after Flush.
func (w *autoFixLogWriter) Usage() (in, out int, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.usageIn, w.usageOut, w.usageSeen
}

func (w *autoFixLogWriter) emit() {
	line := strings.TrimRight(w.buf.String(), "\r")
	truncated := w.buf.Len() > autoFixLogChunk
	w.buf.Reset()
	if in, out, ok := parseAutoFixUsage(line); ok && !truncated {
		w.usageIn, w.usageOut, w.usageSeen = in, out, true
	}
	line = w.redact.Redact(line)
	line = tokenShapes.ReplaceAllString(line, "[redacted]")
	if r := []rune(line); len(r) > autoFixLogLineRunes {
		line, truncated = string(r[:autoFixLogLineRunes-1]), true
	}
	if truncated {
		line += "…"
	}
	if strings.TrimSpace(line) == "" || w.logf == nil {
		return
	}
	w.logf("%s%s", w.prefix, line)
}
