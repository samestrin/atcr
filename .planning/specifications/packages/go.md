# go

**Version:** 1.25.0
**Registry:** [pkg.go.dev](https://pkg.go.dev/)
**Official Docs:** [https://pkg.go.dev/std](https://pkg.go.dev/std)
**Tier:** Critical
**Last Updated:** June 14, 2026

---

## Overview

The Go standard library is a comprehensive collection of packages that ships with every Go installation. It provides built-in functionality for text processing, I/O, networking, cryptography, data encoding, operating system interactions, and concurrency — eliminating the need for external dependencies for most common programming tasks.

The library is organized hierarchically into functional domains: standalone top-level packages (e.g., `fmt`, `os`, `context`) and grouped sub-packages under parent directories (e.g., `net/http`, `encoding/json`, `crypto/tls`).

---

## Installation

Go and its standard library are installed together via the official toolchain:

```bash
# Download from https://go.dev/dl/
# macOS (Homebrew)
brew install go

# Verify installation
go version
```

The standard library is bundled with the Go toolchain — no separate installation required. All packages are available by default in any Go module.

---

## Core API

### Top-Level Packages

| Package | Description |
|---------|-------------|
| `fmt` | Formatted I/O with functions analogous to C's `printf` and `scanf` |
| `os` | Platform-independent interface to operating system functionality |
| `io` | Basic interfaces to I/O primitives |
| `strings` | Simple functions to manipulate UTF-8 encoded strings |
| `sync` | Basic synchronization primitives such as mutual exclusion locks |
| `context` | Defines the `Context` type, which carries deadlines, cancellation signals, and request-scoped values across API boundaries |
| `errors` | Functions to manipulate errors |
| `log` | Simple logging package |
| `strconv` | Conversions between strings and primitive types |
| `time` | Time and duration functionality |
| `math` | Basic mathematical constants and functions |
| `sort` | Sorting primitives for slices and user-defined collections |
| `reflect` | Run-time reflection (type introspection) |
| `testing` | Automated testing support |

### Grouped Packages

| Group | Key Packages | Description |
|-------|-------------|-------------|
| `net/` | `http`, `url`, `smtp` | Networking — `net/http` provides HTTP client and server implementations |
| `encoding/` | `json`, `xml`, `csv`, `base64` | Data encoding/decoding — `encoding/json` implements JSON per RFC 7159 |
| `crypto/` | `aes`, `rsa`, `sha256`, `tls`, `x509` | Cryptographic primitives and TLS |
| `os/` | `exec`, `signal` | OS sub-packages — `os/exec` runs external commands |
| `database/` | `sql` | Generic SQL database interface |
| `html/` | `template` | HTML templating |
| `text/` | `template` | General text templating |
| `archive/` | `tar`, `zip` | Archive format handling |
| `compress/` | `gzip`, `zip` | Compression algorithms |

---

## Common Patterns

### Error Handling

Go uses explicit error returns rather than exceptions:

```go
result, err := someFunc()
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}
```

### Context Propagation

`context.Context` is passed as the first argument to functions for cancellation and deadlines:

```go
func DoWork(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case result := <-compute():
        return nil
    }
}
```

### Resource Management with defer

`defer` ensures cleanup runs when the surrounding function returns:

```go
f, err := os.Open("file.txt")
if err != nil {
    return err
}
defer f.Close()
```

### Interface-Based Design

The `io` package defines core interfaces (`io.Reader`, `io.Writer`) that compose throughout the stdlib:

```go
// Any io.Reader can be wrapped
buf := new(bytes.Buffer)
io.Copy(buf, response.Body)
```

### Struct Tags for JSON

`encoding/json` uses struct tags for field mapping:

```go
type User struct {
    Name  string `json:"name"`
    Email string `json:"email,omitempty"`
}
```

---

## Integration Notes

- **No external dependencies required** — the standard library covers HTTP, JSON, TLS, SQL, templating, and more out of the box.
- **`encoding/json/v2`** is available for improved JSON processing with separate syntactic (`jsontext`) and semantic layers.
- **`golang.org/x`** sub-repositories contain extended packages maintained by the Go team for functionality not yet in the main stdlib.
- **`internal/`** packages are for stdlib-internal use only — do not import them.
- The Go Playground at https://play.golang.org provides a zero-setup environment for testing stdlib APIs.

---

**Source:** Extracted from [pkg.go.dev/std](https://pkg.go.dev/std) and [pkg.go.dev](https://pkg.go.dev/) on June 14, 2026.

**Reference Links:**
1. [Go User Manual](https://go.dev/doc/)
2. [Effective Go](https://go.dev/doc/effective_go)
3. [Standard Library Reference](https://pkg.go.dev/std)
4. [Get Started](https://learn.go.dev/)
5. [Go Playground](https://play.golang.org)
