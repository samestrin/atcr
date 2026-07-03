Let me analyze the diff carefully for snippet-level bugs.

Let me go through the changed files:

1. `.github/workflows/refresh-synthetic-manifest.yml` - new file
2. `.planning/epics/active/16.0_quick_start.md` - planning doc
3. `.planning/technical-debt/README.md` - tech debt doc
4. `CHANGELOG.md` - changelog
5. `README.md` - readme
6. `cmd/atcr/main.go` - adds newQuickstartCmd()
7. `cmd/atcr/main_test.go` - test update
8. `cmd/atcr/quickstart.go` - new file, 308 lines
9. `cmd/atcr/quickstart_test.go` - new test file
10. `cmd/refresh-manifest/main.go` - new file
11. `internal/boundaries_test.go` - adds quickstart entry
12. `internal/quickstart/manifest.go` - new file
13. `internal/quickstart/manifest_test.go` - new test
14. `internal/quickstart/refresh.go` - new file
15. `internal/quickstart/refresh_test.go` - new test
16. `internal/quickstart/synthetic.json` - new file
17. `internal/quickstart/workflow.go` - new file
18. `internal/quickstart/workflow_test.go` - new test

Let me look for bugs:

### `cmd/atcr/quickstart.go`

Looking at `keyEnvFlow`:
```go
scanner := bufio.NewScanner(o.in)
readLine := func(prompt string) string {
    _, _ = fmt.Fprint(o.out, prompt)
    if scanner.Scan() {
        return strings.TrimSpace(scanner.Text())
    }
    return ""
}
```

This uses a single scanner. Let me check the flow:
1. `key := readLine(...)` - reads one line for the key
2. If key is empty, returns nil
3. `profile := readLine(...)` - reads another line for the profile

The `quickstartInput = "\n\n"` provides two empty lines, so this should work.

But wait - in `TestQuickstart_NoKey_PrintsInstructions`, the input is `"\n"` (just one newline). The first `readLine` reads it (empty key), returns nil. That's fine.

In `TestQuickstart_KeyEntry_ExportAndProfileAppend`, input is `"MYSECRETKEY\n" + profile + "\n"`. First readLine gets "MYSECRETKEY", second gets the profile path. OK.

Let me check `profileIsAtcrOwned`:

```go
func profileIsAtcrOwned(profile, dir string) bool {
    abs := resolveProfilePath(profile)
    if abs == "" {
        return false
    }
    if atcr, err := filepath.Abs(filepath.Join(dir, ".atcr")); err == nil {
        if abs == atcr || strings.HasPrefix(abs, atcr+string(os.PathSeparator)) {
            return true
        }
    }
    if reg, err := registry.DefaultRegistryPath(); err == nil {
        if regAbs, err := filepath.Abs(reg); err == nil && abs == regAbs {
            return true
        }
    }
    return false
}
```

This checks if the profile is inside `.atcr/` or is the registry file. But it only checks exact match for the registry, not if the profile is inside the registry's directory. That might be a gap, but probably acceptable.

Wait, there's a more interesting issue. Let me look at `resolveProfilePath`:

```go
func resolveProfilePath(profile string) string {
    if strings.HasPrefix(profile, "~/") {
        if home, err := os.UserHomeDir(); err == nil {
            profile = filepath.Join(home, profile[2:])
        }
    }
    abs, err := filepath.Abs(profile)
    if err != nil {
        return ""
    }
    return abs
}
```

And `appendExport`:
```go
func appendExport(profile, env, key string) error {
    if strings.HasPrefix(profile, "~/") {
        home, err := os.UserHomeDir()
        if err != nil {
            return err
        }
        profile = filepath.Join(home, profile[2:])
    }
    f, err := os.OpenFile(profile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
    ...
}
```

Both expand `~/`. Consistent.

Now, `profileIsAtcrOwned` uses `resolveProfilePath` which expands `~/`. But `appendExport` also expands `~/`. So the check and the write use the same expansion. Good.

