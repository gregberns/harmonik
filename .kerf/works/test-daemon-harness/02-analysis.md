# Analyze — test-daemon-harness

> **Status note (2026-07-07):** This plan was opened 2026-06-25. Since then the
> three implementation beads it anticipated have ALL LANDED and closed. This
> analysis therefore documents the *as-built* territory, and the later passes
> scope only the genuine remaining gaps (verification-under-CI, durability,
> discoverability), not a greenfield build. Where a success criterion from
> `01-problem-space.md` is already met, that is called out explicitly.

## Affected areas (with file paths)

### 1. The harness driver — `scripts/scratch-daemon.sh` (LANDED)
The single load-bearing artifact. ~820 lines of `bash` (set -euo pipefail).
Subcommands, all implemented:

| Subcommand | Purpose | Fleet-safety posture |
|---|---|---|
| `init`   | clone repo → build → `harmonik init --project <scratch> --force --no-supervise` | `guard_path` scratch≠fleet |
| `build`  | `go build -C <scratch>` → `<scratch>/.harmonik/bin/harmonik` | guard_path |
| `up`     | `tmux new-session` running bare `harmonik --project <scratch>`; wait ≤45s for socket; strips `ANTHROPIC_*` keys (subscription-pool billing, codename:credfence) | `assert_not_supervised` + guard_path |
| `status` | print project/tmux/socket/pid/log state | read-only |
| `down`   | argv-verified PID kill (kills ONLY if `ps -o command=` contains the scratch path); tmux teardown gated on the SAME ownership proof; stale-socket rm | argv-ownership proof; guard_path; assert_not_supervised |
| `cycle`  | down → build → up (the fast inner loop) | inherits |
| `batch`  | submit named batch to scratch queue, await terminal events, emit structured pass/fail | targets ONLY the scratch socket |
| `feedback` | scratch FAILURES → deduped OPEN beads on the FLEET beads DB | the ONE deliberate fleet write |

Landed commits (git log on the file): `a720e63b` (init/build/up/down/cycle +
runbook, hk-4tdlw), `7612ee06` (reviewer fixes), `95702bcb` (batch, hk-6vr02),
`da5a6596` (feedback, hk-1gkc8), `0c3750f9` (end-to-end smoke, hk-6eqv9).

### 2. The verification harness — `scripts/scratch-daemon-smoke.sh` (LANDED, hermetic-only by default)
Self-contained smoke. Default phases run always and are HERMETIC (no live
daemon, no claude, no API):
- **A / A2** — `fail_signature` stability + normalization guards: two synthetic
  streams for the *same* logical failure with different volatile tokens fold to a
  byte-identical signature; two *distinct* vet failures stay distinct (no
  false-merge) — proven both at the signature level and at the bead level
  (created=2).
- **B** — trivial 1-bead pass batch via the offline `--from-events` fold; asserts
  the JSON artifact + `BATCH_SUMMARY`.
- **C** — `feedback` CREATE against a THROWAWAY git+beads fleet
  (`SCRATCH_FEEDBACK_FLEET_ROOT`), never the live DB.
- **D** — cross-run dedup: re-run + a different run of the same failure both
  UPDATE the one bead (created=0); fleet ends with exactly one feedback bead.

Gated phases (only with `--full` / `SMOKE_SCENARIO_RUN=1`, need a real claude +
passwordless ssh localhost):
- **E–F** — real daemon lifecycle + the remote-substrate localhost e2e scenario
  executed for real. By default these only assert the scenario test *compiles*
  (`go test -tags=scenario -run '^$'`), not that a live batch ran.

### 3. The offline fold test seam — `batch --from-events <ndjson>` (LANDED)
The `oneline` jq function (redacts volatile tokens: ISO-8601/epoch timestamps,
UUIDs, hex addresses, goroutine ids, worktree-agent/run-/wt-ids, pids/ports,
duration literals, git SHAs, tmpdir roots — path *tail* preserved) is the single
shared definition used by BOTH the live subscribe fold and the offline fold. It
is the correctness core of the dedup guarantee.

### 4. Documentation (LANDED)
- `docs/scratch-daemon-runbook.md` — the command reference (every subcommand,
  flag, env var, guard). Declares the script the source of truth.
