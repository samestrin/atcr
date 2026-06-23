package ghaction

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateCheckRunRejectsPlaintextAPIURL(t *testing.T) {
	c := &Client{APIURL: "http://example.com/api/v3", Token: "tok"}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insecure API URL")
}

func TestCreateCheckRunAllowsLoopbackHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.NoError(t, err)
}

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

func TestPostSetsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(gotUA, "atcr"), "User-Agent must identify the app, got: %q", gotUA)
}

func TestClientTimeoutConfigurable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", Timeout: 50 * time.Millisecond}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestPostRetriesTransientFailures(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, attempts, 3)
}

func TestPostRetriesExhausted(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.CreateCheckRun(context.Background(), "o", "r", CheckRunRequest{Name: "atcr", HeadSHA: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestCreateCheckRunWithID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	id, err := c.CreateCheckRunWithID(context.Background(), "o", "r", CheckRunRequest{
		Name: "atcr", HeadSHA: "sha", Conclusion: "success",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
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

// TestPostDo_422ReturnsAPIError pins that postDo wraps non-retriable HTTP errors as
// *APIError so callers can inspect StatusCode (e.g. to treat 422 off-diff as expected).
func TestPostDo_422ReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
	}))
	defer srv.Close()

	c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
	err := c.post(context.Background(), "/repos/o/r/test", map[string]string{"k": "v"})
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr, "non-retriable error must be *APIError so callers can inspect the status code")
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "Validation Failed")
}
