package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/doctor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declareResponseFormatOnBruce appends the response_format declaration to the
// single-agent registry the setup helpers write. The short-reply floor only
// runs for a declared agent (internal/doctor probes response_format only when
// the target declares it), so a short_reply row cannot exist without this.
func declareResponseFormatOnBruce(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(regPath, append(data, []byte("    response_format: json_object\n")...), 0o644))
}

// setupDoctorEnvPersonaShortWithFallback wires bruce (unresolvable persona,
// declared response_format) at the short-reply provider with a fallback at a
// healthy one. A working fallback covers the short primary, so the endpoint
// verdict stays 0 while rep.Agents keeps the short_reply row — the shape
// internal/doctor's TestSmokeFloor_AWorkingFallbackCoversAShortPrimary pins.
// The persona fold then raises the exit code with endpointFailed=false, which
// is the ONLY way a short_reply row reaches the persona-only error branch.
func setupDoctorEnvPersonaShortWithFallback(t *testing.T, shortURL, okURL string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(""+
		"providers:\n"+
		"  short:\n"+
		"    api_key_env: ATCR_DOCTOR_TEST_KEY\n"+
		"    base_url: "+shortURL+"/v1\n"+
		"  normal:\n"+
		"    api_key_env: ATCR_DOCTOR_TEST_KEY\n"+
		"    base_url: "+okURL+"/v1\n"+
		"agents:\n"+
		"  bruce:\n"+
		"    provider: short\n"+
		"    model: test-model\n"+
		"    persona: never-installed\n"+
		"    response_format: json_object\n"+
		"    fallback: bruce-back\n"+
		"  bruce-back:\n"+
		"    provider: normal\n"+
		"    model: test-model\n"), 0o644))

	work := t.TempDir()
	t.Chdir(work)
	atcrDir := filepath.Join(work, ".atcr")
	require.NoError(t, os.MkdirAll(atcrDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(atcrDir, "config.yaml"), []byte(""+
		"agents:\n  - bruce\npayload_mode: blocks\ntimeout_secs: 600\nfail_on: HIGH\n"), 0o644))
}

// TD 7e6b577fab4f6ab0 (cli/doctor.go:305): the persona-resolution branches of
// the final doctor error did not name short_reply agents — only the plain
// endpoint branch did. An operator whose run has BOTH an unresolvable persona
// and a sub-floor review-shaped reply was sent to the persona files alone, with
// the model that answered in 4 tokens unnamed. Both branches must carry the
// same short_reply clause the plain branch carries: the agent, its model, and
// the token floor. Under --json this error string is the only prose a caller
// gets, so it is asserted there.
func TestDoctor_PersonaErrorExitMessageNamesShortReplyAgents(t *testing.T) {
	floorClause := fmt.Sprintf("%d-token floor", doctor.SmokeTokenFloor)

	// Branch 1: the short_reply row itself fails the endpoint verdict, so
	// endpointFailed=true and the message names both the failed probe and the
	// persona — and must now name the short reply too.
	t.Run("endpoint failed", func(t *testing.T) {
		srv := shortReplyProvider(t)
		setupDoctorEnvWithPersonaRef(t, srv.URL, "never-installed")
		declareResponseFormatOnBruce(t)
		t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

		_, err := execute(t, "doctor", "--json")
		require.Error(t, err, "an unresolvable persona plus a sub-floor reply must not exit 0")

		msg := err.Error()
		assert.Contains(t, msg, "an endpoint probe failed")
		assert.Contains(t, msg, "personas could not be resolved for: bruce")
		assert.Contains(t, msg, floorClause,
			"the persona branch must name the token floor like the plain endpoint branch does")
		assert.Contains(t, msg, "bruce (test-model)",
			"the short_reply agent AND its model must be named — the model is what failed")
	})

	// Branch 2: a working fallback covers the short primary, so the endpoint
	// verdict is 0 and only the persona fold fails the run. The short_reply row
	// is still in the report, and the persona-only message must name it too.
	t.Run("persona only", func(t *testing.T) {
		shortSrv := shortReplyProvider(t)
		okSrv := echoProvider(t, 0)
		setupDoctorEnvPersonaShortWithFallback(t, shortSrv.URL, okSrv.URL)
		t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

		_, err := execute(t, "doctor", "--json")
		require.Error(t, err, "an unresolvable persona must not exit 0 even when a fallback covers the short primary")

		msg := err.Error()
		assert.Contains(t, msg, "persona could not be resolved for: bruce")
		assert.NotContains(t, msg, "an endpoint probe failed",
			"the fallback covers the short primary, so the endpoint verdict did not fail")
		assert.Contains(t, msg, floorClause,
			"the persona-only branch must name the token floor like the plain endpoint branch does")
		assert.Contains(t, msg, "bruce (test-model)",
			"the short_reply agent AND its model must be named")
	})
}

// The known-good direction: a persona error with NO short_reply row must keep
// the exact message it had — no floor clause, no stray separator. A clause
// appended unconditionally would name a token floor nobody failed.
func TestDoctor_PersonaErrorWithoutShortReplyKeepsItsMessage(t *testing.T) {
	srv := echoProvider(t, http.StatusUnauthorized)
	setupDoctorEnvWithPersonaRef(t, srv.URL, "never-installed")
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-bad")

	_, err := execute(t, "doctor", "--json")
	require.Error(t, err)

	msg := err.Error()
	assert.Contains(t, msg, "an endpoint probe failed")
	assert.Contains(t, msg, "personas could not be resolved for: bruce")
	assert.NotContains(t, msg, "token floor",
		"no agent answered under the floor, so the clause must not appear")
	assert.NotContains(t, msg, ";)", "an empty clause must not leak its separator")
}
