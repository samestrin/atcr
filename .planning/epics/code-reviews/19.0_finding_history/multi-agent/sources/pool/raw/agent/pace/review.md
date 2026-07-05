We are only looking for performance findings in the changed code.
 We must output findings in the format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE
 CATEGORY must be "performance".
 We must not output any finding that is not about performance (e.g., maintainability, correctness, etc.).
 We must name a concrete, measurable runtime cost (algorithmic factor, allocation in hot path, repeated work, N+1, large copy, blocking call, leak).
 We must report each distinct issue only once (if a pattern repeats, we output one finding with count in EVIDENCE).
 We must not nitpick test files unless there's a real performance problem (e.g., fixture build that dominates the suite).
 We must not micro-optimize without a hot-path or scale argument.
 Severity reflects runtime impact.

 We are reviewing the changed regions in the diff.

 Let's look at the changed files and focus on performance issues.

 We are only allowed to output findings for changed lines (FILE:LINE must be in the changed lines).

 We'll go through each changed file and look for performance issues.

 Note: The payload shows the change in context, but we must only flag if the FILE:LINE is in the changed lines.

 We are to output at most ~15 findings, highest impact first.

 Let's examine the diff:

 1. .planning/epics/active/19.0_finding_history.md
    - This is a markdown file. We are not looking for performance issues in documentation? 
      But note: the rules say we only flag performance issues. However, if there is a performance issue in the code that is in the changed lines of a markdown file? 
      Actually, the file is markdown, so it's not code. We skip.

 2. .planning/epics/active/19.4_history_time_sharding.md
    - Also markdown. Skip.

 3. .planning/epics/active/24.0_final_documentation_sweep.md
    - Markdown. Skip.

 4. .planning/technical-debt/README.md
    - Markdown. Skip.

 5. CHANGELOG.md
    - Markdown. Skip.

 6. cmd/atcr/history.go
    - New file. We must check for performance issues in the new code.

 7. cmd/atcr/history_test.go
    - Test file. We only flag if there's a real performance problem (e.g., fixture build that dominates the suite). 
      We'll look for things like building a large fixture in a test that runs often.

 8. cmd/atcr/main.go
    - Changed: added newHistoryCmd() to the root command.

 9. cmd/atcr/main_test.go
    - Test: changed the test for number of subcommands.

10. cmd/atcr/review.go
    - Changed: added history recording after successful review.

11. internal/boundaries_test.go
    - Test file: added "history" to allowedInternalImports.

12. internal/history/capture.go
    - New file: history capture logic.

13. internal/history/capture_test.go
    - Test file.

14. internal/history/edge_test.go
    - Test file.

15. internal/history/filter.go
    - New file: filter for history.

16. internal/history/filter_test.go
    - Test file.

17. internal/history/reader.go
    - New file: reader for history.

18. internal/history/record.go
    - New file: record definition.

19. internal/history/render.go
    - New file: render history table.

20. internal/history/render_test.go
    - Test file.

