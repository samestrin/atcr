package payload

// RED stub for the claim-ledger wiring seam (TD re-attempt,
// internal/fanout/claim_ledger_wiring_test.go:26): the contract test in
// claims_wiring_test.go pins what these must do — accept byte-identical
// ledgers, name the agent and clause otherwise. This stub deliberately verifies
// nothing so the test fails first.

// ClaimLedgerPromptSection is a stub; the real extractor lands with GREEN.
func ClaimLedgerPromptSection(prompt string) (section string, ok bool) {
	return "", false
}

// VerifyClaimLedgerWiring is a stub; the real wiring check lands with GREEN.
func VerifyClaimLedgerWiring(prompts map[string]string) error {
	return nil
}
