# Verification-first plan — kernel + comms substrate

**Date:** 2026-09-07
**Answers:** operator amendment A2 (`07-operator-amendments-2026-09-07.md`) and the live-reload amendment A1.
**Companion to:** the substrate-v2 `ROADMAP.md` (the build order) and `ARCHITECTURE.md` (the design).

## The one rule this document exists to enforce

A green unit suite does not prove a comms substrate works. The operator said it plainly (A2):
*"Running unit tests is not acceptable to say a comms system like this is working."* So the bar is
not a passing suite. The bar is **fault injection against a running system** — partitions, kills,
real sleeps, reloads under load, and slow consumers — with the system built one small slice at a
time and the verification built around each slice **before the next slice starts**.

A unit suite is necessary and never sufficient. It is the fast inner loop that keeps a slice from
regressing between fault-injection runs. It is not the thing that says the slice is done. This
document states, for each slice, what "done" means in terms of a fault survived, not a test passed.

The substrate-v2 investigation already built throwaway harnesses that inject most of these faults.
They are listed in `design/21-transport.md` §Sources and in `ROADMAP.md` M0. **Reuse them. Do not
re-invent them.** The design docs are their specification; the `/tmp/sleeplab/` copies are gone, so
each standing test is rebuilt from the documented behavior of its seed harness.

---

## 1. The verification classes

Each class is a standing test. It asserts one property, injects one fault, and has one pass/fail
bar. The right-hand column names the substrate-v2 throwaway harness it graduates from — the seed
whose behavior the standing test reproduces and then keeps.

