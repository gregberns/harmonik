# Bus Execution Plan — Changes From Review

What changed in `02-execution-plan.md` on 2026-09-07, and which finding each change answers. The review
is `03-review.md` (verdict REWORK). The reconciliation with the sibling restructure track is a
cross-plan decision recorded in both plans.

## Must-fix 1 — the layout deadlock (review finding 1, most severe)

The plan pinned its join point to `libs/transport`, `libs/cli.Env.Bus`, and `tools/keeper` — none of
which the restructure execution plan builds (it builds ONE `clean/` module and defers the `libs/` +
`tools/` split to END-STATE).

- Added the leading "Reconciliation with the restructure track — READ FIRST" section. Decision: the bus
  lands FIRST as packages INSIDE the clean module — `clean/transport` and `clean/cli` — and the
  `libs/` + `tools/keeper` split stays END-STATE, deferred.
- Retargeted every path: D1 (`clean/transport`), D4 (no domain noun in `clean/transport`), Phase 0
  scratch destination (`clean/transport`, not `libs/transport`), Phase 1 (`git mv → clean/transport`,
  add `clean/cli`, allow-list wall check), the §6 reviewer checks, and tasks T6–T8.
- Phase 1 now needs ONLY the restructure's Phase-0 scaffold (clean module + go.work + wall, restructure
  task 10). The bus builds `clean/cli` itself, so it does not wait on a `libs/cli` the restructure never
  plans — the deadlock is gone.

## Must-fix 2 — do not bet the first Service on keeper (review finding 2)

- Rewrote Phase 2 (§3) from "First real Service: keeper" to "Prove the seam on STUB services". The first
  cross-tool proof mounts two trivial stub/echo services on one in-mem bus — it depends on nothing from
  the keeper untangling.
- Moved keeper to a new later slice §3a, gated on a named trigger: the restructure's Track B has landed
  `clean/keeper` (severed `digest` + `dashboard` + `presence`, routed `time.Now`, ported the core
  types). Tasks T11–T12 are now the §3a slice, depending on restructure task 27.
- Was honest (in §3a and §6) that keeper's real surface is enable / doctor / set-dispatching, not the
  seed's invented `keeper.session.*` examples, so the keeper slice is packaging, not oversold as "two
  tools that needed to talk" (answers review finding 5 in passing).

## Must-fix 3 — hub/spoke distribution fully out of the first slice (review finding 3)

- D2 retitled and rewritten: hub/spoke distribution defers as ONE unit — point-to-point + State +
  Roster — with a single trigger (dispatch becomes a real bus service). Stated why the interim pull
  model violates locked C5 (a dead spoke strands its job, no durable lease) and C2 (build-then-rewrite
  is the temporary-pipe C2 bans).
- Rewrote §5 to describe the END-STATE mapping ONLY, marked deferred, with the interim pull /
  offer-claim models and their subjects removed. Added the "Why no interim" explanation citing C5 and
  C2. Stated plainly that §1–§3 build no hub/spoke distribution.
- §4 table: replaced the separate point-to-point and State rows with one coupled "Hub/spoke
  distribution = point-to-point + State + Roster (ONE unit)" row, and added a note that the three
  cannot land alone.

## Must-fix 4 — reconcile with existing comms (review finding 4)

- Added a tracked DEFERRED slice to the §4 table: "Reconcile / absorb `comms`" — re-express
  `harmonik comms` (then `dispatch`) as a bus Service; retire `daemon.SubscribeHub`, the `CursorStore`,
  and the daemon Unix-socket RPC; fold the `internal/eventbus` journal into the State seam. Named its
  dependency on the State seam first (comms' N3 at-least-once + `event_id` dedupe must rebuild on that
  journal).
- Added the "Honesty note — two comms systems until the reconcile slice lands": the live
  eventbus / `harmonik comms` / `SubscribeHub` path carries ALL real traffic; the new bus carries only
  stub-test and (later) keeper traffic until the reconcile slice fires. The split is a tracked deferral,
  not an accident.
- §5's "how dispatch works today" now says the old plane keeps carrying all real dispatch traffic until
  the reconcile slices land.

## Other review findings addressed while here

- Finding 5 (Phase 2 test barely more than Phase 0): resolved by making the seam proof explicitly a
  stub-service proof and dropping the "first cross-tool test" oversell; keeper §3a is named as
  packaging.
- Finding 7 (P1 guardrail home lost in the rename): D1 now notes the dep-allowlist / no-domain-noun
  guardrail machinery lives against `clean/transport`.
- Finding 6 (scratch-then-mv is ceremony): kept the scratch package for "start today" parallelism, and
  Phase 0 now says explicitly it stays put until the clean module exists, then moves into
  `clean/transport`.
