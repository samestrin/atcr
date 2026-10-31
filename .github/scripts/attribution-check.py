#!/usr/bin/env python3
"""Fail a pull request that would put anyone but the owner into a public repo's history.

    attribution-check.py --range <base>..<head> --allow sam.estrin@gmail.com \
        [--allow <bot email>]... [--message-file <PR title file>] [--message-file <PR body file>]

Checks every commit in the range, and each --message-file (the PR title and body, which GitHub
may use as the squash message):
  - author and committer email must be on the --allow list. GitHub's own committer identity
    (noreply@github.com, from update-branch and web edits) is accepted as committer only;
  - no Co-authored-by, Claude-Session or Generated-with line, and no claude.ai session link.

Prints one GitHub `::error` line per problem and exits 1; exits 2 if git fails; 0 when clean.
Python 3 standard library only, so a public repo can vendor this file as is.

Why (2026-10-30): a stranger's GitHub account, Claude and Paperclip showed as contributors on
public atcr. A test fixture's `t@t.t` identity leaked into real commits, and Co-authored-by
lines came through GitHub-side squash merges, which no local hook sees.

The canonical copy is samestrin/research `paperclip/orchestrator/attribution-check.py`, with its
tests; public repos carry a copy at `.github/scripts/attribution-check.py`.
"""
from __future__ import annotations

import argparse
import re
import subprocess
import sys

# Matched at the start of a line, after any indentation or emoji ("🤖 Generated with"),
# case-insensitive.
FORBIDDEN = re.compile(
    r"^[\W_]*(co-authored-by\s*:|claude-session\s*:|generated[\s-]with\b|https://claude\.ai/code/session_)",
    re.IGNORECASE,
)
GITHUB_COMMITTER = "noreply@github.com"
SEP = "\x1f"
END = "\x1e"


def forbidden_lines(text: str) -> list[str]:
    """The attribution lines in a message, stripped of indentation."""
    return [ln.strip() for ln in text.splitlines() if FORBIDDEN.match(ln)]


def commits(repo: str, rng: str) -> list[dict]:
    fmt = SEP.join(["%H", "%an", "%ae", "%cn", "%ce", "%B"]) + END
    out = subprocess.run(["git", "-C", repo, "log", f"--format={fmt}", rng],
                         check=True, capture_output=True, text=True).stdout
    rows = []
    for rec in out.split(END):
        rec = rec.lstrip("\n")
        if not rec:
            continue
        sha, an, ae, cn, ce, body = rec.split(SEP, 5)
        rows.append({"sha": sha, "an": an, "ae": ae, "cn": cn, "ce": ce, "body": body})
    return rows


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--repo", default=".")
    p.add_argument("--range", required=True, help="<base>..<head>")
    p.add_argument("--allow", action="append", required=True,
                   help="an email allowed as author or committer (repeat)")
    p.add_argument("--message-file", action="append", default=[],
                   help="a file holding text that may become the squash message (repeat)")
    a = p.parse_args(argv)
    allowed = {e.lower() for e in a.allow}

    try:
        rows = commits(a.repo, a.range)
    except subprocess.CalledProcessError as e:
        print(f"::error::git log {a.range} failed: {e.stderr.strip()}")
        return 2

    problems = []
    for c in rows:
        short = c["sha"][:10]
        if c["ae"].lower() not in allowed:
            problems.append(f"{short}: author {c['an']} <{c['ae']}> is not the owner or a listed bot")
        if c["ce"].lower() not in allowed and c["ce"].lower() != GITHUB_COMMITTER:
            problems.append(f"{short}: committer {c['cn']} <{c['ce']}> is not the owner or a listed bot")
        for ln in forbidden_lines(c["body"]):
            problems.append(f"{short}: message has attribution line: {ln}")
    for path in a.message_file:
        with open(path, encoding="utf-8") as f:
            for ln in forbidden_lines(f.read()):
                problems.append(f"{path}: planned squash message has attribution line: {ln}")

    for msg in problems:
        print(f"::error::{msg}")
    if problems:
        print(f"{len(problems)} problem(s). Reword or re-author the commits (git rebase, git commit "
              "--amend --reset-author) and edit the PR title/body; nothing but the owner may reach "
              "a public repo's history.")
        return 1
    print(f"ok: {len(rows)} commit(s), {len(a.message_file)} message file(s), no foreign identity or attribution")
    return 0


if __name__ == "__main__":
    sys.exit(main())
