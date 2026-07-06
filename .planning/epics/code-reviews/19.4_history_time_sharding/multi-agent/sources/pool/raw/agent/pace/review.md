We are reviewing the diff for performance issues only. We must look for:
 - O(n²) or worse
 - Unnecessary allocations in hot paths
 - Repeated work (missing memoization)
 - N+1 queries (database/network calls in loops)
 - Large copies (copying large structs where reference would do, blocking I/O in async, holding locks too long, unbounded caches, wrong container)

We are only to look at the changed regions (the diff). We must not flag test files unless there's a real performance problem (e.g., a fixture build that dominates the suite).

We must output findings in the format:
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE

CATEGORY is always `performance`.

We are to emit at most ~15 findings, highest impact first.

Let's go through the changed files:

1. .planning/.knowledge/clarifications-19.4_history_time_sharding-Q1.md: This is a markdown file (documentation). No performance issue here.

2. .planning/epics/active/19.4_history_time_sharding.md: Also markdown. No performance.

3. .planning/technical-debt/README.md: This is a markdown file that includes a table of technical debt items. We are to look for performance issues in the code, but note that this file itself is not code. However, note that the diff in this file includes a table that describes some technical debt items. We are not to flag the markdown file for performance, but we must check if any of the described issues are in the changed code? Actually, the diff in this file is just updating the table. We are to look at the changed code in the diff, so we ignore this markdown file for performance.

4. CHANGELOG.md: Markdown, no performance.

