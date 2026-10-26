package slack

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	typeURLVerification = "url_verification"
	typeEventCallback   = "event_callback"
	eventAppMention     = "app_mention"
)

// ErrUnsupportedEvent is returned for a payload that is neither a
// url_verification challenge nor an app_mention event callback.
var ErrUnsupportedEvent = errors.New("slack: unsupported event")

// Event is a parsed, supported Slack payload.
type Event struct {
	// Challenge is set for url_verification and must be echoed back.
	Challenge string
	// Mention is set for an app_mention event callback.
	Mention *AppMention
}

// AppMention is the subset of an app_mention event ATCR uses.
type AppMention struct {
	EventID  string // delivery id, used to deduplicate retries
	TeamID   string
	Channel  string
	User     string
	Text     string
	TS       string
	ThreadTS string
}

type envelope struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	TeamID    string `json:"team_id"`
	EventID   string `json:"event_id"`
	Event     struct {
		Type     string `json:"type"`
		User     string `json:"user"`
		Text     string `json:"text"`
		TS       string `json:"ts"`
		ThreadTS string `json:"thread_ts"`
		Channel  string `json:"channel"`
	} `json:"event"`
}

// ParseEvent parses a verified Slack request body.
func ParseEvent(body []byte) (*Event, error) {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("slack: invalid payload: %w", err)
	}
	switch env.Type {
	case typeURLVerification:
		if env.Challenge == "" {
			return nil, errors.New("slack: url_verification without challenge")
		}
		return &Event{Challenge: env.Challenge}, nil
	case typeEventCallback:
		if env.Event.Type != eventAppMention {
			return nil, fmt.Errorf("%w: event type %q", ErrUnsupportedEvent, env.Event.Type)
		}
		return &Event{Mention: &AppMention{
			EventID:  env.EventID,
			TeamID:   env.TeamID,
			Channel:  env.Event.Channel,
			User:     env.Event.User,
			Text:     env.Event.Text,
			TS:       env.Event.TS,
			ThreadTS: env.Event.ThreadTS,
		}}, nil
	}
	return nil, fmt.Errorf("%w: type %q", ErrUnsupportedEvent, env.Type)
}
