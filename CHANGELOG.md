# Changelog

All notable changes to harmonik are documented here. Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). harmonik follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html) — `0.y.z` line, pre-1.0.

Sections: **Added** / **Changed** / **Fixed** / **Removed** / **Security** / **Spec**.

---

## [Unreleased]

> Tracks changes on `main` not yet tagged. Items marked _(pipeline)_ are part of the goreleaser+ledger+rollback release pipeline defined in `specs/release-pipeline.md`.

### Added

- core-loop-proof seed (hk-nyx): trivial one-line changelog entry verifying the dispatch → implement → merge daemon loop end-to-end.
- core-loop-proof seed v2 (hk-vi3): trivial one-line changelog entry re-verifying the dispatch → implement → merge daemon loop end-to-end.
- core-loop-proof seed v3 (hk-7x5): trivial one-line changelog entry re-verifying the dispatch → implement → merge daemon loop end-to-end.

---

## [0.4.0] — 2026-06-30

### Added

- _(pipeline)_ Tag-triggered 4-stage release pipeline (CREATE → VALIDATE → CERTIFY → ROLLBACK) per `specs/release-pipeline.md`. Tag push triggers goreleaser; release becomes stable only after VALIDATE passes and CERTIFY flips the ledger entry.
- _(pipeline)_ goreleaser binary matrix: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`; binaries + `checksums.txt` published to a GitHub pre-release on tag push.
- _(pipeline)_ Release ledger (`internal/release/manifest.go` `ReleaseEntry` / `ArtifactEntry` schema): compiled-in slice recording every release with semver, commit hash, pre-release state, certification timestamp, and yank record.
- _(pipeline)_ VALIDATE gates: CI Tier 2 (`make check-full`), full scenario suite (`-tags=scenario`), and `harmonik --version` smoke — all required before a release is certified; any failure yanks the pre-release automatically.
- _(pipeline)_ CERTIFY step: CI flips `Prerelease: false` + sets `CertifiedAt` in both the ledger and the GitHub release; commits the updated `internal/release/manifest.go` to `main`.
- _(pipeline)_ Supervisor last-good-binary guard: refuses to adopt a yanked binary; falls back to the last-good binary on crash within 30 s of start (`specs/release-pipeline.md §7.2`).
- _(pipeline)_ `harmonik --version` standardized output: `harmonik v0.y.z (commit: <sha>)`. Both `version` and `commitHash` injected via ldflags by goreleaser.
- `harmonik promote`: cherry-pick a commit from a worktree branch onto `main` behind a build gate with race-safe push.
- Captain & Crew: multi-session orchestration system with `harmonik crew` commands and `--remote-control` bracketed-paste seeding.
- `harmonik keeper`: context-window watcher emitting warn / clear / reseed lifecycle events; `--warn-pct`, `--act-pct`, `--respawn-cmd` flags.
- AR-006 ZFC sensor: mechanism-tagged requirements must not carry non-zero `llm-freedom`.

### Fixed

- Daemon: pre-close `.br_history` trim + `BrUnavailable`-as-success after merge (hk-hypbi).
- Keeper: scope `statusLine` to tmux session; remove hardcoded agent name from global settings.
- DOT cascade: extend reviewer no-verdict retry to iter-2+ stalls (hk-ycxfa).
- Daemon: set `HARMONIK_AGENT=impl-<runID>` env var on implementer spawn (hk-4hk).
- Keeper: reject UUIDv7 gauge SIDs to fix clear→resume latch race (hk-lap).
- Keeper: self-heal stale UUIDv7 in `.managed` on read (hk-6mp).
- Tests: use short `/tmp` base for socket-path tests on macOS.

---

## [0.1.0] — 2026-06-09

> First pre-release. Released **by hand** via `gh release create v0.1.0` at commit `f571ca89`.
> The goreleaser pipeline was not yet active for this release; see `[Unreleased]` above for the pipeline implementation in progress.

### Added

- Persistent daemon architecture: single daemon per project enforced by pidfile lock (`hk-li14r`); dispatches beads concurrently in isolated git worktrees; merges to `main` one-at-a-time to prevent conflicts.
- Queue model: `wave` and `stream` group kinds; mid-flight appends via `harmonik queue append`; idle-wake channel (`QueueStore.WakeCh()`) fires immediately on submit/append.
- Review-loop workflow mode: agent-reviewer runs on every non-trivial bead commit; `Reviewed-By:` / `Review-Verdict:` trailers enforced via pre-commit hook.
- Beads integration (BI-024): `internal/brcli` adapter with `BeadsVersion` pin; daemon startup fails (exit 8) on version mismatch.
- `harmonik subscribe`: NDJSON event stream with server-side heartbeat for monitoring daemon progress from an agent.
- `harmonik comms`: multi-agent message bus (send / recv / who / log) with at-least-once delivery and dedup-on-event_id; replaces file-based `AGENT_COMMS.md` outboxes.
- DOT-mode: declarative orchestration templates with `commit_gate` shell nodes; shell nodes inherit the daemon's full environment so `PATH` and `go` are available.
- Reconciliation subsystem: startup investigator with 6-category rule table; verdict-execution is durable and idempotent (RC-025).
- Named-queue architecture: per-queue workers under a global concurrency cap; `harmonik queue set-concurrency N` adjusts the ceiling live.
- Integration-branch deployment: `--target-branch`, `--protect-branch`, `--forbid-default-main` flags; `.harmonik/branching.yaml` config file read at daemon startup.
- Binary commit-hash stamp: `cmd/harmonik/version.go` injected via `-ldflags "-X main.commitHash=..."` and forwarded to `daemon_started` event payload (`specs/event-model.md §8.7.1`).
- Supervisor keepalive script (`scripts/hk-keeper.sh`): keeps daemon alive in a `hkdkeeper` tmux session, strips `ANTHROPIC_API_KEY`, revives on process absence.
- `harmonik crew`: captain + named crew sessions with `--remote-control` bracketed-paste seed; `crew stop` / `crew start` for context-clear/reseed cycle.
- Post-commit watchdog: 60 s grace after commit-detection + `/quit` before force-kill, preventing infinite `sess.Wait` stalls (hk-5s7tg).
- tmux `new-window` 60 s timeout: bounded hung-window detection emitting `tmux_new_window_timeout` event instead of wedging a daemon slot indefinitely (hk-r1rup).
- `harmonik queue dry-run`: validate a `QueueSubmitRequest` JSON file without persisting; reports ledger-dep deferrals.

### Spec

- `specs/release-pipeline.md`: normative 4-stage release pipeline contract (CREATE → VALIDATE → CERTIFY → ROLLBACK), semver rules, ledger schema, event types. Implementation in progress; gaps listed in §9.
- `specs/event-model.md §8.7.1`: `daemon_started` payload with `binary_commit_hash` field.
- `specs/beads-integration.md §4.8`: `BeadsVersion` compat window (BI-024 / BI-026).
