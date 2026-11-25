package llmclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chatAWSKey is split so the source holds no literal credential.
const chatAWSKey = "AKIA" + "QYLPMN5HHHFPZAM2"

// guardServer records each request body and answers a plain stop turn.
func guardServer(t *testing.T) (*httptest.Server, *atomic.Int32, *string) {
	t.Helper()
	var hits atomic.Int32
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TEST_KEY", testKey)
	return srv, &hits, &body
}

// stubScanner swaps scanSecrets for a fake that reports every chatAWSKey as an
// aws-access-token hit, fails with err when set, and records each text it is
// handed.
func stubScanner(t *testing.T, err error) *[]string {
	t.Helper()
	var scanned []string
	old := scanSecrets
	scanSecrets = func(_ context.Context, text string, _ *slog.Logger) ([]payload.SecretHit, error) {
		scanned = append(scanned, text)
		var hits []payload.SecretHit
		for off := 0; ; {
			i := strings.Index(text[off:], chatAWSKey)
			if i < 0 {
				break
			}
			start := off + i
			hits = append(hits, payload.SecretHit{Kind: "aws-access-token", Start: start, End: start + len(chatAWSKey)})
			off = start + len(chatAWSKey)
		}
		return hits, err
	}
	t.Cleanup(func() { scanSecrets = old })
	return &scanned
}

func strp(s string) *string { return &s }

func guardInv(srv *httptest.Server, policy string, guarded int) Invocation {
	return Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1", SecretGuard: policy, AlreadyGuardedMessages: guarded}
}

func TestChat_SecretGuardDefaultsToOff(t *testing.T) {
	srv, hits, body := guardServer(t)
	scanned := stubScanner(t, nil)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "", 0),
		[]Message{{Role: "user", Content: strp("key " + chatAWSKey)}}, nil)
	require.NoError(t, err)
	assert.Empty(t, *scanned, "an unset policy is off: nothing is scanned")
	assert.EqualValues(t, 1, hits.Load())
	assert.Contains(t, *body, chatAWSKey)
}

func TestChat_SecretGuardOffNeverScans(t *testing.T) {
	srv, hits, _ := guardServer(t)
	scanned := stubScanner(t, nil)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "off", 0),
		[]Message{{Role: "user", Content: strp("key " + chatAWSKey)}}, nil)
	require.NoError(t, err)
	assert.Empty(t, *scanned)
	assert.EqualValues(t, 1, hits.Load())
}

func TestChat_SecretGuardBlockRefusesBeforeSending(t *testing.T) {
	srv, hits, _ := guardServer(t)
	stubScanner(t, nil)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "block", 0),
		[]Message{{Role: "user", Content: strp("clean")}, {Role: "tool", Content: strp("found " + chatAWSKey)}}, nil)
	require.ErrorIs(t, err, ErrSecretGuardBlocked)
	assert.Contains(t, err.Error(), "aws-access-token")
	assert.Contains(t, err.Error(), "message 1")
	assert.NotContains(t, err.Error(), chatAWSKey, "the error never carries the secret")
	assert.EqualValues(t, 0, hits.Load(), "no request is sent")
}

func TestChat_SecretGuardBlockFailsClosedOnScanError(t *testing.T) {
	srv, hits, _ := guardServer(t)
	stubScanner(t, payload.ErrSecretScanFailed)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "block", 0),
		[]Message{{Role: "user", Content: strp("clean")}}, nil)
	require.ErrorIs(t, err, ErrSecretGuardBlocked)
	require.ErrorIs(t, err, payload.ErrSecretScanFailed)
	assert.EqualValues(t, 0, hits.Load())
}

func TestChat_SecretGuardBlockSendsCleanMessages(t *testing.T) {
	srv, hits, _ := guardServer(t)
	scanned := stubScanner(t, nil)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "block", 0),
		[]Message{{Role: "user", Content: strp("clean")}}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"clean"}, *scanned)
	assert.EqualValues(t, 1, hits.Load())
}

