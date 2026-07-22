# Hardening input 1 — Falsifiability + PASS/BLOCK rubric (adversarial pass)

> Produced 2026-07-18 by an adversarial hardening agent. Ready-to-paste additions to PLAN.md.
> Angle: "find every way a genuinely-broken daemon could still PASS this campaign."

## A. Per-suite assertion adds (paste under each suite in §2)

**Blanket rule for all suites:** every assertion must resolve to a concrete artifact — a specific
`events.jsonl` line matched by `jq`, an exit code, a file hash/byte-count, or a state read via
`harmonik`/`br`. "Observed working" without a cited artifact is a non-result and fails the critic.

**S1 — Lifecycle**
- `harmonik init` exits 0 AND socket+pidfile exist AND health RPC returns bound-port; grep boot log for the ⑤ bind line, not just "no error."
- Supervisor revive: capture daemon PID pre-SIGTERM, assert post-revive PID differs AND non-zero AND health green within N s; assert exactly ONE new daemon (no double-spawn) via process count.
- Crew start: assert registry record exists AND `crew-<name>` tmux session exists — both, not either.
- Orphan sweep: kill a crew's tmux out-of-band, boot daemon, assert it reaped the dead crew AND did NOT reap a live crew (false-positive guard).
- Suspend/resume: hash full state (open beads, leases, crew registry) before sleep; after resume assert byte-identical — not "came back up."

**S2 — Harnesses/handlers**
- Per harness × {local,remote}: assert terminal bead transition is daemon-written (trailer present) AND matches expected; a bead stuck in `ready` is a FAIL, not a skip.
- H13 lost-wakeup: assert `agent_ready` consumed exactly once; run ≥20 launch iterations, assert zero timeouts (one hang = BLOCK).
- H11 codexwire string-id: feed a string id, assert no panic AND graceful error class.
- HC-004 double-spawn: restart mid-run, assert run process count == 1 AND no second worktree.
- A9 HC-contract drift: assert live handshake fields match HC spec version, byte-compared.

**S3 — Workflow matrix**
- DOT: assert the FULL graph executed — every node reached, edges in order, review node fired — via node-transition events. DOT collapsing to single-mode = FAIL (prove the workflow graph, not getting-started mode).
- review-loop: inject one REQUEST_CHANGES, assert it re-dispatched ≥1 revision.
- Assert `RUN + SKIPPED == enumerated total` (no silent cell drop).

**S4 — Comms (Hamlet)**
- Assert comms log == checked-in `expected-transcript.txt` line-for-line (empty diff).
- Assert `count(delivered)==count(sent)` per recipient AND `count(distinct event_id)==count(logical msgs)`.
- Inject a duplicate `event_id`, assert exactly one logical delivery survives.
- Assert monotonic per-speaker sequence (no reordering).

**S5 — Keeper timing**
- Assert WARN fires in band around 30k (e.g. 28k–32k) and ACT at its threshold — cite both gauge log lines with token counts.
- Assert resumed session re-hydrates intent (pre-restart bead/task marker == post-resume).
- C4: assert explicitly REPRODUCED / NOT-REPRODUCED with captured pane state (prove the inject actually triggered).
- Negative guard: keeper does NOT fire below WARN.

**S6 — Log watcher**
- FAIL set: any `level=error`/`panic`/`fatal`, goroutine/WaitGroup/fd growth over baseline, orphan PID or held lease at teardown.
- Assert fd + goroutine counts at teardown ≤ baseline + threshold (capture both numbers).
- Watcher liveness: heartbeat every N s in its own log — a watcher that died at minute 3 and "found nothing" is a FAIL of S6, not a PASS.
- Structured queries only (`harmonik subscribe --json`/`jq`); zero hand-grep by run_id.

**S7 — Fault injection** (each fault: explicit hard assertion + evidence)
- H2 lease truncate: worktree still on disk AND NOT force-GC'd.
- H4/H5 truncated verdict/auto-status: run marked **inconclusive**, NOT "absent"/DONE.
- H6 concurrent submits: both reflected or a defined winner, NO lost update (a dropped submit = BLOCK).
- H7 drain-while-emitting: clean exit code, no `WaitGroup misuse`.
- H8 remote Kill: NO local PID signalled (capture local PID liveness across the Kill).
- H3 revert-then-sweep: reverted bead NOT set DONE (fabricated-DONE guard; DONE here = automatic BLOCK).
- Each fault: daemon SURVIVES (health green after); a fault that wedges it is itself a finding.

## B. NEW SECTION — "PASS/BLOCK Rubric" (insert before §5)

ASSESSMENT.md classifies every finding into exactly one tier; verdict = max tier reached.
`BLOCK` if any Tier-0 OR any required §2 cell fails at the risk floor; else `PASS-with-concerns`
if any Tier-1; else `PASS`.

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

**Risk-floor coupling (good-enough §3):** any finding touching core dispatch, daemon lifecycle,
the merge/commit gate, or the remote path is floored one tier HIGHER — a Tier-1 defect there
escalates to Tier-0. The assessor may raise a tier on blast-radius; never lower below the path floor.

## C. NEW RULE — "Evidence standard (no unfalsifiable claims)" (insert into §3)

Every assertion resolves to ONE of: (a) an exit code, (b) a specific `events.jsonl`/daemon-log line
quoted with its `jq` selector, (c) a file hash / byte-count / path-exists check, or (d) a state read
(`br show`, registry read, health RPC) with the value shown. Prohibited verdict language with no
artifact: "looks fine", "seems to work", "no obvious issues", "observed working", "ran cleanly".
A suite that RAN but recorded no hard pass/fail assertion is **INCONCLUSIVE**, never PASS —
inconclusive at the risk floor is BLOCK-equivalent for that cell.

## D. NEW SUBSECTION — "Build-SHA pinning (anti-stale-PASS)" (insert in §0)

1. **Pin at start.** Record `HK_BASELINE_SHA = git rev-parse HEAD` before suite 1; build the sandbox daemon from exactly this SHA; record its build-stamp/hash. Tag every assertion with the SHA.
2. **Freeze during the run.** No mid-campaign rebuild; captain's new fixes do NOT enter this run.
3. **Mandatory re-run-at-baseline.** Verdict valid ONLY for `HK_BASELINE_SHA`. If any commit lands after the pin, the PASS is void → re-run at new tip (min S2/S3/S7 + any suite whose paths the new commits touched). A mismatch is recorded **STALE — re-run required**, not PASS.
4. **Green-tree precondition.** Assert `go build`/`test`/`vet`/`-race` green at the pin and cite outputs — a campaign on a red tree is void.
