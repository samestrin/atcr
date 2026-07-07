# Design Note: Extending the Review-Time Persona Resolution Chain

**Phase 1 spike output — Sprint 19.6 (Community-Canonical Model-Indexed Personas).**
Design only: no production code or tests change in Phase 1. Phase 3 implements against
this note. All decisions here conform to Clarifications **C1/C2/C3**
(`plan/original-requirements.md`, 2026-07-07) and the pre-locked Phase 1 decisions in
`sprint-plan.md`.

> **Pre-locked (not re-decided here):** unit on-disk shape = co-located `<name>.md`
> installed atomically with `<name>.yaml`; precedence = project `.atcr/personas`
> override > pinned community (`~/.config/atcr/personas`) > embedded built-in; length cap
> mirrors `MaxExecutorSystemPromptLen` (4096). This note documents **how to extend the
> existing chain** to honor them — it does not author a second resolver.

---

## 1. The existing resolver — signature and source order (verbatim)

**Resolver:** `internal/registry/persona.go:46`

```go
func ResolvePersona(agentName, persona string, taskMessage *string, dirs PersonaDirs) (ResolvedPersona, error)
```

- `PersonaDirs` (`persona.go:16`): `struct { Project string; Registry string }` — **Project
  takes precedence over Registry.**
- `ResolvedPersona` (`persona.go:23`): `struct { Text string; Source string }` — resolved
  prompt text + origin (a file path, `"task-message"`, or `"embedded:<name>"`).
- `ErrPersonaNotFound` (`persona.go:29`): an explicit persona ref that resolves nowhere
  fails hard; it never silently falls through to `_base`/embedded.

**Six-level chain today** (`persona.go:31-105`):

1. `taskMessage` (programmatic override; wins outright when non-nil — internal seam, not a
   CLI flag).
2. `<persona>.md` in `dirs.Project`.
3. `<persona>.md` in `dirs.Registry`.
4. `_base.md` in `dirs.Project`, then `dirs.Registry`.
5. embedded `<agentName>.md`, then embedded `_base.md` (`personas.Get` / `personas.Base`).

Names are sanitized against path traversal (`validateName`, `persona.go:111`); empty/
whitespace-only files and symlinks are treated as "not present" (`readNonEmpty`).

**Review-time call site:** `internal/fanout/review.go:999`

```go
persona, err := registry.ResolvePersona(name, ac.Persona, nil, cfg.PersonaDirs)
```

`cfg.PersonaDirs` is populated at `review.go:158-161`:

```go
PersonaDirs: registry.PersonaDirs{
    Project:  filepath.Join(root, ".atcr", "personas"),
    Registry: filepath.Join(filepath.Dir(regPath), "personas"),
},
```

where `regPath` is `DefaultRegistryPath()` = `~/.config/atcr/registry.yaml`
(`config.go:444-449`, built from `os.UserHomeDir()` + hardcoded `.config`). So the
resolver's **Registry** dir is `~/.config/atcr/personas` on every OS.

---

## 2. The extension point (C1/C2 — one chain, one unit, no second resolver)

**The chain already resolves a co-located `<name>.md` — no new format is needed.** Level 3
(`persona.go:60-72`) reads `filepath.Join(dir, persona+".md")` from `dirs.Registry`. A
community persona whose prompt is delivered as a co-located `<name>.md` in the resolver's
Registry dir is therefore **already** resolved by the existing level-3 read.

So "extending the chain for community units" is **not** new resolution branching. It is two
things, both landing on the *existing* level-3 read:

1. **Make the install path write the unit into the resolver's Registry dir** so the
   co-located `<name>.md` is where level 3 already looks (see §3 dir reconciliation).
2. **Guard the untrusted content** the level-3 read now returns (see §4 guardrails).

**No second resolver** is authored in `internal/personas`. `internal/personas` owns
*install* (fetch + atomic write of `<name>.yaml` + `<name>.md`); `internal/registry.ResolvePersona`
remains the sole *resolver*. Built-ins stay embedded (level 5) and resolve through the same
chain — the C2 "embedded subset of the unit model." Built-in `.md` reformatting is deferred
to a bounded fast-follow (out of scope this sprint).

