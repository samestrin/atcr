Let me carefully analyze this diff for snippet-level bugs. I need to focus on the changed regions and look for off-by-one errors, null/nil/undefined dereferences, logic inversions, copy-paste errors, and shadowing/dead paths.

Let me go through the key changes:

1. **cmd/atcr/init.go** - Changed `builtins.Names()` to `nil` for roster, and added nil-roster derivation logic in `installCommunityPersonas`.

2. **cmd/atcr/main.go** - Added `newModelsCmd()` to the command list.

3. **cmd/atcr/main_test.go** - Updated subcommand count from 21 to 22.

4. **cmd/atcr/personas.go** - Added resolved-lock reporting cases in the switch statement.

5. **cmd/atcr/personas_test.go** - Added new tests.

6. **cmd/atcr/quickstart.go** - Changed `builtins.Names()` to `nil`.

7. **cmd/atcr/quickstart_test.go** - Added new tests.

8. **docs/personas-authoring.md** - Added section 6 about bindings.

9. **docs/personas-install.md** - Added sections about models commands.

10. **internal/fanout/lock_test.go** - New test file.

11. **internal/personas/binding_test.go** - New test file.

12. **internal/personas/community_schema_test.go** - Added tests.

13. **internal/personas/drift_test.go** - New test file.

14. **internal/personas/search.go** - Added `Binding` field to `PersonaIndexEntry`.

15. **internal/personas/snapshot.go** - New file with snapshot loading/marshaling.

16. **internal/personas/testdata/catalog_snapshot.json** - New fixture.

17. **internal/registry/binding_test.go** - New test file.

18. **internal/registry/config.go** - Added `Binding` field to `AgentConfig`.

Let me look more carefully at the code changes for bugs:

### cmd/atcr/personas.go - The switch statement

```go
switch {
case res.Resolved && res.FixtureBlocked:
    // ...
case res.Resolved && res.SlugChanged && dryRun:
    // ...
case res.Resolved && res.SlugChanged:
    // ...
case res.Resolved:
    // Resolution ran but the lock did not advance — report explicitly, never omit.
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: %s (unchanged)\n", name, res.ToSlug)
case res.UpToDate:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already up to date (%s)\n", name, res.ToVersion)
case dryRun:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "Would upgrade %s: %s → %s\n", name, res.FromVersion, res.ToVersion)
default:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "Upgraded %s: %s → %s\n", name, res.FromVersion, res.ToVersion)
}
// A major-version jump always surfaces the verify flag...
if res.MajorJump {
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "  ⚠ prompt tuned for the prior major — verify\n")
}
```

Wait, let me think about the ordering of cases here. The switch is a `switch{}` (tagless), so cases are evaluated top to bottom. Let me check if there's a logic issue:

1. `res.Resolved && res.FixtureBlocked` - blocked case
2. `res.Resolved && res.SlugChanged && dryRun` - dry run with change
3. `res.Resolved && res.SlugChanged` - real run with change
4. `res.Resolved` - resolved but unchanged
5. `res.UpToDate` - up to date (non-resolved path)
6. `dryRun` - dry run (non-resolved path)
7. `default` - real run upgrade (non-resolved path)

This looks correct to me. The `res.Resolved` cases come first, then the non-resolved cases.

But wait - what about the case where `res.Resolved && res.FixtureBlocked && dryRun`? The first case catches it (FixtureBlocked takes priority over dryRun). Is that the intended behavior? Looking at AC 04-03 Error Scenario 1: "A blocked major-version bump is reported with its would-be slug and blocking reason in both dry-run and real-run modes, without writing the lock." So yes, blocked should be reported in both modes. This seems correct.

What about `res.Resolved && !res.SlugChanged && dryRun`? Case 4 catches it (`res.Resolved` alone), printing "(unchanged)". That seems right.

### internal/personas/snapshot.go