func TestChat_SecretGuardSkipsAlreadyGuardedMessages(t *testing.T) {
	srv, hits, _ := guardServer(t)
	scanned := stubScanner(t, nil)

	// Message zero holds a key but is declared already guarded, so block
	// neither scans nor refuses it.
	msgs := []Message{
		{Role: "user", Content: strp("prompt " + chatAWSKey)},
		{Role: "tool", Content: strp("tool result")},
	}
	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "block", 1), msgs, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"tool result"}, *scanned)
	assert.EqualValues(t, 1, hits.Load())
}

func TestChat_SecretGuardAllMessagesGuarded(t *testing.T) {
	srv, hits, _ := guardServer(t)
	scanned := stubScanner(t, nil)

	msgs := []Message{{Role: "user", Content: strp("prompt " + chatAWSKey)}}
	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "block", 1), msgs, nil)
	require.NoError(t, err)
	assert.Empty(t, *scanned)
	assert.EqualValues(t, 1, hits.Load())
}

func TestChat_SecretGuardRejectsBadGuardedCount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		policy  string
		guarded int
	}{
		{"negative", "warn", -1},
		{"above len", "warn", 3},
		{"negative under off", "off", -1},
		{"above len under off", "", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, hits, _ := guardServer(t)
			stubScanner(t, nil)
			_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, tc.policy, tc.guarded),
				[]Message{{Role: "user", Content: strp("a")}, {Role: "user", Content: strp("b")}}, nil)
			require.ErrorIs(t, err, ErrAlreadyGuardedMessages)
			assert.EqualValues(t, 0, hits.Load())
		})
	}
}

func TestChat_SecretGuardRejectsUnknownPolicy(t *testing.T) {
	srv, hits, _ := guardServer(t)
	stubScanner(t, nil)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "blcok", 0),
		[]Message{{Role: "user", Content: strp("a")}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blcok")
	assert.EqualValues(t, 0, hits.Load())
}

func TestChat_SecretGuardWarnSendsUnchangedAndLogs(t *testing.T) {
	srv, hits, body := guardServer(t)
	stubScanner(t, nil)
	var logs bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))

	_, err := fastRetry(srv.Client()).Chat(ctx, guardInv(srv, "warn", 0),
		[]Message{{Role: "user", Content: strp("key " + chatAWSKey)}}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 1, hits.Load())
	assert.Contains(t, *body, chatAWSKey, "warn sends the message unchanged")
	assert.Contains(t, logs.String(), "aws-access-token")
	assert.Contains(t, logs.String(), "secret_guard: warn")
	assert.NotContains(t, logs.String(), chatAWSKey)
}

func TestChat_SecretGuardWarnLogsScanError(t *testing.T) {
	srv, hits, _ := guardServer(t)
	stubScanner(t, payload.ErrSecretScanFailed)
	var logs bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))

	_, err := fastRetry(srv.Client()).Chat(ctx, guardInv(srv, "warn", 0),
		[]Message{{Role: "user", Content: strp("clean")}}, nil)
	require.NoError(t, err, "only block fails closed on a scan error")
	assert.EqualValues(t, 1, hits.Load())
	assert.Contains(t, logs.String(), "secret scan")
}

