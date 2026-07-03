# Flywheel v1 — Completion Plan (path to FULLY WORKING)

> **Goal (operator, 2026-06-16):** everything needed to FULLY complete flywheel v1 — *"fully complete
> includes it is fully working."* Documented + tasked into beads, incl. testing, deployment, validation.
> **Synthesizes:** test-coverage audit (agent a32e9d5a), spec-vs-code review (agent afb4ec56),
> integration-wiring map (agent a1fe59a8). Spec: `05-spec-drafts/flywheel-motion.md`. Epic: `hk-0oca`.

## Live status — 2026-06-20 (re-validated against `main` + the live daemon)
- **Baseline confirmed:** `sentinel.Evaluate()` STILL has zero production callers (grep `internal/`, `cmd/` → only `hooksystem`/`dot_gate` Evaluate, unrelated). The §0 honest baseline below holds unchanged — the loop is dead code on `main`.
- **All 26 beads OPEN, none started.** Ready-now head (no blockers): FW1 `hk-y9fn`, FW5 `hk-z25w`, FW6 `hk-psv4`, AC1 `hk-3ndb`, AC2 `hk-lacr`, AC3 `hk-zlwq`, AC5 `hk-kgwv`, BT1 `hk-tbg8`, BT2 `hk-fvzt`, ED1 `hk-zf4n`.
- **⛔ DISPATCH BLOCKED (NOT a flywheel defect):** the fleet daemon is routing every bead to the remote worker `gb-mbp` (`worker_name='gb-mbp'` on every run), whose remote-exec path fails (`agent_ready_timeout` / `hk-3vbc` worktree-create). Submitted FW1 + 4 others on 2026-06-20 → all instant-failed `no_commit (exit=0)` with empty worktrees. **No successful daemon run since 2026-06-18 20:58.** Root cause is owned by another agent (remote-substrate lane, gb-mbp e2e window in `.harmonik/workers.yaml`); flywheel dispatch resumes once local execution is restored (disable gb-mbp + daemon restart, or the remote path is fixed).
- **Next action when unblocked:** dispatch FW1 (keystone — pure config-adapter + workloop plumbing) → on merge, FW2 (wire `Evaluate` observe-only). Hold the workloop.go-heavy AC beads until FW1 merges to avoid same-file merge churn.

## 0. Where v1 actually is (the honest baseline)
The 9 v1 components are **landed on `main`, well-unit-tested, and faithful to the spec AS ALGORITHMS**
(governor staircase, mode-resolver, config validation, two-phase-done — all high quality). **But the
loop does not run:** `sentinel.Evaluate` has **zero production callers**, no trip auto-emits, the
adversary never auto-spawns, G-liveness never fires, the pending exception never actually blocks the
captain's all-clear, and the positive loop is a live no-op (no `sentinel:` config) with an in-memory
(non-crash-safe) ledger. So the spec's own §8.1 "ship the thinnest negative-loop slice so it can be
falsified against the real stream" is **NOT met**. **Fully working = wire it → test it → deploy it →
validate it live.**

---

## PHASE A — Implementation / wiring fixes (make the loop actually run)
*The blockers gate everything; nothing downstream can be validated until A1–A3 land.*

