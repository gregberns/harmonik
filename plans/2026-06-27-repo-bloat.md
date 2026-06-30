# Repo Bloat Investigation — Working Tree at ~13 GB

**Date:** 2026-06-27
**Problem:** The working tree at `/Users/gb/github/harmonik` is **~13 GB** (`du -sh .` = 13G; UBS measured 12,726 MB after its own ignore rules). UBS refused to scan because it copies the scan target to `/tmp` before analysis and 12.7 GB blows past its 1000 MB safety limit. A normal Go repo this size (~2,100 `.go` files, ~24 MB of `internal/`) should be a few hundred MB. **62% of the bloat is a single directory: `.beads/.br_history-archive` (8.0 GB).**

> This is a **read-only handoff doc**. Nothing here was deleted, moved, gitignored, or fixed. It documents what is big, why, and what a future cleanup agent should investigate. **The harmonik fleet is LIVE while this was written** (keepers for `admiral`/`thufir` running, `comms recv --follow` for `leto` running, daemon worktrees active) — any cleanup must account for live daemon/agent processes.

---

## 1. Size table (everything ≥ 10 MB, sorted desc)

| Path | Size | Tracked in git? | Notes |
|---|---|---|---|
| `.beads/.br_history-archive/` | **8.0 GB** | No (ignored via `.beads/*`) | **THE problem.** 8,310 files, mostly ~4.5 MB `issues.*.jsonl.archived-*` snapshots, spanning 2026-06-11 → 2026-06-26 (15 days) |
| `.harmonik/worktrees/` | 2.4 GB | No (ignored via `/.harmonik/*`) | ~40 daemon **run worktrees** (`run/019e…`), each a full repo checkout. Operational state, daemon-owned |
| `.claude/worktrees/` | 2.0 GB | No (ignored) | 35 **orchestrator-spawned agent worktrees** (`agent-a…`), each a full checkout. Mostly orphaned/stale |
| `.git/` | 133 MB | n/a | Healthy: `size-pack` 60 MB, 45,818 objects in-pack. **Not a clone problem** (see §3) |
| `.tools/` | 104 MB | No (ignored via `/.tools/`) | Dev-tool binaries: golangci-lint 55M, lefthook 19M, govulncheck 14M, gci 11M, gofumpt 5.8M |
| `.kerf/` | 97 MB | Partial | `diagnostics-cache.json` 53 MB + `works/` 45 MB. `.kerf/*` ignored except project-identifier/jigs |
| `.beads/.br_history/` | 77 MB | No | 35 live (non-archived) history snapshots |
| `.beads.bak.1781144490/` | 75 MB | No (ignored via `.beads.bak*/`) | Ledger backup dir from 2026-06-10 (ledger-archive-runbook rollback artifact) |
| `.harmonik/events/` | 53 MB | No | `events.jsonl` typed-event log (workflow state) |
| `.beads/.br_recovery/` | 52 MB | No | 8 recovery snapshots |
| `internal/` | 24 MB | Yes | Legitimate Go source |
| `.beads/beads.db` | 23 MB | No (ignored) | SQLite task ledger (live) |
| `daemon.test` | 20 MB | No (ignored via `*.test`) | Compiled Go test binary at repo root |
| `plans/` | 18 MB | Yes | Planning docs incl. `005_workflow_modes/source/.history/` snapshots |
| `scenario.test` | 16 MB | No (`*.test`) | Compiled test binary at root |
| `harmonik` | 15 MB | No (ignored via `/harmonik`) | Built main binary at root |
| `.beads/issues.db` | 10 MB | No | Second SQLite DB |
| `.pi/extensions/flywheel/node_modules/` | 271 MB | No (ignored) | **UBS DOES ignore node_modules**, so this 271 MB is excluded from UBS's 12.7 GB |
| `hooksystem.test` | 8.8 MB | No (`*.test`) | Compiled test binary |
| `docs/` | 8.6 MB | Yes | Knowledge base |
| `harmonik-twin-claude` / `twins/generic-twin` | 4.7M / 4.3M | No (ignored) | Built twin binaries |
| external: `/Users/gb/github/hk-release-prep` | 62 MB | n/a | Linked git worktree OUTSIDE the repo dir (`release-infra` branch) — does NOT count toward the 13 GB but is an orphan-worktree candidate |

**Accounting:** the items above sum to ~13 GB, matching `du -sh .`. UBS's 12.7 GB ≈ 13 GB minus the ~271 MB of node_modules it strips. **The 8.0 GB archive alone is the entire overage** — remove it and the repo drops to ~5 GB; remove it plus the two `worktrees/` dirs and it drops to ~600 MB.

