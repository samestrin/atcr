package webhook

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrJobBusy is returned when a job with the same key (one pull request)
	// is already running. One PR runs one job at a time.
	ErrJobBusy = errors.New("webhook: a job for this pull request is already running")
	// ErrShuttingDown is returned for a job started after Shutdown.
	ErrShuttingDown = errors.New("webhook: daemon is shutting down")
)

// Job is one unit of triggered work, run asynchronously after the request
// that triggered it has been acknowledged.
type Job struct {
	// Key identifies the pull request the job works on (e.g. "owner/repo#12").
	// Two jobs with one key never run at the same time.
	Key string
	// Run does the work. Its context is cancelled at the per-job timeout and
	// on shutdown; Run must return promptly once it is.
	Run func(ctx context.Context) error
}

// jobs runs Jobs in their own goroutines, each under a timeout, at most one
// per key at a time.
type jobs struct {
	timeout time.Duration
	logf    func(format string, args ...any)

	base   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	running map[string]struct{}
	closed  bool
}

func newJobs(timeout time.Duration, logf func(string, ...any)) *jobs {
	base, cancel := context.WithCancel(context.Background())
	return &jobs{timeout: timeout, logf: logf, base: base, cancel: cancel, running: map[string]struct{}{}}
}

// Start runs job in a new goroutine and returns at once. It returns
// ErrJobBusy when a job with the same key is running and ErrShuttingDown
// after Shutdown.
func (j *jobs) Start(job Job) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return ErrShuttingDown
	}
	if _, busy := j.running[job.Key]; busy {
		return fmt.Errorf("%w: %s", ErrJobBusy, job.Key)
	}
	j.running[job.Key] = struct{}{}
	j.wg.Add(1)
	go j.run(job)
	return nil
}

func (j *jobs) run(job Job) {
	ctx, cancel := context.WithTimeout(j.base, j.timeout)
	defer func() {
		cancel()
		j.mu.Lock()
		delete(j.running, job.Key)
		j.mu.Unlock()
		j.wg.Done()
	}()
	defer func() {
		// A panicking job must not take the daemon down with it.
		if r := recover(); r != nil {
			j.logf("webhook: job %s panicked: %v", job.Key, r)
		}
	}()
	err := job.Run(ctx)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		j.logf("webhook: job %s cancelled at the %s timeout", job.Key, j.timeout)
	case err != nil:
		j.logf("webhook: job %s failed: %v", job.Key, err)
	}
}

// Shutdown refuses new jobs, cancels the running ones and waits for them to
// return, or for ctx to be done.
func (j *jobs) Shutdown(ctx context.Context) error {
	j.mu.Lock()
	j.closed = true
	j.mu.Unlock()
	j.cancel()
	done := make(chan struct{})
	go func() {
		j.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dedupe remembers delivery ids for a window, so a delivery Slack retries is
// handled once.
type dedupe struct {
	window time.Duration
	mu     sync.Mutex
	seen   map[string]time.Time
}

func newDedupe(window time.Duration) *dedupe {
	return &dedupe{window: window, seen: map[string]time.Time{}}
}

// firstSeen records id at now and reports whether it was not already seen
// within the window. An empty id cannot be matched and is always new.
func (d *dedupe) firstSeen(id string, now time.Time) bool {
	if id == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, at := range d.seen {
		if now.Sub(at) > d.window {
			delete(d.seen, k)
		}
	}
	if _, ok := d.seen[id]; ok {
		return false
	}
	d.seen[id] = now
	return true
}
