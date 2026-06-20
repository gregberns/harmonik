# Wire `await-ack` into the restart flow — VERIFIED restart, not assumed (hk-uldg)

Integration follow-up to `18-design-agent-side-ack.md` (operator-approved) and
`19-impl-await-ack.md` (the primitive). The `harmonik keeper await-ack` CLI already
exists on main: it polls the agent's pane for `[KEEPER ACK <nonce>]`, exit 0 on
match / exit 3 + `session_keeper_ack_timeout` event on timeout. This bead WIRES it
into the actual restart procedure so a restart is confirmed, not trusted.

Ownership (operator-confirmed, design decision 1): **captain watches crews; a
restart wrapper watches for self.** On a SELF restart-now the firing agent is
`/clear`-wiped before its ACK lands, so an EXTERNAL process must run `await-ack`.

## What was wired

### 1. Self-restart wrapper — `scripts/captain-tools/keeper-restart-verified.sh` (NEW)
The external watcher for the SELF restart case. Fires `harmonik keeper restart-now
--agent <agent>`, parses the printed `nonce=rn-<millis>` (via `sed`), then runs
`harmonik keeper await-ack --agent <agent> --nonce <nonce> --kind restart
--timeout 30s`. Because it is a separate OS process, the agent's `/clear` does not
kill it. Exit codes mirror the primitive: 0 = ACK observed (alive), 1 = arg/
restart-now/nonce-parse error, 3 = ACK never landed (await-ack emitted
`session_keeper_ack_timeout`; logs an INVESTIGATE line). `--project`/`--timeout`/
`--poll` flags + `HK_PROJECT` env. `bash -n` clean; usage path verified.

### 2. Captain skill — `.claude/skills/captain/SKILL.md` §10 Restart continuity
Added "VERIFY a crew restart you TRIGGER" — captain watches crews. When the captain
fires `restart-now --agent <crew>` it captures the nonce and runs `await-ack --agent
<crew> --kind restart` DIRECTLY (the captain's process is external to the crew, so
it survives the crew's `/clear`). On non-zero exit: escalate — comms-alert the
operator with `--from <your-lane>` (NOT a hardcoded `captain`, per the freeze-the-
fleet footgun), check the fired command's stderr reason, re-arm the crew's keeper.
Also added a self-restart note: the captain CANNOT verify its own restart (its
`/clear` wipes it); that is delegated to the `keeper-restart-verified.sh` wrapper.

### 3. Crew-launch skill — `.claude/skills/crew-launch/SKILL.md` § Self-restart
Added "Restart verification — who confirms the ACK": the crew does NOT verify its
own restart (its `/clear` wipes it before the ACK is readable); the captain confirms
via `await-ack` and re-arms on timeout. Added a self-service `ping` + `await-ack
--kind ping` liveness recipe (fresh nonce each time) the live crew CAN run, and the
instruction to surface a ping timeout to the captain over comms rather than silently
continuing.

### 4. Keeper skill — `.claude/skills/keeper/SKILL.md`
- restart-now section: noted a restart is now VERIFIABLE (ACK injected before the
  gated `/clear`; `restart-now` prints `nonce=rn-<millis>`); don't assume it landed.
- NEW `await-ack` subcommand reference (flags, injectable-seam rationale, exit
  codes, "binary does not comms — caller owns escalation with `--from <lane>`").
- NEW § "Verifying a restart with await-ack — who runs it (design decision 1)":
  ping (self-service) vs restart-now SELF (the wrapper) vs restart-now CREW (captain).
- Quick-reference block gained the wrapper, the crew `await-ack`, and the ping recipe.

### 5. Go glue — verified, NONE needed
`runKeeperRestartNow` (`cmd/harmonik/keeper_cmd.go:452`) already prints
`...nonce=rn-<millis>...` in a parseable form, and `await-ack` already exists. No
Go source changed.

### 6. Embedded-asset re-sync (required)
The three SKILL.md files are embedded under `cmd/harmonik/assets/skills/` and gated
by `TestSkillAssetsEmbedInSync`. Re-synced all three embedded copies (`cp` per the
test's own remediation hint) so the test passes.

## Untouched (collision-avoidance)
`cycle.go`, `watcher.go`, `restartnow.go`, `injector.go` — NOT edited (the
automatic-cycle area is owned by companion bead hk-vpnp).

## Gap left (out of scope, flagged)
The AUTOMATIC keeper cycle (`MaybeRun`/`runCycle`) does NOT run `await-ack` on its
own restarts — it still relies on its internal handoff-nonce poll. ACK-verifying the
automatic cycle is a SEPARATE bead (hk-vpnp owns that code path). The wiring here
covers the MANUAL `restart-now` / `ping` paths only. This is documented inline in
the keeper + captain skills.

## Green
- `go build ./...` ✓
- `go vet ./internal/keeper/... ./cmd/harmonik/...` ✓
- `go test ./internal/keeper/... ./cmd/harmonik/...` ✓ (after embedded re-sync)
- `gofumpt -l internal/ cmd/` clean
- NO live-tmux smoke run (per instruction).