---

## 2. Tracked vs untracked

**Good news: git tracking is NOT the problem.** Only 3,713 files are tracked; the largest tracked files are normal source/spec/markdown (top: `internal/daemon/workloop.go` 332 KB, `specs/execution-model.md` 256 KB). The `.gitignore` already excludes every big offender: `.beads/*`, `.beads.bak*/`, `/.harmonik/*`, `/.claude/worktrees/`, `/.tools/`, `*.test`, `/harmonik`, `.pi/extensions/*/node_modules/`, `.kerf/*`.

So **the bloat is pure working-tree disk that is correctly git-ignored** — it never enters a clone. The problem is exclusively (a) local disk consumption and (b) tools like UBS that copy/scan the working tree without honoring `.gitignore`.

`git check-ignore` confirms `.beads/.br_history-archive`, `.claude/worktrees`, `.tools`, `daemon.test`, `harmonik` are all ignored. (`.harmonik`, `.pi`, `.kerf` return empty only because the *directory* isn't ignored — their *contents* are, via `/.harmonik/*` etc.)

---

## 3. Git-history bloat — minimal, not a clone problem

`.git` is 133 MB on disk; `git count-objects -vH` reports `size-pack: 60 MB`. The largest historical blobs are all `.beads/issues.jsonl` at ~3.1–3.4 MB each (it was tracked historically before being gitignored — see the `.gitignore` comment referencing hk-yru). That churn is baked into the 60 MB pack but is modest. **No giant accidental commit; clones are fine.** History-rewrite (filter-repo/BFG) is NOT warranted unless someone wants to reclaim the ~20–30 MB of old `issues.jsonl` revisions — low priority.

---

## 4. Categorized findings

### A. Operational state — DO NOT scan, DO NOT blindly delete (daemon/agent-owned, live)
- **`.harmonik/worktrees/` (2.4 GB)** — live daemon run worktrees. The daemon GCs these; manual deletion risks killing in-flight runs. Let the daemon reap, or only remove worktrees for runs confirmed terminal.
- **`.beads/beads.db` + `issues.db` + `issues.jsonl` (≈37 MB)** — live task ledger. Never touch.
- **`.harmonik/events/` (53 MB)** — `events.jsonl` is authoritative workflow/liveness state. Keep.
- **`.tools/` (104 MB)** — dev-tool binaries; cheap to reinstall but harmless. Keep; just exclude from scans.
- **`.pi/extensions/flywheel/node_modules/` (271 MB)** — npm output; UBS already ignores it.
- **`.kerf/works/` (45 MB)** — kerf process artifacts (partial mirror of the global bench). Keep.

### B. Cruft to clean (genuine accumulation — the real cleanup targets)
- **`.beads/.br_history-archive/` (8.0 GB, 8,310 files)** — **the headline target.** This is `br`'s archive of archived ledger snapshots, ~4.5 MB each, accumulating ~one every few minutes for 15 days with NO retention/pruning. Every `br sync`/flush appears to snapshot the full ledger and the archive never prunes. Investigate:
  - `br --help` / `br sync --help` / `br history --help` for a built-in retention/prune/compaction command (**preferred** — let `br` own its own data).
  - Whether anything reads `.br_history-archive` (grep harmonik source for `br_history-archive`); if it's write-only audit cruft, snapshots older than N days are deletable.
  - Why a new ~4.5 MB snapshot is written so frequently (flush-on-every-write amplification?).
  - **Risk: LOW-MEDIUM.** These are timestamped, already-archived copies — not the live DB. But the fleet is live and `br` may assume the dir exists; prefer a `br`-native prune, else delete by mtime (e.g. older than 2–3 days) rather than `rm -rf` the whole dir.
- **`.claude/worktrees/` (2.0 GB, 35 worktrees)** — orchestrator-spawned agent worktrees, most orphaned. Investigate with `git worktree list` (already captured: 35 `agent-a…` entries; one is `locked`, a few are `detached HEAD`, some on `run/…` or named fix branches). Clean via `git worktree prune` + `git worktree remove` for entries whose branch is merged/abandoned. **Risk: MEDIUM** — some may belong to in-flight agents; cross-check against running processes/sessions before removing. The `locked` one (`agent-a974b0efc2bf94289`) is intentionally retained — leave it.
- **`.beads.bak.1781144490/` (75 MB)** — a 2026-06-10 ledger rollback backup. If the rollback it guarded is long settled, deletable. **Risk: LOW.**
- **`.kerf/diagnostics-cache.json` (53 MB)** — a regenerable cache. **Risk: LOW** (kerf rebuilds it).
- **Root build artifacts (≈64 MB):** `daemon.test` 20M, `scenario.test` 16M, `harmonik` 15M, `hooksystem.test` 8.8M, `harmonik-twin-claude` 4.7M. All gitignored; `make clean` / `go clean` territory. **Risk: LOW.**
- **`.beads/.br_history/` (77 MB) + `.br_recovery/` (52 MB)** — live-ish history/recovery snapshots; smaller siblings of the archive. Likely covered by the same `br` retention fix. **Risk: LOW-MEDIUM** — confirm `br` doesn't need recent recovery snapshots before pruning.
- **`.harmonik/daemon-boot.log` (1.3 MB) + `daemon.boot.log` (708 KB)** and `queue.json.cancelled-*` files — small but stale; sweep opportunistically.

### C. Git-history bloat
- None material (see §3). Optional, low-priority: history-rewrite to drop old tracked `issues.jsonl` revisions (~20–30 MB). Not recommended while the fleet is active.

### D. Needs human / operator decision
- Is `.beads/.br_history-archive` **safe to purge wholesale**, or must a retention window be preserved? (Operator owns the ledger-archive runbook referenced in `.gitignore`.) The durable fix is a retention policy, not a one-time delete.
- The external linked worktree `/Users/gb/github/hk-release-prep` (`release-infra` branch, 62 MB) — still needed, or prune?
- Should the root build artifacts be moved under `/bin` (already gitignored) so `go build` stops littering the repo root?

---

## 5. What to put in `.gitignore` and `.ubsignore`

**`.gitignore`:** already correct — every large dir is ignored. **No changes required** for bloat purposes. (Optional hygiene: it does not currently ignore the root-level `*.test` by an explicit comment for `daemon.test`/`scenario.test`/`hooksystem.test`, but `*.test` already covers them.)

**`.ubsignore` (CREATE — this is the actual gap):** UBS only ignores `.venv`/`node_modules` by default, so it does NOT exclude `.beads`, `.harmonik`, `.claude`, `.pi`, `.tools`, `.kerf`, `twins`, build binaries, or backups — which is exactly why it measured 12.7 GB. A future agent should add a `.ubsignore` (and/or invoke UBS scoped to changed files / `internal/` only) covering:

```
# Operational state and caches — never scan
.beads/
.beads.bak*/
.harmonik/
.claude/worktrees/
.kerf/
.pi/
.tools/
twins/
harmonik-twin-claude/

# Built binaries / test artifacts at repo root
/harmonik
*.test

# Large planning/research history snapshots (optional — keeps scan fast)
plans/**/.history/
research/

# Standard
.git/
node_modules/
.venv/
```

With those excluded, the scannable surface drops to roughly `cmd/ + internal/ + specs/ + docs/` ≈ well under 100 MB, comfortably under UBS's 1000 MB ceiling. **The single most important line is `.beads/`** — it alone removes 8.2 GB.

In practice the right default UBS invocation here is `ubs $(git diff --name-only --cached)` or `ubs internal/ cmd/` — scope to changed files, never `ubs .` on this repo.

---

## 6. Open questions / needs operator decision

1. **`.br_history-archive` retention** — does `br` have a native prune/retention command, or does harmonik need to add a reaper? (8 GB and growing ~0.5 GB/day with no cap.) Operator decision on retention window.
2. **Why snapshots churn so fast** — a new ~4.5 MB full-ledger snapshot every few minutes suggests flush-on-every-write amplification; worth a root-cause look so the fix is durable, not a one-time sweep.
3. **`.claude/worktrees` pruning while fleet is live** — safe to `git worktree prune` now, or wait for a quiescent window? Need to confirm none of the 35 belong to a running agent.
4. **External `hk-release-prep` worktree** — keep or remove.
5. **`.beads.bak.1781144490` and `.br_recovery`** — confirm the rollback/recovery they guard is settled before deleting.

---

## Appendix — key commands used (reproducible)

```bash
du -sh ./* ./.[!.]* | sort -rh                 # top-level sizes
du -ah .beads/.br_history-archive | sort -rh    # the 8 GB dir
git worktree list                               # 35 .claude + ~40 .harmonik + 1 external
git check-ignore .beads/.br_history-archive …   # confirm ignored
git ls-files -z | xargs -0 du -h | sort -rh     # largest tracked files
git count-objects -vH                           # .git footprint (60 MB packed — healthy)
git rev-list --objects --all | git cat-file --batch-check … | sort -rn   # history blobs
pgrep -fl harmonik                              # fleet is LIVE
```
