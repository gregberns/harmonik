# Plan 006: `harmonik run <bead-id>` subcommand

## Objective
Add a `harmonik run <bead-id>` subcommand that submits a single-bead queue and exits when that bead reaches a terminal state — the canonical Phase-2 single-bead invocation.

## Status
not-started

## Done means...

`harmonik run <bead-id>` is done when a single command targets a bead, runs it, and exits cleanly — no manual daemon management, no queue JSON authoring, no priority-bump workarounds.

1. **Exits on terminal state.** `harmonik run <bead-id>` exits 0 when the bead reaches DONE, exits non-zero when the bead reaches FAILED. Verified by `hk-icecw` acceptance criteria.
2. **One-shot semantics.** After the target bead terminates, the daemon does NOT cascade to the next `br ready` bead. Verified by a test that submits a 1-item queue and confirms the daemon exits rather than claiming a second eligible bead. Covers `hk-ajchp`.
3. **Help is discoverable.** `harmonik run --help` exits 0 and names `<bead-id>` as a required positional argument with a one-line description. Per plan 009 CLI-help criteria.
4. **Scenario test GREEN.** A twin-based test confirms that `harmonik run <id>` against a real bead: spawns the implementer, observes a commit, emits `run_completed` in JSONL, and exits cleanly with no orphaned processes. This is the scenario-test bead required by `plans/README.md`; file it alongside `hk-icecw`.
5. **Exploratory test GREEN.** An agent can run `harmonik run --help` followed by `harmonik run <real-bead-id>` and produce a closed bead without reading any other documentation. This is the exploratory-test bead required by `plans/README.md`.

## What's done
- Queue subsystem wired into the daemon composition root (commit `9925ce7`), with the close-out commit at `9b89471`. This was the prerequisite blocker: the submit + drain machinery `harmonik run` reuses is now reachable from the daemon entry path.
- `internal/queue/` has the surface needed: `internal/queue/cli/submit.go` (submit path), `internal/queue/queue.go` + `state.go` (state), `internal/queue/rpc.go` (daemon-side RPC).

## What's remaining
- `hk-icecw` (P1) — implement `harmonik run <bead-id>`: parse bead ID, submit a 1-item queue, run the daemon, wait for terminal transition, exit. Reuses the queue submit path; no parallel code path.
- `hk-ajchp` (P2) — idle/exit semantics: after the target bead completes, the daemon must not cascade to the next `br ready` candidate. Either gate via a `--one-shot` flag on the existing daemon, or — preferred — make it implicit in `harmonik run` semantics (queue has exactly one item; on its terminal transition, drain → exit).

## References
- beads: `hk-icecw` (P1, this plan's main bead), `hk-ajchp` (P2, dependent — exit-on-completion semantics). Label search: `phase2-dogfood-friction`, `harmonik-cli`.
- source commit (discovery): `dcd7f7e` — Phase-2 dogfood of `hk-iuaed.6` exposed the friction.
- unblocking commits: `9925ce7` (queue wired into daemon), `9b89471` (close-out of `hk-gi471`).
- code:
  - `internal/queue/cli/submit.go` — existing submit entry point to mirror/reuse.
  - `internal/queue/queue.go`, `internal/queue/state.go` — queue state + terminal-transition observation.
  - `internal/queue/rpc.go` — daemon RPC surface.
  - `cmd/harmonik/` (root subcommand wiring — where `run` lands).
- chat-context: Phase-2 dogfood of `hk-iuaed.6` (2026-05-15) had to bump bead priority to P0 to target a specific bead via `harmonik --project DIR --max-concurrent 1`, because the daemon polls `br ready` and grabs the first eligible bead. Priority-bump-and-pray is unreliable; `harmonik run <id>` is the canonical fix. Plan written 2026-05-18 once the queue-wiring dependency landed.

## Next steps
1. Dispatch implementer for `hk-icecw`: add `harmonik run <bead-id>` subcommand. Pattern — parse bead ID, build a 1-item queue payload, hand to the existing queue submit path, start the daemon, block until the queue reports the bead in a terminal state, then exit cleanly.
2. In the same change (or as a tight follow-up for `hk-ajchp`), wire exit-on-empty so the daemon does not cascade to the next `br ready` bead after the target completes. Preferred: make `run` use a one-shot queue mode rather than introducing a separate `--one-shot` daemon flag.
3. Smoke test by re-running the `hk-iuaed.6`-style scenario without any priority bump — `harmonik run <id>` should target the bead directly and the daemon should exit when the bead terminates.

## Open questions
- One-shot semantics surface: implicit in `run` (preferred, no new flag) vs. an explicit `--one-shot` flag on the existing daemon entry. Implementer's call unless review surfaces a reason to prefer the flag for symmetry.
