# Crash-Recovery Adversary Review — beads-integration.md v0.3.0

**Reviewer role.** Crash-Recovery Adversary (round 2).
**Target.** `/Users/gb/github/harmonik/specs/beads-integration.md` v0.3.0 (696 lines; 32 numbered BI requirements plus letter-suffixed; 4 invariants; 7 OQs).
**Lens.** Pressure the BI adapter at the boundary where harmonik's transactional discipline (intent log + fsync-boundary events + git) meets Beads's transactional discipline (SQLite WAL + audit log). The adapter is a composition point between two independent durability systems; crash-recovery has to hold across *both* of them, not just inside each.
**Date.** 2026-04-24.
**Status.** R2 input; v0.3.0 is `status: draft` pending this + the skeptic + adapter-author R2 reviews.

---

## 1. Verdict summary

v0.3.0 is **markedly stronger than v0.2** on crash-safety. The R1 integration landed three load-bearing crash-safety improvements:

- **BI-031 reframed away from "Beads's own idempotency" to a status-check-before-reissue protocol** (lines 366–376). This was the single biggest crash-recovery hole in v0.2 — BI v0.2 assumed a Beads-native idempotency-key audit query that the Beads surface has never declared. The reframed protocol is Beads-idempotency-independent and survives Beads surface changes.
- **BI-031b `br show` JSON-consistency dependency** (lines 381–385) plugs the torn-JSON attack against the status-check read: a parse failure classifies as `BrSchemaMismatch` and routes to divergence, not to "status differs."
- **BI-025c timeout discipline** (lines 322–327) bounds adapter hang exposure to 5s/10s.

But v0.3.0 still leaves **eleven crash-recovery gaps**, ranked by severity in §1.1 below. None reopens a locked decision; none requires a subsystem re-envelope; nine are one-or-two-sentence normative additions. Two (finding 2 directory-fsync, finding 10 startup-race-between-orphan-sweep-and-dispatch) need small new requirements. The most consequential gap is **finding 1 (partial-write window between `br` WAL-commit and `br` exit-code delivery)** — BI-031 step 3 reads the post-state as evidence of success, but SQLite may have *partially* committed the transaction and then crashed before the adapter observed a return code; the status-check cannot disambiguate "Beads committed + adapter missed exit-code" from "Beads committed + adapter crashed reading stdout" without additional discipline.

### 1.1 Gap summary table

| # | Gap | Severity | Fix shape |
|---|---|---|---|
| 1 | Timeout-fires between Beads WAL-commit and adapter exit-code observation: BI-031 treats "status matches post-state" as success evidence, but adapter never sees the `br` exit code | blocking | 1-paragraph amendment to BI-031 step 3 |
| 2 | No parent-directory fsync after intent-file create AND after intent-file delete; `.harmonik/beads-intents/` dirent is not durable | blocking | 2-sentence addition to BI-030 |
| 3 | Crash ordering in BI-030 is underspecified: the spec does not pin `fsync(intent_file)` ordering against `write(2)` or `rename(2)` | important | replace "MUST fsync the file" with the full write-temp + rename + dir-fsync sequence |
| 4 | Cached `br --version` handshake (BI-024a) is stale if operator upgrades Beads mid-session; new `br` invocations use new exit-code semantics | important | add BI-024b re-probe requirement on `BrSchemaMismatch` |
| 5 | Disk-full (ENOSPC) during intent-log write: no failure-taxonomy row; adapter can't write the intent, can't emit `store_divergence_detected` if JSONL is ENOSPC-blocked too | important | add §8 `intent_log_write_failed` routing row |
| 6 | Two-daemon-same-Beads-store misconfiguration: BI-009 cites atomic-claim as defense, but a Cat 3c auto-resolver from daemon A can race a `br claim` from daemon B through the same SQLite file | important | out-of-contract per per-project memory, but spec should name the detection + refusal surface |
| 7 | Intent-file directory deleted by operator (`rm -rf .harmonik/beads-intents/`) between crash and recovery: BI-031 recovery loses all pending intents silently | important | add BI-031c directory-presence sentinel + Cat 6a classification |
| 8 | BI-010a Cat 3c reconciliation auto-resolver and operator `br close` race — operator uses raw `br` (outside adapter), out-of-band write hits Beads while reconciliation's mid-flight `br close` observes "already closed" | important | 1-sentence amendment to BI-010b for BrConflict-during-reissue path |
| 9 | `br` subprocess hangs past 10s timeout: BI-025c classifies `BrUnavailable`, but the hung `br` may still hold SQLite WAL lock + eventually commit after the timeout fires | blocking | add BI-025c continuation: kill-not-terminate; SIGKILL policy; bounded-retry with status-check guard |
| 10 | Concurrent daemon startup: orphan sweep finds intent files; daemon races to dispatch Cat 3a resolver for intent X while same intent's `br` call is *still running* as a re-parented zombie of the prior daemon | blocking | add BI-031d pre-recovery subprocess-scan requirement |
| 11 | Beads SQLite file corruption (partial WAL write at OS layer) — `br show` returns torn JSON or parse error; BI-031b classifies as `BrSchemaMismatch`, refuses reissue, but the intent file is never released | important | add BI-031e escalation path to Cat 6b for persistent-parse-failure |
| 12 | `schema_version` on intent files is declared (§6.1a line 492) as "N-1 readable" but no readability-check-before-consume mechanism; an intent file from N+1 after a downgrade-then-restart is consumed silently | nit | add BI-031f schema-version-check on recovery |
| 13 | Intent-file timestamp `requested_at` is specified "RFC 3339 wall clock" (§6.1a line 491); crash-recovery that depends on clock ordering regresses if wall clock regressed between sessions | nit | add "MAY use monotonic-within-process for ordering; MUST NOT depend on wall-clock monotonicity across restarts" |
| 14 | Beads mid-session schema migration: operator runs `br migrate` (hypothetical); ongoing adapter calls see an in-progress-migration error; not mapped in §6.1a taxonomy | nit | add `BrMigrationInProgress` row OR fold under `BrDbLocked` with 60s extended retry |

