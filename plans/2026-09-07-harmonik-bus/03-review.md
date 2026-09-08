# Harmonik Bus — Execution Plan Review (adversarial)

**Reviewer pass:** 2026-09-07 · **Target:** `02-execution-plan.md`
**Grounding read:** `01-base-plan.md`, `00-seed-bus.md`, the sibling
`../2026-09-07-harmonik-restructure/{00-seed-playbook,01-base-plan,02-execution-plan}.md`,
`plans/2026-07-21-platform-architecture/DECISIONS.md` (C1–C6 locked),
`plans/2026-07-21-p1-kernel-fabric/_plan.md`, and the real `internal/eventbus/eventbus.go`.

## VERDICT: REWORK

The design is sound and honors the locked C1–C6. But the execution plan's entire join point
(Phase 1 and Phase 2) is pinned to a target layout the sibling restructure track **abandoned in
its own execution plan**. As written the two "Track 2 of 2" siblings deadlock: the bus waits on
`libs/transport`, `libs/cli.Env.Bus`, and a bus-carrying keeper port that the restructure
execution plan does not build. This is fixable, but not by editing during — the dependency
contract has to be renegotiated with the restructure track before any code starts.

---

## MUST-FIX BEFORE STARTING

### 1. The restructure delivers `clean/`, not `libs/` — every Phase 1/2 dependency is against a layout that no longer exists (most severe)

The bus plan builds its join point (§2, §3, tasks T6–T9) entirely on the restructure's **seed**
(`00-seed-playbook.md`): `libs/transport`, `libs/cli` with `Env.Bus transport.Bus`, `tools/keeper`
as library + thin main. Its Appendix source map cites the seed, never the restructure's
**execution** plan.

The restructure's execution plan superseded that layout:
- Its module is **`clean/`** (`clean/go.mod` = `…/harmonik/clean`), with `clean/eventbus`,
  `clean/queue`, `clean/keeper`. There is **no `libs/` anywhere** — grep of restructure
  `01-base-plan.md` and `02-execution-plan.md` returns zero `libs/`, zero `transport`, zero
  `Env.Bus`, zero `cli.Env`. Those tokens survive only in the restructure **seed** and README.
- There is **no `libs/cli` and no `Env.Bus` composition seam** in the restructure task list at
  all. The restructure never plans to build the seam bus-T8 depends on.
- Verified against the tree: no `libs/`, no `go.work`, no `tools/keeper` (only lint helpers in
  `tools/`). The restructure Phase 0 is not landed.

Consequences, task by task:
- **T7** `git mv internal/transportproto → libs/transport` moves into a directory the sibling
  track never creates.
- **T8** "Depends on: restructure `libs/cli`" — deadlock. That deliverable is not in the
  restructure plan.
- **T9** "rides restructure keeper Track B — the same port, carrying the bus." Restructure Track B
  never mentions the bus, `transport`, or `Env.Bus`, and lands `clean/keeper`, not `tools/keeper`
  as the domain library the bus assumes.

**Fix:** reconcile the two siblings against ONE layout before starting. Either (a) the restructure
adopts `libs/` + `libs/cli.Env.Bus` back into its execution plan and task list, or (b) the bus
retargets to `clean/transport` and the composition seam is renegotiated (the restructure has no
`cli.Env`, so where the umbrella injects `transport.InMem()` must be named). Until one layout is
agreed and written into BOTH execution plans, T6–T9 cannot be scheduled.

### 2. "Keeper carries the bus in one turn" collides with the restructure's finding that keeper is one of its hardest, latest ports

Base plan §5 "Recommended coupling" and bus Phase 2 bundle six hard things into keeper's port:
land `libs/transport` (T7), wire `libs/cli.Env.Bus` (T8), port keeper domain lib (T9), the
`NewService` adapter (T10), dual-binary mount (T11), cross-tool test (T12) — asserting keeper is a
"clean leaf, small blast radius."

