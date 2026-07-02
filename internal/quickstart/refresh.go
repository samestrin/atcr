package quickstart

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// modelsResponse is the subset of an OpenAI-compatible /models response the
// refresh job needs: the list of model ids under "data".
type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// BuildManifestFromModels rebuilds the synthetic manifest JSON from an
// OpenAI-compatible /models API response, preserving the base manifest's
// signup_url, referral, and provider (none of which come from /models). It
// refuses an empty model list so a transient/misconfigured API response can
// never ship a zero-model manifest that breaks the wizard. Output is 2-space
// indented with a trailing newline to match the committed synthetic.json.
func BuildManifestFromModels(apiResp []byte, base *Manifest) ([]byte, error) {
	var resp modelsResponse
	if err := json.Unmarshal(apiResp, &resp); err != nil {
		return nil, fmt.Errorf("parsing /models response: %w", err)
	}
	models := make([]string, 0, len(resp.Data))
	for _, d := range resp.Data {
		if strings.TrimSpace(d.ID) != "" {
			models = append(models, d.ID)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("refusing to write manifest: /models returned no usable model ids")
	}

	updated := *base
	updated.Models = models
	if err := updated.validate(); err != nil {
		return nil, err
	}

	out, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding manifest: %w", err)
	}
	return append(out, '\n'), nil
}

// RunRefresh is the entry point for the scheduled manifest-refresh job: it reads
// a /models response from in, rebuilds the manifest atop the embedded base, and
// writes the result to out. It returns a process exit code (0 success, 1 error).
func RunRefresh(args []string, in io.Reader, out, errOut io.Writer) int {
	base, err := LoadManifest()
	if err != nil {
		fmt.Fprintln(errOut, "refresh:", err)
		return 1
	}
	apiResp, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintln(errOut, "refresh: reading input:", err)
		return 1
	}
	result, err := BuildManifestFromModels(apiResp, base)
	if err != nil {
		fmt.Fprintln(errOut, "refresh:", err)
		return 1
	}
	if _, err := out.Write(result); err != nil {
		fmt.Fprintln(errOut, "refresh: writing output:", err)
		return 1
	}
	return 0
}
