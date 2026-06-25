# Known Workarounds

Dated entries for recurring operational issues. Each entry names the version it was first observed, the symptom, and the resolution. Extracted from HANDOFF.md to keep session state lean.

---

## Worktree issues

**WORKTREE BEADS-JSONL STALE-AT-FORK (first seen v48).**
Symptom: merge conflict on `.beads/issues.jsonl` when merging a worktree branch.
Resolution: `git checkout --theirs .beads/issues.jsonl`

**WORKTREE TASK-INJECTION LEAK (first seen v36, ongoing).**
Symptom: stale task-injection state leaks across worktree merges.
Resolution: Stash before merge.

**WORKTREE AUTO-REMOVED BY HARNESS (first seen v41).**
Symptom: worktree directory is gone by the time orchestrator tries to read it.
Resolution: The branch survives; merge directly from the branch ref.

**WORKTREE-REMOVE STEALS CWD (first seen v45).**
Symptom: shell CWD becomes invalid after daemon removes a worktree.
Resolution: Prepend `cd /Users/gb/github/harmonik` to all subsequent commands; prefer `git -C /Users/gb/github/harmonik` for git ops.

**WORKTREE BEADS-JSONL LEAK (first seen v41).**
Symptom: `.beads/issues.jsonl` changes from a worktree appear in a rebase.
Resolution: Stash before rebase; never pop the stash.

**ISOLATED-WORKTREE STALE-BASE BUG (first seen v35, ongoing).**
Symptom: code read from a worktree reflects an old base — changes from main are not visible.
Resolution: Rebase the worktree branch onto current main before reading code.

---

## Daemon keep-alive

**DAEMON CRASH-LOOP / EXIT-17 (first seen v54, ongoing).**
Symptom: harmonik daemon exits unexpectedly; queue stalls; `harmonik queue status` returns exit 17.
Resolution: Use `scripts/hk-keeper.sh` (checked in, canonical) to keep the daemon alive:

```bash
# From the repo root (parameterized — defaults to CWD, concurrency=6):
./scripts/hk-keeper.sh [/path/to/project] [max-concurrent]
# Or via env vars:
HK_PROJECT=/path/to/project HK_CONCURRENCY=4 ./scripts/hk-keeper.sh
```

