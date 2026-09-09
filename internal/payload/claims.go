package payload

// DefaultMaxClaimBytes is the default size cap for the commit-message text read
// into the claim ledger, in the style of DefaultMaxDiffBytes: a squashed branch
// or an imported history can carry a very long message, and an uncapped read
// would let it displace the diff it is supposed to be checked against. 64 KiB
// holds every realistic branch's messages; a maxBytes <= 0 means unlimited.
const DefaultMaxClaimBytes int64 = 64 * 1024

// commitMessages returns the commit messages of base..head, oldest first, with
// merge commits excluded.
//
// STUB — replaced in GREEN.
func (g *gitRunner) commitMessages(base, head string, maxBytes int64) (msgs []string, truncated bool, err error) {
	return nil, false, nil
}
