# Phase 1 Spike Findings (Sprint 17.0)

**Recorded:** 2026-07-03
**Status:** Both spikes executed and passed. Throwaway spike code removed; findings + reusable fixtures retained here for Phase 2 (RED) and Phase 4/6.

Both spikes were written as `_test.go` files, run green, then deleted per the
Phase 1 "throwaway spike code removed" DoD item. `go-gitdiff v0.8.1` remains in
`go.mod`/`go.sum`. This note is the durable artifact that carries forward.

---

## Spike 1.1 — `go-gitdiff` against representative fixtures

**Dependency:** `github.com/bluekeyes/go-gitdiff v0.8.1` (added via `go get`).

**API confirmed:**
- `gitdiff.Parse(r io.Reader) ([]*File, string, error)` — returns parsed files, preamble, error.
- `gitdiff.Apply(dst io.Writer, src io.ReaderAt, f *File) error` — applies `f` to `src`, writes result to `dst`.
- `gitdiff.File` carries `OldName`, `NewName`, `IsNew`, `IsDelete`, `TextFragments`.

**Behavior per diff type:**

| Diff type | Flags observed | Apply behavior |
|-----------|----------------|----------------|
| modify | `IsNew=false, IsDelete=false`, both names set | clean apply, exact expected output |
| create (`/dev/null` old-side) | `IsNew=true, OldName=""`, `NewName` set | apply against **empty** `src` yields new content |
| delete (`/dev/null` new-side) | `IsDelete=true, NewName=""`, `OldName` set | apply yields **empty** output |
| drifted-context (non-locatable hunk) | — | **non-nil error** |

**THE CONTRACT (Story 1 depends on this):** A hunk that cannot be located in the
source produces a **hard failure** — `gitdiff.Apply` returns a non-nil error
(`conflict: fragment line does not match src line`), wrapped as **both**
`*gitdiff.ApplyError` and `*gitdiff.Conflict`. **No silent mis-apply.** CONFIRMED.

**Interface decision for Story 1:** Treat **any** non-nil `gitdiff.Apply` error as
a hard per-file failure. Do NOT branch on error type (the wrapping is an
implementation detail); `errors.As(*ApplyError)` / `*Conflict` are available if a
richer message is wanted, but the pass/fail decision is simply `err != nil`.

**Reusable fixtures (verbatim, for Phase 2 RED):**

```
// modify — src: "line1\nline2\nline3\n"  ->  "line1\nline2-modified\nline3\n"
diff --git a/foo.txt b/foo.txt
index 1111111..2222222 100644
--- a/foo.txt
+++ b/foo.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2-modified
 line3

// create — src: ""  ->  "hello\nworld\n"  (IsNew=true)
diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world

// delete — src: "gone1\ngone2\n"  ->  ""  (IsDelete=true)
diff --git a/del.txt b/del.txt
deleted file mode 100644
index 4444444..0000000 100644
--- a/del.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-gone1
-gone2

// drift — src that does NOT contain the hunk context -> hard error
diff --git a/drift.txt b/drift.txt
index 5555555..6666666 100644
--- a/drift.txt
+++ b/drift.txt
@@ -1,3 +1,3 @@
 alpha
-beta
+beta-modified
 gamma
```

---

## Spike 1.2 — GitHub Git Data API 4-call sequence via existing `postDo`/`get`

**Driven entirely through `internal/ghaction.Client.postDo`** against an
`httptest.Server` — **no second HTTP client required.**

**Sequence confirmed:** `blob → tree → commit → ref`

| # | Method + path | Request body (keys) | Response parsed |
|---|---------------|---------------------|-----------------|
| 1 | `POST /repos/{o}/{r}/git/blobs` | `content`, `encoding` | `{sha}` |
| 2 | `POST /repos/{o}/{r}/git/trees` | `base_tree`, `tree:[{path,mode,type,sha}]` | `{sha}` |
| 3 | `POST /repos/{o}/{r}/git/commits` | `message`, `tree`, `parents` | `{sha}` |
| 4 | `POST /repos/{o}/{r}/git/refs` | `ref:"refs/heads/<b>"`, `sha` | `{ref, object:{sha}}` |

**Confirmed:**
- `Authorization: Bearer <token>` and `X-GitHub-Api-Version: 2022-11-28` flow on
  **every** call (set inside `postDo`) — Story 4 inherits auth for free.
- `postDo`'s `out` param decodes each `{sha}` response cleanly.
- **422 "Reference already exists"** on the ref-create call surfaces as
  `*ghaction.APIError{StatusCode: 422, Message: "Reference already exists"}` —
  **distinguishable and recoverable** via `errors.As(err, *APIError)` +
  `StatusCode == 422`. This is the mechanism AC 04-02 (branch collision) uses.
- Existing retry/backoff (`postDo` retries 5xx/429) carries these calls unchanged.

**Interface decision for Story 4:** `CreateBranch`/`CreateCommit` route all HTTP
through the single existing `postDo` — no new client, no scattered auth. For
`CreateBranch`, a `POST /git/refs` returning 422 is a **recoverable collision**,
not a generic failure.

**Reusable stub-routing pattern (for Phase 4/6 tests):** route the `httptest`
handler on **method + path suffix** (`strings.HasSuffix(r.URL.Path, "/git/blobs")`
etc.), never on call order — this makes the 4-call sequence tests order-robust
and is what the Phase 6 zero-HTTP-on-failure regression test relies on. Capture
each request's `Authorization` / `X-GitHub-Api-Version` header + a call counter
so tests can assert both auth flow and the "zero calls on validation failure"
guarantee.
```
