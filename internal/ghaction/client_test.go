package ghaction

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateCheckRun(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "samestrin", "atcr", CheckRunRequest{
		Name:       "atcr",
		HeadSHA:    "deadbeef",
		Conclusion: "failure",
		Output:     CheckOutput{Title: "atcr — 1 finding", Summary: "s", Text: "t"},
	})
	require.NoError(t, err)
	assert.Equal(t, "/repos/samestrin/atcr/check-runs", gotPath)
	assert.Equal(t, "Bearer tok", gotAuth)
	assert.Equal(t, "completed", gotBody["status"])
	assert.Equal(t, "failure", gotBody["conclusion"])
	assert.Equal(t, "deadbeef", gotBody["head_sha"])
}

func TestCreateCheckRunAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "not accessible")
}
