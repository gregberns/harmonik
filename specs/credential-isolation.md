# Credential Isolation

```yaml
---
title: Credential Isolation
spec-id: credential-isolation
requirement-prefix: CI
status: draft
spec-shape: requirements-first
spec-category: foundation-cross-cutting
version: 0.1.0
spec-template-version: 1.1
owner: flywheel-author
last-updated: 2026-05-31
depends-on:
  - architecture
  - cognition-loop
  - claude-launchspec
  - claude-hook-bridge
  - process-lifecycle
  - operator-nfr
---
```


## 1. Purpose
This spec defines harmonik's **credential isolation** contract: which process MAY hold a sensitive model credential, the scrub guarantee owed at every `claude` spawn boundary, and the scoped-injection rule that delivers the credential to the one process permitted to hold it. The contract is cross-cutting — it spans daemon child-env assembly ([claude-launchspec.md §4], [claude-hook-bridge.md §4.2 CHB-006]), the cognition (Pi) process launch ([cognition-loop.md §4.1, §4.12]), and the tmux substrate handoff — and no single subsystem owns all three boundaries. This spec is the normative source for the credential-holder discipline; the sibling specs it touches carry additive notes referencing it (Appendix A).

The contract is **mechanism**, not cognition ([architecture.md §4.4]): a fixed deny-list of environment-variable keys, a deterministic scrub, and a deterministic scoped-injection builder. No model judgment is consulted to decide whether to scrub or which process may hold a credential.

## 2. Scope
### 2.1 In scope
- The **sole-holder** rule: exactly one process MAY hold a credential named in the credential env deny-list.
- The **credential env deny-list**: a normatively named, single-source-of-truth set of environment-variable keys.
- The **scrub guarantee** at the single daemon child-env assembly boundary, and the substrate handoff as an **assertion point**.
- The **scoped-injection** rule for the holder process: an explicit allow-list-based env builder, not blanket parent-environment inheritance.
- The **`supervise start` injection source**: a non-committed scoped source from which the holder process is authenticated on a fresh boot.
- The invariant that no credential **value** appears in any committed artifact or emitted event.

### 2.2 Out of scope
- Spend governance — the per-day USD / max-runs meter is owned by [cognition-loop.md §4.11 CL-090].
- A secrets-management subsystem (keychain, vault, rotation daemon, encrypted-at-rest store). The injection source is a scoped non-committed file or an operator-exported variable; this spec introduces no new infrastructure layer.
- The `.gitignore` rule and the pre-commit secret-scan **mechanism** are implementation tasks; this spec owns only the invariant that the deny-list keys' values MUST NOT appear in committed artifacts (CI-007), which is the secret scan's normative input.
- The LLM transport substrate (raw Messages API vs `pi-agent-core`) — informative per [cognition-loop.md §2.2]; this spec is normative on *who holds the credential*, not on transport.
- The forbidden-flag deny-list of [claude-hook-bridge.md §4.2 CHB-007] — that is a deny-list for CLI **flags**, a distinct concept from the credential **env** deny-list defined here.

## 3. Glossary
- **credential** — a sensitive value that authenticates against a paid model API; the governed set is the credential env deny-list (§4 CI-002).
- **credential env deny-list** — the fixed set of environment-variable keys whose presence in a process env constitutes holding a credential. (see CI-002)
- **holder process** — the single process permitted to hold a credential: the Pi cognition process per [cognition-loop.md §4.1]. (see CI-001)
- **scrub boundary** — the single daemon child-env assembly point (`ClaudeEnvVars`, [claude-hook-bridge.md §4.2 CHB-006]) at which credential env deny-list keys are removed from a constructed child env. (see CI-003)
- **assertion point** — a boundary that MUST NOT re-introduce a credential and verifies (test/debug-build assertion) that the env it carries is already scrubbed; not a second scrub site. (see CI-004)
- **scoped-injection** — construction of the holder process's env from an explicit allow-list base plus the credential, rather than a blanket `os.Environ()` inheritance or union. (see CI-005)

## 4. Normative requirements

