I'll review this diff against the sprint plan scope. The changes implement Epic 1.3: Project-Level Registry Overlay, including the `.atcr/registry.yaml` loader, trust gate with `atcr trust`, merged-view validation, provenance in `atcr doctor`, and documentation updates.

Let me analyze the code for issues within scope:

HIGH|internal/registry/trust.go:178|Raw emoji U+26A0 in banner can mojibake on Windows|Use ASCII "WARNING:" or gate behind terminal check|maintainability|10|fmt.Fprintf(&b, "⚠ Using project-defined provider(s)|bruce

MEDIUM|internal/registry/trust.go:90|Trust summary under-reports mixed runs|Report "N new entries written" or both counts|correctness|10|_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %d trust entr%s|bruce

MEDIUM|internal/registry/gate.go:39|ResolveGateThreshold reads fail_on from unmerged user registry only|Resolve from merged view or document restriction|correctness|20|fail_on only from the unmerged user registry|bruce

MEDIUM|internal/registry/graph.go:54|Cross-tier cycle attributed to path[0] not project tier|Attribute to project node when cycle spans tiers|correctness|30|cycle attributed to path[0] (alphabetically-first DFS entry)|bruce

LOW|cmd/atcr/trust.go:90|Trust summary count only shows newly written entries|Clarify as "N new entries written" in output|maintainability|5|fmt.Fprintf(cmd.OutOrStdout(), "wrote %d trust entr%s|bruce

LOW|internal/registry/overlay.go:178|ProjectProviderBanner uses raw emoji without terminal check|Gate emoji behind terminal capability detection|maintainability|10|fmt.Fprintf(&b, "⚠ Using project-defined provider(s)|bruce

Note: The issues from `.planning/technical-debt/README.md` are already documented as technical debt items, so I'm reporting them as findings in the actual code locations. The `gate.go:39` issue appears to be in scope based on the sprint plan's mention of "merged-view validation" and the technical debt note about `fail_on` resolution inconsistency.