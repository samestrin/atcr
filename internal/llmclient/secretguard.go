package llmclient

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
)

// secret_guard policies, as in internal/fanout/secretguard.go. Empty is off
// here: Chat callers opt in.
const (
	secretGuardOff    = "off"
	secretGuardWarn   = "warn"
	secretGuardRedact = "redact"
	secretGuardBlock  = "block"
)

// ErrSecretGuardBlocked is returned by Chat when secret_guard=block finds a
// credential in an outbound message, or cannot trust the scan. No request is
// sent.
var ErrSecretGuardBlocked = errors.New("secret_guard=block: messages not sent")

// ErrAlreadyGuardedMessages is returned by Chat when
// Invocation.AlreadyGuardedMessages is negative or above len(messages).
var ErrAlreadyGuardedMessages = errors.New("already-guarded message count out of range")

// scanSecrets is the scanner Chat runs; a variable so tests can stub it.
var scanSecrets = payload.ScanPayloadContext

// guardMessages applies inv's secret_guard policy to the messages at and after
// inv.AlreadyGuardedMessages. It scans each message's Content and its text
// Parts; image parts, tool-call arguments and reasoning members are not text
// the caller supplied. off and warn return messages unchanged; redact returns
// a copy with every hit replaced by payload.SecretPlaceholder, leaving the
// caller's slice and strings alone; block returns ErrSecretGuardBlocked when
// anything is found or the scan could not be trusted, the fail-closed rule of
// internal/fanout/secretguard.go. Neither a warning nor an error holds secret
// material: only message indexes and rule kinds are printed. A text
// inv.ScanCache already holds is not scanned again; its stored hits are used.
func guardMessages(ctx context.Context, inv Invocation, messages []Message) ([]Message, error) {
	from := inv.AlreadyGuardedMessages
	if from < 0 || from > len(messages) {
		return nil, fmt.Errorf("%w: %d of %d messages", ErrAlreadyGuardedMessages, from, len(messages))
	}
	policy := strings.TrimSpace(inv.SecretGuard)
	switch policy {
	case "", secretGuardOff:
		return messages, nil
	case secretGuardWarn, secretGuardRedact, secretGuardBlock:
	default:
		return nil, fmt.Errorf("unknown secret_guard policy %q (want off, warn, redact or block)", policy)
	}

	var (
		out     = messages
		copied  bool
		scanErr error
		kinds   = map[string]bool{}
		indexes []int
		count   int
	)
	// scan reports text's hits, recording them for the summary, and returns
	// text redacted when the policy says so.
	scan := func(i int, text string) (string, bool) {
		if text == "" {
			return text, false
		}
		hits, err := inv.ScanCache.scan(ctx, text)
		if err != nil {
			scanErr = err
		}
		if len(hits) == 0 {
			return text, false
		}
		count += len(hits)
		for _, h := range hits {
			kinds[h.Kind] = true
		}
		if len(indexes) == 0 || indexes[len(indexes)-1] != i {
			indexes = append(indexes, i)
		}
		if policy != secretGuardRedact {
			return text, false
		}
		return payload.RedactSecrets(text, hits), true
	}
	for i := from; i < len(messages); i++ {
		m := messages[i]
		contentChanged, partsCloned := false, false
		if m.Content != nil {
			if s, ok := scan(i, *m.Content); ok {
				m.Content, contentChanged = &s, true
			}
		}
		for j, p := range m.Parts {
			if p.Type != "text" {
				continue
			}
			if s, ok := scan(i, p.Text); ok {
				if !partsCloned {
					m.Parts, partsCloned = slices.Clone(m.Parts), true
				}
				m.Parts[j].Text = s
			}
		}
		if contentChanged || partsCloned {
			if !copied {
				out, copied = slices.Clone(messages), true
			}
			out[i] = m
		}
	}

	logger := log.FromContext(ctx)
	if scanErr != nil {
		// ScanPayloadContext's error names only the failure mode, never scanner
		// output, so it is safe to print.
		logger.Warn(fmt.Sprintf("chat: secret scan: %v; using the built-in ruleset", scanErr))
	}
	found := describeHits(count, kinds, indexes)
	switch policy {
	case secretGuardBlock:
		if count > 0 {
			return nil, fmt.Errorf("%w: %s; remove the credential or set secret_guard: redact", ErrSecretGuardBlocked, found)
		}
		if scanErr != nil {
			return nil, fmt.Errorf("%w: the secret scan failed, so the messages could not be checked: %w", ErrSecretGuardBlocked, scanErr)
		}
	case secretGuardRedact:
		if count > 0 {
			logger.Warn(fmt.Sprintf("chat: redacted %s (secret_guard: redact)", found))
		}
	default:
		if count > 0 {
			logger.Warn(fmt.Sprintf("chat: %s sent unchanged (secret_guard: warn); set secret_guard: redact or block to stop it", found))
		}
	}
	return out, nil
}

// describeHits renders "2 secret(s) [aws-access-token, github-pat] in message 1, 3".
func describeHits(count int, kinds map[string]bool, indexes []int) string {
	ks := make([]string, 0, len(kinds))
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	is := make([]string, len(indexes))
	for n, i := range indexes {
		is[n] = fmt.Sprint(i)
	}
	return fmt.Sprintf("%d secret(s) [%s] in message %s", count, strings.Join(ks, ", "), strings.Join(is, ", "))
}
