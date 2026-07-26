# 06 — Integration

> Pass 6. Cross-reference + terminology + coherence check of the HN-025/HN-026 amendment against
> the whole `specs/` corpus. Back-fill; the surface is one additive amendment, so the check is small
> but was run against the full tree, not just the modified file.

## Cross-Reference Checks Performed

| Reference in the draft | Target | Status |
|------------------------|--------|--------|
| HN-026 cites **PI-015** ("harness runs unsandboxed") | `specs/pi-harness.md:64` — "The harness MUST NOT pass a `--sandbox` flag (Pi is unsandboxed)" | ✅ exists, matches cited meaning |
| HN-026 INFORMATIVE defers to **ON-024** | `specs/operator-nfr.md:535` — "ON-024 — Command-execution sandbox invariant" (§4.7) | ✅ exists; harmonik-level sandbox invariant the parallel workstream engages |
| Amendment declared additive per **HN-023** | `specs/harness-contract.md:435` — "HN-023 — All harness-contract additions are additive" | ✅ exists; amendment adds only new IDs |
| New IDs **HN-025/HN-026** | highest existing HN id = HN-024 | ✅ next free; no collision |
| Insertion point **§4.10** (after §4.9, before §5) | §4.9 at line 433, §5 at line 457 | ✅ clean slot |
| Prefix **HN** registry entry | `specs/_registry.yaml:32` — `HN: {spec-id: harness-contract, status: draft}` | ✅ reserved; only ID confirmation at finalize |

Reverse direction: nothing in the corpus links to removed content. The amendment removes NO spec
text (the removed *code* — `CodexRequireIsolationBoundary`, the `.git` writable-roots carveout — was
never spec'd, per Pass-3 F1/F2), so there are no orphaned inbound links.

## Contradictions Found

None. No prior requirement pinned the isolation fence or the codex sandbox mode (Pass-3 F1/F2), so
HN-025/HN-026 contradict nothing. Nearest neighbors are consistent:
- **HN-022** (codex billing) — orthogonal (billing, not sandbox); untouched; same launch-spec
  builder, no interaction.
- **PI-015** — HN-026 makes codex *uniform* with it, not exceptional.
- **ON-024** — HN-026's INFORMATIVE note frames ON-024's uniform sandbox as the superseding
  parallel workstream, so the two coexist (per D3 "security re-homed").

## Consistency Issues Found

None. "danger-full-access" / "workspace-write" / "writable_roots" / "`-c` override" appear nowhere
else in `specs/`, so there is no competing usage to reconcile. "isolation boundary" names only the
removed rule (no surviving normative use). "unsandboxed on the host" matches PI-015's framing.

## Cross-Reference Validity

Every reference in the draft resolves (table above) and the linked content is accurate in both
directions. No `[text](file.md)`-style link was introduced by the draft (it uses ID citations),
and none was orphaned.

## Changelog Verification

`05-changelog.md` lists exactly the two drafted requirements (HN-025, HN-026), the additive
declaration, and the deliberate non-drafts (no new spec file; deferred test beads). It matches
`05-spec-drafts/harness-contract-amendment.md` one-to-one. ✅

## Final Assessment

**Coherent.** The amendment is purely additive, contradicts nothing, all cross-references resolve
in both directions, terminology is consistent, and the changelog matches the drafts. The one
non-spec risk (the exec-facet dissolution is reasoned but not yet live-proven) is carried as the
go/no-go gate in the Tasks pass, not a spec-coherence issue.
