# HC Cluster — Bootstrap Subset (`.41` Pass 2 enumeration)

**Date:** 2026-05-05
**Cluster:** C — Handler interface + twin
**Epic:** `hk-8i31` (80 child beads verified — `.1` … `.81` with `.54` a yaml load gap)
**User-resolved questions applied:** Q1 INCLUDE basic claude-twin, Q2 EXCLUDE Pi handler, Q4 INCLUDE basic S07 harness.

## 1. Counts

| | Count | % |
|---|---|---|
| INCLUDE | 47 | 59% |
| EXCLUDE | 33 | 41% |
| **Total** | **80** | 100% |

Of the 47 INCLUDE, **11 are explicitly twin-related**. **0 Pi-handler-specific beads exist in HC** — HC defines the generic handler-interface; Pi-specific consumer logic lives in PL/AR (orchestrator-agent boundary).

## 2. INCLUDE — by HC §-section

Twin-related beads flagged **[TWIN]**.

- **§6.1 schemas (6):** `.71`–`.76` (`hc-schema.{handler, session, adapter, launch-spec, session-id}` + `hc-error.taxonomy`). Boundary contract; everything consumes one of these. LaunchSpec carve-out under Q-B.
- **§4.1–§4.3 wire + handler/session (10):** `.1` (`hc-001` Handler), `.2` (Session), `.3` (config-level real-vs-twin select — Q1 anchor), `.4` (idempotent Launch — restart-no-state-loss), `.5` (stdin-JSON delivery), `.6` (LaunchSpec conformance), `.7` (NDJSON wire coalesce — end-to-end blocker), `.8` (`outcome_emitted` delivery), `.10` (`handler_capabilities` first-message), `.11` (`session_log_location` early-emit).
- **§4.3 concurrency (2):** `.12` (one-watcher-per-session), `.14` (S04 owns adapter).
- **§4.4 / §4.5 ctx + sentinels (4):** `.21` (ctx-first), `.24` (5+2 sentinel set), `.25` (ErrProtocolMismatch), `.26` (ErrSkillProvisioningFailed).
- **§4.6 crash + dead-letter (2):** `.28` (crash → typed `agent_failed`), `.34` (dead-letter routing — prevents bus-full deadlock).
- **§4.8 twin parity (4):** `.42`–`.45` **[TWIN]** (twin = same iface/wire/tags; drift-detection clause scoped to S07 — rule itself is one line).
- **§4.9 ready ordering (3):** `.46` (`agent_ready`), `.47` **[TWIN]** (twin emits identically), `.48` (DetectReady).
- **§4.10 trust posture (5):** `.49` (repo-relative path), `.50` (commit-hash), `.51` (subprocess as direct child), `.52` (fail-fast on orphan workspace), `.53` **[TWIN]**.
- **§4.11 minimal skill provisioning (3):** `.55` (provisioning surface — empty `required_skills` still needs contract), `.58` (`skills_provisioned` event), `.59` **[TWIN]** (wire-only twin parity).
- **§5 sensors (4):** `.64` (one-watcher), `.65` **[TWIN]** (twin-indistinguishable), `.67` (ready-before-dispatch), `.69` (exactly-one-terminal).
- **§6 test infra (2):** `.77` **[TWIN]** (twin binary — Q1+Q4 anchor), `.78` (wire fixture).

## 3. EXCLUDE categories

- **Pi handler:** 0 beads (HC has no Pi-specific beads; Q2 vacuous).
- **Silent-hang FSM:** `.31`/`.32`/`.33` + `.79` harness.
- **Rate-limit + account rotation:** `.30`, `.16`.
- **Advanced skill injection:** `.56` (fail-launch unresolvable), `.57` (retry-with-backoff), `.60` (LaunchSpec-only reads).
- **Watcher panic recovery + orphan-reconnect:** `.13`, `.20`.
- **Redaction + secrets registry:** `.35`–`.41` + `.66` sensor + `.81` harness.
- **Crash-recovery harness + post-outcome window:** `.9`, `.29`, `.80`.
- **Foundation declarative rules** (compile-absorbed): `.17`/`.18`/`.19`/`.22`/`.23`/`.27`.
- **Execution-shape evolution metaclaims:** `.61`/`.62`/`.63`.
- **Trust audit + sole-publisher sensors:** `.68`, `.70`.

