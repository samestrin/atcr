# Commercial License (Placeholder)

> **This is a placeholder for evaluation, not a license grant.** It does not
> convey any commercial rights by itself. Final commercial terms are negotiated
> on contact (see below), not granted by this file.

## Dual licensing

The `github.com/samestrin/atcr/reconcile` module is **dual-licensed**:

- **Open source — [Apache 2.0](LICENSE).** Use the module under the Apache 2.0
  license for open-source projects. This is the default and covers most use.
- **Commercial — this document.** A separate commercial license is available for
  **proprietary or closed-source embedding** (for example, white-label / OEM use
  where a devtools vendor ships the reconciler as their merge layer and cannot or
  prefers not to comply with Apache 2.0 attribution and notice obligations).

The commercial license is distinct from, and an alternative to, the Apache 2.0
license — it is not an addition to it. Both halves live at the module root
(`LICENSE` and `LICENSE-COMMERCIAL.md`) so the dual-license is visible without
hunting through the file tree.

## What this placeholder is — and is not

- **It is** a statement that a commercial option exists and an invitation to
  start that conversation.
- **It is not** the commercial license text. The actual grant language awaits a
  licensee and legal review; drafting it before there is a counterparty would be
  premature.

## Contact path

To initiate a commercial licensing discussion, use the project's GitHub channel —
a path that moves with the repository and survives contact with public forks
(preferred over a personal email, which can rot before a vendor reaches out):

- **Open a GitHub issue:** <https://github.com/samestrin/atcr/issues/new> with
  the title prefixed `[commercial-license]`, or
- **Start a GitHub Discussion:** <https://github.com/samestrin/atcr/discussions>
  in the Q&A / licensing category.

Either route reaches the maintainer (Sam Estrin), who will follow up to scope
terms.

## No enforcement — by design

This project intentionally ships **no** license-enforcement, license-check,
license-validation, payment-gating, feature-gating, or telemetry / phone-home
code anywhere in the `./reconcile/` module. The absence of enforcement is a
deliberate design choice, not an oversight or a gap a future release will "fix":

- Commercial compliance is a **legal-wrapper** concern handled through the
  contact path above, **not a runtime concern** baked into the binary.
- Embedders get a clean, dependency-light, stdlib-only library with no hidden
  runtime checks gating its behavior.

If you expected automated detection or a runtime gate, there is none on purpose.
