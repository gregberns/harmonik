# Pilot — Decompose (affected components / spec areas)

**Work:** `pilot` — Pi-driven dispatch & control plane
**Inputs:** `01-problem-space.md`; `specs/{execution-model,cognition-loop,queue-model,operator-nfr}.md`; `docs/flywheel/2026-05-30-lifecycle-feasibility-and-gaps.md`
**Mode:** autonomous (no user present)

## Decomposition principle

This work changes **existing** specs only — it adds **no new spec file**. The
wave/stream queue model, the `operator_pause_status` event + queue-side consumer,
the `harmonik subscribe` channel, and the CL-051/070..073 control points all
already exist as text. The gap is that the contract is **incomplete or
contradicted by the code** in four places, and the Pi-side drive surface is
**unspecified at the call-site level**. So decomposition = four spec-area edits,
sequenced.

Each affected area below states: the change in one line, the concrete requirements
(what must be **true** after the change, not the text wording — that is Change
Design), and dependencies.

---

## Affected Existing Specs

### A1. `specs/execution-model.md` — quiet daemon + pause-gated fallback

- **Change summary:** define a no-auto-pull / queue-only daemon topology and
  reconcile the `br.Ready()` fallback with the existing "MUST NOT fall back to br
  ready" language, then gate that fallback on operator-pause state.
- **Requirements (what must be true after):**
  - EM defines a **daemon topology in which a bare boot with no submitted queue
    dispatches zero runs** (no `run_started`, no credit). The operator flag that
    selects it is named (the `--no-auto-pull` / `--queue-only` flag hk-exd7m tracks).
  - The contradiction between `execution-model.md:1395,1647` ("daemon MUST NOT fall
    back to br ready") and the live fallback at `workloop.go:833-858` is **resolved**:
    the fallback is specified as **opt-in**, off by default for the flywheel topology,
    preserved for the historical single-daemon topology (constraint C3).
  - The `br-ready` fallback dispatch path is **gated on operator-pause state** — when
    the daemon is `paused`/`pausing`, the fallback MUST NOT dispatch (today only
    handler-pause gates it; S9 br-ready gate, G3).
  - A conformance scenario exists for SC1 ("boot, submit nothing → zero `run_started`")
    and for the pause-gated fallback.
- **Maps to goals:** G1 (S2 quiet daemon), G3 (S9 br-ready pause gate).
- **Dependencies:** A3 (pause-state semantics must be defined before the fallback can
  be gated on them) — soft; the gate references A3's state names.

### A2. `specs/cognition-loop.md` — Pi dispatch surface (CL-070..073) + two-phase-done (CL-051) + pause-verb naming (CL-080)

- **Change summary:** realize the abstract loop control points (CL-070..073 curated
  dispatch, CL-051 two-phase-done) as a **concrete Pi drive surface that names the CLI
  calls**, and reconcile CL-080's pause-verb naming.
- **Requirements (what must be true after):**
  - CL-070..073 name the **concrete mechanism path**: harness reads `kerf next
    --format=json --only=bead`, applies the CL-072 pre-screen guards in order,
    dependency-orders survivors, and dispatches via `harmonik queue submit` (first
    fill) / `harmonik queue append` (refill on slot release) against a **stream**
    group. The mechanism/cognition line stays byte-clean (C1): eager refill is
    mechanism and MUST NOT consult the model; the empty-queue wake (CL-073) is
    cognition.
  - The eager-refill submit is **idempotency-keyed** per CL-055
    (`dispatch_intent:<event_id>:<bead_id>`) so it is effectively-once across crashes
    (C2).
  - CL-051 two-phase-done is stated as a **harness obligation with a definite
    done-condition**: a bead is DONE only when BOTH `run_completed{success}` is
    observed AND `git log origin/main --grep "Refs: hk-…"` is non-empty; the two
    single-condition windows route exactly per CL-051 (event-only → re-poll;
    trailer-only → `loop_observed_phantom_done` → Tier-2). The harness MUST NOT mark a
    deterministic completion done without the trailer check.
  - CL-080's pause-verb name is **reconciled to one canonical form** (OQ1) and the
    references in `budget.ts:8` / `circuit-breaker.ts:5` to a non-existent command are
    noted for correction.
- **Maps to goals:** G2 (S5/S6/S10 curated dispatch), G4 (S8 two-phase-done), G3 (S9
  pause-verb naming), G5 (stale-doc fixes).
- **Dependencies:** A4 (queue-model) for the stream-append + submit-as-start contract
  it references; A3 for the canonical pause-verb name.

### A3. `specs/operator-nfr.md` — pause/resume command + `operator_pause_status` producer

- **Change summary:** define the **agent-callable pause/resume command verb** and its
  **producer** of `operator_pause_status` / `operator_resuming`, mapped onto the
  existing ON pause/resume state machine (not a parallel one).
- **Requirements (what must be true after):**
  - ON defines a **command verb** (the `CommandPause`/`CommandResume` reserved in
    `internal/operatornfr/commandcodes.go:34` becomes wired) reachable by an agent
    (Pi), with the transport named (CLI→daemon Unix-socket RPC per PL-003a, or signal —
    OQ2). The agent-callable requirement is explicit: Pi can issue it without a human.
  - The command **emits `operator_pause_status{pausing|paused}` / `operator_resuming`**
    through the existing ON state machine (the one already emitting it in
    `statemachinetransition_test.go` / `eventreg_hqwn59.go`). Re-emit-once discipline
    (ON-012/ON-013c) is preserved; no new event type is invented (constraint C4).
  - The produced status is what **both** the queue-side consumer (A4 / QM-054) **and**
    the `br-ready` fallback gate (A1) observe — single source of pause truth.
- **Maps to goals:** G3 (S9 pause/resume producer).
- **Dependencies:** none (upstream producer; A1, A2, A4 depend on it).
- **Note:** operator-nfr is flagged "possibly touched" in the problem space. The
  decompose decision is that it **is** touched — it owns the operator command state
  machine, and inventing the producer anywhere else would create the parallel surface
  C4/N3 forbid.

### A4. `specs/queue-model.md` — stream-curation path + producer-side pause confirmation

- **Change summary:** confirm the **stream group** is the Pi curation path (accepts
  CL-071 appends) and confirm the `operator_pause_status` **producer** (A3) feeds the
  existing consumer side (QM-054) without changing consumer semantics.
- **Requirements (what must be true after):**
  - QM states that Pi-driven curated dispatch uses a **stream** group (appendable while
    pending/active), not the default **wave** group `harmonik run --beads` produces
    (which rejects appends) — constraint C5. `queue submit` returning `status: active`
    is confirmed as the "start" semantics that satisfies S6 (no separate start verb —
    non-goal N4).
  - QM confirms the **producer** added by A3 drives the existing active→paused-by-drain
    transition (QM-054, `queue_operatoreventconsumer_7urls.go:118-150`) with **no
    change to consumer semantics** and **no auto-resume across restart**
    (`queue-model.md:271`, C4).
  - The wake-on-submit guarantee (EM-NOTE-WAKE, hk-24xn1 closed) is referenced so CL-071
    eager refill can rely on sub-poll-interval append latency (C6).
- **Maps to goals:** G2 (S5/S10 stream curation), G3 (S9 producer→consumer wiring), G5
  (coherence).
- **Dependencies:** A3 (the producer it confirms is defined in operator-nfr).

---

## New Specs

**None.** All requirements land as amendments/annotations to A1–A4. Introducing a new
spec file would violate the project's spec-first convention (the contracts already have
homes) and the "don't add abstraction layers the user hasn't asked for" directive. The
changelog (`05-changelog.md`, Spec Draft pass) enumerates the per-control-point edits
across A1–A4.

