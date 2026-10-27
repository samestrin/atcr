package fanout

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
)

// The vision agent is selected only when it declares supports_vision and is
// on a loopback host, or with allowCloud on any host (Epic 36.0.2 T3).
func TestSelectVisionAgent(t *testing.T) {
	t.Parallel()
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{
			"local":   {APIKeyEnv: "K", BaseURL: "http://127.0.0.1:11434/v1"},
			"cloud":   {APIKeyEnv: "K", BaseURL: "https://api.example.com/v1"},
			"default": {APIKeyEnv: "K"},
		},
		Agents: map[string]registry.AgentConfig{
			"eye":     {Provider: "local", Model: "m", SupportsVision: true},
			"remote":  {Provider: "cloud", Model: "m", SupportsVision: true},
			"blank":   {Provider: "default", Model: "m", SupportsVision: true},
			"textual": {Provider: "local", Model: "m"},
			"orphan":  {Provider: "gone", Model: "m", SupportsVision: true},
		},
	}
	cases := []struct {
		name       string
		agent      string
		allowCloud bool
		wantErr    error
		loopback   bool
	}{
		{"loopback vision agent", "eye", false, nil, true},
		{"loopback vision agent with allowCloud", "eye", true, nil, true},
		{"non-loopback refused", "remote", false, ErrVisionAgentRefused, false},
		{"non-loopback with allowCloud", "remote", true, nil, false},
		{"no supports_vision", "textual", false, ErrVisionAgentRefused, false},
		{"no supports_vision with allowCloud", "textual", true, ErrVisionAgentRefused, false},
		{"empty base_url refused", "blank", false, ErrVisionAgentRefused, false},
		{"empty base_url with allowCloud", "blank", true, nil, false},
		{"unknown agent", "nobody", true, ErrVisionAgentRefused, false},
		{"unknown provider", "orphan", true, ErrVisionAgentRefused, false},
		{"unset key", "", true, ErrVisionAgentUnset, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			settings := registry.Settings{VisualReview: registry.VisualReview{Agent: tc.agent}}
			got, err := SelectVisionAgent(reg, settings, tc.allowCloud)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Empty(t, got.Name)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.agent, got.Name)
			assert.True(t, got.Agent.SupportsVision)
			assert.Equal(t, tc.loopback, got.Loopback)
		})
	}
}

// A nil registry refuses the agent rather than panicking, like the
// threat-map mapper's no-registry guard.
func TestSelectVisionAgent_NoRegistry(t *testing.T) {
	t.Parallel()
	settings := registry.Settings{VisualReview: registry.VisualReview{Agent: "eye"}}
	got, err := SelectVisionAgent(nil, settings, true)
	require.ErrorIs(t, err, ErrVisionAgentRefused)
	assert.Empty(t, got.Name)
}

// visionServer is a fake chat-completions endpoint: it records each request
// body and answers with reply as the assistant content.
type visionServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	bodies [][]byte
}

func newVisionServer(t *testing.T, reply string) *visionServer {
	t.Helper()
	vs := &visionServer{}
	vs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vs.mu.Lock()
		vs.bodies = append(vs.bodies, body)
		vs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": reply}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(vs.srv.Close)
	return vs
}

func (vs *visionServer) requests() [][]byte {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return append([][]byte(nil), vs.bodies...)
}

// visionAgentAt is a supports_vision agent served by baseURL.
func visionAgentAt(t *testing.T, baseURL string, loopback bool) VisionAgent {
	t.Helper()
	t.Setenv("ATCR_TEST_VISION_KEY", "test-key")
	return VisionAgent{
		Name:     "eye",
		Agent:    registry.AgentConfig{Provider: "local", Model: "vision-model", SupportsVision: true},
		Provider: registry.Provider{APIKeyEnv: "ATCR_TEST_VISION_KEY", BaseURL: baseURL},
		Loopback: loopback,
	}
}

const visualSpecPath = "e2e/felix-run1.spec.ts"

// visualReply is a model reply with one out-of-scope finding on the spec.
var visualReply = "```json\n" + `[{"severity": "HIGH", "file_line": "` + visualSpecPath + `:12", "problem": "Pay now button clipped", "fix": "Remove overflow hidden", "category": "out-of-scope", "est_minutes": 10, "evidence": "checkout screenshot: the button is cut off"}]` + "\n```\n"

func visualRequest(console string) VisualReviewRequest {
	return VisualReviewRequest{
		Prompt:   "Review the screenshots.",
		SpecPath: visualSpecPath,
		Browser: BrowserResult{
			Screenshots: []BrowserScreenshot{
				{Name: "checkout", Path: "test-results/checkout.png", Data: []byte("png-one")},
				{Name: "cart", Path: "test-results/cart.jpg", Data: []byte("jpg-two")},
			},
			Console:         []ConsoleEntry{{Level: "error", Text: console, Location: "app.js:3"}},
			NetworkFailures: []NetworkFailure{{URL: "http://localhost:3000/api/cart", Method: "GET", Status: 500}},
		},
		// The patch touched only src/app.ts, so the spec path is absent from it.
		Changed: payload.ChangedLines{"src/app.ts": {Ranges: []payload.LineRange{{Start: 1, End: 5}}}},
	}
}

