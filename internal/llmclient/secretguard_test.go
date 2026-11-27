package llmclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	atcrerrors "github.com/samestrin/atcr/internal/errors"
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

// AlreadyGuardedMessages 3 on the single-shot path is ErrAlreadyGuardedMessages
// on every Complete* variant, with no request sent: Complete* sends Prompt as
// one message, so anything above 1 is out of range (Invocation doc,
// client.go:159-164). Pins the documented error behaviour against a clamp
// (row da5c6b591648ba42); value 2 is pinned by TestComplete_SecretGuardRejectsBadInput.
func TestComplete_SecretGuardRejectsGuardedCountThree(t *testing.T) {
	for _, v := range completeVariants {
		t.Run(v.name, func(t *testing.T) {
			srv, hits, _ := guardServer(t)
			stubScanner(t, nil)
			inv := guardInv(srv, "block", 3)
			inv.Prompt = "clean"
			err := v.call(fastRetry(srv.Client()), inv)
			require.ErrorIs(t, err, ErrAlreadyGuardedMessages)
			assert.ErrorContains(t, err, "3 of 1 messages")
			assert.EqualValues(t, 0, hits.Load(), "no request is sent")
		})
	}
}

// A guard result that does not carry exactly one prompt message with text is
// a broken invariant, not a panic: every Complete* variant returns a
// non-retryable system error and sends nothing (row fc6aae632c3ca6f7).
// guardMessages cannot produce these shapes today (it returns the one message
// or a same-length clone), so the seam completeGuardMessages is stubbed, as
// stubScanner stubs scanSecrets.
func TestComplete_SecretGuardEmptyGuardResultFailsClosed(t *testing.T) {
	for _, v := range completeVariants {
		for _, tc := range []struct {
			name    string
			guarded []Message
		}{
			{"no messages", nil},
			{"nil content", []Message{{Role: "user"}}},
			{"two messages", []Message{{Role: "user", Content: strp("a")}, {Role: "user", Content: strp("b")}}},
		} {
			t.Run(v.name+"/"+tc.name, func(t *testing.T) {
				srv, hits, _ := guardServer(t)
				stubScanner(t, nil)
				old := completeGuardMessages
				completeGuardMessages = func(context.Context, Invocation, []Message) ([]Message, error) {
					return tc.guarded, nil
				}
				t.Cleanup(func() { completeGuardMessages = old })
				inv := guardInv(srv, "redact", 0)
				inv.Prompt = "clean"

				err := v.call(fastRetry(srv.Client()), inv)
				require.ErrorContains(t, err, "secret guard returned no prompt message")
				var ce *atcrerrors.ClassifiedError
				require.ErrorAs(t, err, &ce)
				assert.Equal(t, atcrerrors.SystemError, ce.Classification, "a broken guard invariant is a system error")
				assert.False(t, atcrerrors.IsRetryable(err), "retrying cannot repair a broken invariant")
				assert.EqualValues(t, 0, hits.Load(), "no request is sent")
			})
		}
	}
}

// A ScanCache on the invocation scans each distinct text once across calls,
// and every call still redacts it: the cache holds the scan's hits, never the
// redacted text, so the caller's history keeps its original strings and each
// request is rewritten from them again.
func TestChat_SecretGuardScanCacheScansEachTextOnce(t *testing.T) {
	srv, hits, body := guardServer(t)
	scanned := stubScanner(t, nil)

	content := "key " + chatAWSKey
	msgs := []Message{{Role: "user", Content: strp("prompt")}, {Role: "tool", Content: &content}}
	inv := guardInv(srv, "redact", 1)
	inv.ScanCache = NewScanCache(8)
	c := fastRetry(srv.Client())
	for call := 1; call <= 3; call++ {
		_, err := c.Chat(context.Background(), inv, msgs, nil)
		require.NoError(t, err, "call %d", call)
		assert.NotContains(t, *body, chatAWSKey, "call %d is redacted", call)
		assert.Contains(t, *body, payload.SecretPlaceholder, "call %d", call)
		msgs = append(msgs, Message{Role: "user", Content: strp(fmt.Sprintf("turn %d", call))})
	}
	assert.EqualValues(t, 3, hits.Load())
	assert.Equal(t, []string{content, "turn 1", "turn 2"}, *scanned, "each text is scanned on the first call that sends it")
	assert.Equal(t, "key "+chatAWSKey, *msgs[1].Content, "the caller's history is untouched")
}

// A scan that failed is not cached, so the next call scans the text again
// (gitleaks may recover); the hits the failed scan returned are still used.
func TestChat_SecretGuardScanCacheRetriesErrors(t *testing.T) {
	srv, _, body := guardServer(t)
	scanned := stubScanner(t, payload.ErrSecretScanFailed)

	content := "key " + chatAWSKey
	inv := guardInv(srv, "redact", 0)
	inv.ScanCache = NewScanCache(8)
	c := fastRetry(srv.Client())
	for call := 1; call <= 2; call++ {
		_, err := c.Chat(context.Background(), inv, []Message{{Role: "tool", Content: &content}}, nil)
		require.NoError(t, err)
		assert.NotContains(t, *body, chatAWSKey, "call %d is redacted from the failed scan's hits", call)
	}
	assert.Equal(t, []string{content, content}, *scanned)
}

// The cache is bounded: once it holds max texts it stores no more, and a text
// it did not store is scanned on every call, as without a cache.
func TestChat_SecretGuardScanCacheIsBounded(t *testing.T) {
	srv, _, _ := guardServer(t)
	scanned := stubScanner(t, nil)

	inv := guardInv(srv, "redact", 0)
	inv.ScanCache = NewScanCache(1)
	msgs := []Message{{Role: "user", Content: strp("a")}, {Role: "user", Content: strp("b")}}
	c := fastRetry(srv.Client())
	for range 2 {
		_, err := c.Chat(context.Background(), inv, msgs, nil)
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"a", "b", "b"}, *scanned)
}