| # | Class | Asserts | Fault injected | Pass / fail bar | Graduates from |
|---|---|---|---|---|---|
| VC-1 | **Kill, 1 of N** | The survivors still serve their own agents; the dead box's agents are the only thing lost. | `SIGKILL` one `fleetd`. | Every survivor answers publish / request locally. A survivor that hangs or errors is a FAIL. | `driver/main.go` (real DGX kill, 9.27 ms survivor RTT) |
| VC-2 | **Kill, 2 of N (lone survivor)** | One box alone does everything; a cold box boots alone. | Kill both peers; then cold-start a third with both peers dead. | Lone box serves req/reply and pubsub; cold boot < ~50 ms. No election, no wait. | `lone/main.go` (28 ms cold start, both peers dead) |
| VC-3 | **Blackhole (half-open partition)** | Survivors are unaffected during a half-open partition; the slept box self-rejoins on wake. | A proxy holds sockets `ESTABLISHED` and discards bytes. | Survivor traffic uninterrupted for the whole partition; rejoin ≤ ~1 s, no operator action. | `main.go` (blackhole proxy, 90 s partition, 0.8 s rejoin) |
| VC-4 | **Real macOS sleep (M0)** | The mesh re-forms after a real lid close, with no restart. | `pmset sleepnow` on the laptop; wait overnight; open the lid. | Mesh re-forms unprompted. Detection and rejoin times inside the roster thresholds. **Mesh does not re-form → project stop (M0 kill criterion).** | the M0 harness (`ROADMAP.md` M0) — the one seed not yet built |
| VC-5 | **Interest staleness** | `no responders` is a hint, never the ACK; the stale window is bounded. | Sleep the receiver via the blackhole; publish from a peer. | With `PingInterval=2s` the window is ≤ ~5 s, not 61 s. Any code path that treats `no responders` as delivery is a FAIL. | `window/main.go` (61 s default → 5 s tuned) |
| VC-6 | **Message loss under partition (store-and-forward)** | Nothing is lost when the receiver is asleep; the sender's log drains on wake. | Publish to a sleeping box, then wake it. | Every message is on the sender's disk while asleep and delivered after wake. One lost message is a FAIL. | `durable/main.go` phases 1–3 |
| VC-7 | **Duplicate delivery / dedupe-on-id** | At-least-once plus dedupe-on-id equals effectively-once. | Re-ship a message the receiver already holds (`crash-before-ack`). | The receiver ACKs again and the inbox still holds exactly one copy. A duplicate record is a FAIL. | `durable/main.go` phase 4 |
| VC-8 | **Crash mid-ack** | A receiver that dies after writing but before ACKing loses nothing. | Kill the receiver between its disk write and its ACK. | The sender retries; the id dedupes; the inbox holds exactly one copy. | `durable/main.go` (rule 2, made a standing kill) |
| VC-9 | **Cursor correctness** | A reader advances monotonically, never replays, never skips. | Read to a cursor, re-read, restart the reader, re-read. | A re-read after the cursor returns zero new records. `SetCursor` refuses to move backwards. | `durable/main.go` phase 5 |
| VC-10 | **Per-node ordering under skew** | `node_seq` is monotonic within one node; no cross-node order is claimed. | Publish interleaved from three boxes with skewed clocks. | Within a node, `origin_seq` is strictly increasing. No API returns a cross-node total order. Sorting by `wall_time` is display only. | `menu/main.go` + `durable/main.go` (the `node_seq` claim) |
| VC-11 | **Load / backpressure (slow consumer)** | A slow consumer never blocks the producer; drops are counted, never hidden. | Attach a consumer that reads far slower than the producer publishes. | Producer throughput is flat. The slow consumer gets a `subscription_gap` with an accurate drop count. A blocked producer or a silent drop is a FAIL. | harmonik's `SubscribeHub` drop-oldest+count contract (ported, §21 §4) |
| VC-12 | **Reload under load — the drain gate** ⭐ | A plugin hot-reloads under traffic with zero message loss, and `fleetd` never restarts. | Publish 100 msg/s; `fleetctl plugin reload <plugin>` mid-stream. | Journal count after reload equals messages sent, exactly. `fleetd` PID unchanged. Reload completes in single-digit ms. **One lost message is a hard stop** (`ROADMAP.md` M1 kill criterion). | new for A1; the drain gate is the ~40-line kernel-side gate substrate-v2 measured as required |
| VC-13 | **Channel conformance** | All four channel types behave to contract across boxes. | Exercise PUBSUB, POINT_TO_POINT, REQUEST_REPLY, LOOKUP across a 3-node mesh. | Each type meets its contract (copy-to-all, exactly-one, correlated reply, all-claimants-returned). | `menu/main.go` (all five menu items, JetStream off) |
| VC-14 | **Boundary / vocabulary** | The kernel names no domain concept. | Static: `grep -riE 'echo\|agent\|tmux\|claude\|ssh' internal/kernel/`. | Zero hits. Verify the artifact, not the tool exit code (`ARCHITECTURE.md` §12). | `20-kernel-boundary-test.sh` |

VC-12 is the headline. A1 makes live-reload the top priority, and the reload-under-load-zero-loss
test is its proof. Its failure is not a bug to file — it is a stop, because every other slice
assumes the drain gate holds.

VC-4, VC-8, VC-10 are the three that test a claim the design **deduced but never measured**. They
are called out again in §3, because the ground under them is not real yet.

---

## 2. Slice by slice — build a slice, then wrap it, then move on

Each slice ships with its own fault-injection gate. The gate is a set of the VC classes above,
run against a live scratch daemon, before the next slice is allowed to start. The slices track
`ROADMAP.md` M0–M4, recut so the verification grows with the system.

### Slice 0 — M0 measurement (no product code)

**Build:** the M0 harness — an embedded core NATS mesh plus a 1 s TCP probe loop on three boxes
(`ROADMAP.md` M0).
**Verify:** VC-4. Close the laptop lid for real, wait overnight, read the four-number table off the
dgx log.
**Verified gate:** the mesh re-forms on wake with no restart, and the detection numbers sit inside
the roster thresholds. **If the mesh does not re-form → stop.** No product code is written until
this passes. This is the only measurement on the critical path before code.

### Slice 1 — in-memory kernel + one trivial plugin, one box

