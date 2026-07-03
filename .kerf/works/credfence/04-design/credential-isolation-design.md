# Change Design — `specs/credential-isolation.md` (NEW spec)

> Pass 4 (`change-design`) of the `credfence` spec work. Covers components C1 (holder contract + deny-list), C2 (scrub-at-spawn-boundary + regression test), C3 (Pi-scoped injection + `supervise start` source). The NORMATIVE output is `05-spec-drafts/credential-isolation.md`; this file documents intent. Grounded in `03-research/credential-isolation/findings.md`, `02-components.md §3`, and the live tree (anchors re-verified 2026-05-31).

## Current state

There is **no** `specs/credential-isolation.md`. Credential handling is implicit:

- The daemon's child env is assembled at one boundary — `internal/handler/claudehandler_chb006_024.go:200` `ClaudeEnvVars(cfg)` — which starts from `cfg.BaseEnv`, strips `HARMONIK_SECRET_*` (prefix match, `strings.HasPrefix`), appends CHB-006 vars, returns the `KEY=VALUE` slice (research F1). `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH*` are NOT stripped — they pass straight through into every daemon-spawned `claude`.
- `BaseEnv` provenance: `claudelaunchspec.go:336` `BaseEnv: rc.baseEnv` ← daemon `Config.HandlerEnv` ← daemon process env, seeded from `os.Environ()` at `cmd/harmonik/run.go:404` self-wrap exec. A key the operator exports reaches `BaseEnv` and flows through (research F1).
- The tmux substrate handoff `internal/daemon/tmuxsubstrate.go:179` `Env: in.Env` carries the pre-built slice as-is — no per-var `-e` injection (research F1; corrects the assessment's stale `osadapter.go:497` anchor).
- Pi receives the key via THREE `os.Environ()` passthroughs (research F2): `cmd/harmonik/supervise/shim.go:103` `syscall.Exec(resolved, command, os.Environ())`; `cmd/harmonik/supervise/attach.go:58` `syscall.Exec(tmuxBin, argv, os.Environ())`; `internal/supervise/supervisor.go:336` `cmd.Env = append(os.Environ(), s.spec.Env...)`. The supervisor *unions* `spec.Env` with the full parent env — so an explicit `spec.Env` ADDS, it does not replace.
- `supervise start` injects NO credential; a fresh Pi boot silently fails auth unless the operator hand-exported the key (research F5; assessment §"Genuinely lost" #4).

No spec states which process may hold the key, requires a scrub at the spawn boundary, or requires scoped injection.

## Target state

A NEW thin cross-cutting invariant spec `specs/credential-isolation.md`, mirroring the `handler-pause.md` / `claude-launchspec.md` cross-subsystem-invariant pattern (research F4), with an Appendix-A-style amendment list for the siblings it touches. Requirement prefix `CI`. Sections:

**§1 Purpose / §2 Scope.** Owns the credential-holder discipline spanning daemon env assembly, Pi process launch, and the tmux substrate handoff — none of which is the natural single owner. Out of scope: a vault/keychain subsystem (problem-space §3); spend governance (cognition-loop.md CL-090); the `.gitignore`/secret-scan *mechanism* (an implementation task, though the "no deny-list key in committed artifacts" invariant lives here).

**§3 Glossary.** `credential deny-list`, `holder process`, `scrub boundary`, `assertion point`, `scoped-injection`.

**§4 Normative requirements.** Target requirement IDs:

- **CI-001 — Sole holder.** Exactly ONE process — the Pi cognition process (`cognition-loop.md` CL-001/CL-100) — MAY hold a credential in the deny-list. The daemon process and every daemon-spawned `claude` implementer/reviewer child MUST NOT receive any deny-list key. Stated as an invariant.
- **CI-002 — Credential deny-list (single source of truth).** Names the set normatively: `{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}` — two exact names + one prefix glob (`HasPrefix("CLAUDE_CODE_OAUTH")`), matching the existing `HARMONIK_SECRET_*` prefix-strip shape (research F3). Named distinctly from the existing CHB-007 *forbidden-flag* deny-list to avoid conflation (launchspec research F1/R1) — this is the **credential env deny-list**. One code-level constant; referenced by CI-003 (scrub), CI-005 (Pi builder), and the secret scan (C7).
- **CI-003 — Scrub at the single env-assembly boundary.** The daemon's child-env assembly (`ClaudeEnvVars`, CHB-006) MUST remove every CI-002 deny-list key from the constructed child env, symmetric with the existing `HARMONIK_SECRET_*` strip. The scrub is at the SINGLE assembly point — NOT a scatter of `env -u` wrappers at call sites.
- **CI-004 — Substrate handoff is an assertion point.** The tmux substrate handoff (`tmuxsubstrate.go:179` `Env: in.Env`) MUST NOT re-introduce a deny-list key and is an ASSERTION point (a debug-build / test assertion that the carried env is already scrubbed), NOT a second scrub site (research F1/F4).
- **CI-004a — Scrub invariant + named regression test.** Invariant: "no daemon-spawned `claude` ever receives a CI-002 deny-list key." Locked by a named regression test (hk-4g32m) that asserts absence-by-key and MUST NOT print any value (research R4).
- **CI-005 — Pi-scoped injection (not blanket inheritance).** The credential MUST reach Pi via an EXPLICIT Pi-scoped env builder constructed from an allow-list base, NOT a blanket `os.Environ()` passthrough/union. Because `supervisor.go:336` UNIONS `spec.Env` with `os.Environ()` (research F2/R1), the contract enumerates all THREE sites — `shim.go`, the supervisor, and (conditionally) `attach.go` — and requires each to build from a scoped base rather than inherit the full parent env.
- **CI-005a — Attach-path carve-out.** `attach.go:58` carries the key into the OPERATOR's own attached tmux client, and the operator already legitimately holds the key (research R2). CI-005a states attach is OUT of the scrub scope — the scrub contract covers the daemon→`claude` path (CI-003) and the Pi-injection path (CI-005); attach is the operator's own shell and is exempt. (This resolves the carried-to-design attach-scope open question.)
- **CI-006 — `supervise start` injection source.** `supervise start` MUST inject the credential from a NON-COMMITTED scoped source so a fresh Pi boot authenticates without a manual `export` (closing HANDOFF blocker #1). The source is a gitignored `.env` read ONLY by `supervise start` (research F5; OQ-5 lean), never read by the daemon and never unioned into a child env. Precedence: explicit env export > gitignored `.env` > error. This source is registered as an `operator-nfr.md` ON-004 config-inventory entry (operator-nfr design).
- **CI-007 — No credential value in committed artifacts or event payloads.** The deny-list keys' VALUES MUST NOT appear in any committed artifact, spec example, or emitted event; the scrub/scan reference keys by name only and MUST NOT log the matched value (problem-space §4). (This is the credential-isolation-side anchor for the C7 `.gitignore`/secret-scan implementation task; the scan's input is the CI-002 deny-list.)

**§5 Invariants.** CI-INV-001 (single holder); CI-INV-002 (no deny-list key in any daemon-spawned child env); CI-INV-003 (mechanism-tagged: fixed deny-list, deterministic scrub, no model judgment — architecture.md §4.4).

**§6 Conformance.** Conformant when CI-001..CI-007 + invariants hold; acceptance scenario = the hk-4g32m regression test plus a Pi-boot-from-scoped-source check.

**Appendix A — cross-spec amendments this spec asks siblings to carry:** (a) `claude-launchspec.md` §4 baseEnv-row note (the env-assembly scrub); (b) `operator-nfr.md` §4.3 + ON-004 entries (injection source). These are designed in the launchspec and operator-nfr design files; Appendix A lists them so the new spec is self-describing.

## Rationale

- **Why a new spec (OQ-2 resolved).** The obligation spans daemon + Pi + substrate; no existing spec owns all three. A thin invariant spec with an Appendix-A amendment list is idiomatic in this corpus (research F4: `handler-pause.md`, `claude-launchspec.md` follow the same shape). Folding into `cognition-loop.md` would bury a cross-cutting invariant inside the cognition layer and deny the scrub regression test (hk-4g32m) a clear normative anchor.
- **Why scrub at `ClaudeEnvVars` (CI-003), not per-call-site.** It is the single env-assembly point; the `HARMONIK_SECRET_*` strip already lives there, so the scrub is a one-branch generalization that catches every daemon-spawned `claude` regardless of how the key entered `BaseEnv` (research F1). A scatter of `env -u` wrappers is fragile and violates the single-boundary constraint (problem-space §4).
- **Why three sites for CI-005.** Research F2/R1 found the supervisor UNIONS `spec.Env` with `os.Environ()`; scoped injection therefore requires replacing the BASE, not just setting `spec.Env`. The problem-space's two-site framing undercounts — the design enumerates shim + supervisor + (carve-out) attach.
- **Why the sanctioned-`.env` source (CI-006).** No secrets subsystem exists to reuse, and inventing one violates problem-space §3. A gitignored `.env` read only by `supervise start` turns the incident's exact vector into the sanctioned path (operator muscle memory works; the file is uncommittable once C7's `.gitignore` rule lands) and slots into the existing ON-004 config-inventory home (research F5).

## Requirements traceability

| 02-components requirement | Goal (01 §2) | Target requirement |
|---|---|---|
| C1 holder contract + deny-list (SC1) | G1 | CI-001, CI-002 |
| C2 scrub guarantee + regression test (SC2) | G2 | CI-003, CI-004, CI-004a |
| C3 Pi-scoped injection (SC3) | G3 | CI-005, CI-005a |
| C3 supervise-start source (SC3) | G3 | CI-006 |
| no-value-in-committed-artifacts (constraint, problem-space §4) | — | CI-007 |

Every C1/C2/C3 requirement has a target; no target lacks a backing requirement. The §4.3 / ON-004 / launchspec-note pieces of C2/C3 are designed in the sibling design files (cross-referenced by Appendix A), not duplicated here.
