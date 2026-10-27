package webhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/integrations/linear"
	"github.com/samestrin/atcr/internal/integrations/slack"
	"github.com/samestrin/atcr/internal/registry"
)

const (
	// SlackEventsPath is the route Slack's Events API posts to.
	SlackEventsPath = "/slack/events"
	// LinearWebhookPath is the route Linear's issue webhooks post to.
	LinearWebhookPath = "/linear/webhook"
	// linearDeliveryHeader carries Linear's delivery id; it is only logged.
	linearDeliveryHeader = "Linear-Delivery"
	// AckDeadline is how soon Slack needs a 2xx before it retries a delivery.
	// The handler never waits on a job, so it answers well inside it.
	AckDeadline = 3 * time.Second
	// dedupeWindow is how long a delivery id is remembered. Slack retries a
	// delivery three times over about five minutes.
	dedupeWindow = time.Hour
	// shutdownGrace bounds how long Serve waits for in-flight requests and
	// cancelled jobs to return once its context is done.
	shutdownGrace = 30 * time.Second
)

// ErrNotLoopback is returned by Listen for an address that is not loopback.
var ErrNotLoopback = errors.New("webhook: listen address must be loopback (localhost, 127.0.0.0/8 or ::1)")

// Options configures a Server. Zero values take the registry.WebhookConfig
// defaults.
type Options struct {
	// MaxBodyBytes caps a request body; a larger body gets 413.
	MaxBodyBytes int64
	// JobTimeout bounds each job; the job's context is cancelled when it passes.
	JobTimeout time.Duration
	// SlackSigningSecret verifies Slack requests. Empty disables the Slack
	// route: it answers 404.
	SlackSigningSecret string
	// Dispatch turns a verified app_mention into a job. ok=false acknowledges
	// the mention and runs nothing. nil runs nothing.
	Dispatch func(m *slack.AppMention) (job Job, ok bool)
	// LinearSigningSecret verifies Linear requests. Empty disables the Linear
	// route: it answers 404.
	LinearSigningSecret string
	// LinearDispatch turns a verified Linear event into a job. ok=false
	// acknowledges the event and runs nothing. nil runs nothing.
	LinearDispatch func(ev linear.Event) (job Job, ok bool)
	// Now is the clock for signature timestamps and deduplication.
	Now func() time.Time
	// Logf receives one line per refused request and per failed job. Lines
	// carry ids and keys, never the request body.
	Logf func(format string, args ...any)
}

// Server is the `atcr webhook` HTTP listener. It verifies and acknowledges
// each request at once and runs the job it triggers asynchronously.
type Server struct {
	opts  Options
	jobs  *jobs
	seen  *dedupe
	mu    sync.Mutex
	httpS *http.Server
}

// NewServer returns a Server for o.
func NewServer(o Options) *Server {
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = registry.DefaultWebhookMaxBodyBytes
	}
	if o.JobTimeout <= 0 {
		o.JobTimeout = registry.DefaultWebhookJobTimeout
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	return &Server{opts: o, jobs: newJobs(o.JobTimeout, o.Logf), seen: newDedupe(dedupeWindow)}
}

// Handler returns the request router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(SlackEventsPath, s.handleSlack)
	mux.HandleFunc(LinearWebhookPath, s.handleLinear)
	return mux
}

// listenAddr returns addr, or the loopback default when it is empty.
func listenAddr(addr string) string {
	if addr == "" {
		return registry.DefaultWebhookListen
	}
	return addr
}

// Listen binds addr (the loopback default when empty) and refuses any address
// whose host is not loopback, so the daemon is never exposed directly.
func Listen(addr string) (net.Listener, error) {
	addr = listenAddr(addr)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrNotLoopback, addr)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("%w: %q", ErrNotLoopback, addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("webhook: listen on %s: %w", addr, err)
	}
	return ln, nil
}

