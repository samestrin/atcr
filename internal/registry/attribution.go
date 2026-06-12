package registry

import (
	"errors"
	"fmt"
)

// Entry kinds for source attribution of a validation failure.
const (
	entryProvider = "provider"
	entryAgent    = "agent"
)

// entryError tags a validation failure with the offending entry (kind + name)
// so a merged-view load can attribute it to the file that defined the entry.
// Its Error() string is exactly the underlying message, so single-file callers
// (LoadRegistry) and their tests see no change; Unwrap preserves any wrapped
// sentinel (ErrDanglingFallback / ErrFallbackCycle) for errors.Is.
type entryError struct {
	kind string // entryProvider | entryAgent
	name string
	msg  string
	err  error // optional wrapped sentinel
}

func (e *entryError) Error() string { return e.msg }
func (e *entryError) Unwrap() error { return e.err }

// agentErrf builds an entryError attributed to agent name.
func agentErrf(name, format string, a ...any) error {
	return &entryError{kind: entryAgent, name: name, msg: fmt.Sprintf(format, a...)}
}

// providerErrf builds an entryError attributed to provider name.
func providerErrf(name, format string, a ...any) error {
	return &entryError{kind: entryProvider, name: name, msg: fmt.Sprintf(format, a...)}
}

// agentSentinelErr builds an entryError that also wraps a sentinel so
// errors.Is(err, sentinel) keeps working through attribution.
func agentSentinelErr(name string, sentinel error, msg string) error {
	return &entryError{kind: entryAgent, name: name, msg: msg, err: sentinel}
}

// attribute wraps an entry-specific validation error with the file that defined
// the offending entry, so a merged-view failure names project vs user. Plain
// (non-entry) errors and entries with no recorded source pass through unchanged.
func (r *Registry) attribute(err error) error {
	var ee *entryError
	if !errors.As(err, &ee) {
		// Non-entry failures are top-level settings faults (payload_mode,
		// timeout_secs, ...) which only the user registry carries; preserve the
		// "registry.yaml: " framing LoadRegistry attaches in the single-file path.
		return fmt.Errorf("%s: %w", userRegistryLabel, err)
	}
	var src EntrySource
	switch ee.kind {
	case entryProvider:
		src = r.ProviderSource[ee.name]
	case entryAgent:
		src = r.AgentSource[ee.name]
	}
	if src.File == "" {
		return err
	}
	return fmt.Errorf("%s: %w", src.File, err)
}
