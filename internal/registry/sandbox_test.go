package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSandboxConfig_Validate(t *testing.T) {
	pid := 0
	secsValid := 60
	secsZero := 0
	secsNeg := -1
	secsOver := MaxTimeoutSecs + 1
	cases := []struct {
		name string
		cfg  *SandboxConfig
		ok   bool
		// wantSub is the substring the error MUST contain for a failing case; it
		// pins each rejection to the field it is meant to catch, so a refactor that
		// short-circuits on the wrong field or reorders checks fails here instead of
		// staying green on a generic require.Error. Empty for ok==true cases.
		wantSub string
	}{
		{"nil is valid (unconfigured)", nil, true, ""},
		{"docker + image + test command", &SandboxConfig{Backend: "docker", Image: "golang:1.25", TestCommand: []string{"go", "test", "./..."}}, true, ""},
		{"default backend + image + test command", &SandboxConfig{Image: "python:3.12", TestCommand: []string{"make", "test"}}, true, ""},
		{"unsupported backend", &SandboxConfig{Backend: "podman", Image: "img", TestCommand: []string{"go", "test"}}, false, "is unsupported"},
		{"missing image", &SandboxConfig{Backend: "docker", TestCommand: []string{"go", "test"}}, false, "image is required"},
		{"missing test command", &SandboxConfig{Backend: "docker", Image: "img"}, false, "test_command is required"},
		{"empty test token", &SandboxConfig{Image: "img", TestCommand: []string{"go", ""}}, false, "must not contain empty tokens"},
		{"non-positive pids", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, PidsLimit: &pid}, false, "pids_limit must be positive"},
		{"valid timeout_secs", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, TimeoutSecs: &secsValid}, true, ""},
		{"zero timeout_secs", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, TimeoutSecs: &secsZero}, false, "timeout_secs must be within"},
		{"negative timeout_secs", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, TimeoutSecs: &secsNeg}, false, "timeout_secs must be within"},
		{"over-max timeout_secs", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, TimeoutSecs: &secsOver}, false, "timeout_secs must be within"},
		{"valid memory with unit", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, Memory: "512m"}, true, ""},
		{"valid memory bare bytes", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, Memory: "512"}, true, ""},
		{"invalid memory non-numeric", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, Memory: "abc"}, false, "is not a valid docker size"},
		{"invalid memory zero", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, Memory: "0"}, false, "is not a valid docker size"},
		{"valid cpus float", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, CPUs: "1.5"}, true, ""},
		{"invalid cpus non-numeric", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, CPUs: "abc"}, false, "must be a positive number"},
		{"invalid cpus zero", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, CPUs: "0"}, false, "must be a positive number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.ok {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantSub,
					"failing case must return the error naming its root cause")
			}
		})
	}
}

// TestSandboxConfig_Validate_AutoFixTensionUnchanged pins the open design tension
// surfaced by Sprint 32.0 (AC 02-02): an operator who adds a `sandbox:` block
// SOLELY so `--auto-fix`'s validation step can be sandboxed — and omits
// test_command, which is irrelevant to auto-fix (it runs auto_fix.validate_command,
// not test_command) — STILL fails config load with the unconditional
// Image+TestCommand requirement. Sprint 32.0 deliberately does NOT relax this
// (loosening it would weaken `--exec`'s existing contract); it pins the current
// behavior here and leaves a split/parallel-light-validation path as future work.
// If a later change relaxes SandboxConfig.Validate() for the auto-fix case, this
// test must be revisited as a conscious decision, not slipped in silently.
func TestSandboxConfig_Validate_AutoFixTensionUnchanged(t *testing.T) {
	// A sandbox block added only for --auto-fix, missing the (auto-fix-irrelevant)
	// test_command, must still be rejected at config load.
	autoFixOnly := &SandboxConfig{Backend: "docker", Image: "golang:1.25"}
	err := autoFixOnly.Validate()
	require.Error(t, err, "auto-fix-only sandbox block must still require test_command (tension NOT relaxed)")
	assert.Contains(t, err.Error(), "test_command is required")

	// Symmetrically, omitting image is still rejected.
	noImage := &SandboxConfig{Backend: "docker", TestCommand: []string{"go", "test", "./..."}}
	noImageErr := noImage.Validate()
	require.Error(t, noImageErr)
	assert.Contains(t, noImageErr.Error(), "image is required")
}

