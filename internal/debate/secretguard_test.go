package debate

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
)

// debateAWSKey is split so the source holds no literal credential.
const debateAWSKey = "AKIA" + "QYLPMN5HHHFPZAM2"

// Epic 106.3 T2: the registry's secret_guard reaches every seat's Invocation
// through debateOne, RunDebate, runTurn and driveSeat, and an unset policy is
// warn, the registry default. driveSeat guards the seat's prompt before it is
// built (so a single-shot seat, which llmclient.Chat never sees, is covered),
// so the prompt is counted as already guarded and the tool loop scans only the
// messages it adds.
func TestDebateOne_SeatsCarryRegistrySecretGuard(t *testing.T) {
	for setting, want := range map[string]string{
		"":       registry.DefaultSecretGuard,
		"off":    "off",
		"warn":   "warn",
		"redact": "redact",
		"block":  "block",
	} {
		reg := debateRoster()
		reg.SecretGuard = setting
		cc := &fakeChatCompleter{turns: []chatTurn{
			{content: "proposer defends"},
			{content: "challenger attacks"},
			{content: `{"outcome":"uphold","settled_severity":"HIGH"}`},
		}}
		dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
		_, err := runDebate(context.Background(), dir, reg, Options{}, harness(cc))
		require.NoError(t, err)
		invs := cc.invocations()
		require.Len(t, invs, 3, "setting %q: one Chat call per seat", setting)
		for i, inv := range invs {
			assert.Equal(t, want, inv.SecretGuard, "setting %q: seat %d", setting, i+1)
			assert.Equal(t, 1, inv.AlreadyGuardedMessages, "setting %q: seat %d", setting, i+1)
		}
	}
}

// seatServer is a chat endpoint that records each request body and answers the
// n-th request with reply(n).
type seatServer struct {
	mu     sync.Mutex
	bodies []string
}

func (s *seatServer) start(t *testing.T, reply func(n int) string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(b))
		n := len(s.bodies)
		s.mu.Unlock()
		content, _ := json.Marshal(reply(n))
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":` + string(content) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (s *seatServer) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// seatShape is how a debate seat reaches its endpoint: the tool loop (Chat),
// or the single-shot degrade (CompleteWithMeta) a seat without function
// calling, or one with no dispatcher wired, is sent down.
type seatShape struct {
	name     string
	fc       bool
	dispatch bool
}

var seatShapes = []seatShape{
	{name: "tool loop", fc: true, dispatch: true},
	{name: "no function calling", fc: false, dispatch: true},
	{name: "no dispatcher", fc: true, dispatch: false},
}

// realCast is the three-seat cast of debateRoster, routed to baseURL, with
// every seat's function calling set by fc.
func realCast(t *testing.T, baseURL string, fc bool) Cast {
	t.Helper()
	reg := debateRoster()
	reg.Providers["p"] = registry.Provider{APIKeyEnv: "ATCR_DEBATE_TEST_KEY", BaseURL: baseURL}
	for n, a := range reg.Agents {
		a.SupportsFC = fc
		reg.Agents[n] = a
	}
	t.Setenv("ATCR_DEBATE_TEST_KEY", "k")
	cast, ok, reason := CastRoles(reg, debateItem(), Config{})
	require.True(t, ok, reason)
	return cast
}

// captureStderr returns what fn writes to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

// runRealDebate drives one debate through the real llmclient under policy and
// returns the record and what was logged or written to stderr.
func runRealDebate(t *testing.T, policy string, item reconcile.DisagreementItem, cast Cast, shape seatShape) (Record, string) {
	t.Helper()
	var logs bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
	var disp Dispatcher
	if shape.dispatch {
		disp = &fakeDispatcher{}
	}
	var rec Record
	stderr := captureStderr(t, func() {
		rec = RunDebate(ctx, item, cast, policy, llmclient.New(), disp, nil)
	})
	return rec, logs.String() + stderr
}

func ruling(int) string { return `{"outcome":"uphold","settled_severity":"HIGH"}` }

// Through the real client: a credential in the debated finding is in every
// seat's prompt, and each seat's request is warned, redacted or blocked as the
// policy says.
func TestRunDebate_SecretInEverySeatPromptFollowsPolicy(t *testing.T) {
	secretItem := func() reconcile.DisagreementItem {
		item := debateItem()
		item.Problem = "hard-coded key " + debateAWSKey
		return item
	}

	for _, shape := range seatShapes {
		t.Run(shape.name+": warn sends it and warns", func(t *testing.T) {
			srv := &seatServer{}
			rec, out := runRealDebate(t, "warn", secretItem(), realCast(t, srv.start(t, ruling), shape.fc), shape)
			assert.Empty(t, rec.Halted)
			bodies := srv.sent()
			require.Len(t, bodies, 3, "proposer, challenger and judge are each sent")
			for i, b := range bodies {
				assert.Contains(t, b, debateAWSKey, "seat %d: warn sends the prompt unchanged", i+1)
			}
			assert.Contains(t, out, "secret_guard: warn")
			assert.NotContains(t, out, debateAWSKey, "a warning never holds the secret")
		})

		t.Run(shape.name+": redact sends the placeholder", func(t *testing.T) {
			srv := &seatServer{}
			rec, _ := runRealDebate(t, "redact", secretItem(), realCast(t, srv.start(t, ruling), shape.fc), shape)
			assert.Empty(t, rec.Halted)
			bodies := srv.sent()
			require.Len(t, bodies, 3)
			for i, b := range bodies {
				assert.NotContains(t, b, debateAWSKey, "seat %d", i+1)
				assert.Contains(t, b, payload.SecretPlaceholder, "seat %d", i+1)
			}
		})

		t.Run(shape.name+": block sends nothing and halts every seat", func(t *testing.T) {
			srv := &seatServer{}
			cast := realCast(t, srv.start(t, ruling), shape.fc)
			rec, _ := runRealDebate(t, "block", secretItem(), cast, shape)
			assert.Empty(t, srv.sent(), "no seat's request may leave")
			assert.Equal(t, []string{cast.Proposer.Label, cast.Challenger.Label, cast.Judge.Label}, rec.Halted)
			assert.Empty(t, rec.JudgeRaw)
		})
	}
}

// Through the real client: a credential that first appears in the proposer's
// statement reaches only the challenger's and the judge's prompts, and block
// stops those two seats' requests while the proposer's clean one was sent.
func TestRunDebate_SecretInProposerStatementBlocksLaterSeats(t *testing.T) {
	for _, shape := range seatShapes {
		t.Run(shape.name, func(t *testing.T) {
			srv := &seatServer{}
			cast := realCast(t, srv.start(t, func(n int) string {
				if n == 1 {
					return "the config holds " + debateAWSKey
				}
				return ruling(n)
			}), shape.fc)
			rec, _ := runRealDebate(t, "block", debateItem(), cast, shape)
			assert.Len(t, srv.sent(), 1, "only the proposer's clean request is sent")
			assert.Equal(t, []string{cast.Challenger.Label, cast.Judge.Label}, rec.Halted)
		})
	}
}
