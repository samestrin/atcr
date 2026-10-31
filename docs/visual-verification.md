# Visual Verification

Visual verification runs a Playwright spec against a page from the change under
review and brings back what the browser saw: screenshots, console errors, and
failed network requests. The spec is model-written code, so it runs only inside
the [sandbox](execution.md) — no network, a read-only snapshot of the repo, and
a writable copy that dies with the run. The browser backend sits behind a small
interface (`internal/fanout`, `BrowserBackend`) so another runner can be added
later; the Playwright backend is the one that ships today.

## Prerequisite: a Playwright project in the repo under review

The backend never scaffolds anything. It runs only when the repo under review
already has:

- a `playwright.config.*` at the repo root (any of the names Playwright itself
  looks for: `.ts`, `.js`, `.mts`, `.mjs`, `.cts`, `.cjs`), and
- `@playwright/test` installed — declared in `package.json` **and** present in
  `node_modules`. The run has no network, so a declared-but-not-installed
  dependency cannot be fetched on the way in.

If either is missing the backend returns a typed `ErrNoPlaywrightProject` error
before anything runs. atcr writes no `package.json` and no config of its own.

On the operator side, the sandbox image must carry Node and the browsers. The
default Docker image (`alpine:3.20`) has neither, so `Preflight` refuses it
with `ErrPlaywrightImageRequired`. Set `sandbox.image` in `.atcr/config.yaml` to
an image that has both — for example the official
`mcr.microsoft.com/playwright` image, prepared with `npx playwright install`.

## The dev server runs inside the sandbox

