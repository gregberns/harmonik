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

---

## Crew context management

**SESSION-KEEPER NOT DEPLOYED FOR CREWS (first seen 2026-06-09, ongoing).**
Symptom: when a crew's context fills (~200k tokens), the pane stops accepting keystrokes — `harmonik keeper` auto-clear/reseed cycle does NOT fire because the statusLine hook is not wired.
Resolution: manual restart — `harmonik crew stop <name>` followed by `crew start <name>` with a fresh mission file. Full enablement procedure: `docs/retro/2026-06-10/A6-session-keeper-enable.md`. Decision to defer: `docs/captain-restart.md §Current deployment state`. Refs: hk-ekap1, hk-njetn.

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

## DOT-mode issues

**DOT COMMIT_GATE SHELL NODE: `go` COMMAND NOT FOUND / EXIT 127 (first seen 2026-06-08, fixed hk-m5axg).**
Symptom: DOT-mode `commit_gate` shell nodes failed with `go: command not found` (exit 127), triggering fix-loop and eventually `run_stale`; `go` is not on `PATH` inside the shell node's subprocess.
Resolution: Fixed in `internal/daemon/dot_cascade.go` by inheriting the daemon's full environment (`append(os.Environ(), env...)`) when spawning shell nodes, so `PATH` (and all other env vars) propagate correctly.
