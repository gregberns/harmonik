# Keeper restart-now / ping ACK handshake — agent-side protocol

> Capability `hk-5da7`. Reference for an **agent** (captain or crew) that wants to
> self-restart or liveness-check **its own keeper**.

## Purpose

The keeper now injects a **verifiable ACK line** into the agent's own pane when it
acts on a request. This lets the requesting agent (or an external watcher)
**confirm the keeper actually received the request** instead of trusting a silent
"success". The old `restart-now` was a silent no-op — it could fail with no signal.
That failure mode is now impossible: the command fails loudly (non-zero exit +
logged reason) and, on success, writes an ACK line you can match on.

## The commands (flag-only — positionals are rejected)

```bash
# Self-restart: verify session id, check handoff freshness, then inject the
# ACK line, /clear, and /session-resume into the agent's own pane.
# REQUIRES a fresh HANDOFF-<name>.md (written by /session-handoff < 10 min ago).
harmonik keeper restart-now --agent <name>

# Liveness only: inject just the ACK line. No /clear, no resume.
harmonik keeper ping --agent <name> [--nonce <N>]
```

## The ACK line format (exact)

```text
[KEEPER ACK <nonce>] received <restart|ping>
```

- **restart-now**: nonce is auto-generated as `rn-<unix_millis>` and printed on
  stdout (`nonce=rn-...`). Capture it from the command output.
- **ping**: pass your own `--nonce <N>`; if omitted the keeper picks one and prints it.
- The agent **matches on the nonce substring** in the pane.

## Agent-side procedure

1. **(restart-now only) Write your handoff first.** Run `/session-handoff` so
   `HANDOFF-<name>.md` is fresh (within the last 10 minutes). restart-now refuses
   on a missing or stale handoff.
2. **Fire the command** (`restart-now` or `ping`). Capture the nonce: restart-now
   prints `nonce=rn-...` on stdout; for ping use your chosen `--nonce`.
3. **Arm a timer** (~15s for ping). **Caveat for restart-now:** the `/clear` wipes
   this agent's context, so the *restarting agent* will not "see" its own ACK. The
   ACK is injected **first, before `/clear`**, precisely so an **external watcher,
   operator, or the next session reading pane scrollback** can verify it. For
   **ping**, the verifier is the **same live agent**.
4. **Await the `[KEEPER ACK <nonce>]` line** in the pane (`tmux capture-pane`)
   matching your nonce.
5. **ACK seen → keeper received the request.** For ping: keeper is alive. For
   restart-now: the clear/resume sequence is in flight.
6. **No ACK within the timer → the keeper did NOT receive it** (dead keeper, wrong
   pane, or unverifiable session id). **Investigate** — check the command's
   **non-zero exit + its stderr log**. Every failure logs a reason:
   `no_tmux_target`, `sid_not_primary`, `handoff_missing`, `handoff_stale`,
   `ack_inject_failed`. **Do not assume success.**

## Key behavioral notes

- **Fails loudly.** A silent no-op is impossible. The **exit code is the first
  verification**; the **ACK line is the second** (pane-level) verification.
- **restart-now refuses** (non-zero exit, no `/clear`) if any of:
  - no resolvable pane,
  - the session id is not a trusted lowercase UUIDv4,
  - the handoff is missing, or
  - the handoff is older than 10 minutes before the request.
- **Direct / synchronous now** — no marker file, no watcher-poll delay. The old
  `.restart-now` marker path was **removed**: it was the silent-no-op bug — the
  marker was written under the caller's CWD while the watcher polled a different
  project dir, so the request was never seen.
