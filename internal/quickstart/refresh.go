package quickstart

import "io"

// BuildManifestFromModels rebuilds the synthetic manifest JSON from an
// OpenAI-compatible /models API response, preserving the base manifest's
// signup_url, referral, and provider (none of which come from /models). Stub —
// implemented in GREEN.
func BuildManifestFromModels(apiResp []byte, base *Manifest) ([]byte, error) {
	return nil, nil
}

// RunRefresh is the entry point for the scheduled manifest-refresh job: it reads
// a /models response from in, rebuilds the manifest atop the embedded base, and
// writes the result to out. It returns a process exit code. Stub — implemented
// in GREEN.
func RunRefresh(args []string, in io.Reader, out, errOut io.Writer) int {
	return 1
}
