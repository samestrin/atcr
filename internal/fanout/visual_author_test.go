package fanout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
)

// fakeSpecAuthor is a scripted felix agent: it records every prompt it is sent
// and returns reply (or err).
type fakeSpecAuthor struct {
	persona string
	reply   string
	err     error
	prompts []string
}

func (f *fakeSpecAuthor) Persona() string { return f.persona }

func (f *fakeSpecAuthor) Complete(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	return f.reply, f.err
}

const specBody = `import { test, expect } from '@playwright/test';

test('home renders', async ({ page }) => {
  await page.goto(process.env.PREVIEW_URL!);
  await expect(page.locator('h1')).toBeVisible();
});`

func newAuthor(reply string) *fakeSpecAuthor {
	return &fakeSpecAuthor{persona: "# {{.AgentName}} — frontend QA reviewer\n\n{{.Payload}}", reply: reply}
}

// e2eEntries lists the names under repo/e2e, or nil when the directory is absent.
func e2eEntries(t *testing.T, repo string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(repo, "e2e"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func TestAuthorVisualSpec_WritesExactlyOneFile(t *testing.T) {
	repo := t.TempDir()
	author := newAuthor("Here is the spec.\n\n```ts\n" + specBody + "\n```\n\nDone.")

	rel, err := AuthorVisualSpec(context.Background(), author, VisualAuthorRequest{
		RepoDir: repo,
		RunID:   "2026-10-27-main-abc123",
		Prompt:  payload.PayloadContext{Payload: "preview: http://127.0.0.1:3000"},
	})
	if err != nil {
		t.Fatalf("AuthorVisualSpec: %v", err)
	}
	want := "e2e/felix-2026-10-27-main-abc123.spec.ts"
	if rel != want {
		t.Fatalf("path = %q, want %q", rel, want)
	}
	if got := e2eEntries(t, repo); len(got) != 1 || got[0] != "felix-2026-10-27-main-abc123.spec.ts" {
		t.Fatalf("e2e entries = %v, want exactly the one spec", got)
	}
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != specBody+"\n" {
		t.Fatalf("spec content = %q, want the fenced body", data)
	}
	if len(author.prompts) != 1 {
		t.Fatalf("model calls = %d, want 1", len(author.prompts))
	}
	if p := author.prompts[0]; !strings.Contains(p, "# felix — frontend QA reviewer") || !strings.Contains(p, "preview: http://127.0.0.1:3000") {
		t.Fatalf("prompt not rendered from the persona: %q", p)
	}
}

func TestAuthorVisualSpec_TypescriptFenceAndFirstBlockWins(t *testing.T) {
	repo := t.TempDir()
	reply := "```json\n{\"not\": \"this\"}\n```\n\n```typescript\n" + specBody + "\n```\n\n```ts\n// second block, ignored\n```\n"
	rel, err := AuthorVisualSpec(context.Background(), newAuthor(reply), VisualAuthorRequest{RepoDir: repo, RunID: "r1"})
	if err != nil {
		t.Fatalf("AuthorVisualSpec: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != specBody+"\n" {
		t.Fatalf("spec content = %q, want the first ts/typescript block", data)
	}
}

func TestAuthorVisualSpec_NoBlockWritesNothing(t *testing.T) {
	for name, reply := range map[string]string{
		"prose only":       "I could not write a spec for this page.",
		"other language":   "```js\nconsole.log(1)\n```",
		"unclosed fence":   "```ts\n" + specBody,
		"empty block":      "```ts\n\n```",
		"ts inside a json": "````json\n```ts\nx\n```\n````",
	} {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			_, err := AuthorVisualSpec(context.Background(), newAuthor(reply), VisualAuthorRequest{RepoDir: repo, RunID: "r1"})
			if !errors.Is(err, ErrNoSpecBlock) {
				t.Fatalf("err = %v, want ErrNoSpecBlock", err)
			}
			if got := e2eEntries(t, repo); len(got) != 0 {
				t.Fatalf("e2e entries = %v, want nothing written", got)
			}
		})
	}
}

func TestAuthorVisualSpec_RejectsUnsafeRunID(t *testing.T) {
	for _, id := range []string{"", "..", "a/../b", "a/b", "v1.2", ".hidden", "UPPER", "a b", "a\\b", strings.Repeat("x", 59)} {
		t.Run(id, func(t *testing.T) {
			repo := t.TempDir()
			author := newAuthor("```ts\n" + specBody + "\n```")
			_, err := AuthorVisualSpec(context.Background(), author, VisualAuthorRequest{RepoDir: repo, RunID: id})
			if !errors.Is(err, ErrInvalidSpecName) {
				t.Fatalf("err = %v, want ErrInvalidSpecName", err)
			}
			if len(author.prompts) != 0 {
				t.Fatalf("model called %d times for a rejected run id", len(author.prompts))
			}
			if got := e2eEntries(t, repo); len(got) != 0 {
				t.Fatalf("e2e entries = %v, want nothing written", got)
			}
		})
	}
	// The longest legal name is 64 characters: "felix-" plus 58.
	repo := t.TempDir()
	if _, err := AuthorVisualSpec(context.Background(), newAuthor("```ts\nx\n```"), VisualAuthorRequest{RepoDir: repo, RunID: strings.Repeat("x", 58)}); err != nil {
		t.Fatalf("58-char run id: %v", err)
	}
}

func TestAuthorVisualSpec_RefusesExistingFile(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "e2e")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "felix-r1.spec.ts")
	const theirs = "// the operator's own spec\n"
	if err := os.WriteFile(existing, []byte(theirs), 0o644); err != nil {
		t.Fatal(err)
	}
	author := newAuthor("```ts\n" + specBody + "\n```")

	_, err := AuthorVisualSpec(context.Background(), author, VisualAuthorRequest{RepoDir: repo, RunID: "r1"})
	if !errors.Is(err, ErrSpecExists) {
		t.Fatalf("err = %v, want ErrSpecExists", err)
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != theirs {
		t.Fatalf("existing spec changed to %q", data)
	}
	if len(author.prompts) != 0 {
		t.Fatalf("model called %d times although the spec path is taken", len(author.prompts))
	}
	if got := e2eEntries(t, repo); len(got) != 1 {
		t.Fatalf("e2e entries = %v, want only the existing spec", got)
	}
}

