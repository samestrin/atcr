package payload

import (
	"strings"
	"testing"
)

// The wiring seam's own contract: VerifyClaimLedgerWiring must accept any set
// of prompts whose ledgers are byte-identical, and must name the agent and the
// violated clause when one is missing or diverges. The fanout wiring test
// drives the real end-to-end path; this test pins the seam itself.
func TestVerifyClaimLedgerWiring(t *testing.T) {
	section := claimLedgerSection([]string{"Begin() no longer returns a wiped offset"}, claimsComplete)
	if section == "" {
		t.Fatal("precondition: the fixture claim must render a ledger section")
	}
	promptWith := "persona wrapper\n" + section + "diff body\n"

	t.Run("identical ledgers across agents pass", func(t *testing.T) {
		err := VerifyClaimLedgerWiring(map[string]string{
			"greta": promptWith,
			"kai":   "other wrapper\n" + section + "other body\n",
		})
		if err != nil {
			t.Fatalf("byte-identical ledgers must pass: %v", err)
		}
	})

	t.Run("a prompt without the ledger is named", func(t *testing.T) {
		err := VerifyClaimLedgerWiring(map[string]string{"greta": promptWith, "kai": "no ledger here"})
		if err == nil {
			t.Fatal("a prompt missing the ledger must fail the wiring check")
		}
		if !strings.Contains(err.Error(), `"kai"`) {
			t.Fatalf("the error must name the offending agent, got: %v", err)
		}
	})

	t.Run("divergent ledgers are named", func(t *testing.T) {
		divergent := strings.Replace(promptWith, "wiped", "restored", 1)
		err := VerifyClaimLedgerWiring(map[string]string{"greta": promptWith, "kai": divergent})
		if err == nil {
			t.Fatal("divergent ledgers must fail the wiring check")
		}
		if !strings.Contains(err.Error(), "byte-identical") {
			t.Fatalf("the error must name the violated clause, got: %v", err)
		}
	})

	t.Run("an empty verification is an error, not a silent pass", func(t *testing.T) {
		if err := VerifyClaimLedgerWiring(map[string]string{}); err == nil {
			t.Fatal("verifying nothing must not report success")
		}
	})
}
