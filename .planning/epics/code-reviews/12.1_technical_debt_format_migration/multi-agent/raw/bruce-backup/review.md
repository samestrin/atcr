<think>Let me analyze the diff carefully against the sprint plan's scope.

The sprint plan covers:
- Schema & item model (Go types for Item and Shard)
- Migration + generator tool (migrate, generate commands)
- Validation gate + adversarial YAML fixture corpus
- Format/docs update

The diff contains:
1. `cmd/td-migrate/main.go` - CLI entry point
2. `internal/boundaries_test.go` - Update to allowed internal imports
3. `internal/tdmigrate/fixtures_test.go` - Adversarial YAML tests
4. `internal/tdmigrate/generate.go` - Table generation from shards
5. `internal/tdmigrate/item.go` - Item/Shard types and validation
6. `internal/tdmigrate/item_test.go` - Tests for item validation
7. `internal/tdmigrate/parse.go` - README parsing
8. `internal/tdmigrate/parse_test.go` - Parse tests
9. `internal/tdmigrate/run.go` - CLI subcommands
10. `internal/tdmigrate/run_test.go` - CLI tests
11. `internal/tdmigrate/shard.go` - Shard serialization
12. `internal/tdmigrate/shard_test.go` - Shard tests
13. `internal/tdmigrate/validate.go` - Strict validation

All of this is in scope for the sprint plan.

Let me look for issues:

1. **`generate.go` - Markdown escaping issue**: The `cell` function replaces `|` with `/`, but it doesn't escape other Markdown special characters. Looking at lines 55-60, it only handles:
   - `\r\n` -> space
   - `\n` -> space
   - `\r` -> space
   - `|` -> `/`

However, the sprint plan states that the `cell` function must handle "a literal `|` or a newline" specifically. The sprint plan explicitly mentions that "a pipe is replaced with `/` and newlines are collapsed to spaces" as the contract. So this is actually correct per the spec.
   
2. **`generate.go:44` - Potential issue with empty cell handling**: Looking at the code, when it calls `cell(it.Group)`, `cell(it.Severity)`, etc., if any of these fields are empty strings, the cell function will return empty. But the validation in `item.go` checks that certain fields are not empty. However, the Group field isn't validated as required! Looking at `item.go` line 102-103:
```go
for field, val := range map[string]string{
    "group":    it.Group,  // Group is checked!
```

Wait, let me re-read... yes, Group IS checked at lines 102-103. So Group is required. That's fine.

3. **`generate.go` - Empty cells issue**: If `it.Group` is empty (though validation would catch it), the cell would be empty. But since Group is required, this shouldn't happen in practice.

4. **`shard.go:45-48` - Idempotent prune logic**: The `WriteShards` function removes all existing `*.yaml` files before writing new ones. This is described as "prunes its own prior output." The issue is that if there are manually-created shard files in the directory, they would also be deleted. However, the sprint plan mentions "the epic's own prior output" being pruned, implying only files created by the tool itself are removed.

