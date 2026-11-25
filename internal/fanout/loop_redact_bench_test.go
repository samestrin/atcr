package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
)

// BenchmarkToolLoop_RedactRescan measures what redact's re-scan costs a tool
// agent (TD row 53b836f16720b331, markSent). Under redact the loop never marks
// its history guarded, so every Chat scans every message again and a run of N
// turns scans O(N^2) messages; under block a clean message is scanned once,
// which is the best a cache of successful scan results could do. The gap
// between the redact and block lines at the same turns and scanner is the most
// such a cache could save; off is the loop with no scanning at all.
//
// Each turn reads 8 KB of real Go source (this package's loop.go) headed by the
// path it asked for, so no two tool results are the same text, through the real
// client against a local server: the history grows by an assistant tool-call
// turn and one tool result per turn. The history holds no
// credential, so block sends every turn and redact rewrites nothing: the
// numbers are scan cost alone.
//
// The scanner is the one a user gets: the built-in ruleset, or gitleaks when it
// is on PATH (a subprocess per non-empty message). The builtin lines run with
// an empty PATH; the gitleaks lines are skipped when gitleaks is not
// installed.
//
//	go test ./internal/fanout -run '^$' -bench BenchmarkToolLoop_RedactRescan -benchtime 3x
func BenchmarkToolLoop_RedactRescan(b *testing.B) {
	src, err := os.ReadFile("loop.go")
	if err != nil {
		b.Fatal(err)
	}
	const resultBytes = 8 << 10
	if len(src) < resultBytes {
		b.Fatalf("loop.go is %d bytes, want at least %d", len(src), resultBytes)
	}
	result := string(src[:resultBytes])

	gitleaks, _ := exec.LookPath("gitleaks")
	for _, scanner := range []string{"builtin", "gitleaks"} {
		for _, turns := range []int{5, 10, 20} {
			for _, policy := range []string{secretGuardOff, secretGuardBlock, secretGuardRedact} {
				b.Run(fmt.Sprintf("scanner=%s/turns=%d/policy=%s", scanner, turns, policy), func(b *testing.B) {
					switch {
					case scanner == "builtin":
						b.Setenv("PATH", b.TempDir())
					case gitleaks == "":
						b.Skip("gitleaks is not on PATH")
					}
					benchRedactLoop(b, turns, policy, result)
				})
			}
		}
	}
}

// benchRedactLoop runs one tool agent of maxTurns turns per iteration under
// policy, every turn a read_file that returns result.
func benchRedactLoop(b *testing.B, maxTurns int, policy, result string) {
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c := n.Add(1)
		if !strings.Contains(string(body), `"tools"`) {
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"NO FINDINGS"}}]}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,`+
			`"tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f%d.go\"}"}}]}}]}`, c, c)
	}))
	defer srv.Close()
	b.Setenv("ATCR_TEST_KEY", "k")

	d := readDispatcher{body: result}
	a := toolAgent("a", maxTurns, 0)
	a.Invocation = llmclient.Invocation{Model: "m", BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY",
		Prompt: "review this", SecretGuard: policy, AlreadyGuardedMessages: 1}
	e := toolEngine(llmclient.New(llmclient.WithHTTPClient(srv.Client())), d)

	b.ResetTimer()
	for range b.N {
		if r := e.invokeAgent(context.Background(), a); r.Status != StatusOK {
			b.Fatalf("status %s: %v", r.Status, r.Err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(n.Load())/float64(b.N), "requests/op")
}

// readDispatcher answers every tool call with body headed by the call's
// arguments, so each turn's result is a distinct text.
type readDispatcher struct{ body string }

func (d readDispatcher) Execute(_ context.Context, _ string, args json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "// " + string(args) + "\n" + d.body}, nil
}