// TestSandboxConfig_Validate_Fallback covers the `sandbox.fallback` field on its
// own, deliberately as a separate function rather than folded into
// TestSandboxConfig_Validate's table: the field's whole purpose is to be an
// ADDITIVE opt-in that cannot disturb the pinned Image/TestCommand contract, and
// a rejection that accidentally rode on one of those checks would be invisible
// inside a shared table. Every case pairs the Fallback value with an otherwise
// valid config so the assertion isolates the fallback branch specifically.
//
// The allowlist is exact-match with no case-folding, mirroring the Backend check
// in the same function: a typo'd casing must fail at config load rather than
// silently no-op'ing at the resolver, where "fallback configured" and "fallback
// absent" have opposite security meanings.
//
// Every accepted case asserts the field's value AFTER Validate(), not merely the
// absence of an error. Without that, a case named "treated as unset" passes on a
// config whose Fallback still holds "   " — and the resolvers test their config
// fields for raw non-emptiness, so that leftover reads as "opted in".
func TestSandboxConfig_Validate_Fallback(t *testing.T) {
	base := func(fallback string) *SandboxConfig {
		return &SandboxConfig{
			Backend:     SandboxBackendDocker,
			Image:       "golang:1.25",
			TestCommand: []string{"go", "test", "./..."},
			Fallback:    fallback,
		}
	}
	cases := []struct {
		name     string
		fallback string
		ok       bool
		// wantStored is the value the field MUST hold after a successful
		// Validate() — the two-state invariant the resolvers rely on. Only
		// meaningful for ok==true cases.
		wantStored string
		wantSub    string
	}{
		{"unset is valid (pre-story configs unchanged)", "", true, "", ""},
		{"exact sentinel is valid", SandboxFallbackOSLevel, true, SandboxFallbackOSLevel, ""},
		{"whitespace-only is treated as unset", "   ", true, "", ""},
		{"tab/newline-only is treated as unset", "\t\n", true, "", ""},
		{"surrounding whitespace is trimmed", "  os-level  ", true, SandboxFallbackOSLevel, ""},
		{"unsupported value", "always", false, "", "is unsupported"},
		{"a backend name is not a fallback value", "docker", false, "", "is unsupported"},
		{"upper camel case is rejected (no case folding)", "OS-Level", false, "", "is unsupported"},
		{"all caps is rejected (no case folding)", "OS-LEVEL", false, "", "is unsupported"},
		{"near-miss spelling is rejected", "oslevel", false, "", "is unsupported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base(tc.fallback)
			err := cfg.Validate()
			if tc.ok {
				assert.NoError(t, err)
				assert.Equal(t, tc.wantStored, cfg.Fallback,
					"after Validate() the field must hold exactly \"\" or the sentinel — a resolver testing it for raw non-emptiness must not see a third state")
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantSub)
			assert.Equal(t, tc.fallback, cfg.Fallback,
				"rejected config must leave Fallback unmodified — normalization runs only on the success path")
			assert.Contains(t, err.Error(), "sandbox.fallback",
				"the error must name the field it rejected, not a neighbouring one")
			assert.Contains(t, err.Error(), SandboxFallbackOSLevel,
				"the error must name the supported value so the operator can fix it")
		})
	}
}

// TestSandboxConfig_Validate_FallbackDoesNotWeakenPinnedChecks is the
// independence proof for the fallback branch: a valid Fallback must not exempt a
// config from the unconditional Image/TestCommand requirements, and an INVALID
// Fallback must not preempt them either. Both directions matter — the first
// would let `fallback: os-level` smuggle an incomplete block past config load,
// the second would reorder which failure an operator sees for a config that is
// broken in two ways, and either would show up as churn in
// TestSandboxConfig_Validate_AutoFixTensionUnchanged.
func TestSandboxConfig_Validate_FallbackDoesNotWeakenPinnedChecks(t *testing.T) {
	// Valid fallback does not exempt the config from the Image requirement.
	noImage := &SandboxConfig{TestCommand: []string{"go", "test"}, Fallback: SandboxFallbackOSLevel}
	err := noImage.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image is required")

	// ...nor from the TestCommand requirement.
	noCmd := &SandboxConfig{Image: "img", Fallback: SandboxFallbackOSLevel}
	err = noCmd.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test_command is required")

	// An invalid fallback on a config that ALSO fails a pinned check reports the
	// pinned failure: the fallback branch runs after them and cannot short-circuit.
	both := &SandboxConfig{Fallback: "always"}
	err = both.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image is required",
		"the fallback branch must not preempt the unconditional Image/TestCommand checks")

	// Whitespace-only fallback must stay raw when an earlier check rejects the
	// config: trimming it only on the success path guarantees "   " reads as
	// "unset" to downstream raw-non-emptiness tests.
	whitespaceFallback := &SandboxConfig{Fallback: "   "}
	err = whitespaceFallback.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image is required")
	assert.Equal(t, "   ", whitespaceFallback.Fallback,
		"rejected config must leave Fallback unmodified")
}

// TestSandboxConfig_FallbackYAMLRoundTrip pins the wire form. omitempty is the
// back-compat guarantee: a config that never set a fallback must marshal without
// a `fallback:` key at all, so round-tripping an operator's existing file cannot
// introduce one.
func TestSandboxConfig_FallbackYAMLRoundTrip(t *testing.T) {
	var parsed SandboxConfig
	require.NoError(t, yaml.Unmarshal([]byte("backend: docker\nimage: golang:1.25\nfallback: os-level\ntest_command: [go, test]\n"), &parsed))
	assert.Equal(t, SandboxFallbackOSLevel, parsed.Fallback)
	assert.NoError(t, parsed.Validate())

	out, err := yaml.Marshal(&parsed)
	require.NoError(t, err)
	assert.Contains(t, string(out), "fallback: os-level")

	unset := SandboxConfig{Backend: SandboxBackendDocker, Image: "img", TestCommand: []string{"go", "test"}}
	unsetOut, err := yaml.Marshal(&unset)
	require.NoError(t, err)
	assert.NotContains(t, string(unsetOut), "fallback",
		"an unset fallback must not appear in the marshalled config (omitempty)")
}
