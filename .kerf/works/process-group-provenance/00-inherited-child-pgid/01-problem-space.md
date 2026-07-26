# 01 — Problem Space

**Work:** child-pgid
**Bead:** hk-n93gq (P1, bug) — "Run children join the daemon process group, so no kill path can reach a grandchild"
**Type:** spec (amends two normative specs)

## What is changing and why

Today the daemon spawns every run/handler subprocess into the **daemon's own process
group**. The spawn site is `internal/handler/handler.go:309`:

```go
cmd.SysProcAttr = lifecycle.SpawnChildSysProcAttr(lifecycle.RecordedPGID())
```

`RecordedPGID()` returns `syscall.Getpgrp()` (the daemon's PGID), and
`SpawnChildSysProcAttr` sets `SysProcAttr{Setpgid: true, Pgid: <daemon_pgid>}`
(`internal/lifecycle/spawndaemonchild_{darwin,linux}.go:20-25`). The child therefore
**joins the daemon's group instead of leading its own**.

Consequence, documented in production comments at `internal/handler/session.go:404-419`:
a group-directed kill is impossible. `Kill` can only signal the child's **positive
PID** (`session.go:426`, `session.go:448`). It cannot signal the group:

- `syscall.Kill(-childPid, …)` addresses a group whose ID equals the child's PID — which
  does not exist (the child is not a group leader) — so it returns `ESRCH`.
- `syscall.Kill(-daemonPgid, …)` would kill the daemon itself and every sibling handler.

So any **grandchild** the run child forked survives `Kill`, is re-parented to init, and
leaks. This is the **structural cause of the orphan leak** (found by crew kilo while
root-causing hk-bl2k6). The two named backstops both fail today: `waitWithSocketGrace`
passes an already-cancelled ctx (zero grace), and the darwin orphan sweep is a no-op
(hk-o7x4w). darwin additionally has no `Pdeathsig` (the Linux spawn variant sets
`Pdeathsig: SIGTERM`; the darwin variant cannot).

## Goal — what should be true after this change

1. Each run child is its **own process-group leader** (`Pgid: 0` at spawn ⇒ the kernel
   sets the child's PGID equal to its own PID).
2. `session.Kill` signals the child's **process group** (`kill(-childPGID, sig)`), so the
   immediate child **and every grandchild in that group** receive SIGTERM→SIGKILL. The
   live-daemon kill path reaches grandchildren on **both** Linux and darwin.
3. Dropping the daemon-PGID does **not** weaken the orphan sweep's provenance detection
   (proven in research/design; the env-marker is the load-bearing signal on Linux).
4. The two normative specs (HC-044, PL-006a) and the anti-regression test
   (`session_test.go:195-286`) encode the **new** correct behavior.

## Non-goals

- **br subprocesses.** `br` (Beads CLI) children are spawned via the parallel PL-006a
  helper `lifecycle.SpawnSysProcAttr` (`provenance.go:59`), are short-lived, and do not
  fork grandchildren needing group-kill. They stay in the daemon group. Out of scope.
- **Fixing the darwin post-crash orphan sweep** (hk-o7x4w). This work must not *regress*
  it, but repairing the already-inert darwin sweep is separate.
- **`waitWithSocketGrace` zero-grace ctx bug** — sibling in-lane fix (crew kilo). Not here.
- Reworking `Pdeathsig` / `SetsidDaemon` (PL-006a step 0 has no production caller).

## Constraints / must-not-change

- **Provenance sufficiency is the load-bearing precondition.** PL-006a currently names the
  PGID as *one of two* provenance markers (env var + PGID), and OQ-PL-008 designates PGID
  as the *primary darwin* marker. The design MUST confirm the env-marker path carries
  provenance before the PGID stops being a per-project-constant signal. (Finding: env is
  the sole *implemented* signal on Linux; darwin was already inert — see research.)
- Preserve `PL-INV-005` (subprocess parentage is daemon-originated; sweep only reaps
  marked processes) and the "exactly one `cmd.Wait()`" reap discipline (`session.go`).
- Keep the darwin/linux spawn split (`Pdeathsig` is Linux-only and must not appear in the
  darwin build).

## Success criteria (spec-level)

- HC-044 states run children are spawned as **own-group leaders** (`Pgid: 0`), and that
  `Kill` targets the child's process group.
- PL-006a(ii) is rewritten: the run child's PGID is a **kill handle**, no longer a
  per-project provenance value; provenance rests on the env marker (Linux), with darwin
  provenance re-routed to OQ-PL-008's marker-file fallback (or explicitly deferred).
- The `session_test.go` marker asserts the grandchild is **dead** after `Kill`.

## Spec areas affected (preliminary)

- `specs/handler-contract.md` §4.10 HC-044 (+ interaction with HC-044a, HC-018).
- `specs/process-lifecycle.md` §4.2 PL-006a(ii), PL-006 subprocess-cleanup clause,
  PL-INV-005 sensor, OQ-PL-008.
- Blast radius (code): handler (spawn site + Kill), lifecycle (spawn variants, provenance
  contract, orphan-sweep darwin comment), daemon (pidfile line-2 records the daemon's own
  PGID — unaffected; the linkage "recorded_pgid passed to child spawn" is severed).