// requestParts decodes the content parts of the single user message of body.
func requestParts(t *testing.T, body []byte) []llmclient.ContentPart {
	t.Helper()
	var req struct {
		Messages []struct {
			Role    string                  `json:"role"`
			Content []llmclient.ContentPart `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &req))
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "user", req.Messages[0].Role)
	return req.Messages[0].Content
}

func partText(parts []llmclient.ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// The request carries one image_url part per screenshot, as a data URL, and
// the out-of-scope finding on the spec survives the grounding gate although
// the spec is not in the patch (Epic 36.0.2 T4).
func TestRunVisualReview_ImagePartPerScreenshot(t *testing.T) {
	countingScanner(t, nil)
	vs := newVisionServer(t, visualReply)
	agent := visionAgentAt(t, vs.srv.URL, true)

	got, err := RunVisualReview(context.Background(), llmclient.New(), agent, registry.Settings{SecretGuard: secretGuardBlock}, visualRequest("TypeError: x is undefined"))
	require.NoError(t, err)

	reqs := vs.requests()
	require.Len(t, reqs, 1)
	parts := requestParts(t, reqs[0])
	var images []string
	for _, p := range parts {
		if p.Type == "image_url" {
			require.NotNil(t, p.ImageURL)
			images = append(images, p.ImageURL.URL)
		}
	}
	assert.Equal(t, []string{
		"data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-one")),
		"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("jpg-two")),
	}, images)
	text := partText(parts)
	assert.Contains(t, text, "Review the screenshots.")
	assert.Contains(t, text, visualSpecPath)
	assert.Contains(t, text, "TypeError: x is undefined")
	assert.Contains(t, text, "http://localhost:3000/api/cart")

	require.Len(t, got.Findings, 1)
	assert.Equal(t, visualSpecPath, got.Findings[0].File)
	assert.Equal(t, "eye", got.Findings[0].Reviewer)
	assert.Equal(t, 0, got.Ungrounded)
	assert.Equal(t, "eye", got.Result.Agent)
	assert.Equal(t, StatusOK, got.Result.Status)
	assert.False(t, got.Result.ImagesSentOffMachine)
}

// secret_guard: block with a token in the console text sends nothing.
func TestRunVisualReview_BlockSendsNothing(t *testing.T) {
	countingScanner(t, nil)
	vs := newVisionServer(t, visualReply)
	agent := visionAgentAt(t, vs.srv.URL, true)

	_, err := RunVisualReview(context.Background(), llmclient.New(), agent, registry.Settings{SecretGuard: secretGuardBlock}, visualRequest("token="+guardAWSKey))
	require.ErrorIs(t, err, ErrSecretGuardBlocked)
	assert.NotContains(t, err.Error(), guardAWSKey)
	assert.Empty(t, vs.requests(), "no request is sent")
}

// secret_guard: redact sends the console text with the token replaced.
func TestRunVisualReview_RedactSendsRedactedText(t *testing.T) {
	countingScanner(t, nil)
	vs := newVisionServer(t, visualReply)
	agent := visionAgentAt(t, vs.srv.URL, true)

	_, err := RunVisualReview(context.Background(), llmclient.New(), agent, registry.Settings{SecretGuard: secretGuardRedact}, visualRequest("token="+guardAWSKey))
	require.NoError(t, err)
	reqs := vs.requests()
	require.Len(t, reqs, 1)
	assert.NotContains(t, string(reqs[0]), guardAWSKey)
	assert.Contains(t, partText(requestParts(t, reqs[0])), "token="+payload.SecretPlaceholder)
}

// A token in a failed request or a failed test's error is guarded too: every
// captured text reaches the model only through the guard.
func TestRunVisualReview_BlockCoversNetworkAndTestText(t *testing.T) {
	cases := map[string]func(*VisualReviewRequest){
		"network url": func(r *VisualReviewRequest) {
			r.Browser.NetworkFailures[0].URL = "http://localhost/api?key=" + guardAWSKey
		},
		"network error": func(r *VisualReviewRequest) { r.Browser.NetworkFailures[0].Error = guardAWSKey },
		"failed test": func(r *VisualReviewRequest) {
			r.Browser.FailedTests = []BrowserTestFailure{{Title: "t", Error: "expected " + guardAWSKey}}
		},
		"prompt": func(r *VisualReviewRequest) { r.Prompt += guardAWSKey },
		"screenshot name": func(r *VisualReviewRequest) {
			r.Browser.Screenshots[1].Name = "cart\n" + guardAWSKey
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			countingScanner(t, nil)
			vs := newVisionServer(t, visualReply)
			agent := visionAgentAt(t, vs.srv.URL, true)
			req := visualRequest("clean")
			mutate(&req)
			_, err := RunVisualReview(context.Background(), llmclient.New(), agent, registry.Settings{SecretGuard: secretGuardBlock}, req)
			require.ErrorIs(t, err, ErrSecretGuardBlocked)
			assert.Empty(t, vs.requests())
		})
	}
}

// A cloud vision agent (admitted by allowCloud) records in status.json that
// the screenshots left the machine; a loopback one leaves the key out.
func TestRunVisualReview_CloudRecordedInStatus(t *testing.T) {
	countingScanner(t, nil)
	vs := newVisionServer(t, visualReply)
	agent := visionAgentAt(t, vs.srv.URL, false)

	got, err := RunVisualReview(context.Background(), llmclient.New(), agent, registry.Settings{}, visualRequest("ok"))
	require.NoError(t, err)
	require.True(t, got.Result.ImagesSentOffMachine)

	st := statusFor(got.Result, findingsResult{Findings: got.Findings})
	raw, err := json.Marshal(st)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"images_sent_off_machine":true`)

	local, err := json.Marshal(statusFor(Result{Agent: "eye", Status: StatusOK}, findingsResult{}))
	require.NoError(t, err)
	assert.NotContains(t, string(local), "images_sent_off_machine")
}

// A finding with another category on a file absent from the patch is dropped
// by the grounding gate, while out-of-scope keeps it.
func TestRunVisualReview_GroundingKeepsOnlyOutOfScope(t *testing.T) {
	countingScanner(t, nil)
	reply := strings.Replace(visualReply, `"out-of-scope"`, `"layout"`, 1)
	vs := newVisionServer(t, reply)
	agent := visionAgentAt(t, vs.srv.URL, true)

	got, err := RunVisualReview(context.Background(), llmclient.New(), agent, registry.Settings{}, visualRequest("ok"))
	require.NoError(t, err)
	assert.Empty(t, got.Findings)
	assert.Equal(t, 1, got.Ungrounded)
}

// With no screenshot bytes there is nothing to review, so no request is sent.
func TestRunVisualReview_NoScreenshots(t *testing.T) {
	countingScanner(t, nil)
	vs := newVisionServer(t, visualReply)
	agent := visionAgentAt(t, vs.srv.URL, true)
	req := visualRequest("ok")
	req.Browser.Screenshots = []BrowserScreenshot{{Name: "lost", Path: "x.png"}}

	_, err := RunVisualReview(context.Background(), llmclient.New(), agent, registry.Settings{}, req)
	require.ErrorIs(t, err, ErrNoScreenshots)
	assert.Empty(t, vs.requests())
}

// A provider failure is returned, wrapped with the agent name.
func TestRunVisualReview_ProviderError(t *testing.T) {
	countingScanner(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	agent := visionAgentAt(t, srv.URL, true)

	_, err := RunVisualReview(context.Background(), llmclient.New(llmclient.WithRetry(0, time.Millisecond, 1)), agent, registry.Settings{}, visualRequest("ok"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `vision agent "eye"`)
}

// A screenshot whose bytes were not returned gets no image part, and the
// others are still sent, each after its one-line label.
func TestRunVisualReview_SkipsScreenshotWithoutData(t *testing.T) {
	countingScanner(t, nil)
	vs := newVisionServer(t, "NO FINDINGS")
	agent := visionAgentAt(t, vs.srv.URL, true)
	req := visualRequest("ok")
	req.Browser.Screenshots = append([]BrowserScreenshot{{Name: "lost", Path: "lost.png"}}, req.Browser.Screenshots...)
	req.Browser.Screenshots[2].Name = "cart\nview"

	got, err := RunVisualReview(context.Background(), llmclient.New(), agent, registry.Settings{}, req)
	require.NoError(t, err)
	assert.Empty(t, got.Findings)
	reqs := vs.requests()
	require.Len(t, reqs, 1)
	parts := requestParts(t, reqs[0])
	require.Len(t, parts, 5)
	assert.Equal(t, "Screenshot 1: checkout (test-results/checkout.png)", parts[0].Text)
	assert.Equal(t, "image_url", parts[1].Type)
	assert.Equal(t, "Screenshot 2: cart view (test-results/cart.jpg)", parts[2].Text)
	assert.Equal(t, "image_url", parts[3].Type)
	assert.NotContains(t, parts[4].Text, "lost")
}

// A screenshot with no recognised extension is typed from its bytes, else PNG.
func TestScreenshotMIME(t *testing.T) {
	t.Parallel()
	gif := []byte("GIF89a\x01\x00\x01\x00")
	assert.Equal(t, "image/webp", screenshotMIME(BrowserScreenshot{Path: "a.WEBP", Data: []byte("x")}))
	assert.Equal(t, "image/gif", screenshotMIME(BrowserScreenshot{Path: "shot", Data: gif}))
	assert.Equal(t, "image/png", screenshotMIME(BrowserScreenshot{Path: "shot.bin", Data: []byte("x")}))
}