```go
func SnapshotModels() ([]CatalogModel, error) {
    data := embeddedSnapshot
    if p := strings.TrimSpace(os.Getenv(envCatalogSnapshot)); p != "" {
        d, err := os.ReadFile(p)
        if err != nil {
            return nil, fmt.Errorf("failed to load catalog snapshot: %w", err)
        }
        data = d
    }
    var resp catalogResponse
    if err := json.Unmarshal(data, &resp); err != nil {
        return nil, fmt.Errorf("failed to parse catalog snapshot: %w", err)
    }
    return resp.Data, nil
}
```

This looks fine. The embedded snapshot is used by default, and the env var overrides it.

```go
func MarshalSnapshot(models []CatalogModel) ([]byte, error) {
    out := struct {
        Meta snapshotMeta       `json:"_fixture_meta"`
        Data []snapshotModelOut `json:"data"`
    }{
        Meta: snapshotMeta{
            Note:    "...",
            Fetched: time.Now().UTC().Format("2006-01-02"),
            Source:  strings.TrimRight(CatalogBaseURL, "/") + "/models",
        },
        Data: make([]snapshotModelOut, len(models)),
    }
    for i, m := range models {
        out.Data[i] = snapshotModelOut(m)
    }
    b, err := json.MarshalIndent(out, "", "  ")
    if err != nil {
        return nil, err
    }
    return append(b, '\n'), nil
}
```

The struct conversion `snapshotModelOut(m)` - this works if the fields are in the same order and types match. Let me check: `CatalogModel` has `ID`, `CanonicalSlug`, `Created`, `ExpirationDate` and `snapshotModelOut` has the same fields with the same types. The conversion should work since they have identical field layouts (the struct conversion in Go requires identical field names, types, and order).

Wait, I don't see the `CatalogModel` definition in this diff. It's referenced but not shown. The comment says "CatalogModel and snapshotModelOut share an identical field layout (the latter only adds JSON tags), so a struct conversion is exact." I'll assume this is correct based on the comment.

### internal/personas/search.go - PersonaIndexEntry

```go
type PersonaIndexEntry struct {
    Name        string `json:"name"`
    Version     string `json:"version"`
    Description string `json:"description"`
    Path        string `json:"path"`
    Provider    string `json:"provider,omitempty"`
    Model       string `json:"model,omitempty"`
    Binding string   `json:"binding,omitempty"`
    Tasks   []string `json:"tasks,omitempty"`
    Tags    []string `json:"tags,omitempty"`
}
```

The field alignment is a bit off (gofmt would fix it), but that's not a bug. The struct looks correct.

### cmd/atcr/init.go - installCommunityPersonas

```go
if roster == nil {
    roster = make([]string, 0, len(entries))
    for _, e := range entries {
        roster = append(roster, e.Name)
    }
}
```

This derives the roster from the fetched index entries. Looks correct.

### internal/personas/drift_test.go

Let me look at the test for `TestCheckDrift_BindinglessNewerMember_SameTierOnly`:

```go
func TestCheckDrift_BindinglessNewerMember_SameTierOnly(t *testing.T) {
    models := []CatalogModel{
        {ID: "vendor/fam-1.0", Created: 100},
        {ID: "vendor/fam-2.0", Created: 200},
        {ID: "vendor/fam-3.0-mini", Created: 900}, // sibling tier, must not bleed
    }
    f := CheckDrift([]InstalledLock{{Name: "p", Model: "vendor/fam-1.0"}}, models)
    require.Len(t, f, 1)
    assert.Equal(t, ConditionNewerMember, f[0].Condition)
    assert.Equal(t, "vendor/fam-2.0", f[0].SuggestedSlug) // not fam-3.0-mini
    assert.Equal(t, "vendor/fam", f[0].Family)
    assert.Equal(t, "stable", f[0].Channel)
}
```

This test asserts that `vendor/fam-3.0-mini` (created: 900) is NOT selected as the newer member because it's a "sibling tier" — presumably because "mini" is a different tier. But wait, how does `CheckDrift` know that "fam-3.0-mini" is a different tier from "fam-1.0"? The test says "sibling tier, must not bleed" but I don't see the `CheckDrift` implementation in this diff. The test expects `vendor/fam-2.0` (created: 200) to be the suggested slug, not `vendor/fam-3.0-mini` (created: 900). 

