# Kernel Slice B chaos cases

The live cases behind the Slice B slice gate (task B10): the competing-consumers slice — a
`dispatch` plugin running as one primary and two workers on an N=3 in-process mesh. They are driven
by the standing chaos harness at `tools/dispatch/chaos_test.go`, behind `//go:build chaos`, run by
one command: `make chaos`. Like the echo VC-12 cases, they build their own binaries (one `harmonikd`,
dispatch plugin copies, a byte-distinct reload target) and launch a live `harmonikd mesh` subprocess,
so a reader runs one command and reads the verdict.

The slice question: **does work load-balance, survive a worker death, and survive a stateful-plugin
reload — with every job done exactly once, none lost, none duplicated?** Any lost or duplicated job
is a HARD STOP.

---

## SB-001 — work does not load-balance evenly (G1)

Class:       protocol
Exercises:   POINT_TO_POINT round-robin over two live members, across the memmesh
Status:      HOLDS at da7cd0113215a583e9cf11fb7286b76bd3b5cfa6 (verified 2026-09-09, three runs)

Preconditions: a Go toolchain and the workspace `go.work`. No daemon running; the harness launches
its own `harmonikd mesh` (N=3) on ephemeral loopback ports.

Steps:

    cd tools/dispatch && go test -tags chaos -run TestG1LoadBalanceFiveFive -v -count=1 ./.

Expect: 10 jobs submitted to `dispatch.submit` land 5 in worker-1's `done` journal and 5 in
worker-2's, set-equal to the primary's 10-job `accepted` set. Assignment is deterministic
round-robin over the two members (both attached before the first job — the harness gates on all
nodes logging "node listening"), so the 5+5 split is scheduler-independent.

Failure signature: a share other than 5+5 (one member starved or never attached), or a
`HARD STOP` no-loss / no-duplication / no-strays line naming job ids.

---

## SB-002 — a killed worker's in-flight job is lost (G2, the C5 path)

Class:       protocol
Exercises:   the dead-worker requeue path — lease held on the declaring kernel, nacked to a surviving
             peer when a `Deliver` surfaces Unavailable
Status:      HOLDS at da7cd011 (verified 2026-09-09, three runs)

Preconditions: as SB-001. The dispatch worker's `HARMONIK_DISPATCH_WORK_DELAY_MS` knob holds each
job in flight long enough for the kill to land while a job is leased-but-unjournaled.

Steps:

    cd tools/dispatch && go test -tags chaos -run TestG2WorkerKillRequeue -v -count=1 ./.

Expect: with a steady submit stream and a 300 ms work delay, one worker is `kill -9`ed after it has
completed at least one job (so another is in flight). Afterwards the union of the two `done` journals
equals the `accepted` set, each job exactly once; the dead worker's `done` set is frozen at its
pre-kill snapshot; the survivor completed every remaining job, including the one the victim held in
flight at the kill. Nothing relaunches the dead worker — the C5 minimum is requeue-to-survivors, not
relaunch.

Failure signature: a `HARD STOP` no-loss line (the in-flight leased job was dropped, not requeued),
or the dead worker's `done` set growing after the kill (a zombie completing work).

---

## SB-003 — a stateful-plugin reload mid-stream loses or duplicates a job (G3, VC-12 re-run)

Class:       protocol
Exercises:   the K8 drain gate + the B8 PTP lease detach/re-attach, against a STATEFUL plugin (the
             dispatch journals), under the memmesh mesh composition
Status:      HOLDS at da7cd011 (verified 2026-09-09, three runs)

Preconditions: as SB-001. A byte-distinct dispatch binary (a different linker build-id) is the
reload target, so the host's sha256 VERIFIED step re-checks a genuinely different file.

Steps:

    cd tools/dispatch && go test -tags chaos -run TestG3ReloadUnderLoadStateful -v -count=1 ./.

Expect: 500 jobs flow to `dispatch.submit` at >= 100 msg/s; a third of the way through the PRIMARY is
reloaded to the byte-distinct binary, and two-thirds through a WORKER is reloaded. Afterwards the
`accepted` set and the done-union set are equal (zero loss, zero duplicates by job id); the mesh
process PID is unchanged and alive (the reload swapped a plugin child, never the daemon that hosts
every kernel), and at least one plugin child PID changed (the swap was real); each reload completes
within the echo gate's recalibrated 150 ms budget. This is the stateful counterpart to the echo
VC-12 gate: the dispatch plugin holds journals, and journal-replay rehydration on `Start` is what
lets a reload lose nothing.

Failure signature: a `HARD STOP` no-loss / no-duplication line (a submit that landed mid-reload was
dropped, or an in-flight delivery was re-dispatched instead of drained/acked); a changed mesh PID (a
reload restarted the daemon); or a reload latency past the 150 ms ceiling (a latency regression,
reported distinctly from a zero-loss failure).

Measured 2026-09-09 (three runs): primary reload ~60–70 ms, worker reload ~50–60 ms, 500/500
exactly-once, mesh PID stable.

---

## SB-004 — four-type channel conformance across the mesh (G4, VC-13 full width)

Class:       probe
Exercises:   all four channel types across nodes — PUBSUB, POINT_TO_POINT, REQUEST_REPLY
             (+ INTEREST_NONE fast-fail), LOOKUP (all-claimants)
Status:      HOLDS at da7cd011 (verified 2026-09-09, three runs)

Preconditions: as SB-001.

Steps:

    cd tools/dispatch && go test -tags chaos -run TestG4FourTypeConformance -v -count=1 ./.

Expect: a dispatch-native PUBSUB-across-nodes check — a submit published on a WORKER node's kernel
fans out over the memmesh to the primary's subscription and lands in the primary's `accepted`
journal. Then B9's four-type conformance cases (they live in the kernel module, which tool-isolation
keeps this module from importing, so the gate runs them as a subprocess) pass: PUBSUB copy-to-all,
POINT_TO_POINT exactly-one, REQUEST_REPLY correlated + INTEREST_NONE fast-fail, LOOKUP two-claimant
clash. POINT_TO_POINT exactly-one is also proven by SB-001..003.

Failure signature: the cross-node submit never reaching the primary's `accepted` journal (PUBSUB
fan-out broken), or any B9 conformance subtest failing.
