# Bootstrap Subset — Deferred Cluster Enumeration (AR / CP / ON / RC)

**Date:** 2026-05-05
**Tracks:** `hk-ahvq.41` Pass 2 — sparse-INCLUDE pass for the four "deferred overall" clusters.
**Inputs:** opening pass (§3 cluster note); `core-scope.md` Ground rules (RC non-core, Pi post-MVH); `bootstrap.md` §2 MVH cut + §5 step order + §4 between-task framing; user-resolved Q1 (TWIN in), Q2 (Pi out), Q4 (S07 in).
**Epics queried via `br show`:** `hk-zs0` (AR, 54 beads), `hk-a8bg` (CP, 85 beads), `hk-sx9r` (ON, 84 beads), `hk-63oh` (RC, 79 beads).

Header working definition: an INCLUDE bead is one whose absence breaks the §1 foothold scenario (start → resolve a non-agentic bead → twin round-trip → checkpoint → merge → restart with no state loss) under Q1 + Q4. "Implicit-via-A–F-conformance" beads are NOT INCLUDE.

---

## 1. AR — Architecture (`hk-zs0`, 54 beads)

Opening pass said "sensor beads only (`zs0.41`, `.50`)." Verification finds **five** invariant-sensor beads, not two. The opening-pass list is incomplete.

INCLUDE (5 sensor beads only — all `kind:invariant`):

- **`hk-zs0.41`** — *Invariants MUST name their sensor* (AR-042). Spec-draft-time-only sensor, but it gates 7 cross-cluster invariant beads (CP `.7,.16,.28,.29,.30,.40,.70,.78`; RC `.44,.45`; AR `.50,.52`).
- **`hk-zs0.50`** — *Sensor: mechanism/cognition split is strict at the process boundary* (AR-INV-001). Reviewer-enforced corpus-search heuristic.
- **`hk-zs0.51`** — *Sensor: search + verifier + traces are required* (AR-INV-003). Corpus-presence test against EM corpus.
- **`hk-zs0.52`** — *Sensor: centralized-controller invariant* (AR-INV-007). Reviewer-agent scenario; load-bearing for the centralized-controller thesis.
- **`hk-zs0.53`** — *Sensor: three-artifact separation* (AR-INV-008). Corpus-lint blocking introduction of a fourth compositional artifact.

Everything else in `hk-zs0` (the 48 declarative requirement beads `.1`–`.40` plus `.42`–`.49`, `.54`) is satisfied by structural conformance of A–F clusters (envelope declarations, role vocabulary, four-axis tagging, etc.) and does not require a dedicated implementation task at MVH. **Count: 5.**

## 2. CP — Control Points (`hk-a8bg`, 85 beads)

Opening-pass guidance: "control-points are post-skeleton — defer entirely." Verification holds. CP §6.1 records (Gate / Hook / Guard / Budget / FreedomProfile / Role / Registry, beads `.57`–`.80`) are evaluator types only invoked when the workflow has gate/hook/guard/control-point nodes — none of which appear in a 1–2-node linear DOT (Q1 scenario). The §6.2 freedom-profile / role records are not exercised until S02 policy engine arrives, which `core-scope.md` §10 keeps for post-MVH.

INCLUDE: **NONE — fully deferred.** The "trivial pass-through" pattern named in `bootstrap-subset-opening.md` §1 means: no Gate node, no Hook attach point, no Guard, no policy YAML. CP can stay 0/85 through MVH. **Count: 0.**

## 3. ON — Operator NFR (`hk-sx9r`, 84 beads)

Opening-pass named exceptions: `.4` (startup-failure catalog) + `.20` (queue-schema version-check). Both verify as appropriate INCLUDE candidates. Q4 (S07 IN) forces additions — see §4.

INCLUDE under opening-pass guidance (verified):

- **`hk-sx9r.4`** — *Startup failure-mode catalog obligation* (ON-003). Catalog co-owned with PL §4.2; required by PL startup steps (cluster A). Confirmed.
- **`hk-sx9r.20`** — *Queue schema version check on daemon startup* (ON-016). Blocks dispatch when Beads + harmonik schemas mismatch; load-bearing for the "start cleanly" requirement of §1. Confirmed.

