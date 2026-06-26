package fanout

import (
	"os"

	"github.com/samestrin/atcr/internal/registry"
)

// minSecretLen is the floor a resolved API key value must meet before it is
// added to a log Redactor's exact-value scrub list. It guards against
// over-redaction: a coincidentally-short or misconfigured env value (e.g. a
// placeholder "dev") matched verbatim would scrub unrelated log text. No real
// provider key is shorter than this (OpenAI ~51, Google AIzaSy… 39, Azure
// 32-hex, JWTs longer), so the floor never drops a genuine key. It is symmetric
// with the Redactor's own empty-string guard (internal/log/redact.go).
//
// Tradeoff (accepted): the floor is deliberately low (8), so an 8–31 char
// misconfigured or self-hosted key value is still admitted and matched
// verbatim, and could over-redact an unrelated log substring that contains it.
// This is accepted because genuine provider keys are 32+ and the asymmetry
// favors never leaking a real key over never over-redacting a short one. If
// self-hosted short keys become a concern, raise this floor or scope the
// verbatim match to header-adjacent contexts.
const minSecretLen = 8

// SecretValues returns the distinct, resolved API key values across this
// review's slots (each slot's primary plus its fallback chain), suitable for
// passing as the variadic secrets to log.NewRedactor so the exact-value scrub
// is live in production. Each agent names its key via Invocation.APIKeyEnv; the
// actual secret string is materialized here via os.Getenv (the same on-demand
// resolution llmclient.resolveKey performs at invoke time — keys are never
// persisted on the slot). Values are deduped by resolved value and any value
// shorter than minSecretLen is skipped to avoid over-redaction. The resolved
// values are never logged: they flow only into the Redactor by value.
//
// Snapshot contract (known limitation): the values are resolved once, at this
// call, and handed to the Redactor by value. A key set or rotated in the
// environment AFTER this call returns is therefore not added to the exact-value
// scrub list and could appear verbatim in a later log line. This is acceptable
// because every entry point resolves keys here before any provider call runs
// (cmd/atcr review.go/resume.go via correlateAndRedact, the MCP handler via
// reviewContext), so the live scrub set always covers the keys the run uses.
//
// It also returns a list of human-readable warnings (never containing a value)
// for configured-but-unusable slots: a named APIKeyEnv that resolves empty or
// below minSecretLen means exact-value redaction is silently inactive for that
// slot. The caller logs these with its own logger so a fat-fingered env name or
// too-short key is observable; a slot with no APIKeyEnv configured is not a
// misconfiguration and is not warned. SecretValues stays dependency-light
// (imports only "os") — it returns the warnings rather than logging them.
func (p *PreparedReview) SecretValues() (secrets []string, warnings []string) {
	seen := make(map[string]struct{})
	warned := make(map[string]struct{})
	for _, s := range p.Slots {
		for _, a := range append([]Agent{s.Primary}, s.Fallbacks...) {
			env := a.Invocation.APIKeyEnv
			v := os.Getenv(env)
			if len(v) < minSecretLen {
				// Configured (non-empty env name) but empty/too short: exact-value
				// redaction is inactive for this slot. Warn once per env name; a slot
				// with no APIKeyEnv is not a misconfiguration and is not warned.
				if env != "" {
					if _, dup := warned[env]; !dup {
						warned[env] = struct{}{}
						warnings = append(warnings, "API key env "+env+" is unset or below the redaction floor; exact-value log redaction is inactive for that slot")
					}
				}
				continue
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			secrets = append(secrets, v)
		}
	}
	return secrets, warnings
}

// RegistrySecretValues resolves the distinct API key values configured across a
// registry's providers, suitable for the variadic secrets of log.NewRedactor.
// It mirrors (*PreparedReview).SecretValues for entry points that hold only a
// resolved registry — `atcr verify` and the atcr_verify MCP tool — and never
// built a PreparedReview: each provider names its key via APIKeyEnv, the value
// is read from the environment here, deduped, and dropped below minSecretLen to
// avoid over-redaction. Resolved values are never logged; they flow only into
// the Redactor by value. A nil registry yields no secrets.
func RegistrySecretValues(reg *registry.Registry) []string {
	if reg == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var secrets []string
	for _, prov := range reg.Providers {
		v := os.Getenv(prov.APIKeyEnv)
		if len(v) < minSecretLen {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		secrets = append(secrets, v)
	}
	return secrets
}