The restructure execution plan says the opposite, with evidence (§Track B, §6): keeper is **NOT
clean today** and "earns its move by cuts, not relocation." Its clean move (restructure task 23,
size **L**) is the *culmination* of a five-step Track B that first severs the `digest` tentacle
(task 19, **L** — `digest` alone drags in "crew, queue, dispatch, run, sentinel, eventbus,
lifecycle/tmux, presence — the whole run machine"), then `dashboard` (task 20), routes `time.Now`
(21), and ports keeper-relevant core types (task 22) — which itself is gated on the Phase-1
core-vocab spike (task 12). Keeper is explicitly **not** an early port; `substrate` is Port 0 and
keeper trails the entire spine.

So the bus's only real Service is coupled to the single messiest keeper refactor in the sibling
program. If restructure Track B slips or the vocab spike returns RED, the bus reaches T12 with **no
real service at all**. **Fix:** decouple. Prove the bus with a genuinely dependency-free first
Service (or a dedicated trivial one) that does not wait on the keeper untangling, and let keeper
become a bus Service *after* Track B lands it in clean — not as the vehicle that lands the bus.

### 3. The interim hub/spoke pull model (§5) cannot satisfy locked C5, and building it then rewriting it violates locked C2

§5 recommends modeling real hub/spoke on the three verbs today: "a spoke `Request`s work and the
hub `Serve`s exactly one job in reply … no double-delivery, because the hub is the single
authoritative queue owner (C3)." Three defects:

- **Job-taken-but-spoke-dies breaks C5.** DECISIONS C5 is a *non-skippable minimum*: a dead/hung
  worker's in-flight bead must not strand — heartbeat/timeout requeues it. The interim pull model
  has no durable lease: the hub hands out job X over an at-most-once bus (D3), the spoke dies, and
  nothing requeues it. The plan itself admits (§5 "When point-to-point lands") that the durable
  lease lives in `Resources.State()` — the **deferred** state seam (§4) — and worker liveness needs
  `Roster` — **also deferred**. So the interim is only safe single-process/in-memory, where a spoke
  cannot independently die. The moment hub/spoke is real, **three deferrals (point-to-point + State
  + Roster) fire together**; the §4 table presents them as independently-triggered, which understates
  the coupling and hides that none of them can land alone.
- **Hub-has-no-job is unspecified.** With `Request`/`Serve`, a spoke asking when the queue is empty
  forces either long-poll (hub parks open requests — real, unbuilt machinery) or return-empty-and-
  retry (polling latency). The plan names neither.
- **The rewrite violates C2.** The plan claims point-to-point is "additive, not a rewrite." That is
  true of the *Bus interface* (a new optional `Subscribe(pattern, group)` verb). It is false of the
  *dispatch service*: pull (`spoke Requests`, `hub Serves one`) and competing-consumers (`hub
  Publishes groupKey`, `spokes Subscribe(group)`) are different control flows — the interim
  dispatcher is throwaway. Building the interim and later rewriting it to point-to-point is exactly
  the "temporary pipe, migrate later" that C2 forbids ("no hacks in either P1 or P3 … that is
  exactly how the ssh model happened").

**Fix:** do not recommend building a real hub/spoke dispatcher on the interim. State plainly that
hub/spoke is out of this slice and lands when point-to-point + State + Roster land **together** (one
coupled trigger, per P1 §4), and keep §5 as forward design, not a "works today" path a crew will
build and then throw away.

---

## FIX DURING

### 4. The execution plan reconciles nothing — it ships a second comms system carrying zero production traffic, and does not schedule the retirement

The base plan §1 states the real end goal: re-express `comms` (then `dispatch`) as a `Service` on
the bus, "with today's daemon-socket RPC and `SubscribeHub` retired into the bus," and eventbus's
journal becoming the State-seam implementation. That reconciliation is asserted in prose and then
**dropped from every task**. T1–T12 stop at keeper. There is no task to port comms, retire
`daemon.SubscribeHub`/the daemon socket, or fold the eventbus journal into `State` — and the §4
deferred-slice table (NATS / registration / roster / point-to-point / state) does **not** list
"port comms / retire SubscribeHub" at all, so it is not even a tracked deferral with a trigger.

Standing up a NEW in-memory subject router is the right call — the base plan §1 is honest that the
real `internal/eventbus` (`Subscribe` is boot-only, sealed post-`Seal()` per EV-009, type-keyed by
`core.EventPattern`, no request/reply — confirmed in `eventbus.go`) genuinely cannot be extended
into the bus. But at T12-done, harmonik runs BOTH systems: the live eventbus/comms/SubscribeHub
plane carrying *all* real agent traffic, and the new bus carrying *one synthetic keeper request in a
test*. The plan does nothing within its own scope to close that split and does not name it as a
deferred slice. **Fix:** add the comms-port + SubscribeHub-retirement as an explicit deferred slice
in §4 with its trigger, and note that porting comms depends on the State seam first (comms' N3
at-least-once + `event_id` dedupe must rebuild on a journal the deferred State seam owns) — another
coupling the plan currently hides.

### 5. Phase 2's "first cross-tool test" is barely more than Phase 0's cross-service test

Phase 0 exit gate #3 already mounts "two trivial services … one `Request`s a subject the other
serves." Phase 2 exit gate #3 mounts "two real services … exchange a request/reply and an event."
The only delta is that one trivial stub now wears keeper's name — keeper has no natural request/reply
counterpart (its real job is a tmux paste + `/clear` + `/session-resume`, an effectful local action,
not a subject someone `Request`s), so the "second participant" in T12 is still a synthetic stub. The
subjects `keeper.session.restart` / `keeper.session.grew` are the seed's *invented* examples, not
keeper's real surface (the keeper skill is enable/doctor/set-dispatching). So T12 proves the router
(already proven in Phase 0) plus keeper packaging — not "two tools that needed to talk now talk."
**Fix:** either pick a first Service whose subjects are real, or drop the "first cross-tool test"
framing to "keeper packaged as a Service" so the proof is not oversold.

### 6. `internal/transportproto` scratch-then-`git mv` is avoidable ceremony

The bus is domain-blind (`[]byte` only, imports nothing legacy — base plan §5 says it "depends on
no legacy code"). A package with zero legacy dependency has no reason to be born under `internal/`
and moved: once the clean module exists (which T6 requires anyway), it can be born directly in
`clean/transport` (or `libs/transport`), skipping T7's mv + import-rewrite. The only argument for the
scratch package is "start today before the clean room" — defensible for parallelism, but then say so
and keep the destination name aligned with finding #1. As written, the mv is cheap only if the
destination (`libs/`) and the seam (`cli.Env.Bus`) exist, and per finding #1 neither is delivered.

### 7. Package-home divergence from P1 is flagged but under-stated

P1 (`_plan.md` §5, §7) puts the fabric at `internal/kernel/`, dep-allowlisted, out of
`internal/daemon`. The bus plan moves it to `libs/transport` and renames kernel→Bus (D1). This does
not violate a *locked* C-decision (C1–C6 name no package), and D1 flags it operator-overridable, so
it is honest. But D1 undersells that it also drops P1's dep-allowlist/vocabulary-test *home*
(`internal/kernel`) — the guardrail machinery in P1 §5 is written against that package. Note where
the dep-allowlist and no-domain-noun tests live in the new home so the D4 guardrail does not
evaporate in the rename.

---

## What the plan gets right (so rework does not throw it away)

- **C1 / git-as-artifact-plane / `[]byte`-only / 256 KB ceiling** — honored (§3.7, D4, T1).
- **C6 in-proc plugin boundary, never Go `plugin`** — honored (Service is in-proc).
- **At-most-once by contract (D3)** — correctly matches P1 §1 "not a delivery guarantor" and is
  stated honestly as a service concern.
- **The three-verbs-first, point-to-point-as-optional-richer-interface shape** — the right
  first-surface call, consistent with the seed and P1, *provided* finding #3 is addressed.
- The base-plan grounding is unusually honest about the real eventbus limits; that honesty just did
  not survive into the task list (findings #3, #4).

---

## The 3 things most likely to make this plan fail

1. **The layout deadlock (finding #1).** The bus is pinned to `libs/transport` + `libs/cli.Env.Bus`;
   the sibling restructure builds `clean/` and no `cli.Env` seam. Nothing in either task list closes
   the gap, so Phase 1 starts by waiting on artifacts that will never be produced.
2. **Betting the bus's only real Service on the hardest keeper refactor (finding #2).** Keeper's
   clean move is the late, L-sized, digest+dashboard-severing, spike-gated tail of the restructure —
   not the clean leaf the bus assumes. If it slips or goes RED, the bus has no real service.
3. **Hub/spoke pulled forward the moment it is wanted, dragging three coupled deferrals and a
   C2-violating rewrite (finding #3).** The operator's actual model (hub exposes jobs, spoke picks
   them up — the seed's reason to exist) is unbuildable on the interim without point-to-point +
   State + Roster together, and building the interim first is the "temporary pipe" C2 bans.
