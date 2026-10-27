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

var (
	// ErrNoPushToken is returned when the push-token variable is not named or
	// holds no value. Nothing is checked out.
	ErrNoPushToken = errors.New("webhook: auto-fix push token is not set")
	// ErrNoAutoFixBase is returned for a job with no branch for the fix pull
	// request to target.
	ErrNoAutoFixBase = errors.New("webhook: auto-fix needs a base branch for the fix pull request")
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
}

// Run checks out t's pull request head and runs `atcr review --auto-fix` in
// it, bounded by ctx: when ctx is done the child gets SIGTERM, then a kill
// after autoFixKillGrace, and the worktree and its branch are removed either
// way. A missing push token or base is refused before anything is checked out.
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
		cmd.Cancel = func() error { return terminate(cmd.Process) }
		cmd.WaitDelay = autoFixKillGrace

		err := cmd.Run()
		out.Flush()
		if cerr := ctx.Err(); cerr != nil {
			return fmt.Errorf("webhook: auto-fix of %s: %w", label, cerr)
		}
		if err != nil {
			return fmt.Errorf("webhook: auto-fix of %s: atcr review --auto-fix: %w", label, err)
		}
		return nil
	})
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

// terminate asks p to stop with SIGTERM, which `atcr` turns into a cancelled
// context and a cleanup. Where SIGTERM cannot be sent (Windows) p is killed.
func terminate(p *os.Process) error {
	if p == nil {
		return nil
	}
	err := p.Signal(syscall.SIGTERM)
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return p.Kill()
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

func (w *autoFixLogWriter) emit() {
	line := strings.TrimRight(w.buf.String(), "\r")
	truncated := w.buf.Len() > autoFixLogChunk
	w.buf.Reset()
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