// A file that appears at the spec path while the model is answering is still
// refused, never overwritten.
func TestAuthorVisualSpec_RefusesFileCreatedDuringCall(t *testing.T) {
	repo := t.TempDir()
	existing := filepath.Join(repo, "e2e", "felix-r1.spec.ts")
	const theirs = "// written mid-call\n"
	author := &racingAuthor{fakeSpecAuthor: newAuthor("```ts\n" + specBody + "\n```"), path: existing, data: theirs}

	_, err := AuthorVisualSpec(context.Background(), author, VisualAuthorRequest{RepoDir: repo, RunID: "r1"})
	if !errors.Is(err, ErrSpecExists) {
		t.Fatalf("err = %v, want ErrSpecExists", err)
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != theirs {
		t.Fatalf("existing spec changed to %q", data)
	}
}

type racingAuthor struct {
	*fakeSpecAuthor
	path, data string
}

func (r *racingAuthor) Complete(ctx context.Context, prompt string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(r.path, []byte(r.data), 0o644); err != nil {
		return "", err
	}
	return r.fakeSpecAuthor.Complete(ctx, prompt)
}

// e2e as a symlink could point outside the repo under review; the write is
// refused rather than followed.
func TestAuthorVisualSpec_RefusesSymlinkedE2EDir(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo, "e2e")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	_, err := AuthorVisualSpec(context.Background(), newAuthor("```ts\n"+specBody+"\n```"), VisualAuthorRequest{RepoDir: repo, RunID: "r1"})
	if err == nil {
		t.Fatal("want an error for a symlinked e2e directory")
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("wrote through the symlink: %v", ents)
	}
}

func TestAuthorVisualSpec_ModelErrorWritesNothing(t *testing.T) {
	repo := t.TempDir()
	author := newAuthor("")
	author.err = errors.New("provider down")
	_, err := AuthorVisualSpec(context.Background(), author, VisualAuthorRequest{RepoDir: repo, RunID: "r1"})
	if err == nil || !strings.Contains(err.Error(), "provider down") {
		t.Fatalf("err = %v, want the provider error", err)
	}
	if got := e2eEntries(t, repo); len(got) != 0 {
		t.Fatalf("e2e entries = %v, want nothing written", got)
	}
}

func TestAuthorVisualSpec_EmptyPersonaIsAnError(t *testing.T) {
	author := newAuthor("```ts\nx\n```")
	author.persona = "  "
	if _, err := AuthorVisualSpec(context.Background(), author, VisualAuthorRequest{RepoDir: t.TempDir(), RunID: "r1"}); err == nil {
		t.Fatal("want an error for an empty persona")
	}
	if len(author.prompts) != 0 {
		t.Fatal("model called with an empty persona")
	}
}

func TestAuthorVisualSpec_BadPersonaTemplateIsAnError(t *testing.T) {
	author := newAuthor("```ts\nx\n```")
	author.persona = "{{.NoSuchField}}"
	repo := t.TempDir()
	if _, err := AuthorVisualSpec(context.Background(), author, VisualAuthorRequest{RepoDir: repo, RunID: "r1"}); err == nil {
		t.Fatal("want a render error")
	}
	if len(author.prompts) != 0 {
		t.Fatal("model called with an unrenderable persona")
	}
	if got := e2eEntries(t, repo); len(got) != 0 {
		t.Fatalf("e2e entries = %v, want nothing written", got)
	}
}

// An inline-code line such as "```ts` is not a spec" is not a fence opener.
func TestExtractSpecBlock_InlineBackticksAreNotAFence(t *testing.T) {
	if _, ok := extractSpecBlock("```ts` is the language\nnope\n```"); ok {
		t.Fatal("inline backticks opened a fence")
	}
}