Severity key: **blocking** = must be fixed before R2 `reviewed` status; **important** = should be fixed in R2 integration; **nit** = can defer to OQ.

No finding reopens locked decision #13 (Beads adopted) or #14 (skill injection).

---

## 2. Scenarios probed

Fourteen scenarios, focused on the physical-reality boundaries that emerge from the harmonik-intent-log ↔ Beads-SQLite-WAL interaction.

### Scenario 1 — Crash between Beads WAL-commit and `br` exit-code delivery to adapter

**Affected requirements.** BI-030 (lines 359–364, intent log + fsync); BI-031 (lines 366–379, status-check-before-reissue); BI-031b (lines 381–385, JSON consistency); BI-009 (atomic-claim); §6.1a (BrError taxonomy lines 466–482).

**What the spec says.** BI-031 declares a three-branch recovery: (3) if status matches post-state, declare done; (4) if matches pre-state, re-issue; (5) if neither, Cat 3a torn-write. The protocol is derived from the premise that Beads's transition is atomic at the SQLite-WAL layer — if `br claim` returns, WAL is committed; if it doesn't return, WAL is not committed.

**What actually happens.** SQLite's `COMMIT` path has three physical fsync steps (on default `journal_mode=WAL`): (a) WAL page fsync, (b) WAL header fsync, (c) optional checkpoint fsync. The transaction is durable after (b). But `br`'s exit-code delivery is a separate kernel-level operation: after SQLite's COMMIT returns, `br` has to `exit(0)` which takes the child through `_exit` → parent's `wait4` → adapter observes exit code.

A SIGKILL of `br` between (b) WAL-header-fsync and `_exit` leaves: Beads committed (post-state durable), adapter never sees exit code (either `wait4` returns prematurely with a signaled-exit, or the adapter is the one that died and `br` is orphaned to init). The adapter's knowledge at the moment it comes back is: "intent file present; `br` was launched; I have no exit-code evidence."

BI-031 step 3 says "if current status equals `intended_post_state` … the prior write landed before crash." This is correct for *Beads state* but not for *adapter knowledge*. The adapter cannot distinguish:

- (A) Beads committed; adapter saw exit=0; adapter crashed before `unlink(intent_file)`.
- (B) Beads committed; adapter never saw exit code.
- (C) Beads committed; a *different* caller (operator raw `br close`, out-of-adapter) produced this post-state.

All three land at the same "status matches post-state" observation. Case (A) is safe (adapter re-deletes, emits `bead_terminal_transition_recovered{recovery_path=status_match}`). Case (B) is safe by coincidence — but only because status-check is sufficient to infer completion. Case (C) is *unsafe*: the status matches post-state for an unrelated reason, and BI-031 step 3 marks the harmonik intent as complete even though harmonik's write never landed and a different actor produced the post-state.

BI-INV-004 (lines 416–418) says case (C) "MUST trigger Cat 3a." But BI-031 step 3 deletes the intent file and emits `bead_terminal_transition_recovered` — contradicting the invariant. The invariant's Cat 3a dispatch requires "a status-change observed in Beads with NO matching intent-log entry" — but in case (C) the intent log entry IS present (harmonik wrote it), it's just that harmonik's `br` call isn't what produced the change.

**Safe or unsafe.** **Unsafe at the edge.** The adapter cannot distinguish "my write landed" from "someone else's write landed to the same post-state." In single-daemon / single-actor configurations (the MVH norm), case (C) does not arise. But the spec does not *restrict* to that configuration — BI-002 says "All Beads interactions route through the `br` CLI" (mechanism of access, not actor count), and the Beads-CLI skill gives agents `br` read access. An agent with the Beads-CLI skill that reads-only cannot violate this, but the spec nowhere forbids a co-installed operator script from issuing `br close`.

**Concrete spec-text proposal.** Amend BI-031 step 3 to:

> 3. If the current status equals the `intended_post_state` for this transition AND the bead's audit-log tail (queried via `br show <bead_id> --format json`) contains an entry whose `requested_at` precedes the current daemon's start time AND whose transition matches this intent's `(op, bead_id)`, the recovery is a no-op write … [original text continues]. If the audit-log tail does NOT contain a matching entry (status was reached by some other path), the divergence MUST emit `store_divergence_detected{divergence_kind="beads_status_preceded_intent"}` per [event-model.md §8.6] and route to Cat 3a (NOT step 3's no-op completion). This closes the scenario where an out-of-adapter actor produced the post-state concurrently with the adapter's pending intent.

Cross-link BI-INV-004 to BI-031 step 3's new audit-match sub-check so they agree.

---

### Scenario 2 — Parent-directory fsync missing after intent-file create and delete

**Affected requirements.** BI-030 (lines 359–364); BI-031 (line 372, "delete the intent file (with directory fsync)"); §6.2 on-disk layout (lines 504–513); §4.10 durability cite of [event-model.md §4.4].

**What the spec says.** BI-030: "persist an intent-log entry to `.harmonik/beads-intents/<idempotency_key>.json` and MUST fsync the file per the durability contract of [event-model.md §4.4]." BI-031 step 3 mentions "directory fsync" parenthetically only for the delete path. The create path (BI-030) mentions fsync only of the file, not of the parent directory. Event-model §4.4 (EV-015–EV-017) scope is about JSONL fsync cadence; it does NOT prescribe directory-fsync discipline for arbitrary files written by other subsystems.

**What actually happens.** POSIX semantics: `fsync(fd)` on a newly-created file flushes the file's data blocks and inode metadata, but the *parent directory's* dirent entry (the entry that says "this filename is at this inode") is a separate durability domain. Without `fsync(parent_dir_fd)` after create, a power-loss can leave: file inode durable with correct contents, parent directory's dirent lost, so on recovery `readdir(.harmonik/beads-intents/)` yields nothing and the adapter's recovery sweep finds no pending intents — even though the intent (intended to be) is there.