This is a test, not the implementation. The implementation of `CheckDrift` is not shown in this diff, so I can't verify if the logic is correct. But the test itself seems to be testing specific behavior.

### internal/fanout/lock_test.go

```go
func lockCfg(srvURL string) *ReviewConfig {
    reg := &registry.Registry{
        Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: srvURL}},
        Agents: map[string]registry.AgentConfig{
            "greta": {Provider: "p", Model: "model-greta", Binding: "binding-greta", Persona: "greta", Temperature: ptrF(0.7)},
        },
    }
    return &ReviewConfig{
        Registry:    reg,
        Project:     &registry.ProjectConfig{Agents: []string{"greta"}},
        Settings:    registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
        PersonaDirs: registry.PersonaDirs{},
    }
}
```

This uses `ptrF` which I assume is a helper that returns a pointer to a float64. Let me check if it's defined... It's not shown in this diff, but it's likely a pre-existing helper in the fanout test package.

```go
func TestReviewPath_ZeroCatalogEndpointToResolveModel(t *testing.T) {
    // ...
    for _, p := range paths {
        assert.Equal(t, "/chat/completions", p,
            "review path must issue ONLY the completion call — any other endpoint (catalog/resolution) breaks reproducibility (got %q)", p)
    }
}
```

This asserts every path is `/chat/completions`. This is a positive allowlist approach, which is good.

### cmd/atcr/personas_test.go

```go
func TestPersonasUpgrade_BindingSlugReport(t *testing.T) {
    cat := `{"data":[` +
        `{"id":"deepseek/deepseek-v4.0","canonical_slug":"deepseek/deepseek-v4.0","created":1700000000,"expiration_date":null},` +
        `{"id":"deepseek/deepseek-v4.1","canonical_slug":"deepseek/deepseek-v4.1","created":1780000000,"expiration_date":null}]}`
    srv := personasTestServer(t, map[string]string{"/models": cat})
    dir := withPersonasEnv(t, srv)
    t.Setenv("ATCR_CATALOG_URL", srv.URL)
    // ...
}
```

Wait, this test sets `ATCR_CATALOG_URL` to `srv.URL`. But the test server is created with `personasTestServer` which serves `/models` with the catalog. The test expects the catalog to be fetched from the test server. But the `ATCR_CATALOG_URL` env var is set AFTER `withPersonasEnv`. Let me check if `withPersonasEnv` already sets the catalog URL... I can't see the implementation of `withPersonasEnv` in this diff, but it likely sets `ATCR_PERSONAS_URL`. The test additionally sets `ATCR_CATALOG_URL` to point the catalog client at the same test server. This seems intentional.

### internal/personas/community_schema_test.go

```go
func TestPinnedModelIsLockZeroMigration(t *testing.T) {
    names := builtins.CommunityNames()
    require.NotEmpty(t, names)
    for _, name := range names {
        t.Run(name, func(t *testing.T) {
            data, err := os.ReadFile(filepath.Join(communityYAMLRoot(), name+".yaml"))
            // ...
            var ac registry.AgentConfig
            require.NoError(t, yaml.Unmarshal(data, &ac))
            require.NotEmptyf(t, ac.Model, "persona %q pinned model must serve as the initial lock", name)

            var rawKeys struct {
                Binding string `yaml:"binding"`
            }
            require.NoError(t, yaml.Unmarshal(data, &rawKeys))
            if rawKeys.Binding == "" {
                require.Emptyf(t, ac.Binding,
                    "persona %q ships no binding, so Binding must decode as \"\" (inert) — model lock alone is the lock", name)
            }
        })
    }
}
```

This test reads community persona YAML files and checks that the model field is non-empty. It also checks that if the YAML has no `binding:` key, the `AgentConfig.Binding` decodes as empty. This looks correct.