func TestChat_SecretGuardRedactRewritesACopy(t *testing.T) {
	srv, hits, body := guardServer(t)
	scanned := stubScanner(t, nil)

	content := "key " + chatAWSKey
	msgs := []Message{
		{Role: "user", Content: strp("prompt " + chatAWSKey)}, // already guarded
		{Role: "tool", Content: &content},
		{Role: "user", Parts: []ContentPart{TextPart("part " + chatAWSKey), ImageURLPart("data:image/png;base64,AAAA")}},
	}
	inv := guardInv(srv, "redact", 1)
	inv.SupportsVision = true
	var logs bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
	_, err := fastRetry(srv.Client()).Chat(ctx, inv, msgs, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 1, hits.Load())

	assert.Equal(t, []string{content, "part " + chatAWSKey}, *scanned, "Content and text Parts are scanned; image parts and guarded messages are not")
	assert.Equal(t, 1, strings.Count(*body, chatAWSKey), "only the already-guarded message keeps its text")
	assert.Equal(t, 2, strings.Count(*body, payload.SecretPlaceholder))
	assert.Contains(t, logs.String(), "2 secret(s) [aws-access-token] in message 1, 2")
	assert.Contains(t, logs.String(), "secret_guard: redact")
	assert.NotContains(t, logs.String(), chatAWSKey)

	// The caller's history is untouched.
	assert.Equal(t, "key "+chatAWSKey, *msgs[1].Content)
	assert.Same(t, &content, msgs[1].Content)
	assert.Equal(t, "part "+chatAWSKey, msgs[2].Parts[0].Text)
}

// The default scanner is payload.ScanPayloadContext: with no gitleaks on PATH
// its built-in ruleset catches the key, and block refuses the turn.
func TestChat_SecretGuardUsesThePayloadScanner(t *testing.T) {
	srv, hits, _ := guardServer(t)
	t.Setenv("PATH", t.TempDir())

	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "block", 0),
		[]Message{{Role: "user", Content: strp("aws_access_key_id = " + chatAWSKey)}}, nil)
	require.True(t, errors.Is(err, ErrSecretGuardBlocked), "got %v", err)
	assert.EqualValues(t, 0, hits.Load())
}

// An empty Content or text Part has nothing to scan, so the scanner is never
// handed one.
func TestChat_SecretGuardSkipsEmptyText(t *testing.T) {
	srv, hits, _ := guardServer(t)
	scanned := stubScanner(t, nil)

	msgs := []Message{
		{Role: "user", Content: strp("")},
		{Role: "user", Parts: []ContentPart{TextPart(""), TextPart("clean")}},
	}
	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "block", 0), msgs, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 1, hits.Load())
	assert.Equal(t, []string{"clean"}, *scanned, "empty texts are not scanned")
}

// Hits in two text Parts of one message name that message once.
func TestChat_SecretGuardListsEachMessageOnce(t *testing.T) {
	srv, hits, _ := guardServer(t)
	stubScanner(t, nil)

	msgs := []Message{
		{Role: "user", Content: strp("clean")},
		{Role: "user", Parts: []ContentPart{TextPart("a " + chatAWSKey), TextPart("b " + chatAWSKey)}},
	}
	_, err := fastRetry(srv.Client()).Chat(context.Background(), guardInv(srv, "block", 0), msgs, nil)
	require.True(t, errors.Is(err, ErrSecretGuardBlocked), "got %v", err)
	assert.EqualValues(t, 0, hits.Load())
	assert.Contains(t, err.Error(), "2 secret(s) [aws-access-token] in message 1;")
}

// completeVariants runs each single-shot entry point, so every Complete*
// test covers all three (row 819f83f94a4cca42).
var completeVariants = []struct {
	name string
	call func(c *Client, inv Invocation) error
}{
	{"Complete", func(c *Client, inv Invocation) error {
		_, err := c.Complete(context.Background(), inv)
		return err
	}},
	{"CompleteWithUsage", func(c *Client, inv Invocation) error {
		_, _, _, err := c.CompleteWithUsage(context.Background(), inv)
		return err
	}},
	{"CompleteWithMeta", func(c *Client, inv Invocation) error {
		_, err := c.CompleteWithMeta(context.Background(), inv)
		return err
	}},
}

func TestComplete_SecretGuardBlockRefusesBeforeSending(t *testing.T) {
	for _, v := range completeVariants {
		t.Run(v.name, func(t *testing.T) {
			srv, hits, _ := guardServer(t)
			stubScanner(t, nil)
			inv := guardInv(srv, "block", 0)
			inv.Prompt = "key " + chatAWSKey

			err := v.call(fastRetry(srv.Client()), inv)
			require.ErrorIs(t, err, ErrSecretGuardBlocked)
			assert.Contains(t, err.Error(), "aws-access-token")
			assert.NotContains(t, err.Error(), chatAWSKey, "the error never carries the secret")
			assert.EqualValues(t, 0, hits.Load(), "no request is sent")
		})
	}
}

