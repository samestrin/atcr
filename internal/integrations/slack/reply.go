package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	// DefaultAPIBaseURL is the Slack Web API root.
	DefaultAPIBaseURL = "https://slack.com/api"
	// DefaultPostTimeout bounds one chat.postMessage call.
	DefaultPostTimeout = 10 * time.Second
	// maxResponseBytes caps how much of Slack's answer is read.
	maxResponseBytes = 64 << 10
)

// ErrPostFailed is returned when a thread reply could not be posted. Its
// message never carries the bot token.
var ErrPostFailed = errors.New("slack: post thread reply failed")

// slackErrorCode is the shape of Slack's documented error codes
// (e.g. "channel_not_found"). Anything else is not echoed into a log.
var slackErrorCode = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// Poster posts thread replies through the Slack Web API's chat.postMessage.
type Poster struct {
	// Token is the bot token, sent only as the Authorization header.
	Token string
	// BaseURL is the Web API root; empty is DefaultAPIBaseURL.
	BaseURL string
	// Timeout bounds each post; zero is DefaultPostTimeout.
	Timeout time.Duration
	// Client sends the request; nil is http.DefaultClient.
	Client *http.Client
}

type postMessage struct {
	Channel     string `json:"channel"`
	ThreadTS    string `json:"thread_ts"`
	Text        string `json:"text"`
	UnfurlLinks bool   `json:"unfurl_links"`
	UnfurlMedia bool   `json:"unfurl_media"`
}

// PostThreadReply posts text as a reply in the thread threadTS of channel,
// within the Poster's timeout. text is sent as is: escape untrusted parts with
// EscapeText first.
func (p *Poster) PostThreadReply(ctx context.Context, channel, threadTS, text string) error {
	if p.Token == "" {
		return fmt.Errorf("%w: no bot token", ErrPostFailed)
	}
	if channel == "" || threadTS == "" {
		return fmt.Errorf("%w: no channel or thread", ErrPostFailed)
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultPostTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(postMessage{Channel: channel, ThreadTS: threadTS, Text: text})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPostFailed, err)
	}
	base := p.BaseURL
	if base == "" {
		base = DefaultAPIBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPostFailed, err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+p.Token)

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return fmt.Errorf("%w: %w", ErrPostFailed, cerr)
		}
		// A *url.Error names the method and URL, never the request headers.
		return fmt.Errorf("%w: %w", ErrPostFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: HTTP %d", ErrPostFailed, resp.StatusCode)
	}
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return fmt.Errorf("%w: unreadable response: %w", ErrPostFailed, err)
	}
	if !out.OK {
		code := out.Error
		if !slackErrorCode.MatchString(code) {
			code = "unrecognized error"
		}
		return fmt.Errorf("%w: %s", ErrPostFailed, code)
	}
	return nil
}

// textEscaper escapes the three characters Slack's mrkdwn treats as control
// characters, so untrusted text cannot form a mention such as <!channel> or a
// link.
var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// EscapeText escapes s for a Slack message's text field.
func EscapeText(s string) string { return textEscaper.Replace(s) }
