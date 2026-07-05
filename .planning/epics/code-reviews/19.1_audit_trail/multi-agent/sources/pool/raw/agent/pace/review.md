We are only looking for performance issues in the changed code.
 The changes are in multiple files, but we must focus on the diff.

 Let's go through the changed files and look for performance issues.

 We are to look for:
   - O(n²) or worse
   - Unnecessary allocations (in hot paths)
   - Repeated work (same computation multiple times)
   - N+1 queries (database/network calls in loops)
   - Large copies (copying large structs where references would work, blocking I/O in async, etc.)

 We must ignore:
   - Maintainability, style, naming, comments, imports, unused code, correctness, security, testing.

 We must report each distinct issue once, with a representative FILE:LINE and count if repeated.

 We are to output at most ~15 findings, highest impact first.

 Let's examine the changes:

 1. New files: many markdown files, test files, and Go files for the new audit feature.

 We are only interested in performance issues in the changed code.

 Since the changes are mostly about adding an audit trail, we look for performance issues in the new audit code.

 However, note that the audit code is new and we are to look for performance problems in the changed regions.

 Let's look at the Go files that were changed or added:

 - cmd/atcr/audit_report.go
 - cmd/atcr/audit_report_test.go
 - cmd/atcr/audit_pr_test.go
 - cmd/atcr/main.go (added audit-report command)
 - cmd/atcr/resume.go (added audit hook for resume)
 - cmd/atcr/review.go (added audit hook for review, and pr flag)
 - internal/audit/capture.go
 - internal/audit/capture_test.go
 - internal/audit/reader.go
 - internal/audit/reader_test.go
 - internal/audit/record.go
 - internal/audit/render.go
 - internal/audit/render_test.go
 - internal/audit/writer.go
 - internal/audit/writer_test.go
 - internal/boundaries_test.go (added audit to allowed imports)

 We are to look for performance issues in the changed code.

 Let's examine each:

 1. cmd/atcr/audit_report.go:
    - It reads the entire audit ledger into memory (audit.Load) and then filters for a given PR.
    - If the ledger is large, this could be inefficient. However, the audit ledger is expected to be small (one record per review run).
    - But note: the ledger is append-only and grows over time. However, the number of review runs is not expected to be huge (maybe thousands?).
    - We are to look for a hot path: is this function called in a loop? It's called once per audit-report command.
    - The function `audit.Load` reads the entire file and returns all records. Then we filter by PR.
    - This is O(n) in the number of records, which is acceptable for a few thousand records.

    However, note that the audit ledger is a JSONL file and we are reading it entirely. If the ledger becomes very large (e.g., millions of records) then this could be a problem.

    But the problem says: "Report each distinct issue ONCE. If a pattern repeats across many lines, emit a single finding at one representative FILE:LINE and put the count in EVIDENCE"

    We don't have evidence that the ledger will be huge. Moreover, the audit ledger is only written by the atcr review command, which is not expected to run millions of times.

    We might consider this a potential issue if the ledger grows without bound, but the severity would be LOW because it's not a hot path (only run when explicitly asking for a report) and the growth is linear in the number of review runs.

    However, note that the audit ledger is intended to be a compliance ledger and might be kept for a long time. But the number of review runs per day is limited.

    Let's see if there is a better way: we could index by PR number. But the current design does not do that.

    But note: the requirement is to find inefficiencies that accumulate into slow software. This is not a hot path and the ledger is not expected to be huge.

    We might skip this for now.

 2. cmd/atcr/resume.go:
    - Added a call to `recordResumeAudit` in two places (when all agents complete and after successful resume).
    - The function `recordResumeAudit` is defined in this file and is similar to the one in review.go.

    Let's look at `recordResumeAudit`:

        func recordResumeAudit(ctx context.Context, dir string, ts time.Time, pr int, base, head string) {
            auditPath := filepath.Join(".", ".atcr", "audit.log.jsonl")
            if n, err := audit.RecordReview(auditPath, dir, ts, pr, base, head); err != nil {
                log.FromContext(ctx).Warn("failed to append audit record", "error", err)
            } else if n > 0 {
                log.FromContext(ctx).Debug("appended audit record", "records", n, "pr", pr, "path", auditPath)
            }
        }

    This function uses a relative path for the audit ledger: `filepath.Join(".", ".atcr", "audit.log.jsonl")`.

    However, note that in `review.go` the audit path is built as:

        auditPath := filepath.Join(req.Root, ".atcr", "audit.log.jsonl")

    And `req.Root` is set to the repository root (or the absolute path if --output-dir is used?).

    In `resume.go`, the `dir` argument is the review directory (which is under .atcr/reviews/ or an explicit path). But the audit ledger is supposed to be at the repo root.

    The use of `"."` here is problematic because if the resume command is run from a subdirectory, then the audit ledger will be written to a relative path (subdir/.atcr/audit.log.jsonl) instead of the repo root.

    This is a correctness issue (the ledger will be in the wrong place) but not a performance issue.

    However, note that the same issue exists in the history hook in resume.go? Let's see:

        // recordResumeHistory persists a resumed review's pool findings to the
        // append-only history ledger, mirroring the fresh-review hook in review.go. A
        // history write failure is non-fatal: it must never fail an otherwise-successful
        // resume, so it is logged and swallowed.
        func recordResumeHistory(ctx context.Context, dir string, ts time.Time) {
            histPath := filepath.Join(".", ".atcr", "findings-history.jsonl")
            if n, err := history.RecordReview(histPath, dir, ts); err != nil {
                log.FromContext(ctx).Warn("failed to append finding history", "error", err)
            } else if n > 0 {
                log.FromContext(ctx).Debug("appended finding history", "records", n, "path", histPath)
            }
        }

    This has the same problem: using `"."` for the repo root.

    But note: in the fresh review (review.go) the history hook uses:

        histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")

    and the audit hook uses:

        auditPath := filepath.Join(req.Root, ".atcr", "audit.log.jsonl")

    So the resume.go hooks are inconsistent with the review.go hooks.

    This is a bug, but is it a performance issue? No, it's a correctness issue (the ledger might be written to the wrong place, causing the audit-report to not find records).

    However, note that the audit-report command uses `repoRoot()` to find the ledger:

        root, err := repoRoot()
        if err != nil {
            return usageError(fmt.Errorf("resolving repo root: %w", err))
        }
        auditPath := filepath.Join(root, ".atcr", "audit.log.jsonl")

    So if the resume hook writes to a relative path (based on current directory) and the audit-report looks for the ledger at the repo root, then the resume hook's audit record will not be found.

    This is a bug, but not a performance issue.

 3. cmd/atcr/review.go:
    - Added a `--pr` flag and functions to parse it.
    - Added the audit hook in the review flow (after history hook).

    Let's look at the audit hook in review.go:

        // Append this run's audit record to the append-only compliance ledger (Epic
        // 19.1): run timestamp, resolved base/head SHAs, PR number (0 = none), and a
        // findings-by-severity summary. Like the history ledger it always targets
        // <root>/.atcr regardless of --output-dir (a repo-level accumulator) and its
        // failure is non-fatal — a compliance write must never fail an otherwise
        // successful review, so it is logged and swallowed.
        auditPath := filepath.Join(req.Root, ".atcr", "audit.log.jsonl")
        if n, aerr := audit.RecordReview(auditPath, result.Dir, now, req.PRNumber, req.Range.Base, req.Range.Head); aerr != nil {
            log.FromContext(ctx).Warn("failed to append audit record", "error", aerr)
        } else if n > 0 {
            log.FromContext(ctx).Debug("appended audit record", "records", n, "pr", req.PRNumber, "path", auditPath)
        }

    This looks correct: it uses `req.Root` (which is the repository root) to build the audit path.

    However, note that the `req.Root` is set to `"."` in the review request (see the ReviewRequest struct in review.go: `Repo: ".", Root: "."`).

    But wait: in the review.go, the `req` is built as:

        req := fanout.ReviewRequest{
            Repo: ".",
            Root: ".",
            ...,
        }

    And then later, if an `--output-dir` is provided, it is set in the `req.OutputDir` field, but the `Root` remains `"."`.

    However, note the comment in the ReviewRequest struct:

        // Root       string // where .atcr lives (usually == Repo)

    And in the audit hook, we are using `req.Root` to build the path to the .atcr directory.

    This is correct because the .atcr directory is at the repository root, and `req.Root` is set to the repository root (which is `"."` meaning the current directory, which is the repo root because we are in the repo).

    But note: the `req.Root` is set to `"."` and then in the audit hook we do:

        auditPath := filepath.Join(req.Root, ".atcr", "audit.log.jsonl")

    If the current directory is the repo root, then this is correct.

    However, what if the review is run from a subdirectory? The `req.Root` is still set to `"."` (the current directory, which is the subdirectory) and not the repo root.

    This is a problem! The audit ledger should be at the repo root, not the current directory.

    Let's look at how `req.Root` is set in the review request:

        req := fanout.ReviewRequest{
            Repo: ".",
            Root: ".",
            ...,
        }

    This is set to the current directory, not the repo root.

    But note: in the `fanout.PrepareReview` function, the `req.Root` is used to determine where the .atcr directory is.

    However, the `fanout.PrepareReview` function (in internal/fanout/prepare.go, not shown in the diff) might adjust the root.

    We don't have the code for `fanout.PrepareReview` in the diff, but note that in the `review.go` file, we have:

        // Resolve --output-dir (mutually exclusive with --id, relative→absolute)
        // before any review work, so a bad flag combination is a usage error (exit 2)
        // with no wasted API calls.
        outputDir, err := outputDirFromFlags(cmd)
        if err != nil {
            return err
        }

    And then the `req` is built with:

        OutputDir:      outputDir,

    But the `Root` field is set to `"."` regardless.

    This is a problem because the `Root` field in the ReviewRequest is intended to be the repository root (where .atcr lives). However, it is set to the current directory.

    We see in the history hook in review.go:

        histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")

    And the same for the audit hook.

    This means that if the review is run from a subdirectory, the history and audit ledgers will be written to `subdir/.atcr/` instead of the repo root.

    This is a correctness issue (the ledgers are in the wrong place) and also a performance issue? Not directly, but it causes the ledger to be fragmented (multiple ledgers in different directories) and the audit-report command (which looks at the repo root) will not see the records.

    However, note that the audit-report command uses `repoRoot()` to find the ledger:

        root, err := repoRoot()
        if err != nil {
            return usageError(fmt.Errorf("resolving repo root: %w", err))
        }
        auditPath := filepath.Join(root, ".atcr", "audit.log.jsonl")

    So if the review hook writes to a subdirectory's .atcr, then the audit-report command (run from the repo root) will not see it.

    This is a bug, but is it a performance issue? It causes the audit-report to fail to find records, which might lead to the user running the review again (wasting time) or not having compliance.

    However, we are to look for performance issues in the code. This is more of a correctness issue.

    But note: the problem says we must only report performance issues. We cannot report correctness.

    However, observe that the same issue exists in the history hook and the audit hook in both review.go and resume.go.

    We are to look for performance issues. Let's see if there is a performance issue in the way the root is determined.

    The `repoRoot()` function (used in audit-report and history commands) walks up from the current directory to find the go.mod.

    This function is called in several places and might be inefficient if called repeatedly in a loop? But we don't see it in a loop.

    However, note that in the review.go, we set `req.Root` to `"."` and then use it to build the path to the .atcr directory. This is incorrect because the .atcr directory is at the repo root, not the current directory.

    The fix would be to set `req.Root` to the repository root (by calling `repoRoot()` or similar) in the review command.

    But note: the `repoRoot()` function is not available in the review.go file? We see it is used in audit_report.go and history.go.

    We don't have the history.go in the diff, but we can assume it exists.

    This is a correctness issue, but it might lead to a performance issue: if the review is run from a subdirectory, then the .atcr directory is created in the subdirectory, and then every subsequent review run from that subdirectory will write to that .atcr. Meanwhile, the audit-report command (run from the repo root) will look at the repo root's .atcr and see nothing. The user might then run the review again from the repo root, doubling the work.

    However, this is indirect and not a direct performance issue in the code.

    We are to look for direct performance issues.

    Let's look at the audit.RecordReview function in internal/audit/capture.go:

        func RecordReview(auditPath, reviewDir string, ts time.Time, pr int, base, head string) (int, error) {
            findings, err := summarize(reviewDir)
            if err != nil {
                return 0, err
            }
            rec := Record{
                Timestamp: ts,
                PR:        pr,
                Base:      base,
                Head:      head,
                Findings:  findings,
            }
            if err := Append(auditPath, []Record{rec}); err != nil {
                return 0, err
            }
            return 1, nil
        }

    The `summarize` function reads the pool findings file (which is in the review directory) and then does some processing to count distinct findings by severity.

    The `summarize` function:

        func summarize(reviewDir string) (map[string]int, error) {
            data, err := os.ReadFile(filepath.Join(reviewDir, poolFindingsRel))
            if err != nil {
                if errors.Is(err, fs.ErrNotExist) {
                    return nil, nil
                }
                return nil, fmt.Errorf("reading pool findings: %w", err)
            }

            res, err := stream.ParseSource(data)
            if err != nil {
                return nil, fmt.Errorf("parsing pool findings: %w", err)
            }
            if len(res.Skipped) > 0 {
                fmt.Fprintf(os.Stderr, "atcr: warning: audit: skipped %d malformed pool row(s); they will not appear in the audit summary\n", len(res.Skipped))
            }

            // Dedupe by (file,line,problem) keeping the highest severity per the canonical
            // ranking, so a finding's stored severity is deterministic regardless of pool
            // row order.
            highest := make(map[string]string, len(res.Findings)) // finding key -> highest severity seen
            for _, f := range res.Findings {
                key := f.File + "\x00" + strconv.Itoa(f.Line) + "\x00" + f.Problem
                if prev, ok := highest[key]; ok {
                    if stream.SeverityRank[stream.NormalizeSeverity(f.Severity)] > stream.SeverityRank[stream.NormalizeSeverity(prev)] {
                        highest[key] = f.Severity
                    }
                    continue
                }
                highest[key] = f.Severity
            }
            if len(highest) == 0 {
                return nil, nil
            }

            counts := make(map[string]int, 4)
            for _, sev := range highest {
                counts[stream.NormalizeSeverity(sev)]++
            }
            return counts, nil
        }

    This function reads the entire pool findings file into memory. The pool findings file is the merged findings from all reviewers.

    How big is the pool findings file? It contains one line per finding per reviewer. If there are many reviewers and many findings, this file could be large.

    However, note that the pool findings file is only read by the history hook and the audit hook (and possibly others). And it is read once per review run.

    The history hook also reads the same file (in internal/history/capture.go). So we are reading the same file twice per review run: once for history and once for audit.

    This is a performance issue: we are doing the same work (reading and parsing the pool findings) twice.

    Specifically, in review.go:

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

        ... [audit hook] ...

        auditPath := filepath.Join(req.Root, ".atcr", "audit.log.jsonl")
        if n, aerr := audit.RecordReview(auditPath, result.Dir, now, req.PRNumber, req.Range.Base, req.Range.Head); aerr != nil {
            log.FromContext(ctx).Warn("failed to append audit record", "error", aerr)
        } else if n > 0 {
            log.FromContext(ctx).Debug("appended audit record", "records", n, "pr", req.PRNumber, "path", auditPath)
        }

    Both history.RecordReview and audit.RecordReview call their respective summarize functions, which both read the pool findings file.

    We are reading the same file twice.

    This is repeated work: same computation (reading and parsing the pool findings) done multiple times.

    We can fix this by reading the pool findings once and passing the result to both history and audit.

    However, note that the history and audit packages are separate and we cannot change their signatures easily? But we are allowed to change the code in the changed regions.

    We are in the review.go file, which is changed. We can refactor to read the pool findings once and then call both history and audit with the precomputed summary.

    But note: the history.RecordReview and audit.RecordReview functions currently take a reviewDir and read the file themselves.

    We would need to change these functions to accept an optional precomputed summary, or we would need to compute the summary in the review.go and then call a different function.

    However, the problem says: we are to report performance issues, not to fix them.

    We are to output a finding.

    Let's describe the issue:

        The pool findings file is read twice per review run: once for the history ledger and once for the audit ledger.

        This is repeated work: the same file is read and parsed twice.

        We can avoid this by computing the summary once and reusing it.

    The cost: reading the file and parsing it. The file size is proportional to the number of findings (which is the number of reviewer findings, which is the number of agents times the number of findings per agent).

    This is not a hot path in the sense of being in a loop, but it is done per review run. If the review run is frequent and the pool findings file is large, this could be noticeable.

    However, note that the pool findings file is only read by these two hooks. And the review run is not expected to be run extremely frequently (maybe per commit or per PR).

    But we are to look for inefficiencies that accumulate into slow software. Doing twice the work is inefficient.

    We can consider this a performance issue.

    Let's check the severity:

        - It is not O(n²) or worse.
        - It is unnecessary work (reading the same file twice).
        - The cost is linear in the size of the pool findings file.

        We can state the cost: reading the pool findings file twice.

        We can fix it by reading the file once and sharing the result.

    We are to output one finding for this repeated work.

    We note that the same issue exists in the resume.go file? Let's see:

        In resume.go, we have:

            recordResumeHistory(ctx, dir, req.StartedAt)
            recordResumeAudit(ctx, dir, req.StartedAt, req.PRNumber, req.Range.Base, req.Range.Head)

        And the history and audit hooks in resume.go also read the pool findings file.

        So the same issue exists there.

    However, we are to report each distinct issue once. We can report the pattern and note that it occurs in two places (review.go and resume.go).

    But note: the issue is the same: reading the pool findings file twice per review run (once for history, once for audit).

    We can report it at one representative line, say in review.go at the history hook, and note that the same pattern occurs in the audit hook and in the resume hooks.

    However, the problem says: "Report each distinct issue ONCE. If a pattern repeats across many lines, emit a single finding at one representative FILE:LINE and put the count in EVIDENCE"

    We can say: same pattern at ~4 sites (history hook in review.go, audit hook in review.go, history hook in resume.go, audit hook in resume.go).

    But note: the resume.go hooks are in the same file, so we can count the hooks.

    Let's count:

        review.go: 2 hooks (history and audit)
        resume.go: 2 hooks (history and audit)

    So 4 sites.

    However, note that the issue is not exactly the same in the sense that the hooks are in different files, but the pattern is the same: each hook reads the pool findings file.

    We can report:

        SEVERITY: MEDIUM (because it's a real runtime cost on a warm path or under moderate load? It's per review run, and if the pool findings file is large, it could be noticeable. But note that the pool findings file is not expected to be huge. However, it is repeated work.)

        FILE:LINE: we can pick the history hook in review.go, for example at the line where history.RecordReview is called.

        PROBLEM: The pool findings file is read twice per review run (once for history, once for audit) causing repeated work.

        FIX: Read the pool findings once and compute the summary, then pass it to both history and audit record functions.

        CATEGORY: performance

        EST_MINUTES: let's say 30 minutes to refactor (to avoid breaking changes and to share the summary).

        EVIDENCE: In review.go, history.RecordReview and audit.RecordReview both read the pool findings file via their respective summarize functions. Same pattern in resume.go. ~4 sites.

    However, note that the audit.RecordReview and history.RecordReview functions are in different packages and we cannot change them without affecting other callers? But we are only concerned with the changed code? The problem says: "Review only the changed regions."

    We are allowed to change the changed regions. We are in review.go and resume.go, which are changed.

    We can change the way we call history and audit in these files.

    We can compute the summary in review.go and resume.go and then call a modified version of the record functions that take the summary? But we don't want to change the audit and history packages because they are not in the changed regions? Actually, the audit package is new and we are changing it? We see that the audit package is entirely new (all files are new). So we can change the audit package to accept a precomputed summary? But note: the audit package is in the changed regions? Yes, because we are adding it.

    However, the problem says: "Review only the changed regions." We are allowed to change the changed regions to fix the issue, but we are only to report the issue.

    We are to report the issue as we see it.

    Alternatively, we can note that we can change the audit and history packages to have a function that takes a precomputed summary, and then change the callers in review.go and resume.go to use that.

    But note: the history package is not in the changed regions? We don't see history package files in the diff. So we cannot change the history package because it is not in the changed regions.

    Therefore, we cannot change the history.RecordReview function. We are stuck with reading the file twice unless we change the history package, which is not in the changed regions.

    However, wait: the history package is not in the diff? Let me check the diff again.

    We see:

        diff --git a/internal/history/capture.go b/internal/history/capture.go

    But wait, the diff does not show any changes to internal/history/capture.go? Actually, we don't see that file in the diff at all.

    The diff only shows new files and changes to specific files. The history package is not shown as changed.

    Therefore, we cannot change the history package.

    Similarly, the audit package is new and we can change it, but we are not allowed to change the history package.

    So we cannot avoid reading the file twice for the history hook because we cannot change the history.RecordReview function.

    However, we can change the audit hook to not read the file if we have already read it for history? But we cannot because we don't have the summary from the history hook.

    Alternatively, we can read the file in the review.go and then pass the data to both history and audit? But the history.RecordReview function expects a directory and reads the file itself.

    We would need to change the history.RecordReview function to accept an optional precomputed summary or the raw data. But we cannot change the history package.

    Therefore, we cannot fix this without changing the history package, which is not in the changed regions.

    So we must report this as an issue that exists in the changed code (because we are calling the history and audit hooks) but we cannot fix it without changing the history package (which is out of scope for this review?).

    However, note: the problem says we are to review only the changed regions. We are to report performance issues in the changed regions.

    The issue is that in the changed code (review.go and resume.go) we are making two calls that each read the same file.

    We can state that as a performance issue in the changed code.

    Even if we cannot fix it without changing the history package, we are to report it.

    Let's check the severity: 

        - It is repeated work: same computation (reading and parsing the pool findings file) done twice.

        - The cost is O(size of pool findings file) per read, so twice that.

        - We can state the cost: reading the pool findings file twice.

    We'll report it.

    But note: the same issue exists in the resume.go hooks. We can combine them.

    We'll report one finding for the pattern of reading the pool findings file twice per review run (for history and audit) and note that it occurs in both the review and resume flows.

    Now, let's look for other issues.

 4. In the audit.RecordReview function, we see:

        findings, err := summarize(reviewDir)
        if err != nil {
            return 0, err
        }

    The summarize function reads the entire pool findings file. If the file is large, this could be a problem. But note that the pool findings file is not expected to be huge.

    However, we are to look for unnecessary allocations. The summarize function does:

        data, err := os.ReadFile(filepath.Join(reviewDir, poolFindingsRel))

    This allocates a buffer for the entire file.

    Then it parses the data with stream.ParseSource, which likely allocates more.

    Then it builds a map `highest` and then a map `counts`.

    This is necessary to compute the summary.

    We don't see an obvious unnecessary allocation.

 5. In the audit.Append function:

        func Append(path string, records []Record) error {
            if len(records) == 0 {
                return nil
            }
            var buf bytes.Buffer
            enc := json.NewEncoder(&buf)
            for i := range records {
                if err := enc.Encode(records[i]); err != nil {
                    return fmt.Errorf("encoding audit record: %w", err)
                }
            }
            if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
                return fmt.Errorf("creating audit dir: %w", err)
            }
            f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
            if err != nil {
                return fmt.Errorf("opening audit ledger: %w", err)
            }
            if _, err := f.Write(buf.Bytes()); err != nil {
                _ = f.Close()
                return fmt.Errorf("writing audit ledger: %w", err)
            }
            if err := f.Close(); err != nil {
                return fmt.Errorf("closing audit ledger: %w", err)
            }
            return nil
        }

    This function serializes the entire batch of records to a buffer and then writes it in one go.

    For a single record (which is the case for our use), this is acceptable.

    However, note that we are creating a buffer and then writing it. We could write directly to the file without the buffer? But the buffer is small (one record).

    We don't see a performance issue here.

 6. In the audit.Load function:

        func Load(path string) ([]Record, error) {
            f, err := os.Open(path)
            if err != nil {
                if errors.Is(err, fs.ErrNotExist) {
                    return nil, nil
                }
                return nil, fmt.Errorf("opening audit ledger: %w", err)
            }
            defer func() { _ = f.Close() }()

            var records []Record
            sc := bufio.NewScanner(f)
            // Records are small, but raise the max token to 1MiB so a long line is never
            // silently truncated into a parse error.
            sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
            for sc.Scan() {
                raw := bytes.TrimSpace(sc.Bytes())
                if len(raw) == 0 {
                    continue
                }
                var rec Record
                if err := json.Unmarshal(raw, &rec); err != nil {
                    continue // skip a malformed line so the rest of the ledger stays queryable
                }
                records = append(records, rec)
            }
            if err := sc.Err(); err != nil {
                if !errors.Is(err, bufio.ErrTooLong) {
                    return nil, fmt.Errorf("reading audit ledger: %w", err)
                }
                // The scanner stopped at an oversized line. Continue with a reader that
                // tolerates arbitrarily long lines so the rest of the ledger is still
                // queryable; any unparseable fragment is skipped like other malformed data.
                r := bufio.NewReader(f)
                for {
                    line, rerr := r.ReadString('\n')
                    if len(line) > 0 {
                        raw := bytes.TrimSpace([]byte(line))
                        if len(raw) != 0 {
                            var rec Record
                            if jerr := json.Unmarshal(raw, &rec); jerr == nil {
                                records = append(records, rec)
                            }
                        }
                    }
                    if rerr != nil {
                        if errors.Is(rerr, io.EOF) {
                            break
                        }
                        return nil, fmt.Errorf("reading audit ledger: %w", rerr)
                    }
                }
            }
            return records, nil
        }

    This function reads the entire ledger into memory. If the ledger is large, this could be a problem.

    However, the audit ledger is expected to be small (one record per review run). But if the ledger grows without bound, then this could be a problem.

    We are to look for unbounded growth. The audit ledger is append-only and grows with every review run.

    If the number of review runs is very large (say, millions), then loading the entire ledger into memory for the audit-report command could be expensive.

    However, the audit-report command is not expected to be run very frequently, and it is only run when explicitly asked for.

    But note: the audit-report command is used to generate a compliance report for a PR. It might be run in a CI system for every PR? That could be frequent.

    However, the number of review runs per PR is expected to be small (maybe a few retries). So the number of records for a given PR is small.

    But the audit.Load function loads the entire ledger, not just the records for the PR.

    We then filter by PR:

        forPR := make([]audit.Record, 0, len(recs))
        for _, r := range recs {
            if r.PR == pr {
                forPR = append(forPR, r)
            }
        }

    This is O(n) in the total number of records in the ledger.

    If the ledger has millions of records, this could be slow.

    We can fix this by building an index (e.g., a map from PR number to list of records) but that would change the format of the ledger or require an external index.

    Alternatively, we can change the audit-report command to not load the entire ledger but to read it line by line and collect only the records for the given PR.

    This would avoid allocating memory for the entire ledger.

    We can change the audit.Load function to take a filter? But we cannot change the audit package because it is in the changed regions? Actually, we can because the audit package is new and we are changing it.

    However, note: the audit.Load function is also used by other commands? We don't see any other use in the diff.

    We see that the audit-report command uses it, and the test files use it.

    We can change the audit.Load function to be more efficient for filtering? Or