The sandbox gives every run **no network at all** — no egress, and no inbound
except loopback inside the run itself. So the page under test must come up
inside the same sandbox run as the browser: the repo's own Playwright
[`webServer`](https://playwright.dev/docs/test-webserver) config starts the dev
server, and the spec and the server reach each other on the run's loopback. The
server's start-up time counts against the run timeout.

A consequence worth knowing: an `https://` preview URL passes flag validation,
but under the no-network sandbox nothing outside the run can be reached, so
such a page cannot load. Only the loopback server the `webServer` config starts
is reachable.

## `--preview-url`

`atcr review --preview-url <url>` names the page a visual-verification spec
opens. It follows the same rule as `--telemetry-endpoint`:

- `https://` is always accepted;
- `http://` is accepted only for a loopback host: `localhost`, `127.0.0.0/8`,
  or `::1`;
- anything else (another scheme, a non-loopback `http://` host, an empty host)
  is a usage error (exit 2) before any review work, and the rejected value is
  not echoed in the error.

The flag is handed to the spec through the `ATCR_PREVIEW_URL` environment
variable inside the run.

## Timeout

A Playwright run is bounded by a 2 minute default timeout. At the deadline the
sandbox kills the run's whole process group — the browser and the dev server
together — and the backend returns a result marked timed out (exit code 124)
rather than hanging the review.

## When Playwright cannot run

How an unavailable Playwright surfaces depends on whether visual verification
was requested:

- **`--preview-url` was given:** a missing Playwright project or an unusable
  sandbox is a **hard error**, not a skip — the review fails rather than
  silently dropping the visual check you asked for.
- **Not given:** the review continues, with the visual check reported as
  timed out where a run exceeded its deadline.

## The darwin OS-level backend is refused

The OS-level sandbox backend on macOS (darwin) cannot host a browser run: its
sandbox-exec profile denies all network, loopback included, so the dev server
and the browser inside one run cannot reach each other. The backend refuses it
with `ErrBrowserBackendUnsupported` before anything runs. **Use the Docker
backend or run on Linux** — Linux's OS-level sandbox keeps loopback.

## The visual review: felix and the vision agent

Visual verification becomes a visual **review** when the `felix` persona is in
the roster (`agents:` in `.atcr/config.yaml`). Felix is a community persona for
frontend QA. It never reviews the diff: it gets no reviewer slot, and with
`--preview-url` it runs after the reviewers as a separate stage. Without
`--preview-url` felix is skipped with a warning.

The stage makes **two model calls**, to two different agents:

1. **Call 1 — felix writes the spec.** Felix's own `agents:` entry and persona
   prompt receive the reviewers' diff and author a Playwright spec, written to
   `e2e/felix-<review id>.spec.ts` in the repo under review (an existing file
   is never overwritten). The browser then runs that spec in the sandbox.
2. **Call 2 — the vision agent reviews the screenshots.** The agent named by
   `visual_review.agent` receives the screenshots, plus the persona's rendered
   screenshot-review prompt and the captured console, failed-request and
   failed-test text, and replies with findings that are reported under felix's
   name.

The vision agent is **local by default**. It must declare
`supports_vision: true` in the registry, and its provider's `base_url` must be
a loopback host — so out of the box screenshots never leave the machine. An
empty `base_url` is the client's remote default and counts as non-loopback. An
unset `visual_review.agent`, a missing agent, one without `supports_vision`,
or a non-loopback provider without the opt-in is a usage error (exit 2) raised
before any provider call.

### `--allow-cloud-vision`: pixels leave the machine

`atcr review --allow-cloud-vision` is the opt-in that lets a vision agent on a
non-loopback provider receive the screenshots. With it, **the screenshot
pixels leave the machine**, and **`secret_guard` does not scan pixels**: the
guard scans every text bound for the provider (the prompt, the screenshot
names, and the captured console, failed-request, failed-test and spec-path
text), but the image bytes themselves pass through unscanned. When the agent
is non-loopback the run warns about this and felix's `status.json` records
`images_sent_off_machine: true`; a loopback vision agent writes no such field.
The flag only applies with `--preview-url`.

## Evidence and findings: `.atcr/reviews/<id>/visual/`

Everything the browser run captures is written under the review directory, in
`.atcr/reviews/<id>/visual/`:

- **Screenshots**, one image file per capture. The file name is the runner's
  attachment name sanitized to `[a-z0-9._-]` (at most 64 bytes, de-duplicated
  with `-2`, `-3`, … suffixes), so a name can never escape `visual/`.
- **`console.txt`** — the captured console output, when non-empty.
- **`network.txt`** — the failed network requests, when non-empty.

The text files pass `secret_guard` under the configured policy before they are
written (the screenshot pixels are not scanned, as above). The text files are
written even when the run captured no screenshots. Nothing is written — and no
`visual/` directory is created — when the run captured no screenshots and no
console or network text.

The browser run also **files its own findings**, separate from the vision
agent's: one MEDIUM `out-of-scope` finding per distinct console error and per
distinct failed network request, filed at line 1 of felix's spec. Each finding's
EVIDENCE starts with a tag naming its source — `ui-console:` for a console
error, `ui-network:` for a failed request — and names the saved `visual/<file>`
evidence. Because these findings are **`out-of-scope`**, the reconciler counts
them in the summary and keeps them in the artifacts, but it never promotes them
and they **never fail the `--fail-on` severity gate**: a broken page the change
did not cause cannot block the review.

A finding whose EVIDENCE names files under `visual/` carries structured
`visual_evidence` in `findings.json`: the screenshot paths, the console log it
cites, and an excerpt of that log (the head, capped at 20 lines or 2048 bytes).
A reference counts only when the file actually exists as a regular file inside
a real (not symlinked) `visual/` directory, so a finding that merely mentions
such a path is left untouched.

Reports render that evidence. The markdown report embeds the screenshots as
links and the console excerpt in a fenced block. The SARIF report carries one
**attachment** per screenshot on the finding's result, each with a
review-dir-relative `visual/<file>` URI, so a SARIF viewer can open the
screenshot alongside the finding.

## The deferred Isolate backend

The original plan for running browser code included an Isolate-based engine.
That backend is deferred: no Isolate runtime is named, and a Playwright spec
needs a real browser, which an Isolate does not provide. The `BrowserBackend`
interface is the extension point where such a runner would be added later;
nothing in the engine assumes Playwright is the only backend.
