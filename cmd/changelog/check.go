package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/gitexec"
)

// codeDirs are the trees whose non-test files need a changelog fragment.
var codeDirs = []string{"cli/", "internal/", "cmd/", "reconcile/", "personas/"}

const (
	fragmentDir   = "changelog.d/"
	optOutLabel   = "no-changelog"
	changelogFile = "CHANGELOG.md"
)

var (
	optOutLineRE  = regexp.MustCompile(`(?i)^\s*changelog:\s*none\s*$`)
	htmlCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)
)

// change is one path in a PR diff; status is git's letter (A, M, D, ...).
type change struct {
	status byte
	path   string
}

// checkEnv is the untrusted PR metadata. CI passes it through environment
// variables, never through the command line or a shell expansion.
const (
	envBody   = "CHANGELOG_PR_BODY"
	envLabels = "CHANGELOG_PR_LABELS" // JSON array of label names
)

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("changelog check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	base := fs.String("base", "", "base commit (required)")
	head := fs.String("head", "", "head commit (required)")
	repo := fs.String("repo", ".", "repository directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *base == "" || *head == "" || fs.NArg() > 0 {
		_, _ = fmt.Fprintln(stderr, "usage: changelog check --base <commit> --head <commit> [--repo dir]  (PR body and labels come from "+envBody+" and "+envLabels+")")
		return 2
	}
	labels, err := parseLabels(os.Getenv(envLabels))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "changelog:", err)
		return 2
	}
	problems, err := checkPR(context.Background(), *repo, *base, *head, os.Getenv(envBody), labels)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "changelog:", err)
		return 1
	}
	if len(problems) > 0 {
		for _, p := range problems {
			_, _ = fmt.Fprintln(stderr, "changelog: "+p)
		}
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "changelog: ok")
	return 0
}

func parseLabels(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var labels []string
	if err := json.Unmarshal([]byte(raw), &labels); err != nil {
		return nil, fmt.Errorf("%s is not a JSON array of strings: %w", envLabels, err)
	}
	return labels, nil
}

func checkPR(ctx context.Context, repo, base, head, body string, labels []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := gitOutput(ctx, repo, "diff", "--name-status", "--no-renames", "-z", base+"..."+head)
	if err != nil {
		return nil, err
	}
	changes, err := parseNameStatus(out)
	if err != nil {
		return nil, err
	}
	mergeBase, err := gitOutput(ctx, repo, "merge-base", base, head)
	if err != nil {
		return nil, err
	}
	mb := strings.TrimSpace(string(mergeBase))
	baseLog, err := gitOutput(ctx, repo, "show", mb+":"+changelogFile)
	if err != nil {
		return nil, err
	}
	headLog, err := gitOutput(ctx, repo, "show", head+":"+changelogFile)
	if err != nil {
		return nil, err
	}
	frags := map[string]string{}
	if unreleasedBody(string(baseLog)) != unreleasedBody(string(headLog)) {
		for _, c := range changes {
			if !isAddedFragment(c) {
				continue
			}
			b, err := gitOutput(ctx, repo, "show", head+":"+c.path)
			if err != nil {
				return nil, err
			}
			frags[c.path] = string(b)
		}
	}
	return evaluate(changes, frags, string(baseLog), string(headLog), body, labels), nil
}