The script runs *outside* tmux, launches the daemon in a `hkdkeeper` tmux session (not `harmonik-*` prefixed so orphan sweeps can't kill it), strips `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN` (subscription-billed), and revives on process absence. Do NOT `rm` the socket while the daemon is live — only the keeper does that after confirming the process is gone.

**TMUX NEW-WINDOW TIMEOUT (fixed hk-r1rup, commit ec30b225).**
2026-06-08: tmux new-window calls are now bounded by a 60s timeout (hk-r1rup, commit ec30b225). A hung tmux new-window now emits a tmux_new_window_timeout event + returns ErrStructural to reopen the bead, instead of wedging a daemon slot at launch_stall forever (was the hk-9vp51 residual no-spawn wedge).

**WHEN DISPATCH HANGS (manual recovery — rare; most hang classes auto-recover).**
The pasteinject quit-on-commit hang (hk-trjef, `internal/daemon/pasteinject.go:146-208`) and the post-commit `/quit` watchdog (hk-5s7tg) are **auto-recovered in the daemon** — you should rarely intervene. For other hangs:
1. Identify the stuck `run_id` from `.harmonik/queue.json` or the worktree listing.
2. `git -C .harmonik/worktrees/<run_id> log --oneline -3` — if a `Refs:` commit exists, work was done; the daemon is stuck on a later step (merge, reviewer, push).
3. Tail `.harmonik/events/events.jsonl` filtered by `run_id` — which event types fired, which expected ones did not?
4. If the implementer claude already exited but the daemon is hung: kill the daemon PID (`pkill -f "harmonik --project"`), ff-merge the worktree branch by hand, push, close the bead, then re-start the daemon. File a friction bead with the missing-event signature.

> SAFETY: `pkill -f "harmonik --project"` matches **every** harmonik daemon, including a scratch test-daemon (below). When more than one daemon is running, kill by the exact PID from `<project>/.harmonik/daemon.pid` instead — never a blanket pkill.

---

## Fast iteration via a scratch-clone standalone daemon

**THE WIN: a real-daemon reproducer round-trip drops from ~30 minutes to seconds.**
Instead of churning the production ("fleet") daemon — or your main checkout's git
history — to test ANY daemon change, run a SECOND, fully-isolated harmonik daemon
on a separate git clone via `scripts/scratch-daemon.sh`, then `cycle` (down → build
→ up) it in one command, drive a `batch` of beads through it, and `feedback` any
failures back as fleet beads. The fleet daemon is never touched.

```bash
# The fast inner loop (drive from your fleet checkout; pass the scratch path):
./scripts/scratch-daemon.sh init   /tmp/hk-scratch   # one-time clone + init + build
./scripts/scratch-daemon.sh up     /tmp/hk-scratch   # start standalone daemon (no supervisor)
./scripts/scratch-daemon.sh cycle  /tmp/hk-scratch   # after each edit: down → build → up
./scripts/scratch-daemon.sh batch  /tmp/hk-scratch smoke --beads hk-test001   # run + verdict
./scripts/scratch-daemon.sh down   /tmp/hk-scratch
```

**Full runbook — including the `batch` / `feedback` output contracts, the minimal
`--file <queue.json>` format, two worked examples (a trivial 1-bead batch and the
remote-substrate scenario batch), and the four-layer safety guarantee — lives in
[docs/scratch-daemon-runbook.md](scratch-daemon-runbook.md).**

**SAFETY (summary).** The script NEVER touches the fleet daemon: `down` kills ONLY
the PID whose argv contains the scratch path (refuses empty/`/`/stale/mismatch); the
tmux teardown is gated on that same ownership proof; `guard_path` refuses `/` and the
script's own repo root (symlink-resolved, so a symlink-to-fleet is caught); and
`up`/`down`/`batch` refuse a project with a live `hk-<hash>-supervise` session. It
never runs a blanket `pkill harmonik`. The ONE deliberate fleet write is `feedback`,
which creates/updates OPEN beads (never claims) on the fleet ledger on purpose. If you
ever stop a daemon by hand while a scratch daemon is also running, kill by the exact
PID from the relevant `.harmonik/daemon.pid`, not by name.

---

## Crew context management

Crew keepers are auto-armed by the daemon on `crew start`: `HandleCrewStart → SpawnCrewSession` adds a sibling `keeper` window running full force-cut mode (hk-rmy1, hk-lcga, hk-tt9q). Run `harmonik keeper doctor --agent <name>` to confirm any crew is armed and the watcher is live. If doctor reports no live watcher: `harmonik crew stop <name>` then `crew start <name>` re-arms it.

---

## Harness issues

**HARNESS BLOCKS `.md` WRITES FOR SUB-AGENTS (first seen v47).**
Symptom: sub-agent Write tool calls on `.md` files are blocked by the harness.
Resolution: Orchestrator must persist markdown files via its own Write tool; do not delegate `.md` writes to sub-agents.

---

## Release process

**MANUAL RELEASE ESCAPE HATCH — pre-goreleaser CI (as of v0.1.0, 2026-06-10).**
Symptom: `.goreleaser.yaml` and the tag-triggered CI workflow do not yet exist. Automated VALIDATE and CERTIFY stages cannot run.
Resolution: cut releases manually until the goreleaser CI workflow lands (`specs/release-pipeline.md §9`).

Steps:

```bash
VERSION=v0.y.z
COMMIT=$(git rev-parse HEAD)

# 1. Build for current platform (darwin/arm64 example):
go build \
  -ldflags "-X main.commitHash=${COMMIT} -X main.version=${VERSION}" \
  -o harmonik ./cmd/harmonik

# 2. Verify --version output matches the spec:
./harmonik --version
# Expected: harmonik v0.y.z (commit: <sha>)

# 3. Create a GitHub pre-release:
gh release create "${VERSION}" ./harmonik \
  --title "${VERSION}" \
  --notes "$(awk "/^## \[${VERSION#v}\]/{found=1; next} found && /^## \[/{exit} found{print}" CHANGELOG.md)" \
  --prerelease

# 4. Manually run VALIDATE gates (CI Tier 2 + scenario suite):
make check-full
go test -tags=scenario ./tests/scenarios/...

# 5. If all gates pass, promote to stable (CERTIFY):
gh release edit "${VERSION}" --prerelease=false

# 6. Add ledger entry to internal/release/manifest.go by hand:
#    Append a ReleaseEntry{Semver, CommitHash, Tag, Prerelease: false, CertifiedAt: "<RFC3339>"}
#    Commit: chore(release): certify v0.y.z (Trivial: true)
```

Manual releases skip the automated per-gate failure reporting. Document any gate that was skipped or run manually in the CHANGELOG entry and commit body.

---

## Salvage-promote: recovering context_cancelled runs

**CONTEXT_CANCELLED RUN WITH A COMMITTED SHA — SALVAGE VIA PROMOTE, NOT RE-DISPATCH.**

Symptom: `.harmonik/events/events.jsonl` shows `run_failed` with `failure_class: context_cancelled` for a bead, but `git log run/<run_id>` (or the bead's worktree branch) shows a `Refs: <bead_id>` commit — the implementer completed its work before the session was cancelled.

**Do not re-dispatch.** The work is done and the SHA is immutable. Re-dispatching bills another Opus session (~30–50 min) for work already completed.

**Recovery procedure:**

```bash
# 1. Locate the committed SHA on the run-branch:
git log --oneline run/<run_id> | head -5
# look for the commit bearing "Refs: <bead_id>"

# 2. Verify completeness (build + tests must pass):
git stash                            # if needed
git checkout <sha>                   # or use --detach
go build ./...
go test -short ./...
git checkout -                       # return to previous branch

# Optional: byte-identity check across independent runs.
# If two independent runs produced the same commit content (e.g. hk-rai2:
# b86a9ea2 == 4a62ff8e, identical 291 lines), that is proof-of-determinism
# and a single review suffices — no second review needed.

# 3. Promote the SHA onto the target branch (race-safe, 3 non-ff retries):
harmonik promote --project $HARMONIK_PROJECT <sha>

# 4. Close the bead once promote succeeds (or let the daemon's reconciler do it):
br close <bead_id> --reason "Salvaged: context_cancelled; SHA promoted via harmonik promote"
```

**Safety properties:**
- The run-branch SHA is immutable — the implementer's output can be audited at any time.
- Byte-identity across independent runs proves determinism; single-review is sufficient for salvage.
- `harmonik promote` is race-safe (cherry-pick `-x` + up to 3 non-ff rebase retries, build gate).
- This is **not a gate-bypass** — the commit still carries the `Refs:` trailer and goes through the same build gate as a normal daemon merge.

**When push-mode is refused (exit 5):** the target branch is in `protect_branches`. Use `harmonik promote --pr` to open a PR instead of pushing directly.

---

## DOT-mode issues

**DOT COMMIT_GATE SHELL NODE: `go` COMMAND NOT FOUND / EXIT 127 (first seen 2026-06-08, fixed hk-m5axg).**
Symptom: DOT-mode `commit_gate` shell nodes failed with `go: command not found` (exit 127), triggering fix-loop and eventually `run_stale`; `go` is not on `PATH` inside the shell node's subprocess.
Resolution: Fixed in `internal/daemon/dot_cascade.go` by inheriting the daemon's full environment (`append(os.Environ(), env...)`) when spawning shell nodes, so `PATH` (and all other env vars) propagate correctly.

---

## Orchestrator gotchas (ex-AGENT_OPERATING_MANUAL)

Five hard-won operational failures, relocated here from the retired AGENT_OPERATING_MANUAL.

### Gotcha 1 — ENV-STRIP / BILLING

**Symptom:** API credit consumed in ~2 hours with no obvious cause (2026-05-30 incident — all credit gone in ~2h).

**Cause:** `ANTHROPIC_API_KEY` was present in a repo `.env` file that `harmonik --project` auto-sourced. Daemon-spawned claude sessions inherit the parent environment. An inherited API key makes claude bill pay-per-token API instead of the Max subscription.

**Fix:**
- Never put `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` / `CLAUDE_CODE_OAUTH*` in a repo `.env` that a bare `harmonik --project` daemon can inherit.
- The credential deny-list in the daemon scrubs these keys from every daemon-spawned claude. Only `harmonik supervise start` reads `.env` and injects the key into Pi (the flywheel cognition process).
- Always start the daemon with `--no-auto-pull`.

### Gotcha 2 — WIDE WAVES (disk + CPU exhaustion)

**Symptom:** builds crawl, `run_stale` false alarms fire at ~10 min, eventual `no space left on device`.

**Cause:** `--max-concurrent ≥ 6` exhausts disk (each isolated worktree ≈ 26 MB; dozens add up fast) and oversubscribes CPU (each implementer runs multi-core `go build`/`go test`; 8–10 wide makes every bead crawl).

**Fix:**
- Throughput knee is **4–5 wide** on a 10-core box. Start there.
- Change a live daemon's ceiling without restart: `harmonik queue set-concurrency N`.
- Biggest safe disk reclaim: `go clean -cache` (~12 GB freed in the incident).
- Before `go install`, always `git fetch && git reset --hard origin/main` — the daemon pushes per-bead merges but your local `main` lags. Rebuilding from stale `main` silently ships a daemon WITHOUT the just-landed fix.

### Gotcha 3 — EPIC-DEP BLOCKS DISPATCH

**Symptom:** `harmonik queue submit` shows `group_failure`, `failed > 0`, but ZERO `run_started` events — no implementer ever launches.

**Cause:** `br dep add <task> <epic>` makes the task blocked-by the OPEN epic. The daemon silently insta-fails dispatch for any task with an open blocker — no implementer spawns, no log, just failure.

**Fix:**
- Attach a bead to its kerf work via the `codename:<name>` **label**, not an epic dependency.
- Example: `br label add hk-abc codename:productization` (not `br dep add hk-abc hk-epic`).
- To diagnose: `br show <id>` — look for `blocked_by` entries listing an open bead.

### Gotcha 4 — $TMUX REQUIRED

**Symptom:** `harmonik run` or `harmonik status` exits immediately with `"$TMUX is not set"` (exit 1). No daemon spawns.

**Cause:** harmonik hard-requires a tmux environment. Invoking from a plain shell (terminal not inside tmux, or a headless script) triggers this check.

**Fix:**
- Always wrap launches in a detached tmux session: `tmux new-session -d -s harmonik-daemon '...'`
- The persistent daemon is launched the same way.
- If running interactively, start inside a tmux window first.

### Gotcha 5 — STALE BINARY

**Symptom:** "but I already fixed that" — a known bug persists after you patched the code.

**Cause:** The running daemon is using the old binary. `go install` only updates the binary on disk; a running daemon doesn't reload.

**Fix:**
1. After any harmonik code change: `go install ./cmd/harmonik`
2. **Then restart the daemon** — kill its tmux session (`tmux kill-session -t harmonik-daemon`) or `pkill -f "harmonik --project"`, then wait for the supervisor to revive it (or relaunch manually).
3. Pair with Gotcha #2's reset-before-install: `git fetch && git reset --hard origin/main` first so you build from the latest merged code.