### Now let me look for actual bugs more carefully:

Let me re-examine the `personas.go` switch statement more carefully:

```go
switch {
case res.Resolved && res.FixtureBlocked:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "Blocked %s: %s → %s not applied — major version jump; %s (lock unchanged)\n", name, res.FromSlug, res.ToSlug, res.FixtureReason)
case res.Resolved && res.SlugChanged && dryRun:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "Would upgrade %s: %s → %s\n", name, res.FromSlug, res.ToSlug)
case res.Resolved && res.SlugChanged:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "Upgraded %s: %s → %s\n", name, res.FromSlug, res.ToSlug)
case res.Resolved:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: %s (unchanged)\n", name, res.ToSlug)
case res.UpToDate:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already up to date (%s)\n", name, res.ToVersion)
case dryRun:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "Would upgrade %s: %s → %s\n", name, res.FromVersion, res.ToVersion)
default:
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "Upgraded %s: %s → %s\n", name, res.FromVersion, res.ToVersion)
}
if res.MajorJump {
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "  ⚠ prompt tuned for the prior major — verify\n")
}
```

Wait, there's a potential issue here. The `MajorJump` flag is printed AFTER the switch, unconditionally for any case. But consider: if `res.Resolved && res.FixtureBlocked` is true, the blocked message is printed, AND then if `res.MajorJump` is true, the verify flag is also printed. Is that the intended behavior?

Looking at AC 06-01: "A major-version jump always surfaces the verify flag, independent of the fixture outcome: a passing fixture proves the template renders, never that the prompt is still well-tuned for the new major." So yes, the verify flag should appear even when the fixture blocks. This seems correct.

But what about the non-resolved path? If `res.UpToDate` is true and `res.MajorJump` is true... can that happen? If the persona is up to date, there's no version jump, so `MajorJump` should be false. But what if `MajorJump` is true but the resolved path didn't run? The `MajorJump` flag is checked outside the switch, so it would print regardless. This could be a bug if `MajorJump` is true on a non-resolved path. But logically, `MajorJump` should only be true when `res.Resolved` is true (since the major jump is detected during resolution). I'll note this as a potential issue but it's likely not a real bug because `MajorJump` is only set during resolution.

Actually wait - let me reconsider. The non-resolved path (cases 5-7) handles the old-style version-string comparison. Could `MajorJump` be true on that path? If `MajorJump` is only set by the resolver (the resolved path), then it should be false on the non-resolved path. But if someone sets it incorrectly... this is more of a defensive concern than a bug.

Let me look at the `TestPersonasUpgrade_AllDryRunReportsNoWrite` test more carefully:

```go
func TestPersonasUpgrade_AllDryRunReportsNoWrite(t *testing.T) {
    cat := `{"data":[` +
        `{"id":"deepseek/deepseek-v4.0","canonical_slug":"deepseek/deepseek-v4.0","created":1700000000,"expiration_date":null},` +
        `{"id":"deepseek/deepseek-v4.1","canonical_slug":"deepseek/deepseek-v4.1","created":1780000000,"expiration_date":null}]}`
    srv := personasTestServer(t, map[string]string{"/models": cat})
    dir := withPersonasEnv(t, srv)
    t.Setenv("ATCR_CATALOG_URL", srv.URL)
    changing := `provider: openrouter
model: deepseek/deepseek-v4.0
role: reviewer
binding: deepseek@stable
version: "1.0.0"
`
    unchanged := `provider: openrouter
model: deepseek/deepseek-v4.1
role: reviewer
binding: deepseek@stable
version: "1.0.0"
`
    pa := filepath.Join(dir, "vendor", "aa.yaml")
    pb := filepath.Join(dir, "vendor", "bb.yaml")
    require.NoError(t, os.MkdirAll(filepath.Dir(pa), 0o755))
    require.NoError(t, os.WriteFile(pa, []byte(changing), 0o644))
    require.NoError(t, os.WriteFile(pb, []byte(unchanged), 0o644))

    out, err := execute(t, "personas", "upgrade", "--all", "--dry-run")
    require.NoError(t, err)
    assert.Contains(t, out, "deepseek/deepseek-v4.0 → deepseek/deepseek-v4.1")
    assert.Contains(t, out, "(unchanged)")

    gotA, _ := os.ReadFile(pa)
    assert.Equal(t, changing, string(gotA), "dry-run must not write the changing persona")
    gotB, _ := os.ReadFile(pb)
    assert.Equal(t, unchanged, string(gotB), "dry-run must not write the unchanged persona")
}
```