---

## Dependency Map

```
A3 (operator-nfr: pause producer + verb)   ← upstream; defines pause truth + verb name
  ├─→ A1 (exec-model: br-ready gate references A3 pause state)
  ├─→ A2 (cognition-loop: CL-080 verb name references A3)
  └─→ A4 (queue-model: confirms A3 producer → existing consumer)
A4 (queue-model: stream-curation contract)
  └─→ A2 (cognition-loop: CL-070..073 reference the stream submit/append surface)
A1 (exec-model: quiet daemon) — largely independent; only the pause-gate sub-part
    depends on A3.
```

**Authoring order for Change Design / Spec Draft:** A3 first (pause producer + canonical
verb), then A4 (stream curation + producer→consumer confirmation), then A2 (loop
control points referencing both), then A1 (quiet daemon + br-ready gate referencing
A3's pause state). A2's two-phase-done sub-part (G4) is independent and can be drafted
in parallel.

---

## Goal → Area Traceability

| Goal (problem space) | A1 exec-model | A2 cognition-loop | A3 operator-nfr | A4 queue-model |
|---|:---:|:---:|:---:|:---:|
| G1 — S2 quiet daemon (no-auto-pull) | ✔ primary | | | |
| G2 — S5/S6/S10 curated dispatch | | ✔ primary | | ✔ stream path |
| G3 — S9 pause/resume + producer | ✔ br-ready gate | ✔ verb naming | ✔ primary | ✔ consumer wiring |
| G4 — S8 two-phase-done | | ✔ primary | | |
| G5 — coherent end-to-end + stale-doc fixes | ✔ | ✔ | ✔ | ✔ |

Every goal maps to ≥1 area. No area is listed without a goal: A1↔G1/G3, A2↔G2/G4/G3/G5,
A3↔G3, A4↔G2/G3/G5.

### Bead → spec-area coverage

| Bead | Title (short) | Primary area |
|---|---|---|
| `hk-ry8q1` (P0) | agent-callable pause/resume + producer; gate br-ready | A3 (+A1 gate, +A4 consumer) |
| `hk-dg42b` (P2) | Pi-side curated dispatch via `queue submit/append` (CL-070..073) | A2 (+A4 stream) |
| `hk-3ix6o` (P1) | CL-051 two-phase-done in `bridge.ts` | A2 |
| `hk-ytj2r` (P2) | replace `buildMinimalDigest` stub with real `harmonik digest --json` | A2 (recycle/digest; supports G2 post-reset) |
| `hk-5bw7a` (P3) | stale-doc correction (subscribe implemented; no supervise pause verb) | A2/A3 docs (G5) |

No attached bead is orphaned; no bead requires a spec area outside A1–A4.

---

## Cross-reference inventory (specs that reference the affected areas — read for ripple)

- `execution-model.md` ↔ `queue-model.md`: QM-010..012 dispatch, EM-015f group-advance,
  EM-NOTE-WAKE. Edits in A1/A4 must keep these cross-refs valid.
- `cognition-loop.md` → `queue-model.md §6` (consumes `queue-submit`/`queue-append`),
  → `reconciliation/spec.md §4.4` (CL-051 trailer-without-event routing),
  → `process-lifecycle.md §4.9` + `operator-nfr.md §4.3/§4.7` (CL-080 lifecycle).
  A2/A3 edits must keep these targets resolvable.
- `operator-nfr.md` ON-027 (drain ordering) is *inherited* by queue-model §8.5; A3's
  producer must not change ON-027's drain contract — only emit into it.