The symmetric attack on the delete path: `unlink(intent_file)` followed by power-loss before `fsync(parent_dir_fd)` leaves the dirent intact (pointing at the inode which is now freed). On recovery the adapter sees a stale intent that its BI-031 reissue path will drive through again, even though the underlying `br` write landed and was cleaned up.

BI-031 parenthetically names "directory fsync" only on the delete path (line 372). The create path does NOT name it. This asymmetry is a crash-safety hole.

**Safe or unsafe.** **Unsafe on power loss.** ext4 `data=ordered` is usually forgiving in practice, but APFS power-loss and any `data=writeback` or non-journaling fs (NFS, SMB-shared workspaces) will lose the dirent. `data=ordered` on ext4 does NOT guarantee dirent durability without explicit dir-fsync; it only guarantees data precedes metadata in the journal.

**Concrete spec-text proposal.** Amend BI-030 to:

> Before issuing the `br` subprocess call for a terminal-transition write, the adapter MUST persist an intent-log entry as follows: (a) create `.harmonik/beads-intents/<idempotency_key>.tmp` with the full JSON payload; (b) `fsync(2)` the temp file; (c) `rename(2)` it to `<idempotency_key>.json`; (d) `fsync(2)` the parent directory `.harmonik/beads-intents/`. The intent is durable only after step (d). After the `br` call returns success, the adapter MUST (e) `unlink(2)` the intent file and (f) `fsync(2)` the parent directory. Step (f) is load-bearing: a crash between (e) and (f) leaves a dirent pointing at a freed inode and the recovery sweep re-drives the already-landed write.

Add a corresponding sensor on BI-INV-004 naming the directory-fsync discipline as part of the audit trail.

---

### Scenario 3 — Torn `br claim` transaction: SQLite partial commit

**Affected requirements.** BI-031 (status-check); BI-031b (JSON consistency); BI-025a (BrError); BI-009 (atomic-claim); §6.1a (exit code 2 → BrConflict, exit code 3 → BrDbLocked).

**What the spec says.** BI-031 assumes that at any crash point, `br show <bead_id>` returns one of three states: pre-state, post-state, or "neither." SQLite's WAL mode is atomic: a transaction is either fully committed or not committed. `br show` will return a coherent status value.

**What actually happens.** SQLite-WAL's atomicity holds at the *database level* for the transaction proper. But `br claim` may be a multi-statement transaction (update status + insert audit-log entry + write claim metadata). If the entire transaction is inside a single `BEGIN`/`COMMIT`, WAL gives atomicity. But Beads's implementation is not known to harmonik, and BI-002 declares the CLI is the only surface — so harmonik cannot assert the multi-statement shape.

A worse failure mode: `br claim` writes to the WAL, then the process is `kill -9`'d between WAL-commit and WAL-checkpoint. The next `br show` invocation reads the WAL correctly (SQLite recovery logic handles the partially-checkpointed WAL). Safe. But if the underlying filesystem has `data=writeback` (non-journaling) and a power loss occurs, the WAL file itself may be torn. The next `br show` fails — depending on SQLite version, either (a) returns the pre-state (WAL torn-write detector reverts), (b) errors out with SQLITE_CORRUPT, or (c) returns inconsistent data.

BI-031b maps parse failures to `BrSchemaMismatch`. A corrupt SQLite file may not produce a JSON parse failure at all — it may produce a `br` exit code 128+ (generic SQLite error) that BI-025a's mapping table (line 471–480) classifies as `BrOther`. The "unrecognized exit code" path in BI-025a (line 310) emits `store_divergence_detected{divergence_kind="br_unrecognized_exit_code"}` which is *correct*, but the adapter then has no defined recovery — it cannot re-issue (Beads is broken), cannot declare done (status unknown), cannot continue normal operation.

**Safe or unsafe.** **Partially safe.** The `BrOther` + `store_divergence_detected` path surfaces the problem to reconciliation. But the spec does NOT pin the adapter's behavior *during* this state: does it keep trying? Block on startup? Enter degraded state? The implementer has to invent the answer.

**Concrete spec-text proposal.** Amend BI-025a: "`BrOther` AND `BrSchemaMismatch` classifications produced during daemon startup (PL-005 step 6) MUST cause the daemon to remain in `degraded` state per [process-lifecycle.md §4.3 PL-010] until the condition clears (as evidenced by `br --version` succeeding AND a subsequent `br show <any_known_bead>` returning a parseable payload). Intent-log recovery (BI-031) MUST NOT proceed while any intent's target bead returns `BrOther` or `BrSchemaMismatch` on status-check; the intent remains on disk for a future recovery attempt."

---

### Scenario 4 — Beads version drift mid-session: operator `brew upgrade br` while daemon runs

**Affected requirements.** BI-024 (version pin); BI-024a (startup handshake, lines 301–306); BI-026 (adapter-change-for-breaking-Beads); BI-025a (exit-code taxonomy).

**What the spec says.** BI-024a says the handshake runs at startup during PL-005 step 6. The pinned version is cached in-process. Subsequent `br` invocations inherit the trust.

**What actually happens.** On a long-running daemon session (hours or days), an operator may `brew upgrade br` or equivalent. The next `br` invocation by the adapter invokes the *new* binary. If the new version has changed exit-code semantics (e.g., Beads bumps from 0.5 → 0.6 and renames error code 3 "DbLocked" to code 7 "Busy"), BI-025a's mapping table returns `BrOther` for a condition that would have been `BrDbLocked` — driving the adapter into the wrong retry path (Cat 3 generic investigator dispatch instead of bounded retry).

BI-026's adapter-change-for-breaking-Beads-change contract is about *harmonik release* level, not runtime. The spec has no runtime re-probe mechanism.