This test creates two personas with `binding: deepseek@stable`. The "changing" one is locked at v4.0, and the catalog has v4.1 as newer. The "unchanged" one is already at v4.1. The test asserts the dry-run reports the change and doesn't write.

But wait - the test uses `--all` which should enumerate all installed community personas. But the personas are in `dir/vendor/aa.yaml` and `dir/vendor/bb.yaml`. Does `--all` enumerate from `dir`? I can't see the `installedCommunityNames` implementation, but the test seems to expect it to find these two personas. This should be fine if `installedCommunityNames` walks the dir.

Now let me look at the catalog snapshot fixture more carefully:

```json
{ "id": "deepseek/deepseek-v5-pro", "canonical_slug": "deepseek/deepseek-v5-pro", "created": 1785000000, "expiration_date": "2027-06-30" },
{ "id": "deepseek/deepseek-v4-pro", "canonical_slug": "deepseek/deepseek-v4-pro", "created": 1777000679, "expiration_date": null },
{ "id": "deepseek/deepseek-v3.2-exp", "canonical_slug": "deepseek/deepseek-v3.2-exp", "created": 1759150481, "expiration_date": null },
{ "id": "deepseek/deepseek-legacy", "canonical_slug": "deepseek/deepseek-legacy", "created": 0, "expiration_date": null },
```

The `deepseek-v5-pro` has the newest `created` timestamp but has an `expiration_date` of "2027-06-30". Under `@stable`, this should be excluded (non-null expiration_date), and `deepseek-v4-pro` should be selected. This is correct per the AC.

But wait - `deepseek-v3.2-exp` has "exp" in its name. Under `@stable`, this should also be excluded (preview/beta/exp token). So the eligible entries under `@stable` for `deepseek/` are:
- `deepseek-v4-pro` (created: 1777000679, no expiration, no preview token) ✓
- `deepseek-legacy` (created: 0, no expiration, no preview token) ✓

And `deepseek-v5-pro` is excluded by expiration_date, `deepseek-v3.2-exp` is excluded by the "exp" token. So the newest eligible is `deepseek-v4-pro`. This seems correct.

Now let me look at the z-ai entries:

```json
{ "id": "z-ai/glm-5.2", "canonical_slug": "z-ai/glm-5.2", "created": 1781631930, "expiration_date": null },
{ "id": "z-ai/glm-4.5", "canonical_slug": "z-ai/glm-4.5", "created": 1760000000, "expiration_date": "2026-12-31" },
{ "id": "z-ai/glm-5v-turbo", "canonical_slug": "z-ai/glm-5v-turbo", "created": 1782000000, "expiration_date": "2098-12-31" },
```

Under `@stable`:
- `glm-5.2` (created: 1781631930, no expiration) ✓
- `glm-4.5` (created: 1760000000, expiration: 2026-12-31) ✗ (expiring)
- `glm-5v-turbo` (created: 1782000000, expiration: 2098-12-31) ✗ (expiring)

So `glm-5.2` is the newest eligible. But wait, `glm-5v-turbo` has a higher `created` (1782000000 > 1781631930) but is excluded by expiration_date. So `glm-5.2` should be selected. This seems correct.

But in the drift test:

```go
func TestCheckDrift_BoundPersona_NewerMemberViaResolver(t *testing.T) {
    models, err := SnapshotModels()
    require.NoError(t, err)
    // z-ai/glm-4.5 is deprecated in the snapshot; the glm@stable binding resolves
    // the newest stable z-ai/ member (glm-5.2).
    f := CheckDrift([]InstalledLock{{Name: "glenna", Model: "z-ai/glm-4.5", Binding: "glm@stable"}}, models)
    byCond := map[string]DriftFinding{}
    for _, x := range f {
        byCond[x.Condition] = x
    }
    nm, ok := byCond[ConditionNewerMember]
    require.True(t, ok, "expected a newer-member finding")
    assert.Equal(t, "z-ai/glm-5.2", nm.SuggestedSlug)
    assert.Equal(t, "glm", nm.Family)
    assert.Equal(t, "stable", nm.Channel)
    _, dep := byCond[ConditionDeprecation]
    assert.True(t, dep, "expected a deprecation finding")
}
```

This test expects both a newer-member finding AND a deprecation finding for the same persona. The locked slug is `z-ai/glm-4.5` which has `expiration_date: "2026-12-31"` (deprecation), and the binding `glm@stable` resolves to `z-ai/glm-5.2` (newer member). So both conditions should be reported. This seems correct.

Now, let me look at the `TestCheckDrift_DeterministicOrder` test:

```go
func TestCheckDrift_DeterministicOrder(t *testing.T) {
    models := []CatalogModel{
        {ID: "vendor/fam-1.0", Created: 100, ExpirationDate: strptr("2026-01-01")},
        {ID: "vendor/fam-2.0", Created: 200},
    }
    locks := []InstalledLock{{Name: "p", Model: "vendor/fam-1.0"}}
    first := CheckDrift(locks, models)
    // newer-member before deprecation, stable across repeated calls.
    require.Len(t, first, 2)
    assert.Equal(t, ConditionNewerMember, first[0].Condition)
    assert.Equal(t, ConditionDeprecation, first[1].Condition)
    for i := 0; i < 5; i++ {
        assert.Equal(t, first, CheckDrift(locks, models))
    }
}
```

This asserts that newer-member comes before deprecation in the output. The implementation of `CheckDrift` is not shown in this diff, so I can't verify the ordering logic. But the test is asserting a specific ordering.

Let me now look more carefully at the `snapshot.go` file for potential bugs:

```go
func WriteSnapshot(path string, models []CatalogModel) error {
    data, err := MarshalSnapshot(models)
    if err != nil {
        return err
    }
    return writeFileAtomic(path, data)
}
```

This calls `writeFileAtomic` which is not shown in this diff. I'll assume it's a pre-existing function that does temp+rename.

Now let me look at the `snapshotModelOut` struct conversion:

```go
for i, m := range models {
    out.Data[i] = snapshotModelOut(m)
}
```

This is a struct type conversion. For this to work, `CatalogModel` and `snapshotModelOut` must have the exact same field names, types, and order. The comment says they share an identical field layout. But I can't verify this because `CatalogModel` is not shown in this diff. If the fields don't match exactly, this would be a compile error, not a runtime bug. So this is fine.

Let me look at the `catalogResponse` type used in `SnapshotModels`:

```go
var resp catalogResponse
if err := json.Unmarshal(data, &resp); err != nil {
    return nil, fmt.Errorf("failed to parse catalog snapshot: %w", err)
}
return resp.Data, nil
```

`catalogResponse` is not shown in this diff. It must be a pre-existing type that has a `Data` field of type `[]CatalogModel`. The snapshot JSON has a `data` key, so this should work if `catalogResponse` has `Data []CatalogModel `json:"data"``.

But wait - the snapshot also has a `_fixture_meta` key. If `catalogResponse` uses strict JSON decoding, the `_fixture_meta` key would cause an error. But the comment says "Only the `data` array is consumed by CatalogClient.FetchModels; this key is ignored." So `catalogResponse` must be using permissive decoding (ignoring unknown keys). This is the default behavior of `json.Unmarshal` in Go, so it should be fine.

Now let me look at the docs changes for any issues:

### docs/personas-install.md

