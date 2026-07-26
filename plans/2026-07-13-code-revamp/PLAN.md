# Code Revamp — Structured Plan (DRAFT v1)

> **Status: DRAFT for operator direction.** Built from the six research dossiers in
> `research/` (real `file:line` facts, not summaries) plus the census
> (`plans/2026-07-12-codebase-census/REPORT.md` + `PLAN.md`) and the event-substrate
> direction (`generative-system-exploration/{3,4}.md`). Fleet FROZEN; nothing dispatches.
> This plan decides **what "good" is, which areas are worst, in what order, behind which
> abstractions** — and zooms into the first small vertical concretely. It does not create
> beads or lift the freeze.

## 0. How to read this plan

The operator's ask: *take one part of the code, embed typed-functional + good architectural
discipline, prove it, then generalize.* This plan is organized to answer that in order:

1. **§1 What "good" already looks like** — we do not have to invent the target. Two packages
   in this repo already embody it: `internal/queue` (functional-core/imperative-shell +
   consumer-owned ports, spec-pinned, named regression tests) and the codex app-server stack
   (the same, *plus* record→replay→fault-injection). The revamp is "make more of the tree
   look like these two," not "import a new paradigm."
2. **§2 The concerning areas, ranked** — from the dossiers.
3. **§3 The abstraction stack** — the small number of seams/ports/enforcement levers every
   vertical will share. This is the "what abstractions are needed and how we structure this."
4. **§4 The order** — general sequence, with the dependency reasoning.
5. **§5 The first vertical, concretely** — session-restart, structured end to end.
6. **§6 Measurement**, **§7 Enforcement**, **§8 Open decisions**.

---

## 1. What "good" already looks like (the two exemplars)

We are not starting from a blank idea of "typed FP + hexagonal." Two working, green subsystems
already are the target shape. Every principle the operator named maps to something concrete
here.

### 1a. `internal/queue` — the hexagonal / functional-core island
(dossier 06 §3)
- **Mutex-free:** 0 mutexes in the package; the daemon owns all locking. (Daemon has 47.)
- **Functional core / imperative shell by contract:** *"Each handler is a pure function: it
  receives the parsed request and the in-memory queue state … and returns a typed response"*
  (`internal/queue/rpc.go:5`). I/O + emission live in the daemon shell.
- **Consumer-owned ports (dependency inversion for free):** queue declares the *minimal*
  interfaces it needs (`QueueSetter`, `EventEmitter`, `BeadLedger`, `HandlerPauseChecker`) and
  the daemon's types satisfy them structurally — so queue never imports daemon. This is
  textbook ports-and-adapters (`rpc.go:56/66`, `validation.go:144/170`).
- **Spec-pinned + named regression tests:** 41 QM-/EM- spec citations; 11 test files named for
  the exact bead/rule they pin (`validation_em065_hkxizhl_test.go`, …).

This is the standard. "Embed FP/architectural discipline" = **replicate the queue island's
properties** in the areas that lack them.

### 1b. The codex app-server stack — the island *plus* record→replay
(dossier 03)
The five codex packages are a built, green instance of the exact quality mechanism the
operator wants: **capture a raw stream → pure decode → pure state machine → swappable effect →
replay the captured corpus with fault injection → an L0–L3 test taxonomy that is ~95%
zero-token.** The reusable spine is three tiny interfaces + a 10-line driver:

```go
// internal/codexreactor/reactor.go
type EventSource interface { Events(ctx context.Context) <-chan Event }   // :133
type Effector    interface { Execute(ctx context.Context, a Action) error } // :120
func (r *Reactor) Run(ctx, src EventSource, eff Effector) error { … }      // :282 — for ev := range src.Events { for _, a := range r.Step(ev) { eff.Execute(a) } }
```

Any `EventSource` × any `Effector` composes; the reactor never knows if the effect is real,
a `FakeEffector` (records actions), or a `HarmonikBridgeSink`. The `Twin` replays a captured
`.jsonl` corpus as an `EventSource` and injects drop/stall/truncate/dup faults (dossier 03 §5,
§7). Dossier 03 §8 already maps, line by line, **what is generic (moves to `internal/substrate`)
vs codex-specific (stays)**.