But wait - there's a subtle issue. `profileIsAtcrOwned` checks against `dir` (the project dir) and the registry path. But what if the user names a profile that's inside the registry's directory but not the registry file itself? E.g., `~/.config/atcr/registry.yaml.bak`. The check would return false for that (only exact match for registry). But that's probably fine - the invariant is about not writing the key into atcr-owned files, and `.bak` files aren't really atcr-owned config.

Actually, let me think about this more carefully. The check is:
- Is it inside `.atcr/`? (prefix check)
- Is it exactly the registry file? (exact match)

But it doesn't check if it's inside the registry's directory (`~/.config/atcr/`). So `~/.config/atcr/some-other-file` would pass the check. Is that a bug? The comment says "the files whose key-free posture the wizard must preserve" - the `.atcr/` workspace and the user registry. But `~/.config/atcr/` is the registry's directory. A file like `~/.config/atcr/personas/...` would not be caught. Hmm, but this is probably LOW severity at best.

Let me look at `shellSingleQuote`:
```go
func shellSingleQuote(s string) string {
    return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
```

This is the standard POSIX single-quote escaping. Looks correct.

Now let me look at `RegistryYAML` in `manifest.go`:

```go
func RegistryYAML(m *Manifest, roster []string) string {
    var b strings.Builder
    b.WriteString("# atcr user registry — see docs/registry.md\n")
    ...
    fmt.Fprintf(&b, "  %s:\n", m.Provider.Name)
    fmt.Fprintf(&b, "    base_url: %s\n", m.Provider.BaseURL)
    fmt.Fprintf(&b, "    api_key_env: %s\n", m.Provider.APIKeyEnv)
    if len(m.Models) == 0 {
        return b.String()
    }
    b.WriteString("agents:\n")
    for i, persona := range roster {
        model := m.Models[i%len(m.Models)]
        fmt.Fprintf(&b, "  %s:\n", persona)
        fmt.Fprintf(&b, "    persona: %s\n", persona)
        fmt.Fprintf(&b, "    provider: %s\n", m.Provider.Name)
        fmt.Fprintf(&b, "    model: %s\n", model)
    }
    return b.String()
}
```

Wait - the provider name and base_url and api_key_env are written verbatim without any YAML escaping or quoting. The `validate()` method checks for control characters in model IDs but NOT in the provider name, base_url, or api_key_env. If the provider name contained a newline or special YAML characters, it could forge YAML structure.

Let me check `validate()`:
```go
func (m *Manifest) validate() error {
    switch {
    case strings.TrimSpace(m.Provider.Name) == "":
        return fmt.Errorf("synthetic manifest: provider.name is required")
    case strings.TrimSpace(m.Provider.BaseURL) == "":
        return fmt.Errorf("synthetic manifest: provider.base_url is required")
    case strings.TrimSpace(m.Provider.APIKeyEnv) == "":
        return fmt.Errorf("synthetic manifest: provider.api_key_env is required")
    case len(m.Models) == 0:
        return fmt.Errorf("synthetic manifest: at least one model is required")
    }
    for i, model := range m.Models {
        if strings.TrimSpace(model) == "" {
            return fmt.Errorf("synthetic manifest: models[%d] is empty", i)
        }
        if strings.IndexFunc(model, func(r rune) bool { return unicode.IsControl(r) }) >= 0 {
            return fmt.Errorf("synthetic manifest: models[%d] contains a control character", i)
        }
    }
    return nil
}
```

The control character check is only applied to models, not to provider name, base_url, or api_key_env. But these fields also get written verbatim into the YAML. A control character (e.g., newline) in `Provider.Name` could inject YAML structure. The comment in the code says "A control character (esp. a newline) in an id sourced from the live /models endpoint could forge YAML structure" - but the same risk exists for provider name, base_url, and api_key_env if they come from a manifest that was refreshed.