```markdown
- On the live path it **requires `OPENROUTER_API_KEY`** and refuses to run under a CI environment, failing closed (exit 2) so CI can never fetch live.
```

This says the refresh command "refuses to run under a CI environment." How does it detect CI? This is a claim about behavior, not a code issue I can verify from the diff.

```markdown
- The comparison uses a catalog snapshot compiled into the binary. Point `ATCR_CATALOG_SNAPSHOT` at a file to compare against a different snapshot.
```

This is consistent with the `envCatalogSnapshot` const in `snapshot.go`.

Let me now look at the `quickstart_test.go` changes more carefully:

```go
func TestQuickstart_Online_InstallsNonEmptyCommunityRoster(t *testing.T) {
    dir := t.TempDir()
    home := t.TempDir()
    t.Setenv("HOME", home)

    index := `[
      {"name":"anthony","version":"1.2.0","description":"d","path":"anthony.yaml"},
      {"name":"sonny","version":"1.2.0","description":"d","path":"sonny.yaml"}
    ]`
    srv := unitServer(t, index, map[string]string{
        "/anthony.yaml": communityUnitYAML,
        "/sonny.yaml":   communityUnitYAML,
    })
    t.Setenv("ATCR_PERSONAS_URL", srv.URL)

    destDir := filepath.Join(home, ".config", "atcr", "personas")
    oldDir := personasDir
    personasDir = func() (string, error) { return destDir, nil }
    t.Cleanup(func() { personasDir = oldDir })

    require.NoError(t, runQuickstart(quickstartOpts{
        dir:            dir,
        fetchCommunity: true,
        in:             strings.NewReader(quickstartInput),
        out:            &bytes.Buffer{},
        errOut:         &bytes.Buffer{},
    }))

    assert.Equal(t, []string{"anthony", "sonny"}, communityPinNames(t, destDir),
        "online quickstart installs the same index-derived roster as init")
}
```

This test modifies a package-level variable `personasDir` (swapping it with a function that returns a temp dir) and restores it via cleanup. This is a common pattern in tests but is NOT thread-safe. If tests run in parallel, this could cause issues. But Go tests in the same package run sequentially by default unless `t.Parallel()` is called, so this should be fine.

Wait, but there's a potential issue: `oldDir := personasDir` captures the current value of `personasDir`, which is a function value. If another test has already swapped it and not yet restored it (e.g., if a test panics before cleanup), this could capture the wrong value. But with `t.Cleanup`, the restoration should happen even on panic. So this is fine.

Let me look at the `TestQuickstart_Online_NoSkipWarnings` test:

```go
func TestQuickstart_Online_NoSkipWarnings(t *testing.T) {
    srv := realCommunityServer(t)
    dir := t.TempDir()
    home := t.TempDir()
    t.Setenv("HOME", home)
    t.Setenv("ATCR_PERSONAS_URL", srv.URL)

    destDir := filepath.Join(home, ".config", "atcr", "personas")
    oldDir := personasDir
    personasDir = func() (string, error) { return destDir, nil }
    t.Cleanup(func() { personasDir = oldDir })

    errOut := &bytes.Buffer{}
    require.NoError(t, runQuickstart(quickstartOpts{
        dir:            dir,
        fetchCommunity: true,
        in:             strings.NewReader(quickstartInput),
        out:            &bytes.Buffer{},
        errOut:         errOut,
    }))

    assert.NotContains(t, errOut.String(), "not found in community index",
        "online quickstart emits no misleading skip warnings against the real index")
    // Positive guard: reject a silent zero-install regression (which would also
    // emit zero warnings) by asserting a non-empty install.
    assert.NotEmpty(t, communityPinNames(t, destDir),
        "online quickstart installs a non-empty community roster (not a silent zero-install)")
}
```

This test uses `realCommunityServer(t)` which presumably serves the real `personas/community/index.json`. The test asserts no skip warnings and a non-empty install. This looks correct.

Now let me look at the `internal/registry/config.go` change more carefully:

