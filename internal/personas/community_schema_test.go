package personas

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	builtins "github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// communityYAMLRoot is the on-disk root of the community persona library YAMLs.
func communityYAMLRoot() string { return filepath.Join("..", "..", "personas", "community") }

// humanNameRe pins the all-human-names convention: a slug is one lowercase
// alphabetic word — no hyphen, digit, or separator that a role-based slug
// (security-reviewer, perf-checker) would carry.
var humanNameRe = regexp.MustCompile(`^[a-z]+$`)

// retiredRoleSlugs are role-based names barred by the all-human-names convention
// (Epic 23.0's retired stragglers plus common role words). A single lowercase
// word can still be a disguised role, so this denylist is a backstop; the
// load-bearing guarantee is name==slug consistency plus manual review.
var retiredRoleSlugs = map[string]struct{}{
	"sentinel": {}, "tracer": {}, "idiomatic": {},
	"security": {}, "perf": {}, "reviewer": {}, "checker": {},
	"auditor": {}, "scanner": {}, "linter": {}, "critic": {},
	"analyst": {}, "inspector": {}, "guardian": {}, "grader": {},
	"monitor": {}, "validator": {}, "enforcer": {}, "judge": {},
	"skeptic": {}, "fixer": {}, "executor": {}, "reviewerbot": {},
}

// TestCommunityPersonas_StrictSchema covers AC 04-06 Scenario 1 / Edge 2: every
// authored community YAML passes the strict community-persona decode (only
// recognized agent fields ∪ defined catalog keys present).
func TestCommunityPersonas_StrictSchema(t *testing.T) {
	names := builtins.CommunityNames()
	require.NotEmpty(t, names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(communityYAMLRoot(), name+".yaml"))
			require.NoErrorf(t, err, "read yaml %s", name)
			require.NoErrorf(t, registry.ValidateCommunityPersonaYAML(name, data),
				"persona %q must pass strict community-persona validation", name)
		})
	}
}

// TestCommunityPersonas_NoPlaceholderModel covers AC 04-06 Scenario 2: no persona
// ships an empty or placeholder provider/model binding.
func TestCommunityPersonas_NoPlaceholderModel(t *testing.T) {
	placeholders := []string{"", "todo", "tbd", "changeme", "<model>", "<provider>", "xxx", "placeholder"}
	for _, name := range builtins.CommunityNames() {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(communityYAMLRoot(), name+".yaml"))
			require.NoErrorf(t, err, "read yaml %s", name)
			var m struct {
				Provider string `yaml:"provider"`
				Model    string `yaml:"model"`
			}
			require.NoError(t, yaml.Unmarshal(data, &m))
			require.NotEmptyf(t, m.Provider, "persona %q provider must be non-empty", name)
			require.NotEmptyf(t, m.Model, "persona %q model must be non-empty", name)
			for _, ph := range placeholders {
				require.NotEqualf(t, ph, strings.ToLower(m.Model), "persona %q model is a placeholder", name)
			}
		})
	}
}

// TestCommunityPersonas_HumanNames covers AC 04-06 Scenario 3 / Error 2: every
// community persona slug is a human first name — no role-based names.
func TestCommunityPersonas_HumanNames(t *testing.T) {
	for _, name := range builtins.CommunityNames() {
		t.Run(name, func(t *testing.T) {
			require.Truef(t, humanNameRe.MatchString(name),
				"persona slug %q must be a single lowercase human name (no hyphen/digit)", name)
			_, retired := retiredRoleSlugs[name]
			require.Falsef(t, retired, "persona slug %q is a retired role-based name", name)

			// The YAML's own name must equal the slug, so a role-based name can't
			// hide inside a human-slugged file (closes the 5.17.A guard gap).
			data, err := os.ReadFile(filepath.Join(communityYAMLRoot(), name+".yaml"))
			require.NoErrorf(t, err, "read yaml %s", name)
			var m struct {
				Name string `yaml:"name"`
			}
			require.NoError(t, yaml.Unmarshal(data, &m))
			require.Equalf(t, name, m.Name, "YAML name %q must equal the human slug %q", m.Name, name)
		})
	}
}

// TestValidateCommunityPersonaYAML_RejectsUnknownField covers AC 04-06 Error 1:
// the strict decode rejects a key that is neither a known agent field nor a
// defined catalog key.
func TestValidateCommunityPersonaYAML_RejectsUnknownField(t *testing.T) {
	const yaml = "provider: openrouter\nmodel: anthropic/claude-opus-4.8\nfoobar: 1\n"
	err := registry.ValidateCommunityPersonaYAML("bad", []byte(yaml))
	require.Error(t, err, "an unknown field must be rejected by the strict decode")
	require.Contains(t, strings.ToLower(err.Error()), "foobar")
}

// TestValidateCommunityPersonaYAML_AcceptsCatalogKeys covers AC 04-06 Edge 2: the
// defined catalog keys are members of the combined known set and pass strict.
func TestValidateCommunityPersonaYAML_AcceptsCatalogKeys(t *testing.T) {
	const yaml = "name: sample\nversion: 1.0.0\ndescription: a sample\nprovider: openrouter\nmodel: anthropic/claude-opus-4.8\npersona: sample\nrole: reviewer\n"
	require.NoError(t, registry.ValidateCommunityPersonaYAML("sample", []byte(yaml)))
}

// TestValidateCommunityPersonaYAML_RejectsOutOfRangeRole covers AC 04-06 Edge 1:
// an out-of-range role value fails agent validation.
func TestValidateCommunityPersonaYAML_RejectsOutOfRangeRole(t *testing.T) {
	const yaml = "provider: openrouter\nmodel: anthropic/claude-opus-4.8\nrole: auditor\n"
	require.Error(t, registry.ValidateCommunityPersonaYAML("bad", []byte(yaml)),
		"an out-of-range role must be rejected")
}