**One flagged friction (dossier 03 §8 note):** the seam is hard-typed to
`codexreactor.Event`/`Action`. Genericizing it needs Go generics (`EventSource[E]`,
`Effector[A]`, `Run[E,A]`) or an `any`-typed boundary. Interfaces are one method each, so the
cost is low, but this is the one non-copy-paste part of the extraction.

---

## 2. The concerning areas, ranked

Grounded in the dossiers. Ranked by *how much they block the revamp method itself* (an area
you cannot record/replay/test is worse than one that is merely large).

| # | Area | The concrete problem | Dossier | Disposition |
|---|------|----------------------|---------|-------------|
| 1 | **daemon run-lifecycle god-function** | `beadRunOne` = ~2,366 lines, 17 params incl. a `*bool` out-param; `workLoopDeps` = 85 fields passed by value but aliasing shared mutex/map state; launch→gate→merge→close open-coded **4×**; no explicit state machine (spread across 4 representations). | 01 | **Refactor/extract** (keep the ~466-test net) |
| 2 | **`mergeMu` held across build+network+git IO** | one global mutex spans `git rebase → go build/vet → git push → reset → br sync` (retry loop) **plus** worktree-add **plus** the escape check — so ≤1 bead across *all* queues can be in any of those phases; `cacheReapMu` W-lock spans `go clean -cache` up to 5 min. | 01 §3 | **Rebuild the merge boundary** (explicit merge queue) |
| 3 | **Agent-input channel is ack-free** | `handler.Substrate` seam has **no input method**; input flows out-of-band via type-asserted side-interfaces; exit-0 ≠ TUI accepted (code says so); only half-measures (screen-scrape verify, blind Enter×3). | 05 A | **Two peers:** Codex → structured driver; Claude → keep tmux paste, source the ack from the **hook bridge** (`Stop`→`outcome_emitted`, `SessionStart`→`agent_ready`), bounded by a stale timeout. No structured Claude driver (subscription-first). See `M2-RESCOPE-hook-sourced-ack.md` |
| 4 | **Remote/SSH channel is ack-free + event-dark** | fresh `ssh -- '<string>'` per op through the login shell, ControlMaster OFF; box-A mutexes own worker state; an embedded Python flock script patches a lost-update race; **emits zero events** — the least observable path in the system. | 05 B, 04 §7 | **Rebuild** (instrument first) |
| 5 | **No single writer for shared state** | `queue.json` has two writers (queue-RPC `Load→AppendItems→Persist→SetQueue` at `rpc.go:1006-1016` **outside** the daemon's `queueMu`) → last-write-wins lost update. | 06 §4 | **Fix** (data-integrity) |
| 6 | **Restart vertical has an unimplemented invariant** | Step 4 "wait for model done before `/clear`" (**SR4, load-bearing**) has *no interior implementation* — cycle goes confirm→`/clear` with only a `.idle`-marker pre-gate. 32 direct clock sites in `cycle.go`, no `ClockPort`. Interior steps are journal-only (overwritten), not on the bus. | 02 | **First vertical** — see §5 |
| 7 | **No enforcement of the principles** | **No cyclomatic/complexity linter** (gocyclo/cyclop/funlen/gocognit all absent) → `workloop.go` violates nothing. **No coverage gate.** No typed-FP containers (0 Result/Option/Either). depguard leaves ~32/45 packages unconstrained and `daemon` is allow-all (no deny). | 06 §6 | **Turn on the levers** — see §7 |
| 8 | **Test-theater masks real coverage** | `specaudit` = 37.6k test LOC / 9 non-test; `operatornfr` = 13.6k test LOC. "Green" partly means "constants asserted," not "product ran." | census §4 | **Delete/relocate** (census M1) |
| 9 | **Dead event-registry read path** | `DecodePayload`/`DispatchObservational/Synchronous`/`ValidateEnvelopeSchemaVersion`/`pertypecompat` table have **zero production readers**; only the write-side version-stamp + secret-scan are live. | 04 §6 | **Adopt or delete** — see §3d |

> **SUPERSEDED BY P1:** the "zero production readers / adopt-or-delete" framing is stale.
> `internal/replay` is now a **LIVE reader** of the typed-decode path, and **ADOPT is ratified**
> (D6 / EV-048 normative at `specs/event-model.md:720`). This is no longer an open disposition.

---

## 3. The abstraction stack (what every vertical shares)

Five shared pieces. Build them once; each vertical instantiates them. This is the structural
answer to "what abstractions are needed."

### 3a. `internal/substrate` — the generic record→replay seam
Extract from the codex stack (dossier 03 §8), generic over event/action types:
- `Tap` — the protocol-agnostic stdio capture tee (`internal/apptap/tap.go`, moves ~verbatim;
  note it has **no production consumer yet** — currently test-only).
- `EventSource[E]` / `Effector[A]` interfaces + the `Run[E,A]` driver loop.
- `FakeEffector[A]` (recorder) + `SyntheticSource[E]` (fixed-slice source) — both test doubles.
- The `Twin` replay engine + `FaultConfig` (drop/stall/truncate/dup), parameterized by a
  pluggable `decode([]byte)→Frame` and `frame→Event` mapper.
- The **L0–L3 taxonomy + Makefile policy** (zero-token replay tiers + one env-gated live tier +
  drift canary).
Codex becomes the first instantiation; session-restart the second. **Decision needed** (§8): Go
generics vs an `any`-typed boundary for the seam.

### 3b. Ports — the hexagonal boundary for each vertical
Each vertical names the *minimal* interfaces it needs and lets the shell satisfy them
structurally (the queue pattern, §1a). For session-restart the ports are already latent as
`CyclerConfig` function-fields (dossier 02 §6) — this is a promotion of existing seams to
named interfaces, not a rewrite.

### 3c. `ClockPort` — the #1 testability unblock
Time must be a port before any replay is deterministic. Today: 32 direct clock sites in
`cycle.go`, real-wall-clock sleeps in `injector.go`; only `restartnow.go`/`awaitack.go` thread
an injectable `now` (dossier 02 §3). A single `Clock` interface (`Now`, `Since`, `NewTicker`,
`Sleep`) threaded through the cycle core is the prerequisite for property-testing timeouts and
poll races. Same gap exists in the daemon (`time.Now()` throughout `beadRunOne`).

### 3d. Durable interior events — the record substrate
Record→replay needs the interior events to *exist and be durable*. The bus + offline replay
primitive already exist and are production-proven: `eventbus.ScanAfter`/`Filter`
(`jsonlwriter.go:312/380`), append-only NDJSON, UUIDv7-ordered (dossier 04 §5). What's missing:
- **Keeper interior events** (steps 3/4/5-success/6-success) are journal-only + overwritten;
  keeper events also carry a **zero run_id**, so they're not joinable (dossier 04 §7). Fix:
  emit 3–4 durable interior events with a real cycle/run id.
- **Remote emits nothing** (dossier 04 §7) — instrument before rebuilding (area #4).
- **The dead typed-decode path** (§2 #9) is exactly the type-safe payload decode + schema
  assertion a replay harness wants for invariant checks — **adopt it** rather than delete, or
  delete and let the harness do ad-hoc unmarshal. **Decision needed** (§8).
  > **SUPERSEDED BY P1:** decided — **ADOPT** (D6 / EV-048). The path is no longer dead;
  > `internal/replay` reads it. No §8 decision remaining.

### 3e. Enforcement levers — embed the principles structurally
The operator's "beyond linting" is right in spirit, but several structural levers simply
**aren't turned on** (dossier 06 §6). See §7.

---

## 4. The general order

The ordering rule: **instrument → extract the shared seam → prove it on the smallest
self-contained vertical → then take the big/ugly areas behind that proven seam.** Two tracks
run in parallel because their file sets are disjoint.

```
TRACK A — method proof (low blast radius, single-writer)
  A1  ClockPort + 3–4 durable interior keeper events        (§3c, §3d)   ── prereq
  A2  Extract internal/substrate from codex stack           (§3a)        ── generic seam
  A3  First vertical: session-restart behind the seam       (§5)         ── proof + real fix
        └─ first property test = SR4/SR9 ("never /clear before model-done;
           every cycle terminates or emits failure") = a real correctness gap today
  A4  Measurement: replay the 476 recorded cycles vs baseline (§6)

TRACK B — data-integrity fixes (parallel, out-of-pipeline)
  B1  queue.json two-writer fix (rpc.go:1016)               (area #5)
  B2  noChange-subsumption false-close (census 0b)          (census)
  B3  resume-hang direct fix OR fold into A3's SR9 invariant (area #1 branch / census 0a)
  #   > SUPERSEDED BY P1: the DAEMON resume-hang was relocated to M3-5 (it lives inside
  #   > beadRunOne — the function M3 rewrites). Keeper SR9 / SK-INV-005 is a SEPARATE keeper
  #   > invariant, not the daemon hang; the two are no longer the same item.

TRACK C — enforcement (parallel, mechanical)
  C1  Turn on complexity + coverage levers                  (§7)

  ──────── after A3 proves the method + C1 caps regressions ────────

  M-remote   instrument (done in A) → rebuild remote behind the seam    (area #4, census M4)
  M-input    rebuild agent-input channel behind a real protocol         (area #3, census M2)
  M-runexec  extract beadRunOne state machine + merge-queue split        (areas #1,#2, census M3)
  M-tests    delete/relocate test-theater                                (area #8, census M1)
```

**Why session-restart is first** (not the daemon god-function): it is the only candidate that
is (a) self-contained — `internal/keeper`, depguard-forbidden from importing daemon; (b)
already partly event-sourced; (c) a clean exemplar of the whole method; and (d) its first
property test closes a *real, currently-unenforced* correctness gap (SR4). The daemon
god-function (areas #1/#2) is the highest-value target but the highest blast radius — it goes
*after* the seam is proven and the complexity/coverage levers (C1) are catching regressions.

**Where Fable fits:** the collection pass just done, and later the bulk-mechanical passes —
per-file keep/delete classification for M-tests, and the wide "map every clock/emit site"
sweeps — are ideal Fable fan-outs. The synthesis/design judgment (this plan, the kerf specs)
stays on the top model.

---

## 5. The first vertical, concretely — session-restart

The proving ground. Structured end to end so it doubles as the `internal/substrate` template
validation.

**The 7-step stream → its real code** (dossier 02 §1): steps 2/3/5/6/7 map to concrete
functions (`cycle.go:989/1003/1111/1128/1147`); **step 1** is an 11-gate ladder re-evaluated
every 5s (not one event); **step 4 has no code at all**.

**Structure the rebuild as:**

1. **Ports (from existing `CyclerConfig` fn-fields, dossier 02 §6):**
   - `ClockPort` (§3c) — replaces the 32 direct clock sites + injector sleeps.
   - `PanePort` — the tmux inject/capture seam (`InjectFn`, `CapturePane`).
   - `GaugePort` — token/session_id read (`ReadCtxFile`).
   - `HandoffPort` — handoff-file nonce poll + mtime freshness.
   - `EmitterPort` — durable bus (already `FileEmitter`).

2. **Events (make the interior durable, §3d):** add `keeper_handoff_written`,
   `keeper_model_done`, `keeper_clear_sent`, `keeper_new_session_up` as durable bus events with
   a real cycle id (today they are journal-only + overwritten). These four are what makes the
   476 recorded cycles replayable at interior granularity.

3. **The reactor (pure state machine):** the cycle becomes a `Step(event) → []action` machine
   over the 7 steps, mirroring `codexreactor`. The gate ladder (step 1) becomes explicit
   states; the terminal outcomes (`cycle_complete` / `cycle_aborted` / `clear_unconfirmed`)
   become terminal transitions.

4. **The load-bearing invariants (the "quality mechanism"), as property tests:**
   - **SR4 — `/clear` NEVER before model-done.** Today unimplemented (dossier 02 §1). This is
     step 4: introduce a real model-done signal (durable event) and assert the ordering. *This
     is the headline — the vertical closes a correctness gap that is invisible today, not just
     a refactor.*
   - **SR9 — bounded liveness:** every cycle reaches a terminal event within a bounded window
     or emits `restart_failed`; never silence. (This is the resume-hang class expressed as a
     keeper invariant.)
     > **SUPERSEDED BY P1:** SR9 / **SK-INV-005** is a KEEPER invariant only. The DAEMON
     > resume-hang (census 0a) is a distinct item, relocated to **M3-5** — do not conflate them.
   - **SR3** handoff-write-done before `/clear`; **SR6** brief only after new-session confirmed;
     **SR7** no overlapping restarts.

5. **Replay harness:** drive the reactor over the 476 recorded cycles + the four fault modes;
   assert the invariants. `clear_unconfirmed` (347 baseline occurrences, dossier 02 §5) is the
   first large real-world failure surface to characterize under replay.

6. **The template validation:** if `internal/substrate` (§3a) cannot host this vertical without
   codex-specific leakage, the extraction boundary is wrong — session-restart is the test of
   the abstraction, not just of the keeper.

---

## 6. Measurement — replay + fault-injection vs the frozen baseline

Not live A/B (too costly/high-variance). The baseline is frozen and verified:
`.harmonik/events/baseline-2026-07-13/` — **237,099 events**, run-completion **54%**
(1155/2142), restart-completion **84%** (427/507), **clear_unconfirmed 347** (dossier 04 §3,
synthesis doc 4). The standard of proof per vertical:
1. **Replay-regression:** old vs new logic over the *same* recorded streams (catches the
   hang/false-close/ordering class — they are logic bugs).
2. **Fault-injection pass-rate:** the four twin faults produce a terminal signal, never silence.
3. **N consecutive clean runs** + an **out-of-band** check (diff/coverage/filesystem — not the
   pipeline path under repair), per the census Acceptance Oracle.
4. **Longitudinal fleet trend** vs the 54% / 84% / 347 numbers once dogfooded.

---

## 7. Enforcement — turn on the levers (embed the principles)

The operator wants the principles enforced structurally. Several levers exist and are simply
off (dossier 06 §6). Cheap, mechanical, parallel (Track C):
- **Complexity ceiling:** enable `cyclop`/`funlen`/`gocognit` in `.golangci.yml`. Today
  `workloop.go` (8,184 lines) violates *nothing* because no complexity linter is configured.
  Set a ceiling; grandfather existing violations with `//nolint` + a tracking bead so *new*
  code is capped (the ratchet, not a big-bang cleanup).
- **Coverage floor** on carve targets: no coverage gate exists. Add a measured
  line/branch floor for any refactored run-lifecycle path (the census M1→M3 audit).
- **depguard ceiling on `daemon`:** it is allow-all with no deny (the sanctioned god-package);
  ~32/45 packages are unconstrained; several rules target packages that were never built
  (`orchestrator`, `policy`, `agentrunner`, …). As `internal/substrate`/`runexec` are
  extracted, give them real allow/deny edges so logic cannot leak back into daemon.
- **Typed-FP:** no `Result`/`Option` types today. *Not* proposing a monad library — the queue
  pattern (`(newState, events, error)` pure handlers) is the idiom to spread. Keep it Go-native.

---

## 8. Open decisions for the operator

1. **Confirm session-restart as the first vertical** (§5), with SR4 (the unimplemented
   model-done ordering) as the headline invariant — i.e. the proof *is* a real fix.
2. **Substrate genericization:** Go generics (`EventSource[E]`/`Effector[A]`) vs an `any`-typed
   boundary for `internal/substrate` (§3a). Recommend generics — one-method interfaces, low cost,
   keeps it typed.
3. **Dead typed-decode path** (§3d, area #9): adopt `DecodePayload`/schema-validate for the
   replay harness's invariant checks, or delete it and let the harness do ad-hoc unmarshal?
   Recommend adopt — it's exactly the type-safe replay decode we'd otherwise rebuild.
   > **SUPERSEDED BY P1:** resolved — **ADOPT ratified** (D6 / EV-048). `internal/replay` is a
   > live reader; the path is not dead. Decision closed.
4. **Lift the freeze for Track A + C** (ClockPort + interior events + substrate extraction +
   enforcement levers) — all low blast radius, single-writer, out-of-pipeline? Tracks B's
   data-integrity fixes too?
5. **Enforcement ratchet vs cleanup:** grandfather existing complexity violations and cap only
   new code (recommended), or budget a cleanup pass?
6. **Relationship to the census PLAN:** this plan re-frames census STEP-0/M1–M4 as
   abstraction-first tracks (mapping noted in §4). Supersede the census PLAN with this one, or
   keep both (this as the how, census as the diagnosis)?

---

## Appendix — source dossiers (the detail behind every claim)
- `research/01-daemon-godfunction.md` — beadRunOne / workLoopDeps / mergeMu anatomy
- `research/02-session-restart.md` — the keeper vertical, 7 steps, ClockPort gap, SR4 gap
- `research/03-codex-substrate-template.md` — the substrate template + generic/specific split
- `research/04-event-system.md` — envelope, emission, replay primitive, dark spots
- `research/05-io-boundaries.md` — tmux + SSH ack-free channels
- `research/06-architecture-and-fp.md` — package mass, depguard, queue island, enforcement gaps