| Bead | Sev | Scope | Done-when |
|---|---|---|---|
| **A1 wire-governor-cadence** | BLOCKER | Invoke `sentinel.Evaluate` on a daemon tick (or `harmonik schedule` job); persist `GovernorState` across calls; on `ActivationActive` emit a trip + spawn the adversary; on `ActivationDormant` with a pending trip, `ClearTrip`. | A synthetic zero-movement+ready-bead window past warmup produces a `decision_required` sentinel exception in `events.jsonl` + `decision_acks/` with **no human running the CLI**; a later `bead_closed` auto-clears it. |
| **A2 exception-blocks-all-clear** | BLOCKER | Make the pending sentinel exception STRUCTURALLY block the all-clear (§2.1): consume `DigestJSON.PendingDecisions` in the captain/cognition all-clear path and/or wire `IsQueueBlocked` (currently zero callers); render it in `printHumanDigest`; read the durable `decision_acks/` anchor, not only the (non-fatal) `events.jsonl` line. | With a pending sentinel ack on disk, the captain digest CANNOT present "nothing to do"; it is rendered; a dropped `events.jsonl` line does not hide an on-disk block. |
| **A3 wire-g-liveness-halt** | BLOCKER | Add the consumer for `ActivationHalt`/`LivenessViolated` (§6.1, computed but never consumed): on violation, halt dispatch + emit a page/`liveness_halt` event. | After `liveness_no_progress_n` zero-progress governor cycles, dispatch halts and a `liveness_halt` page fires; N-1 cycles do not. |
| **A4 schedule+restart-wire-goalkeeper** | major | Register a `harmonik schedule` job (or idle-trigger) that runs `harmonik goal-keeper` (§4.2); add goal-state re-read on the captain resume path (§4.3); replace the dropped fixed `/loop 12m` tick with an idle-triggered realign (§4.4). | goal-keeper runs without manual invocation; captain resume re-reads `goal-state.json`; realign fires on idle, not a clock. |
| **A5 durable-staged-ledger** | major | §5.4 guardrail 4: `followUpLedger` is an in-memory `map` re-`make()`d per session (`workloop.go:786`) → a restart double-emits the deploy+verify follow-up. Persist `reacted_ledger` keyed `(target_bead_id, follow_up_class)` to disk, loaded at boot (or dedup by reading back the `followup:` label / existing bead). | Restart + replay of the same completion creates NO second follow-up bead. |
| **A6 captain-greenlight-gate** | major | §5.3/§6.2 "STAGED/captain-greenlit, NEVER autonomous" today rests ONLY on the daemon running `--no-auto-pull` (the staged bead lands `open`, no deps, `br ready`-eligible). Add a real gate — a `needs-greenlight`/`staged` block the daemon refuses to dispatch until cleared. | A staged deploy+verify bead is NOT dispatchable until explicit captain greenlight, independent of `--no-auto-pull`. |
| **A7 provenance-verified-merge** | major | §6.2: the generator gates on a per-run `runSucceeded` flag, not a verified `Refs:` SHA landed on `origin/main` ("own merged commits" is weaker than spec). Gate on confirmed origin/main landing. | A run that succeeds but whose commit is NOT on origin/main spawns NO follow-up. |
| **A8 legitimate-halt-clear-path** | major | §2.2 clause 2 is UNIMPLEMENTED: `ClearTrip` hardcodes `ack_method:"governor_movement"`, so a captain-declared legitimate-halt (e.g. ENOSPC) cannot clear/represent the exception. Implement the legitimate-halt class + its NEXT-pass re-adjudication; never bare self-ack. | A captain legitimate-halt clears the exception subject to re-adjudication next pass; a bare self-ack does not clear it. |
| **A9 verify-not-always-exit0** | minor | §5.3: the per-class verify command must assert an OBSERVABLE post-condition, not a trivial always-pass. Validate/reject a Phase-2 class whose verify is `true`/`:`/empty at config-load or emit. | A Phase-2 class with verify `"true"` is rejected; a real observable-postcondition command is accepted. |
| **A10 clear-trip-path + spec-reconcile** | minor | (a) Add a `harmonik sentinel clear-trip` operator escape hatch (or fold the auto-clear into A1). (b) §5.4 says "via the existing eagerfill refill path" but the generator shells `br create` (shares only the file, not `AppendItems`) — align the impl or correct the spec wording. | An operator/governor can clear a stuck trip; spec text matches implementation. |

---

## PHASE B — Tests (lock the behavior; rigor per operator)
*Unit gaps run in parallel after their component's Phase-A fix (if any). Scenario/e2e tests are the
behavioral validation and MUST be authored via a worktree sub-agent + cherry-pick (they exceed the
30-min daemon commit budget; the fast gate skips `//go:build scenario`).*

**B-unit (unit-gap beads, consolidate by component cluster):**
- **B1 governor units** — HEAD-advance counting (currently 0 coverage), weight-0-chatter vs a *populated* file, boundary-equality/staircase reproducibility, single-low-window stays WATCHING. *(test-audit A1–A3)*
- **B2 sentinel/trip units** — no-warn-nag over ≥5 cycles, never-clears-on-bare-self-ack. *(A4–A5; pairs with A8)*
- **B3 resolver units** — dialogue-recency decay (expired), issue-clearing-is-NOT-a-mode negative test. *(A6)*
- **B4 liveness + goal-keeper units** — G-liveness halt+page artifact + HEAD-advance reset; the entire goal-keeper distil contract (cursor advance, verbatim directives, MaxDirectives prune) — needs a small testability seam (remove the `exec.LookPath` coupling). *(A7–A8; pairs with A3/A4)*
- **B5 config + staged-bead units** — `sentinel:` sibling-to-`keeper:` non-interference + default accessors + malformed-expiry; staged-bead guardrail edges (WIP `==max-1` off-by-one, multi-class/multi-bead ledger keys, `--status open` + no-dispatch). *(A9–A11; pairs with A5/A9)*

**B-integration (component + real collaborators):**
- **B6 exception-blocks-digest-all-clear** — real projector + pending sentinel-class decision → captain cannot all-clear. *(pairs with A2 — highest-value assertion)*
- **B7 governor-trip→emit-trip wiring** — sustained-low+ready → exactly one `EmitTrip`; dormant → none; movement → `ClearTrip`. *(pairs with A1)*
- **B8 undeployed-tail-keeps-non-quiescent** — Phase-2 class + closed bead → digest reports actionable work even when `br ready` empty.

