# Acceptance Criteria: Provider and Agent Registry Configuration

**Related User Story:** [02: Agent Configuration](../user-stories/02-agent-configuration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Config Loader | Go `yaml.v3` | Strict mode `KnownFields(true)` |
| Registry Path | `~/.config/atcr/registry.yaml` | User-level config |
| Struct Validation | Custom Go validation | Required field checks |
| Environment Resolution | `os.LookupEnv` | API keys resolved at invoke time |
| Test Framework | `testify` (assert, require) | Table-driven tests with YAML fixtures |

## Related Files
- `internal/registry/config.go` - create: Registry struct, Provider struct, AgentConfig struct, loader functions
- `internal/registry/config_test.go` - create: Tests for registry parsing and validation
- `internal/registry/testdata/` - create: Test YAML fixtures (valid configs, invalid configs)
- `docs/registry.md` - create: User-facing documentation for registry.yaml schema

## Happy Path Scenarios

**Scenario 1: Developer configures a provider with API key env var**
- **Given** a `~/.config/atcr/registry.yaml` file containing:
  ```yaml
  providers:
    openai:
      api_key_env: OPENAI_API_KEY
  ```
- **When** atcr loads the registry
- **Then** the provider "openai" is registered with `api_key_env: "OPENAI_API_KEY"`
- **And** the API key is NOT read from the environment at load time

**Scenario 2: Developer configures an agent with required fields**
- **Given** a registry.yaml containing:
  ```yaml
  agents:
    bruce:
      provider: openai
      model: gpt-4
  ```
- **When** atcr loads the registry
- **Then** agent "bruce" is registered with `provider: "openai"` and `model: "gpt-4"`
- **And** optional fields use defaults: `temperature: 0.7`, `timeout_secs: 600`, `rate_limited: false`, `payload: "blocks"`

**Scenario 3: Developer configures a custom provider with base_url override**
- **Given** a registry.yaml containing:
  ```yaml
  providers:
    local-llm:
      api_key_env: LOCAL_API_KEY
      base_url: http://localhost:11434/v1
  ```
- **When** atcr loads the registry
- **Then** the provider "local-llm" is registered with `base_url: "http://localhost:11434/v1"`
- **And** atcr uses this base_url for API calls to this provider

**Scenario 4: Developer configures multiple agents across different providers**
- **Given** a registry.yaml with providers "openai" and "anthropic" and agents referencing each
- **When** atcr loads the registry
- **Then** each agent resolves to its configured provider
- **And** all agent configurations are available for roster selection

**Scenario 5: Developer selects a subset of agents in project config**
- **Given** `.atcr/config.yaml` contains `agents: [bruce, greta, kai]`
- **When** atcr loads the project config
- **Then** only bruce, greta, and kai are in the active roster
- **And** mira, dax, otto are excluded from the review

## Edge Cases

**Edge Case 1: Agent references non-existent provider**
- **Given** registry.yaml has agent "bruce" with `provider: "nonexistent"`
- **When** atcr loads the registry
- **Then** the tool returns an error: "agent 'bruce' references unknown provider 'nonexistent'"
- **And** exits with non-zero exit code

**Edge Case 2: Agent with no provider field (missing required field)**
- **Given** registry.yaml has agent "bruce" without a `provider` key
- **When** atcr loads the registry
- **Then** the tool returns an error: "agent 'bruce': required field 'provider' is missing"
- **And** exits with non-zero exit code

**Edge Case 3: Agent with no model field (missing required field)**
- **Given** registry.yaml has agent "bruce" with `provider` but no `model`
- **When** atcr loads the registry
- **Then** the tool returns an error: "agent 'bruce': required field 'model' is missing"

**Edge Case 4: registry.yaml does not exist**
- **Given** `~/.config/atcr/registry.yaml` does not exist
- **When** atcr loads the registry
- **Then** atcr uses embedded defaults (default provider and agent configurations)
- **And** prints an informational message: "no registry found, using embedded defaults"

**Edge Case 5: Empty agents list in project config**
- **Given** `.atcr/config.yaml` contains `agents: []`
- **When** atcr loads the project config
- **Then** the tool returns an error: "no agents selected — add at least one agent to .atcr/config.yaml"

**Edge Case 6: Project config references agent not in registry**
- **Given** `.atcr/config.yaml` contains `agents: [unknown-agent]`
- **When** atcr loads configuration
- **Then** the tool returns an error: "agent 'unknown-agent' in project config not found in registry"

## Error Conditions

**Error Scenario 1: Invalid YAML syntax**
- Error message: "failed to parse registry.yaml: yaml: line <N>: <detail>"
- Exit code: 1

**Error Scenario 2: Unknown field in provider config (strict mode)**
- Error message: "registry.yaml providers.<name>: unknown field 'typo_field'"
- Exit code: 1

**Error Scenario 3: Unknown field in agent config (strict mode)**
- Error message: "registry.yaml agents.<name>: unknown field 'temprature'"
- Exit code: 1

**Error Scenario 4: API key env var not set at invoke time**
- Error message: "API key env var OPENAI_API_KEY not set (required by provider 'openai')"
- Exit code: 1
- Timing: Error occurs at invoke time (when making API call), not at config load time

## Performance Requirements
- **Response Time:** Registry loading and validation completes in < 50ms
- **Throughput:** N/A (single load per invocation)

## Security Considerations
- **Authentication:** API keys are NEVER stored in config files — only env var names are referenced
- **Input Validation:** All YAML parsed with `KnownFields(true)` to catch typos
- **Secret Resolution:** API keys read from environment at invoke time only, not persisted
- **Path Traversal:** `base_url` is not validated for scheme but should be user-controlled (local endpoints)

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Valid registry.yaml fixtures (single provider, multi-provider, all optional fields)
- Invalid registry.yaml fixtures (missing required fields, unknown fields, bad YAML syntax)
- Edge case fixtures (empty agents, dangling provider refs)

**Mock/Stub Requirements:**
- Filesystem: use `t.TempDir()` with test fixture files
- Environment: use `t.Setenv()` to set/unset API key env vars
- Home directory: override via `HOME` env var or inject path parameter

**Test Cases:**
1. `TestRegistryLoad_ValidConfig` — parse valid YAML, verify all fields
2. `TestRegistryLoad_MissingProvider` — verify error for missing required field
3. `TestRegistryLoad_MissingModel` — verify error for missing required field
4. `TestRegistryLoad_UnknownField` — verify strict mode catches typos
5. `TestRegistryLoad_DanglingProviderRef` — verify agent referencing unknown provider errors
6. `TestRegistryLoad_NoRegistryFile` — verify embedded defaults used
7. `TestRegistryLoad_APIKeyNotReadAtLoad` — verify key NOT read during load (unset env var, no error)
8. `TestRegistryLoad_APIKeyResolvedAtInvoke` — verify key IS read at invoke time
9. `TestRegistryLoad_CustomBaseURL` — verify base_url stored correctly
10. `TestProjectConfig_AgentSubset` — verify roster subset selection works
11. `TestProjectConfig_EmptyAgents` — verify error on empty roster
12. `TestProjectConfig_UnknownAgent` — verify error for agent not in registry

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds
- [ ] Registry loads valid configs without error
- [ ] Strict mode rejects unknown fields with clear error messages
- [ ] Missing required fields produce descriptive errors

**Story-Specific:**
- [ ] Provider struct has fields: `api_key_env` (required), `base_url` (optional)
- [ ] AgentConfig struct has fields: `provider` (required), `model` (required), `persona` (optional), `temperature` (optional), `timeout_secs` (optional), `rate_limited` (optional), `fallback` (optional), `payload` (optional)
- [ ] API keys are resolved by env var name at invoke time, not load time
- [ ] Project config can select agent subset from registry
- [ ] Agent referencing unknown provider produces error at load time
- [ ] Empty agent roster produces error

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Registry schema documented in `docs/registry.md`
- [ ] Error messages are clear and actionable