### 4.1 Holder contract
- **CI-001 — Sole holder.** Exactly ONE process — the Pi cognition process ([cognition-loop.md §4.1 CL-001], composed at [cognition-loop.md §4.12 CL-100]) — MAY hold a credential named in the credential env deny-list (CI-002). The daemon process and every daemon-spawned `claude` implementer/reviewer child MUST NOT receive any credential env deny-list key. This is an invariant (CI-INV-001).
- **CI-002 — Credential env deny-list (single source of truth).** The credential env deny-list is the set `{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}`: two exact keys and one prefix glob (`CLAUDE_CODE_OAUTH*` matches any key beginning `CLAUDE_CODE_OAUTH`). The set is defined ONCE (a single code-level constant) and referenced by the scrub (CI-003), the scoped-injection builder (CI-005), and the committed-artifact invariant (CI-007). The deny-list is distinct from, and MUST NOT be conflated with, the forbidden-flag deny-list of [claude-hook-bridge.md §4.2 CHB-007].

### 4.2 Scrub guarantee
- **CI-003 — Scrub at the single env-assembly boundary.** The daemon's child-env assembly — `ClaudeEnvVars` per [claude-hook-bridge.md §4.2 CHB-006], the single point at which a daemon-spawned `claude` child's environment is constructed — MUST remove every credential env deny-list (CI-002) key from the constructed child env. The scrub is applied at this single boundary, symmetric with the existing `HARMONIK_SECRET_*` prefix strip; it MUST NOT be implemented as a scatter of per-call-site `env -u` wrappers. A key reaching the daemon's base environment by any path (operator export, inherited `os.Environ()`, daemon `Config.HandlerEnv`) MUST be removed at this boundary before the child env is materialized.
- **CI-004 — Substrate handoff is an assertion point.** The tmux substrate handoff that carries the assembled env to the spawned `claude` MUST NOT re-introduce any credential env deny-list key and MUST treat the carried env as already scrubbed. The handoff is an assertion point — a test or debug-build assertion that the env contains no deny-list key — NOT a second scrub site. Production behavior carries the pre-scrubbed env as-is.
- **CI-004a — Scrub invariant locked by a named regression test.** The invariant "no daemon-spawned `claude` ever receives a credential env deny-list key" (CI-INV-002) MUST be locked by a named regression test. The test asserts absence-by-key against the constructed child env; it MUST NOT print, log, or otherwise emit any credential value.

