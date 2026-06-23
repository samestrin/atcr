package ghaction

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildInlineComments(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 7, Problem: "boom", Fix: "do x", Evidence: "snippet; fix by opus"},
		{Severity: "LOW", File: "b.go", Line: 3, Problem: "minor", Fix: "", Evidence: "snippet"},
		{Severity: "MEDIUM", File: "c.go", Line: 9, Problem: "mid", Fix: "patch it", Evidence: "no attribution here"},
		{Severity: "HIGH", File: "d.go", Line: 0, Problem: "unanchored", Fix: "x", Evidence: "e"},
		{Severity: "HIGH", File: "", Line: 5, Problem: "nofile", Fix: "x", Evidence: "e"},
		{Severity: "HIGH", File: "e.go", Line: 5, Problem: "", Fix: "x", Evidence: "e"},
	}

	comments := BuildInlineComments(findings)

	// Only the three anchorable findings with non-empty Problem produce comments.
	require.Len(t, comments, 3)

	assert.Equal(t, "a.go", comments[0].Path)
	assert.Equal(t, 7, comments[0].Line)
	assert.Equal(t, "RIGHT", comments[0].Side)
	assert.Contains(t, comments[0].Body, "ATCR found: boom")
	assert.Contains(t, comments[0].Body, "Fix: do x")
	assert.Contains(t, comments[0].Body, "Suggested by: opus")

	// No fix → no Fix clause, no attribution.
	assert.Contains(t, comments[1].Body, "ATCR found: minor")
	assert.NotContains(t, comments[1].Body, "Fix:")
	assert.NotContains(t, comments[1].Body, "Suggested by:")

	// Fix present but no executor token → Fix clause, no attribution.
	assert.Contains(t, comments[2].Body, "Fix: patch it")
	assert.NotContains(t, comments[2].Body, "Suggested by:")
}

func TestCommentBodyDefangsInjection(t *testing.T) {
	// @mentions and #issue-refs in untrusted model output must not render as
	// GitHub notifications or linkified issue references.
	f := reconcile.JSONFinding{
		File:     "foo.go",
		Line:     1,
		Problem:  "reported by @alice or see #123",
		Fix:      "ping @bob and close #456; <!-- secret -->",
		Evidence: "",
	}
	body := commentBody(f)
	assert.Contains(t, body, `\@alice`, "@ in Problem must be backslash-escaped")
	assert.Contains(t, body, `\@bob`, "@ in Fix must be backslash-escaped")
	assert.Contains(t, body, `\#123`, "# in Problem must be backslash-escaped")
	assert.Contains(t, body, `\#456`, "# in Fix must be backslash-escaped")
	assert.NotContains(t, body, "<!--", "HTML comment open sequence must be neutralized")
}

func TestListReviewComments(t *testing.T) {
	existing := []ReviewComment{
		{Path: "a.go", Line: 7, Body: "ATCR found: boom."},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(existing)
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	got, err := c.ListReviewComments(context.Background(), "o", "r", 5)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "a.go", got[0].Path)
	assert.Equal(t, 7, got[0].Line)
	assert.Equal(t, "ATCR found: boom.", got[0].Body)
}

func TestCreatePRReview(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":10}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreatePRReview(context.Background(), "o", "r", 5, PRReviewRequest{
		CommitID: "sha123",
		Comments: []CommentRequest{
			{Path: "a.go", Line: 7, Side: "RIGHT", Body: "hello"},
			{Path: "b.go", Line: 3, Side: "RIGHT", Body: "world"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "/repos/o/r/pulls/5/reviews", gotPath)
	assert.Equal(t, "sha123", gotBody["commit_id"])
	assert.Equal(t, "COMMENT", gotBody["event"])
	comments, ok := gotBody["comments"].([]any)
	require.True(t, ok)
	require.Len(t, comments, 2)
}

func TestCreateReviewComment(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":2}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateReviewComment(context.Background(), "o", "r", 5, "sha123", CommentRequest{
		Path: "a.go", Line: 7, Side: "RIGHT", Body: "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "/repos/o/r/pulls/5/comments", gotPath)
	assert.Equal(t, "sha123", gotBody["commit_id"])
	assert.Equal(t, "a.go", gotBody["path"])
	assert.Equal(t, float64(7), gotBody["line"])
	assert.Equal(t, "hello", gotBody["body"])
}
