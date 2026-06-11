Code review.

A pre-computed unified diff is at:
  {{.DiffPath}}
Size: {{.DiffBytes}} bytes ({{.DiffLines}} lines). Range: {{.BaseRef}}..{{.HeadRef}}. Repo clone: {{.RemoteRepo}}/

INSTRUCTIONS — follow exactly:
1. Run `cat {{.DiffPath}}` ONCE. That is the diff you must review.
2. If the cat output looks empty or wrong, run `ls -la {{.DiffPath}}` and
   `wc -l {{.DiffPath}}` and INCLUDE the literal output in your reply.
3. Do NOT report "repository missing", "clone failed", or "refs not found"
   unless step 2 actually shows the file is absent. Hallucinating an
   infrastructure failure when the file exists is worse than no review.
4. You may also `cd {{.RemoteRepo}}` and inspect individual files referenced
   in the diff for context. The clone IS there.

{{if .LargeDiff}}LARGE DIFF WORKFLOW (REQUIRED — diff is {{printf "%.1f" .DiffMB}} MB):

This diff is too large for full-text review within your time budget. Follow
this workflow exactly:

a. FIRST, run `git -C {{.RemoteRepo}} diff --stat {{.BaseRef}}..{{.HeadRef}}` to see the
   per-file change summary. Pick AT MOST 10 files: the largest by line
   count, plus any files in security-sensitive paths (auth, crypto,
   sessions, permissions, payments, SQL).
b. For each picked file, `cat` only that file's hunk:
   `git -C {{.RemoteRepo}} diff {{.BaseRef}}..{{.HeadRef}} -- <path>`. Do NOT cat the full diff.txt.
c. After inspecting your picked files, produce findings. Stop exploring.
   You have one job: write the review.
d. Tool-call budget: aim for at most 15 tool calls total (1 for --stat,
   up to 10 for per-file diffs, the rest for surgical greps if needed).
   If you've made 20+ tool calls without writing findings, you are
   over-exploring — stop and write what you have.

{{end}}## Scope rule — STRICT

The diff defines what's in scope. A finding is in-scope only if its `FILE:LINE` is on a line **touched by the diff** (added, removed, or modified — not just present in a file the diff touches).

- Findings about unmodified code you inspected for context (a caller, a sibling function, an adjacent file) are OUT OF SCOPE.
- Out-of-scope observations MAY appear in the prose "out-of-scope" section of your review — useful signal for the sprint owner.
- Out-of-scope observations MUST NOT appear in TD_STREAM. TD_STREAM is consumed by automation (`/resolve-td`) that opens fix work against the sprint; out-of-scope rows generate noise and rework.
- When in doubt about whether a line was touched, leave it out of TD_STREAM.

Produce your normal review report (verdict + severity-graded findings + what was done well + out-of-scope).
Reply with the review body only — no preamble.

After your normal review, append a section titled "TD_STREAM" with each finding as a single pipe-delimited line in this format:

  SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY

Where SEVERITY is HIGH/MEDIUM/LOW (map blocking->HIGH, significant->MEDIUM, minor->LOW). One line per finding. No header row, no commentary in this section. Include only in-scope findings per the scope rule above.

IMPORTANT: If any field (PROBLEM, FIX, etc.) needs to contain a literal pipe character, replace it with a forward slash (/). The pipe is the column separator and unescaped pipes will corrupt the row.
