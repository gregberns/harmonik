# Change Design — `specs/claude-launchspec.md` (C2 env-assembly scrub note)

> Pass 4 (`change-design`) of the `credfence` spec work. Covers the additive note recording the credential deny-list scrub as part of CHB-006 env assembly (component C2). NORMATIVE output is `05-spec-drafts/claude-launchspec.md`. Grounded in `03-research/launchspec-operator-nfr/findings.md §F1`, `02-components.md §2`, and the live spec (`specs/claude-launchspec.md:54, 108, 152, 156, 276`, re-verified 2026-05-31).

## Current state

- **§4 field table, `baseEnv` row (line 108):** "Environment inherited from daemon `Config.HandlerEnv`. MUST already include `HARMONIK_PROJECT_HASH` per PL-006a. CHB-006 vars are appended (or overwrite) by `ClaudeEnvVars`." — names the assembly step and `ClaudeEnvVars` but says nothing about a credential scrub.
- **§4 assembly sequence, step 5 (line 152):** "**Env assembly** — per [claude-hook-bridge.md §4.2 CHB-006]: call `ClaudeEnvVars` with a fully-populated `ClaudeEnvConfig` to derive the subprocess environment slice." — the exact step where the scrub happens in code.
- **§4 step 7 / §2 scope (lines 54, 156, 276):** the spec ALREADY references a "**Forbidden-flag deny-list**" (`claude-hook-bridge.md §4.2 CHB-007`) — a deny-list for CLI FLAGS, a DIFFERENT concept from the new credential ENV deny-list (research F1/R1 — conflation risk).

## Target state

A single ADDITIVE note; no existing requirement changes. Two touch points:

- **§4 `baseEnv` row note (line 108):** append a sentence: "Env assembly additionally removes the **credential env deny-list** keys (`{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}`, per `credential-isolation.md` CI-002/CI-003) from the constructed child env, symmetric with the existing `HARMONIK_SECRET_*` strip. This is distinct from the forbidden-flag deny-list of CHB-007."
- **§4 assembly sequence, step 5 note (line 152):** append: "`ClaudeEnvVars` MUST strip every credential-env-deny-list key per `credential-isolation.md` CI-003 (the daemon-side scrub boundary); the substrate handoff is an assertion point per CI-004, not a second scrub site."
- **§2 scope / §5 references table (line 276 area):** add a row pointing the "Credential env deny-list / scrub" concept to `credential-isolation.md` CI-002/CI-003, parallel to the existing CHB-007 forbidden-flag-deny-list row — keeping the two deny-lists visibly distinct.
- **Revision history:** add a 0.x row noting the credfence credential-scrub note.

## Rationale

- The scrub belongs at `ClaudeEnvVars` (the single env-assembly boundary) and `claude-launchspec.md` is the spec that owns the launch-spec env-assembly contract; recording the scrub here keeps the launch-spec consistent with the new `credential-isolation.md` contract (research F1). The change is purely additive — no requirement is reversed; the existing `HARMONIK_SECRET_*` strip proves the boundary already filters env (credential-isolation research R3).
- **Naming discipline (research R1):** `claude-launchspec.md` already has a "deny-list" (CHB-007 forbidden FLAGS). The note explicitly names the new one the "credential env deny-list" and cross-references `credential-isolation.md` so a reader cannot conflate scrubbed-ENV-keys with forbidden-CLI-flags.
- The note is the launchspec-side realization of `credential-isolation.md` Appendix A item (a).

## Requirements traceability

| 02-components requirement | Goal (01 §2) | Target |
|---|---|---|
| C2 scrub recorded in launch-spec env assembly (SC2) | G2 | §4 baseEnv-row note + step-5 note + references-table row pointing to CI-002/CI-003 |

Single requirement, single target, additive only. The normative scrub obligation itself lives in `credential-isolation.md` CI-003; this spec merely keeps the launch-spec contract consistent with it. No contradiction with any other area.