func TestComplete_SecretGuardBlockFailsClosedOnScanError(t *testing.T) {
	for _, v := range completeVariants {
		t.Run(v.name, func(t *testing.T) {
			srv, hits, _ := guardServer(t)
			stubScanner(t, payload.ErrSecretScanFailed)
			inv := guardInv(srv, "block", 0)
			inv.Prompt = "clean"

			err := v.call(fastRetry(srv.Client()), inv)
			require.ErrorIs(t, err, ErrSecretGuardBlocked)
			require.ErrorIs(t, err, payload.ErrSecretScanFailed)
			assert.EqualValues(t, 0, hits.Load())
		})
	}
}

func TestComplete_SecretGuardRedactSendsThePlaceholder(t *testing.T) {
	for _, v := range completeVariants {
		t.Run(v.name, func(t *testing.T) {
			srv, hits, body := guardServer(t)
			stubScanner(t, nil)
			inv := guardInv(srv, "redact", 0)
			inv.Prompt = "key " + chatAWSKey

			require.NoError(t, v.call(fastRetry(srv.Client()), inv))
			assert.EqualValues(t, 1, hits.Load())
			assert.NotContains(t, *body, chatAWSKey)
			assert.Contains(t, *body, payload.SecretPlaceholder)
		})
	}
}

func TestComplete_SecretGuardWarnSendsUnchanged(t *testing.T) {
	for _, v := range completeVariants {
		t.Run(v.name, func(t *testing.T) {
			srv, hits, body := guardServer(t)
			scanned := stubScanner(t, nil)
			inv := guardInv(srv, "warn", 0)
			inv.Prompt = "key " + chatAWSKey

			require.NoError(t, v.call(fastRetry(srv.Client()), inv))
			assert.Equal(t, []string{inv.Prompt}, *scanned)
			assert.EqualValues(t, 1, hits.Load())
			assert.Contains(t, *body, chatAWSKey)
		})
	}
}

// Unset and off never scan, and a caller that already guarded the prompt
// (AlreadyGuardedMessages 1, as fanout sets) is not scanned twice.
func TestComplete_SecretGuardSkipsOffAndGuardedPrompts(t *testing.T) {
	for _, v := range completeVariants {
		for _, tc := range []struct {
			policy  string
			guarded int
		}{{"", 0}, {"off", 0}, {"block", 1}} {
			t.Run(v.name+"/"+tc.policy, func(t *testing.T) {
				srv, hits, body := guardServer(t)
				scanned := stubScanner(t, nil)
				inv := guardInv(srv, tc.policy, tc.guarded)
				inv.Prompt = "key " + chatAWSKey

				require.NoError(t, v.call(fastRetry(srv.Client()), inv))
				assert.Empty(t, *scanned)
				assert.EqualValues(t, 1, hits.Load())
				assert.Contains(t, *body, chatAWSKey)
			})
		}
	}
}

func TestComplete_SecretGuardRejectsBadInput(t *testing.T) {
	for _, v := range completeVariants {
		t.Run(v.name, func(t *testing.T) {
			srv, hits, _ := guardServer(t)
			stubScanner(t, nil)
			inv := guardInv(srv, "block", 2)
			inv.Prompt = "clean"
			require.ErrorIs(t, v.call(fastRetry(srv.Client()), inv), ErrAlreadyGuardedMessages)

			inv = guardInv(srv, "maybe", 0)
			inv.Prompt = "clean"
			require.ErrorContains(t, v.call(fastRetry(srv.Client()), inv), "unknown secret_guard policy")
			assert.EqualValues(t, 0, hits.Load())
		})
	}
}
