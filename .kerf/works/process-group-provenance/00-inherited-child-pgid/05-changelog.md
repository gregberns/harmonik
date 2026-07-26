# Spec-draft changelog — child-pgid

Bead: hk-n93gq. Amends two normative specs. Drafts live on the kerf bench; NOT applied to
`specs/` by this work (finalize applies them).

## specs/handler-contract.md — §4.10 HC-044

- **Was:** the daemon spawns handler subprocesses into the daemon's process group
  (`Setpgid:true, Pgid:<daemon_pgid>`); `Kill` signals the bare positive child PID; the
  requirement was silent on group membership and grandchild reach.
- **Now:** run children MUST spawn as **own-group leaders** (`Setpgid:true, Pgid:0`);
  `session.Kill` MUST signal the child's **process group** (`-child_pgid`, SIGTERM→SIGKILL,
  `ESRCH`-tolerant), reaching the immediate child and every in-group grandchild on Linux and
  darwin. States explicitly that the own-group PGID is a kill handle, not provenance; env
  marker (PL-006a) is provenance. Linux `Pdeathsig` guidance retained.

## specs/process-lifecycle.md — §4.2 PL-006a(ii), PL-006, PL-INV-005, OQ-PL-008

- **PL-006a(ii):** provenance marker reframed from "BOTH env var + PGID" to **env var only**
  (`HARMONIK_PROJECT_HASH`); the run child's PGID is reclassified as a kill handle. `br`
  subprocesses stay in the daemon group. Daemon still records its own PGID in the pidfile.
- **PL-006 subprocess-cleanup:** drop the "(env var + PGID)" handler-marker characterization;
  env marker governs.
- **PL-INV-005 sensor:** provenance sensor is the env marker; own-group PGID noted as
  kill-handle only.
- **OQ-PL-008:** PGID is no longer a darwin provenance candidate (children lead own groups);
  updated default = darwin post-crash sweep stays a no-op (hk-o7x4w) until a per-pid
  marker-file fallback is adopted; the darwin **live-daemon** kill path is fixed by HC-044
  regardless. Marker-file resolution is a scoped follow-up, not a prerequisite.

## Open decision carried to integration/review

Whether to resolve OQ-PL-008 toward the marker-file fallback now or explicitly accept the
darwin post-crash no-op (status quo) and track it. Not a blocker for the kill-path fix.
