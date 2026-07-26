# Change Design — child-pgid

One coherent change across handler + lifecycle, with a spec consequence in
process-lifecycle. Grounded in the research findings (03-research/*).

## Design invariant (the target state)

> Every run child is the **leader of its own process group** (`pgid == child_pid`).
> `session.Kill` signals that **group** (`kill(-pgid, sig)`), so SIGTERM→SIGKILL reaches
> the immediate child **and every grandchild** it forked, on Linux and darwin alike.
> Provenance for the orphan sweep is carried by the `HARMONIK_PROJECT_HASH` **env marker**,
> independent of the PGID.

## 1. Run child gets its own PGID (`Pgid: 0`) + group-signal on kill

### Spawn (C1/C2)

- `lifecycle.SpawnChildSysProcAttr` returns `Pgid: 0` (was `Pgid: pgid.Int()`). With
  `Setpgid:true, Pgid:0` the kernel runs `setpgid(child, child)` → the child is a new group
  leader whose PGID equals its PID. Drop the now-dead `pgid core.PGID` parameter.
  - linux: `{Setpgid:true, Pgid:0, Pdeathsig:SIGTERM}` — **keep** `Pdeathsig` (kernel
    SIGTERM on daemon-thread death; orthogonal safety net).
  - darwin: `{Setpgid:true, Pgid:0}` — no `Pdeathsig` (field absent; the split file exists
    for exactly this platform difference).
- `handler.go:309`: `cmd.SysProcAttr = lifecycle.SpawnChildSysProcAttr()` — drop
  `lifecycle.RecordedPGID()`. `cmd.Env = spec.Env` (`:308`, the env marker) is untouched.

### Kill (C3)

- `session.Kill` (`session.go:421`): read `pid := s.cmd.Process.Pid`; because the child is
  its own group leader, `pgid == pid`. Signal the **negative** pid:
  - SIGTERM: `syscall.Kill(-pid, syscall.SIGTERM)` (was `Kill(pid, …)` at `:426`).
  - SIGKILL escalation: `syscall.Kill(-pid, syscall.SIGKILL)` (was `Kill(pid, …)` at `:448`).
  - Keep `ESRCH` tolerance (already-reaped group is not an error).
- Rewrite the `:404-419` comment block: the old text explains why group-kill was
  impossible; the new text states that the child leads its own group so `-pid` reaches the
  whole subtree.

### Why this is correct on both platforms

`kill(-pgid)` is POSIX and works identically on Linux and darwin **while the daemon is
alive** — which is the primary orphan-leak path (kill-on-run-completion / kill-on-shutdown).
Grandchildren inherit the child's PGID unless they call `setsid` (OQ-PL-011 escape hatch,
pre-existing, out of scope). This does not depend on `/proc`, so it fixes darwin's live-kill
path that the sweep-based backstop never could.

## 2. Provenance-sufficiency proof (the load-bearing question)

**Answer: the env marker is sufficient; dropping the daemon-PGID does not weaken the sweep.**

- The sweep (`orphansweep.go:225-277`) matches candidates on `PPID==1` + the
  `HARMONIK_PROJECT_HASH` env var only. **The PGID is never read by the implementation.**
- On **Linux** the env var is inherited by grandchildren and is the sole implemented signal
  → unaffected by the PGID change. ✅
- On **darwin** the sweep is already a no-op (`/proc` absent → every candidate skipped;
  hk-o7x4w) → the change cannot regress it.

**The one honest cost (spec-level):** PL-006a(ii) and OQ-PL-008 *designate* PGID as the
darwin provenance marker on paper. Once each child leads its own group there is no
project-constant PGID to match, so that theoretical darwin path is foreclosed. Since it was
never implemented, no runtime behavior regresses — but the spec must be updated to reflect
that darwin post-crash provenance now rests on OQ-PL-008's marker-file fallback **or** is an
explicitly-tracked darwin gap. (See §5, operator decision.)

## 3. darwin-vs-linux spawn variants (summary)

| | linux | darwin |
|---|---|---|
| before | `{Setpgid:true, Pgid:daemon, Pdeathsig:SIGTERM}` | `{Setpgid:true, Pgid:daemon}` |
| after | `{Setpgid:true, Pgid:0, Pdeathsig:SIGTERM}` | `{Setpgid:true, Pgid:0}` |
| kill reaches grandchildren (live daemon) | ✅ | ✅ (new) |
| post-crash sweep reaches grandchildren | ✅ via env (`/proc`) | ✗ pre-existing gap (hk-o7x4w), unchanged |

## 4. Test inversion (C4 — `session_test.go:195-286`)

The anti-regression marker currently encodes "grandchildren are NOT Kill's responsibility"
(`:199-213`). Rewrite to encode the **new** invariant:

1. `sessionFixtureCmd` spawns with `Pgid:0` (own group) — matching production.
2. After `Kill`, poll `syscall.Kill(grandchildPID, 0)` and assert it returns `ESRCH`
   (grandchild **dead**) within a bounded window.
3. Keep the prompt immediate-child reap assertion (`:271-285`) — hk-4c7kw guard survives.
4. Keep the `defer` grandchild-SIGKILL as belt-and-suspenders (a *regression* must not leak).
5. Rewrite the doc comment; rename → `TestSession_Kill_ReapsProcessGroupIncludingGrandchildren`.
6. Update `spawndaemonchild_hc044_test.go` (`_PgidMatches`, `_DistinctFromSpawnSysProcAttr`)
   to the no-arg / `Pgid:0` contract; `_ZeroPGIDAllowed` becomes the canonical case.

## 5. Cross-subsystem blast radius + the one operator decision

- **handler:** spawn site (`handler.go:309`), `Kill` (`session.go`), marker test.
- **lifecycle:** both `SpawnChildSysProcAttr` variants, and the PL-006a/PL-006/PL-INV-005
  provenance wording (`orphansweep.go` darwin comment is documentation-only). `br` spawn
  via `SpawnSysProcAttr` unchanged.
- **daemon:** pidfile line-2 records the daemon's own PGID (PL-002b) — **unaffected**; only
  the *use* of `RecordedPGID()` at the child spawn site is removed.

**Operator/design decision (surfaced, not a gate):** how to record darwin post-crash
provenance now that PGID is no longer a darwin marker — (a) resolve OQ-PL-008 toward the
per-pid marker-file fallback, or (b) explicitly accept the darwin post-crash sweep remains a
no-op (status quo, hk-o7x4w) and track it. This work does not require either to land — the
live-daemon kill path is fixed regardless — but the PL-006a draft must pick a wording, so
the choice should be made when the spec amendment is reviewed.
