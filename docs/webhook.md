# Slack-triggered reviews (`atcr webhook`)

`atcr webhook` is a small daemon that reviews a GitHub pull request when
someone mentions your Slack app:

```
@atcr review https://github.com/acme/widget/pull/7
```

The daemon checks the request, fetches the pull request into a throwaway
worktree of a local checkout you configured, runs the review there and replies
in the same Slack thread with a short summary.

## What the reply contains

The reply is the review's severity counts and up to 10 finding titles, most
severe first:

```
*ATCR review of acme/widget#7*: 3 findings (1 HIGH, 2 MEDIUM)
• [HIGH] Unchecked error from Close
• [MEDIUM] ...
```

A title is the first line of the finding's problem text, cut to 120
characters. Before it is sent, atcr removes every word that holds a path
separator (file paths and URLs), the configured credential values and anything
shaped like an API, Slack or GitHub token. File names, fixes, evidence and the
reviewers' raw output never reach Slack.

If the review fails, times out or is cancelled, the reply says so in one line
and nothing more. The details are in the daemon's log and in the review
directory (see [Where reviews are written](#where-reviews-are-written)).

## Configuration

The daemon reads the `webhook:` block of `.atcr/config.yaml` in the directory
you start it in. That project's review settings (agents, personas, payload
mode, consensus) are the ones every webhook review uses. A pull request cannot
change them: its own `.atcr/` is never read.

```yaml
agents: [bruce, sasha]

webhook:
  listen: 127.0.0.1:8787        # default; loopback only
  max_body_bytes: 1048576       # default 1 MiB; a larger request gets 413
  job_timeout: 30m              # default; a review running longer is cancelled
  github_token_env: GITHUB_TOKEN
  repos:
    acme/widget: /srv/checkouts/widget
  slack:
    bot_token_env: SLACK_BOT_TOKEN
    signing_secret_env: SLACK_SIGNING_SECRET
    workspaces: [T0123ABCD]
    channels: [C0456EFGH]
    users: [U0789IJKL, U0789MNOP]
```

atcr checks the block when it loads the config and reports every problem at
once.

### Credentials: environment-variable names, never values

Every credential key holds the **name** of an environment variable. atcr reads
the value from the environment when the daemon starts, so a token is never
written into the config file. A key whose value does not look like a variable
name (letters, digits and `_`, not starting with a digit) is rejected.

| Key | Holds the name of | Required |
| --- | --- | --- |
| `webhook.slack.signing_secret_env` | The Slack app's **signing secret**, used to check each request's signature | Yes |
| `webhook.slack.bot_token_env` | The Slack app's **bot token** (`xoxb-...`), used to post the reply | Yes |
| `webhook.github_token_env` | A GitHub token | No |

If a required variable is not set, `atcr webhook` exits with status 2 before it
starts listening, and the error names the variable, not a value.

`github_token_env` is optional. The pull request is fetched with the git
credentials the checkout already has (its `origin` remote), so a checkout that
can `git fetch` needs nothing more. When the variable is set, its value is
added to the list of values scrubbed from replies and logs.

The model providers' API keys come from your registry as usual (`api_key_env`,
see [registry.md](registry.md)).

### Allowlists

Every list must be present and non-empty. An empty or missing list admits
nobody; it never means "anyone".

| Key | What must match |
| --- | --- |
| `webhook.repos` | The pull request's `owner/repo` (matched case-insensitively, as GitHub does). Each entry maps it to the **absolute** path of a local git checkout of that repository. |
| `webhook.slack.workspaces` | The Slack workspace (team) id the mention came from |
| `webhook.slack.channels` | The channel id the mention was posted in |
| `webhook.slack.users` | The user id of the person who mentioned the app |

The workspace, channel and user are checked before the message is read. A
mention that fails any check gets no reply at all, so an unlisted user learns
nothing; the refusal is logged.

The only accepted command is `review` followed by one URL of the form
`https://github.com/<owner>/<repo>/pull/<n>`. Any other host, an extra path
segment, `..` or a query string is refused.

### Listen address: loopback by default

The daemon binds `127.0.0.1:8787` unless `webhook.listen` says otherwise, and
it refuses any address that is not loopback (`localhost`, `127.0.0.0/8` or
`::1`), both when it loads the config and when it binds. It is never exposed
on a network interface directly. To receive Slack's requests, put a tunnel or
a reverse proxy on the same host in front of it and forward
`https://<your-host>/slack/events` to `http://127.0.0.1:8787/slack/events`.

## Setting up the Slack app