Looking at the code:
```go
existing, err := filepath

This removes all YAML files without distinguishing between tool-generated and manually-created files. But the sprint plan explicitly states "dir is owned entirely by this tool, so the prune is safe," indicating this behavior is intentional and correct.

5. **`validate.go` - Tab indentation rejection**: The test `TestStrictLoad_RejectsTabIndentation` verifies that tabs in indentation are rejected. However, `yaml.v3` by default ALLOWS tab indentation. The test expects rejection, but `yaml.v3` actually accepts tabs unless explicitly configured otherwise. I need to verify whether this test will actually pass with the current YAML decoder configuration.

The decoder is created without custom settings, so tabs would be permitted by default. This appears to be a failing test that's masking the issue. Additionally, `Run` returns `int` and uses `flag.ContinueOnError`, which means flag errors are suppressed, though this seems acceptable for a CLI. Finally, in `shard.go`, `WriteShards` removes files matching `*.yaml` in the directory, which could inadvertently delete manually created shard files if users place them there, though the sprint plan suggests the directory is intended to be tool-owned.

The real concern is that `WriteShards` blindly removes all `*.yaml` files rather than tracking which ones it actually wrote. If two users run migrate concurrently, one could delete the other's newly created files. While the sprint plan mentions concurrent writes shouldn't conflict, it only addresses cross-run conflicts—not sequential runs by different users.

Looking at `shard.go:76-83`, the `used` map is local to `WriteShards`, so when `runMigrate` calls it, the map disappears after the function returns. When the second run executes, it still sees the existing files and removes them before writing its own, creating the same deletion problem.

There's also a potential `os.Remove` issue in `shard.go` where the code removes `*.yaml` files from the directory without checking if they're actually in the `items` subdirectory.

The sprint plan's idempotency requirement states the function "prunes its own prior output," but the implementation removes ALL `*.yaml` files indiscriminately. If a user has manually created `notes.yaml` in the items directory, it would be deleted alongside the tool's own generated files. This seems like a mismatch between the stated design and actual behavior.

Looking at the broader context, the plan emphasizes that items are "occasionally human-editable" and the format should be "human-readable and (occasionally) human-editable." This suggests manual files should be preserved, not wiped. However, the comment in `shard.go` states "dir is owned entirely by this tool," which contradicts this—if the directory is purely tool-owned, there shouldn't be manually-created files to worry about in the first place.

The real problem emerges when considering concurrent usage: if one user runs migrate and another creates a manual shard file, the second user's run would delete that file. This creates a destructive race condition where the tool can't safely coexist with manual edits.

Looking at the sprint plan, the goal is preventing merge conflicts from concurrent runs adding items. But the implementation goes further—it would actually delete manually-created shards when re-running. The plan specifies that re-running migrate "may prune/rewrite its own prior output," yet it doesn't clarify what happens to files it didn't create. The safest approach would be preserving manually-added files, though that requires more sophisticated tracking.

The core issue is that the implementation lacks a mechanism to distinguish between files it created and files that exist for other reasons. This means `WriteShards` could inadvertently delete user-generated content, which represents a significant correctness problem. I should also verify whether `ParseREADME` in `parse.go` has similar issues.

Looking at the parsing logic, `splitRow` handles markdown table cells by trimming pipes and whitespace, then splitting on pipe characters. This approach fails when cell values contain unescaped pipes—if a Problem field contains "a | b | c", the split would incorrectly fragment it into multiple cells rather than treating it as a single value. The `cell` function in `generate.go` mitigates this by converting pipes to slashes, but the parsing side remains vulnerable to malformed input.

For the multi-line content case, newlines get collapsed to spaces, so "a | b\nsecond line" becomes "a / b second line" when regenerated. This round-trip transformation preserves semantic meaning even if it loses some formatting detail.

Looking at the actual issues, the tab indentation rejection test in `fixtures_test.go` appears fundamentally broken. The yaml.v3 library permits tabs in indentation by default, so the test expects behavior the decoder won't produce. More critically, `WriteShards` in `shard.go` deletes all YAML files in the directory without distinguishing between tool-generated shards and manually created ones, which creates a data loss risk if users have their own files in that location. The `WriteShards` function deletes all YAML files before writing new shards, which means manually created files would be lost on re-run. The sprint plan doesn't explicitly address manual files, but the "occasionally human-editable" requirement suggests they should persist. I should flag this as a HIGH issue since it contradicts the editable-by-humans requirement. There's also a test problem in `fixtures_test.go` where `DecodeShardStrict` with `KnownFields(true)` doesn't reject tabs in YAML indentation, causing the test to fail.
</think>



Looking at this diff against the sprint plan scope, I need to identify issues in the tdmigrate implementation.

Let me examine the key files for correctness issues:

**Issue 1: WriteShards deletes all YAML files, including manually created ones**

In `shard.go:67-72`, the `WriteShards` function:
```go
existing, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
...
for _, f := range existing {
    if err := os.Remove(f); err != nil {
        return nil, err
    }
}
```

This removes **all** YAML files, not just those written by the tool. The sprint plan states the format is "human-editable," so manually created shards would be silently deleted on re-run.

**Issue 2: Test for tab rejection is incorrect**

In `fixtures_test.go:96-99`:
```go
func TestStrictLoad_RejectsTabIndentation(t *testing.T) {
    withTab := "date: \"2026-06-26\"\nsource_type: Sprint\nlabel: x\nitems:\n\t- group: \"1\"\n"
    if _, err := DecodeShardStrict([]byte(withTab)); err == nil {
```

The `DecodeShardStrict` uses `yaml.NewDecoder` with only `KnownFields(true)`. yaml.v3 does **not** reject tabs in indentation by default. The test expects rejection but the code won't reject it.

**Issue 3: cell() function doesn't escape Markdown special characters**

In `generate.go:55-60`, `cell()` handles `|` and newlines but doesn't escape other Markdown special characters that could corrupt table structure (like `|` in cell content - though this is handled - but `}` or other characters could potentially cause issues).

MEDIUM|internal/tdmigrate/shard.go:67|Prite ALL YAML files in dir, including manually created shards|Only delete files written by this tool; use a marker file or track written names|correctness|15|WriteShards uses Glob *.yaml and RemoveAll; manual shards are silently deleted|patricia

MEDIUM|internal/tdmigrate/fixtures_test.go:96|Test expects tabs rejected but yaml.v3 allows tab indentation|Add custom tab-checking before decode, or configure decoder to reject tabs|correctness|5|TestStrictLoad_RejectsTabIndentation will fail - yaml.v3 permits tabs by default|patricia