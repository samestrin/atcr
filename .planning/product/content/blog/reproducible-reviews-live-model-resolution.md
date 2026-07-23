# Reproducible AI Reviews in a World Where Models Change Under You

**Status:** Draft
**Target:** Platform Engineers, DevOps, Reliability-minded Teams
**Publication Phase:** 2 — Education & Architecture
**Grounded in:** Epic 19.7 (live model resolution: family/channel bindings, lock, `models check`, `personas upgrade`)

## The Hook

Here's a failure mode nobody warns you about: your AI review pipeline was green for months, you changed nothing, and one morning the findings are different. You didn't touch a line. The *model* changed under you — a provider rotated a slug, deprecated a version, or silently pointed "latest" at a new snapshot. Your reproducible pipeline quietly stopped being reproducible.

Binding a reviewer to a moving target is convenient right up until the target moves.

## The Technical Challenge

There are two bad extremes. **Pin every persona to a hard model slug** and your reviews are reproducible but you're doing manual archaeology every time a provider ships a new version — and you'll eventually pin to something deprecated. **Bind to "latest"** and you get free upgrades and non-reproducible reviews that change without a diff to explain them. What you actually want is: reproducible *by default*, upgradeable *on purpose*, and *loud* when the ground shifts.

## The ATCR Solution

ATCR layers live model resolution over persona bindings, so a persona binds to a vendor *family/channel* and resolves to a concrete slug recorded in a **lock** (19.7):

- **Reproducible by default:** the lock pins the resolved slug, so reviews stay identical run to run and only change on an explicit **`atcr personas upgrade`** — which resolves family/channel bindings against the live OpenRouter catalog and reports a before→after slug per persona.
- **A grammar that matches intent:** bindings support `<family>@stable` / `@latest`, or `pin:<slug>` for a slug that *never* floats — resolved via provider aliases, a created-timestamp vendor scan, or an explicit pin.
- **Loud on drift:** **`atcr models check [--json]`** reports drift (a newer family member is available), deprecation, and missing-slug conditions with machine-readable output and exit codes — drop it in CI and get told when a reviewer's model is aging out, instead of finding out from mystery findings.
- **Safe upgrades:** a *major*-version model jump gates on the persona's fixture still passing and surfaces a "verify" flag; a *minor* jump auto-locks. Upgrades don't silently change reviewer behavior without a signal.

So your reviews are frozen until you decide to move them — and when the model world shifts underneath you, you hear about it from `models check`, not from a confused developer.

## Call to Action

Put `atcr models check --json` in CI and let it fail loudly the day a reviewer's model drifts or deprecates. Keep your reviews reproducible with the lock, and upgrade with `atcr personas upgrade` when *you* choose — with the fixture gate proving the new model still holds the line.

## Next Steps (drafting notes)

- [ ] Show a `models check --json` output with a drift and a deprecation condition.
- [ ] Example CI step that gates on model drift.
- [ ] Explain the `@stable` / `@latest` / `pin:` grammar with concrete slugs.