**Safe or unsafe.** **Unsafe silently.** The adapter will produce misleading classifications; reconciliation will dispatch investigators for non-divergences; the daemon's log will fill with `store_divergence_detected{divergence_kind=br_unrecognized_exit_code}`.

**Concrete spec-text proposal.** Add BI-024b:

> **BI-024b — Runtime re-probe of `br --version` on suspicious classification.** On any `BrOther` or `BrSchemaMismatch` classification (§6.1a) produced after daemon startup, the adapter MUST invoke `br --version` (subject to BI-025c's 5s read timeout) and compare the result against the cached version from BI-024a. A mismatch MUST emit `daemon_degraded{reason="br_version_changed_midsession"}` per [event-model.md §8.7.5] and MUST cause the daemon to transition to `degraded` per [process-lifecycle.md §4.3 PL-010], suspending intent-log recovery and new dispatch until an operator resolves the mismatch (via `harmonik stop` + restart against the new Beads version, which re-runs BI-024a). The daemon MUST NOT silently adapt to a mid-session Beads upgrade.

This is the BI analogue of PL's ntm-mid-session-drift concern.

---

### Scenario 5 — `br` subprocess hangs past timeout: adapter kills, but `br` has already WAL-committed

**Affected requirements.** BI-025c (timeout lines 322–327); BI-031 step 3 (status-match); §6.1a ((timeout) → BrUnavailable).

**What the spec says.** BI-025c: "Timeout expiry MUST classify as `BrUnavailable`." No subprocess-termination discipline is declared. §6.1a (line 478) maps "(timeout) → Unavailable."

**What actually happens.** At the 10s write-timeout boundary, `br` is probably in one of:

- (a) Waiting on SQLite lock held by another Beads client — will complete eventually; harmless.
- (b) Blocking on disk IO — may complete or not.
- (c) Already WAL-committed, slowly returning through the CLI parse-response path — will produce a successful exit code *after* the adapter times out.

The adapter's required behavior after timeout is unspecified. If the adapter SIGKILLs the `br` process, cases (a) and (b) abandon clean; case (c) becomes "Beads committed, adapter never saw exit code" (scenario 1 redux). If the adapter *doesn't* SIGKILL, the `br` process lingers as a zombie holding a socket fd / SQLite lock, blocking subsequent `br` invocations.

BI-031's intent-file-on-disk path will re-recover this on restart — but only after the daemon has already emitted `daemon_degraded` and refused to dispatch new work. The timeout-classified-as-BrUnavailable path at §8 (line 541) says "bounded retry per PL-010 cadence" but PL-010 is a degraded-state retry (10s cadence) which is compatible with the bead being semi-committed.

**Safe or unsafe.** **Unsafe without explicit termination discipline.** The adapter must kill the subprocess *and* leave the intent file on disk for BI-031 recovery to pick up on a future (or same-process) retry.

**Concrete spec-text proposal.** Amend BI-025c:

> Timeout expiry MUST classify as `BrUnavailable`. On timeout, the adapter MUST (a) send `SIGTERM` to the `br` subprocess; (b) wait an additional 2s for graceful exit; (c) send `SIGKILL` if the subprocess has not exited; (d) reap the exit code via `wait4`. The intent-log file MUST remain on disk; the adapter MUST NOT delete it. A retry of the same terminal-transition MUST go through the BI-031 status-check-before-reissue path, NOT a direct re-invocation (the prior subprocess may have WAL-committed in the timeout window).

---

### Scenario 6 — Concurrent claim from two daemons pointing at the same Beads store

**Affected requirements.** BI-009 (atomic-claim, lines 135–139); BI-INV-001 (no intra-run writes); PL-002a (fd-lifetime lock — project-local).

**What the spec says.** BI-009 says "two agents or daemons cannot simultaneously observe the same bead as claimed-by-self." The atomicity is Beads-native (SQLite row-lock + audit append). The user memory says per-project daemons; the spec does not restrict multi-daemon-same-Beads-store.

**What actually happens.** Two daemons A and B share `.harmonik/beads/` somehow (symlinked from one project to another; two worktrees of the same project with a symlinked `.harmonik/beads`; hypothetical cross-project bead reference per OQ-BI-005). Both dispatch loops read `br ready`, both decide to claim bead X.

- Daemon A's adapter writes intent `idempotency_key_A` (= A's run_id + transition_id + claim).
- Daemon B's adapter writes intent `idempotency_key_B`.
- Daemon A invokes `br claim X`; SQLite gives A the row lock, claim succeeds.
- Daemon B invokes `br claim X`; SQLite returns exit 2 (Conflict, BrConflict per §6.1a).

Daemon B's BrConflict path per §8 maps to "Cat 3a … concurrent-claim race; idempotency recovery per §4.10." But BI-031 is for *crash recovery*, not live concurrent-race. Daemon B's adapter has an intent file on disk that was NOT a crash remnant — it was written seconds ago for a claim that collided. BI-031's status-check says: if status equals `intended_post_state` (`in_progress`), declare success. But the in_progress was produced by daemon A, not by B! Daemon B will mark its intent as satisfied, proceed to dispatch a run against bead X, race with daemon A's already-running run.

This is the Scenario-1 case (C) in a different guise — the adapter cannot distinguish "my claim landed" from "someone else's claim landed."

**Safe or unsafe.** **Unsafe in multi-daemon-shared-store.** Supported configuration memory says "no." Spec does not prohibit explicitly.

**Concrete spec-text proposal.** Add BI-009a:

> **BI-009a — Multi-daemon-same-Beads-store is out of contract.** Harmonik's crash-safety and idempotency contracts (§4.10, BI-031) assume a single daemon per Beads SQLite file per project. Two or more daemons sharing a Beads store is unsupported; the daemon MUST detect this configuration at startup via a Beads-side lease (post-MVH; tracked as OQ-BI-008) or via a harmonik-side detection (at minimum, a `.harmonik/beads.owner` file fsynced at PL-005 step 6 naming this daemon's PID + start-time; any pre-existing file with a live PID different from this daemon's MUST fail startup with the `beads-unavailable` exit code per [operator-nfr.md §8]). This requirement closes the concurrent-claim-race-across-daemons hole left by BI-009's atomic-claim which secures single-claim-per-row but NOT "which caller's intent was satisfied."

This matches user memory on single-daemon-per-project; spec should name the detection mechanism.

---

### Scenario 7 — Crash during reconciliation Cat 3c auto-resolver's `br close` reissue

**Affected requirements.** BI-010b (reconciliation writes, lines 179–183); BI-031 (status-check); Cat 3c detector (reconciliation §8.6); §4.7 INFORMATIVE Cat 3a/3c (lines 276).

**What the spec says.** BI-010b: "Reconciliation auto-resolvers MAY fire `close` or `reopen` writes … MUST route through the §4.8 adapter and MUST carry the §4.10 idempotency-key infrastructure." The idempotency key for a Cat 3c auto-resolver's close is `<reconciliation_run_id>:<verdict_transition_id>:close`. §4.7's informative note explains Cat 3c's action = fire a `close` write identically to a daemon-driven close.

**What actually happens.** Cat 3c fires mid-startup (PL-005 step 8, reconciliation dispatch). Sequence:

1. Cat 3c detector: bead Y is `in_progress` in Beads, merge commit exists on integration branch with matching Harmonik-Bead-ID.
2. Auto-resolver fires `br close Y` via adapter.
3. Adapter writes intent file `<rec_run_id>:<transition_id>:close.json`.
4. Adapter invokes `br close Y`.