Wait, but the provider fields come from the *base* manifest (the embedded `synthetic.json`), not from the `/models` API response. The `BuildManifestFromModels` function preserves the base manifest's provider:
```go
updated := *base
updated.Models = models
```

So the provider fields are always from the embedded `synthetic.json`, not from the API. The models come from the API. So the control-char check on models makes sense as the defense against hostile API responses. The provider fields are trusted (embedded in the binary).

But wait - `RegistryYAML` is an exported function. A direct caller could pass a manifest with arbitrary provider fields. But that's a stretch - the function is in an internal package.

Actually, let me reconsider. The `BuildManifestFromModels` function calls `updated.validate()`, which validates models for control chars. But `LoadManifest` also calls `m.validate()`. So both paths validate. The provider fields are from the embedded JSON, which is trusted. This seems OK.

But there's a more subtle issue: the `validate()` function checks for control characters in models but NOT in the provider name. If someone were to modify `synthetic.json` (the embedded file) with a provider name containing a newline, it would pass validation and get written into the YAML. But that's a trusted file in the repo. Low severity at best.

Actually, let me focus on more concrete bugs.

### `SignupLink` method:

```go
func (m *Manifest) SignupLink() string {
    if strings.TrimSpace(m.Referral) == "" {
        return m.SignupURL
    }
    sep := "?"
    if strings.Contains(m.SignupURL, "?") {
        sep = "&"
    }
    return m.SignupURL + sep + "referral=" + url.QueryEscape(m.Referral)
}
```

This constructs the referral URL. If the signup URL has a fragment (e.g., `https://synthetic.new/#signup`), the `?` check would still add `?referral=...` after the fragment, which is technically wrong (query params should come before the fragment). But this is a minor issue and the current signup URL is `https://synthetic.new/` with no fragment. LOW at best.

### `WorkflowYAML`:

```go
b.WriteString("      - name: Install atcr\n")
b.WriteString("        run: go install github.com/samestrin/atcr/cmd/atcr@latest\n")
```

This uses `@latest` which is unpinned. But this is already noted in the tech debt README as a known issue. Not a new finding.

### `keyEnvFlow` - scanner issue:

Let me look more carefully at the scanner usage:

```go
scanner := bufio.NewScanner(o.in)
readLine := func(prompt string) string {
    _, _ = fmt.Fprint(o.out, prompt)
    if scanner.Scan() {
        return strings.TrimSpace(scanner.Text())
    }
    return ""
}

key := readLine(fmt.Sprintf("\nPaste your API key (or press Enter to set %s yourself later): ", env))
if key == "" {
    _, _ = fmt.Fprintf(o.out, "\nNo problem. When you have a key, set it with:\n  export %s=<your-key>\n", env)
    return nil
}

_, _ = fmt.Fprintf(o.out, "\nSet it in your current shell:\n  export %s=%s\n", env, shellSingleQuote(key))

profile := readLine("\nAppend this export to a shell profile? Enter a path (or Enter to skip): ")
```

The scanner is created once and used for both reads. This is fine - `bufio.Scanner` maintains state across `Scan()` calls.

But wait - there's a potential issue with `bufio.Scanner`'s default max token size. The default `bufio.MaxScanTokenSize` is 64KB. If a user pastes a very long API key, the scanner might fail. But API keys are typically short. LOW at best.

### `runQuickstart` - init skip logic:

```go
cfgPath := registry.DefaultProjectConfigPath(o.dir)
if _, statErr := os.Lstat(cfgPath); statErr == nil && !o.force {
    _, _ = fmt.Fprintf(o.errOut, "Using existing workspace at %s (run with --force to regenerate config + personas).\n", filepath.Dir(cfgPath))
} else if err := runInit(o.dir, o.force, o.out, o.errOut); err != nil {
    return err
}
```

