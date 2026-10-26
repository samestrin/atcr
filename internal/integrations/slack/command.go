package slack

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Distinct command refusals; callers can match them with errors.Is.
var (
	ErrUsage               = errors.New("slack: expected `review <github-pr-url>`")
	ErrInvalidPRURL        = errors.New("slack: not a https://github.com/<owner>/<repo>/pull/<n> URL")
	ErrWorkspaceNotAllowed = errors.New("slack: workspace not allowlisted")
	ErrChannelNotAllowed   = errors.New("slack: channel not allowlisted")
	ErrUserNotAllowed      = errors.New("slack: user not allowlisted")
	ErrRepoNotAllowed      = errors.New("slack: repository not allowlisted")
)

// prURL is the only accepted PR URL form. It is matched against the raw string,
// so userinfo, a port, a query, a fragment, percent-encoding, a trailing slash
// and any extra path segment are all refused.
var prURL = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/([1-9][0-9]*)$`)

// Allowlist is who may trigger a review and on which repositories, filled
// from the webhook config's slack lists and repos keys. An empty list admits
// nobody.
type Allowlist struct {
	Workspaces []string
	Channels   []string
	Users      []string
	// Repos holds `owner/repo` names, matched case-insensitively as GitHub does.
	Repos []string
}

// ReviewCommand is an authorized `review <pr-url>` request.
type ReviewCommand struct {
	// Repo is the allowlisted `owner/repo` name as configured.
	Repo   string
	Number int
}

// ParseReviewCommand authorizes the mention's workspace, channel and user, then
// parses `review <pr-url>` from its text and checks the repository. Identity is
// checked first so an unlisted caller learns nothing about accepted input.
func ParseReviewCommand(m *AppMention, allow Allowlist) (ReviewCommand, error) {
	if m == nil {
		return ReviewCommand{}, ErrUsage
	}
	switch {
	case !listed(allow.Workspaces, m.TeamID):
		return ReviewCommand{}, ErrWorkspaceNotAllowed
	case !listed(allow.Channels, m.Channel):
		return ReviewCommand{}, ErrChannelNotAllowed
	case !listed(allow.Users, m.User):
		return ReviewCommand{}, ErrUserNotAllowed
	}

	args := strings.Fields(m.Text)
	for len(args) > 0 && strings.HasPrefix(args[0], "<@") && strings.HasSuffix(args[0], ">") {
		args = args[1:]
	}
	if len(args) != 2 || !strings.EqualFold(args[0], "review") {
		return ReviewCommand{}, ErrUsage
	}

	owner, repo, number, err := ParsePRURL(unwrapLink(args[1]))
	if err != nil {
		return ReviewCommand{}, err
	}
	want := owner + "/" + repo
	for _, name := range allow.Repos {
		if strings.EqualFold(name, want) {
			return ReviewCommand{Repo: name, Number: number}, nil
		}
	}
	return ReviewCommand{}, fmt.Errorf("%w: %s", ErrRepoNotAllowed, want)
}

// ParsePRURL accepts only https://github.com/<owner>/<repo>/pull/<n>.
func ParsePRURL(raw string) (owner, repo string, number int, err error) {
	m := prURL.FindStringSubmatch(raw)
	if m == nil {
		return "", "", 0, ErrInvalidPRURL
	}
	owner, repo = m[1], m[2]
	if owner == "." || repo == "." || strings.Contains(owner, "..") || strings.Contains(repo, "..") {
		return "", "", 0, ErrInvalidPRURL
	}
	number, convErr := strconv.Atoi(m[3])
	if convErr != nil {
		return "", "", 0, ErrInvalidPRURL
	}
	return owner, repo, number, nil
}

// unwrapLink returns the target of a Slack-formatted link `<url>` or
// `<url|label>`. The label is display text and is never trusted.
func unwrapLink(s string) string {
	if !strings.HasPrefix(s, "<") || !strings.HasSuffix(s, ">") {
		return s
	}
	s = s[1 : len(s)-1]
	if i := strings.IndexByte(s, '|'); i >= 0 {
		s = s[:i]
	}
	return s
}

func listed(ids []string, id string) bool {
	return id != "" && slices.Contains(ids, id)
}