## 4. Cross-cluster edges OUT (23 total, 4 clusters)

Verified via `br dep list` per INCLUDE bead:
- **EM (`hk-b3f`) — 11:** LaunchSpec → Workflow/RunID/bead-tied-runs (`b3f.{14,66,72}`); `Session.Wait` + `outcome_emitted` → Outcome record (`b3f.{5,79}`); `hc-error.taxonomy` (and consumers `.25`/`.28`) → EM failure-class taxonomy (`b3f.86`); `hc-003` → DOT validator (`b3f.53`).
- **EV (`hk-hqwn`) — 4:** envelope (`hqwn.1`); subscription contract (`hqwn.12`) cited twice (watcher + dead-letter); async consumer class (`hqwn.14`).
- **AR (`hk-zs0`) — 4:** agent_type 4-surfaces (`zs0.28`) cited by `hc-schema.handler` and `hc-schema.launch-spec`; agent_type regex (`zs0.54`); mech/cog tag (`zs0.7`).
- **PL (`hk-8mup`) — 4:** one-daemon (`8mup.2`) cited by `hc-007`/`hc-044`/`hc-044a`; daemon concurrency ceiling (`8mup.25`).
- **WM, BI, CP, RC, ON: 0 outgoing.** WM `workspace_path` cite is unresolved forward-deferred per pilot §5.5.

The 26 EV co-owned event-row edges (per pilot §5.3) are NOT counted here — they will materialize as cross-cluster edges OUT once EV's bootstrap subset finalizes its row-bead INCLUDE list. Conservative projection: **23 + ~9–13 = ~32–36 outgoing total.**

## 5. Cross-cluster edges IN (76 total, 6 clusters)

Verified via full corpus dep walk:
- **EV — 27:** §8.3 row family + select §8.1/.2/.4/.7/.8 rows. Heavy concentration on `.75 hc-schema.session-id` (14 incoming alone — every event-row payload references the type) and `.76 hc-error.taxonomy`.
- **WM — 13:** lease-lock + session-log dir + Workspace record + merge-back + conflict-resolver.
- **CP — 12:** required_skills declarations + hook-failures + policy-cost ceiling + budget-accrual.
- **PL — 9:** "subprocesses are children" (`8mup.24`) hits HC handler/watcher/crash-route; socket wire format; silent-hang delegation.
- **RC — 8:** investigator-as-handler, verdict via outcome envelope, snapshot-token-bound inputs.
- **ON — 7:** prompt-injection, sandbox, commit-hash, secrets/skill injection, runners-wait, drain-forced silent-hang.

**Implication:** `.75` + `.76` are highest-fanin INCLUDE beads. Suggested order inside cluster: schemas (`.71`–`.76`) → wire (`.7`) → handler/session/adapter → twin → invariants → test infra.

## 6. Open questions / ambiguities

**Q-A. Minimum viable claude-twin?** Per the twin slice (10 beads), v0 is a static binary that (i) opens daemon socket per `hc-007`; (ii) emits the 5-message handshake `handler_capabilities → session_log_location → skills_provisioned → agent_ready → outcome_emitted` from a script file; (iii) honors commit-hash check (`hc-043`) with a build-time-pinned hash. NOT needed: silent-hang heartbeats, watcher-panic recovery, redaction, rate-limit. Implementable in under one self-build cycle once `.78` (wire-protocol fixture) lands.

**Q-B. Smallest LaunchSpec for v0?** Of 13 fields keep `{run_id, workflow_id, node_id, agent_type, workspace_path, timeout, schema_version}`; skip `required_skills`/`skill_search_paths`/`provisioning_timeout`/`budget`/`freedom_profile_ref`/`bead_id`/`snapshot_token`. Schema bead `.74` stays INCLUDE; carve-out is implementation guidance, not a separate bead.

**Q-C/D/E.** (i) `hc-024a` socket-I/O classification — keep EXCLUDE; revisit first self-build. (ii) Foundation declarative rules (`.17`/`.18`/`.22`/`.23`/`.27`) absorb at compile time — if `.41` labels beads `scope:bootstrap`, consider including for traceability. (iii) EM `b3f.86` failure-taxonomy attracts 3 HC edges; **EM's bootstrap subset MUST include `b3f.86`** — flagged for synthesis.