// ScanCache.scan hands every caller its own copy of the hits: a caller that
// mutates the slice it got back cannot poison the stored scan, so the next
// call still sees the original hits (rows 2b7624d41b293a44, a74273e0462c92e2).
func TestScanCache_ScanReturnsACopy(t *testing.T) {
	scanned := stubScanner(t, nil)
	c := NewScanCache(4)
	text := "key " + chatAWSKey

	first, err := c.scan(context.Background(), text)
	require.NoError(t, err)
	require.Len(t, first, 1)
	first[0].Kind = "mutated"
	first[0].Start = -1

	second, err := c.scan(context.Background(), text)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, "aws-access-token", second[0].Kind, "a cached hit is copied, not shared")
	assert.Equal(t, len("key "), second[0].Start, "the stored hit keeps its offsets")
	assert.Equal(t, []string{text}, *scanned, "the second call is served from the cache")
}

// Callers sharing one ScanCache across concurrent calls each mutate their own
// returned hits. With the stored slice handed out uncopied this is a data race
// on the shared backing array that the map's mutex does not catch; under
// -race this test fails on the uncopied code and passes on the copy (row
// a74273e0462c92e2). CI runs every package with -race.
func TestScanCache_ScanCopiesAreRaceFree(t *testing.T) {
	stubScanner(t, nil)
	c := NewScanCache(4)
	text := "key " + chatAWSKey
	// Warm the cache first so every goroutine below takes the cached path and
	// the stub scanner's unsynchronised scan log is never touched concurrently.
	warm, err := c.scan(context.Background(), text)
	require.NoError(t, err)
	require.Len(t, warm, 1)

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range 50 {
				hits, err := c.scan(context.Background(), text)
				if err != nil || len(hits) != 1 {
					t.Errorf("goroutine %d iteration %d: hits %v, err %v", g, i, hits, err)
					return
				}
				hits[0].Kind = fmt.Sprintf("mutated-%d-%d", g, i)
				hits[0].Start = g
			}
		}(g)
	}
	wg.Wait()

	after, err := c.scan(context.Background(), text)
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.Equal(t, "aws-access-token", after[0].Kind, "the stored scan survives concurrent callers")
}

// The guard's log lines name the caller: Chat's say "chat:" (row
// b08aa592c80f6566 pins the single-shot side in
// TestComplete_SecretGuardLogsNameTheCompleteCaller).
func TestChat_SecretGuardLogsNameTheChatCaller(t *testing.T) {
	srv, _, _ := guardServer(t)
	stubScanner(t, nil)
	var logs bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))

	_, err := fastRetry(srv.Client()).Chat(ctx, guardInv(srv, "redact", 0),
		[]Message{{Role: "user", Content: strp("key " + chatAWSKey)}}, nil)
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "chat: redacted 1 secret(s) [aws-access-token] in message 0 (secret_guard: redact)")
}

// Every Complete* variant's guard log lines say "complete:", not "chat:": a
// warn/redact/scan-error line from a single-shot call must not read as a
// tool-loop event (row b08aa592c80f6566).
func TestComplete_SecretGuardLogsNameTheCompleteCaller(t *testing.T) {
	variants := []struct {
		name string
		call func(c *Client, ctx context.Context, inv Invocation) error
	}{
		{"Complete", func(c *Client, ctx context.Context, inv Invocation) error {
			_, err := c.Complete(ctx, inv)
			return err
		}},
		{"CompleteWithUsage", func(c *Client, ctx context.Context, inv Invocation) error {
			_, _, _, err := c.CompleteWithUsage(ctx, inv)
			return err
		}},
		{"CompleteWithMeta", func(c *Client, ctx context.Context, inv Invocation) error {
			_, err := c.CompleteWithMeta(ctx, inv)
			return err
		}},
	}
	for _, v := range variants {
		for _, tc := range []struct {
			name    string
			policy  string
			prompt  string
			scanErr error
			want    string
		}{
			{"redact", "redact", "key " + chatAWSKey, nil, "complete: redacted 1 secret(s) [aws-access-token] in message 0 (secret_guard: redact)"},
			{"warn", "warn", "key " + chatAWSKey, nil, "complete: 1 secret(s) [aws-access-token] in message 0 sent unchanged (secret_guard: warn)"},
			{"scan error", "warn", "clean", payload.ErrSecretScanFailed, "complete: secret scan:"},
		} {
			t.Run(v.name+"/"+tc.name, func(t *testing.T) {
				srv, _, _ := guardServer(t)
				stubScanner(t, tc.scanErr)
				var logs bytes.Buffer
				ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
				inv := guardInv(srv, tc.policy, 0)
				inv.Prompt = tc.prompt

				require.NoError(t, v.call(fastRetry(srv.Client()), ctx, inv))
				assert.Contains(t, logs.String(), tc.want)
				assert.NotContains(t, logs.String(), "chat:", "a single-shot call is not a tool-loop event")
				assert.NotContains(t, logs.String(), chatAWSKey, "the log never carries the secret")
			})
		}
	}
}

// Without a cache every call scans every unguarded text, as before.
func TestChat_SecretGuardNoScanCacheRescans(t *testing.T) {
	srv, _, _ := guardServer(t)
	scanned := stubScanner(t, nil)

	msgs := []Message{{Role: "user", Content: strp("a")}}
	c := fastRetry(srv.Client())
	for range 2 {
		_, err := c.Chat(context.Background(), guardInv(srv, "redact", 0), msgs, nil)
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"a", "a"}, *scanned)
}
