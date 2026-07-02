package quickstart

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseManifest() *Manifest {
	return &Manifest{
		SignupURL: "https://synthetic.new/",
		Referral:  "CODE",
		Provider:  Provider{Name: "synthetic", BaseURL: "https://api.synthetic.new/openai/v1", APIKeyEnv: "LLM_SYNTHETIC_API_KEY"},
		Models:    []string{"old-model"},
	}
}

func TestBuildManifestFromModels_ExtractsIdsPreservesBase(t *testing.T) {
	resp := []byte(`{"data":[{"id":"model-a"},{"id":"model-b"}]}`)
	out, err := BuildManifestFromModels(resp, baseManifest())
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, json.Unmarshal(out, &m))
	assert.Equal(t, []string{"model-a", "model-b"}, m.Models, "model ids refreshed from /models")
	// Fields not present in /models are preserved from the base.
	assert.Equal(t, "https://synthetic.new/", m.SignupURL)
	assert.Equal(t, "CODE", m.Referral)
	assert.Equal(t, "LLM_SYNTHETIC_API_KEY", m.Provider.APIKeyEnv)
	// Output ends with a trailing newline for a clean file diff.
	assert.True(t, strings.HasSuffix(string(out), "\n"))
}

func TestBuildManifestFromModels_EmptyDataRefused(t *testing.T) {
	// An empty model list must NOT overwrite the manifest — that would ship a
	// zero-model quickstart. Refuse instead.
	_, err := BuildManifestFromModels([]byte(`{"data":[]}`), baseManifest())
	assert.Error(t, err)
}

func TestBuildManifestFromModels_InvalidJSON(t *testing.T) {
	_, err := BuildManifestFromModels([]byte(`not json`), baseManifest())
	assert.Error(t, err)
}

func TestBuildManifestFromModels_RegeneratedManifestLoads(t *testing.T) {
	// The regenerated manifest must satisfy the same validation LoadManifest
	// applies, so a refresh can never ship a manifest the wizard rejects.
	resp := []byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`)
	out, err := BuildManifestFromModels(resp, baseManifest())
	require.NoError(t, err)
	var m Manifest
	require.NoError(t, json.Unmarshal(out, &m))
	assert.NoError(t, m.validate())
}

func TestBuildManifestFromModels_RejectsControlCharInId(t *testing.T) {
	// A newline in a model id would forge YAML lines in the generated
	// registry.yaml — a hostile /models response must be refused.
	resp := []byte(`{"data":[{"id":"ok"},{"id":"evil\n    injected: true"}]}`)
	_, err := BuildManifestFromModels(resp, baseManifest())
	assert.Error(t, err)
}

func TestRunRefresh_Success(t *testing.T) {
	in := strings.NewReader(`{"data":[{"id":"x"},{"id":"y"}]}`)
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := RunRefresh(nil, in, out, errOut)
	assert.Equal(t, 0, code, "exit 0 on success")

	var m Manifest
	require.NoError(t, json.Unmarshal(out.Bytes(), &m))
	assert.Equal(t, []string{"x", "y"}, m.Models)
	// Provider is preserved from the embedded base.
	assert.Equal(t, "synthetic", m.Provider.Name)
}

func TestRunRefresh_BadInput(t *testing.T) {
	code := RunRefresh(nil, strings.NewReader("garbage"), &bytes.Buffer{}, &bytes.Buffer{})
	assert.NotEqual(t, 0, code, "non-zero exit on bad input")
}