**Build:** `fleetd` with the four skeleton RPCs (`Publish`, `Subscribe`, `JournalAppend`, `Info`),
in-memory transport, SQLite journal, and the go-plugin host **with the drain gate** (A1: plugins
are subprocesses). One plugin: `echo`, ~80 lines, which journals every message it sees.
**Verify:** VC-13 (channel conformance, single box), VC-14 (boundary grep), and the split demo —
publish a byte, read it back from the journal, prove the kernel never parsed it.
**Verified gate:** `echo` runs as a child process; the boundary grep returns zero hits; `fleetd`
stays under the ~2,000-line skeleton budget. If `echo` needs any RPC outside the 19, stop and re-cut
the API.

### Slice 1b — reload under load (the A1 gate)

**Build:** nothing new — this slice exercises Slice 1's drain gate.
**Verify:** VC-12. Publish 100 msg/s at `echo` and `fleetctl plugin reload echo` mid-stream.
**Verified gate:** journal count equals messages sent, exactly; `fleetd` PID unchanged; reload in
single-digit ms. **A single lost message is a hard stop.** This is the money demo (`ROADMAP.md`
§2.2 command 2) turned into a standing test. It runs on every slice from here on, because every
later plugin must survive its own reload.

### Slice 2 — the split across two boxes

**Build:** the same kernel on a second box; the mesh from a config file; `PingInterval=2s`.
**Verify:** VC-1, VC-2, VC-3. Kill one box, then two, then cold-boot a third alone. Blackhole the
laptop.
**Verified gate:** survivors serve throughout every kill and the blackhole; a cold box boots alone;
the slept box rejoins on wake. Re-run VC-12 across the boundary — the plugin on the far box must
still reload losslessly.

### Slice 3 — the roster

**Build:** the probe loop, `ALIVE/SUSPECT/DEAD/UNKNOWN` as pure functions of a local counter,
`boot_id`, `RosterWatch`, the sleep-announce hook (`ROADMAP.md` M2).
**Verify:** VC-5 (interest staleness bounded to ~5 s), plus a re-run of VC-3 asserting the roster
state transitions match the M0 numbers.
**Verified gate:** the stale window is ≤ ~5 s; `DEAD(ANNOUNCED_SLEEP)` shows in ~0.6 s on a lid
close and `DEAD(PROBE_TIMEOUT)` in ~6.5 s on a `-9`. If the M0 numbers contradict the thresholds,
retune here, before comms depends on them.

### Slice 4 — LOOKUP

**Build:** `LookupPut/Get/List`, single-writer-per-key, `(writer_node, boot_id, revision)` identity,
the revision counter persisted in SQLite (`ROADMAP.md` M3).
**Verify:** the **disk-wipe convergence** experiment (§3, claim B) and the name-clash demo
(two claimants, kernel picks neither).
**Verified gate:** after a node's disk is wiped and it restarts, no peer converges on a wrong value,
because the fresh `boot_id` forces re-convergence. A clash returns two entries and the kernel picks
neither. This is the riskiest code in the project — the only component with no upstream — so its
gate is an experiment, not a unit test.

### Slice 5 — the comms plugin (harmonik migrates here)

**Build:** the comms plugin — outbox/inbox, store-and-forward, retry, dedupe, the end-to-end ACK,
cursors in `KVPut` not a file. Port `jsonlwriter.go`'s batching drainer and `SubscribeHub`'s
drop-oldest contract; delete `commscursor.go` and `fsyncBoundaryEventTypes` (`ROADMAP.md` M4).
**Verify:** VC-6, VC-7, VC-8, VC-9, VC-11, and VC-12 re-run against comms (a real, stateful plugin).
**Verified gate:** a message to a sleeping box is queued on disk and delivered on wake, with no loss
and no dupes; the cursor never replays; the slow consumer never blocks the producer; and comms
hot-reloads mid-conversation with the conversation none the wiser and `fleetd`'s PID unchanged.
If comms needs a kernel RPC outside the 19, or needs to hold in-memory state across a reload, stop —
the boundary or the stateless-plugin rule is wrong, and this is where A1's live-reload promise is
truly tested.

