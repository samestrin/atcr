package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

const testSecret = "s3cret"

var testNow = time.Unix(1_700_000_000, 0)

func sign(secret string, ts time.Time, body string) http.Header {
	t := strconv.FormatInt(ts.Unix(), 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("v0:" + t + ":" + body))
	h := http.Header{}
	h.Set(HeaderTimestamp, t)
	h.Set(HeaderSignature, "v0="+hex.EncodeToString(m.Sum(nil)))
	return h
}

func TestVerify(t *testing.T) {
	body := `{"type":"url_verification"}`
	good := sign(testSecret, testNow, body)

	noSig := good.Clone()
	noSig.Del(HeaderSignature)
	noTS := good.Clone()
	noTS.Del(HeaderTimestamp)
	badTS := good.Clone()
	badTS.Set(HeaderTimestamp, "yesterday")
	wrongSig := sign("other", testNow, body)

	cases := []struct {
		name   string
		secret string
		h      http.Header
		body   string
		now    time.Time
		want   error
	}{
		{"good", testSecret, good, body, testNow, nil},
		{"good 4m old", testSecret, sign(testSecret, testNow.Add(-4*time.Minute), body), body, testNow, nil},
		{"bad signature", testSecret, wrongSig, body, testNow, ErrBadSignature},
		{"tampered body", testSecret, good, body + " ", testNow, ErrBadSignature},
		{"missing signature", testSecret, noSig, body, testNow, ErrMissingSignature},
		{"missing timestamp", testSecret, noTS, body, testNow, ErrMissingTimestamp},
		{"malformed timestamp", testSecret, badTS, body, testNow, ErrMalformedTimestamp},
		{"6 minutes old", testSecret, sign(testSecret, testNow.Add(-6*time.Minute), body), body, testNow, ErrStaleTimestamp},
		{"6 minutes in future", testSecret, sign(testSecret, testNow.Add(6*time.Minute), body), body, testNow, ErrStaleTimestamp},
		{"empty secret", "", good, body, testNow, ErrNoSigningSecret},
	}
	seen := map[error]bool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Verify(c.secret, c.h, []byte(c.body), c.now)
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
		if c.want != nil {
			seen[c.want] = true
		}
	}
	if len(seen) != 6 {
		t.Fatalf("expected 6 distinct errors, got %d", len(seen))
	}
}

func TestParseEvent(t *testing.T) {
	ev, err := ParseEvent([]byte(`{"type":"url_verification","challenge":"abc"}`))
	if err != nil || ev.Challenge != "abc" || ev.Mention != nil {
		t.Fatalf("url_verification: %+v %v", ev, err)
	}

	ev, err = ParseEvent([]byte(`{"type":"event_callback","team_id":"T1","event_id":"Ev1","event":{"type":"app_mention","user":"U1","channel":"C1","text":"<@B> review https://github.com/o/r/pull/1","ts":"1.2","thread_ts":"1.1"}}`))
	if err != nil || ev.Mention == nil {
		t.Fatalf("app_mention: %+v %v", ev, err)
	}
	want := AppMention{EventID: "Ev1", TeamID: "T1", Channel: "C1", User: "U1", Text: "<@B> review https://github.com/o/r/pull/1", TS: "1.2", ThreadTS: "1.1"}
	if *ev.Mention != want {
		t.Fatalf("got %+v", *ev.Mention)
	}

	for name, body := range map[string]string{
		"other event":  `{"type":"event_callback","event":{"type":"message"}}`,
		"other type":   `{"type":"app_rate_limited"}`,
		"no challenge": `{"type":"url_verification"}`,
		"invalid json": `{`,
	} {
		if _, err := ParseEvent([]byte(body)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
