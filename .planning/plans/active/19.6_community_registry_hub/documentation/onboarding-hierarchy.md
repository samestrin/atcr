# Onboarding Hierarchy and Discover-by-Model Flow [IMPORTANT]

## Overview

AC5 rewrites the first-run documentation so new users are guided through the monetizing, flat-rate path first, while frontier-provider personas are explicitly positioned as opt-in "bring your own key" choices. The hierarchy is not a code change to the CLI (except documenting existing flags); it is a content change to `README.md` and `docs/personas-install.md` so the funnel matches the maintainer's referral economics and the user's likely cost curve.

## Onboarding Hierarchy

Present the options to a new user in this order, with the caveats noted:

1. **Primary path: `atcr quickstart` with Synthetic**
   - Already implemented: `atcr quickstart` scaffolds `.atcr/`, writes a `synthetic` provider registry, sets `LLM_SYNTHETIC_API_KEY`, shows the signup link, and binds one synthetic agent per persona.
   - Position this as the one-command default: "run this first."
   - Reference `README.md:## Quickstart` and `docs/personas-install.md`.
   > Source: original-requirements.md (Context / Proposed Solution #4), plan.md (Theme 7)

2. **Secondary flat-rate option: DashScope / Alibaba**
   - Document as a flat-rate alternative the user can switch to after trying Synthetic.
   - No `quickstart` wiring this epic (explicitly out of scope); provide a manual registry snippet and a link to DashScope docs.
   > Source: original-requirements.md (Out of Scope), plan.md (Theme 7)

3. **Explore-only: Chutes → Featherless**
   - Mention Chutes first, then Featherless, as places to access "more models" but with caveats: slower inference, tighter context windows, concurrency limits.
   - Language: "explore, not default." Do not place these above Synthetic in the funnel.
   > Source: original-requirements.md (Proposed Solution #4), plan.md (Theme 7)

4. **Advanced aggregation: LiteLLM**
   - Note LiteLLM only as an OpenAI-compatible proxy for users who want to aggregate several providers behind a single endpoint.
   - Keep it in an "Advanced" section; it is not a first-run recommendation.
   > Source: original-requirements.md (Proposed Solution #4), plan.md (Theme 7)

5. **Opt-in frontier / majors personas**
   - Claude, GPT, Gemini, and other frontier-tuned personas are documented as "bring your own key" options.
   - They must not appear in the default `quickstart` funnel; they are discovered and installed deliberately by users who already have an API key for that provider/model.
   > Source: original-requirements.md (AC3/AC5), plan.md (Theme 4 / Theme 7)

## Discover-and-Install-by-Model Flow

Once AC2 lands, the docs should describe this end-to-end flow:

```bash
# Discover a persona tuned for the model you already have
atcr personas search deepseek
# or filter explicitly
atcr personas search --provider deepseek --model deepseek-chat

# Install the discovered persona
atcr personas install community/deepseek

# Confirm it is installed and pinned
atcr personas list

# Run its fixture to verify it matches the model-in-metadata convention
atcr personas test community/deepseek
```

Key doc points:

- Search now matches structured `provider`/`model` metadata in `index.json`, not just free-text name/description.
- A persona's YAML carries the bound `provider`/`model`; the index entry exposes the same values so search can surface it.
- Install writes the persona to `~/.config/atcr/personas/` and pins the version from the YAML's `version` field.
- `upgrade` advances the pin when the remote `index.json` advertises a newer version.

## Files to Update

- `README.md` — rewrite the `## Quickstart` section to lead with `atcr quickstart` (Synthetic) and summarize the hierarchy.
- `docs/personas-install.md` — add the discover-by-model examples and position frontier personas as opt-in.
- `docs/personas-authoring.md` — ensure the contribution checklist notes that new personas must use human names and bind a model in structured metadata (AC7/AC8).

## Related Documentation

- [../plan.md](../plan.md) — AC5 planning context
- [Installing Community Personas](../../../../../docs/personas-install.md)
- [Project README](../../../../../README.md)
- [Authoring a Persona](../../../../../docs/personas-authoring.md)
- [CLI Flag Wiring for Model-Aware Search](cli-search-flags.md)
