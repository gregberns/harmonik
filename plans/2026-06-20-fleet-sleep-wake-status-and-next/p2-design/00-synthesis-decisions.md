# P2-DESIGN synthesis — reconciliation decisions log

**Codename:** `fleet-state` · **Bead:** hk-9fvk (P2-DESIGN) · **Date:** 2026-06-20
**Output:** `specs/system-state.md` (DRAFT v1.0.0, pending operator sign-off).
**Inputs:** five parallel lens contributions (lens-1 FSM-rollup, lens-2 drain-facts,
lens-3 cognition, lens-4 readers-union, lens-5 ZFC-spec-shape).

This log records the cross-lens reconciliation, the conflicts between lenses and
how they were resolved, the pre-resolved decisions applied verbatim, and the
short list of genuinely-open items for the operator.

---

## 1. Pre-resolved decisions applied (NOT re-opened)

These were settled by the orchestrator; the spec encodes them as-is.

1. **State labels** PROCESSING / WAITING / DRAINING / INACTIVE; priority order
   PROCESSING > DRAINING > WAITING > INACTIVE; first-match-wins. Lens-1's
   predicates are the normative definitions (spec §4.2, SS-003–SS-006).
2. **`Unsure` → fail-awake + veto.** `Unsure == true` prevents INACTIVE (folds
   into WAITING) AND non-force `harmonik sleep` refuses when Unsure; `--force`
   overrides. Stated normatively (SS-004, SS-006, SS-010, SS-015, SS-INV-005).
3. **No new captain/crew session FSM this pass.** Long-lived sessions observed
   via the coarse `SESSIONS_ALIVE` presence/liveness probe; the 8-state lifecycle
   FSM stays on in-flight RUNS only. Inventing a session FSM is explicitly future
   (spec §2.2 + §6 note).
4. **`harmonik state` shape** = `digest.Build` (disk/durable half, runs
   daemon-down) ∪ a new daemon-side `state` RPC (snapshots RunRegistry +
   QueueStore + computes the fold in-daemon) ∪ a sleep-marker scan; degrades to
   disk-only on daemon-down. Duplication rule: each fact tagged
   `source: live|disk`; live wins when daemon up; NEVER sum/union conflicting
   counts (SS-001a, SS-002fold).
5. **Cognition:** agent-name-keyed (survives `/clear` SID-flip); all thresholds
   config-sourced via `ResolveKeeperConfig` (NO literals); `absent` gauge ≠ zero
   (unknown, not stuck); `loop_detected` = a Haiku MODEL call with provenance
   (never a Go regex); session-level first; `subagents` is a named null stretch
   slot (SS-011–SS-014slot).
6. **ZFC invariants:** all 7 of lens-5's SS-INV-### are normative, each with its
   code-level catch. INACTIVE gates POLLS only, never triggers park (SS-007,
   SS-INV-004).
7. **Vocabulary:** "quiesce" dropped from this spec entirely (SLEEP/PARK/STOP/
   TEARDOWN; resting = "asleep"/"at rest"). The live internal `QuiesceArbiter`
   rename is a separate low-pri cleanup; park-resume-protocol.md still uses the
   word (P3-SPEC scrubs it) — both noted in SS-INV-006 (spec §3.1, §5).
8. **Desired-state:** §6 is a forward-looking STUB only (Phase 4 HOLD, hk-cyec);
   no desired-state field may appear in the actual snapshot (SS-INV-001, §6.2).

---

## 2. Cross-lens conflicts and how they were resolved

The five lenses were largely complementary (each owned a different slice). The
genuine tensions were about overlap and naming, not direction:

1. **`Unsure`: keep-fleet-awake (lens-1) vs read-quality-caveat (lens-2).**
   Lens-1 folds `GatherDrainFacts == UNSURE` into WAITING (fail-awake). Lens-2
   *demotes* UNSURE from the oracle's awake-keeping verdict to a plain flag that
   "does not keep the fleet awake and does not veto sleep," and explicitly flags
   this as the subtlest change for the synthesizer. **Resolution:** the
   pre-resolved decision #2 governs — `Unsure` IS a fail-awake control at the
   *fold* + *veto* boundary, even though it is no longer a verdict *inside the
   bundle*. The bundle reports `Unsure` as a flag (lens-2 is right about the
   bundle); the *fold* and the *sleep veto* re-home the safety (lens-1 is right
   about the consumer). Spec SS-010 states both halves explicitly: a caveat in
   the bundle, a fail-awake at the consumer.

