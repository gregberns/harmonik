# Assessor Daemon Campaign — full exploratory acceptance test of the new daemon

**Owner:** admiral (spawns + gates) · **Executor:** assessor (runs + records + recommends) · **Date:** 2026-07-17 (v2 hardened 2026-07-18)
**Status:** ACTIVE — exploratory pass authorized to start NOW against current HEAD. Baseline pass re-runs at the clean SHA when the captain signals `mediums-complete`.

> The point: do not *assume* the daemon works — **prove it**, exhaustively, in an isolated environment, with
> everything written down and committed so the whole campaign re-runs on demand. This is the M6 controlled-testing
> harness put to work, driven by the assessor. Every claim resolves to a cited artifact; a suite that ran without a
> hard assertion is INCONCLUSIVE, never PASS.

**Operator decisions (LOCKED — 2026-07-17, baked into this v2):**
1. **Start now.** Exploratory pass begins immediately against current HEAD; authoritative baseline re-runs at the clean SHA on `mediums-complete`.
2. **Environment:** isolated sandbox on THIS box (`/tmp/h-assessor`). The deferred M4 real-box remote proof is NOT in scope here.
3. **Harness set:** claude + pi full; **codex minimal** (codex matrix rows pre-marked SKIPPED-with-reason).
4. **Driver:** the assessor role drives the campaign interactively now; harden into a scripted harness after the first run teaches the shape.

---

## 0. Where this sits in the sequence

1. **Captain lands the fix waves** (Wave 4 §c mediums in flight). At minimum all correctness + the merge/strand DOT
   drivers must land before the *baseline* pass is authoritative. The *exploratory* pass runs against the moving tree now.
2. **Consolidate** — tree green out-of-band (`go build`/`test`/`vet`/`-race`), COORD updated.
3. **THIS campaign** — assessor stands the daemon up in isolation and hammers it (§2). Several hours.
4. **Admiral gate** — assessor delivers a reasoned PASS/BLOCK + concern list; admiral makes the release call against
   `good-enough-principles.md`. Only then does the branch-advance / PR conversation open.

### 0.D — Build-SHA pinning (anti-stale-PASS) — HARD REQUIREMENT

1. **Pin at start.** Record `PIN_SHA = git rev-parse HEAD` before suite 1; build the sandbox daemon from *exactly* this
   SHA (clone + `git checkout --detach "$PIN_SHA"`); record the binary build-stamp/`--version`. Tag every assertion with the SHA.
2. **Freeze during the run.** No mid-campaign rebuild; the captain's new fixes do NOT enter a running pass.
3. **Mandatory re-run at baseline.** A verdict is valid ONLY for its `PIN_SHA`. If commits land after the pin, the PASS
   is void → re-run at the new tip (min S2/S3/S7 + any suite whose paths the new commits touched). A mismatch is recorded
   **STALE — re-run required**, never PASS.
4. **Green-tree precondition.** Assert `go build`/`test`/`vet`/`-race` green at the pin and cite the outputs — a campaign
   on a red tree is void.

### 0.E — Two-pass model (pinned to a moving tree)

| Pass | Trigger | PASS_ID | Meaning |
|---|---|---|---|
| **Exploratory** | starts NOW, current HEAD | `exploratory-<sha8>` | Shakes the daemon out against a moving tree. Findings real + filed; PASS/BLOCK **provisional**. |
| **Baseline** | captain posts `mediums-complete` | `baseline-<sha8>` | Authoritative. Full re-run S1–S7 from a **fresh clone** at the clean SHA. **Only a baseline pass produces the ASSESSMENT.md the admiral gates on.** |

Re-run policy: when `mediums-complete` arrives mid-exploratory, finish the current suite, checkpoint, then **start a fresh
baseline pass from a new clone** — never hot-swap the binary under a running pass (a moving SHA mid-pass poisons attribution).

## 1. The isolated environment (hard requirement)

