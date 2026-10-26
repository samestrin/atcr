// Package slack verifies and parses inbound Slack Events API requests.
package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// HeaderSignature carries the v0 HMAC of the request.
	HeaderSignature = "X-Slack-Signature"
	// HeaderTimestamp carries the Unix-seconds time Slack signed the request.
	HeaderTimestamp = "X-Slack-Request-Timestamp"

	// MaxTimestampSkew is how far a request timestamp may differ from now.
	MaxTimestampSkew = 5 * time.Minute

	signatureVersion = "v0"
)

// Distinct verification failures; callers can match them with errors.Is.
var (
	ErrMissingSignature   = errors.New("slack: missing " + HeaderSignature + " header")
	ErrMissingTimestamp   = errors.New("slack: missing " + HeaderTimestamp + " header")
	ErrMalformedTimestamp = errors.New("slack: malformed " + HeaderTimestamp + " header")
	ErrStaleTimestamp     = errors.New("slack: request timestamp outside the 5-minute window")
	ErrBadSignature       = errors.New("slack: signature mismatch")
	ErrNoSigningSecret    = errors.New("slack: signing secret is empty")
)

// Verify checks a Slack request signature over body using the signing secret.
// now is the current time (injected for tests). The signature is compared in
// constant time and the timestamp must be within MaxTimestampSkew of now.
func Verify(secret string, h http.Header, body []byte, now time.Time) error {
	if secret == "" {
		return ErrNoSigningSecret
	}
	sig := h.Get(HeaderSignature)
	if sig == "" {
		return ErrMissingSignature
	}
	ts := h.Get(HeaderTimestamp)
	if ts == "" {
		return ErrMissingTimestamp
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ErrMalformedTimestamp
	}
	skew := now.Sub(time.Unix(sec, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > MaxTimestampSkew {
		return ErrStaleTimestamp
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signatureVersion + ":" + ts + ":"))
	mac.Write(body)
	want := signatureVersion + "=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(sig)), []byte(want)) {
		return ErrBadSignature
	}
	return nil
}
