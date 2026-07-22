# 04-Design / 00b — PLANNER-RECONCILE registry + design-time corrections

> Items the design agent explicitly did NOT decide alone, plus premise corrections the
> planner must see. Referenced from 00-decisions (D7, D11).

## PLANNER-RECONCILE #1 — A11 "keeper migrates first" premise corrected (D7)

**Brief said:** the design MUST force "keeper migrates to the M2 driver FIRST" before any
deletion of the old input path (REVIEW-FINDINGS A11).

**Verified call graph says (seam-contract findings §2):** the keeper does NOT consume
`pasteinject.go` or the `internal/lifecycle/tmux` write verbs. It carries a private injector
(`internal/keeper/injector.go:154/158/189/204`, own `exec.CommandContext("tmux",...)`).
Deleting the entire M2 deletion set cannot break the keeper restart cycle at the file/symbol
level. Furthermore keeper targets interactive operator TUIs — migrating them to a headless
driver is a product-direction change, not an M2 gate.

**Design response (implements A11's intent, not its letter):** the deletion gate is a
**boundary audit** (seam-contract-design §4): grep-proven zero references per deleted symbol,
survivor list intact (keeper injector named survivor), keeper + daemon suites green, Stage-B
bake evidence. Keeper migration is explicitly OUT of scope (00-decisions D7, IN-007
carve-out).

**Planner action requested:** confirm the restated guard; if keeper-on-driver is desired as a
goal, file it as separate future work (it would also sweep crew mission paste, CLI boot-seed,
quiesce wake — the whole interactive-input family).

## PLANNER-RECONCILE #2 — M2-1 ↔ M3-4 contract ownership direction (D11)

**Brief said:** M3 OWNS defining the reactor Step's input/ack contract; M2-1 CONSUMES it.
**Ratified ROADMAP/A8 say the reverse:** "M3-4 (reactor Step) → M2-1 (seam input/ack
contract)" — M3-4 *needs* M2-1 (REVIEW-FINDINGS.md:25,27; ROADMAP.md:81-84).
**M3 state:** `2026-07-14-run-state-machine` is itself at decompose — no Step contract exists
to consume either way.

**Design response:** pinned the M2-produces reading (matches the ratified docs): the exported
surface is D1's `InputMsg`/`InputAck`/`ErrInputStale`/`ErrInputUnsupported` + D2's bounded
ack-or-stale + MsgID idempotency; M3-4 models `SubmitSeed`/`SubmitBrief` Actions and
`InputAcked`/`InputStale` Events over it. The surface is deliberately minimal so an M3-side
veto is a small diff, not a redesign.

**Planner action requested:** reconcile the ownership direction; hand the D1/D2 surface to
the M3 design pass as its input; route any M3 counter-proposal back before M2's T-seam task
freezes the handler types.

## Corrections recorded (no action needed, but they change published framings)

1. **SC3/SC4 scope narrowed by evidence:** "tmux write verbs retired" holds only for the
   bead-run input path. Survivors (all verified callers): keeper injector, crew mission paste
   (`crewstart.go:470-535`), CLI captain/crew boot-seed, `quiesce.go:730` wake, crew-stop
   `/quit`, `CapturePane`, spawn verbs, `SSHRunner`. `internal/lifecycle/tmux` loses only
   `SendKeysLiteral` (zero callers) in M2.
2. **C6 cannot physically complete inside M2** while the daemon is stopped: Stage-B bake (20
   live runs) necessarily post-DOGFOOD. M2 delivers delete-ready + gates; deletion commits
   are a pinned follow-through (D9).
3. **No normative stdin-ownership spec clause exists** — IN creates it rather than amending
   (seam-contract findings §7).
4. **The claude stream-json stdin surface is unproven in-tree** — the T0' spike gates the
   wire freeze; fallback (per-turn `-p --resume`) is pre-decided (D3).
5. **WAL-guard:** ADAPT/home, not delete — the ack cannot prevent SIGKILL-mid-flight;
   `codexwalguard.go` stays (D10).