The pattern is the same at every slice: the fault-injection harness exists and passes **before** the
next slice's code is written. Verification is not a phase at the end; it is the gate between slices.

---

## 3. Deduced-but-unbuilt claims that must become standing tests

`ARCHITECTURE.md` §11 ranks the open questions. Three of them are load-bearing claims the design
labels `[DEDUCED]` and nobody has measured. Until each has run as a live experiment, the ground
under the slice that depends on it is not real. Each becomes a standing test, and the slice that
depends on it does not pass its gate until the experiment has run.

**Claim A — a real macOS sleep behaves like the blackhole proxy (§11 q1 → VC-4).**
The blackhole models a sleeping laptop as "sockets stay open, bytes vanish." Nobody has closed a
real lid. The unknown is whether macOS sends RSTs on wake (faster detection than modelled) or leaves
peers hanging (matches the model). This is Slice 0's whole job, and it gates everything: it is the
top risk in three separate designs and it is the M0 kill criterion.

**Claim B — LOOKUP converges after a disk wipe (§11 q2 → Slice 4 gate).**
Read-repair is sound only if a writer never re-issues a `(writer, revision)` pair with a different
value. That needs the revision counter to survive a restart **and** a disk wipe to force a fresh
incarnation. The mitigation — adopt the roster's `boot_id` as the incarnation — is specified and
unbuilt. The experiment: wipe a node's disk, restart it, and prove peers do not converge on the old
counter's stale values. This must run before any plugin trusts LOOKUP for name resolution.

**Claim C — per-node ordering is a sufficient causality story (§11 q9 → VC-10).**
`node_seq` monotonicity is deduced by two authors and measured by neither; the doc says plainly
*"independent agreement is not measurement."* The experiment: publish interleaved from three boxes
with real clock skew, and assert within-node order holds while no API offers a cross-node order.
Measure the actual skew between the three boxes at the same time (§11 q6), so the "wall clocks must
not order messages" rule rests on a number, not an assertion.

These three are the "the ground is not real yet" items. A standing test that has run and held is the
only thing that converts a `[DEDUCED]` label into a trusted claim.

---

## 4. Who owns this — the assessor role

This work is the assessor's kind of work, and A2 says so. The assessor already exists to *"spin the
process up and run real work through it"* (`roles/assessor/soul.md`), already runs a live process no
other agent produces (LT and XT legs, `roles/assessor/operating.md`), and already owns a live
failure corpus. **Do not invent a new role. Fit the substrate verification into the assessor's
existing legs.**

- **The fault-injection classes are the XT leg (exploratory break-testing).** VC-1 through VC-13 are
  adversarial angles against a live scratch daemon — exactly what XT is. VC-14 and the unit suite
  are CR/MG-adjacent.
- **Results live in a dated `assessments/` folder**, written as the run happens, one folder per gate
  (`assessments/README.md`). The mission, the evidence (one row per run, with the pinned commit read
  from inside the log), the findings, the verdict. A record assembled after the verdict proves
  nothing.
- **A failure becomes a standing regression test in `test/exploratory/cases/`.** That directory is
  the assessor's corpus. Each of VC-1 through VC-14 is a case there, in the file format the corpus
  defines (`test/exploratory/cases/README.md`): a `protocol` case for the ones that need a live run
  and an observation method (the sleep, the reload-under-load, the disk-wipe), a `probe` case for
  the ones that reduce to a command and an exit code (the boundary grep, the cursor refusal). The
  corpus README's warning applies: a library of only `probe` cases decays into a regression suite
  that finds nothing new, so the sleep, the blackhole, and the reload stay as protocols.
- **Every defect found gets a `found-by:assessor` bead and a case**, so it is replayable forever. A
  finding that cannot be re-run is a story, not a test.

The assessor runs the scratch daemon pinned to the commit under audit, never the live fleet, exactly
as the merge gate already does (`scripts/scratch-daemon.sh init … --rev`). The substrate verification
adds harnesses to the XT leg; it does not add a process.