**B-scenario (`//go:build scenario`, real daemon — author via worktree+cherry-pick):**
- **B9 sentinel trips on idle+ready-work past warmup** (names the un-dispatched bead IDs; all-clear blocked). *(needs A1+A2)*
- **B10 exception clears on real movement, NOT self-ack.** *(needs A1+A8)*
- **B11 G-liveness halts a no-progress loop** (page fires; N-1 does not). *(needs A3)*
- **B12 work-gen-work spawns EXACTLY ONE deploy+verify bead, `open`, not same-tick-dispatched.**
- **B13 at-most-once ledger SURVIVES daemon restart** (one staged bead across the restart boundary). *(needs A5 — would FAIL today)*
- **B14 own-merged-commit provenance** (no follow-up when the run's `Refs:` SHA is absent from origin/main). *(needs A7)*
- **B15 adversary runs fresh-context as a foreign-artifact reviewer** (no captain transcript in its context; ≤1 exception/trip). *(needs A1+A4-adversary)*

---

## PHASE C — Deployment / activation (safe, default-off → on)
*Refined by the integration-wiring audit (a1fe59a8). Activate ONLY after Phase A lands + Phase B green —
turning on a sentinel that can block dispatch in the LIVE fleet daemon while it's unproven would risk
the whole fleet.*

- **C1 sentinel-config-block-live** — add the `sentinel:` block to the live `.harmonik/config.yaml`, **default OFF / observe-only** (governor evaluates + logs, but does NOT block dispatch or page) so we can watch its decisions against the real stream before it has teeth.
- **C2 build+deploy-wired-daemon** — `go install` + supervised daemon restart onto the wired binary; confirm the governor is now evaluated on-cadence (events show governor samples) with blocking still off.
- **C3 flip-sentinel-on** — after observe-only looks correct + Phase B green, enable blocking + the adversary spawn + G-liveness halt via config; staged-bead generator behind the A6 greenlight gate.

**Safe activation order:** A1–A3 land → B-unit/B-integration green → C1+C2 deploy **observe-only** → B-scenario green → C3 flip on → Phase D validate live.

---

## PHASE D — Validation (prove it is FULLY WORKING — the operator's bar)
- **D1 v1-smoke** — run the long-running smoke (`hk-m8zqv`, ~4h, needs the deploy) against the wired daemon.
- **D2 live-behavioral-validation** — demonstrate against the live loop: the sentinel actually trips on idle+ready-work, clears on real movement, blocks the all-clear; G-liveness halts a doom-loop; the goal-keeper runs + distils on schedule; work-gen-work spawns exactly one greenlit follow-up. (The Phase-B scenario tests are the deterministic half; this is the live half.)
- **D3 fully-working acceptance** — the flywheel demonstrably keeps itself moving / self-corrects on the real fleet without a human re-spinning it (the operator's definition of done). Sign-off.

---

## PHASE E — Docs / closeout
- **E1 spec+runbook** — reconcile the spec (§5.4 wording, document the wiring + activation), write an activation/operations runbook (how to tune `sentinel:`, how to clear a stuck trip, observe-only→on).
- **E2 epic closeout** — reconcile `hk-0oca` (stale) → close/redefine; mark v1 fully-complete; record v2 deferrals for separate scoping.

---

## DONE MEANS (fully complete — all true)
1. **Wired:** every Phase-A blocker+major landed + reviewed; the governor runs on-cadence, the exception blocks the all-clear, G-liveness halts, the goal-keeper is scheduled, the ledger is crash-safe, staged beads are greenlight-gated.
2. **Tested:** all Phase-B unit/integration/scenario tests green (incl. the restart-durability + no-self-ack + provenance scenarios that would fail today).
3. **Deployed:** the wired flywheel runs in the live daemon (observe-only → on).
4. **Validated:** D1 smoke + D2 live-behavioral + D3 sign-off — the loop demonstrably self-sustains on the real fleet.
5. **Documented:** spec reconciled, runbook written, epic closed/redefined.

## Integration refinements (audit a1fe59a8 — supersedes Phase-A wiring rows A1–A3)
Of the 9 components: **3 already LIVE** (mode-resolver `builder.go:141`, config loader + `done_definition`/`HasUndeployedTail` `builder.go:136,148`, eagerfill staged-bead generator `workloop.go:1226,4501` — *already firing; only needs a Phase-2 `done_definition` class configured*), **1 CLI-only** (trip), **5 DORMANT** (governor `Evaluate`, G-liveness, adversary spawn, goal-keeper schedule, goal-state resume-read). **The one missing keystone = the per-tick `sentinel.Evaluate()` call at `workloop.go:1226`.** Free wins already in place: `LoadDecisionAckState` (`daemon.go:1439`) restores a pending trip across restart; `init_cmd.go:333` writes the `sentinel:` block commented-out (safe default). The wiring decomposes into a **staged, de-risked rollout** (replaces A1–A3):
- **FW1** config adapter (`digest.SentinelConfig.GovernorConfig() sentinel.Config` — bridges the `map[string]int`→`map[core.EventType]int` type-mismatch) **+** deps plumbing (`governorState`/`governorCfg` on `workLoopDeps`, init at `daemon.go:1628`). Pure, low-risk, land together.
- **FW2** wire `Evaluate()` into the tick **OBSERVE-ONLY** (emit `GovernorSignal` as a typed event, **no EmitTrip / no halt**) — the falsify-early pass; guard behind `sentinel.mode: observe` (default). Medium-risk (hot loop).
- **FW3** **ACT mode** (config-default-OFF): on `ActivationActive` & not-suppressed → `EmitTrip`; on `Dormant`+ack → `ClearTrip`; on `ActivationHalt` → halt+page; **plus** the consumer fix the review found — render `PendingDecisions` in `printHumanDigest` and/or wire `IsQueueBlocked` (currently zero callers) so the trip truly blocks the all-clear. **HIGH-risk — can block dispatch in the live fleet daemon.**
- **FW4** spawn the fresh-context adversary on trip (`SpawnAdversary` via `deps.crewHandler`). Medium (LLM cost); overlap-skip bounds it.
- **FW5** schedule + seed the goal-keeper (`scheduleStore.Add`); **FW6** resume re-grounding (read `goal-state.json` on `/session-resume` + `restart-now` — a captain-skill change). Both low-risk.

**Safe activation order (Phase C):** FW1 → FW5/FW6 (independent) → FW2 observe-only **on a non-fleet project** (confirm the staircase reproduces by hand) → FW3 ACT default-off **on a validation project** (verify trip writes exactly one exception, clears only on real movement never self-ack, suppression masks it, restart preserves it, G-liveness halts a doom-loop) → FW4 adversary → **promote `sentinel.mode: act` on the LIVE FLEET daemon LAST.**

## Dependency summary
PHASE A (A1→A2→A3 blockers first; A4–A10 parallel) → PHASE B (each test after its fix) →
PHASE C (deploy observe-only after A1-3 + B-unit/integration green; flip-on after B-scenario green) →
PHASE D (validate) → PHASE E (docs/closeout). **Independent:** `hk-t08m` (the `-flywheel` tmux
coordinator-reaper leak — already filed). **Out of scope (v2, defer):** crew-utilization axis,
continuous-governor curve, work-gen sources b/c/d, ACT-mode auto-deploy.

## Filed beads (26, all `codename:flywheel,flywheel-v1-completion`, OPEN, dep-ordered, NOT dispatched)
**Phase A — wiring:** FW1 `hk-y9fn` adapter+plumbing · FW2 `hk-z1lr` Evaluate observe-only · FW3 `hk-4toh` ACT mode+block-all-clear [HIGH] · FW4 `hk-jsvc` adversary spawn · FW5 `hk-z25w` goal-keeper schedule · FW6 `hk-psv4` resume re-ground.
**Phase A — correctness:** AC1 `hk-3ndb` durable ledger · AC2 `hk-lacr` greenlight gate · AC3 `hk-zlwq` provenance · AC4 `hk-jvul` legitimate-halt clear · AC5 `hk-kgwv` verify+clear-trip+spec.
**Phase B — tests:** BT1 `hk-tbg8` units(governor/trip/resolver) · BT2 `hk-fvzt` units(liveness/goalkeeper/config/staged) · BT3 `hk-vdk4` integration · BT4 `hk-5v3r` scenario(trip/clear) · BT5 `hk-5pcr` scenario(liveness/work-gen/ledger-restart) · BT6 `hk-rsje` scenario(provenance/adversary).
**Phase C — deploy/activate:** CD1 `hk-p195` config-live(observe) · CD2 `hk-m33l` deploy+observe-validate(non-fleet) · CD3 `hk-8qot` flip-act(validation project) · CD4 `hk-tu3i` promote-act(live fleet, LAST).
**Phase D — validation:** DV1 `hk-dbpp` smoke(hk-m8zqv) · DV2 `hk-wxd8` live-behavioral · DV3 `hk-xq1i` fully-working sign-off.
**Phase E — docs:** ED1 `hk-zf4n` spec+runbook · ED2 `hk-n6rb` epic closeout.
**Independent (already filed):** `hk-t08m` (`-flywheel` tmux coordinator-reaper leak).
**Ready-now head** (no blockers): FW1, FW5, FW6, AC1, AC2, AC3, AC5, BT1, BT2, ED1.