5. cmd/atcr/history.go: This file has changes. Let's look:

   - The function `newHistoryCmd` and `runHistory` are changed to use sharded history.
   - Specifically, the old way: 
        histPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
        recs, err := history.Load(histPath)
     is replaced by:
        shardDir := filepath.Join(root, ".planning", "history")
        legacyPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
        recs, err := history.LoadAll(shardDir, legacyPath)

   - The `history.LoadAll` function (which we see in internal/history/shard.go) does:
        shards, err := LoadShards(shardDir)
        legacy, err := Load(legacyPath)
        return append(shards, legacy...), nil

   - And `LoadShards` does:
        matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
        ... then for each match, it calls `Load(path)` and appends.

   - The `Load` function (in internal/history/record.go, not changed in this diff) reads a JSONL file and returns the records.

   - The concern: if there are many shard files (e.g., years of history), then `LoadShards` will read every shard file and then `LoadAll` will also read the legacy file. Then, in `runHistory`, we do:
        filtered := history.Filter(recs, since, pkg, time.Now())

   - The `Filter` function (not shown in the diff, but we can assume it's in internal/history/record.go) filters the records by time and package.

   - The problem: We are loading all history (from all shards and the legacy file) and then filtering. If the history is large (many years) and we are only interested in a recent window (e.g., 90 days), then we are doing unnecessary work: loading and parsing all the old shards that are outside the window.

   - This is a repeated work issue: we are loading data that we then throw away in the filter.

   - We should instead filter by shard (by the month in the filename) to avoid loading shards that are entirely outside the time window.

   - However, note that the `LoadShards` function does not take a time window. It loads all shards.

   - This is a performance issue: O(n) in the number of shards (and the total records) when we could be O(1) or O(k) for the shards in the window.

   - But note: the shard files are named by month (YYYY-MM). We can compute which months are in the window and only load those.

   - However, the current change does not do that. It loads all shards.

   - We must check if this is in the changed region: yes, because we changed the way we call the history loading (from `history.Load` to `history.LoadAll`).

   - But note: the `history.LoadAll` function is new (in internal/history/shard.go) and we are using it. The old way was to load a single file. Now we are loading multiple files and then filtering.

   - This is a performance regression if the number of shards is large and the time window is small.

   - However, note that the epic is about sharding by month to avoid a single ever-growing file. The trade-off is that we now have multiple files, but we are loading them all and then filtering.

   - We should consider: is this a hot path? The `atcr history` command is run by users to query history. It might be run frequently.

   - The evidence: we are loading every shard file (and the legacy file) and then filtering. If there are N shards and the window only covers k shards (k << N), then we are doing (N-k) extra file reads and parsing.

   - We can fix this by modifying `LoadShards` to take a time window and only load the shards that might contain records in the window. Since the shards are by month, we can compute the range of months that overlap with the window.

   - However, note that the `LoadShards` function is in the changed region (internal/history/shard.go) and we are allowed to change it? But we are reviewing the diff, and the diff for internal/history/shard.go is adding the file. We are not changing it in this diff? Actually, we are adding it. So we have the opportunity to fix it in this same change? But the diff we are reviewing is the one that adds the sharding feature. We are to review the changes in this diff for performance issues.

   - Since we are adding the sharding feature, we should design it to avoid loading unnecessary shards.

   - Therefore, this is a performance issue in the new code we are adding.

   - Let's check the severity: 
        - If the history has many years (say 10 years = 120 shards) and the user queries the last 90 days (about 3 months), then we are loading 120 shards instead of 3. That's 40x more work. 
        - This is noticeable at scale (many years of history) -> HIGH.

   - However, note that the epic says: "Once a month rolls over, its shard stops receiving writes, so old shards no longer churn new git blobs." It doesn't say we avoid reading them. But for performance, we should.

   - We are to flag this as a performance issue.

   - But note: the `LoadAll` function is called from `runHistory` in `cmd/atcr/history.go`. We are changing that call site. We are also adding the `LoadShards` and `LoadAll` functions.

   - We can fix this by modifying `LoadShards` to take an optional time range and only load the shards that fall in that range? Or we can do the filtering in `LoadAll` by shard name? 

   - Alternatively, we can change the `LoadShards` function to accept a time range and then only glob for the shards in that range? But note: the shard names are by month, so we can compute the list of months that overlap with the time window and then only load those.

   - However, the `LoadShards` function currently does a glob for "*.jsonl". We could change it to take a list of shard files to load? But that would change the interface.

   - Since we are in the same diff, we can adjust the design.

   - But note: we are reviewing the diff as given. We are not allowed to change the diff? We are to report the issue.

   - We will report the issue in the `cmd/atcr/history.go` file at the line where we call `history.LoadAll` because that's where we are loading all shards without filtering by time.

   - However, the actual inefficiency is in the `LoadShards` function (which we are adding) because it loads every shard. But we are adding that function in this diff, so we can also note that the function `LoadShards` is inefficient for large histories when a time window is applied.

   - We must pick one representative line. Let's pick the call site in `cmd/atcr/history.go` because that's where we are using the function without considering the time window for shard selection.

   - The line: `recs, err := history.LoadAll(shardDir, legacyPath)`

   - We are going to change that to: we should first compute the time window and then only load the shards that are in the window? But note that the `LoadAll` function doesn't take a time window.

   - Alternatively, we can change the `LoadShards` function to take a time window and then only load the shards that are in the window? But we are adding that function, so we can fix it in this diff? However, the diff we are reviewing is fixed. We are to report what we see.

   - Since we are reviewing the diff as given, we see that the `LoadShards` function loads every shard. We see that the `runHistory` function then filters the entire set. We can report that.

   - We'll report the issue at the `LoadShards` function because that's where the unnecessary work is happening (loading shards that will be filtered out). But note: the `LoadShards` function is new and we are adding it. We are to look at the changed region. The changed region for `LoadShards` is the entire file (since it's new). We can pick the first line of the function? But we need a line number.

   - Let's look at the diff for `internal/history/shard.go`:

        func LoadShards(dir string) ([]Record, error) {
        matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
        if err != nil {
            return nil, fmt.Errorf("globbing history shards: %w", err)
        }
        sort.Strings(matches)

        var all []Record
        for _, path := range matches {
            recs, err := Load(path)
            if err != nil {
                return nil, err
            }
            all = append(all, recs...)
        }
        return all, nil
        }

   - We can say: the function `LoadShards` loads every shard file without considering the time window, which may lead to loading unnecessary data when the query time window is small.

   - We'll pick the line: `matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))` because that's where we start the process of loading all shards.

   - However, note that the `LoadShards` function is called from `LoadAll` which is called from `runHistory`. We can also report at the call site in `runHistory` but the issue is in the implementation of `LoadShards`.

   - We'll report at the function `LoadShards` and the line of the glob.

   - But note: the diff for `internal/history/shard.go` is adding the file, so the line numbers are relative to the new file. We don't have the exact line numbers from the diff? We can see:

        @@ -0,0 +1,67 @@
        ... 
        +func LoadShards(dir string) ([]Record, error) {
        +	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
        +	if err != nil {
        +		return nil, fmt.Errorf("globbing history shards: %w", err)
        +	}
        +	sort.Strings(matches)
        +

   - The line of the glob is line 13 (if we count from the start of the function: line 1 is the function declaration, line 2 is the matches line? Actually, the diff shows:

        +func LoadShards(dir string) ([]Record, error) {
        +	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
        +	if err != nil {
        +		return nil, fmt.Errorf("globbing history shards: %w", err)
        +	}
        +	sort.Strings(matches)
        +

   - So the glob is at line 2 of the function? But the file starts at line 1. So the glob is at line 2 of the file? Actually, the diff shows:

        @@ -0,0 +1,67 @@
        --- /dev/null
        +++ b/internal/history/shard.go
        @@ -0,0 +1,67 @@
        +package history
        +
        +import (
        +	"fmt"
        +	"path/filepath"
        +	"sort"
        +	"time"
        +)
        +
        +// shardMonthLayout is the year-month stem of a monthly history shard file.
        +const shardMonthLayout = "2006-01"
        +
        +// ShardPath returns the monthly shard file for a run at ts under dir, e.g.
        +// <dir>/2026-07.jsonl. The month is taken in UTC so shard names are
        +// deterministic regardless of the caller's local zone, and every record from a
        +// single run (all stamped with the same ts) lands in exactly one shard.
        +//
        +// Because the file name is derived from ts, a run in a new month writes to a new
        +// file and never reopens a prior month's shard — that is what stops old shards
        +// from churning fresh git blobs once the month rolls over (Epic 19.4 AC3).
        +func ShardPath(dir string, ts time.Time) string {
        +	return filepath.Join(dir, ts.UTC().Format(shardMonthLayout)+".jsonl")
        +}
        +
        +// LoadShards reads every monthly shard (*.jsonl) under dir and returns the
        +// merged records across all months, ordered by shard file name — which, for the
        +// YYYY-MM naming, is chronological. A missing or empty dir is a valid empty
        +// history, not an error (mirroring Load), so `atcr history` answers a query
        +// across whatever shards exist without the caller naming one (Epic 19.4 AC2).
        +// Malformed lines inside a shard are skipped by Load; an unreadable shard is a
        +// hard error.
        +func LoadShards(dir string) ([]Record, error) {
        +	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
        +	if err != nil {
        +		return nil, fmt.Errorf("globbing history shards: %w", err)
        +	}
        +	sort.Strings(matches)
        +
        +	var all []Record
        +	for _, path := range matches {
        +		recs, err := Load(path)
        +		if err != nil {
        +			return nil, err
        +		}
        +		all = append(all, recs...)
        +	}
        +	return all, nil
        +}

   - The glob is at line 20 (if we count the lines in the diff: the first line of the diff is the @@ line, then the next lines are the content). Actually, we can count:

        Line 1: +package history
        Line 2: 
        Line 3: +import (
        Line 4: +	"fmt"
        Line 5: +	"path/filepath"
        Line 6: +	"sort"
        Line 7: +	"time"
        Line 8: +)
        Line 9: 
        Line 10: +// shardMonthLayout is the year-month stem of a monthly history shard file.
        Line 11: +const shardMonthLayout = "2006-01"
        Line 12: 
        Line 13: +// ShardPath returns the monthly shard file for a run at ts under dir, e.g.
        Line 14: +// <dir>/2026-07.jsonl. The month is taken in UTC so shard names are
        Line 15: +// deterministic regardless of the caller's local zone, and every record from a
        Line 16: +// single run (all stamped with the same ts) lands in exactly one shard.
        Line 17: +//
        Line 18: +// Because the file name is derived from ts, a run in a new month writes to a new
        Line 19: +// file and never reopens a prior month's shard — that is what stops old shards
        Line 20: +// from churning fresh git blobs once the month rolls over (Epic 19.4 AC3).
        Line 21: +func ShardPath(dir string, ts time.Time) string {
        Line 22: +	return filepath.Join(dir, ts.UTC().Format(shardMonthLayout)+".jsonl")
        Line 23: +}
        Line 24: 
        Line 25: +// LoadShards reads every monthly shard (*.jsonl) under dir and returns the
        Line 26: +// merged records across all months, ordered by shard file name — which, for the
        Line 27: +// YYYY-MM naming, is chronological. A missing or empty dir is a valid empty
        Line 28: +// history, not an error (mirroring Load), so `atcr history` answers a query
        Line 29: +// across whatever shards exist without the caller naming one (Epic 19.4 AC2).
        Line 30: +// Malformed lines inside a shard are skipped by Load; an unreadable shard is a
        Line 31: +// hard error.
        Line 32: +func LoadShards(dir string) ([]Record, error) {
        Line 33: +	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
        Line 34: +	if err != nil {
        Line 35: +		return nil, fmt.Errorf("globbing history shards: %w", err)
        Line 36: +	}
        Line 37: +	sort.Strings(matches)
        Line 38: 
        Line 39: +	var all []Record
        Line 40: +	for _, path := range matches {
        Line 41: +		recs, err := Load(path)
        Line 42: +		if err != nil {
        Line 43: +			return nil, err
        Line 44: +		}
        Line 45: +		all = append(all, recs...)
        Line 46: +	}
        Line 47: +	return all, nil
        Line 48: 

   - So the glob is at line 33.

   - We'll report:

        SEVERITY: HIGH (because if there are many years of history, the cost is significant)
        FILE: internal/history/shard.go:33
        PROBLEM: LoadShards loads every shard file via glob, then reads and parses each, even if the query time window excludes most shards; this is O(total shards) when it could be O(shards in window)
        FIX: Modify LoadShards to take an optional time range and only glob for shards that overlap the window (e.g., by year-month), or filter the shard list by name before loading
        CATEGORY: performance
        EST_MINUTES: 30 (to fix)
        EVIDENCE: The function LoadShards uses filepath.Glob(dir, "*.jsonl") to get all shard files, then loads each via Load(path); if the history spans 10 years (120 shards) and the query is for the last 90 days (~3 shards), 117 shards are loaded and parsed unnecessarily.

   - However, note that we are in the same diff and we are adding the function. We have the chance to fix it in this diff? But we are reviewing the diff as given. We are to report the issue.

   - But wait: the issue might be mitigated by the fact that the `history.Filter` function might be efficient? But we are still loading and parsing the entire shard files (which could be large) and then throwing away most of the records.

   - This is a clear performance issue.

   - However, note that the `Load` function (which we are calling for each shard) might be efficient? But we are still doing the work of reading the file and parsing the JSONL for each shard, which we then discard if the records are outside the time window.

   - We'll go ahead and report.

6. Another issue in the same file: `internal/history/shard.go` at the `LoadAll` function? We see:

        func LoadAll(shardDir, legacyPath string) ([]Record, error) {
        shards, err := LoadShards(shardDir)
        if err != nil {
            return nil, err
        }
        legacy, err := Load(legacyPath)
        if err != nil {
            return nil, err
        }
        return append(shards, legacy...), nil
        }

   - This function calls `LoadShards` (which we already flagged) and then loads the legacy file and appends. The legacy file is only one file, so that's acceptable.

   - So we don't need to flag `LoadAll` separately.

7. Now, let's look at `cmd/atcr/history.go` again. We changed the way we call the history loading. We are now calling `history.LoadAll` which in turn calls `LoadShards` and `Load`. We have flagged the issue in `LoadShards`.

8. Are there any other performance issues?

   - Look at `cmd/atcr/resume.go`: 
        We changed the history path in `recordResumeHistory` from:
            histPath := filepath.Join(".", ".atcr", "findings-history.jsonl")
        to:
            histPath := history.ShardPath(filepath.Join(".", ".planning", "history"), ts)

        This is just changing the path to use the shard. It's a single write per resume. No performance issue.

   - `cmd/atcr/resume_test.go`: changed to use `history.LoadShards` instead of `history.Load` for the test. This is in a test, and we are not to flag test files unless there's a real performance problem (e.g., a fixture build that dominates the suite). Here, the test is loading the shard directory (which in the test is small) so no issue.

   - `cmd/atcr/review.go`: 
        We changed the history write path from:
            histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")
        to:
            histPath := history.ShardPath(filepath.Join(req.Root, ".planning", "history"), now)

        This is a single write per review. No performance issue.

   - `internal/history/record.go`: only changed the package comment. No code change.

   - `internal/history/shard_test.go`: new test file. We are not to flag test files for performance unless there's a real problem. The tests are creating a few shard files and loading them. The test `TestLoadShards_MergesAllMonthlyFiles` creates two shards and loads them. This is negligible.

   - However, note that the test `TestLoadShards_MergesAllMonthlyFiles` is loading two shards. But if we were to run this test with a large number of shards, it might be slow? But the test is creating only two shards. So no issue.

   - But wait: the test `TestLoadShards_MergesAllMonthlyFiles` is in the new test file. We are not to flag test files for performance unless there's a real performance problem in the test suite (e.g., a test that takes a long time and is run frequently). This test is fast.

   - However, note that the test `TestLoadShards_MergesAllMonthlyFiles` is creating two shards and then loading them. It's not a problem.

   - But we see a potential issue in the test: the test `TestLoadShards_MergesAllMonthlyFiles` is creating two shards and then calling `LoadShards` which loads both. This is fine.

   - However, we are not to flag test files.

9. Another issue: in `internal/history/shard.go`, the `LoadShards` function sorts the matches (the shard file names) with `sort.Strings(matches)`. This is O(n log n) in the number of shards. But note: the shard files are named by month (YYYY-MM) so they are already in lexicographical order which is chronological. We could avoid the sort if we trust the glob to return them in order? But the glob does not guarantee order. However, on most filesystems, glob returns in lexicographical order? But we cannot rely on that. The sort is necessary for correctness (to get chronological order). The number of shards is the number of months of history. For 10 years, 120 shards, the sort is negligible. So we don't flag this.

10. We have one issue so far.

11. Let's look at the technical debt table in `.planning/technical-debt/README.md`. We are not to flag the markdown file, but note that the table includes some items that are marked as performance? We are to look at the code changes, not the markdown.

    However, note that the diff in this file is updating the table. We are not to look for performance issues in the markdown.

12. But wait: the payload includes the diff for `.planning/technical-debt/README.md` and we see:

        | Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
        +-------|---|----------|------|---------|-----|----------|-------------|--------+
        | 1 | [ ] | LOW | internal/history/shard.go:33 | LoadShards builds a glob pattern from the dir path, so a repo path containing filepath.Glob metacharacters (e.g. a [1] segment) is parsed as a character class and matches no shards, silently returning empty history instead of the real records | Use os.ReadDir + strings.HasSuffix(name, .jsonl) so the directory path is treated literally | EDGE_CASES | 20 | execute-epic-independent |
        | 1 | [ ] | LOW | internal/history/shard.go:43 | A single unreadable shard (permission/IO) aborts LoadShards/LoadAll and fails atcr history with exit 2, so one bad monthly file bricks all trend queries (deliberate exit-2-on-corrupt contract, but harsher now that history spans many files) | Log-and-skip an unreadable individual shard so the remaining shards stay queryable, mirroring Load's torn-line tolerance | ERROR_PATHS | 20 | execute-epic-independent |
        | 1 | [ ] | LOW | internal/history/shard.go:32 | LoadShards loads every shard into memory before Filter applies --since, so a narrow window still reads all shards (acceptable within the stated 1-2yr bound; no month-prefix pruning) | Skip shard files whose YYYY-MM stem is entirely older than now-since before loading, to keep long histories fast | UNDER_ENGINEERING | 30 | execute-epic-independent |
        | U | [ ] | LOW | cmd/atcr/history.go:50 | History ledger path segments (.planning/history shard dir and legacy .atcr/findings-history.jsonl) are duplicated across review.go, resume.go, and history.go | Centralize as history.ShardDir(root) + history.LegacyLedgerPath(root) helpers so the storage layout lives in one place | CROSS_CUTTING | 15 | execute-epic-cumulative |
        | U | [ ] | MEDIUM | cmd/atcr/resume.go:213 | recordResumeHistory writes shards to a cwd-relative ./.planning/history while review.go uses req.Root and atcr history reads via repoRoot(), so a resume run from a subdirectory writes shards history never reads (pre-existing; resume also writes its audit ledger cwd-relative at resume.go:228 — DEFERRED as out of scope: a resume-wide root-resolution fix spanning the Epic 19.1 audit path) | Resolve repoRoot() once in runResume and pass it to both recordResumeHistory and recordResumeAudit so all resume ledger writes and the read path agree on location | REGRESSION_RISK | 30 | execute-epic-independent |

    - We see that the technical debt table already has an entry for `internal/history/shard.go:32` (which is the line of the function declaration? or the line of the comment?) that says:

          LoadShards loads every shard into memory before Filter applies --since, so a narrow window still reads all shards (acceptable within the stated 1-2yr bound; no month-prefix pruning)

    - And the fix is: Skip shard files whose YYYY-MM stem is entirely older than now-since before loading, to keep long histories fast.

    - This is exactly the issue we flagged! But note: the severity in the table is LOW. However, we argued that it could be HIGH if there are many years of history.

    - The table says: "acceptable within the stated 1-2yr bound". So if the history is bounded to 1-2 years, then it's LOW. But if the history can be longer, then it might be higher.

    - However, the epic does not specify a bound on the history. The technical debt table says it's acceptable for 1-2 years. We don't know the actual bound in the project.

    - But note: we are to flag only if there is a real runtime cost. The technical debt table says it's LOW. We must use our judgment.

    - We are to look for measurable runtime cost. The issue is that we are loading all shards. If the history is unbounded (or can grow beyond 1-2 years) then it becomes a problem.

    - The epic does not specify a bound. The technical debt table says it's acceptable for 1-2 years. We don't know if the project expects to keep history longer than 2 years.

    - However, note the epic's motivation: to avoid a single ever-growing file. It doesn't say we want to keep history forever. But it also doesn't say we want to limit to 2 years.

    - We must decide: is this a performance issue in the changed code?

    - We are to flag if we can state a concrete, measurable runtime cost. We can: if the history has N shards and the query window covers k shards, then we are doing (N-k) extra file reads and JSON parses.

    - The cost is linear in the number of shards outside the window.

    - We don't know N, but we know that N can grow over time. The cost will grow linearly with the age of the history.

    - Therefore, it is an unbounded cost in the sense that as the history grows, the cost of a query for a fixed window grows linearly with the total history.

    - This is a performance issue that will become worse over time.

    - We should flag it.

    - However, note that the technical debt table already has it as LOW. We are to override? No, we are to use our judgment.

    - We'll stick with our initial judgment: HIGH if the history can be large (which it can, because there's no bound mentioned in the epic). But note the epic says: "Once a month rolls over, its shard stops receiving writes, so old shards no longer churn new git blobs." It doesn't say we delete old shards. So the history can grow indefinitely.

    - Therefore, we flag it as HIGH.

    - But wait: the technical debt table says LOW. We are reviewing the diff, and the diff does not include the fix for this issue. We are to report what we see.

    - We see the inefficiency. We report it.

    - However, note that the line number in the technical debt table is `internal/history/shard.go:32`. In our diff, the function `LoadShards` starts at line 32? Actually, we saw the function declaration at line 32 (in the diff for the file). The line 32 in the file is:

          func LoadShards(dir string) ([]Record, error) {

    - So we can report at that line.

    - But note: the technical debt table also has an issue at line 33 (the glob) and line 43 (the error handling). We are not to duplicate.

    - We'll report the issue at the function declaration line (line 32) because it represents the entire function.

    - However, the problem is in the implementation (the glob and the loop). We can report at the glob line (line 33) as we initially thought.

    - Let's report at the glob line (line 33) because that's where we start the process of loading all shards.

    - We'll use:

        SEVERITY: HIGH
        FILE: internal/history/shard.go:33
        PROBLEM: LoadShards loads every shard file via glob, then reads and parses each, even if the query time window excludes most shards; this is O(total shards) when it could be O(shards in window)
        FIX: Modify LoadShards to take an optional time range and only glob for shards that overlap the window (e.g., by year-month), or filter the shard list by name before loading
        CATEGORY: performance
        EST_MINUTES: 30
        EVIDENCE: The function LoadShards uses filepath.Glob(dir, "*.jsonl") to get all shard files, then loads each via Load(path); if the history spans 10 years (120 shards) and the query is for the last 90 days (~3 shards), 117 shards are loaded and parsed unnecessarily.

    - But note: the technical debt table has an entry for line 32 (the function) and line 33 (the glob) and line 43 (the error). We are picking line 33.

    - However, the technical debt table says the issue at line 32 is LOW. We are saying HIGH. We are allowed to disagree? Yes, because we are reviewing the code and we see a potential for high impact.

    - We'll go ahead.

12. Are there any other issues?

    - Look at `cmd/atcr/history.go` at the call to `history.LoadAll`:

          recs, err := history.LoadAll(shardDir, legacyPath)

    - We are loading the shards and the legacy file and then filtering. We have flagged the issue in `LoadShards` (which loads all shards). The legacy file is only one file, so that's acceptable.

    - But note: the `LoadAll` function also loads the legacy file. If the legacy file is large, then we are loading it entirely and then filtering. However, the legacy file is the pre-19.4 flat ledger. The epic says we are not to move or rewrite it, but we are to read it. The legacy file is only one file, and we are going to read it once per query. If the legacy file is large (because it contains years of history before sharding) then we are reading the entire legacy file and then filtering by time. This is the same issue as with the shards, but for the legacy file.

    - However, note that the legacy file is only read if it exists. And we are only reading it once. The cost is O(size of legacy file). We cannot avoid reading the legacy file because we don't know which parts are in the time window without reading it? Unless we index it? But