1. Create a Slack app and install it to your workspace.
2. Under **OAuth & Permissions**, add the bot scopes `app_mentions:read` and
   `chat:write`. Copy the **Bot User OAuth Token** into the variable named by
   `bot_token_env`.
3. Under **Basic Information**, copy the **Signing Secret** into the variable
   named by `signing_secret_env`.
4. Start the daemon (next section), then under **Event Subscriptions** set the
   Request URL to your tunnel's `/slack/events` URL. Slack sends a
   `url_verification` challenge, which the daemon answers.
5. Subscribe to the bot event `app_mention`.
6. Invite the app to each allowlisted channel.

## Running it

```
export SLACK_SIGNING_SECRET=... SLACK_BOT_TOKEN=xoxb-...
atcr webhook
```

The daemon prints the address it listens on to stderr and logs one line for
each refused request and each failed job. It runs until it gets SIGINT or
SIGTERM; then it stops accepting requests, cancels the running reviews and
removes their worktrees before it exits.

## How a request is handled

1. **Signature.** The request must carry `X-Slack-Signature` and
   `X-Slack-Request-Timestamp`. The signature is checked against the signing
   secret with a constant-time compare, and a timestamp more than 5 minutes
   from the daemon's clock is refused (this blocks replays). A failed check
   gets HTTP 401.
2. **Acknowledgement.** A valid request gets HTTP 200 at once, before any work
   starts, well inside the 3 seconds Slack waits before it retries.
3. **Retries.** Slack retries a delivery it thinks was lost. Each event id is
   remembered for an hour and a repeated one is ignored, so one mention runs
   one review.
4. **One job per pull request.** While a review of a pull request is running,
   another mention of the same pull request is refused and logged.
5. **Checkout.** atcr fetches `refs/pull/<n>/head` from the checkout's `origin`
   into a branch under `atcr-webhook/` and adds a worktree for it in the system
   temporary directory. Git hooks are disabled for these commands, so a hook
   the pull request adds never runs. The checkout's own branches, index and
   working tree are not touched.
6. **Review.** The review covers the range from the merge base with the
   checkout's default branch (`origin/HEAD`, else `origin/main` or
   `origin/master`) to the pull request head, and is reconciled the way
   `atcr review --fail-on` reconciles it.
7. **Reply and cleanup.** The reply is posted in the mention's thread, with a
   10-second timeout. The worktree and its branch are removed whether the
   review succeeded, failed, timed out or was cancelled, so a killed job leaves
   no half-written branch or worktree. A Slack error is logged and does not stop
   the daemon.

The review range uses the default branch, not the pull request's own base
branch. A pull request that targets another branch is reviewed against the
default branch.

## Linear-triggered reviews

With a `webhook.linear:` block the daemon also reviews the GitHub pull request
attached to a Linear issue when the issue moves to one of your trigger
statuses. Point a Linear webhook for Issue events at your tunnel's
`/linear/webhook` URL. Either trigger block is enough on its own.

```yaml
webhook:
  repos:
    acme/widget: /srv/checkouts/widget
  linear:
    webhook_secret_env: LINEAR_WEBHOOK_SECRET   # the webhook's signing secret
    api_key_env: LINEAR_API_KEY                 # reads the issue's attachments
    trigger_statuses: [In Review]
    teams: [EST]                                # team key or id
```

1. **Signature.** `Linear-Signature` must be the HMAC-SHA256 of the body under
   the signing secret, and the body's `webhookTimestamp` must be within a
   minute of the daemon's clock. A failed check gets HTTP 401.
2. **Acknowledgement.** A valid request gets HTTP 200 at once. The pull request
   lookup happens afterwards, inside the job.
3. **Trigger.** Only an Issue `update` whose `updatedFrom` shows the state
   changed, in a listed team, to a listed status (names match ignoring case)
   runs a review. Any other edit runs nothing.
4. **Repeats.** The issue id and the new state are remembered for an hour, so a
   redelivery, or a second delivery of the same status change, runs one review.
5. **Pull request.** The job asks the Linear API (10-second timeout) for the
   issue's attachments and takes the first GitHub pull request URL. A pull
   request whose repository is not in `repos` is ignored and logged.
6. **One job per pull request.** While a review of that pull request is
   running, from Linear or Slack, the new event is refused and logged.

The checkout, review and cleanup are the same as for a Slack mention. The
daemon logs the finding count when the review finishes.

## Where reviews are written

Each webhook review is a normal review directory under `.atcr/reviews/` of the
project the daemon runs in, and `.atcr/latest` points at the most recent one.
Use `atcr report` and the other commands on it as usual. The daemon logs the
directory when a review finishes.
