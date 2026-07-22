<!--
Spec-draft AMENDMENT for specs/process-lifecycle.md §4.2 PL-006a (marker clause),
the PL-006 subprocess-cleanup clause, the PL-INV-005 sensor, and OQ-PL-008.
DRAFT on the kerf bench — NOT applied to specs/ by this work. Replacement text for
the affected paragraphs only; surrounding text unchanged and elided.
-->

# process-lifecycle.md — §4.2 amendment (PL-006a / PL-006 / PL-INV-005 / OQ-PL-008)

## PL-006a(ii) — provenance marker (replacing the "BOTH … env var … PGID" paragraph)

The provenance marker used by the orphan sweep (§PL-006) to identify this project's
subprocesses is the environment variable `HARMONIK_PROJECT_HASH=<project_hash>`, set on
every subprocess the daemon spawns and readable via `/proc/<pid>/environ` on Linux. The env
var is inherited by any grandchildren the subprocess forks, so re-parented descendants remain
identifiable by the same marker.

Handler (run) subprocesses are spawned as **leaders of their own process group**
(`SysProcAttr{Setpgid: true, Pgid: 0}`, per [handler-contract.md §4.10 HC-044]). A run
child's PGID therefore equals its own PID and is a **kill handle** — it enables
`session.Kill` to signal `-child_pgid` and reach the child's whole subtree — **not** a
per-project provenance value. The daemon MUST NOT rely on a run child's PGID for
orphan-sweep provenance matching.

`br` (Beads CLI) subprocesses spawned via the BI adapter continue to use the daemon-group
spawn helper (`SpawnSysProcAttr`); they are short-lived, fork no descendants requiring
group-kill, and carry the same `HARMONIK_PROJECT_HASH` env marker.

The daemon MUST still record its own PGID in the pidfile per PL-002b (line 2) for the
liveness sensor (§4.9); that value is the daemon's `getpgrp()` and is unrelated to any run
child's group.

Subprocess trees that internally call `setsid` escape the group-kill handle and the sweep
cannot reap their descendants; this hazard remains tracked as OQ-PL-011.

## PL-006 — Subprocess cleanup (provenance-marker phrasing reconciliation)

In the subprocess-cleanup bullet, the provenance match for **handler** subprocesses is the
`HARMONIK_PROJECT_HASH` env marker (Linux `/proc/<pid>/environ`). Delete the parenthetical
"(env var + PGID)" characterization of the handler marker; for `br` subprocesses the env-var
marker likewise governs. Identification MUST NOT rely on binary path alone (unchanged).

## PL-INV-005 — sensor (replacing "environment variable + PGID")

Sensor: every spawn site MUST set the `HARMONIK_PROJECT_HASH` env-var provenance marker of
PL-006a. A subprocess without the marker is not a harmonik-owned subprocess by definition and
MUST NOT be reaped by PL-006. (Run children additionally lead their own process group per
HC-044; that group is a kill handle, not part of the provenance sensor.)

## OQ-PL-008 — macOS provenance-marker mechanism (updated default)

Question (unchanged): on darwin, `/proc/<pid>/environ` is unavailable, so the env-var side of
the marker is not readable by the orphan sweep. With run children now leading their **own**
process groups, there is no project-constant PGID to match on darwin either, so PGID is no
longer a candidate darwin provenance mechanism.

Updated default-if-unresolved: the darwin **post-crash** orphan sweep for re-parented run
descendants remains a no-op (status quo; tracked as hk-o7x4w) UNTIL a filesystem-based
per-pid marker file (`/tmp/harmonik-<project_hash>-<pid>.marker`) is adopted. This does NOT
affect the darwin **live-daemon** kill path, which is fixed by the group-directed
`session.Kill` per HC-044 (POSIX `kill(-pgid)` works on darwin without `/proc`). Resolving
this OQ toward the marker-file fallback is a scoped follow-up and is NOT a prerequisite for
the HC-044 group-kill change.
