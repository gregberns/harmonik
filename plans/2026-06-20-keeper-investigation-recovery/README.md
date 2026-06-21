# Keeper Investigation — Recovery Report (2026-06-20)

> Reconstructed on 2026-06-20 AM from the Claude transcripts of the night of
> 2026-06-19. Purpose: recover the research and fix-work done overnight on the
> harmonik session-keeper, which was partially lost when the main thread killed
> a sub-agent mid-flight. **No code was changed by this report** — it is a
> read-only forensic reconstruction.

## TL;DR

- The overnight session investigated why the **keeper / captain self-restart is
  unreliable** (operator's #1 pain). ~13 sub-agents fanned out over the keeper
  code + tests.
- **The single highest-value artifact survives and is recoverable:** an
  unmerged, reviewer-approved, smoke-tested **−542-line keeper simplification**
  on branch `worktree-agent-acb20218b63a573e6`, commit **`89852bb3`**.
- One bead — **hk-5da7** (P1) — captures the 3 headline bugs. **Several
  additional bugs and all the test-coverage gaps are uncaptured** (no bead).
- The "Fix keeper + ack handshake" sub-agent (`a337a187`) was **killed via
  TaskStop** ~20 min in; its uncommitted work is lost (but its *findings* are
  reconstructed below).

---

## 1. Provenance — the sessions

| Item | ID / path |
|---|---|
| Origin session ("internet issues / 3 days of context") | `fe5efd0e-72b2-4b67-b8e9-e1ff3092181d` (msg @ 04:38 UTC = ~21:38 PDT Jun 19) |
| Sub-agent transcripts (all survived) | `~/.claude/projects/-Users-gb-github-harmonik/fe5efd0e-…/subagents/agent-*.jsonl` |
| Bead filed this session | **hk-5da7** (P1, @05:18 UTC) |
| Bead filed *later* (different session) | **hk-7myt** (P1, @06:18 UTC) — fork-bomb smoke harness |

### The ~13-agent keeper fan-out (two waves)

**Wave 1 (~21:43–21:50 PDT) — initiatives, not keeper bugs:** flywheel landing,
initiative ranking, remote-substrate, **300k context-watchdog design**.

**Wave 2 (~22:33–22:40) — read-only keeper code/test audit (~12 agents):**
`watcher.go`, `cycle.go`, `keeper_cmd.go`, `keeper_enable_doctor_cmd.go`, and
keeper test batches.

**Wave 3 (~22:49–22:58) — synthesis + fix:**
- `a337a187` "Fix keeper + build ack handshake" — **KILLED** via TaskStop @05:49:33 UTC.
- `acb20218` "Audit + simplify keeper restart" — **COMPLETED**, committed `89852bb3`.

### Why the fix agent was killed

At 05:47 UTC the operator reframed the problem as **architectural** ("the keeper
may architecturally be flawed and much too complicated… positional arguments
should no longer be allowed… then have critics and architects reevaluate"). The
main thread could not redirect the running agent (`SendMessage` unavailable), so
it stopped `a337a187` and re-dispatched `acb20218` with the
"prefer deleting complexity, keep the gauge, flag-only args" direction baked in.

---

## 2. Bugs identified

| # | Bug | Root cause | Source | Captured? |
|---|-----|-----------|--------|-----------|
| 1 | `--warn-pct/--act-pct` **inert on 1M-window models** | pct ints feed only a legacy fallback; live gate uses hardcoded ceils + abs caps **200k/215k**, which always win on a 1M window. Operator intent was a 300k cap. | `internal/keeper/thresholds.go`, `cmd/harmonik/keeper_cmd.go` | ✅ hk-5da7 #1 |
| 2 | `restart-now` **silent no-op** (writes "marker written", exit 0, cycle never advances) | **Two** root causes pinned: (a) `--project` defaults to `os.Getwd()`; captain runs from a different CWD → marker lands in the wrong `.harmonik/keeper/` dir while the watcher polls a fixed path. (b) The marker is only consumed on a **fresh, non-stale, non-foreign gauge tick** — a stale/foreign gauge silently swallows it. | `cmd/harmonik/keeper_cmd.go`, `internal/keeper/watcher.go` (~L765-781), `cycle.go` `RunOnDemand` | ✅ hk-5da7 #2 (root causes are new detail) |
| 3 | **ACT-when-idle path LOOPS** | Injects `/session-handoff`, times out before `/clear` completes, re-fires with a new nonce, and **truncated the handoff file to 0 lines** between cycles. Observed **live** (nonces `-000001`, `-000002`). | `internal/keeper/cycle.go` / `watcher.go` ACT path | ⚠️ Partial — hk-5da7 #3 only says "force-act/idle path unverified"; the **loop + handoff-truncation specifics are NOT in the bead** |
| A | `restart-now captain --project X` **exits 2** | `resolveKeeperAgent` only accepts `--project` *before* the positional agent — any trailing dash token is rejected. Reproduced live in `/tmp/kx-bug1`. | `cmd/harmonik/keeper_cmd.go` `resolveKeeperAgent` | ❌ **No bead** |
| B | `restart-now` **false-success when no keeper is running** | CLI writes the marker and exits 0 even when **no keeper process exists to consume it** — no liveness check. This is the operator's actual "marker written but nothing happens" pain. | `cmd/harmonik/keeper_cmd.go` `runKeeperRestartNow` | ❌ **No bead** (the ACK handshake, §4, is the fix) |
| C | freshness-gate **strict-mtime** false-reject | A strict `handoff.mtime >= requestedAt` rejected a handoff written seconds *before* `restart-now` (captain writes handoff THEN fires). Fixed inside `89852bb3` with a 10-min freshness *window*. | `internal/keeper/restartnow.go` (new) | Fixed in branch |
| D | **Config/intent conflict** (not a code bug) | `captain-launch.sh:112` passes `--warn-pct 30 --act-pct 35` = **300k/350k on a 1M window — *wider* than the locked 200k/215k band**. Naively "honoring" the pct flags would **regress** the operator's band-retune HARD-NO. The fix must clamp so pct can only ever *tighten*, never widen. | `cmd/harmonik/captain-tools/captain-launch.sh` + `scripts/captain-tools/captain-launch.sh` | ❌ Design nuance, no bead |

### Test-coverage gaps (flagged, none beaded)

- **No end-to-end restart-now test** stitching CLI marker-write → on-disk →
  watcher-tick → consume. Both halves are tested in isolation; the consume tests
  use an **injected in-memory stub**, not a marker the write-side wrote to disk
  ("the two halves meet only at the struct shape").
- **`injector.go` `InjectText`** tmux paste mechanics (load-buffer → paste →
  Enter + retries) have **no unit coverage**; only the empty-target guard and
  timing constants are tested. Real delivery relies on integration-tagged tests
  that don't run by default.
- **Zero bash-hook tests** — the jq-path / `[1m]`-window inference in the launch
  scripts can regress silently.
- Note: the alarming "`cmd/harmonik` 8.2% coverage" figure is a measurement
  artifact (`-run Keeper` against the whole package). `internal/keeper/...` is
  **74.4%** (3.3:1 test:code). **No full rewrite is warranted** — the
  gauge/threshold/auto-cycle core is sound; fixes should be surgical.

---

## 3. Recoverable work — commit `89852bb3` 🟢

**Branch:** `worktree-agent-acb20218b63a573e6`  ·  **Commit:** `89852bb3`
"fix+simplify: keeper restart-now direct path, flag-only args, honored pct, ack
handshake (hk-5da7)"  ·  **NOT merged to main (HEAD = `1ccc2b90`).**

- **−542 net lines** (774 added, 1313 removed) across 16 files.
- Reviewer (fresh context) **APPROVE**.
- `go test ./internal/keeper/... ./cmd/harmonik/...` **green**.
- **Live smoke confirmed**: `[KEEPER ACK rn-…] received restart` + `/clear` +
  `/session-resume` all land in a scratch tmux pane; `ping` confirmed.

**What it does:**
- Rips out the **marker → watcher-poll → nonce/journal/freshness-gate** state
  machine; `restart-now` now acts **directly and synchronously** in the CLI
  process. Removed: `RunOnDemand`, `runOnDemandCycleTail`, `onDemandSettle`
  (cycle.go −278); `CyclerConfig` `*RestartNow*Fn` fields; `gates.go`
  `RestartNowMarker` + 5 marker helpers (−131); watcher marker detection;
  deleted `cycle_restart_now_test.go` (−651).
- Adds `internal/keeper/restartnow.go` (`RestartNow` + `Ping` + `AckLine` +
  10-min freshness window), `restartnow_test.go`,
  `docs/keeper-restart-now-ack-protocol.md`.
- `restart-now` is now **flag-only** (`--agent`, rejects positionals at exit 2),
  honors pct, prints `nonce=rn-…`.
- Both `captain-launch.sh` copies updated to pass `--warn-abs-tokens 200000
  --act-abs-tokens 215000`.

> ⚠️ This commit makes the **"keep the gauge, honor pct" decision differently**
> than bug D suggests: it pins the launch scripts to **200k/215k abs** (the
> current band), sidestepping the pct-vs-band conflict rather than clamping pct.
> Worth a conscious check at review against the operator's 300k-cap intent.

### The killed agent (`a337a187`) — lost, but findings recovered

All edits were uncommitted in worktree `agent-a337a187faf313a26` when killed;
**nothing was committed** (last event: `[Request interrupted by user]` mid
test-rerun). It had built a *different*, **surgical** approach: keep the
marker→poll architecture, add a `ping` subcommand + ACK injection, and a pure
`derivePctThreshold` that **clamps pct so it can only tighten, never widen** past
the compiled band (directly addresses bug D). That clamp idea is **not** in
`89852bb3` and is worth grafting in.

> Cleanup check performed: the killed agent first mis-wrote `ping.go` to the
> **main tree** path before re-writing into its worktree. **Verified clean** —
> no stray `internal/keeper/ping.go` exists in the main working tree.

---

## 4. The ACK-handshake design (both agents converged on it)

A verifiable **liveness** protocol that turns a silent no-op into a provable
outcome:

- An agent fires a request (`ping` or `restart-now`) and arms a background timer.
- The keeper, on consuming it, injects **`[KEEPER ACK <nonce>]`** back into the
  agent's tmux pane via the same bracketed-paste path as the warn text.
- ACK seen in the timer window → keeper alive; timer expires → keeper broken,
  escalate.
- `ping` = pure liveness (no cycle, no gates). `restart-now` injects the ACK
  **before** the gated cycle, so liveness is proven even when a safety gate
  blocks the destructive `/clear`.

Protocol doc lives in `89852bb3` at `docs/keeper-restart-now-ack-protocol.md`.

---

## 5. Beads — what exists vs. what's missing

**Created:**
- **hk-5da7** (P1, `keeper,daemon-reliability`) — "keeper: captain self-restart
  unreliable on 1M window (pct flags inert; restart-now no-op)". Captures bugs
  1/2/3 at a headline level.
- **hk-7myt** (P1) — fork-bomb smoke harness incident. *Filed in a later
  session*, not this investigation.

**NOT captured (recommend filing or folding into hk-5da7):**
- Bug **A** — `restart-now` flag-ordering footgun (exit 2 on trailing flags).
- Bug **B** — `restart-now` false-success with no live keeper (no liveness check).
- Bug **3 specifics** — the ACT-when-idle **loop + handoff-file truncation**.
- Bug **D** — the pct-flags-vs-locked-band conflict + the "clamp to tighten-only"
  resolution.
- The **test-coverage gaps** (restart-now E2E, `InjectText`, bash-hooks).

> None of the ~13 fan-out agents filed any beads — all were read-only or
> worktree-scoped. So the bead ledger under-represents what was actually found.

---

## 6. Recommended next actions (decisions for the operator)

1. **Fresh-context review of `89852bb3`** → if good: `go install` → restart the
   keeper on the rebuilt binary → **verify `[KEEPER ACK]` live** → merge. This is
   the operator-requested simplification, already approved + smoke-tested.
   - At review, consciously decide the **pct-vs-band** question (commit pins
     200k/215k; consider grafting the killed agent's **clamp-to-tighten-only**
     `derivePctThreshold` so pct stays meaningful without widening).
2. **Update hk-5da7** with the precise root causes (CWD/`os.Getwd` marker
   misplacement; gauge-freshness consumption dependency) and the **ACT-loop +
   handoff-truncation** specifics.
3. **File the uncaptured bugs** (A, B) and the **test-coverage gaps**, or confirm
   `89852bb3` already resolves each before closing them out.
4. **Architect/critic panel** on keeper architecture (the operator's stated step
   after the merge) — but note the audit's evidence: 74.4% coverage, sound core,
   **no full rewrite warranted**; favor surgical simplification.

---

## Appendix — source transcripts

- Origin: `…/fe5efd0e-72b2-4b67-b8e9-e1ff3092181d.jsonl`
- Killed fix agent: `…/fe5efd0e-…/subagents/agent-a337a187faf313a26.jsonl`
- Simplify agent (committed `89852bb3`):
  `…/fe5efd0e-…/subagents/agent-acb20218b63a573e6.jsonl` (+ continuation under
  `6ba89d87-…/subagents/agent-acb20218b63a573e6.jsonl`)
- 300k context-watchdog design: `…/fe5efd0e-…/subagents/agent-a0f0e65faad5cb000.jsonl`
- Code-audit fan-out: `agent-a4ad92a5…`, `agent-ab5e38156…`, `agent-abfdda3d…`,
  `agent-ac93678a…`, `agent-ac4ca4f8…` (same `subagents/` dir)
