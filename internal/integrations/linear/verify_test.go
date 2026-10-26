package linear

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const secret = "whsec"

func sign(body string) http.Header {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	h := http.Header{}
	h.Set(HeaderSignature, hex.EncodeToString(m.Sum(nil)))
	return h
}

func TestVerify(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	body := `{"action":"update","type":"Issue","webhookTimestamp":1700000000000,"data":{"id":"i1","teamId":"t1","state":{"name":"In Review"}},"updatedFrom":{"stateId":"x"}}`
	ev, err := Verify(secret, sign(body), []byte(body), now)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Data.ID != "i1" || ev.Data.TeamID != "t1" || ev.Data.State.Name != "In Review" || len(ev.UpdatedFromKeys()) != 1 {
		t.Fatalf("bad parse: %+v", ev)
	}

	h := sign(body)
	h.Set(HeaderSignature, strings.Repeat("0", 64))
	cases := []struct {
		name string
		h    http.Header
		body string
		now  time.Time
		want error
	}{
		{"bad sig", h, body, now, ErrBadSignature},
		{"missing sig", http.Header{}, body, now, ErrMissingSignature},
		{"stale", sign(body), body, now.Add(2 * time.Minute), ErrStaleTimestamp},
		{"future", sign(body), body, now.Add(-2 * time.Minute), ErrStaleTimestamp},
		{"no ts", sign(`{}`), `{}`, now, ErrMissingTimestamp},
		{"malformed", sign(`nope`), `nope`, now, ErrMalformedPayload},
	}
	for _, c := range cases {
		if _, err := Verify(secret, c.h, []byte(c.body), c.now); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
	}
	if _, err := Verify("", sign(body), []byte(body), now); !errors.Is(err, ErrNoSigningSecret) {
		t.Errorf("empty secret: %v", err)
	}
}

const apiKey = "lin_api_SECRETKEY"

func fake(t *testing.T, h http.HandlerFunc) *Client {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return &Client{APIKey: apiKey, URL: s.URL, Timeout: 200 * time.Millisecond}
}

func TestPullRequest(t *testing.T) {
	ok := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != apiKey {
			t.Error("missing auth")
		}
		write(t, w, []byte(`{"data":{"issue":{"attachments":{"nodes":[{"url":"https://slack.com/x"},{"url":"https://github.com/o/r/pull/7"}]}}}}`))
	})
	pr, err := ok.PullRequest(context.Background(), "i1")
	if err != nil || pr != (PR{"o", "r", 7}) {
		t.Fatalf("got %+v %v", pr, err)
	}

	slow := fake(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	httpErr := fake(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	none := fake(t, func(w http.ResponseWriter, r *http.Request) {
		write(t, w, []byte(`{"data":{"issue":{"attachments":{"nodes":[{"url":"https://example.com/a"}]}}}}`))
	})
	for name, c := range map[string]struct {
		cl   *Client
		want error
	}{
		"timeout": {slow, ErrAPITimeout},
		"status":  {httpErr, ErrAPIStatus},
		"no pr":   {none, ErrNoPR},
	} {
		_, err := c.cl.PullRequest(context.Background(), "i1")
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v want %v", name, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), apiKey) {
			t.Errorf("%s: key leaked: %v", name, err)
		}
	}
	// unreachable server: connection error must not leak the key either
	dead := &Client{APIKey: apiKey, URL: "http://127.0.0.1:1", Timeout: time.Second}
	if _, err := dead.PullRequest(context.Background(), "i1"); !errors.Is(err, ErrAPIRequest) || strings.Contains(err.Error(), apiKey) {
		t.Errorf("dead: %v", err)
	}
}

func write(t *testing.T, w http.ResponseWriter, b []byte) {
	t.Helper()
	if _, err := w.Write(b); err != nil {
		t.Error(err)
	}
}

func TestPullRequestGuards(t *testing.T) {
	if _, err := (&Client{}).PullRequest(context.Background(), "i1"); !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("no key: %v", err)
	}
	notJSON := fake(t, func(w http.ResponseWriter, r *http.Request) { write(t, w, []byte("<html>")) })
	gqlErr := fake(t, func(w http.ResponseWriter, r *http.Request) { write(t, w, []byte(`{"errors":[{"message":"boom"}]}`)) })
	for name, c := range map[string]struct {
		cl   *Client
		want error
	}{
		"not json": {notJSON, ErrAPIResponse},
		"graphql":  {gqlErr, ErrAPIErrors},
	} {
		if _, err := c.cl.PullRequest(context.Background(), "i1"); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPullRequestDefaults(t *testing.T) {
	var gotURL string
	var hasDeadline bool
	c := &Client{APIKey: apiKey, HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		_, hasDeadline = r.Context().Deadline()
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"data":{"issue":{"attachments":{"nodes":[{"url":"https://github.com/o/r/pull/1"}]}}}}`)),
		}, nil
	})}}
	if _, err := c.PullRequest(context.Background(), "i1"); err != nil {
		t.Fatal(err)
	}
	if gotURL != DefaultAPIURL || !hasDeadline {
		t.Errorf("url=%q deadline=%v", gotURL, hasDeadline)
	}
}

func TestPullRequestEdges(t *testing.T) {
	bad := &Client{APIKey: apiKey, URL: "http://[::1"}
	if _, err := bad.PullRequest(context.Background(), "i1"); !errors.Is(err, ErrAPIRequest) {
		t.Errorf("bad url: %v", err)
	}
	nilIssue := fake(t, func(w http.ResponseWriter, r *http.Request) { write(t, w, []byte(`{"data":{"issue":null}}`)) })
	truncated := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		write(t, w, []byte("{"))
	})
	stall := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		write(t, w, []byte("{"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	for name, c := range map[string]struct {
		cl   *Client
		want error
	}{
		"nil issue": {nilIssue, ErrNoPR},
		"truncated": {truncated, ErrAPIRequest},
		"stall":     {stall, ErrAPITimeout},
	} {
		if _, err := c.cl.PullRequest(context.Background(), "i1"); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if k := (Event{UpdatedFrom: []byte("nope")}).UpdatedFromKeys(); k != nil {
		t.Errorf("keys: %v", k)
	}
}
