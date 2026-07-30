# Parallel worktree runbook

This runbook permits isolated implementation under `/Users/gb/github/harmonik-wt`. The current filesystem
profile is unrestricted, and the existing L0 worktree proves the path is usable. The constraints are shared
Git metadata, disk/caches, machine-global test resources, and the four-agent ceiling—not sandbox access.

## 1. Campaign shape

- Coordinator remains in `/Users/gb/github/harmonik`.
- Every Git command names an absolute repository/worktree path with `git -C`.
- One agent owns one worktree, branch, file lease, and bounded commit.
- Maximum active agents: coordinator + three sub-agents.
- Maximum simultaneous Go builders at current disk headroom: **two**.
- Preferred steady state: one spine builder, one disjoint safety builder, one rotating reviewer/planner.

Successive waves provide scale: integrate a bounded lane, release its slot, and immediately refill it from
the non-overlapping priority queue.

## 2. Wave manifest and leases

Before creating worktrees, the coordinator records:

| Field | Meaning |
|---|---|
| wave/lane/task | stable human-readable identity |
| worktree path | `/Users/gb/github/harmonik-wt/<wave>-<lane>-<task>` |
| branch | `refactor/…`, `fix/…`, `test/…`, or `docs/…` |
| base SHA | exact integrated commit |
| leased files/globs | implementation, tests, shared helpers, generated/mirrored pairs |
| global files | `.golangci.yml`, Makefile, freeze/depguard scripts, registries |
| agent | sole writer |
| test class | targeted, race, fault, process, milestone |
| expected commit | one coherent reviewed change |

Agents do not edit the manifest. The coordinator is the sole owner of Git worktree/ref operations and lease
changes.

The plan/manifest must be committed with required review trailers before implementation worktrees depend
on it. An untracked plan is invisible from sibling worktrees and is not an executable source of truth.

Exclusive global leases include:

- run-path spine files;
- `.golangci.yml` and Makefile;
- shared freeze/depguard scripts;
- composition roots and registries;
- shipped-skill source/mirror pairs;
- package-level helpers whose symbols would collide across lanes.

An agent reports useful out-of-lease findings; it does not opportunistically fix them.

## 3. Creation

At the start of a wave:

```bash
set -e
test -z "$(git -C /Users/gb/github/harmonik status --porcelain)"
git -C /Users/gb/github/harmonik rev-parse HEAD
git -C /Users/gb/github/harmonik worktree list --porcelain

git -C /Users/gb/github/harmonik worktree add \
  -b <branch> \
  /Users/gb/github/harmonik-wt/<wave>-<lane>-<task> \
  <exact-base-sha>
```

All sibling lanes in one wave start from the same pinned base. Do not base a new lane on an unintegrated
sibling branch.

Never reuse a worktree path or branch name. Never create a second writer for an existing lease.
Non-building planner/reviewer worktrees may be created below the disk builder threshold.

## 4. Cache and disk policy

- Share the read-mostly module cache (`GOMODCACHE`).
- Give each live lane stable private `GOCACHE` and `GOLANGCI_LINT_CACHE` directories outside the repo.
- Do not create a fresh cache per command.
- Do not run `go clean -cache` while any builder is active.
- **Remove a lane cache when the lane ends.** Wait until review, integration, and final evidence
  capture are done — then delete it. This line used to say "only after", and every reader obeyed the
  restriction and skipped the obligation. Nothing else removes these directories: no code knows the
  path, so no reaper reaches it. Measured 2026-07-30 at **8.3 GiB, idle for four days, growing about
  3.7 GiB per day while lanes run** (`hk-99szy`).
- Pause new builders at or below 10 GiB available; seek stable headroom above the watermark before
  differential, race, or broad scenario runs.
- Follow `docs/disk-reclaim.md`; never hand-delete other sessions' state.

Example lane environment. Shell exports do not persist across separate tool calls, so every build/test/lint
command must use an `env` prefix or a persistent PTY:

```bash
set -e
HARMONIK_LANE_CACHE=/Users/gb/github/harmonik-wt-cache/<wave>-<lane>-<task>
mkdir -p "$HARMONIK_LANE_CACHE/go-build" "$HARMONIK_LANE_CACHE/golangci"

# Builder-token predicate: fail at or below 10 GiB available.
available_kib="$(df -k /System/Volumes/Data | awk 'NR == 2 {print $4}')"
test "$available_kib" -gt "$((10 * 1024 * 1024))"

env \
  GOCACHE="$HARMONIK_LANE_CACHE/go-build" \
  GOLANGCI_LINT_CACHE="$HARMONIK_LANE_CACHE/golangci" \
  go test ./internal/<package>/...
```

Redeclare `HARMONIK_LANE_CACHE` in the same shell invocation for every separate tool call, or use the
literal manifest path in each `GOCACHE=`/`GOLANGCI_LINT_CACHE=` prefix. Prefix `make`, lint, build, vet,
and test commands the same way. Record the cache path in the wave manifest.

**Tear the cache down when the lane closes**, and sweep for the ones earlier lanes left behind:

```bash
rm -rf -- "$HARMONIK_LANE_CACHE"                                   # this lane

# Anything under the shared root untouched for 2 days belongs to a dead lane.
find /Users/gb/github/harmonik-wt-cache -mindepth 1 -maxdepth 1 -type d -mtime +2 -print
# Re-run with -exec rm -rf {} + once the list looks right.
```

`scripts/with-isolated-gocache.sh` does **not** replace this. It builds one cache per command with
`mktemp` and removes it on exit, so it defeats the warm cache this section exists to keep, and it
isolates `GOCACHE` only — never `GOLANGCI_LINT_CACHE`. Use it for a one-shot gate, not for a lane.