func gitOutput(ctx context.Context, repo string, args ...string) ([]byte, error) {
	full := []string{"-C", repo, args[0]}
	if args[0] == "diff" || args[0] == "show" {
		// Right after the subcommand, the only place git accepts it: a poisoned
		// diff.external in the repo's .git/config must not run.
		full = append(full, "--no-ext-diff")
	}
	cmd := gitexec.CommandContextFn(ctx, append(full, args[1:]...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// parseNameStatus reads `git diff --name-status -z` output: STATUS NUL PATH NUL.
func parseNameStatus(out []byte) ([]change, error) {
	parts := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(parts) == 1 && parts[0] == "" {
		return nil, nil
	}
	if len(parts)%2 != 0 {
		return nil, errors.New("unexpected git diff output")
	}
	var cs []change
	for i := 0; i < len(parts); i += 2 {
		if parts[i] == "" {
			return nil, errors.New("unexpected git diff output")
		}
		cs = append(cs, change{status: parts[i][0], path: parts[i+1]})
	}
	return cs, nil
}

// evaluate returns one message per failed rule; none means the PR passes.
// frags holds the content of each fragment the PR adds, keyed by path.
func evaluate(changes []change, frags map[string]string, baseLog, headLog, body string, labels []string) []string {
	var problems []string
	if !optedOut(body, labels) && needsFragment(changes) && !addsFragment(changes) {
		problems = append(problems, "this PR changes code under "+strings.Join(codeDirs, ", ")+" but adds no fragment. "+
			"Add changelog.d/<category>-<slug>.md (category: "+strings.Join(categories, ", ")+") holding \"- \" bullet lines. "+
			"If the change is not user-visible, add the \""+optOutLabel+"\" label or a line \"changelog: none\" to the PR description.")
	}
	if b, h := unreleasedBody(baseLog), unreleasedBody(headLog); b != h && !(hasUnreleased(headLog) && movedToFragments(b, h, frags)) {
		problems = append(problems, "this PR edits the \"## [Unreleased]\" section of "+changelogFile+" directly. "+
			"Revert that edit and write the entry to changelog.d/<category>-<slug>.md instead; fragments are assembled into "+changelogFile+" at release time.")
	}
	return problems
}

func optedOut(body string, labels []string) bool {
	for _, l := range labels {
		if l == optOutLabel {
			return true
		}
	}
	body = htmlCommentRE.ReplaceAllString(strings.ReplaceAll(body, "\r\n", "\n"), "")
	for _, line := range strings.Split(body, "\n") {
		if optOutLineRE.MatchString(line) {
			return true
		}
	}
	return false
}

func needsFragment(changes []change) bool {
	for _, c := range changes {
		if strings.HasSuffix(c.path, "_test.go") {
			continue
		}
		for _, d := range codeDirs {
			if strings.HasPrefix(c.path, d) {
				return true
			}
		}
	}
	return false
}

// addsFragment reports whether the PR adds a top-level, well-named fragment.
func addsFragment(changes []change) bool {
	for _, c := range changes {
		if isAddedFragment(c) {
			return true
		}
	}
	return false
}

func isAddedFragment(c change) bool {
	rest, ok := strings.CutPrefix(c.path, fragmentDir)
	if c.status != 'A' || !ok || path.Base(rest) != rest {
		return false
	}
	_, ok = categoryOf(rest)
	return ok
}

// movedToFragments reports whether the Unreleased edit from base to head only
// moves listed entries out into fragments the PR adds: head is base with lines
// removed, and every removed line is blank, a "### " heading, or part of a
// whole "- " bullet (with its indented continuation lines) that appears
// verbatim, as whole lines, in an added fragment. A heading goes only with
// every bullet under it, and at least one bullet must go.
func movedToFragments(base, head string, frags map[string]string) bool {
	var added []string
	for _, content := range frags {
		added = append(added, "\n"+strings.ReplaceAll(content, "\r\n", "\n")+"\n")
	}
	bl, hl := strings.Split(base, "\n"), strings.Split(head, "\n")
	removed := make([]bool, len(bl))
	i := 0
	for _, h := range hl {
		for i < len(bl) && bl[i] != h {
			removed[i] = true
			i++
		}
		if i == len(bl) {
			return false // head has a line base does not: an addition or a reorder
		}
		i++
	}
	for ; i < len(bl); i++ {
		removed[i] = true
	}
	moved, headingGone := false, false
	for i := 0; i < len(bl); i++ {
		line := bl[i]
		if strings.HasPrefix(line, "### ") {
			headingGone = removed[i]
			continue
		}
		if strings.HasPrefix(line, "- ") {
			if headingGone && !removed[i] {
				return false // a kept bullet would fall under another heading
			}
			j := i + 1
			for j < len(bl) && strings.TrimSpace(bl[j]) != "" && strings.TrimLeft(bl[j], " \t") != bl[j] {
				j++
			}
			for k := i + 1; k < j; k++ {
				if removed[k] != removed[i] {
					return false // part of a bullet removed
				}
			}
			if removed[i] {
				bullet := "\n" + strings.Join(bl[i:j], "\n") + "\n"
				if !containsAny(added, bullet) {
					return false
				}
				moved = true
			}
			i = j - 1
			continue
		}
		if removed[i] && strings.TrimSpace(line) != "" {
			return false
		}
	}
	return moved
}

func containsAny(texts []string, sub string) bool {
	for _, t := range texts {
		if strings.Contains(t, sub) {
			return true
		}
	}
	return false
}

// hasUnreleased reports whether log still has its "## [Unreleased]" heading.
func hasUnreleased(log string) bool {
	return strings.Contains("\n"+strings.ReplaceAll(log, "\r\n", "\n")+"\n", "\n"+unreleasedHeading+"\n")
}

// unreleasedBody returns the text under "## [Unreleased]" up to the next
// "## [" heading, or "" when the heading is missing.
func unreleasedBody(log string) string {
	log = strings.ReplaceAll(log, "\r\n", "\n")
	i := strings.Index(log, "\n"+unreleasedHeading+"\n")
	if i < 0 {
		return ""
	}
	rest := log[i+len(unreleasedHeading)+2:]
	if j := strings.Index(rest, "\n## ["); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}
