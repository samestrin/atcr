# Configuration & Registry [IMPORTANT]

## Overview

atcr uses a two-tier configuration architecture that separates user-level concerns from project-specific settings. The system loads configuration from `~/.config/atcr/registry.yaml` (user-level: providers + agents) and `.atcr/config.yaml` (project-level: roster, payload mode, timeouts, fail-on). This separation allows developers to maintain consistent provider credentials and agent definitions across multiple projects while tailoring review behavior per repository. > Source: [codebase-discovery.json:integration_points - "Filesystem (config)"]

Configuration resolution follows a strict precedence chain: CLI flags override project config, which overrides registry values, which fall back to embedded defaults. This ensures maximum flexibility for ad-hoc experimentation while maintaining stable baselines. All YAML parsing uses `KnownFields(true)` via `NewDecoder` for strict mode, catching typos like `serial_agnets:` at load time instead of silently ignoring them. > Source: [yaml-v3.md:Integration Notes (atcr)]

The fallback-chain validation mechanism detects dangling references and cycles in agent fallback chains, producing hard errors before any provider calls are made. This prevents subtle runtime failures and ensures deterministic behavior during review execution. > Source: [codebase-discovery.json:architecture_notes - "fallback-chain validation (dangling + cycle = error)"]

## Key Concepts

### Two-Tier Configuration Architecture

User-level configuration (`~/.config/atcr/registry.yaml`) contains provider definitions (API endpoints, auth env vars) and agent definitions (persona, temperature, timeout, fallback chain). Project-level configuration (`.atcr/config.yaml`) specifies the agent roster, payload mode, global timeouts, and failure thresholds. > Source: [codebase-discovery.json:existing_patterns - "Two-tier: ~/.config/atcr/registry.yaml (user-level: providers + agents) and .atcr/config.yaml (project-level: roster, payload mode, timeouts, fail-on)"]

### Strict Mode Parsing

Always decode with `KnownFields(true)` via `NewDecoder` for strict config parsing. Unknown YAML keys become errors immediately, preventing silent misconfiguration. > Source: [yaml-v3.md:Key APIs - "Decoder.KnownFields(true) — strict mode: unknown YAML keys become errors"]

### Precedence Rules

Configuration values resolve in this order: CLI flag > project config (.atcr/config.yaml) > registry (~/.config/atcr/registry.yaml) > embedded default. This allows targeted overrides without duplicating entire configuration blocks. > Source: [codebase-discovery.json:architecture_notes - "Precedence: CLI flag > project config > registry > embedded default"]

### Fallback-Chain Validation

Agent fallback chains are validated at load time. Dangling references (pointing to non-existent agents) and cycles produce immediate errors rather than runtime failures. > Source: [codebase-discovery.json:architecture_notes - "fallback-chain validated at load time (dangling + cycle = error)"]

### YAML Boolean Behavior

YAML 1.1 booleans (`yes/no`, `on/off`) decode as bools into typed bool fields. This is relevant when users hand-edit `registry.yaml` and may expect string matching. > Source: [yaml-v3.md:Caveats]

## Code Examples

### Struct Tags for YAML Mapping

```go
type Config struct {
    Agents       []string `yaml:"agents"`
    SerialAgents []string `yaml:"serial_agents,omitempty"`
    TimeoutSecs  int      `yaml:"timeout_seconds,omitempty"`
}
```
> Source: [yaml-v3.md:Quick Start]

### Strict Decoder Usage

```go
var cfg Config
decoder := yaml.NewDecoder(r)
decoder.KnownFields(true)
if err := decoder.Decode(&cfg); err != nil { ... }
```
> Source: [yaml-v3.md:Key APIs - "Always decode with KnownFields(true) via NewDecoder"]

### Whole-Document Marshal/Unmarshal

```go
var cfg Config
if err := yaml.Unmarshal(data, &cfg); err != nil { ... }
out, err := yaml.Marshal(cfg)
```
> Source: [yaml-v3.md:Quick Start]

## Quick Reference

| Concept | Details |
|---------|---------|
| **User config** | `~/.config/atcr/registry.yaml` — providers + agents |
| **Project config** | `.atcr/config.yaml` — roster, payload mode, timeouts, fail-on |
| **Precedence** | CLI flag > project config > registry > embedded default |
| **Strict mode** | `NewDecoder().KnownFields(true)` catches typos immediately |
| **Validation** | Fallback-chain: dangling + cycle = error |
| **Package version** | `gopkg.in/yaml.v3` v3.0.1 (long-term stable since 2022) |
| **Boolean quirk** | YAML 1.1 `yes/no`, `on/off` decode as bools |
| **Custom marshaling** | Implement `MarshalYAML()` / `UnmarshalYAML()` on custom types |

## Related Documentation

- [Coding Standards](../../../../specifications/coding-standards.md) — Go coding standards including package naming, receiver names, error wrapping
- [Standard Library Patterns](../../../../specifications/packages/standard-library.md) — stdlib package assignments (os/exec, context, sync) used alongside YAML config loading
- [Registry Pinned Versions](../../../../specifications/packages/registry.yaml) — Dependency versions including yaml.v3 v3.0.1
- [Implementation Standards](../../../../specifications/implementation-standards.md) — Architecture principles (black-box, single responsibility, primitive-first) driving package design