// Serve serves on ln until ctx is done, then stops accepting requests,
// cancels running jobs and waits for them (bounded by shutdownGrace).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	s.mu.Lock()
	s.httpS = srv
	s.mu.Unlock()

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		_ = s.jobs.Shutdown(context.Background())
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	err := s.Shutdown(sctx)
	if serr := <-served; serr != nil && !errors.Is(serr, http.ErrServerClosed) {
		err = errors.Join(err, serr)
	}
	return err
}

// Shutdown stops the HTTP server started by Serve, if any, cancels running
// jobs and waits for them to return or for ctx to be done.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpS
	s.mu.Unlock()
	var err error
	if srv != nil {
		err = srv.Shutdown(ctx)
	}
	return errors.Join(err, s.jobs.Shutdown(ctx))
}

func (s *Server) handleSlack(w http.ResponseWriter, r *http.Request) {
	if s.opts.SlackSigningSecret == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, ok := s.readBody(w, r, "slack")
	if !ok {
		return
	}
	now := s.opts.Now()
	if err := slack.Verify(s.opts.SlackSigningSecret, r.Header, body, now); err != nil {
		s.opts.Logf("webhook: refused slack request: %v", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ev, err := slack.ParseEvent(body)
	switch {
	case errors.Is(err, slack.ErrUnsupportedEvent):
		// Acknowledge, or Slack retries an event ATCR will never handle.
		w.WriteHeader(http.StatusOK)
		return
	case err != nil:
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if ev.Challenge != "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, ev.Challenge)
		return
	}
	// Acknowledge first: nothing below may wait on the job.
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	s.dispatch(ev.Mention, now)
}

// readBody reads the request body under the size cap. On failure it has
// already answered the request and returns ok=false.
func (s *Server) readBody(w http.ResponseWriter, r *http.Request, source string) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.opts.MaxBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			s.opts.Logf("webhook: %s request body over the %d-byte cap", source, s.opts.MaxBodyBytes)
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return nil, false
		}
		http.Error(w, "cannot read request body", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

func (s *Server) handleLinear(w http.ResponseWriter, r *http.Request) {
	if s.opts.LinearSigningSecret == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, ok := s.readBody(w, r, "linear")
	if !ok {
		return
	}
	now := s.opts.Now()
	ev, err := linear.Verify(s.opts.LinearSigningSecret, r.Header, body, now)
	if err != nil {
		s.opts.Logf("webhook: refused linear request: %v", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Acknowledge first: nothing below may wait on the job, and the PR
	// lookup is made inside it.
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	s.dispatchLinear(ev, r.Header.Get(linearDeliveryHeader), now)
}

// dispatchLinear starts the job for ev unless the same issue already moved to
// the same state within the dedupe window: a redelivery, or a second delivery
// of one status change, runs one job.
func (s *Server) dispatchLinear(ev linear.Event, delivery string, now time.Time) {
	if s.opts.LinearDispatch == nil {
		return
	}
	job, ok := s.opts.LinearDispatch(ev)
	if !ok || job.Run == nil {
		return
	}
	key := "linear:" + ev.Data.ID + ":" + ev.Data.State.ID
	if !s.seen.firstSeen(key, now) {
		s.opts.Logf("webhook: linear delivery %q for issue %s already handled for this state; ignored", delivery, ev.Data.ID)
		return
	}
	// A job that never reviewed must not burn the key for the window.
	job.Unrun = func() { s.seen.forget(key) }
	if err := s.jobs.Start(job); err != nil {
		s.seen.forget(key)
		s.opts.Logf("webhook: linear delivery %q for issue %s not started: %v", delivery, ev.Data.ID, err)
	}
}

// dispatch starts the job for m unless its delivery id was already handled.
func (s *Server) dispatch(m *slack.AppMention, now time.Time) {
	if !s.seen.firstSeen(m.EventID, now) {
		s.opts.Logf("webhook: slack event %s already handled; retry ignored", m.EventID)
		return
	}
	if s.opts.Dispatch == nil {
		return
	}
	job, ok := s.opts.Dispatch(m)
	if !ok || job.Run == nil {
		return
	}
	if err := s.jobs.Start(job); err != nil {
		s.opts.Logf("webhook: slack event %s not started: %v", m.EventID, err)
	}
}
