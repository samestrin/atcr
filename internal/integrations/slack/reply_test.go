package slack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testBotToken = "xoxb-111-222-plantedTOKENvalue"

type postedMessage struct {
	auth        string
	contentType string
	path        string
	body        map[string]any
}

func fakeSlack(t *testing.T, handler func(w http.ResponseWriter)) (*httptest.Server, chan postedMessage) {
	t.Helper()
	got := make(chan postedMessage, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		got <- postedMessage{auth: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type"), path: r.URL.Path, body: body}
		handler(w)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestPostThreadReply_SendsChatPostMessage(t *testing.T) {
	srv, got := fakeSlack(t, func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"ok":true}`) })
	p := &Poster{Token: testBotToken, BaseURL: srv.URL}

	require.NoError(t, p.PostThreadReply(context.Background(), "C1", "1700000000.000100", "hello"))

	msg := <-got
	assert.Equal(t, "/chat.postMessage", msg.path)
	assert.Equal(t, "Bearer "+testBotToken, msg.auth)
	assert.Contains(t, msg.contentType, "application/json")
	assert.Equal(t, "C1", msg.body["channel"])
	assert.Equal(t, "1700000000.000100", msg.body["thread_ts"])
	assert.Equal(t, "hello", msg.body["text"])
	assert.Equal(t, false, msg.body["unfurl_links"])
}

func TestPostThreadReply_SlackErrorIsReturnedWithoutToken(t *testing.T) {
	srv, _ := fakeSlack(t, func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"ok":false,"error":"channel_not_found"}`) })
	p := &Poster{Token: testBotToken, BaseURL: srv.URL}

	err := p.PostThreadReply(context.Background(), "C1", "1.2", "hello")
	require.ErrorIs(t, err, ErrPostFailed)
	assert.Contains(t, err.Error(), "channel_not_found")
	assert.NotContains(t, err.Error(), testBotToken)
}

func TestPostThreadReply_HTTPErrorStatus(t *testing.T) {
	srv, _ := fakeSlack(t, func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) })
	p := &Poster{Token: testBotToken, BaseURL: srv.URL}

	err := p.PostThreadReply(context.Background(), "C1", "1.2", "hello")
	require.ErrorIs(t, err, ErrPostFailed)
	assert.Contains(t, err.Error(), "500")
}

func TestPostThreadReply_UntrustedErrorCodeIsNotEchoed(t *testing.T) {
	srv, _ := fakeSlack(t, func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"ok":false,"error":"<!channel> /etc/passwd"}`)
	})
	p := &Poster{Token: testBotToken, BaseURL: srv.URL}

	err := p.PostThreadReply(context.Background(), "C1", "1.2", "hello")
	require.ErrorIs(t, err, ErrPostFailed)
	assert.NotContains(t, err.Error(), "/etc/passwd")
}

func TestPostThreadReply_TimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	p := &Poster{Token: testBotToken, BaseURL: srv.URL, Timeout: 50 * time.Millisecond}

	start := time.Now()
	err := p.PostThreadReply(context.Background(), "C1", "1.2", "hello")
	require.ErrorIs(t, err, ErrPostFailed)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "want a deadline error, got %v", err)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.NotContains(t, err.Error(), testBotToken)
}

func TestPostThreadReply_RefusesMissingInputs(t *testing.T) {
	p := &Poster{Token: testBotToken, BaseURL: "http://127.0.0.1:1"}
	assert.ErrorIs(t, p.PostThreadReply(context.Background(), "", "1.2", "x"), ErrPostFailed)
	assert.ErrorIs(t, (&Poster{BaseURL: "http://127.0.0.1:1"}).PostThreadReply(context.Background(), "C1", "1.2", "x"), ErrPostFailed)
}

func TestPostThreadReply_TransportErrorHidesToken(t *testing.T) {
	// A closed port: the dial fails. The error names the URL, never the token.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	p := &Poster{Token: testBotToken, BaseURL: url}

	err := p.PostThreadReply(context.Background(), "C1", "1.2", "hello")
	require.ErrorIs(t, err, ErrPostFailed)
	assert.False(t, strings.Contains(err.Error(), testBotToken))
}

func TestEscapeText(t *testing.T) {
	assert.Equal(t, "a &amp; &lt;!channel&gt; b", EscapeText("a & <!channel> b"))
}
