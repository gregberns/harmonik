# session-keeper — Tasks

Parent: hk-ekap1. Three groups: **Phase 1** ships now (non-destructive); **Spike** gates Phase 2; **Phase 2** is blocked on the spike. Each task carries `codename:session-keeper`.

## Phase 1 — warn-only (buildable now, dogfood on flywheel)
- **SK-1 — scaffold.** New `internal/keeper/` subsystem + `harmonik keeper --agent <name>` subcommand skeleton. depguard component-matrix entry (`.golangci.yml`) per `go-subsystem-add`. Single-keeper lockfile §13.1 + opt-in `.managed` marker check §13.2 (refuse panes without it). *Done:* `harmonik keeper --agent flywheel` starts, acquires lock, no-ops without `.managed`.
- **SK-2 — gauge signal (C1).** `scripts/keeper-statusline.sh` reads `context_window.used_percentage` + `session_id` from stdin JSON, atomically writes `.harmonik/keeper/<agent>.ctx`. *Done:* file updates within ~1s of each assistant message (S1). Depends: SK-1.
- **SK-3 — watcher warn-mode (C2).** Poll loop, `warn_pct`=80 crossing logic, quiesce idle-gate (§4.5 Phase-1), staleness + no-gauge self-check §3.4. *Done:* one warn per upward crossing, none when stale. Depends: SK-2.
- **SK-4 — injector (C3).** tmux target from `harmonik-<hash>-<agent>` convention; external send-keys (bracketed-paste + Enter). *Done:* a prompt lands+submits in the correct pane. Depends: SK-1.
- **SK-5 — events (C6, Phase-1 subset).** `session_keeper_warn` + `session_keeper_no_gauge` constants in `eventtype.go`, emit via `EmitWithRunID`. *Done:* both visible in `harmonik subscribe`. Depends: SK-1.
- **SK-6 — Phase-1 dogfood + docs.** Run keeper against this flywheel session; verify S1+S2 end-to-end, zero destructive action; document operator setup (statusLine in `~/.claude/settings.json`, start command). *Done:* validated on a live orchestrator. Depends: SK-3, SK-4, SK-5.

## Spike — Phase-2 verification (gates everything below)
- **SK-7 — run E1–E4 (§12).** Empirically resolve: (E1) does `/clear` mint a new `session_id` in statusLine JSON + post-resume drop speed; (E2) does `/clear`→`/session-resume` preserve the queued resume; (E3) does the statusLine re-run after `/clear` with no assistant message; (E4) does the Stop hook mark a true await-input boundary. Output `12-experiments-findings.md` + wire the concrete mechanism into spec §§5–7. *Done:* all four answered; Phase-2 design de-risked or redesigned. Depends: SK-6 (needs the Phase-1 gauge+inject harness to run experiments).

## Phase 2 — full cycle (BLOCKED on SK-7)
- **SK-8 — hooks.** `scripts/keeper-stop-hook.sh` (idle marker, §4.5 MUST) + `scripts/keeper-precompact-hook.sh` (backstop, §8, blocks auto-compaction). Depends: SK-7.
- **SK-9 — handoff cycle (C4).** §5.0–5.5: cycle journal open, `/session-handoff` with `<!-- KEEPER:<cycle_id> -->` nonce, token-confirm, `/clear`, clear-readiness probe §5.3a (mechanism from SK-7), `/session-resume`, journal close. Crash-recovery §7.3. Depends: SK-7, SK-8.
- **SK-10 — anti-loop + dispatch gating.** §7.2 marker (keyed per SK-7 outcome), §4.6 `.dispatching` attribution marker (fail-closed). Depends: SK-9.
- **SK-11 — Phase-2 events + dogfood.** Remaining `session_keeper_*` constants; validate S3/S4/S5 on a live orchestrator (full cycle, no mid-dispatch fire, no double-fire, native compaction never wins). Depends: SK-9, SK-10.

## Dispatch notes
- Phase 1 (SK-1…6) is independent, non-destructive, and daemon-dispatchable in parallel where deps allow (SK-2/4/5 after SK-1; SK-3 after SK-2; SK-6 last).
- SK-7 is a **flywheel-run experiment** (needs a live orchestrator pane), not a daemon bead — run it in-session, not via the implementer queue.
- Do NOT open SK-8…11 beads until SK-7 lands; they're documented here but should stay un-beaded (or `blocked`) to avoid stale-open Phase-2 work.
- Lane: SK-4's send-keys may share code with pasteinject (named-queues' lane) — coordinate if extracting.