Wait - this has a logic issue. The condition is:
- If config exists AND no force: skip init (use existing)
- Else (config doesn't exist OR force is true): run init

But when `force` is true AND config exists, it falls through to `else if` and runs `runInit(o.dir, o.force, ...)`. That's correct - force overwrites.

But when `force` is true AND config doesn't exist, it also falls through to `runInit`. That's also correct.

When config exists AND no force: skip init. Correct.

When config doesn't exist AND no force: falls through to `runInit`. Correct.

OK, the logic seems right.

### `writeSyntheticRegistry` - guard logic:

```go
_, statErr := os.Lstat(regPath)
switch {
case statErr == nil && !o.force:
    // Exists and no force: do not touch it.
    ...
    return nil
case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
    return fmt.Errorf("cannot check %s: %w", regPath, statErr)
}
```

The switch cases:
1. `statErr == nil && !o.force` - file exists, no force → skip
2. `statErr != nil && !errors.Is(statErr, fs.ErrNotExist)` - error other than not-exist → return error

But what about `statErr == nil && o.force`? That falls through the switch (no case matches), and continues to `os.MkdirAll` and `os.WriteFile`. That's correct - force overwrites.

What about `statErr != nil && errors.Is(statErr, fs.ErrNotExist)` (file doesn't exist)? Also falls through, continues to write. Correct.

OK, this logic is fine.

### `scaffoldWorkflow` - same pattern:

```go
_, statErr := os.Lstat(wfPath)
switch {
case statErr == nil && !o.force:
    _, _ = fmt.Fprintf(o.errOut, "\nA workflow already exists at %s — not overwriting it (use --force to replace).\n", wfPath)
    return nil
case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
    return fmt.Errorf("cannot check %s: %w", wfPath, statErr)
}
```

Same pattern, same analysis. Fine.

### `BuildManifestFromModels`:

```go
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
    ...
}
```

Wait - `updated := *base` copies the Manifest struct. But `Manifest` has a `Models []string` field (a slice). The copy copies the slice header (pointer, length, cap), not the underlying array. Then `updated.Models = models` replaces the slice header, so there's no aliasing issue. The `Provider` field is a value type (struct), so that's copied too. OK, no issue here.

But wait - `updated.Models = models` replaces the models. Then `updated.validate()` validates the new models (checking for control chars). But the provider fields are from `base`, which was already validated by `LoadManifest`. So the provider fields should be fine.

Actually, hold on. Let me re-read `validate()`:

```go
func (m *Manifest) validate() error {
    switch {
    case strings.TrimSpace(m.Provider.Name) == "":
        return fmt.Errorf("synthetic manifest: provider.name is required")
    case strings.TrimSpace(m.Provider.BaseURL) == "":
        return fmt.Errorf("synthetic manifest: provider.base_url is required")
    case strings.TrimSpace(m.Provider.APIKeyEnv) == "":
        return fmt.Errorf("synthetic manifest: provider.api_key_env is required")
    case len(m.Models) == 0:
        return fmt.Errorf("synthetic manifest: at least one model is required")
    }
    for i, model := range m.Models {
        if strings.TrimSpace(model) == "" {
            return fmt.Errorf("synthetic manifest: models[%d] is empty", i)
        }
        if strings.IndexFunc(model, func(r rune) bool { return unicode.IsControl(r) }) >= 0 {
            return fmt.Errorf("synthetic manifest: models[%d] contains a control character", i)
        }
    }
    return nil
}
```

The control character check is only on models. But `RegistryYAML` writes `m.Provider.Name`, `m.Provider.BaseURL`, and `m.Provider.APIKeyEnv` verbatim into YAML without quoting. If any of these contained a newline, it would forge YAML structure. But these come from the embedded `synthetic.json` (trusted) or from `base` in `BuildManifestFromModels` (which is the embedded manifest). So they're trusted.

However, `RegistryYAML` is an exported function. A test or direct caller could pass a manifest with a provider name containing a newline. But it's an internal package, so only code within the repo calls it. LOW at best.

### Let me look at the refresh workflow YAML more carefully:

```yaml
      - name: Fetch models and regenerate manifest
        if: steps.guard.outputs.proceed == 'true'
        env:
          LLM_SYNTHETIC_API_KEY: ${{ secrets.LLM_SYNTHETIC_API_KEY }}
        run: |
          set -euo pipefail
          curl -sSf https://api.synthetic.new/openai/v1/models \
            -H "Authorization: Bearer ${LLM_SYNTHETIC_API_KEY}" > models.json
          go run ./cmd/refresh-manifest < models.json > synthetic.json.tmp
          mv synthetic.json.tmp internal/quickstart/synthetic.json
          rm -f models.json
```

Wait - `go run ./cmd/refresh-manifest < models.json > synthetic.json.tmp` - this redirects stdin from `models.json` and stdout to `synthetic.json.tmp`. But the `cmd/refresh-manifest/main.go` does:

```go
func main() {
    os.Exit(quickstart.RunRefresh(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
```

So `os.Stdin` reads from `models.json` and `os.Stdout` writes to `synthetic.json.tmp`. That's correct.

But there's a subtle issue: `go run` compiles and runs the package. The `//go:embed synthetic.json` directive in `manifest.go` embeds the current `synthetic.json` file at compile time. But the comment in the workflow says:

```
# Write to a temp file, THEN move into place. Redirecting straight onto
# synthetic.json would truncate it before `go run` compiles the package
# that //go:embeds it, so the refresh would embed (and re-emit) an empty
# manifest and fail on every run.
```

So they're aware of this. They write to `synthetic.json.tmp` first, then `mv` it into place. But wait - `go run ./cmd/refresh-manifest` compiles the package, which embeds `internal/quickstart/synthetic.json`. At that point, `synthetic.json` hasn't been modified yet (it's still the committed version). So `go run` embeds the current committed manifest, reads `models.json` from stdin, and writes the refreshed manifest to stdout (which goes to `synthetic.json.tmp`). Then `mv` replaces the committed file. This is correct.

But actually, there's a timing issue. `go run ./cmd/refresh-manifest` compiles the `internal/quickstart` package (which has the `//go:embed`). The compilation happens before the program runs. At compile time, `synthetic.json` is the committed version. So the embedded manifest is the committed one. Then the program runs, reads `/models` response from stdin, and writes the refreshed manifest. This is fine.

### Let me look at `osc8`:

```go
func osc8(url string) string {
    return "\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\"
}
```

OSC 8 hyperlink format: `ESC ] 8 ; params ; URI ESC \`. The params field is empty here (just `;;`). This is valid. The visible text is between the two OSC 8 sequences. Looks correct.

But wait - the parameter `url` is used both as the URI and the visible text. If the URL contains escape sequences, it could break the terminal. But the URL comes from the manifest (`SignupLink()`), which is from the embedded `synthetic.json`. Trusted. LOW at best.

### Let me look at `openBrowser`:

```go
func openBrowser(url string) error {
    switch runtime.GOOS {
    case "darwin":
        return exec.Command("open", url).Start()
    case "windows":
        return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
    default:
        return exec.Command("xdg-open", url).Start()
    }
}
```

This uses `Start()` not `Run()`, so it doesn't wait for the browser. The comment says "returns promptly". But `Start()` leaves the process running. If the wizard exits, the child process might be killed (if it's in the same process group) or might continue. This is probably fine for a browser opener.

But there's a potential issue: `exec.Command("xdg-open", url).Start()` on Linux. If `xdg-open` is not installed, this returns an error. The caller handles the error:
```go
if err := openFn(link); err != nil {
    _, _ = fmt.Fprintf(o.errOut, "could not open browser (%v) — open the link above manually.\n", err)
}
```

OK, error is handled. Fine.

### Let me look at the test file `quickstart_test.go` more carefully:

```go
func TestQuickstart_KeyEntry_ExportAndProfileAppend(t *testing.T) {
    dir := t.TempDir()
    home := t.TempDir()
    t.Setenv("HOME", home)
    profile := filepath.Join(home, "profile.sh")

    out := &bytes.Buffer{}
    // Paste a key, then name a shell profile to append the export to.
    in := strings.NewReader("MYSECRETKEY\n" + profile + "\n")
    require.NoError(t, runQuickstart(quickstartOpts{
        dir: dir, in: in, out: out, errOut: &bytes.Buffer{},
    }))

    // The export instruction is shown to the user.
    assert.Contains(t, out.String(), "export LLM_SYNTHETIC_API_KEY=")

    // The chosen shell profile received the export with the key value.
    prof, err := os.ReadFile(profile)
    require.NoError(t, err)
    assert.Contains(t, string(prof), "export LLM_SYNTHETIC_API_KEY='MYSECRETKEY'")
    ...
}
```

Wait - this test uses `filepath.Join(home, "profile.sh")` as the profile path. But `profileIsAtcrOwned` checks if the profile is inside `.atcr/` or is the registry file. `home/profile.sh` is neither, so it passes. Then `appendExport` is called, which expands `~/` (but this path doesn't start with `~/`, it's an absolute path). So it opens the file and appends. This should work.

But wait - the test sets `HOME` to `home`, but the profile path is `filepath.Join(home, "profile.sh")` which is an absolute path. `appendExport` checks `strings.HasPrefix(profile, "~/")` - since it's an absolute path, this is false. So it opens the file directly. OK.

But `resolveProfilePath` in `profileIsAtcrOwned` also checks `~/` - since the profile is absolute, it skips the expansion and calls `filepath.Abs(profile)`. Since it's already absolute, `filepath.Abs` returns it as-is. Then it checks against `.atcr/` and the registry path. Neither matches. So `profileIsAtcrOwned` returns false. Correct.

### Let me look at `TestQuickstart_KeyEntry_RefusesAtcrOwnedProfilePath`:

```go
func TestQuickstart_KeyEntry_RefusesAtcrOwnedProfilePath(t *testing.T) {
    dir := t.TempDir()
    t.Setenv("HOME", t.TempDir())
    atcrCfg := filepath.Join(dir, ".atcr", "config.yaml")

    errOut := &bytes.Buffer{}
    // User pastes a key, then (foolishly) names an atcr-owned file as the profile.
    in := strings.NewReader("MYSECRETKEY\n" + atcrCfg + "\n")
    require.NoError(t, runQuickstart(quickstartOpts{
        dir: dir, in: in, out: &bytes.Buffer{}, errOut: errOut,
    }))

    assert.Contains(t, errOut.String(), "Refusing to write the key")
    // INVARIANT: the key value must not have been appended to the atcr config.
    cfg, err := os.ReadFile(atcrCfg)
    require.NoError(t, err)
    assert.NotContains(t, string(cfg), "MYSECRETKEY", "key never written into an atcr-owned file")
}
```

Wait - this test passes `atcrCfg` as the profile path. But `atcrCfg` is `filepath.Join(dir, ".atcr", "config.yaml")`. The `profileIsAtcrOwned` function checks:

```go
if atcr, err := filepath.Abs(filepath.Join(dir, ".atcr")); err == nil {
    if abs == atcr || strings.HasPrefix(abs, atcr+string(os.PathSeparator)) {
        return true
    }
}
```

`abs` is the resolved profile path (`dir/.atcr/config.yaml`), and `atcr` is `dir/.atcr`. The prefix check: `strings.HasPrefix(abs, atcr+string(os.PathSeparator))` → `strings.HasPrefix("dir/.atcr/config.yaml", "dir/.atcr/")` → true. So `profileIsAtcrOwned` returns true, and the key is refused. Correct.

But wait - the test also needs `config.yaml` to exist for `os.ReadFile(atcrCfg)` to work. The test runs `runQuickstart`, which calls `runInit` (since no config exists yet), which creates `.atcr/config.yaml`. So the file exists after `runQuickstart`. But `profileIsAtcrOwned` returned true, so `appendExport` was never called. The file was created by `runInit`, not by `appendExport`. So `os.ReadFile(atcrCfg)` reads the init-created config, which doesn't contain "MYSECRETKEY". The assertion passes. Correct.

### Now let me look at a potential issue in `keyEnvFlow`:

When the user enters a key but then the profile path is atcr-owned:

```go
if profileIsAtcrOwned(profile, o.dir) {
    _, _ = fmt.Fprintf(o.errOut, "Refusing to write the key into an atcr-owned file (%s) — choose a shell profile like ~/.zshrc instead.\n", profile)
    return nil
}
```

This returns nil (no error), but the key was already echoed to stdout:
```go
_, _ = fmt.Fprintf(o.out, "\nSet it in your current shell:\n  export %s=%s\n", env, shellSingleQuote(key))
```

So the key is shown in the output but not written to any file. That's fine - the key was already in the user's paste buffer anyway.

### Let me look at `appendExport` more carefully:

```go
func appendExport(profile, env, key string) error {
    if strings.HasPrefix(profile, "~/") {
        home, err := os.UserHomeDir()
        if err != nil {
            return err
        }
        profile = filepath.Join(home, profile[2:])
    }
    f, err := os.OpenFile(profile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
    if err != nil {
        return err
    }
    defer func() { _ = f.Close() }()
    _, err = fmt.Fprintf(f, "\n# added by atcr quickstart\nexport %s=%s\n", env, shellSingleQuote(key))
    return err
}
```

The file is opened with `O_APPEND|O_CREATE|O_WRONLY` and mode `0o600`. This means the file is created if it doesn't exist, with permissions `0o600` (owner read/write only). If it exists, it's opened for appending. The key is written with `shellSingleQuote` which escapes single quotes. The `env` is written verbatim. If `env` contained shell metacharacters, this could be an issue. But `env` comes from the manifest's `APIKeyEnv`, which is `LLM_SYNTHETIC_API_KEY` - a trusted, simple env var name. LOW at best.

### Let me look at `RegistryYAML` more carefully for YAML injection:

```go
fmt.Fprintf(&b, "  %s:\n", m.Provider.Name)
fmt.Fprintf(&b, "    base_url: %s\n", m.Provider.BaseURL)
fmt.Fprintf(&b, "    api_key_env: %s\n", m.Provider.APIKeyEnv)
```

These write the provider name, base_url, and api_key_env verbatim. If any of these contained a newline, it would inject YAML lines. But `validate()` only checks for control characters in models, not in provider fields. 

Wait, but the provider fields come from the embedded `synthetic.json` (trusted) or from `base` in `BuildManifestFromModels` (which is the embedded manifest). So they're trusted.

But `RegistryYAML` is exported. A test could call it with arbitrary values. But it's an internal package. Hmm.

Actually, let me re-examine. In `BuildManifestFromModels`:
```go
updated := *base
updated.Models = models
if err := updated.validate(); err != nil {
    return nil, err
}
```

`base` is from `LoadManifest()`, which loads the embedded `synthetic.json`. So `updated.Provider` is from the embedded file. The `validate()` call checks that provider fields are non-empty but doesn't check for control characters. But since they come from the embedded file, they're trusted.

However, the output of `BuildManifestFromModels` is written to `synthetic.json` (replacing the embedded file). On the next run, `LoadManifest` would load this refreshed file. If a hostile `/models` response somehow modified the provider fields... but it can't, because `BuildManifestFromModels` only replaces `Models`, not `Provider`. So the provider fields are always from the original embedded file. OK, this is fine.

### Let me look at the `modelsResponse` struct:

```go
type modelsResponse struct {
    Data []struct {
        ID string `json:"id"`
    } `json:"data"`
}
```

This only extracts the `id` field from each model entry. The `/models` response might have other fields, but they're ignored. This is fine - we only need the IDs.

### Let me look at `TestBuildManifestFromModels_RejectsControlCharInId`:

```go
func TestBuildManifestFromModels_RejectsControlCharInId(t *testing.T) {
    resp := []byte(`{"data":[{"id":"ok"},{"id":"evil\n    injected: true"}]}`)
    _, err := BuildManifestFromModels(resp, baseManifest())
    assert.Error(t, err)
}
```

Wait - the JSON string `"evil\n    injected: true"` contains a literal newline. In JSON, `\n` is an escape sequence for a newline character. So the model ID would be `evil\n    injected: true` (with a real newline). The `validate()` function checks for control characters:
```go
if strings.IndexFunc(model, func(r rune) bool { return unicode.IsControl(r) }) >= 0 {
    return fmt.Errorf("synthetic manifest: models[%d] contains a control character", i)
}
```

A newline (`\n`) is a control character, so this would be rejected. The test expects an error. Correct.

But wait - let me think about this more carefully. The JSON string `{"id":"evil\n    injected: true"}` - in Go, the raw string literal `` `{"id":"evil\n    injected: true"}` `` does NOT interpret `\n` as a newline. It's a literal backslash followed by 'n'. So the JSON parser would see the string `evil\n    injected: true` where `\n` is a JSON escape sequence for a newline. So the parsed model ID would be `evil<newline>    injected: true`. The control character check would catch the newline. Correct.

Actually wait, let me re-read. The test uses a raw string literal:
```go
resp := []byte(`{"data":[{"id":"ok"},{"id":"evil\n    injected: true"}]}`)
```

In a raw string literal (backticks), `\n` is two characters: backslash and 'n'. But in JSON, `\n` is an escape sequence for a newline. So when `json.Unmarshal` parses this, it converts `\n` to an actual newline character. So the model ID is `evil<newline>    injected: true`. The control character check catches the newline. The test expects an error. Correct.

### Let me look at `RunRefresh`:

```go
func RunRefresh(args []string, in io.Reader, out, errOut io.Writer) int {
    base, err := LoadManifest()
    if err != nil {
        _, _ = fmt.Fprintln(errOut, "refresh:", err)
        return 1
    }
    apiResp, err := io.ReadAll(in)
    if err != nil {
        _, _ = fmt.Fprintln(errOut, "refresh: reading input:", err)
        return 1
    }
    result, err := BuildManifestFromModels(apiResp, base)
    if err != nil {
        _, _ = fmt.Fprintln(errOut, "refresh:", err)
        return 1
    }
    if _, err := out.Write(result); err != nil {
        _, _ = fmt.Fprintln(errOut, "refresh: writing output:", err)
        return 1
    }
    return 0
}
```

The `args` parameter is unused. The function reads from `in`, processes, and writes to `out`. This is fine - `args` might be for future use (e.g., flags).

### Let me look at the GitHub workflow YAML more carefully:

```yaml
      - name: Open PR on change
        if: steps.guard.outputs.proceed == 'true'
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          set -euo pipefail
          if git diff --quiet -- internal/quickstart/synthetic.json; then
            echo "Manifest unchanged — nothing to do."
            exit 0
          fi
          branch="chore/refresh-synthetic-manifest-${GITHUB_RUN_ID}"
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git checkout -b "$branch"
          git add internal/quickstart/synthetic.json
          git commit -m "chore(quickstart): refresh synthetic manifest"
          git push -u origin "$branch"
          gh pr create \
            --title "chore: refresh synthetic manifest" \
            --body "Automated refresh of the bundled synthetic model manifest from the live /models endpoint." \
            --base main --head "$branch"
```

Wait - `git diff --quiet -- internal/quickstart/synthetic.json` checks if the file has changed. But the previous step did `mv synthetic.json.tmp internal/quickstart/synthetic.json`. The `git diff` would show the change. If the manifest is unchanged (same content), `git diff --quiet` returns 0 (no changes), and the step exits 0