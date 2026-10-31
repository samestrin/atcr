// Package linear verifies and parses inbound Linear webhooks and looks up the
// GitHub PR linked to an issue through the Linear GraphQL API.
package linear

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/integrations/slack"
)

const (
	// HeaderSignature carries the hex HMAC-SHA256 of the raw request body.
	HeaderSignature = "Linear-Signature"

	// MaxTimestampSkew is how far webhookTimestamp may differ from now.
	MaxTimestampSkew = time.Minute

	// DefaultAPIURL is the Linear GraphQL endpoint.
	DefaultAPIURL = "https://api.linear.app/graphql"
	// DefaultTimeout bounds a single Linear API request.
	DefaultTimeout = 10 * time.Second

	maxResponseBytes = 1 << 20
)

// Distinct failures; callers can match them with errors.Is.
var (
	ErrNoSigningSecret  = errors.New("linear: signing secret is empty")
	ErrMissingSignature = errors.New("linear: missing " + HeaderSignature + " header")
	ErrBadSignature     = errors.New("linear: signature mismatch")
	ErrMalformedPayload = errors.New("linear: malformed webhook payload")
	ErrMissingTimestamp = errors.New("linear: payload has no webhookTimestamp")
	ErrStaleTimestamp   = errors.New("linear: webhookTimestamp outside the replay window")

	ErrNoAPIKey    = errors.New("linear: API key is empty")
	ErrAPITimeout  = errors.New("linear: API request timed out")
	ErrAPIRequest  = errors.New("linear: API request failed")
	ErrAPIStatus   = errors.New("linear: API returned an error status")
	ErrAPIResponse = errors.New("linear: malformed API response")
	ErrAPIErrors   = errors.New("linear: API returned GraphQL errors")
	ErrNoPR        = errors.New("linear: issue has no GitHub PR attachment")
)

// Event is the part of an issue-update webhook the trigger needs.
type Event struct {
	Action           string          `json:"action"`
	Type             string          `json:"type"`
	WebhookTimestamp int64           `json:"webhookTimestamp"`
	Data             EventData       `json:"data"`
	UpdatedFrom      json.RawMessage `json:"updatedFrom"`
}

// EventData is the issue snapshot inside an Event.
type EventData struct {
	ID     string `json:"id"`
	TeamID string `json:"teamId"`
	Team   struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	} `json:"team"`
	State struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"state"`
}

// UpdatedFromKeys returns the field names present in updatedFrom (the fields
// that changed in this update), e.g. "stateId".
func (e Event) UpdatedFromKeys() []string {
	var m map[string]json.RawMessage
	if json.Unmarshal(e.UpdatedFrom, &m) != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Verify checks the Linear-Signature header against the raw body in constant
// time, then enforces the replay window on the body's webhookTimestamp (Unix
// milliseconds). now is injected for tests. The signature is checked first so
// an unauthenticated body is never parsed.
func Verify(secret string, h http.Header, body []byte, now time.Time) (Event, error) {
	if secret == "" {
		return Event{}, ErrNoSigningSecret
	}
	sig := h.Get(HeaderSignature)
	if sig == "" {
		return Event{}, ErrMissingSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(sig)), []byte(want)) {
		return Event{}, ErrBadSignature
	}
	var ev Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return Event{}, ErrMalformedPayload
	}
	if ev.WebhookTimestamp == 0 {
		return Event{}, ErrMissingTimestamp
	}
	skew := now.Sub(time.UnixMilli(ev.WebhookTimestamp))
	if skew < 0 {
		skew = -skew
	}
	if skew > MaxTimestampSkew {
		return Event{}, ErrStaleTimestamp
	}
	return ev, nil
}

// PR identifies a GitHub pull request.
type PR struct {
	Owner  string
	Repo   string
	Number int
}

// Client queries the Linear GraphQL API.
type Client struct {
	APIKey  string
	URL     string        // defaults to DefaultAPIURL
	Timeout time.Duration // defaults to DefaultTimeout
	HTTP    *http.Client  // defaults to a fresh client
	// Logf receives one line per linked PR that PullRequest passes over, so
	// a later-linked PR never hides the real one without a trace. May be nil.
	Logf func(format string, args ...any)
}

const attachmentsQuery = `query($id: String!) { issue(id: $id) { attachments { nodes { url createdAt } } } }`

type gqlResponse struct {
	Data struct {
		Issue *struct {
			Attachments struct {
				Nodes []struct {
					URL       string `json:"url"`
					CreatedAt string `json:"createdAt"`
				} `json:"nodes"`
			} `json:"attachments"`
		} `json:"issue"`
	} `json:"data"`
	Errors []json.RawMessage `json:"errors"`
}

// PullRequest returns the GitHub PR attached to the issue. When several are
// attached it picks the newest by the attachment's createdAt, whatever order
// the API lists them in. The API key is never included in returned errors.
func (c *Client) PullRequest(ctx context.Context, issueID string) (PR, error) {
	if c.APIKey == "" {
		return PR{}, ErrNoAPIKey
	}
	url, timeout, hc := c.URL, c.Timeout, c.HTTP
	if url == "" {
		url = DefaultAPIURL
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if hc == nil {
		hc = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, _ := json.Marshal(map[string]any{
		"query":     attachmentsQuery,
		"variables": map[string]string{"id": issueID},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return PR{}, ErrAPIRequest
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.APIKey)

	resp, err := hc.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			return PR{}, ErrAPITimeout
		}
		// err can embed the URL; it never carries the key (header only), but
		// keep the text out of the error anyway.
		return PR{}, ErrAPIRequest
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return PR{}, fmt.Errorf("%w: HTTP %d", ErrAPIStatus, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return PR{}, ErrAPITimeout
		}
		return PR{}, ErrAPIRequest
	}
	var out gqlResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return PR{}, ErrAPIResponse
	}
	if len(out.Errors) > 0 {
		return PR{}, ErrAPIErrors
	}
	if out.Data.Issue == nil {
		return PR{}, ErrNoPR
	}
	type linked struct {
		pr PR
		at time.Time
	}
	var all []linked
	best := -1
	for _, n := range out.Data.Issue.Attachments.Nodes {
		owner, repo, num, perr := slack.ParsePRURL(n.URL)
		if perr != nil {
			continue
		}
		// A missing or unparseable createdAt is the zero time, so it loses to
		// any dated attachment and ties keep the first listed.
		at, _ := time.Parse(time.RFC3339Nano, n.CreatedAt)
		all = append(all, linked{PR{Owner: owner, Repo: repo, Number: num}, at})
		if best < 0 || at.After(all[best].at) {
			best = len(all) - 1
		}
	}
	if best < 0 {
		return PR{}, ErrNoPR
	}
	chosen := all[best]
	if c.Logf != nil {
		for _, l := range all {
			if l.pr == chosen.pr {
				continue
			}
			why := "older than"
			if !l.at.Before(chosen.at) {
				why = "attached no later than"
			}
			c.Logf("linear: issue %s: ignored linked PR %s: %s the chosen %s", issueID, l.pr, why, chosen.pr)
		}
	}
	return chosen.pr, nil
}

// String formats the pull request as owner/repo#number.
func (p PR) String() string {
	return p.Owner + "/" + p.Repo + "#" + strconv.Itoa(p.Number)
}
