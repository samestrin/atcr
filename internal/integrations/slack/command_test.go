package slack

import (
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
)

var testAllow = Allowlist{
	Workspaces: []string{"T1"},
	Channels:   []string{"C1"},
	Users:      []string{"U1"},
	Repos:      []string{"samestrin/atcr", "Acme/Widget.go"},
}

func mention(text string) *AppMention {
	return &AppMention{EventID: "Ev1", TeamID: "T1", Channel: "C1", User: "U1", Text: text, TS: "1.0"}
}

func TestParseReviewCommandAccepts(t *testing.T) {
	cases := []struct {
		name, text, repo string
		number           int
	}{
		{"slack-wrapped url", "<@UBOT> review <https://github.com/samestrin/atcr/pull/592>", "samestrin/atcr", 592},
		{"slack-wrapped url with label", "<@UBOT> review <https://github.com/samestrin/atcr/pull/7|https://github.com/samestrin/atcr/pull/7>", "samestrin/atcr", 7},
		{"bare url", "<@UBOT> review https://github.com/samestrin/atcr/pull/1", "samestrin/atcr", 1},
		{"extra whitespace and case", "  <@UBOT>   Review\t<https://github.com/samestrin/atcr/pull/12>  ", "samestrin/atcr", 12},
		{"repo matched case-insensitively, configured name returned", "<@UBOT> review <https://github.com/acme/widget.go/pull/3>", "Acme/Widget.go", 3},
		{"no leading mention", "review <https://github.com/samestrin/atcr/pull/4>", "samestrin/atcr", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseReviewCommand(mention(tc.text), testAllow)
			if err != nil {
				t.Fatalf("ParseReviewCommand(%q) error = %v", tc.text, err)
			}
			if got.Repo != tc.repo || got.Number != tc.number {
				t.Fatalf("ParseReviewCommand(%q) = %+v, want {%s %d}", tc.text, got, tc.repo, tc.number)
			}
		})
	}
}

func TestParseReviewCommandRefusesURL(t *testing.T) {
	urls := map[string]string{
		"non-GitHub host":       "https://gitlab.com/samestrin/atcr/pull/1",
		"lookalike host":        "https://github.com.evil.example/samestrin/atcr/pull/1",
		"subdomain host":        "https://www.github.com/samestrin/atcr/pull/1",
		"userinfo":              "https://evil@github.com/samestrin/atcr/pull/1",
		"port":                  "https://github.com:443/samestrin/atcr/pull/1",
		"http scheme":           "http://github.com/samestrin/atcr/pull/1",
		"dot-dot segment":       "https://github.com/samestrin/atcr/../../evil/pull/1",
		"dot-dot repo":          "https://github.com/samestrin/../pull/1",
		"dot-dot inside repo":   "https://github.com/samestrin/a..b/pull/1",
		"single-dot repo":       "https://github.com/samestrin/./pull/1",
		"encoded dot-dot":       "https://github.com/samestrin/%2e%2e/pull/1",
		"extra path":            "https://github.com/samestrin/atcr/pull/1/files",
		"trailing slash":        "https://github.com/samestrin/atcr/pull/1/",
		"query":                 "https://github.com/samestrin/atcr/pull/1?x=1",
		"fragment":              "https://github.com/samestrin/atcr/pull/1#issuecomment-1",
		"issues not pull":       "https://github.com/samestrin/atcr/issues/1",
		"missing repo":          "https://github.com/samestrin/pull/1",
		"zero number":           "https://github.com/samestrin/atcr/pull/0",
		"leading-zero number":   "https://github.com/samestrin/atcr/pull/01",
		"negative number":       "https://github.com/samestrin/atcr/pull/-1",
		"non-numeric number":    "https://github.com/samestrin/atcr/pull/abc",
		"oversized number":      "https://github.com/samestrin/atcr/pull/99999999999999999999",
		"backslash path":        `https://github.com/samestrin\atcr/pull/1`,
		"protocol-relative":     "//github.com/samestrin/atcr/pull/1",
		"label hides other url": "<https://evil.example/x/y/pull/1|https://github.com/samestrin/atcr/pull/1>",
	}
	for name, u := range urls {
		t.Run(name, func(t *testing.T) {
			text := "<@UBOT> review " + u
			if name != "label hides other url" {
				text = "<@UBOT> review <" + u + ">"
			}
			_, err := ParseReviewCommand(mention(text), testAllow)
			if !errors.Is(err, ErrInvalidPRURL) {
				t.Fatalf("ParseReviewCommand(%q) error = %v, want ErrInvalidPRURL", text, err)
			}
		})
	}
}

