# Research — handler (spawn call-site + kill path + anti-regression test)

## C2 — spawn call site

`internal/handler/handler.go:309`:
```go
cmd.SysProcAttr = lifecycle.SpawnChildSysProcAttr(lifecycle.RecordedPGID())
```
`RecordedPGID()` = `syscall.Getpgrp()` (the daemon's PGID; `provenance.go:95-97`). This is
the single line that places the child in the daemon's group. `cmd.Env = spec.Env` on the
preceding line (`:308`) carries the provenance env var (injected by the daemon per
`handler.go:55-56`) and is **untouched** by this work. Change: drop the `RecordedPGID()`
argument.

## C3 — the kill path

`internal/handler/session.go:404-453` (`Kill`). The current comment block (`:404-419`)
documents exactly why group-kill is impossible under daemon-group membership:

> "A `syscall.Kill(-childPid, ...)` therefore addresses a process group whose ID equals the
> child's PID -- which does not exist -- so the signal returns ESRCH ... Using -daemonPgid
> would be worse: it would signal the daemon itself and every sibling handler."

SIGTERM is sent to the positive `pid` at `:426`; SIGKILL escalation to positive `pid` at
`:448`. `ESRCH` is tolerated on both (already-reaped is not an error).

**Change:** signal the child's group. Because the child now leads its own group
(`pgid == pid`), `syscall.Kill(-pid, sig)` addresses a group that **exists** and contains
the immediate child **plus all grandchildren** it forked (they inherit the child's PGID
unless they call `setsid` -- the OQ-PL-011 escape hatch, out of scope). `ESRCH` tolerance
is preserved. The comment block is rewritten to state the new invariant (group-directed
kill reaches the whole subtree).

Interaction: `waitWithSocketGrace` passing an already-cancelled ctx (zero SIGTERM->SIGKILL
grace) is a **separate** in-lane bug (crew kilo). The group-kill fix is correct regardless
of grace timing; a proper grace window is complementary, not required here.

## C4 — the anti-regression marker test (the inversion)

`internal/handler/session_test.go:195-286`
(`TestSession_Kill_ReapsImmediateChildPromptly`). The comment (`:199-213`) deliberately
encodes the OLD (now-invalid) invariant:

> "grandchildren are NOT Kill's responsibility -- they are torn down by the caller's bounded
> post-kill wait (waitWithSocketGrace) and the daemon's orphan sweep ... This test asserts
> the property Kill DOES own (prompt immediate-child reap) and explicitly does NOT assert
> grandchild death -- that would re-encode the invalid assumption that masked the bug."

Structure today: child forks `sleep 300` grandchild, records its PID (`:220-224`), a
`defer` SIGKILLs the leaked grandchild so it does not escape the test (`:252-254`), then
after `Kill` the test asserts only prompt immediate-child reap (`:271-285`).

The fixture `sessionFixtureCmd` currently sets the production spawn config
(`Setpgid=true, Pgid=<daemon_pgid>`, per the comment at `:196-198`).

**Inversion (new correct behavior):**
1. `sessionFixtureCmd` spawns with `Pgid:0` (own group).
2. After `Kill`, assert the grandchild is **DEAD**: `syscall.Kill(grandchildPID, 0)`
   returns `ESRCH` within a bounded poll. This is the property the fix establishes.
3. Keep the prompt-reap assertion (`:271-285`) -- the hk-4c7kw guard still holds.
4. The `defer` grandchild-SIGKILL becomes belt-and-suspenders (grandchild already dead);
   keep it so a *regression* (grandchild survives) does not leak a process.
5. Rewrite the doc comment to state group-kill semantics; rename to e.g.
   `TestSession_Kill_ReapsProcessGroupIncludingGrandchildren`.

## Lifecycle unit tests that break on the signature change

`spawndaemonchild_hc044_test.go`: `_PgidMatches` (expects `attr.Pgid == wantPGID`) and
`_DistinctFromSpawnSysProcAttr` assume the pgid argument; `_ZeroPGIDAllowed` already
asserts `Pgid==0` and becomes the *canonical* case. These must be updated to the no-arg /
`Pgid:0` contract (task-pass detail).