If the daemon crashes between step 3 and step 4's success delivery, the next daemon's startup runs the orphan sweep per PL-006 which leaves intent files on disk for Cat 3a classification (per PL-006 line 238). Then PL-005 step 8 dispatches reconciliation *again*. The Cat 3c detector will fire against the same bead Y (still `in_progress` if step 4 didn't land, or `closed` if it did). In parallel, the Cat 3a detector sees the pending intent file.

Ordering-priority per reconciliation §8.11's priority list (line 293): "Cat 3c → Cat 3b → Cat 3a." Cat 3c fires *first* on the same bead. Cat 3c's auto-resolver sees intent from the *prior* daemon's attempt AND wants to fire its own `br close`. Does it produce a *new* intent file (with a new idempotency_key using the new `rec_run_id`)? Or does it re-use the prior intent?

The spec does not answer. BI-010b says every reconciliation-driven write "MUST carry the §4.10 idempotency-key infrastructure" — which implies each new auto-resolver invocation gets its own key. So now there are TWO intent files pointing at the same `(bead_id, op=close)` for the same underlying run. Each will be recovered independently by BI-031. The first one's status-check sees `closed` (if the prior landed) or `in_progress` (if not); the second one does the same, finds `closed` (because the first just did it), declares success.

**Safe or unsafe.** **Partially safe** — both intents eventually converge on the correct post-state. But two `reconciliation_verdict_executed` events are emitted (one per intent); auditors see duplicate verdict-execution for the same underlying divergence.

**Concrete spec-text proposal.** Amend BI-010b:

> Reconciliation auto-resolvers (Cat 3a, 3b, 3c per [reconciliation/spec.md §8.4a, §8.5, §8.6]) MAY fire `close` or `reopen` writes … [original]. On detecting a stale intent file for the same `(bead_id, op)` from a prior reconciliation instance (discriminable by the `run_id` prefix of the idempotency key — a reconciliation run_id is distinguishable from a dispatch run_id per [reconciliation/spec.md §4.2]), the auto-resolver MUST route through BI-031's recovery path against the prior intent BEFORE emitting its own intent; if the prior intent's status-check resolves to `status_match`, the new auto-resolver emits `reconciliation_verdict_executed` with `verdict_already_satisfied=true` (new field; coordinate with [reconciliation/schemas.md §6.4]) and does NOT invoke `br` itself.

---

### Scenario 8 — Disk-full (ENOSPC) during intent-log write

**Affected requirements.** BI-030 (intent-log write); BI-031 (recovery); §8 error taxonomy (lines 534–546); [operator-nfr.md §4.1 ON-003] startup catalog.

**What the spec says.** BI-030's fsync discipline cites event-model §4.4. §8 taxonomy has no `intent_log_write_failed` row. ENOSPC is implicitly in the "filesystem-unwritable" startup category per the operator-nfr failure-mode catalog, but only as a startup-time condition, not as a mid-session condition.

**What actually happens.** ENOSPC on `.harmonik/beads-intents/` write mid-session. The adapter attempts to write intent file, `write(2)` returns -1 with errno=ENOSPC. Adapter cannot proceed with `br` call — BI-030 says intent MUST be fsynced BEFORE `br` call. What does the adapter do?

Three options, none declared:

- (A) Emit `store_divergence_detected{divergence_kind=intent_log_write_failed}` and fail the run with `ErrTransient`.
- (B) Fall through to the `br` call without the intent (violates BI-030 "MUST persist"). Unsafe: a crash mid-`br` with no intent file means the Cat 3a detector has no evidence (violates BI-INV-004).
- (C) Block the dispatch loop and retry until space clears. Unsafe: blocks all new dispatch; disk-full on one project blocks all new work.

None of (A)/(B)/(C) is declared. An implementer picks one.

Separately: if ENOSPC affects `.harmonik/` as a whole, the JSONL writer for `store_divergence_detected` ALSO can't write. The fallback of BI-025a (emit `store_divergence_detected` on unrecognized exit) is doubly-blocked.

**Safe or unsafe.** **Unsafe and under-specified.**

**Concrete spec-text proposal.** Add §8 taxonomy row:

> | `intent_log_write_failed` | Cat 0 (infrastructure) per [reconciliation/spec.md §4.3 RC-012] | daemon enters `degraded`; intent-dependent writes blocked; bounded retry per PL-010 cadence; persistent failure beyond 60s → emit `daemon_degraded{reason=disk_full}` and exit code 8 |

And add BI-030a:

> **BI-030a — Intent-log write failure is a Cat 0 condition.** ENOSPC, EACCES, EIO, or any other errno from the intent-log-create sequence (BI-030 steps a–d) MUST classify as Cat 0 per [reconciliation/spec.md §4.3] and MUST transition the daemon to `degraded` per [process-lifecycle.md §4.3]. The adapter MUST NOT proceed to the `br` call with a missing intent; BI-INV-004's "no intent, no write" contract is load-bearing for Cat 3a's evidence path.

---

### Scenario 9 — Beads SQLite file corrupted by partial write at OS layer (post-power-loss)

**Affected requirements.** BI-031b (BrSchemaMismatch lines 381–385); §6.1a exit-code 4 → SchemaMismatch; §8 SchemaMismatch → exit code 8 `beads-unavailable`.

**What the spec says.** BI-031b classifies `br show` JSON parse failures as `BrSchemaMismatch`; §8 routes `SchemaMismatch` → exit code 8 (startup failure; operator must align harmonik release with Beads version).

**What actually happens.** A post-power-loss torn write to Beads's SQLite file (WAL torn, checkpoint torn, or main-db torn) produces a `br` invocation that exits with either SchemaMismatch (4), Other (non-listed), or a SQLite-corrupt-specific code (varies by Beads version). The spec's response is "fail daemon startup with exit code 8; operator upgrades harmonik."

But the corruption is NOT a version-skew problem — it's a physical-disk problem. The operator remediation (upgrade harmonik) is the wrong action. The right action is "restore Beads database from backup" or "run `br repair`" (if Beads provides one).

Worse: an intent file on disk from the pre-corruption session now cannot be recovered. BI-031 step 2's `br show <bead_id>` query fails with `BrSchemaMismatch`. BI-031b refuses reissue. The intent file stays on disk. The daemon cannot start. The operator has no clear remediation path.

**Safe or unsafe.** **Partially safe** (daemon refuses to continue; no silent corruption). **Unsafe** in recovery path — operator is stuck without remediation.

**Concrete spec-text proposal.** Amend BI-031b:

> … A `BrSchemaMismatch` recovery path MUST emit `store_divergence_detected{divergence_kind="beads_schema_drift"}` per [event-model.md §8.6] AND MUST classify the underlying condition via a secondary probe: if `br --version` itself fails or returns a parseable version matching the pinned version per BI-024a, the condition is NOT version-skew but Beads-file-integrity; the daemon MUST emit `store_divergence_detected{divergence_kind="beads_db_corrupt"}` and route to Cat 6b integrity violation per [reconciliation/spec.md §8.11a] rather than Cat 0 + exit code 8. Cat 6b's operator-remediation-required path is the correct destination.

This also requires adding `beads_db_corrupt` to the event payload schema (coordinate with event-model.md).

---

### Scenario 10 — Concurrent startup after crash: prior-daemon's `br` still running as re-parented zombie

**Affected requirements.** BI-031 (recovery); PL-006 (orphan sweep, does NOT currently sweep `br` subprocesses — only handler subprocesses); PL-005 step 6 (Beads availability check).

**What the spec says.** BI-031's recovery sequence assumes: at the moment the adapter runs `br show <bead_id>`, no prior `br` invocation is still holding locks. PL-006's orphan sweep (process-lifecycle.md §4.3) kills *handler* subprocesses bearing the provenance marker; it does NOT specifically target `br` subprocesses the adapter spawned. The adapter's `br` processes are NOT handlers — they are short-lived tool invocations, and PL-006a's provenance marker is documented as applying to handler subprocesses.

**What actually happens.** Daemon crashes mid-`br claim`. The `br` subprocess is re-parented to init (PID 1) by the kernel. It may still be running (holding SQLite WAL lock). New daemon starts. PL-006 orphan sweep does NOT see the orphaned `br` (not a handler; no provenance marker in the PL-006a sense because `br` is not launched via ntm and may not inherit the `HARMONIK_PROJECT_HASH` env var). BI-031 recovery invokes `br show <bead_id>`. SQLite WAL has a conflicting transaction in flight — `br show` blocks on WAL lock. The 5s read timeout fires. `BrUnavailable`. Intent-log recovery cannot proceed.

Meanwhile the old `br` eventually completes its transaction (WAL-commits the claim). A subsequent BI-031 attempt succeeds (status-check sees post-state match), marks intent complete. But during the interim window (5s to minutes depending on the old `br`'s progress), the daemon is `degraded` on startup; the intent is unresolvable; dispatch is blocked.

**Safe or unsafe.** **Unsafe — extended downtime window.** And: the spec doesn't say the adapter should even *look for* orphan `br` processes. A persistent-zombie `br` (stuck on IO, or sleeping on a semaphore) can block forever.

**Concrete spec-text proposal.** Add BI-031d:

> **BI-031d — Orphan `br` subprocess sweep before intent-log recovery.** Before executing BI-031's recovery sequence for any intent file, the daemon MUST scan the process table for `br` subprocesses whose parent is init (PID 1) AND whose CWD is this project's root (per `/proc/<pid>/cwd` on Linux; `lsof -p <pid>` on darwin) OR whose `HARMONIK_PROJECT_HASH` environment variable matches this project. Any such subprocess MUST be sent SIGTERM, waited for 2s, then SIGKILL. The intent-log recovery MUST NOT proceed while any orphan `br` subprocess is alive. The adapter MUST pass `HARMONIK_PROJECT_HASH` to every `br` invocation (env-var inherit) to make this sweep possible.

Cross-link: add this sweep to PL-006's enumeration at the process-lifecycle R3.

---

### Scenario 11 — Intent-file directory deleted by operator between crash and recovery

**Affected requirements.** §4.10 INFORMATIVE (line 351, operator-nfr cleanup MUST preserve this directory); operator-nfr.md does NOT currently declare any such preservation rule.

**What the spec says.** §4.10 has an INFORMATIVE note: "`.harmonik/beads-intents/` is BI-owned. Operator-nfr clean-install / cleanup protocols MUST preserve this directory and any intent files for crash recovery; cite [operator-nfr.md §4.10] coordination." But operator-nfr.md §4.10 is about multi-daemon coordination, not cleanup; there is no cleanup requirement in operator-nfr.md that references intent-dir preservation. The cross-reference is aspirational.

**What actually happens.** An operator cleanup script (or `rm -rf .harmonik/beads-intents/` executed in a reset procedure) deletes the directory. Pending intents vanish. Next daemon startup: no intents to recover. But the `br` writes those intents represented may have partially landed. The Cat 3a detector per RC-INV-004 requires "a status-change observed in Beads with NO matching intent-log entry (present or absented after success) MUST trigger Cat 3a" — and absence-via-operator-rm is indistinguishable from absence-via-successful-completion.

Operator's action silently degrades to "all deleted intents treated as completed successfully" regardless of actual state.

**Safe or unsafe.** **Unsafe.** Operator mistake produces silent completion claims. BI-INV-004's sensor is defeated.

**Concrete spec-text proposal.** Promote the INFORMATIVE note to a requirement AND ADD a reciprocal operator-nfr obligation:

> **BI-030b — Intent-log directory is not operator-cleanup-eligible.** The directory `.harmonik/beads-intents/` MUST NOT be deleted by any operator-invoked harmonik command (including `harmonik stop`, `harmonik reset`, `harmonik upgrade`); see [operator-nfr.md §4.5 ON-019a] for the preservation requirement. At daemon startup (PL-005 step 0), the daemon MUST verify the directory's existence; if absent, the daemon MUST emit `daemon_startup_failed{failure_mode="intent_dir_missing"}` and transition to `degraded`. An operator who intentionally clears the directory MUST use the escalated command `harmonik reset --acknowledge-loss-of-pending-intents` (name TBD; coordinate with operator-nfr).

Coordinate a new ON-XXX in operator-nfr naming the preservation contract.

---

### Scenario 12 — Concurrent Cat 3c reconciliation auto-resolver and operator manual `br close` race

**Affected requirements.** BI-010b; BI-027 (Beads-CLI skill is agent-only, but spec is silent on operator raw `br`); BI-INV-004.

**What the spec says.** BI-027 restricts agents to the skill. The spec does NOT prohibit an operator from invoking `br close X` directly via their own terminal while the daemon is running. BI-INV-004 names out-of-band writes as Cat 3a triggers.

**What actually happens.** Cat 3c auto-resolver identifies bead Y needs a close write. Adapter writes intent `<rec_run_id>:<transition_id>:close.json`. Before the adapter invokes `br close`, an operator runs `br close Y` from a separate shell. The operator's write lands in Beads; bead Y's status becomes `closed`. Adapter then invokes `br close Y`, Beads returns exit 2 (`BrConflict`: already closed).

Per §6.1a, `BrConflict` maps to Cat 3a. §8 routes: "concurrent-claim race; idempotency recovery per §4.10." But BI-031 is for crash recovery; this isn't a crash — it's a live race. The adapter's intent file is still on disk. What does the adapter do with BrConflict on a live (non-recovery) invocation?

The spec doesn't say. Plausible implementer paths:

- (A) Treat BrConflict as success (bead reached post-state); delete intent; emit `reconciliation_verdict_executed`.
- (B) Treat BrConflict as failure; escalate to Cat 3 generic investigator; leave intent on disk.
- (C) Run BI-031 status-check; status matches post-state; declare success.

(A) and (C) converge. (B) is stricter but may thrash. BI-INV-004 says the out-of-band write triggers Cat 3a. So the operator's write should, in principle, trigger a Cat 3a detection — but only if reconciliation observes the audit-log mismatch, which happens at the next reconciliation cycle, not during the adapter's BrConflict handling.

**Safe or unsafe.** **Partially safe.** The final Beads state is correct; reconciliation will catch the out-of-band write eventually. But the adapter's BrConflict-on-live-invocation path is unspecified.

**Concrete spec-text proposal.** Amend BI-010b (or add BI-025e):

> **BI-025e — BrConflict handling on live (non-recovery) terminal-transition invocation.** If the adapter invokes a terminal-transition `br` call and receives `BrConflict` (per §6.1a), the adapter MUST route through the BI-031 status-check-before-reissue protocol: query `br show <bead_id>`; if status matches `intended_post_state`, treat as success (delete intent per BI-030, emit the transition's completion event) AND emit `store_divergence_detected{divergence_kind="concurrent_out_of_band_write"}` per [event-model.md §8.6] so reconciliation can trace the out-of-band actor; if status does NOT match, escalate to Cat 3 generic investigator per [reconciliation/spec.md §8.4].

---

### Scenario 13 — Fsync discipline gap: intent-delete + parent-dir-fsync ordering

**Affected requirements.** BI-030 (delete after success); BI-031 step 3 ("directory fsync").

**What the spec says.** Only BI-031 step 3 mentions directory fsync, and only on the recovery-success path. The normal-path post-success delete (BI-030) does not mention directory fsync. [Partially covered under Scenario 2 above; this scenario addresses the delete-specific ordering.]

**What actually happens.** Normal path: `br claim` returns success → adapter `unlink(intent_file)`. Crash before `fsync(parent_dir_fd)`. On recovery, dirent points to freed inode (EBADF on open); `readdir` may return the stale entry (ext4 behavior varies). Recovery sees "intent file exists" per `readdir` but `open` fails — adapter's BI-031 classification path does not cover this.

**Safe or unsafe.** **Unsafe.** Adapter's recovery sees a ghost intent.

**Concrete spec-text proposal.** Covered by Scenario 2 amendment to BI-030. Additionally:

> **BI-031 step 1a.** If `open(intent_file)` returns ENOENT or EBADF for an entry listed in `readdir(.harmonik/beads-intents/)`, the adapter MUST call `fsync(2)` on the parent directory AND retry `readdir`. A persistent ghost entry (entry listed, file unopenable) MUST classify as Cat 6a integrity violation per [reconciliation/spec.md §8.11] and halt intent-log recovery pending operator dispatch.

---

### Scenario 14 — Beads schema migration mid-session

**Affected requirements.** BI-024 (version pin); BI-024a (startup handshake); BI-025a (schema-mismatch on exit code 4).

**What the spec says.** BI-024 names "adapter change for backwards-incompatible Beads changes." BI-025a classifies schema mismatch. No requirement addresses Beads *migration* (a schema transform that rewrites the SQLite file in place).

**What actually happens.** An operator runs `br migrate` (hypothetical Beads command) while the daemon is running. Mid-migration, `br` invocations observe: the SQLite file is locked (SQLITE_BUSY → BrDbLocked), the schema version differs (BrSchemaMismatch), or the migration has renamed tables (BrOther with SQLite-error exit code).

BI-025a classifies reasonably — `BrDbLocked` → retry with bounded cadence; `BrSchemaMismatch` → exit code 8 startup failure. But the daemon isn't starting — it's running. A post-startup `BrSchemaMismatch` has no declared handling.

**Safe or unsafe.** **Unsafe.** Adapter may partially recover (BrDbLocked retries) or fail hard (BrSchemaMismatch with no runtime handling).

**Concrete spec-text proposal.** Folded into Scenario 4 (BI-024b runtime re-probe) plus Scenario 3 (BrSchemaMismatch at runtime → degraded, not exit code 8). No additional requirement; the existing proposals cover this.

---

## 3. Recommendations — minimum set for R2 `reviewed`

**Blocking (must fix before advance to `reviewed`):**

- Amend BI-030 to name full write-temp + rename + parent-dir-fsync sequence (Scenario 2, 13).
- Amend BI-031 step 3 to add audit-log-match sub-check closing the "someone else produced post-state" hole (Scenario 1).
- Amend BI-025c to name the SIGTERM/SIGKILL discipline on timeout (Scenario 5).
- Add BI-031d orphan-`br`-sweep before recovery (Scenario 10).

**Important (should fix in R2 integration):**

- Add BI-024b runtime re-probe of `br --version` on BrOther/BrSchemaMismatch (Scenarios 4, 14).
- Add BI-030a ENOSPC / intent-log-write-failure as Cat 0 (Scenario 8).
- Add BI-009a multi-daemon-same-store out-of-contract detection (Scenario 6).
- Promote §4.10 INFORMATIVE note to BI-030b intent-dir preservation requirement + reciprocal ON obligation (Scenario 11).
- Amend BI-025a: BrOther/BrSchemaMismatch during startup → degraded (not silent; Scenario 3).
- Amend BI-031b: secondary probe to distinguish version-drift from DB-corrupt → Cat 6b (Scenario 9).
- Amend BI-010b: stale-intent-from-prior-reconciliation handling (Scenario 7).
- Add BI-025e: BrConflict on live invocation routes through status-check (Scenario 12).

**Nits (defer to OQ):**

- BI-031f schema-version-check on intent-file consume (gap 12).
- Monotonic-ordering clarification on `requested_at` timestamp (gap 13).
- BrMigrationInProgress row OR extended-retry BrDbLocked (gap 14).

**Cross-spec coordination required:**

- Operator-nfr: new ON-XXX for intent-dir preservation during operator cleanup commands (Scenario 11).
- Reconciliation: coordinate `divergence_kind` value additions: `beads_status_preceded_intent`, `beads_db_corrupt`, `concurrent_out_of_band_write` (Scenarios 1, 9, 12).
- Event-model: coordinate `bead_terminal_transition_recovered{recovery_path}` new recovery-path enum values + `daemon_degraded{reason="br_version_changed_midsession"}` (Scenarios 1, 4).
- Process-lifecycle R3: extend PL-006 orphan sweep to cover orphan `br` subprocesses (Scenario 10).

---

## 4. What v0.3.0 got right

- **BI-031's reframe.** Status-check-before-reissue is the right shape. It removes the dependency on Beads-native idempotency-key support (which the Beads CLI does not declare) and grounds recovery in observable state.
- **BI-031b JSON-consistency dependency.** Correctly prevents torn-JSON from masquerading as "status differs."
- **BI-025a BrError taxonomy + `store_divergence_detected` on unrecognized exit codes.** Makes adapter-vs-Beads drift observable rather than silently misclassified.
- **BI-025c timeout discipline at the adapter boundary.** Bounds subprocess-hang exposure — even though subprocess-termination details are under-specified (Scenario 5), the timeout itself is a load-bearing crash-safety primitive.
- **BI-010a's tombstone/deferred mid-run treatment.** Classifying mid-run tombstone as Cat 3 is correct — the adapter cannot silently kill the run, and a soft-classify to reconciliation is the right escalation.
- **BI-INV-004's reshape with widened cross-subsystem span.** The intent-log + audit-log conjunction is the right evidence basis; extending the sensor across Cat 3a detector + adapter unit tests + cross-spec scenario tests gives the invariant teeth.

The integrated R1 work closed the two largest crash-safety holes (Beads-native-idempotency-assumption and missing `br` surface contract). v0.3.0's remaining gaps are second-order boundary issues — directory-fsync, multi-actor races, and operator-action/adapter-state coherence. None of them requires restructuring; all are addressable in a single R2 integration pass.

---

**End of review.**