Every suite runs against a **throwaway daemon in an isolated sandbox** — never the operator's real repo state, real beads
DB, real comms bus, or real tmux namespace. The isolation contract:
- Dedicated scratch project dir (fresh `harmonik init` via `scripts/scratch-daemon.sh`), its own `.beads/`, its own comms
  bus, its own daemon pidfile/socket, a **dedicated tmux server** (`tmux -L h-assessor`) in a campaign-scoped namespace.
- Isolated `CODEX_HOME` / `GOCACHE` / `TMPDIR=/tmp/h-assessor`; clean env (`OPENAI_API_KEY` unset).
- The daemon **is turned ON** here (it is OFF in the main freeze-and-carve). Turning it on in the sandbox is part of the
  campaign, not a change to the frozen main line.
- **Pre-flight isolation check (§4A) — ALL must PASS before `daemon on`.** **Teardown verification (§4B) — ALL must PASS**;
  the sandbox leaves no residue in the real project.

## 2. Test suites — "basically all the things"

Each suite is a **committed, re-runnable scenario** (scenario YAML / script under `scenarios/` or the M6 harness) with
explicit assertions. **Blanket rule:** every assertion resolves to a concrete artifact — a specific `events.jsonl` line
matched by `jq`, an exit code, a file hash/byte-count/path-exists, or a state read (`br show`, registry, health RPC) with
the value shown. "Observed working" with no cited artifact is a non-result and fails the critic (see §3, Evidence standard).

- **S1 — Lifecycle: startup / teardown / sleep.**
  - `harmonik init` exits 0 AND socket+pidfile exist AND health RPC returns bound-port; grep the boot log for the ⑤ bind
    line (not just "no error").
  - Supervisor revive: capture daemon PID pre-SIGTERM; post-revive PID differs AND non-zero AND health green within N s;
    exactly ONE new daemon (process count — no double-spawn).
  - Crew start: registry record exists AND `crew-<name>` tmux session exists — **both**.
  - Orphan sweep: kill a crew's tmux out-of-band, boot daemon; assert it reaped the dead crew AND did NOT reap a live crew.
  - Suspend/resume ("sleep the system"): hash full state (open beads, leases, crew registry) before sleep; after resume
    assert byte-identical.

- **S2 — Agent harnesses / handlers (heavy — operator priority).** For **every harness** (claude, pi; codex minimal):
  launch → `agent_ready` handshake → dispatch → capture → review gate → terminal transition, run **both local and remote**
  (`runner==nil/!=nil`). Per harness × {local,remote}: terminal bead transition is daemon-written (trailer present) AND
  matches expected — a bead stuck in `ready` is a FAIL, not a skip. Target the review's handler findings directly:
  - **H13** lost-wakeup: **hit the race window** (warm resume where `SessionStart` fires before `SetAgentReadyCallback`);
    assert `agent_ready` consumed exactly once; run ≥20 launch iterations, zero timeouts (one hang = BLOCK). Reverting the
    latch must time this cell out (a happy-path handshake alone does NOT catch this bug).
  - **H11** codexwire string-id: feed a string id, assert no panic AND graceful error class.
  - **HC-004** double-spawn: restart mid-run, run process count == 1 AND no second worktree.
  - **A9** HC-contract drift: live handshake fields byte-match the HC spec version (see also G13 version negotiation).

- **S3 — The full workflow matrix.** Run beads through **DOT (the locked contract), single, and review-loop** across each
  harness × substrate × local/remote. Enumerated in the coverage manifest (§3); every cell RUN or SKIPPED-with-reason.
  - **DOT:** assert the FULL graph executed — every node reached, edges in order, review node fired — via node-transition
    events. **DOT collapsing to single-mode = FAIL** (prove the workflow graph, not getting-started mode). *(core-loop-proof contract = DOT.)*
  - **review-loop:** inject one REQUEST_CHANGES, assert ≥1 revision re-dispatched.
  - Assert `RUN + SKIPPED == enumerated total` (no silent cell drop).
  - **Matrix axis note:** "substrate" collapses into local/remote (tmux is the only substrate) — the axis is
    (harness × mode × local/remote). Do not carry an undefined "substrate" axis that hides cells.

