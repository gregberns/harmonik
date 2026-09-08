# Operator amendments — 2026-09-07

Three directives from the operator this session. They amend the locked platform decisions
(`../2026-07-21-platform-architecture/DECISIONS.md`) and the vetting verdict
(`06-vetting-ledger.md`). Recorded here so they are not lost and not re-litigated.

## A1 — REOPEN C6: subprocess plugins, not in-proc. Live-reload is the top priority.

**Decision:** revert C6. Plugins are **separate processes** (the original substrate-v2 model:
`hashicorp/go-plugin`, gRPC over a local unix socket), **not** in-proc Go libraries.

**Why (operator, verbatim intent):** "I'd prefer that live-reload be possible. Every time we
want to change something I don't want to have to take the whole system down. My top priority
would be live-reload, which seems like subprocess wins there — and we then get stability too."

**What the evidence says (substrate-v2 §7, measured):**
- Live-reload = kill the child, exec the new binary; the daemon never stops. **Warm reload
  3–4 ms (linux) / 7–11 ms (darwin)**; the one-time macOS signature check (~500 ms) moves off
  the reload path by pre-warming at install.
- **Crash isolation for free** — separate address spaces, CPU-enforced. A plugin panic is an
  ordinary `error` at the call site. The vetting flagged that in-proc (C6) quietly *lost* this;
  subprocess gets it back.
- Cost, and it is real but bounded: a **~40-line kernel-side drain gate** (go-plugin's own
  `GracefulStop` is broken — measured; a 3s call was killed at 504 ms without it), the
  **stateless-plugin rule** (a plugin holds no in-memory state across a reload — state lives in
  the kernel; four independent derivations agree), and **cross-compile discipline**
  (`CGO_ENABLED=0` for the dgx). None of these is new — substrate-v2 designed all three.

**Consequence for the design of record:** the plugin↔kernel boundary is now a **wire contract**
(the reconciled `fleet/kernel/v1/kernel.proto` + `plugin.proto`), not P1's in-proc Go
interface. This makes **raw substrate-v2 the primary reference again on the plugin-model axis** —
its plugin-system design (`design/22-plugin-system.md`), drain gate, and reload measurements all
assume subprocess. P1's SCOPE discipline and DROP list still hold; only the C6 recast is undone.

**What this does NOT reopen:** the kernel/plugin boundary (opaque `[]byte`, no domain nouns),
the four channel types, roster/lookup/state, at-most-once, the DROP list (no search backbone).
Those were premise-independent and stay.

**Flag for the operator to veto if wrong:** subprocess means more deploy artifacts (one binary
per plugin per platform) and a real wire boundary to version. That is the price of live-reload +
isolation. If artifact count ever becomes the bigger pain than downtime, C6 reopens again — but
per the stated priority, downtime is the pain, so subprocess wins.

## A2 — Verification is built in from day one. Unit tests do not prove a comms system works.

**Directive (operator):** "Rigorous verification built in. Running unit tests is not acceptable
to say a comms system like this is working. I'd prefer a smaller part of the system be built,
then testing/validation/verification built around it — tests used from day one to ensure the
system is robust."

**How this shapes the plan:**
- **Build-a-slice-then-harness-it, in that order.** Each slice ships with its own
  fault-injection verification, not just unit tests, before the next slice starts.
- **The verification classes a comms/kernel substrate actually needs** (substrate-v2 already
  built prototypes of several — reuse them, do not re-invent):
  - **Partition / kill tests** — kill 1 and 2 of 3 nodes; the survivor must still serve
    (measured once as a throwaway; make it a standing test).
  - **The blackhole / real-sleep test (M0)** — half-open TCP where bytes vanish but sockets stay
    `ESTABLISHED`; then a *real* `pmset sleep` overnight. This is the #1 unproven risk.
  - **Message-loss-under-partition** — publish to a sleeping box; prove store-and-forward keeps
    it and the drainer replays on wake with dedupe (no loss, no dupes).
  - **Reload-under-load** — 100 msg/s through a plugin while it hot-reloads; assert **zero loss**
    and the daemon PID never changes. This is the drain-gate test and the live-reload proof.
  - **Load / backpressure** — a slow consumer must not block the producer; drops counted and
    reported, never hidden.
- **Owner:** this is the assessor's kind of work (`roles/assessor/`) — spin the real thing up and
  run real traffic through it, extend a live failure corpus. A passing unit suite is explicitly
  not the bar. See the detailed plan in `08-verification-first-plan.md`.

## A3 — Segment the code. Do not drop moved code into one flat library folder.

**Directive (operator):** "All the code that goes into this should be somewhat segmented off
from other parts of the codebase. I don't want just a massive folder of libraries where kernel
libraries are mixed in with keeper libraries. There may be shared libs across all parts. Plan
logically so that as we move code from the 'not-clean' code we don't just drop it all into the
same place."

**How this shapes the plan (detailed layout in the restructure track's
`06-segmented-layout.md`):**
- Clear top-level segmentation by OWNER, not one flat `clean/` or `libs/` bucket:
  - **the kernel** (the substrate) — its own segment, its own module boundary.
  - **each tool** (keeper, dispatch, comms, …) — its own segment; a tool's library never mixes
    with another tool's.
  - **genuinely shared** vocabulary/utilities — a small, explicitly-shared segment that anyone
    may import, with a rule for what earns a place in it (a real second consumer, not "might be
    reused").
- **A placement rule for moved code**, so extraction has a destination decided *before* the
  move: every package pulled out of the not-clean daemon is assigned an owner segment up front;
  "drop it in the shared pile" is not an allowed default.
- This is the segmentation the substrate-v2 target layout (`libs/ · tools/ · cmd/`) and the
  restructure seed both point at; A3 makes it a hard planning constraint, not an aspiration.

## Program-framing question — still open, now partly answered by action

The operator has started **amending locked decisions (C6)** and adding directives (A2, A3),
which is the behaviour of a **re-grounding**, not a from-scratch supersession. Proceeding on that
reading: revive the P1/P2/P3 intent, apply A1–A3 as amendments, re-ratify the amended set. If the
operator instead wants a clean supersession, only the provenance changes, not A1–A3.

**Caveat discovered 2026-09-07 while checking doc accuracy:** the P1/P3 (kernel/dispatch) half was
never superseded and revives cleanly. The P2 (extraction) half WAS superseded on 2026-07-27 by the
active `delete-and-rewrite` program (STATUS.md), which decomposes the same daemon/core **in place**
rather than into segment modules. So the restructure half is not a clean "revive P2" — it collides
with an active program, and the operator must decide replace / follow / alongside. See the
restructure track's `05-relation-to-active-program.md`. This does not touch the bus (kernel) track.