**Binding-only personas still resolve (C1 Edge Case 1) — with an explicit precondition.**
This sprint changes **nothing** about how a binding-only persona (one that sets only
`provider`/`model` and ships no custom `<name>.md`) resolves; it only adds the custom-prompt
content at the existing level-3 read. Binding-only therefore resolves exactly as it does
today, which means it is subject to the existing resolver contract (`persona.go:76-79`):

- When the persona ref **equals the agent name** (`persona == agentName`), a binding-only
  persona with no file resolves via level 5 — embedded `<agentName>.md` (`persona.go:97`
  keys embedded lookup on `agentName`). This is the normal built-in reuse path.
- When the persona ref **differs from the agent name** (an explicit ref) and no `<persona>.md`
  file exists at level 2/3, resolution **hard-fails with `ErrPersonaNotFound`** — it does NOT
  fall through to `_base`/embedded. This is existing, intentional behavior (§1), not a
  regression this sprint introduces.

So "binding-only remains *valid*, just no longer *required*" (C1) means: a binding-only
community persona keeps working under today's rules — a differently-named binding-only ref
must still point at a resolvable `<name>.md` (built-in reformatted into a file in the bounded
fast-follow, or a project/community file), exactly as any persona ref must today. **No new
fall-through is added**, preserving §6's "no new resolution branch." Phase 3 does not need a
resolver change for this case; it only must not break it.

---

## 3. Directory reconciliation (the darwin gap — must fix in Phase 3)

**Problem.** The install dir and the resolver's Registry dir are computed by two different
functions that diverge on darwin:

| Function | Value | darwin | linux |
|----------|-------|--------|-------|
| Resolver `Registry` dir (`review.go:160`) | `filepath.Dir(DefaultRegistryPath())/personas` | `~/.config/atcr/personas` | `~/.config/atcr/personas` |
| Install dir `PersonasDir()` (`internal/personas/paths.go:19`) | `os.UserConfigDir()/atcr/personas` | `~/Library/Application Support/atcr/personas` | `~/.config/atcr/personas` |

`os.UserConfigDir()` on darwin is `~/Library/Application Support`, but
`DefaultRegistryPath()` hardcodes `.config` under `os.UserHomeDir()`. On darwin the two
**differ**, so a persona installed via `PersonasDir()` lands in a directory the resolver
never searches → it would never resolve. On Linux they coincidentally match, which is why
this has not surfaced.

**Fix (Phase 3, task 3.14).** Redefine `internal/personas.PersonasDir()` to derive from the
same source the resolver uses, so install-dir == resolve-dir on every OS:

```go
// PersonasDir must equal the resolver's Registry dir (filepath.Dir(DefaultRegistryPath())/personas).
func PersonasDir() (string, error) {
    regPath, err := registry.DefaultRegistryPath() // ~/.config/atcr/registry.yaml
    if err != nil {
        return "", fmt.Errorf("resolving registry path: %w", err)
    }
    return filepath.Join(filepath.Dir(regPath), "personas"), nil // ~/.config/atcr/personas
}
```

- **Verification (AC 01-06 Edge Case 3):** a darwin test asserts `PersonasDir()` ==
  `filepath.Dir(DefaultRegistryPath())/personas` (same directory), so a fetched persona is
  on the chain.
- **Back-compat obligation (AC1) — Phase 3 must not silently orphan darwin installs.**
  `PersonasDir()` also backs `personas install/list/remove/upgrade`. Redefining it on darwin
  moves the effective dir from `~/Library/Application Support/atcr/personas` to
  `~/.config/atcr/personas`, dropping any pre-existing install from `list`/`remove`. Phase 3
  must either migrate (one-time move/symlink old → new) or explicitly record that no
  back-compat is owed pre-public-launch. Captured as **TD-001** in `tech-debt-captured.md`.