21. internal/history/writer.go
    - New file: writer for history.

 Now, let's look for performance issues in the changed code (non-test files) that are in the changed lines.

 We'll focus on:

 - O(n²) or worse
 - Unnecessary allocations (in hot paths)
 - Repeated work
 - N+1 queries
 - Large copies

 We are to ignore test files unless they have a real performance problem (e.g., a fixture that is built in a way that dominates the test suite).

 Let's go through the non-test changed files:

 1. cmd/atcr/history.go
    - This is the command for `atcr history`. It reads the history file, filters, and renders a table.
    - Look for:
        * The `history.Load` function: it reads the entire file into memory. If the file is large, this could be a problem.
        * However, note that the file is append-only and we are only reading it for the history command. 
        * The `history.Load` function is in `internal/history/reader.go` (which we'll look at next).

 2. cmd/atcr/main.go
    - Only added a command. No performance issue in the changed lines.

 3. cmd/atcr/review.go
    - Added: 
        // Persist this run's findings to the append-only history ledger (Epic 19.0)
        // so `atcr history` can answer per-package trend queries later. It runs on
        // every successful review — before the conditional in-process reconcile
        // below — reading the pool findings.txt that WritePool always writes, and
        // always targets <root>/.atcr regardless of --output-dir (the ledger is a
        // repo-level accumulator, not part of the redirected review tree). A history
        // write failure is non-fatal: it must never fail an otherwise-successful
        // review, so it is logged and swallowed.
        histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")
        if n, herr := history.RecordReview(histPath, result.Dir, now); herr != nil {
            log.FromContext(ctx).Warn("failed to append finding history", "error", herr)
        } else if n > 0 {
            log.FromContext(ctx).Debug("appended finding history", "records", n, "path", histPath)
        }

    - This calls `history.RecordReview` which is in `internal/history/capture.go`.

 4. internal/boundaries_test.go
    - Test file: we skip unless there's a real performance problem. We don't see any.

 5. internal/history/capture.go
    - This is the core of the history recording.

    Let's look at `RecordReview`:

        func RecordReview(histPath, reviewDir string, ts time.Time) (int, error) {
            data, err := os.ReadFile(filepath.Join(reviewDir, poolFindingsRel))
            if err != nil {
                if errors.Is(err, fs.ErrNotExist) {
                    return 0, nil
                }
                return 0, fmt.Errorf("reading pool findings: %w", err)
            }

            res, err := stream.ParseSource(data)
            if err != nil {
                return 0, fmt.Errorf("parsing pool findings: %w", err)
            }

            // The pool findings.txt is the concatenation of every reviewer's rows, so a
            // finding caught by N reviewers appears N times. Dedupe by id within this run
            // so the ledger holds one record per distinct finding per run ("one JSON
            // record per finding", per the plan) and the severity table is not inflated
            // by reviewer multiplicity. The first occurrence wins.
            records := make([]Record, 0, len(res.Findings))
            seen := make(map[string]bool, len(res.Findings))
            for _, f := range res.Findings {
                id := FindingID(f.File, f.Line, f.Problem)
                if seen[id] {
                    continue
                }
                seen[id] = true
                records = append(records, Record{
                    Timestamp: ts,
                    Package:   PackageOf(f.File),
                    Severity:  f.Severity,
                    ID:        id,
                    File:      f.File,
                    Category:  f.Category,
                })
            }
            if err := Append(histPath, records); err != nil {
                return 0, err
            }
            return len(records), nil
        }

    - Potential performance issues:
        * We are reading the entire pool findings file into memory with `os.ReadFile`. 
          The pool findings file is the output of the review run. How big can it be?
          It contains one line per finding per reviewer (before dedup). 
          In the worst case, if there are many findings and many reviewers, this could be large.
          However, note that the review run is already producing this file, so we are just reading it again.
          This is an extra read of a file that we just wrote. But note: the file is written by WritePool and then we read it here.

          We are doing:
            WritePool writes the pool findings file (in the review directory).
            Then we read it again for history.

          This is an extra I/O and memory allocation for the entire file.

          However, note that the file is not expected to be huge. The number of findings per review is typically in the hundreds or thousands? 
          But if it grows to millions, then reading the entire file into memory could be a problem.

          But note: the history ledger is append-only and we are only storing one record per finding per run (after dedup). 
          The pool findings file might be large because it has multiple entries per finding (one per reviewer). 
          The dedup step reduces it to one per finding.

          We are allocating:
            - The entire file content as a byte slice (data)
            - The parsed findings (res.Findings) which is a slice of Findings (each finding has File, Line, Problem, etc.)
            - Then we allocate a slice for records (up to len(res.Findings)) and a map for seen (up to len(res.Findings))

          This could be heavy if the pool findings file is very large.

          However, note that the review run is already holding the findings in memory (in the fanout.ExecuteReview result). 
          We are reading from disk what we just wrote. We could avoid this by passing the findings directly from the review run.

          But the history hook is placed after WritePool and before the conditional reconcile. 
          We have the `result` from `fanout.ExecuteReview` which contains the pooled findings? 
          Actually, looking at the code in review.go:

            result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)

          Then we do the history recording.

          The `result` has a `Dir` field (the review directory) and we are reading from `result.Dir/sources/pool/findings.txt`.

          We could change the history recording to use the pooled findings from the `result` if available? 
          However, note that the `result` from `fanout.ExecuteReview` does not currently contain the pooled findings. 
          The pooled findings are written to disk by WritePool and then we read them back.

          This is a potential performance issue: we are reading a file that we just wrote, and the file might be large.

          But note: the file is written by WritePool and then we read it immediately. 
          The file is likely in the filesystem cache, so the read might be fast. 
          However, we are allocating memory for the entire file and then parsing it.

          We are already holding the findings in memory in the fanout.ExecuteReview result? 
          Let's look at the fanout.ExecuteReview function (not in the diff, but we know it returns a *fanout.ReviewResult). 
          The ReviewResult does not currently contain the pooled findings. 

          We could change the fanout.ExecuteReview to return the pooled findings? 
          But that would be a larger change and is out of scope for this review.

          Alternatively, we could avoid reading the file by having WritePool return the pooled findings? 
          But again, that's a change to the fanout package.

          Given the constraints, we are only allowed to flag issues in the changed lines.

          In the changed lines of `internal/history/capture.go`, we see:

            data, err := os.ReadFile(filepath.Join(reviewDir, poolFindingsRel))

          This is reading the entire file. If the file is large, this could be a problem.

          However, note that the file is the pool findings for one review run. 
          The number of findings per run is limited by the number of files changed and the number of reviewers. 
          In practice, it might be a few thousand lines at most. 
          But we must consider the worst case.

          The problem: if the pool findings file is very large (e.g., 100MB), then reading it into memory and then parsing it could cause a noticeable slowdown or even an OOM.

          But note: the history ledger is only storing one record per finding (after dedup). 
          We are reading the entire file to deduplicate by the finding id (file, line, problem). 

          We could potentially read the file line by line to avoid holding the entire file in memory? 
          However, we need to deduplicate by id, so we need to store the seen ids. 
          The number of distinct findings is at most the number of lines in the file (if all are distinct) but likely less.

          We are already allocating a map for seen ids (with capacity len(res.Findings)) and a slice for records.

          The memory usage is O(n) in the number of findings (before dedup). 

          If the number of findings is very large (say, 1 million), then we are allocating:
            - The file data: 1MB? (if each line is 100 bytes, then 100MB for 1M lines) -> 100MB for the byte slice.
            - The parsed findings: each finding is a struct with a few strings. Let's say 64 bytes per finding -> 64MB for 1M findings.
            - The seen map: map[string]bool with 1M entries -> each entry is about 8 bytes for the key pointer and 1 byte for the value, but the string keys are stored elsewhere. 
              The keys are the ids (16 bytes each) so 16MB for the keys, and the map overhead (about 1/3 to 1/2 extra) -> say 24MB.
            - The records slice: up to 1M records, each Record is about: 
                  Timestamp (8 bytes? but actually a time.Time is 24 bytes?),
                  Package (string), Severity (string), ID (string), File (string), Category (string).
                We are storing strings that are slices of the original data? 
                Actually, we are creating new strings for Package, File, etc. 
                This could be heavy.

          This could be several hundred MB for a very large review.

          However, note that the review run is already producing the findings in memory (in the fanout). 
          We are duplicating that effort by reading from disk and parsing again.

          This is a performance issue: we are doing extra work (reading and parsing) that we just did in the review run.

          But note: the review run does not currently retain the pooled findings in memory after WritePool. 
          We could change the fanout to keep the pooled findings in memory until after the history recording? 
          However, that is out of scope for this review.

          Given that we are only allowed to flag issues in the changed lines, and the changed lines in `internal/history/capture.go` include the ReadFile and ParseSource, 
          we can flag this as an unnecessary allocation (reading the entire file into memory and parsing it) in the hot path of every successful review.

          However, note that the history recording is only done on successful reviews, and it is after the review run. 
          The review run has already done the work of generating the findings. 
          We are now doing extra work to read and parse the same data from disk.

          This is repeated work: we already had the findings in memory (in the fanout.ExecuteReview result) but we are not using them.

          But wait: the `result` from `fanout.ExecuteReview` does not contain the pooled findings. 
          So we cannot avoid reading from disk without changing the fanout package.

          However, note that the pooled findings are written by WritePool, which is called by fanout.ExecuteReview. 
          We could change WritePool to return the pooled findings as well as writing them to disk? 
          But again, that's out of scope.

          Given the constraints, we must decide if this is a performance issue worth flagging.

          The rules say: we must name a concrete, measurable runtime cost. 
          The cost is: 
            - We are reading a file of size S from disk (which is O(S)) and then parsing it (which is O(S)) 
              when we already had the data in memory (in the fanout) but discarded it.

          However, note that the fanout.ExecuteReview does not return the pooled findings, so we don't have them in memory at this point. 
          We just wrote them to disk and are now reading them back.

          This is a common pattern: write then read. 
          The cost is the I/O and the memory allocation for the file and the parsed data.

          But note: the file is likely small in practice. 
          We must consider if this is a hot path. 
          The history recording is on the critical path of every successful review. 
          If the file is large, it could slow down the review.

          However, the review run itself is likely to be much more expensive (LLM calls, etc.). 
          So the relative cost might be low.

          But note: the problem says we should focus on measurable runtime impact. 
          We are to avoid micro-optimizations without a hot-path or scale argument.

          We do not have evidence that the file is large in practice. 
          We are not told that the file is large. 
          We are only allowed to flag if we can state the cost.

          We can state the cost: 
            - Reading the entire pool findings file into memory and parsing it, which is O(n) in the size of the file, 
              and the file size is proportional to the number of findings (before dedup) times the average line length.

          However, without knowing that the file can be large, we cannot say it's a problem.

          Let's look at the other files.

 6. internal/history/filter.go
    - This is used by the history command to filter records.
    - The `Filter` function iterates over all records and applies the time and package filters.
    - If the history file is very large (many records over many runs), then this could be O(n) in the number of records.
    - The history command is not run on every review; it's a separate command. 
      So it's not in the hot path of the review. 
      We are only concerned with performance in the review flow? 
      The rules don't specify, but note: we are reviewing the changed code for performance issues. 
      The history command is a user-facing command, so if it's slow, that's a performance issue.

    - However, note that the history command is intended to be run occasionally to inspect trends. 
      It is not run on every review. 
      The problem says: we are to find inefficiencies that accumulate into slow software. 
      If the history command is slow when the history file is large, that is a performance issue.

    - The `Filter` function does:
          cutoff := now.Add(-since)
          ... 
          for _, r := range recs {
              if r.Timestamp.Before(cutoff) {
                  continue
              }
              if pkg != "" && !packageMatch(r.Pkg, pkg) {
                  continue
              }
              out = append(out, r)
          }

      This is O(n) in the number of records. 
      We are also allocating a new slice for the filtered records.

      If the history file has millions of records, this could be slow and use a lot of memory.

      However, note that the history ledger is append-only and we are not deleting old records. 
      Over time, the file will grow. 
      The problem says: "Unbounded caches and other leaks" are in the focus.

      This is an unbounded ledger: it grows without bound. 
      The history command will become slower and use more memory as the ledger grows.

      This is a performance issue: the history command's runtime and memory usage grow linearly with the number of runs.

      We can flag this as: 
        - The history command loads the entire history file into memory and then filters it, 
          leading to O(n) memory and time where n is the total number of findings ever recorded.

      However, note that the `Load` function in `internal/history/reader.go` reads the entire file into memory. 
      Then `Filter` processes the entire slice.

      We are already loading the entire file into memory in `Load`. 
      Then we are iterating over all records to filter.

      We could change the `Load` function to filter as we read? 
      But that's out of scope.

      In the changed lines of `internal/history/filter.go`, we have the `Filter` function. 
      We are not changing the `Load` function in this diff.

      However, note that the `Filter` function is called by the history command after `Load` has already read the entire file.

      The cost of the history command is:
        - Load: reads the entire file and parses each line into a Record (allocating memory for each record).
        - Filter: iterates over all records and builds a new slice for the filtered ones.

      This is O(n) in time and memory.

      We can flag this as an unnecessary allocation (we are allocating the entire history in memory and then a filtered slice) 
      and repeated work (we are reading and parsing the entire file every time we run the history command).

      But note: the history command is not run frequently? 
      We don't know. 
      However, the problem says: we are to find inefficiencies that accumulate into slow software. 
      If the history command is run often and the history file is large, it will be slow.

      We can state the cost: 
        - The history command reads and parses the entire history file (which grows without bound) into memory, 
          then filters it, leading to O(n) time and memory where n is the total number of findings.

      This is a leak (unbounded growth) in the sense that the cost of the history command grows over time.

      However, note that the focus list includes: "unbounded caches and other leaks". 
      This is not a cache leak, but it is an unbounded data structure that causes increasing cost.

      We can flag this as a performance issue in the history command.

 7. internal/history/reader.go
    - The `Load` function reads the entire file into memory. 
      We already discussed that.

 8. internal/history/writer.go
    - The `Append` function: 
          if len(records) == 0 {
              return nil
          }
          var buf bytes.Buffer
          enc := json.NewEncoder(&buf)
          for i := range records {
              if err := enc.Encode(records[i]); err != nil {
                  return fmt.Errorf("encoding history record: %w", err)
              }
          }
          ... 
          f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
          ... 
          if _, err := f.Write(buf.Bytes()); err != nil {
              ...
          }

      This is allocating a buffer to hold the entire JSON encoding of the records. 
      If we are appending a large batch of records, this could be large.

      However, note that the batch size is the number of distinct findings in one review run (after dedup). 
      This is the same as the `n` we returned from `RecordReview`. 
      In practice, this is not expected to be huge (maybe hundreds or thousands per run). 
      But if a review run produces hundreds of thousands of findings, then this buffer could be large.

      We are already holding the records in memory (as a slice) and then we are allocating a buffer to hold the JSON. 
      The JSON for one record is about: 
          {"ts":"2026-07-04T12:00:00Z","package":"internal/registry","severity":"HIGH","id":"abc","file":"internal/registry/load.go","category":"CORRECTNESS"}
      Let's say 100 bytes per record. 
      For 1000 records: 100KB. 
      For 10000 records: 1MB. 
      For 100000 records: 10MB.

      This is acceptable for most cases, but if we have a very large review run, it could be noticeable.

      However, note that the review run itself is likely to be much more expensive (LLM calls). 
      So this might be negligible.

      We are not seeing an obvious performance issue here.

 9. internal/history/render.go
    - The `RenderTable` function: 
          It builds a counts map and then a string.
          It iterates over the records to build the counts: O(n)
          Then it iterates over the packages and columns to build the string: O(p * c) where p is number of packages and c is number of columns.

      This is efficient.

 Given the above, the most significant performance issues we see are:

  A. In the history recording (during review): 
        We are reading the entire pool findings file from disk and parsing it, 
        when we just wrote it and could have kept it in memory.

     However, note that we are not allowed to change the fanout package in this review. 
     We are only allowed to flag issues in the changed lines.

     The changed lines in `internal/history/capture.go` include the ReadFile and ParseSource. 
     We can flag this as repeated work: we are re-reading and re-parsing data that we just wrote.

     But note: the review run does not currently retain the pooled findings in memory. 
     So we cannot avoid the disk read without changing the fanout.

     However, we are allowed to flag the issue as it is in the changed lines.

     We can state the cost: 
        - We are doing an extra disk read and parse of the pool findings file (size proportional to the number of findings in the review) 
          on every successful review.

     This is O(n) in the number of findings (before dedup) for the read and parse.

     We can consider the severity: 
        - If the number of findings is large (say, 100,000), then the read and parse could take noticeable time (e.g., 10s of milliseconds to seconds). 
          But note: the review run is already doing LLMs which take seconds or minutes per file. 
          So this might be negligible.

     However, note that the problem says: we are to find inefficiencies that accumulate into slow software. 
     This is an extra O(n) step per review.

     We can flag it as MEDIUM if we assume that the number of findings can be large enough to cause a noticeable slowdown.

  B. In the history command: 
        The history command loads the entire history file into memory and then filters it, 
        leading to O(n) time and memory where n is the total number of findings ever recorded.

     This is unbounded growth: as more reviews are run, the history command gets slower and uses more memory.

     We can flag this as a performance issue in the history command.

     We can state the cost: 
        - The history command reads and parses the entire history file (which grows without bound) into memory, 
          then filters it, leading to O(n) time and memory.

     This is a leak (unbounded growth) in the sense that the cost increases over time.

     We can consider the severity: 
        - If the history file becomes very large (e.g., 1GB), then loading it into memory could cause slowdowns or even OOM.

     We can flag this as HIGH if we assume that the history file can grow large enough to cause noticeable slowdowns or memory issues.

     However, note that the history command is not run on every review. 
     It is run by users when they want to inspect the history. 
     If a user runs it and the file is large, they will experience a slowdown.

     We can flag this as MEDIUM or HIGH.

     But note: the focus list includes "unbounded caches and other leaks". 
     This is an unbounded ledger that causes increasing cost over time.

     We can flag this as: 
        SEVERITY: HIGH (if we assume the file can grow large enough to cause noticeable slowdowns in the history command)
        OR MEDIUM if we think the impact is only moderate.

     However, note that the history command is not in the critical path of the review. 
     It is a separate command. 
     The problem says: we are to find inefficiencies that accumulate into slow software. 
     The software includes the history command.

     We'll flag the history command issue.

  C. Another issue: 
        In the history recording, we are allocating a map and a slice for deduping. 
        The map is cleared after the function returns, but during the function we are using O(n) memory.

        This is temporary and proportional to the number of findings in the review (before dedup). 
        It is not unbounded over time.

        We already considered the disk read.

 Let's compare the two:

  Issue A (history recording during review): 
      - Happens on every successful review.
      - Cost: O(n) where n is the number of findings in the review (before dedup).
      - This is a fixed cost per review.

  Issue B (history command): 
      - Happens when the user runs `atcr history`.
      - Cost: O(N) where N is the total number of findings ever recorded (unbounded over time).
      - This cost grows over time.

  Which is worse? 
      Issue B is worse because it gets worse over time and can become arbitrarily bad.

  However, note that the history command might be run infrequently. 
  But if it is run often (e.g., in a CI job that checks the history), then it could be a problem.

  We don't have information on how often the history command is run.

  Given the rules, we must flag only if we can state the cost. 
  We can state the cost for both.

  We are to output at most ~15 findings, highest impact first.

  We'll output the history command issue first because it has unbounded growth.

  Let's write the finding for the history command:

      We are focusing on the `history.Load` and `history.Filter` functions.

      The changed lines in the history command are in `cmd/atcr/history.go` and the history package.

      However, note that the `Load` function is in `internal/history/reader.go` and the `Filter` function is in `internal/history/filter.go`.

      We are allowed to flag if the FILE:LINE is in the changed lines.

      Let's check the changed lines in `internal/history/reader.go` and `internal/history/filter.go`:

        internal/history/reader.go: entire file is new -> all lines are changed.
        internal/history/filter.go: entire file is new -> all lines are changed.

      We can pick a representative line for the issue.

      For the history command issue, we can point to the `Load` function in `internal/history/reader.go` because it reads the entire file.

      However, note that the issue is not just the Load but also the Filter. 
      We can combine them: the history command loads the entire file and then filters it.

      We can write:

          SEVERITY: HIGH
          FILE: internal/history/reader.go:20   (for example, the line where we open the file)
          PROBLEM: Load reads the entire history file into memory, and Filter then processes all records, leading to O(n) time and memory where n is the total number of findings ever recorded (unbounded growth)
          FIX: Implement streaming filtering to avoid loading the entire file into memory, or add retention/rotation to bound the ledger size.
          CATEGORY: performance
          EST_MINUTES: 60   (we have to guess, but note the example in the technical debt had 60 for a similar issue)
          EVIDENCE: Load reads the entire file into memory (reader.go:20-30), then Filter iterates over all records (filter.go:20-30) — same pattern at every history command invocation

      However, note: the technical debt file already has an item for this:

          internal/history/reader.go:14 | Load reads the entire findings-history.jsonl into memory with no rotation/retention, so an unbounded ledger grows memory use over time | Add optional retention/rotation or streaming aggregation if ledgers grow large (retention explicitly out of scope for epic 19.0) | PERFORMANCE | 60 | execute-epic-cumulative

      This is exactly the issue we are describing.

      But note: we are not allowed to flag items that are already in the technical debt file? 
      The problem says: we are reviewing the changed regions. 
      The technical debt file is changed (we saw a change in the technical debt file: 
          .planning/technical-debt/README.md
          index 9a50c6d1..ef762ad0
          ... 
          | LOW | internal/history/reader.go:14 | ... | PERFORMANCE | 60 | execute-epic-cumulative
          ... 
          and then it was changed to:
          | LOW | internal/history/reader.go:14 | ... | PERFORMANCE | 60 | execute-epic-cumulative   (still there, but the Open Items count changed)

      However, note that the technical debt file change we saw was only in the stats and the addition of a new item from epic-19.0.

      The item we are talking about is already in the technical debt file (from epic-19.0) and is marked as LOW.

      But wait: the technical debt file shows:

          | LOW | internal/history/reader.go:14 | Load reads the entire findings-history.jsonl into memory with no rotation/retention, so an unbounded ledger grows memory use over time | Add optional retention/rotation or streaming aggregation if ledgers grow large (retention explicitly out of scope for epic 19.0) | PERFORMANCE | 60 | execute-epic-cumulative

      And then in the stats, it was counted as LOW and deferred.

      However, we are reviewing the changed code. 
      The technical debt file changed, but the line we are interested in (internal/history/reader.go:14) is not in the changed lines of the technical debt file? 
      Let's look:

          The technical debt file diff:

          @@ -3,16 +3,16 @@
           This file is a staging area for small technical debt items discovered during development. Items are triaged and moved to individual sprint documents in `sprints/` as they are prioritized.
           
           ## Stats
           
           | Severity | Open | Deferred | Resolved |
           |----------|------|----------|----------|
           | CRITICAL | 0 | 0 | 0 |
           | HIGH | 0 | 2 | 0 |
           -| MEDIUM | 0 | 30 | 0 |
           -| LOW | 0 | 32 | 0 |
           +| MEDIUM | 1 | 30 | 0 |
           +| LOW | 4 | 32 | 0 |
           
           -**Last Modified:** 2026-07-04 | **Open Items:** 0 | **Deferred Items:** 64 | **Resolved Items:** 0 | **Total Items:** 64
           +**Last Modified:** 2026-07-04 | **Open Items:** 5 | **Deferred Items:** 64 | **Resolved Items:** 0 | **Total Items:** 69
           
           ## Directory Structure
           
           ```
           
          @@ -62,246 +62,256 @@ in [`items/SCHEMA.md`](items/SCHEMA.md). Round-trip fidelity (table → shards 
           table with zero data loss) is proven by the Go test suite in
           `internal/tdmigrate/`, not by a committed generated artifact.
           
          +### [2026-07-04] From Sprint: epic-19.0
           +
           +| Group