```go
type AgentConfig struct {
    Provider    string   `yaml:"provider"`
    Model       string   `yaml:"model"`
    Persona     string   `yaml:"persona,omitempty"`
    Temperature *float64 `yaml:"temperature,omitempty"`
    TimeoutSecs *int     `yaml:"timeout_secs,omitempty"`
    RateLimited bool     `yaml:"rate_limited,omitempty"`
    Fallback    string   `yaml:"fallback,omitempty"`
    Payload     string   `yaml:"payload,omitempty"`
    // Binding is the logical family/channel target...
    Binding string `yaml:"binding,omitempty"`

    // Active in Epic 2.0 — the engine acts on these (tool loop + budgets).
    Tools           bool   `yaml:"tools"`             // enables the multi-turn tool loop
    MaxTurns        *int   `yaml:"max_turns"`         // agent-loop turn cap (default 10 when tools=true)
    ToolBudgetBytes *int64 `yaml:"tool_budget_bytes"` // cumulative tool-result budget (0 = unlimited, matches PayloadByteBudget)
    // Reserved for the agentic stages — parsed + validated, inert in 2.0.
    Role string `yaml:"role"` // Stage 3/4 — reviewer | skeptic | judge
    ...
```

The `Binding` field is added between `Payload` and `Tools`. This is an additive field with `omitempty`, so existing configs without `binding:` will continue to load fine. The field is placed in the struct where it makes sense logically. No issues here.

Let me now look at the `internal/personas/search.go` change:

```go
type PersonaIndexEntry struct {
    Name        string `json:"name"`
    Version     string `json:"version"`
    Description string `json:"description"`
    Path        string `json:"path"`
    Provider    string `json:"provider,omitempty"`
    Model       string `json:"model,omitempty"`
    // Binding is the logical family/channel target...
    Binding string   `json:"binding,omitempty"`
    Tasks   []string `json:"tasks,omitempty"`
    Tags    []string `json:"tags,omitempty"`
}
```

The `Binding` field is added between `Model` and `Tasks`. This is additive with `omitempty`, so existing indexes without `binding` will continue to decode fine. No issues here.

Now let me look at the `gofmt` alignment issue in `search.go`. The original code had:
```go
    Name        string   `json:"name"`
    Version     string   `json:"version"`
    Description string   `json:"description"`
    Path        string   `json:"path"`
    Provider    string   `json:"provider,omitempty"`
    Model       string   `json:"model,omitempty"`
    Tasks       []string `json:"tasks,omitempty"`
    Tags        []string `json:"tags,omitempty"`
```

The new code has:
```go
    Name        string `json:"name"`
    Version     string `json:"version"`
    Description string `json:"description"`
    Path        string `json:"path"`
    Provider    string `json:"provider,omitempty"`
    Model       string `json:"model,omitempty"`
    Binding string   `json:"binding,omitempty"`
    Tasks   []string `json:"tasks,omitempty"`
    Tags    []string `json:"tags,omitempty"`
```

The alignment is inconsistent - `Name`, `Version`, etc. have their types aligned at a certain column, but `Binding`, `Tasks`, `Tags` are aligned at a different (shorter) column. This is a `gofmt` issue that would be caught by `gofmt`. But the sprint claims `gofmt` is clean. Let me check if this is actually a problem...

Actually, `gofmt` aligns struct fields within a contiguous block. Since `Binding` has a comment before it, it breaks the alignment block. So `Name` through `Model` are in one alignment block, and `Binding` through `Tags` are in another. This is actually correct `gofmt` behavior - the comment breaks the alignment group. So this is fine.

Wait, but looking more carefully at the diff:

```go
-	Name        string   `json:"name"`
-	Version     string   `json:"version"`
-	Description string   `json:"description"`
-	Path        string   `json:"path"`
-	Provider    string   `json:"provider,omitempty"`
-	Model       string   `json:"model,omitempty"`
-	Tasks       []string `json:"tasks,omitempty"`
-	Tags        []string `json:"tags,omitempty"`
+	Name        string `json:"name"`
+	Version