- **S4 — Multi-crew comms exercise (Hamlet Act 1 Scene 1).** Stand up N crew as distinct agents (Bernardo, Francisco,
  Horatio, Marcellus, the Ghost) and perform the scene end-to-end over `harmonik comms`.
  - Assert comms log == checked-in `expected-transcript.txt` line-for-line (empty diff).
  - Assert `count(delivered)==count(sent)` per recipient AND `count(distinct event_id)==count(logical msgs)`.
  - Assert monotonic per-speaker sequence (no reordering).
  - (Forced-redelivery dedupe is its own cell — see **G7**; Hamlet is sequential and never forces a dup on its own.)

- **S5 — Keeper restart timing (operator's explicit test).** Set the keeper to **~30k** for several crew. Drive a crew (or
  a "test captain") to **generate text until context fills**.
  - Assert WARN fires in band (~28k–32k) and ACT at its threshold — cite both gauge log lines with token counts.
  - Assert the handoff → `/clear` → `/session-resume` cycle fires and the resumed session re-hydrates intent (pre-restart
    bead/task marker == post-resume).
  - **C4** (bravo's keeper research): the WARN inject is *not* gated by operator-attachment and force-submits a half-typed
    message — assert explicitly **REPRODUCED / NOT-REPRODUCED** with captured pane state.
  - Negative guard: keeper does NOT fire below WARN.
  - **Self-restart answer:** an agent does not restart itself by will mid-turn; the **keeper watcher** drives it on a
    context-fill trigger. The campaign proves the keeper-driven path; a *voluntary* self-restart command is a separate small
    feature — flag it, don't assume it.

- **S6 — Adversarial log watcher (runs the WHOLE time).** A dedicated assessor sub-agent tails `events.jsonl` + daemon logs
  concurrently with S1–S5, hunting panics / errors / leaked goroutines / held leases / orphan processes / fd growth.
  - **FAIL set:** any `level=error`/`panic`/`fatal`; goroutine/WaitGroup/fd growth over baseline; orphan PID or held lease at teardown.
  - Assert fd + goroutine counts at teardown ≤ baseline + threshold (capture both numbers).
  - **Watcher liveness:** heartbeat every N s in its own log — a watcher that died at minute 3 and "found nothing" is a FAIL
    of S6, not a PASS.
  - **Structured queries only** (`harmonik subscribe --json` / `jq` on `run_id`) — NEVER hand-grep by run_id (major-issue-fanout rule).

- **S7 — Fault injection (turn the review's highs into live repros).** Each fault: explicit hard assertion + a
  **revert-confirm-red step** (failed≠clean — the assertion must go red when the fix is reverted, else it is theater). Each
  fault also asserts the daemon SURVIVES (health green after); a fault that wedges it is itself a finding.
  - **H2** lease truncate: worktree still on disk AND NOT force-GC'd.
  - **H4/H5** truncated verdict/auto-status on a remote worker: run marked **inconclusive**, NOT "absent"/DONE.
  - **H6** concurrent submits for one name: both reflected or a defined winner, NO lost update (a dropped submit = BLOCK).
  - **H7** drain-while-emitting: clean exit code, no `WaitGroup misuse` abort.
  - **H8** remote Kill: NO local PID signalled (capture local PID liveness across the Kill).
  - **H3** revert-then-sweep: reverted bead NOT set DONE (fabricated-DONE guard; DONE here = automatic BLOCK).

### 2.G — Coverage-gap cells G1–G14 (added by the adversarial hardening pass — each must go red on revert)

| Cell | What it drives | Assertion (revert → red) |
|---|---|---|
| **G1** | Operator control path (C1 — review's worst finding, zero cells today) | Drive a run to `confirm_required`; `harmonik confirm-verdict`/`veto-verdict` releases it. Revert C1 → command exits 1 / run stuck. |
| **G2** | Daemon crash-recovery | SIGKILL-mid-dispatch → restart: in-flight runs re-adopt; `queue.json` RunID not durably `""` (RU-01a); no double-dispatch of `(run_id,node_id)`. |
| **G3** | DOT merge-vs-strand (A7) | (a) committed tip that SHOULD merge is merged; (b) un-reviewed/cap-hit tip that SHOULD strand is stranded. Plus a graph whose gate node ≠ `"commit_gate"`: cap-hit salvage + gate feedback still fire. |
| **G4** | Review-loop feedback threading **on a remote worker** (A8) | REQUEST_CHANGES writes reviewer feedback, agent re-drives, verdict re-threads — remotely (review-loop vs DOT diverge on remote feedback; H4/H5). |
| **G5** | `harmonik promote`, push-mode AND PR-mode | Reviewed work advances to target (throwaway remote); temp worktree cleanup leaves no registered worktree (RU-17); build-gate doesn't re-run on unchanged tree. |
| **G6** | Supervisor revive race + orphan-sweep false-reap | SIGTERM during in-flight → supervisor revives exactly once (no double-daemon); boot sweep does NOT reap a crew (re)started seconds earlier. |
| **G7** | Comms at-least-once UNDER redelivery (N3) | Force redelivery (consumer reconnect/replay): same `event_id` arrives twice, dedupe drops the second. |
| **G8** | SSH tunnel DROP mid-run (distinct from H4/H5) | Kill SSH transport during remote dispatch → run inconclusive/retry (not silent absent); remote tmux window cleaned (RU-05a); H8 Kill routes remote, no local PID signalled. |
| **G9** | Keeper hold/release override | Under `hold`, context crossing ACT does NOT force handoff; `release` restores the cutoff. |
| **G10** | Config honored, not silently dropped | Partial `config.yaml` (only `watch.absent_thresh_s`, no `schema_version`) is applied, not discarded (projectconfig.go:1287); unknown keeper key rejected. |
| **G11** | Oversized-but-valid event line | A >64 KB `reviewer_verdict`/directive doesn't abort the 64 KB `bufio.Scanner` (smoke.go:381, goalkeeper_cmd.go:156). |
| **G12** | EV-036 secret-field startup guard (A4) + EV-034 sealing | Startup fails-closed on a registered payload with a secret-looking field; post-dispatch registration rejected. |
| **G13** | Version negotiation (HC-009) | Handler advertising `SupportedVersions:[2]` gets `ErrProtocolMismatch`; a handler never emitting `handler_capabilities` aborts at the 5 s timeout (no hang). |
| **G14** | Positive worktree GC (pairs with H2/H2b) | A genuinely stale/aged worktree with a dead lease IS reaped (else the fail-safe over-quarantines and never GCs). |

**Meta-gap (A3 — load-bearing on the campaign itself):** `ExpandMatrix` + `CheckPostSuiteLeaks` are **not wired into the
production runner** (harness.go:356) and the timeout branch never evaluates assertions (harness.go:481). *Assert up front:*
a `matrix:` YAML with `{{.param}}` actually expands to N substituted runs and post-suite leak-check runs — else the matrix
silently executes once, unsubstituted, and S6 leak detection is a no-op. If unwired, run matrix-expansion + leak-sensor
out-of-band (S6 already uses `subscribe --json`) and flag it.

**Also-untraced highs to add one cell each (from hardening doc 03 §B):** H1 (gitignore-hygiene commits to operator branch —
double-covered by teardown D1), H2b (aged-reaper dir-mtime — G14 pair), H9 (RC-024 audit-fetch error swallowed — fail-closed),
H10 (runShell busy-spin on ctx-cancel — or SKIP-with-reason), H12 (BI-010c label guard bypassed by `--flag=value`),
RC-002a (reconcile-lock unlock-before-unlink race), A1/A2 (dead-package static asserts).

## 3. Thoroughness enforcement (the "INCREDIBLY thorough" requirement, made mechanical)

- **Evidence standard (no unfalsifiable claims).** Every assertion resolves to ONE of: (a) an exit code, (b) a specific
  `events.jsonl`/daemon-log line quoted with its `jq` selector, (c) a file hash / byte-count / path-exists, or (d) a state
  read with the value shown. **Prohibited verdict language with no artifact:** "looks fine", "seems to work", "no obvious
  issues", "observed working", "ran cleanly". A suite that RAN but recorded no hard pass/fail assertion is **INCONCLUSIVE**,
  never PASS — inconclusive at the risk floor is BLOCK-equivalent for that cell.
- **Run-log (durable, append-only).** Every command, observation, assertion + pass/fail, with timestamps and artifact
  paths. Reproducible from the log alone.
- **Coverage manifest — no silent gaps.** The full matrix (harness × mode × local/remote, plus every suite S1–S7 and cell
  G1–G14) enumerated up front; each cell RUN / SKIPPED-with-reason at the end. A skipped cell is a logged decision, never a
  silent omission. Codex rows pre-marked `SKIPPED — operator "codex minimal"`.
- **Completeness critic (final pass).** A separate agent asks: *what modality was not exercised, what assertion is missing,
  what harness/mode/local-remote/fault/G-cell was skipped?* Whatever it finds becomes another round. Not "done" until the
  critic comes back dry.
- **Everything committed.** Scenario files, run-log, coverage manifest, S6 findings, and the final assessment land in-repo
  under `runs/<PASS_ID>/`, so the campaign re-runs on demand.

## 4. Isolation pre-flight (§4A) + Teardown verification (§4B)

Let `REAL=$(git -C /Users/gb/github/harmonik rev-parse --show-toplevel)`, `SBX=$(realpath -m "$SCRATCH")`.

### 4A — PRE-FLIGHT ISOLATION CHECK (all PASS before `daemon on`)

1. **Sandbox-not-inside-real (hard refuse).** `realpath -m` both first (symlink defeats leaf-only checks, RU-22);
   `case "$SBX/" in "$REAL/"*) FAIL;; esac` AND the reverse AND `[ "$SBX" != "$REAL" ]`.
2. **Distinct git repo + no real remote.** `git -C "$SBX" rev-parse --show-toplevel` == `$SBX`; `git -C "$SBX" remote -v`
   empty or throwaway — must NOT resolve to the real harmonik remote (guards G5 promote/push).
3. **Distinct `.beads` proven empty.** Real dir (not a symlink into real); scoped `br list` == 0. `HK_PROJECT` **unset**
   (state_cmd.go:107 still honors the retired var).
4. **Distinct socket + pidfile.** Under `$SBX/.harmonik` (or `$TMPDIR`); ≠ real socket; absent before boot; no collision.
5. **Distinct tmux — dedicated server socket.** `tmux -L h-assessor` (separate server), not just a `crew-asr-` prefix.
   Assert `-L h-assessor list-sessions` empty AND default `tmux ls` has no `crew-asr-*`. (Prefix-only is risky: keeper hooks
   derive AGENT from session name with no traversal guard, RU-24.)
6. **Distinct env proven fresh.** `TMPDIR=/tmp/h-assessor`, `GOCACHE=…/gocache`, `CODEX_HOME=…/codex`, each ≠ default +
   fresh. `OPENAI_API_KEY` unset. Worker binary in `workers.yaml` is the freshly-built one, not the `/Users/gb/go/bin/harmonik`
   fallback (RU-23).
7. **Read-leak ack.** `harmonik usage`/session-discovery hardcode the real repo path + `$USER` (usage.go:424) — cannot be
   sandboxed. Assert the campaign never invokes `harmonik usage`; log it as a known un-isolable read.
8. **Pre-snapshot the real env (teardown baseline).** `git -C "$REAL" rev-parse HEAD`, `status --porcelain`, real `.beads`
   JSONL sha256+count, `git worktree list`, default `tmux ls`, `$HOME/.harmonik/keeper` file list, start epoch.
9. **Dry-boot round-trip (⑤ bind fix).** Health-only boot; assert the daemon reports **`$SBX`** as its project dir + binds
   the sandbox socket. A daemon echoing the real path aborts the campaign.

> **Live-environment note for the assessor:** the main daemon is OFF, but two prior scratch daemons are running under
> `/private/tmp/h/{ws44-build,pi-verify}`. `/tmp/h-assessor` is a distinct namespace so there is no collision — but the
> pre-flight socket/pidfile/tmux checks must confirm `$SBX` handles do not overlap those either.

### 4B — TEARDOWN VERIFICATION (zero residue — all PASS; D1/D2 double as the live H1/H3 regression test)

1. **Real branch untouched.** `git -C "$REAL" rev-parse HEAD` == snapshot; `status --porcelain` identical (catches H1,
   EmitTrip direct-append trip_ev043b.go:446).
2. **Real ledger untouched.** Real `.beads` sha256 + `br list` count == snapshot (catches H3-class false-close reaching real DB).
3. **No tmux residue.** `tmux -L h-assessor kill-server`; `-L h-assessor list-sessions` errors AND default `tmux ls` has no `crew-asr-*`.
4. **No processes.** `pgrep -f h-assessor` empty; pidfile+socket gone; no orphan agent/worker PIDs.
5. **No leaked worktrees.** `git -C "$SBX" worktree list` == main only; real `git worktree list` == snapshot.
6. **No leases/fds leaked.** S6 final: 0 held leases, fd count == baseline, 0 leaked goroutines.
7. **Nothing written outside sandbox.** `find "$REAL" -newermt @<start>` empty; `$HOME/.harmonik/keeper` == snapshot
   (catches RU-24 traversal); no new `$HOME/.codex`, no real `.kerf` writes.
8. **Scratch removed.** `rm -rf /tmp/h-assessor` succeeds and is gone.
9. **Residue diff committed** to RUN-LOG.md — a clean teardown is a logged, re-runnable assertion.

## 5. PASS/BLOCK Rubric

ASSESSMENT.md classifies every finding into exactly one tier; **verdict = max tier reached.** `BLOCK` if any Tier-0 OR any
required §2 cell fails at the risk floor; else `PASS-with-concerns` if any Tier-1; else `PASS`.

**Tier 0 — AUTOMATIC BLOCK** (good-enough §2.2 XT critical + §2.4):
- Any panic / fatal / process abort in daemon or worker.
- Data corruption or lost update (H6 dropped submit; S1 resume state mismatch).
- Silent failure / false-green: a run reported DONE/green that didn't execute (DOT→single collapse; H3 fabricated-DONE; H4/H5 "absent").
- Unbounded hang / wedge / silent lost-wakeup (H13; keeper never firing; daemon wedged post-fault).
- Fleet-wide blast: orphan sweep reaping a live crew; remote Kill signalling a local PID (H8).
- Any required matrix cell red or unexercised.
- Any claimed-done not reconciling to a real commit/diff/test.
- A corpus/previously-fixed bug regressing (always critical).

**Tier 1 — RELEASE-WITH-CONCERNS** (recorded, tracked, does not gate):
- Bounded, recoverable defect with a workaround, no core-loop reach.
- A non-required matrix cell skipped WITH a logged reason.
- C4 reproduced but low-frequency and non-corrupting — file + name the follow-up.
- REQUEST_CHANGES-class cold-review notes (idiom, tidiness).
- fd/goroutine growth within threshold but non-zero.

**Tier 2 — COSMETIC** (note only): log noise, wording, naming, non-load-bearing doc drift.

**Risk-floor coupling (good-enough §3):** any finding touching core dispatch, daemon lifecycle, the merge/commit gate, or
the remote path is floored one tier HIGHER — a Tier-1 defect there escalates to Tier-0. The assessor may raise a tier on
blast-radius; never lower below the path floor.

## 6. Execution Runbook (assessor)

**Executor:** assessor · **Sandbox:** isolated scratch daemon via `scripts/scratch-daemon.sh` · **Tree:** moving
(`phase1-session-restart-substrate`; captain landing mediums separately). Operate from `$HARMONIK_PROJECT` (repo root) only
— **never `cd` into the scratch clone**; drive it via `scripts/scratch-daemon.sh <sub> "$SCRATCH"` and `git -C "$SCRATCH"`.
All artifacts under `plans/2026-07-17-assessor-daemon-campaign/runs/<PASS_ID>/`.

### §6.1 STARTUP (exact first commands)

```bash
# (a) IDENTITY + CWD guard
test "$HARMONIK_AGENT" = assessor || { echo "NOT ASSESSOR — abort"; exit 1; }
cd "$HARMONIK_PROJECT" && git rev-parse --show-toplevel   # must == $HARMONIK_PROJECT
harmonik comms join --name assessor
harmonik comms recv --agent assessor --follow --json &     # arm inbox; re-join on <=90s timer

# (b) PIN THE BUILD
SRC="$HARMONIK_PROJECT"
PIN_SHA=$(git -C "$SRC" rev-parse HEAD)
PIN_BRANCH=$(git -C "$SRC" rev-parse --abbrev-ref HEAD)
PASS_KIND=exploratory                                      # or: baseline
PASS_ID="${PASS_KIND}-${PIN_SHA:0:8}"
RUN_DIR="plans/2026-07-17-assessor-daemon-campaign/runs/$PASS_ID"
SCRATCH="/tmp/h-assessor/scratch-$PASS_ID"
mkdir -p "$RUN_DIR"
export TMPDIR=/tmp/h-assessor CODEX_HOME=/tmp/h-assessor/codex GOCACHE=/tmp/h-assessor/gocache
unset OPENAI_API_KEY

# (c) STAND UP + ISOLATION-VERIFY (clone pinned to PIN_SHA)
scripts/scratch-daemon.sh init  "$SCRATCH" "$SRC"
git -C "$SCRATCH" checkout --detach "$PIN_SHA"
scripts/scratch-daemon.sh build "$SCRATCH"
# --- run §4A pre-flight isolation check (ALL PASS) BEFORE bringing the daemon up ---
scripts/scratch-daemon.sh up    "$SCRATCH"
scripts/scratch-daemon.sh status "$SCRATCH"
# (d) BEGIN S6 (background watcher) THEN S1 (foundation) — see §6.2
```

Write the RUN-LOG header and CAMPAIGN-STATE.json (§6.4) **before** the first assertion.

> **First-task check:** `scripts/scratch-daemon.sh` exists with subcommands `init|build|up|status|down|cycle|batch`
> (verified 2026-07-18). If it ever regresses, the assessor's first job is to stand the sandbox up by hand per
> `docs/daemon-redeploy.md`.

### §6.2 EXECUTION ORDER (dependency-ordered)

```
[ ] SANDBOX   up + §4A pre-flight green                       HARD GATE
[ ] S6 ARM    adversarial log watcher -> BACKGROUND, always-on (subscribe --json / jq)
[ ] S1 LIFECYCLE  FOUNDATION -- IF S1 FAILS, STOP (nothing below meaningful).
[ ] S2 HANDLERS   per-harness handshake->dispatch->review->terminal, local+remote. Needs S1.
[ ] S3 MATRIX     DOT x single x review-loop  x harness x local/remote  (+ G1-G14 woven in). Needs S2.
---- S4 & S5 CONCURRENT (disjoint: comms bus vs keeper) ----
[ ] S4 COMMS      Hamlet 1.1; ordered transcript, no drop/dupe (+ G7 forced redelivery). Needs S1.
[ ] S5 KEEPER     ~30k -> WARN->ACT->handoff->/clear->resume; document C4 (+ G9 hold/release). Needs S1.
[ ] S7 FAULTS     LAST -- destructive; needs S1-S3 green for attribution. Each fault: revert-confirm-red.
[ ] CRITIC    completeness critic -> another round until dry.
[ ] S6 STOP   collect LOG-WATCH-FINDINGS.md
[ ] ASSESS    reconcile claimed-done vs commits/diff/tests; write ASSESSMENT.md; mark every cell.
[ ] TEARDOWN  scratch-daemon.sh down; run §4B teardown verification.
```

### §6.3 RESUMABILITY (multi-hour; survives keeper restart)

Checkpoint `runs/<PASS_ID>/CAMPAIGN-STATE.json`, rewritten atomically after each suite + each S3 cell:
`{pass_id, pass_kind, pin_sha, pin_branch, scratch, binary_sha_verified, suites{S1..S7: PASS|BLOCK|IN_PROGRESS|PENDING|RUNNING},
s3_cells_done[], g_cells_done[], s6_watcher_pid, last_checkpoint}`.

**Resume protocol (keeper restart mid-campaign):**
1. Re-read the mission file; confirm `$HARMONIK_AGENT == assessor`; `cd "$HARMONIK_PROJECT"`.
2. Re-join comms + re-arm `recv --follow --json` (the stream does not survive restart).
3. Read newest CAMPAIGN-STATE.json; adopt PASS_ID/PIN_SHA/SCRATCH.
4. Do NOT rebuild if the sandbox is healthy AND `binary --version` == PIN_SHA; else `up` (or rebuild from the SAME pin — never a newer SHA).
5. Re-arm S6 if `s6_watcher_pid` is gone (it must cover the whole campaign).
6. Skip every suite marked PASS/BLOCK; resume IN_PROGRESS; for S3 skip cells in `s3_cells_done`.
7. Append a `## [ts] RESUME` block to RUN-LOG noting the skipped-as-complete suites.

The pin is the anchor: a resume always continues the **same** PIN_SHA pass; a mid-pass SHA change is a new pass, never a resume.

### §6.4 ARTIFACT TEMPLATES (all under `runs/<PASS_ID>/`)

- **RUN-LOG.md** (append-only; one block per command): header carries full PIN_SHA/PIN_BRANCH/PASS_KIND/BINARY/SANDBOX/
  §4A checklist/STARTED. Each block: `## [ts] Sn · <step>` with CMD / OBSERVED / ASSERT+PASS|FAIL / ARTIFACT path; a FAIL
  block links the filed bead id.
- **COVERAGE-MANIFEST.md**: a Suites table (S1–S7 RUN/SKIPPED+evidence), the S3 cell matrix (harness × mode × local/remote),
  and the G1–G14 cell table — **no blank cells**. Codex rows pre-marked `SKIPPED — operator "codex minimal"`. Assert
  `RUN + SKIPPED == enumerated total`.
- **LOG-WATCH-FINDINGS.md**: S6 output.
- **ASSESSMENT.md** (schema v2): `schema_version: 2`, `spawned_by: admiral`, `pass_id`, full `pin_sha`, `pin_branch`,
  `measured_against: good-enough-principles.md`, VERDICT PASS|BLOCK + one-line rationale (reasoned judgment, NOT a bead
  tally); Legs (LT/XT/CR); per-suite results; claimed-done reconciliation (a claim with no commit/diff/test → BLOCK);
  findings table (evidence, not the gate); residual risk for the admiral; coverage (RUN N/M, SKIPPED list, critic dry?).
  Posted to the admiral over comms `--topic gate`; the assessor recommends, **the admiral makes the release call.**

## 7. Deliverables (committed, under `runs/<PASS_ID>/`)

- `scenarios/…` — the S1–S7 + G1–G14 scenarios, re-runnable.
- `RUN-LOG.md` · `COVERAGE-MANIFEST.md` · `LOG-WATCH-FINDINGS.md` · `ASSESSMENT.md`.

> The assessor recommends; the admiral gates against `good-enough-principles.md`. Only a **baseline** pass produces the
> admiral-gating ASSESSMENT; exploratory verdicts are provisional shake-out.
