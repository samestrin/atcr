package payload

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ClaimLedgerPromptSection returns the CLAIMS TO VERIFY block a rendered prompt
// carries, and whether the prompt carries one at all. It is the single reader
// of the ledger's framing markers outside claimLedgerSection, so the block's
// shape — header line, verdict contract, BEGIN/END framing — is parsed by the
// package that renders it rather than re-derived by every consumer that needs
// to inspect a dispatched prompt.
//
// ok is false when the prompt carries no ledger (a branch whose commits assert
// nothing renders no section) and when the block is present but unterminated
// (framing markers are neutralized inside claim text precisely so this cannot
// happen through rendered content; a missing terminator means the prompt was
// truncated by something this package did not write).
func ClaimLedgerPromptSection(prompt string) (section string, ok bool) {
	const head = "## CLAIMS TO VERIFY\n"
	end := claimsEndMarker + "\n"
	i := strings.Index(prompt, head)
	if i < 0 {
		return "", false
	}
	j := strings.Index(prompt[i:], end)
	if j < 0 {
		return "", false
	}
	return prompt[i : i+j+len(end)], true
}

// VerifyClaimLedgerWiring is the exported test seam for the claim ledger's
// end-to-end wiring contract (Epic 35.16.7 AC1/AC3). The ledger is engine-
// rendered instruction text whose whole value depends on reaching EVERY
// reviewer identically — a ledger some agents got and others did not looks
// exactly like a branch that claimed nothing — so the contract is a property
// of the DISPATCHED prompts, not of any single package's internals.
// internal/fanout's wiring test builds the real roster, payloads and slots
// (that part is fanout behavior) and hands the rendered prompts here, so the
// payload contract is asserted by THIS package's own API instead of being
// re-derived in the consumer's test against fanout test internals.
//
// prompts maps an opaque per-agent key to the prompt that agent was
// dispatched. It returns an error naming the first violated clause: an agent
// whose prompt carries no ledger, or two agents whose ledgers are not
// byte-identical. Verifying nothing is an error, not a silent pass — a wiring
// test that collects zero prompts has proven nothing and must fail loudly.
//
// It is a test seam by contract, not by build tag: it reads its arguments and
// returns errors, so it stays importable from production code and assertable
// from any package that can import payload.
func VerifyClaimLedgerWiring(prompts map[string]string) error {
	if len(prompts) == 0 {
		return errors.New("claim ledger wiring: no prompts to verify")
	}
	names := make([]string, 0, len(prompts))
	for name := range prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	firstName, firstSection := "", ""
	for _, name := range names {
		section, ok := ClaimLedgerPromptSection(prompts[name])
		if !ok {
			return fmt.Errorf("claim ledger wiring: agent %q: prompt carries no claim ledger", name)
		}
		if firstName == "" {
			firstName, firstSection = name, section
			continue
		}
		if section != firstSection {
			return fmt.Errorf("claim ledger wiring: agent %q must receive a byte-identical claim ledger to agent %q", name, firstName)
		}
	}
	return nil
}