The coordinator schedules builds. A third active implementer may read or edit while two builders compile,
but waits for a builder token before running Go/lint gates.

## 5. Test isolation

Worker lanes start with the smallest relevant test:

1. targeted package/unit test;
2. failure-injection, race, or real-process test required by the work class;
3. scoped build/vet/lint;
4. commit;
5. coordinator integration gates.

Tests must use:

- `t.TempDir` or lane-unique temp roots;
- ephemeral ports;
- lane-unique tmux/session/socket names;
- isolated worktrees/repos and fake or isolated external binaries;
- no primary daemon, live `.harmonik` socket/event state, or fixed shared `/tmp` paths.

Serialize tests that cannot avoid machine-global state. Do not run daemon-booting suites concurrently.

## 6. Agent return contract

Review/commit sequence:

1. builder prepares and tests the diff, then stops writing;
2. an independent reviewer uses the rotating slot to review the staged/uncommitted diff or candidate commit;
3. builder fixes findings and receives re-review where needed;
4. builder creates/amends the final commit with the review trailers;
5. coordinator validates the committed code.

The builder never self-reviews. An implementation agent then returns:

- branch and commit SHA;
- exact changed files;
- statement of lease compliance;
- targeted and broader test commands/results;
- UBS result on changed source files;
- production call site checked;
- known deferred runtime proof;
- independent review verdict and required commit trailers.

One lane should yield one coherent final commit. WIP commits are squashed before the final review/commit
handoff.

Every non-trivial commit uses an allowed conventional subject and includes:

```text
Reviewed-By: <reviewer>
Review-Verdict: {"schema_version":1,"verdict":"APPROVE",...}
```

Config/skill/instruction changes use the appropriate config reviewer.

## 7. Review and integration

The reviewer checks:

- diff against the pinned base;
- exact lease compliance;
- production composition call site;
- tests exercising the claimed behavior;
- no unrelated file or machine-local bead mutation;
- trailers and commit-message validation.

Integration choices:

1. If integrated HEAD still equals the pinned base, merge `--ff-only`.
2. If HEAD advanced through disjoint lanes, rebase the worktree branch onto the **local integrated branch**,
   compare the pre/post patch with `git range-diff` or patch IDs, obtain a fresh integration-relative review
   when composition context changed, rerun targeted gates, then merge `--ff-only`.
3. Use cherry-pick only as recovery for a reviewed, bounded, file-disjoint commit; rerun all relevant gates.

Do not rebase onto `origin`: the working branch is hundreds of unpushed commits ahead and local integration
state is authoritative.

The coordinator alone rebases, merges, creates/removes worktrees, and prunes metadata.

## 8. Cleanup

Only after the actual post-rebase branch commit is reachable from integrated HEAD and the worktree is clean:

```bash
set -e
git -C /Users/gb/github/harmonik merge-base --is-ancestor <actual-branch-commit> HEAD
test -z "$(git -C /Users/gb/github/harmonik-wt/<lane-path> status --porcelain)"
git -C /Users/gb/github/harmonik worktree remove /Users/gb/github/harmonik-wt/<lane-path>
git -C /Users/gb/github/harmonik branch -d <branch>
git -C /Users/gb/github/harmonik worktree prune
git -C /Users/gb/github/harmonik worktree list --porcelain
df -h /Users/gb/github/harmonik
```

Never force-delete a branch, broadly glob worktrees, or remove a dirty/uncommitted worktree. After a
cherry-pick, the source commit is not an ancestor under its original SHA; retain the branch/worktree until
a deliberate safe cleanup decision rather than using `branch -D`.

## 9. Stale-agent recovery

Silence is not proof of failure.

1. Inspect agent status and live process.
2. Inspect branch log, worktree status, and file timestamps.
3. If committed, independently verify and integrate.
4. If dirty and the original agent is confirmed stopped, freeze the lease and assign one recovery agent to
   that exact worktree.
5. If untouched, retire it safely and redispatch from current integrated HEAD.
6. If the agent wrote to main, stop all related writes and inspect exact commits/diffs before proceeding.

Never launch another agent against the same lease while the first may still be active.

## 10. Shared-resource hazards

Worktrees isolate tracked files, but not:

- Git refs, index locks, and worktree metadata;
- machine-local `.beads` state;
- disk and default build/lint caches;
- ports, sockets, tmux servers/sessions, process groups, and fixed temp names;
- live `.harmonik` control/event state;
- package-level test symbol names after merge.

Workers do not close/reassign beads, create/remove worktrees, or manipulate shared runtime state. The
coordinator owns those transitions.

## 11. Current L0 exception

`/Users/gb/github/harmonik-wt/lift-l0` predates this runbook and now contains a clean two-commit candidate
stack directly descended from the current integration branch: reviewed L0 `95e248cb` plus trivial tagged-vet
cleanup `24a61f40`. It remains exclusively owned until takeover verification. Preserve it exactly:

- do not create a second L0 writer;
- do not clean, reset, rebase, or edit it;
- do not remove it merely because it committed;
- verify and classify both commits separately, including whether the out-of-scope-but-prerequisite trivial
  cleanup is accepted in the fast-forward stack;
- reconcile the candidate's current-import depguard allow-list with the roadmap/handoff's pre-armed L0–L9
  union requirement before merge;
- run the handoff's worktree build/vet/check-fast/freeze/trailer gates;
- then follow `INTEGRATED-EXECUTION-PLAN.md` §8 and `HANDOFF-alpha.md` for takeover verification.
