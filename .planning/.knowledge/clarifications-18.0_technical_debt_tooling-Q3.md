---
id: mem-2026-07-03-3b5548
question: "How should an interactive atcr CLI command be built so it stays testable in TDD/CI?"
created: 2026-07-03
last_retrieved: ""
sprints: []
files: []
tags: [clarifications, epic-18.0_technical_debt_tooling, testing, cli, cobra, convention]
retrievals: 0
status: active
type: clarifications
---

# How should an interactive atcr CLI command be built so it st

## Decision

Make the command flag-driven as the primary path and layer the interactive TTY prompt on top, gated on "no required flags provided AND stdin is a TTY". This is a superset of an interactive-only spec and matches repo convention. The CLI is Cobra (spf13/cobra). The precedent is `quickstart`, which is deliberately built with injectable streams (quickstartOpts{in io.Reader; out/errOut io.Writer}) "so the interactive flow is unit-testable without a TTY", wired from cmd.InOrStdin(), tested via strings.NewReader, and degrading cleanly at EOF. Keep the interactive path a real wizard, not a stub, when an AC calls for it.

Evidence:
- cmd/atcr/quickstart.go:22-33 (injectable streams, testable-without-TTY doc)
- cmd/atcr/quickstart.go:57 (InOrStdin wiring), quickstart.go:181-196 (clean EOF/non-TTY degrade)
- cmd/atcr/quickstart_test.go:45 (tests drive prompts via strings.NewReader)
- cmd/atcr/report.go:25-27, cmd/atcr/benchmark.go:51 (flag-driven command convention)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