INCLUDE forced by structural prerequisites (cited by `.4` / `.20`):

- **`hk-sx9r.2`** — *Operator-observable exit codes are structured* (ON-001). Direct dependency of `.4` and `.5`; required for the daemon to surface a meaningful exit when the catalog fires.
- **`hk-sx9r.3`** — *Exit-code taxonomy obligation* (ON-002). Spec-draft obligation; thin but cited by RC `.35` (verdict-execution durability).
- **`hk-sx9r.73`** — *§8 23-code authoritative table*. The §8 home that `.2`, `.3`, `.4`, `.20` resolve into. Cluster A and BI clusters cite this for exit codes.

INCLUDE forced by Q4 — see §4. **Count under opening-pass guidance: 2; with prerequisites: 5.**

## 4. RC — Reconciliation (`hk-63oh`, 79 beads)

Opening-pass guidance: "Cat 0 (no-op resume) only; everything else first-self-build-cycle." Verification confirms. The §1 "restart with zero state loss" scenario maps to Cat 0 (infrastructure pre-check) plus Cat 5 (clean restart) only.

INCLUDE:

- **`hk-63oh.62`** — *Cat 0 — Infrastructure unavailable (§8.1)*. The taxonomy entry; defines detection rule (br --version, git rev-parse, .harmonik/ writable).
- **`hk-63oh.16`** — *Cat 0 pre-check runs before any other detector* (RC-012). The runtime obligation behind Cat 0; gates ON `.4` and PL startup steps `hk-8mup.10/.19`.
- **`hk-63oh.17`** — *Post-`ready` Cat 0 does not transition daemon state* (RC-012a). Carve-out preventing health-probe-induced state thrash; needed because §1 requires daemon stays `ready` after a clean run.
- **`hk-63oh.70`** — *Cat 5 — Clean restart (§8.8)*. The "no-op resume" path the working-definition explicitly names. Verify this is ALWAYS reachable on restart of a quiescent daemon.

INCLUDE: **4 beads** (Cat 0 trio + Cat 5). Everything else (Cat 1–4, 6a, 6b, investigator-agent contract, verdict-executor, detectors, taxonomy umbrella `.61`) is post-MVH. **Count: 4.**

---

## 5. Q4 / scenario-harness analysis

**Q4 (scenario harness IN) does NOT pull substantial additional ON beads.** The reason: the ON-side `kind:test-infra` beads (`hk-sx9r.74`–`.84`) are *test fixtures the scenario harness consumes*, not scenario-harness *implementation*. They become INCLUDE only when their target ON requirements become INCLUDE. For the §1 foothold:

- **`hk-sx9r.74`** (Exit-code fixture, ON-001..ON-004) is plausibly INCLUDE since its targets (`.2`, `.3`, `.4`, `.5`) are now INCLUDE. **Add to INCLUDE list.**
- **`hk-sx9r.76`** (operator-control FSM harness, ON-007..ON-014) — Q4 §1 scenario does NOT exercise pause/stop/upgrade. Defer.
- **`hk-sx9r.79`** through **`.84`** — none of upgrade / multi-daemon / silent-hang / RTO benchmark / shutdown drain is in §1 foothold. Defer.

**Scenario-harness implementation itself** is not in this cluster — see §7 below.

**RC additions from Q4:** none beyond Cat 0 + Cat 5. The Q4 scenario is a single happy-path run; reconciliation only fires on restart, where Cat 5 + Cat 0 cover it.

**ON post-Q4 INCLUDE total: 6 beads** (`.2`, `.3`, `.4`, `.20`, `.73`, `.74`). The `.5` config-inventory bead is a borderline — defer unless the foothold daemon reads operator-configurable knobs at startup, which under §1 it does not (defaults suffice).

---

## 6. Cross-cluster edges (deferred-INCLUDE beads pointing INTO bootstrap)

