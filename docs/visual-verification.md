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

## The deferred Isolate backend

The original plan for running browser code included an Isolate-based engine.
That backend is deferred: no Isolate runtime is named, and a Playwright spec
needs a real browser, which an Isolate does not provide. The `BrowserBackend`
interface is the extension point where such a runner would be added later;
nothing in the engine assumes Playwright is the only backend.