func TestParseReviewCommandRefusesUsage(t *testing.T) {
	texts := []string{
		"<@UBOT>",
		"<@UBOT> hello there",
		"<@UBOT> review",
		"<@UBOT> review <https://github.com/samestrin/atcr/pull/1> please",
		"<@UBOT> reviewer <https://github.com/samestrin/atcr/pull/1>",
	}
	for _, text := range texts {
		_, err := ParseReviewCommand(mention(text), testAllow)
		if !errors.Is(err, ErrUsage) {
			t.Errorf("ParseReviewCommand(%q) error = %v, want ErrUsage", text, err)
		}
	}
}

func TestParseReviewCommandRefusesUnlisted(t *testing.T) {
	const ok = "<@UBOT> review <https://github.com/samestrin/atcr/pull/1>"
	cases := []struct {
		name string
		edit func(*AppMention)
		want error
	}{
		{"unlisted repo", func(m *AppMention) { m.Text = "<@UBOT> review <https://github.com/other/atcr/pull/1>" }, ErrRepoNotAllowed},
		{"unlisted user", func(m *AppMention) { m.User = "U2" }, ErrUserNotAllowed},
		{"empty user", func(m *AppMention) { m.User = "" }, ErrUserNotAllowed},
		{"unlisted channel", func(m *AppMention) { m.Channel = "C2" }, ErrChannelNotAllowed},
		{"unlisted workspace", func(m *AppMention) { m.TeamID = "T2" }, ErrWorkspaceNotAllowed},
		// Identity is checked before the text, so an unlisted user learns
		// nothing about which commands or URLs are accepted.
		{"unlisted user with bad url", func(m *AppMention) { m.User = "U2"; m.Text = "<@UBOT> review nope" }, ErrUserNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mention(ok)
			tc.edit(m)
			_, err := ParseReviewCommand(m, testAllow)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParseReviewCommandEmptyAllowlistRefusesAll(t *testing.T) {
	m := mention("<@UBOT> review <https://github.com/samestrin/atcr/pull/1>")
	if _, err := ParseReviewCommand(m, Allowlist{}); !errors.Is(err, ErrWorkspaceNotAllowed) {
		t.Fatalf("empty allowlist error = %v, want ErrWorkspaceNotAllowed", err)
	}
	if _, err := ParseReviewCommand(nil, testAllow); !errors.Is(err, ErrUsage) {
		t.Fatalf("nil mention error = %v, want ErrUsage", err)
	}
}

func TestNewAllowlist(t *testing.T) {
	cfg := &registry.WebhookConfig{
		Repos: map[string]string{"samestrin/atcr": "/src/atcr"},
		Slack: &registry.WebhookSlackConfig{Workspaces: []string{"T1"}, Channels: []string{"C1"}, Users: []string{"U1"}},
	}
	a := NewAllowlist(cfg)
	got, err := ParseReviewCommand(mention("<@UBOT> review <https://github.com/samestrin/atcr/pull/9>"), a)
	if err != nil || got.Repo != "samestrin/atcr" || got.Number != 9 {
		t.Fatalf("got %+v, %v", got, err)
	}

	// No slack block refuses every Slack request.
	noSlack := NewAllowlist(&registry.WebhookConfig{Repos: cfg.Repos})
	if _, err := ParseReviewCommand(mention("<@UBOT> review <https://github.com/samestrin/atcr/pull/9>"), noSlack); !errors.Is(err, ErrWorkspaceNotAllowed) {
		t.Fatalf("no slack block error = %v, want ErrWorkspaceNotAllowed", err)
	}
	if a := NewAllowlist(nil); len(a.Repos)+len(a.Users) != 0 {
		t.Fatalf("NewAllowlist(nil) = %+v, want empty", a)
	}
}