- **Import-cycle check for Phase 3:** `internal/registry` imports `github.com/samestrin/atcr/personas`
  (the embedded-content package), **not** `internal/personas`. Having `internal/personas`
  import `internal/registry` for `DefaultRegistryPath` does not close a cycle against that
  edge. Phase 3 must still confirm no *other* `internal/registry → internal/personas` edge
  exists before wiring; if one is found, lift the shared `.config/atcr` path constant into a
  small leaf package both can import. (Flagged as the one integration risk to validate at
  implementation time.)

---

## 4. Untrusted-input guardrails (C3) on the fetched prompt

A fetched `<name>.md` becomes an LLM system prompt at review time and comes from a remote
repo, so it is untrusted. Three guardrails, all enforced **before a prompt can ship or
resolve**:

1. **Length cap.** Reject a custom prompt longer than the cap. Mirror
   `registry.MaxExecutorSystemPromptLen` (= **4096**, `internal/registry/config.go:83`).
   Rejection is a descriptive load-time error ("persona prompt exceeds maximum length"),
   never a silent truncation. Enforce at **install/load time** (in `internal/personas` on
   write, and defensively on the resolve read so a hand-dropped oversized file is also
   caught).
2. **Hard fixture gate.** A custom prompt must pass its render/category fixture before it
   ships or resolves (leverages the per-persona fixture the library already requires; the
   fixture runner is extended for community personas in Phase 6, AC 06-03). A fixture-failing
   prompt is treated as invalid — a HARD gate, consistent with C3.
3. **`{{ }}` template-metacharacter guardrail.** The standard template pipeline fills
   `{{.AgentName}}`, `{{.ScopeRule}}`, etc. A fetched body containing its own `{{ ... }}`
   directives must **not** drive template expansion or reach unintended variables. Phase 3
   decides between the two acceptable enforcements from AC 01-06 Error Scenario 3:
   - **(preferred) reject at load** if the fetched body contains `{{`/`}}` after the known
     required variables are accounted for, OR
   - **render the fetched body as literal text** (escape metacharacters) so directives never
     expand.
   A fixture asserts a `{{ }}`-bearing fetched prompt does not expand.

**Pin for reproducibility.** Fetch-and-pin (AC 01-02) freezes the resolved prompt version;
an upgrade is explicit (`atcr personas upgrade`). The guardrails run against the pinned
content.

**Transport.** `RegistryBaseURL` is HTTPS-only; `ATCR_PERSONAS_URL` may be `http` only for a
local/mock test registry.

---

## 5. Precedence + collision rule (pre-locked; verbatim for Phase 3)

**Single deterministic chain, one winner, no ambiguous double-load:**

> **project `.atcr/personas` override > pinned community (`~/.config/atcr/personas`) >
> embedded built-in**

This is exactly the existing `PersonaDirs{Project, Registry}` order plus the embedded
fallback (`persona.go` levels 2 → 3 → 5) — **no new ordering is introduced.** A name present
as built-in, installed community, and project override resolves to the project file first,
deterministically; the loop structure (`persona.go:60`) already guarantees a single winner
and cannot double-load. Community units land at the Registry (level 3) tier once §3's dir
reconciliation is in place.

---

## 6. Phase-3 exit contract (what Phase 3 consumes from this note, no rework)

- **Signature to extend, not replace:** `ResolvePersona(agentName, persona string, taskMessage *string, dirs PersonaDirs) (ResolvedPersona, error)` — unchanged. Review-time callers
  (`review.go:999`) are untouched.
- **Extension = install-side + guardrails on the existing level-3 read**, not a new
  resolution branch.
- **Dir reconciliation:** redefine `PersonasDir()` per §3; assert install-dir == resolve-dir
  on darwin.
- **Guardrails:** length cap 4096 (mirror `MaxExecutorSystemPromptLen`); hard fixture gate;
  `{{ }}` reject-or-literal — all before ship/resolve.
- **Precedence/collision:** §5, verbatim, already matches the existing chain.
- **No second resolver, no second delivery path, no divergent format** (C2).

---

*Phase 1 spike — no production code changed. Reviewed against C1/C2/C3.*
