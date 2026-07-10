# Utility Packages

**Last Updated:** July 08, 2026 05:30:56PM

> ⚠️ **yaml.v3 stub — see yaml-v3.md.** `gopkg.in/yaml.v3` has full documentation in [yaml-v3.md](yaml-v3.md), which is the authoritative source. Its entry below is kept only for consolidated-table completeness.

This document consolidates documentation for utility packages used in the project.

---

| Package | Version | Description | Docs |
|---------|---------|-------------|------|
| gopkg.in/yaml.v3 | v3.0.1 | YAML support for the Go language | [📖](https://pkg.go.dev/gopkg.in/yaml.v3) |
| golang.org/x/mod | v0.37.0 | Go module mechanics (semver comparison via the `semver` subpackage) | [📖](https://pkg.go.dev/golang.org/x/mod) |

---

## Package Details

### gopkg.in/yaml.v3
- **Purpose:** YAML support for the Go language — encode and decode YAML values
- **Version:** v3.0.1
- **Docs:** [https://pkg.go.dev/gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3)
- **Install:** `go get gopkg.in/yaml.v3`
- **Repository:** [https://github.com/go-yaml/yaml](https://github.com/go-yaml/yaml)
- **Key APIs:**
  - `Marshal(in interface{}) ([]byte, error)` — serialize Go value to YAML
  - `Unmarshal(in []byte, out interface{}) error` — decode YAML into Go value
  - `NewDecoder(r io.Reader)` / `NewEncoder(w io.Writer)` — stream-based YAML I/O
  - `Node` — low-level AST representation with comment/anchor access
- **Notes:** Supports YAML 1.2 with partial YAML 1.1 backwards compatibility (yes/no, on/off booleans, octal 0777 format). Based on a pure Go port of libyaml.

### golang.org/x/mod
- **Purpose:** Packages for writing tools that work directly with Go module mechanics. atcr uses only the `semver` subpackage (`golang.org/x/mod/semver`), which implements comparison of semantic version strings.
- **Version:** v0.37.0 (project-pinned; latest upstream is v0.38.0)
- **Docs:** [https://pkg.go.dev/golang.org/x/mod/semver](https://pkg.go.dev/golang.org/x/mod/semver)
- **Install:** `go get golang.org/x/mod`
- **Repository:** [cs.opensource.google/go/x/mod](https://cs.opensource.google/go/x/mod)
- **Key APIs (semver subpackage):**
  - `IsValid(v string) bool` — reports whether v is a valid semantic version string
  - `Compare(v, w string) int` — returns -1, 0, or +1 comparing two valid semver strings
  - `Major(v string)` / `MajorMinor(v string)` — extract the major or major.minor prefix of a version
- **Notes:** Not at v1 (module itself is not considered API-stable), but the `semver` subpackage's comparison semantics are stable and widely relied upon across the Go ecosystem. In atcr, `internal/personas/upgrade.go`'s `isNewer` is the sole call site, using `IsValid`/`Compare` to decide whether a re-fetched persona's version is newer than the installed copy.

---

**Note:** Utility packages typically don't require detailed local documentation.
Refer to official documentation links above for complete API references.