2. **Where the in-flight run count comes from — three readers
   (lens-2 / lens-4).** `RunRegistry.Len()` (live) vs `.harmonik/worktrees/*`
   count (disk) vs queue.json `dispatched` items. Lens-4 flags all three and
   warns they must never be summed. **Resolution:** RunRegistry is authoritative
   live; the worktrees count is the daemon-down best-effort ONLY; never summed
   (SS-001a). The `RunAxis` keeps `RegistryCount` (live) and `LiveWorktrees`
   (disk) as *separate* fields so a stale-worktree-with-empty-registry case stays
   legible (lens-2's shape), and the source tags make the daemon-up-vs-down
   choice explicit (lens-4's rule).

3. **Queue status: live QueueStore vs queue.json on disk (lens-4).** Two readers
   for the same fact, which disagree during the persist window. **Resolution:**
   live wins when daemon up; disk is the down-only fallback; per-queue
   `source: live|disk` tag; do NOT silently union both (SS-001a). Matches
   pre-resolved decision #4 exactly.

4. **DRAINING vs WAITING race (lens-1 gap #3).** A sleep marker written while
   `GatherDrainFacts` still reports work. **Resolution:** first-match-wins, and
   DRAINING is tested before WAITING, so a present marker ⇒ DRAINING
   deterministically. Stated as a normative disambiguator in SS-005.

5. **No `draining` queue status (lens-1 gap #2).** The DRAINING *fleet label* is
   synthesized from `paused-by-drain` + terminating runs + sleep-marker presence,
   not read from a `queue.draining` enum (which does not exist). **Resolution:**
   documented in SS-005 so a reader does not expect the enum.

6. **`needs_decomposition` double-walk (lens-2 tricky-flag).** The epic-blocked
   axis and the new `needs_decomposition` axis both walk open epics with OPPOSITE
   predicates. **Resolution:** kept as an implementation note (compute both in one
   epic pass; remove the `scanOpenEpics` first-edge short-circuit) — this is a
   build-time concern for hk-pfr4, captured in SS-008a's defense table; not a
   spec contract change.

7. **`loop_detected` compute-timing (lens-3 vs the "no constant checker"
   decision).** Lens-3 resolves this itself: deterministic signals GATE the
   expensive Haiku pass; it never runs on every snapshot build; it is written
   back to `<agent>.loop.json` so subsequent reads are cheap. **Resolution:**
   adopted verbatim into SS-013, and reconciled with SS-INV-007 (the on-demand
   loop-write is gated to the ctx-watchdog refresh path, NOT the bare
   `harmonik state` read, so the read stays observation-only).

8. **ID-band collision (lens-5 outline used both `SS-002` for the fold-over-
   readers requirement AND for an envelope requirement).** **Resolution:** named
   the fold-over-readers requirement `SS-002fold`, the live-vs-disk one
   `SS-001a`, and the stretch slot `SS-014slot`, to avoid colliding with the
   sequential `SS-002`..`SS-015` band while still following the `a`-suffix
   convention from operator-nfr. No `SS-ENV-###` band is emitted in v1.0 (the
   spec emits no cross-subsystem events yet), per lens-5's "likely deferred."

---

## 3. Authoring choices beyond the lenses (cross-lens calls I made)

- **Section structure** follows lens-5's outline verbatim (§1 Purpose … §9
  Changelog) and matches operator-nfr house style: YAML front-matter with
  `requirement-prefix: SS`, `#### SS-NNN —` headed requirements with `Tags:` /
  `Axes:` footers, `#### SS-INV-NNN —` invariants each with a bolded `**Sensor.**`
  line, a §7 conformance section, a §9 dated changelog.
- **`Tags: mechanism` on every requirement** — there is no `cognition`-tagged
  requirement, which is itself the ZFC proof that this is a facts-only spec
  (lens-5 Part 4). `Axes:` only on the requirements that do I/O or a mutating
  branch: SS-001a, SS-008 (the read), SS-015 (the veto).
- **JSON envelope + Go structs are inlined in fenced blocks** (SS-001, SS-001a,
  SS-008, SS-011) so the spec gates code concretely, as the brief required.
- **`Status: DRAFT (P2-DESIGN, pending operator sign-off)`** is in the
  front-matter (`status: draft`) and in the lead blockquote — this is a draft,
  not yet ratified; hk-8lne is the ratification bead.

---

## 4. Genuinely-open questions for the operator (SHORT)

Only truly-open items — everything else was pre-resolved or resolvable in-spec:

1. **`needs_decomposition` and the sleep veto (SS-009 + SS-015).** A project with
   ONLY childless-open-epics (zero ready beads, nothing in-flight) has every
   *dispatchable* axis empty but a non-empty generative axis. SS-INV-003's sensor
   asserts the veto "still has grounds to refuse" in that case — i.e. non-force
   `sleep` would REFUSE when only `needs_decomposition` is non-empty. Confirm:
   should a childless-open-epic alone VETO a non-force sleep (treating "work to
   generate" as "work")? The spec currently implies yes via SS-INV-003; the
   alternative is that `needs_decomposition` is captain-only context that does
   NOT veto (only dispatchable/in-flight axes veto, per SS-015's enumeration,
   which does NOT list it). **These two are in mild tension — pick one.**
   Recommendation: `needs_decomposition` does NOT mechanically veto (it is the
   captain's generative judgment, not a Go-enforced stranding) — so SS-015's
   enumeration is authoritative and SS-INV-003's sensor should test "work-axes
   non-empty so the captain has grounds," not "the Go veto fires." Flagged for a
   one-word ratification.

2. **`stuck_min_intervals` + `loop_confidence_min` are NEW keeper-config knobs
   (SS-012/SS-013).** They do not exist in `KeeperConfig` today. Confirm they
   should be added to the keeper config block (with compiled defaults, fail-loud)
   as part of hk-jay1 — they are required for the stale/loop signals to be
   "config-sourced, no literals." This is a small additive config change, not a
   contract reversal; recommend just doing it under hk-jay1.

---

## 5. Review pass 1 — resolutions (2026-06-20)

Two adversarial reviews of the DRAFT spec surfaced four load-bearing issues
(a contradiction + three boundary leaks) and ten tightenings. All fourteen were
PRE-RESOLVED and applied; status stays DRAFT. Counts after the pass: 17 SS-###
requirements (incl. SS-001a / SS-002fold / SS-008a / SS-014slot), 7 SS-INV-###.

### Three design resolutions (the load-bearing ones) + rationale

1. **`needs_decomposition` is NOT a sleep-veto axis** (resolves SS-INV-003 ↔
   SS-015). The §4 open-question #1 above is closed in favor of the
   recommendation: a childless-open-epics-only project does NOT block a non-force
   `sleep`. Rationale: "work to generate" is the captain's generative judgment,
   not a Go-enforced stranding; SS-015's veto enumeration (dispatchable /
   in-flight axes) is authoritative and never listed `needs_decomposition`.
   Mechanism vs veto are now separated: `needs_decomposition` STILL counts in
   `HAS_LATENT_WORK` (keeps the fleet in WAITING, out of INACTIVE — §4.2) but
   does NOT fire the Go veto. SS-INV-003's sensor now asserts the axis is
   *flagged* (captain has grounds), NOT that the Go veto fires.

2. **`harmonik state` is purely read-only; loop-detection writes move to a
   separate `harmonik keeper loop-check` verb** (resolves SS-013 ↔ SS-INV-007).
   There is NO `harmonik state --refresh-loop-check` flag. The gated Haiku pass +
   its `<agent>.loop.json` write live behind `keeper loop-check`, invoked by the
   ~30-min ctx-watchdog tick; `state` only READS the already-written file.
   Rationale: keeps SS-INV-007 (observation-only, no snapshot-path write) true
   without losing the cheap-read property.

3. **A terminating-but-still-registered run = PROCESSING** (resolves the
   RUN_TERMINATING ↔ RUNS_INFLIGHT overlap). A run in `terminating` is still in
   `RunRegistry`, so it already satisfies `RUNS_INFLIGHT` and the fleet reads
   PROCESSING through teardown (watchers stay armed — safe). `RUN_TERMINATING`
   was therefore REMOVED from the DRAINING predicate as redundant/near-dead
   (registered ⇒ already PROCESSING; deregistered ⇒ FSM unreadable). DRAINING now
   keys on `QUEUE_DRAINING OR ANY_SLEEPING`.

### The 14 fixes applied

Load-bearing:
1. `needs_decomposition` no-veto — SS-INV-003 sensor reworded (flagged, not
   veto-fires) + SS-015 explicit no-veto sentence + §4.2 mechanism-vs-veto split.
2. RUN_TERMINATING removed from DRAINING; RUNS_INFLIGHT documented to include
   `StateTerminating` handles + why (§4.2 note, SS-003, SS-005) + §9 changelog.
3. `state` read-only; `keeper loop-check` is the writing actuator; no
   `--refresh-loop-check` (SS-013, SS-INV-007).
4. SS-INV-004 sensor → positive absence-assertion (zero `GenuineDrain` /
   `DrainStateDrained` call-sites in any tick body), independent of deletion.

Tightening:
5. §7 poll-gating conformance sensor (drive each of the 4 labels; INACTIVE
   disarms StaleWatcher+BandwidthTuner+DOT+reverse-tunnel; DaemonWatchdog always
   ON) — SS-007.
6. SS-007 co-location guard (INACTIVE poll-off MUST NOT share a call frame with a
   park/sleep actuator; enforced by SS-INV-004).
7. SS-012 `stale_stuck` prior-reading source = existing keeper `.ctx` gauge
   history (`Ts`/`Tokens`); "no new persistent store" reconciled with SS-002fold;
   keeps SS-INV-007 true.
8. SS-011 effective-band formula normative: `min(absolute_band_tokens,
   pct_ceiling × window_size)`, citing `EffectiveBandTokens` / `minAbsOrPctCeil`
   (internal/keeper/thresholds.go ~181).
9. SS-011 declared-vs-live SID split: `session_id_declared` (registry) alongside
   live `session_id` (.sid); mismatch → `sid_desync` read-quality flag (the
   /clear SID-flip must be VISIBLE).
10. §4.2 `QUEUE_DISPATCHABLE` re-stated as the exact `selectNextQueue` candidate
    filter (workloop.go:1100, incl. `EligibleItems`) as the single source of
    truth, rather than a drift-prone signature.
11. SS-INV-002 sensor retargeted: the `Drained`-field grep is vacuous (SS-008
    forbids the field existing); load-bearing sensor = no actuator gated on a
    caller-side fold over `FleetFacts`, caught by SS-INV-005's zero-call-sites
    test.
12. SS-007 WAITING StaleWatcher footnote = "ON-but-idle" (reconciles lens-1's
    "OFF"; functionally identical).
13. SS-006 prose: alive-but-workless reads cleanly as INACTIVE ("neither
    PROCESSING, DRAINING, nor WAITING — every axis empty whether or not a session
    is alive, OR no live sessions").
14. §8 bead map +hk-zqb3 (non-force sleep = veto-on-execute → SS-015 / SS-INV-005)
    +hk-kj7d (delete auto-park tick → SS-INV-004).

Plus §9 changelog dated "v1.0.0-draft → review pass 1" entry + "Open for operator
ratification" note (the 3 resolutions above + 2 config knobs `stuck_min_intervals`
/ `loop_confidence_min` under hk-jay1).

---

## 6. Design panel (pass 3) — 4-analyst verdicts + resolutions (2026-06-20)

A 4-analyst design panel reviewed the DRAFT and ratified/resolved the open items.
All applied to `specs/system-state.md`; status stays DRAFT. Counts unchanged: 17
SS-### (incl. SS-001a / SS-002fold / SS-008a / SS-014slot), 7 SS-INV-###.

1. **Terminating-run = PROCESSING — RATIFIED (operator panel).** The pass-2 OPEN
   item is closed = PROCESSING. The justification is tightened from "the FSM
   state" to **REGISTRY MEMBERSHIP**: a run is registered in `RunRegistry` for its
   entire life — claim → agent → review loop → merge (global-mutex-serialized) →
   build/vet gate → push-with-retry → worktree removal — typically 20–60 min,
   worst-case hours. PROCESSING therefore covers the whole post-agent
   merge/build/push/cleanup TAIL (the real teardown), exactly when run-watchers
   must stay armed (a hung merge / reviewer stall / stuck remote worktree-removal
   is the silent-hang class to catch). The lifecycle FSM `Terminating` STATE is a
   separate sub-second SIGTERM-sent micro-state (transitions back-to-back to
   Terminated with no work between) — NOT the teardown window, irrelevant to the
   label; the removed `RUN_TERMINATING` term referenced that micro-state, which is
   why removing it was correct. Do not conflate the two. Applied: §4.2
   RUNS_INFLIGHT comment + note, SS-003, SS-005, §9 changelog.

2. **`stale_stuck` → `context_static` — demoted from Go JUDGMENT to RAW FACT**
   (ZFC §32 / SS-INV-001 conformance). Declaring "stuck" from token-flatness in
   deterministic Go is a banned heuristic judgment (a session can be legitimately
   token-flat in a long test/tool call). Renamed to a fact-not-verdict name;
   reports `gauge_age_seconds` (measurement-quality fact vs `keeper.staleness`),
   `tokens_unchanged_intervals`, and a deterministic `flat` bool (= unchanged ≥
   threshold) — facts about the gauge readings, NOT a stuck verdict. MUST NOT be
   named/rendered a stuck judgment; no Go path acts on it as one. The "actually
   stuck" interpretation is the reader's (captain) or the same gated model pass as
   `loop_detected` — mirroring `too_big`'s band-LEVEL fact and `loop_detected`'s
   model call. gauge-stale ≠ flat-on-a-fresh-gauge preserved. Applied: SS-011 JSON
   key rename + fields, SS-012 bullet, SS-013 gating cross-ref
   (`stale_stuck token-flat` → `context_static.flat`), active-signals list.

3. **Keeper gauge + ctx-watchdog armed on SESSION LIVENESS, not label.** The heavy
   RUN-watchers (StaleWatcher, DOT gate-file poll, reverse-tunnel poll,
   BandwidthTuner) still stand down at INACTIVE. But the per-session keeper gauge
   watcher and the ctx-watchdog are gated on session liveness — a live session
   keeps its gauge armed even when the fleet label is INACTIVE (alive-but-workless
   case), because the whole point of the cognition fields is to catch an
   alive-but-confused captain/crew burning tokens at rest. Stand a session's gauge
   down ONLY when that session is asleep/gone (the existing `.sleeping.*` skip).
   This REVERSES the drafted INACTIVE-stands-down-gauges. **Operator may reverse.**
   Applied: SS-007 two table rows → "ON for any live session (skip only
   `.sleeping.*`)" + footnote ³ + §7 sensor.

4. **Daemon-down ⇒ unsure ⇒ never INACTIVE (safety).** A daemon-down snapshot is a
   disk-only read; it cannot prove the fleet is at rest (stale/leaked worktrees can
   over- or under-count), so it MUST set `read_quality.unsure = true` and MUST NOT
   emit `activity_label: INACTIVE`; the best-effort label holds at WAITING/DRAINING
   (or not-determinable). Stated as a MUST with a sensor (daemon-down snapshot test
   asserts unsure=true and label≠INACTIVE). Applied: SS-001a, SS-006, §7 sensor.

5. **New cognition config knobs opt-in / fail-loud** (honoring the zero-defaults
   MANDATE — the product imposes ZERO default keeper thresholds; the operator sets
   every value or `ResolveKeeperConfig` fails loud). `stuck_min_intervals` (and any
   future cognition threshold) is NOT defaulted from the resolved struct; until
   operator-set, the dependent signal (`context_static.flat`) is DARK/absent
   (`null`, not emitted) — no convenience fallback. `loop_confidence_min` stays
   deferred with the loop producer. Applied: SS-011 threshold prose + cross-ref,
   SS-012 `flat` bullet, §7 sensor.

6. **Fold run-presence accounts for live worktrees** when the registry is empty/
   unavailable AND the daemon is down: `RUNS_INFLIGHT = RegistryCount > 0 OR
   (daemon-down AND LiveWorktrees > 0)` — an OR (never a sum; two readers of one
   truth), so a leaked/live worktree doesn't under-count to INACTIVE on a blind
   disk read. Applied: §4.2 RUNS_INFLIGHT, SS-001a.

7. **Marker-wins keeps latent counts visible** (minor). Marker-wins ⇒ DRAINING is
   KEPT; added one clause that the latent-work counts remain visible in the
   unfolded `work_axes` even at DRAINING (reader not left blind). Applied: SS-005.

**Reviewed & KEPT AS-IS** (noted, no change): the max/union rollup (SS-003), the
five false-negative defenses preserved verbatim (SS-008a).