- `docs/remote-test-daemon-methodology.md` — the "why / how-to-reason" field
  manual layered on the runbook.
- `docs/known-workarounds.md` — references scratch-daemon.

### 5. Interfaces the harness consumes (must be preserved)
- `harmonik project-hash --project <p>` — deterministic 12-hex SHA-256 of
  `realpath` (PL-006a); the tmux session identity `harmonik-<hash>-default` and
  the isolation guarantee both hang off it.
- `harmonik queue submit --project <p> --queue <name> [--beads csv | <file>] --json`
  — the named-queue submit route. The in-file `queue` field is IGNORED, so the
  harness always passes `--queue` explicitly.
- `harmonik subscribe --socket <sock> --types run_started,run_completed,run_failed`
  — the event stream `batch` folds. Event payload shape: `run_started` carries
  `payload.run_id` + `payload.bead_id`; terminal events carry `payload.run_id`,
  `payload.success`, `payload.summary`.
- `br create/list/update/comments add --json` (run with the fleet repo as CWD in
  an isolated subshell) — the feedback write surface.

## Constraints imposed by the existing code (load-bearing, must be preserved)
- **Never touch the fleet daemon.** Four guards are load-bearing and any new
  subcommand MUST extend them: (1) `guard_path` scratch≠fleet refusal
  (symlink-resolved both sides); (2) `assert_not_supervised` refusal on a live
  `hk-<hash>-supervise` session; (3) argv-ownership proof before any kill; (4)
  tmux teardown gated on that same proof.
- **The daemon owns terminal bead transitions.** `feedback` creates beads OPEN,
  NEVER `--assignee`, NEVER `status=in_progress` (project HARD rule; honored).
- **Dedup key stability.** `prov:<hash>` = `sha256(batch-name 0x1f fail_signature)[:12]`;
  queue_id deliberately excluded so re-runs UPDATE. A CLOSED bead is intentionally
  NOT reused (a recurrence after resolution files fresh).
- **Standalone-and-disposable is the point** — no supervisor / auto-revive
  (bootstrap-trap avoidance, per MEMORY: daemon self-fix bootstrap trap).

## Conventions to follow
- Bash: `set -euo pipefail`, `die()` fail-loud with `[scratch-daemon]` prefix, one
  safety banner per invocation, bash-3.2 compatible (no associative arrays — the
  in-invocation dedup uses a space-padded string set).
- Stable grep-able stdout contracts (`BATCH_ITEM`/`BATCH_SUMMARY`/`FEEDBACK_ITEM`/
  `FEEDBACK_SUMMARY`, tab-separated) so downstream steps parse deterministically.
- Scenario tests: `-tags=scenario` build tag, `internal/daemon/scenario_*_test.go`,
  gate helper `internal/daemon/scenariogate.go`, harness `scenariotest/`.
- Feedback beads: `codename:test-daemon-harness` + `scratch-feedback` + `prov:<hash>`.

## Code-health / gaps relevant to the remaining work
1. **No CI/regression gate on the smoke.** `scratch-daemon-smoke.sh` exists and is
   hermetic-by-default (fast, no secrets) but nothing runs it automatically. The
   `oneline` redaction corpus is intricate and rots silently if a new event format
   introduces an un-redacted volatile token → false-merge or false-split of real
   bugs. This is the highest-value gap.
2. **Live end-to-end never proven by default.** Success criterion "demonstrated
   end-to-end … on the remote-substrate scenario batch" is met only in the OFFLINE
   fold sense; the live `init→up→batch(real bead)→feedback→down` path is gated
   behind `--full` + a real claude binary and has no recorded green run.
3. **No `make` target for the scratch smoke.** `make smoke-scratch` runs the
   *harmonik* smoke, not the *scratch-daemon* smoke — discoverability gap.
4. **No disk hygiene.** A durable standing capability accumulates worktrees/
   artifacts in the scratch clone across batches; no `gc`/`reset` subcommand.
   Disk<10GiB is a known merge-failure trigger (MEMORY: disk-pressure cache wipe).

## Recent git activity in the area
All five commits above landed 2026-06-25→26; no changes to the file since. The
adjacent scripted-twin work (recent commits `89301101`/`168952e4`/`676e714d`) is
the PARALLEL Phase-2 lane and does not touch this script — the two lanes are
independent by design.