Yes — every INCLUDE bead identified above has `blocks` edges into the bootstrap clusters. This is the operational reason these few beads cannot be deferred wholesale.

- **`hk-zs0.41,.50,.51,.52,.53`** are required by invariant-sensor beads in CP (`hk-a8bg.7,.16,.28,…`) and RC (`hk-63oh.44,.45`). For MVH, only the AR sensors themselves need to be marked INCLUDE; the CP/RC consumers are deferred under §2/§4 above. (The blocks-edge structure does not flip the deferred state — sensors are spec-draft-time obligations, not runtime code.)
- **`hk-63oh.16` (Cat 0 pre-check)** blocks **`hk-8mup.10`** (PL deterministic startup) and **`hk-8mup.19`** (PL degraded-state on Cat 0) — both PL beads are in cluster A. Confirms RC `.16` is genuinely load-bearing for cluster A.
- **`hk-sx9r.4` (startup catalog)** blocks **`hk-63oh.16`** (declared above) and references `hk-hqwn.59.60` (`daemon_startup_failed` event row, cluster D). Edge into D is fine — that event type is in cluster D INCLUDE per opening-pass §3.
- **`hk-sx9r.20` (queue version check)** blocks **`hk-872.25,.26`** (BI version pin + handshake, cluster E). Cluster E `br --version` is INCLUDE per opening-pass.

No surprise cross-cluster edges from the deferred-INCLUDE set into anything *outside* the established bootstrap clusters A–F.

---

## 7. Open questions / ambiguities

- **S07 scenario-harness implementation has no dedicated epic.** Confirmed by `br list --title-contains "S07"`: only `hk-8i31.45` (twin conformance drift detection scoped to S07) returns. Scenario-harness substrate beads — runner, scenario-DOT loader, twin orchestration glue — are absent from the corpus. The closest analogue is **`hk-8i31.77`** (*Canonical twin handler binary for scenario-harness tests*, cluster C). This is a corpus gap: `bootstrap.md` step 8 names S07 as "minimal scenario runner + first end-to-end scenario" but no spec was authored for S07 (specs/ has 11 files, none scenario-harness).
  - **Implication:** Q4 "S07 IN" cannot be satisfied by a bead INCLUDE list alone. S07 is implementation-only — code authored as a twin-orchestrating wrapper around clusters A–F + HC `hk-8i31.77`. This should be flagged for the Pass-3 synthesis as a **gap requiring either a thin S07 spec pass or explicit "S07 = author-as-code-no-bead" carve-out.**
- **Should AR `.51`–`.53` be in this Pass-2 list?** They are reviewer/corpus-time sensors, not runtime code. Defensible argument either way. Listing them ensures the sensor obligation is named; deferring them risks AR-INV-003/007/008 drifting through MVH unaudited. Recommend: keep INCLUDE (5 sensors), let the reviewer-agent absorb the spec-draft-time sensor work.
- **ON `.32`+drain steps `.33`–`.40`** — the 8-step graceful-shutdown drain. Q1 §1 step 4 ("Survive a clean shutdown + restart") is ambiguous: is "clean shutdown" SIGTERM with 8-step drain, or just `daemon stop` exit code 0? Latter reading defers the drain umbrella entirely; former pulls in `.32` + 8 step-beads + drain-timeout `.43`. **Recommend the latter for MVH** (drain is a policy concern that belongs in cluster A's startup work, not ON), but flag for synthesis.
- **No `scope:bootstrap` label exists yet** (per opening pass §2). Pass-3 synthesis will need to decide whether to apply it via `br update` or keep enumeration in markdown only.

---

**Total deferred-cluster INCLUDE under Q1+Q4: 15 beads** (AR 5 + CP 0 + ON 6 + RC 4) — within the opening-pass `~5–15` range. The growth above the named two-each came from invariant-sensor enumeration completeness (AR) and structural prerequisites of `.4`/`.20` plus the Q4-justified test-infra fixture (ON), not from Q4 forcing wholly new pause/stop/upgrade work.