### 4.3 Scoped injection
- **CI-005 — Scoped injection for the holder process.** The credential MUST reach the holder process (Pi) via an explicit scoped env builder constructed from an allow-list base, with the credential added explicitly — NOT via a blanket `os.Environ()` passthrough or a union of `os.Environ()` with an explicit env. Every launch path that starts or attaches the holder process MUST build its env from a scoped base rather than inherit the full parent environment. (At v0.1 the relevant paths are the supervise `_shim` exec and the supervisor's process exec; see CI-005a for the attach carve-out.)
- **CI-005a — Attach-path carve-out.** The operator `attach` path delivers the operator's own credential into the operator's own attached tmux client; the operator legitimately holds the credential. The attach path is OUT of the scrub scope of CI-003 and the scoped-injection scope of CI-005. The scrub and scoped-injection contract governs only (a) the daemon→`claude` child path (CI-003) and (b) the holder-process injection path (CI-005).

### 4.4 Injection source and committed-artifact discipline
- **CI-006 — `supervise start` injection source.** `harmonik supervise start` MUST inject the credential into the holder process from a non-committed scoped source so that a fresh holder-process boot authenticates without a manual operator export. The source precedence is: an explicit operator env export, then a gitignored credential file (a repo-root `.env` read ONLY by `supervise start`), then a fail-closed error when no source resolves. The credential file MUST NOT be read by the daemon and MUST NOT be unioned into any child env. The source is registered as an operator-configurable knob per [operator-nfr.md §4.1 ON-004].
- **CI-007 — No credential value in committed artifacts or events.** The credential env deny-list keys' VALUES MUST NOT appear in any committed artifact, spec example, or emitted event payload. The scrub (CI-003), the named regression test (CI-004a), and any pre-commit secret scan reference the deny-list keys by NAME only and MUST NOT log or emit a matched value. The credential file of CI-006 MUST be excluded from version control.

## 5. Invariants
- **CI-INV-001 — Single holder.** No harmonik process other than the Pi cognition process holds a credential env deny-list key, in any process state, across any boot path.
- **CI-INV-002 — No deny-list key in any daemon-spawned child env.** Every daemon-spawned `claude` implementer/reviewer child env is free of every credential env deny-list key. Verified by the CI-004a regression test.
- **CI-INV-003 — Mechanism-tagged.** The deny-list is a fixed set; the scrub and the scoped-injection builder are deterministic; no model judgment is consulted. Per [architecture.md §4.4].

## 6. Conformance
**Core v0.1.** Conformant when CI-001..CI-007 and the three invariants hold. Acceptance scenarios:
1. The CI-004a regression test passes: a daemon-spawned `claude`'s constructed child env contains no credential env deny-list key, while a non-deny-list var threaded through `BaseEnv` survives — proving the scrub is keyed to the deny-list, not a blanket strip.
2. A fresh `supervise start` with no operator export but a gitignored credential file authenticates the holder process from that file (CI-006); the daemon process env, inspected at the same boot, contains no deny-list key (CI-001/CI-INV-001).
3. The substrate handoff assertion (CI-004) fires (in a test/debug build) if a deny-list key is injected into the carried env after assembly, and is a no-op in production.

## 7. Open questions
- **OQ-CI-001.** Whether the gitignored credential file path is fixed at repo-root `.env` or operator-configurable per ON-004 precedence. Working: repo-root `.env` at v0.1 (matches operator muscle memory and the incident vector, now sanctioned + gitignored); a configurable path is a later refinement.

## 8. Cross-spec coordination
- **[claude-launchspec.md §4].** The env-assembly step records the credential env deny-list scrub as part of CHB-006 assembly (Appendix A item a); the launch-spec note distinguishes the credential env deny-list (this spec, CI-002) from the forbidden-flag deny-list (CHB-007).
- **[operator-nfr.md §4.1 ON-004, §4.3].** The `supervise start` injection source (CI-006) is an ON-004 config-inventory entry; §4.3 records that `supervise start` injects the credential from the scoped source (Appendix A item b).
- **[cognition-loop.md §4.1 CL-001, §4.12 CL-100].** The Pi cognition process is the sole holder (CI-001); its composition root is where the scoped-injection env is built.

## Appendix A: Cross-spec amendments captured here
This section lists the additive notes this spec asks its sibling specs to carry. The amendments are designed alongside this spec; the sibling specs should reference `specs/credential-isolation.md` as the normative source.

### A.1 `specs/claude-launchspec.md`
- **§4 `baseEnv` field-table note + assembly-sequence step-5 note:** env assembly removes the credential env deny-list keys (this spec, CI-002/CI-003) from the constructed child env, symmetric with the existing `HARMONIK_SECRET_*` strip; the substrate handoff is an assertion point (CI-004), not a second scrub site. Distinct from the CHB-007 forbidden-flag deny-list.
- **§6 Cross-references table:** add a "Credential env deny-list / scrub" row pointing to this spec (CI-002/CI-003).

### A.2 `specs/operator-nfr.md`
- **§4.1 ON-004 inventory entry:** the `supervise start` credential injection source (CI-006), with precedence (operator export > gitignored `.env` > fail-closed), default, and change-takes-effect.
- **§4.3 note:** `supervise start` injects the credential from a non-committed scoped source per CI-006.

## 9. Revision history
| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-05-31 | 0.1.0 | agent (kerf `credfence` work) | Initial draft. Defines CI-001..CI-007 + three invariants over the 2026-05-30 credential-leak incident and the `credfence` decomposition. Establishes the credential env deny-list as a single-source-of-truth set, the scrub at the `ClaudeEnvVars`/CHB-006 boundary, the scoped-injection rule for the Pi holder process, and the `supervise start` injection source. |