---

## 5. What "done / robust" means for a slice

A slice is done when its fault-injection gate holds on a live scratch daemon, pinned to the slice's
commit. Concretely:

- **Slice 1b / VC-12 is done when** 100 msg/s survive a reload with a journal count equal to the
  send count, exactly, and `fleetd`'s PID is unchanged. Not when a unit test of the drain gate
  passes.
- **Slice 5 / comms is done when** a message to a sleeping box is queued, delivered on wake, and
  never duplicated, across a real kill-and-wake — and comms reloads mid-conversation. Not when the
  comms package is green.

State it plainly, because it is the operator's directive: **a passing unit suite is necessary and
never sufficient.** The unit suite is the fast loop that keeps a slice from regressing between
fault-injection runs. The fault-injection gate is what says the slice is robust. The record backs
this: every expensive finding this project has produced — the 79-minute silent stall, the two
harnesses scored as failures — came from watching a live process, and every one of them passed the
tests that existed (`roles/assessor/operating.md` §What only I can do).

A slice that cannot survive its fault is not done, no matter how green its suite. A slice whose fault
is a project kill criterion (VC-4 mesh re-form, VC-12 zero loss, VC-6 no message loss) does not
degrade to a warning — it stops the work until it holds.

---

## 6. CI versus slow-chaos — keep rigor without wrecking iteration

Two loops, kept separate on purpose, so the fault-injection rigor does not slow the inner loop.

**The fast loop — `make fast`, the per-package unit suite.** Runs on every change, in seconds to a
couple of minutes. It is the necessary-not-sufficient floor. It never spins a daemon, never sleeps a
box, never reloads a plugin. It catches a regression between fault-injection runs; it does not claim
the system works.

**The slow-chaos loop — the fault-injection suite, build-tagged.** Guard it with `//go:build chaos`
so it never runs in the fast loop and never lands in `make fast` or the default `go test ./...`. It
needs a live scratch daemon, real subprocess plugins, real kills, and — for VC-4 — a real overnight
sleep, so it is minutes to a whole night, not seconds. Give it its own target, `make chaos`, run at
two moments and only two:

1. **At each slice gate**, before the next slice's code is written. This is the "verified" gate of
   §2, and it is the assessor's XT leg.
2. **Before release**, as part of the deploy gate. CI-green is the portable subset — no real-daemon
   E2E, no forced-local live legs — so CI-green is necessary and never sufficient
   (`roles/assessor/operating.md`). The chaos suite is the superset that CI cannot run.

This mirrors the split the repo already has: `make fast` while you work, `make core` / `make full`
before anyone accepts the work. The chaos suite sits beside `make full` as the merge-and-release
superset the assessor owns. VC-4 (the overnight sleep) is the one class that cannot be automated into
even the slow loop on demand — it runs by hand at the M0 gate and before any release that changes the
transport or the roster thresholds, and its result is recorded as a dated `assessments/` protocol,
not a CI status.

---

## Sources

- `07-operator-amendments-2026-09-07.md` — A1 (subprocess plugins, live-reload is testable), A2
  (this document's directive).
- `../2026-07-15-agent-substrate-v2/ROADMAP.md` — M0 measurement, the walking skeleton, the
  money demo, and the per-milestone kill criteria this plan's gates reuse.
- `../2026-07-15-agent-substrate-v2/design/21-transport.md` §5 and §Sources — the throwaway
  harnesses (`blackhole`, `window`, `menu`, `durable`, `lone`, `fleet`, `driver`) each standing
  test graduates from.
- `../2026-07-15-agent-substrate-v2/ARCHITECTURE.md` §11 — the ranked open questions; claims A, B,
  and C in §3 are its q1, q2, and q9.
- `roles/assessor/operating.md`, `roles/assessor/soul.md`, `test/exploratory/cases/README.md`,
  `assessments/README.md` — the role, the corpus, and the record this work fits into.
