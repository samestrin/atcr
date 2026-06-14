# Utility Packages

**Last Updated:** June 14, 2026

> ⚠️ **Stub — see yaml-v3.md.** The only package listed here (`gopkg.in/yaml.v3`) has full documentation in [yaml-v3.md](yaml-v3.md), which is the authoritative source.
> This file is a legacy stub and can be removed once no references to it exist.

This document consolidates documentation for utility packages used in the project.

---

| Package | Version | Description | Docs |
|---------|---------|-------------|------|
| gopkg.in/yaml.v3 | v3.0.1 | YAML support for the Go language | [📖](https://pkg.go.dev/gopkg.in/yaml.v3) |

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

---

**Note:** Utility packages typically don't require detailed local documentation.
Refer to official documentation links above for complete API references.
