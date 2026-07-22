# Components (Affected Spec Areas)

The change is small in surface but cross-subsystem. Code touches four components; the
normative change touches two specs. Grounded in file:line throughout.

## Affected Existing Specs

### handler-contract.md — §4.10 HC-044

- **Change summary:** run children spawn as their **own process-group leaders** and
  `Kill` targets the child's process **group**, not just its positive PID.
- **Requirements after change:**
  - HC-044 states the daemon spawns each run child with `SysProcAttr{Setpgid:true,
    Pgid:0}` so the child leads a new group (`pgid == child_pid`).
  - HC-044 states `session.Kill` MUST signal `-child_pgid` (SIGTERM→SIGKILL), reaching the
    immediate child and every grandchild in the group, on Linux and darwin alike.
  - The Linux `Pdeathsig:SIGTERM` guidance is retained; darwin still has none.
- **Dependencies:** none (self-contained on the handler side).

### process-lifecycle.md — §4.2 PL-006a(ii), PL-006 subprocess-cleanup, PL-INV-005, OQ-PL-008

- **Change summary:** the child PGID is reframed from a **provenance marker** into a
  **kill handle**; provenance now rests on the env marker (`HARMONIK_PROJECT_HASH`).
- **Requirements after change:**
  - PL-006a(ii): the run child's PGID is no longer a per-project-constant provenance value
    (each child leads its own group). Provenance for the orphan sweep = the env var on
    Linux (`/proc/<pid>/environ`).
  - PL-006 subprocess-cleanup + PL-INV-005 sensor: drop "PGID" from the provenance-marker
    phrasing for run children; env var is the sensor. (`br` children unchanged.)
  - OQ-PL-008: the "PGID-primary-on-darwin" default is no longer viable for run children;
    darwin provenance is re-pointed at the marker-file fallback or explicitly deferred.
- **Dependencies:** depends on the provenance-sufficiency proof (research C0). Must not
  regress the already-inert darwin sweep (hk-o7x4w).

## New Specs

None. This is an amendment to two existing normative specs; no new spec file.

## Dependency Map

- Research **C0 (provenance sufficiency)** gates the PL-006a spec draft (S2/S3): the PGID
  cannot be reframed until env-marker sufficiency is confirmed.
- Code: C1 (`SpawnChildSysProcAttr` → `Pgid:0`) ← C2 (call site drops `RecordedPGID()`);
  C3 (`Kill` → `-pid`) is independent; C4 (test inversion) validates C1+C3.
- Spec S1 (HC-044) is independent of S2/S3 and can be drafted in parallel.

## Goal → Area Traceability

| Goal (from 01) | Satisfying area |
|---|---|
| G1 child is own group leader | HC-044 (S1); code C1/C2 |
| G2 Kill reaches grandchildren via group signal | HC-044 (S1); code C3 |
| G3 dropping daemon-PGID doesn't weaken the sweep | PL-006a/PL-006/PL-INV-005 (S2/S3); research C0 |
| G4 specs + anti-regression test encode new behavior | S1/S2/S3 + code C4 |
