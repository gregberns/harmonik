# Crash-Recovery Adversary Review — workspace-model.md v0.3.0

**Reviewer role.** Crash-Recovery Adversary (round 2).
**Target.** `/Users/gb/github/harmonik/specs/workspace-model.md` v0.3.0 (999 lines; 49 requirements including retired, 5 invariants including retired, 11 OQs, 1 envelope).
**Lens.** Pressure the integrated v0.3.0 against kernel-grade failure modes: process crash, power loss, partial fsync, torn JSON, concurrent writer, permissions change, filesystem/NFS semantics, clock skew, ntm crash, git-tool race, UUID collision, interrupt storm. For every mechanism the spec declares, ask: what state is the system in if this crashes mid-operation, and what's the recovery rule.
**Date.** 2026-04-24.
**Status.** R2 integration input; v0.3.0 is `status: draft` pending this review.

---

## 1. Verdict summary

WM v0.3.0 fixed the round-1 lease-lifetime gap (WM-013a–d are the right shape) and closed the state-machine / sidecar-ordering contradiction (§7.1 + §7.2 + WM-016 agree). The spec is **substantially crash-safe against single-fault scenarios** — every terminal transition releases its lock, every lock birth is fsync'd, every created worktree is rediscoverable by canonical path.

But v0.3.0 leaves **five crash-recovery gaps** that survive R1 integration. Three are structural (need a new requirement), two are cross-spec coordination that R2 must surface before `reviewed`. In priority order:

1. **The §7.2 worktree-create-and-stamp sequence has no unwind rule for crashes *between* its steps.** WM-003 creates the worktree; WM-026 stamps the sidecar; WM-013a writes the lock; WM-016 gates the `workspace_leased` emission on all three. A crash after `git worktree add` but before any of the stamp/lock writes produces a worktree on disk + a branch in git (both durable, fsync'd by git itself in modern versions) + **no lease lock, no sidecar, no `workspace_leased` event**. WM-013c's discovery mechanism classifies this as "directory passes `git worktree list`, no lease-lock file" — a state WM-013c's decision table (a)/(b)/(c)/(d) does not assign a category to. It is neither orphan-stale (lock would be required) nor live-leased (lock is absent) nor ready-for-re-lease (WM-013d forbids path reuse, and WM-034 mints fresh `run_id`s only on `reopen-bead`). **The spec has no rule for "bare worktree, no lock, owning run not yet observed."** Findings 1 & 2 below.

2. **Sidecar-write atomicity is asserted (WM-026) but never mechanized.** WM-013a names write-to-temp + rename + fsync discipline for the lock file; WM-026 says only "MUST write … before the handler launches" with no temp-file discipline declared. The §7.2 pseudocode at line 731 calls `write_json_fsync(...)` without naming the rename pattern. A crash mid-write produces a **partially-written `harmonik.meta.json` that CASS (S08) will ingest as truncated JSON**, silently corrupting the CASS index. Finding 3.

3. **Lock-release ordering depends on JSONL durability class that is weaker than the spec assumes.** WM-013b requires lock release "AFTER the terminal event … has been emitted and flushed to JSONL per EV-015." But event-model §8.5.1 and §8.5.2 classify `workspace_created` and `workspace_leased` as `O` (ordinary) — **not fsync-boundary**. Only `workspace_merge_status` is `F`. So the "flushed to JSONL" ordering holds for merge terminals but is **a lie for the `workspace_discarded` terminal** (also `O`). A crash between the `workspace_discarded` emit and the lock-release `unlink(2)` can leave a stale lock on a terminally-discarded workspace whose discarded event never made it to disk. On restart the orphan sweep sees: worktree registered, lock present whose `pid` is dead, no terminal event. WM-033 classifies this as stale-lock and sweeps — correct outcome — but the **reconciliation category is mis-assigned** (appears as Cat 6 "lost lease" when it should be Cat 5 "nothing in-flight"). Finding 4.

4. **WM-033 staleness rule is clock-dependent and silent on clock regression.** "Lock mtime predates the current daemon's start time" (PL-006 rule imported by WM-033) assumes monotonicity of filesystem mtime vs. daemon wall clock. WSL2, NFS, APFS-over-SMB, and container-mount scenarios all produce mtime values that can be **behind** or **ahead** of the observer's clock by multiple seconds. WM-013a carries a wall-clock `created_at` in the lock's JSON body — available for a clock-independent check — but WM-033 does not cite it as the authoritative staleness signal, deferring entirely to mtime. Finding 5.

5. **UUID reuse and collision are asserted forbidden (WM-013d, WM-034) but never detected.** WM-013d says "MUST reject any attempt to reuse a prior `run_id` at workspace-create time" but the §7.2 `reuse_of_prior_run_id` predicate has no implementation — no registry of prior `run_id`s is declared, and WM-INV-005 prohibits a registry. The only detection path is "canonical path already exists," which depends on the prior run's worktree persisting. **A `reopen-bead` after the prior failed run's worktree has been manually cleaned up by an operator** leaves no filesystem evidence, and a UUIDv7 collision (astronomically rare but not cryptographic) is undetectable. Finding 6.

No R1 finding has been reopened incorrectly; no locked-in decision is pressured. All five fixes are additive spec text plus two cross-spec notes. None requires reopening [architecture.md §4.9] or the decisions-locked-in table.

---

## 2. Scenarios probed

Ten scenarios, selected from the prompt list plus two additions surfaced while tracing the spec.

### Scenario 1 — Crash between `git worktree add` and lock creation

**Affected requirements.** WM-003 (line 152–157), WM-013a (line 235–249), WM-016 (line 298–306), WM-013c (line 258–263), §7.2 pseudocode (line 689–764), WM-033 (line 446–453).

**What the spec says happens.** §7.2's `create_workspace` (line 692–716) executes `git.worktree_add(path, branch, start_point=parent_commit)`, then `mkdir(sessions_dir)`, then builds the in-memory Workspace record, then emits `workspace_created`, then sets `state = "ready"`. The lease-lock is NOT written in `create_workspace` — it is written in `launch_session` (line 718, only for the first session, line 733–739). WM-013a explicitly carves this out: "On every `workspace_created` emission, the workspace manager MUST NOT yet have written a lease-lock file" (line 244).

**Trace through the crash.** A SIGKILL or power loss after `git worktree add` returns but before `launch_session` is called produces:
- A registered worktree (`.git/worktrees/<run_id>/` exists; `.git` file in the workspace; git's worktree registration is fsync'd by modern git).
- A `run/<run_id>` branch visible in `git branch`.
- An empty `${workspace_path}/.harmonik/sessions/` directory (the mkdir is durable on ext4/APFS).
- **NO lease-lock file.** WM-013a's birth is tied to lease acquisition, not worktree existence.
- **NO `workspace_leased` event** in JSONL (it's gated on WM-016 steps a–d, and d did not happen).
- `workspace_created` event MAY or MAY NOT be in JSONL: class `O` means it is written but not fsync'd (EV §8.5.1 line 134). On power loss the emit is likely lost; on SIGKILL the OS flushes the page cache on normal shutdown and the event survives.

On restart, WM-013c's four-step discovery runs (line 258):
- (a) enumerate subdirectories matching `run_id` regex — finds it.
- (b) `git worktree list --porcelain` — registered, passes.
- (c) read lease-lock file "if present" — absent.
- (d) stat `.harmonik/sessions/` — empty directory exists but no session subdirectory.

WM-013c does not name a category for this state. Step (c) says "if present" and moves on silently. The workspace record's `state` field at restart is unknown — the in-memory record died with the daemon, and §6.1 has no on-disk persistence for the `state` enum (§7.2 sets `state` only in memory). WM-INV-005's "canonical-path discovery without a registry" forbids an on-disk state file.

**Is this safe?** No. The system reaches a **silent unreachable state**: a worktree that
- is NOT leased (no lock),
- is NOT orphan in WM-033's sense (no stale lock to sweep),
- is NOT orphan in reconciliation's sense (a `run_id` with no terminal commit and no `workspace_leased` event — but the run_id may also never appear in Beads as `in_progress` if the bead-claim step itself crashed).

Reconciliation [spec.md §8.11 Cat 6a (line 193)] mentions "bead `in_progress` with workspace missing" but not "workspace present with no lease and no in-progress run." The closest fit is Cat 3 generic ("git and Beads tell inconsistent stories"), but the detector rule for Cat 3 ([spec.md line 121]) never enumerates this combination. An investigator dispatched against this would face an ambiguous WIP-capture target (nothing to capture; the worktree is empty).

The path `<repo>/.harmonik/worktrees/<run_id>/` is now permanently reserved: WM-013d forbids path re-use, and WM-034 mints fresh `run_id`s only on `reopen-bead`. If the run is re-dispatched via an ingestion path that re-uses the `run_id` (contrary to WM-013d), the create fails with `WorkspaceAlreadyExists`. The bead stays `open` (or whatever state it was in before claim) and the operator sees **a worktree directory on disk with no corresponding in-flight run** and no obvious cleanup path.

**Spec coverage.** Silent. WM-013c enumerates discovery mechanics but does not map the `{registered, no-lock, empty-sessions}` tuple to any reconciliation category. WM-033 sweeps lock files, not bare worktrees — its last sentence explicitly says "The sweep MUST NOT delete worktree directories or branches" (line 448).

**Recommendation.** Add WM-013e or extend WM-013c:

> **WM-013c(e) — Discovery outcome for bare worktree (registered, no lock).** A worktree whose git-registration (§4.3.WM-013c step b) passes AND whose lease-lock file is absent (§4.3.WM-013c step c) AND whose `.harmonik/sessions/` directory is empty (§4.3.WM-013c step d) is classified as **unleased-bare** and routed to reconciliation Cat 3 (store disagreement) per [reconciliation.md §8.4] with evidence-type `bare-worktree-no-lease`. The investigator's default verdict for this evidence type is `escalate-to-human` with an auto-downgrade path to a daemon-side cleanup workflow (post-MVH) when the bead correlation confirms the run was never claimed. Reconciliation owns the category; WM only produces the evidence.

Cross-spec: reconciliation [spec.md §8.4 Cat 3 (line 119)] and [schemas.md §6.3 detector rules] SHOULD name the `bare-worktree-no-lease` detector explicitly. Today it would fall into Cat 3 generic by default, but the detector rule for Cat 3 doesn't enumerate this case.

---

### Scenario 2 — Crash between sidecar write and lock write

**Affected requirements.** WM-016 (line 298), WM-026 (line 397), WM-013a (line 235), §7.2 `launch_session` (line 718–742).

**What the spec says happens.** §7.2's `launch_session` writes the sidecar (line 731) **before** the lease-lock (line 735), per WM-016's ordering: "(c) sessions-dir and sidecar fsynced; (d) lease-lock fsynced." So the on-disk sequence is: sidecar-durable → lock-durable → state transitions to `leased` → `workspace_leased` emitted.

**Trace through the crash.** A crash after the sidecar fsync (line 731) but before the lock fsync (line 735) yields:
- Registered worktree (from `create_workspace` earlier).
- `${workspace_path}/.harmonik/sessions/<session_id>/harmonik.meta.json` present, fsync'd, valid JSON.
- **NO lease-lock file.**
- `workspace_created` possibly in JSONL (class O); `workspace_leased` definitely not.

On restart, WM-013c's four-step scan: (a) enumerates, (b) registered, (c) lock absent, (d) `${path}/.harmonik/sessions/` non-empty (has one `<session_id>` subdirectory with a valid sidecar).

**Is this safe?** No — it's a second silent unreachable state, distinct from Scenario 1 by the `sessions/` directory non-empty. The spec has no rule for "sidecar present, lock absent."

The sidecar contains a valid `run_id`, `node_id`, `agent_type`, `workflow_id`, `launched_at`. An ingestion agent or reconciliation detector reading the sidecar would conclude that a session was launched — but no `workspace_leased` event says so, no agent subprocess is alive, no checkpoint commits exist on the task branch (no handler ever ran). The bead might be `in_progress` (claim happened before worktree creation) or `open` (claim sequencing owned by EM, TBD).

Reconciliation [§8.11 Cat 6a (line 193)]'s detector checks "live lease-lock whose run_id doesn't appear as in-flight" — this misses because there IS no live lease-lock. Cat 3 generic could absorb it, again without an explicit detector rule.

**Spec coverage.** Silent. Same shape as Scenario 1; the §7.2 pseudocode commits to the ordering but the spec never names the recovery for a crash between steps.

**Recommendation.** Same WM-013c(e) extension as Scenario 1 handles both scenarios if it routes on `(registered AND lock-absent)` regardless of `sessions/` state. Or split into two evidence types:

| Worktree state | Lock | Sessions dir | Evidence type |
|---|---|---|---|
| registered | absent | empty | `bare-worktree-no-lease` |
| registered | absent | non-empty | `sidecar-without-lease` |

The `sidecar-without-lease` case is strictly more informative: the sidecar contains `run_id` and `node_id`, supplying a natural reconciliation correlation key that Scenario 1 lacks. Worth distinguishing.

---

### Scenario 3 — Crash during sidecar write itself

**Affected requirements.** WM-026 (line 397–402), §7.2 line 731 (`write_json_fsync(...)`), WM-013a's atomicity language (line 244: "atomically (write-to-temp + rename)").

**What the spec says happens.** WM-026 says the sidecar "MUST be written before the handler launches" and names the required fields. No atomicity mechanism is declared. §7.2 line 731 calls `write_json_fsync(...)` as a single pseudo-code step, which implies atomicity at the protocol level but doesn't constrain implementation.

**Trace through the crash.** A crash partway through `write(2)` of the sidecar JSON produces a half-written file: `{"run_id":"abcd-...` with no closing brace. If the writer used the pattern `open → write → fsync → close` without a temp+rename, fsync flushed whatever was written — **the half-JSON is durable**.

**Is this safe?** No. On restart:
- WM-013c reads the sidecar to learn session metadata. The current spec doesn't explicitly name this, but WM-022's implementer-identification rule (line 351) says "the sidecar at `.harmonik/sessions/<session_id>/harmonik.meta.json` keyed by that commit's session supplies the `agent_type` and LaunchSpec template." A torn sidecar breaks that path.
- CASS (S08; deferred spec) indexes the sidecar. The read-only guarantee of WM-029 (line 416) does not protect against ingesting corrupt data.
- Any reconciliation investigator reading the sidecar sees invalid JSON and must decide: drop? retry? reclassify? No rule names this.

**Contrast with lock file.** WM-013a explicitly mandates write-to-temp + rename + fsync (line 243–244). WM-026 has no equivalent. The asymmetry is the smoking gun: the spec authors were aware of torn-write risk and named the mitigation for one artifact, silently omitting it for the other.

**Spec coverage.** Missing. WM-026 is silent on atomicity. The §7.2 pseudocode's `write_json_fsync` is a single-step abbreviation that elides the temp+rename.

**Recommendation.** Amend WM-026:

> **WM-026 [amendment].** The workspace manager MUST write the sidecar atomically via write-to-temp-plus-rename-plus-fsync, using the same discipline as WM-013a: write to `${workspace_path}/.harmonik/sessions/${session_id}/.harmonik.meta.json.tmp`, `fsync(2)` the tempfile, `rename(2)` to `harmonik.meta.json`, `fsync(2)` the parent directory `${workspace_path}/.harmonik/sessions/${session_id}/`. A reader observing `harmonik.meta.json` whose contents fail JSON-parse MUST treat this as [reconciliation.md §8.11a Cat 6b] integrity violation (mechanically unrecoverable; the sidecar is immutable after write per WM-026's precedence ordering with WM-016).

Also update §7.2 line 731 pseudocode to name the discipline explicitly, so implementers don't read `write_json_fsync` as a shortcut.

---

### Scenario 4 — Crash between terminal-state transition and lock release

**Affected requirements.** WM-013b (line 251–256), WM-032 (line 440), §7.2 `complete_merge` (line 750–757) and `discard_workspace` (line 759–764), EV §8.5.1/2/4 durability class `O`.

**What the spec says happens.** WM-013b: "Release MUST occur AFTER the terminal event (`workspace_merge_status` with `status=merged`, or `workspace_discarded`) has been emitted and flushed to JSONL per [event-model.md §4.4 EV-015]."

**Trace through the crash.** `complete_merge` (line 750):
```
workspace.state = "merged"                  # in-memory only
emit_event(workspace_merge_status, merged)  # class F: fsync'd by EV-016
release_lease_lock(...)                     # unlink(2); not fsync'd
```

For `merged`: `workspace_merge_status` is class `F` (EV §8.5.3 line 136); the emit blocks until fsync. Then `unlink(2)` runs. A crash between the emit return and the unlink call leaves the lock file on disk; on restart WM-013c sees lock present, owning PID dead (not current daemon), and WM-033 classifies as stale → sweep → safe. **This path is correct.**

For `discarded` (`discard_workspace`, line 759):
```
workspace.state = "discarded"
workspace.interrupt_state = "none"          # per WM-037a
emit_event(workspace_discarded, reason)     # class O: NOT fsync'd by default
release_lease_lock(...)                     # unlink(2)
```

`workspace_discarded` is class `O` (EV §8.5.4 line 137). EV-016 says `O`-class events return from `Append` without fsync. A crash after the emit but before the unlink leaves the lock on disk AND the `workspace_discarded` line in the kernel page cache but not on stable storage. On power loss, both the event and the lock's persistence are disputed:

- **Power loss** (page cache not flushed): event lost; lock file durable (if lock's original write was fsync'd, which WM-013a mandates, the file is durable; the unlink didn't happen, so it stays). On restart: lock present, no terminal event, owning PID dead. WM-033 sweeps the stale lock. Bead state is whatever Beads recorded before crash (TBD in beads-integration). Reconciliation sees no in-flight run (no `workspace_leased` indicator, run has reached some prior terminal state in Beads) → Cat 5 "nothing in-flight" per [spec.md §8.10] — safe, but the `workspace_discarded` event is permanently lost, so downstream observers (audit, improvement-loop) never see the discard.

- **SIGKILL** (page cache eventually flushes): event durable; lock durable; no unlink. On restart: same as power-loss case. Stale lock swept, event in JSONL observable.

The real issue is not crash-safety of the lock sweep — it's **observability loss of the `workspace_discarded` event under power loss**. EV-025 cedes payload ownership, but WM cedes durability classification too — and `O` may be wrong for terminal workspace events.

**Is this safe?** State-reconstruction-safe: git + Beads + lock-sweep all converge on the right answer. Observability-correct: **no.** A power loss during `discard` loses the only event recording which reason drove the discard. The audit trail for failed-run post-mortems depends on `workspace_discarded` carrying the `reason` field — if that event is lossy, post-mortem is degraded.

More serious: WM-013b's "flushed to JSONL per EV-015" (line 253) **reads like a contract** that the release happens only after the event is durable. But for `O`-class events there is no "flushed" moment except opportunistically (EV-016 line 356 allows 1-sec flush timer). The contract is vacuous for `discarded` under the current EV classification.

**Spec coverage.** Mis-specified across the WM/EV boundary. WM-013b assumes strong durability at release; EV provides it only for `F`-class events.

**Recommendation.** Two-part fix:

(a) **WM requests re-classification of `workspace_discarded` to `F`.** Cross-spec OQ. The event marks a terminal state and its loss impairs audit + improvement-loop; the "greater than cost of a disk sync" criterion of EV-016 (line 356) clearly applies.

(b) **WM-013b amendment to remove the false contract.** Until EV lifts the class:

> **WM-013b [amendment].** Lease release by unlink MUST occur only after the terminal event has been emitted via `Emit`; for `F`-class terminal events (`workspace_merge_status` with `status=merged`) this implies durability-on-disk per [event-model.md §4.4 EV-016]. For `O`-class terminal events (`workspace_discarded`), `Emit` return does NOT guarantee fsync; a crash window exists between emit and release during which the event MAY be lost while the lock survives. On restart the stale lock is swept per WM-033; the loss of the `workspace_discarded` event is accepted per [event-model.md §4.4 EV-017] and reconstructed from git (run terminal) + Beads (run-terminal bead status).

This flags the R2 cross-spec coordination with EV that OQ-WM-006 already opens but does not cover this specific case.

---

### Scenario 5 — Daemon restart with stale lock held by dead PID; PID may have been recycled

**Affected requirements.** WM-013a (line 235, the `pid` field in the lock), WM-013c (line 260, "recorded `pid` is NOT the current daemon"), WM-033 (line 446, staleness rule defers to PL-006), [process-lifecycle.md PL-006 line 125] (mtime-based staleness), [handler-contract.md HC-044a] (PID liveness + recycled-to-non-handler-argv check).

**What the spec says happens.** WM-013c step (c) reads the lock's `pid`. WM-033 says "Staleness is determined by the rule in PL-006 (lock mtime predates the current daemon's start time) combined with the liveness-probe discipline of [handler-contract.md §4.10 HC-044a] (recorded PID not live OR recorded PID recycled to a non-handler argv)."

**Trace through the crash.** Scenario A: daemon crash, restart, recorded PID recycled by an unrelated process (e.g., OS PID wraparound; happens on long-running Linux hosts where PIDs wrap at 32K or `/proc/sys/kernel/pid_max`). On restart:
- Lock's `pid` field = 5432.
- Current process table: PID 5432 exists, is `nginx`.
- HC-044a's argv check says "not a harmonik handler argv" → stale.
- WM-033 sweeps the lock.

Scenario B: daemon crash, restart, PID recycled to **another harmonik daemon that was legitimately started by an operator against a different project**. PID 5432 exists, argv is `/path/to/harmonik daemon --project /another/project`. HC-044a's check is "recorded PID not live OR recorded PID recycled to a non-handler argv." The PID IS live and IS a harmonik-shaped argv. **HC-044a's check says "live."** WM-033 does NOT sweep. The lock is preserved — but it's held by a process that has no knowledge of this worktree.

**Is this safe?** No. The lock file becomes permanently stuck until an operator manually removes it or the unrelated daemon dies. The in-flight run whose lock this was cannot be re-classified because WM-013c + WM-033 conclude "live, not stale," routing to reconciliation's Cat 6 "integrity violation" path per [reconciliation.md §8.11 Cat 6a].

The failure mode is real but the scope is narrow (another harmonik daemon on the same host with a recycled PID). Probability is low; blast radius (one operator-facing escalation) is bounded. Not a showstopper, but the spec should name the detection rule.

**Spec coverage.** Partial. WM-013a stores `pid` and `created_at` in the lock JSON body — the latter is clock-independent evidence that could disambiguate. But WM-033 never cites `created_at` as an input.

**Recommendation.** Amend WM-033 to prefer the lock's JSON content over mtime for staleness:

> **WM-033 [amendment].** Staleness detection MUST prioritize the lock file's JSON content (WM-013a) over filesystem mtime. Concretely: (a) if the lock's `pid` is the current daemon's PID, the lock is live-ours (adopted; no sweep); (b) if the lock's `pid` is not live per HC-044a's liveness probe, the lock is stale (sweep); (c) if the lock's `pid` is live but its argv does not match a harmonik handler/daemon for THIS project (the project-root argv test), the lock is stale (sweep) — a recycled-to-unrelated-harmonik-daemon PID from a crashed prior instance of THIS project is NOT live; (d) mtime comparison is a fallback only when (a)–(c) are inconclusive. The `created_at` field in the lock's JSON supplies clock-independent freshness evidence when mtime is unreliable (NFS, container mounts, WSL2).

This also addresses OQ-WM-005 coordination (the lock path and content must align across WM/HC/PL — if WM owns the content schema, WM should own the staleness rule too).

---

### Scenario 6 — Two daemons race on the same repo

**Affected requirements.** WM-013a (line 235, lock write discipline), [process-lifecycle.md PL-INV-001 line 322] ("at most one daemon process MUST hold the pidfile lock"), WM-INV-001 (line 529 lease-by-run sensor).

**What the spec says happens.** PL-INV-001 forbids two daemons per project via a pidfile lock at `.harmonik/daemon.pid`. But WM assumes the pidfile lock is effective; if it's not (e.g., pidfile lock held via `flock(2)` on NFS, which is notoriously unreliable), two daemons can both be running.

**Trace through the crash.** Two daemons, D1 and D2, both start against the same project on an NFS mount where `flock` returns success to both. D1 creates a worktree for run R1, writes the lock file at `${workspace_path}/.harmonik/lease.lock` with `pid = D1`. D2, running the same claim path for its own R2 against the same bead, WM-034 mints a fresh `run_id` = R2, different canonical path → no direct collision at the worktree level.

But at the BEAD level: WM-012 (line 224) says "AT MOST ONE run MAY be in flight for a given bead." If both daemons claim the same bead, both create runs. Beads-integration owns the claim atomicity — if the `br claim` CLI is atomic (SQLite-backed), one daemon's claim wins. But if Beads itself has weak guarantees (OQ in beads-integration), both claims can succeed, and both daemons materialize distinct worktrees at distinct paths.

On restart of D1 (say D2 is still running, D1 crashed and restarts):
- WM-013c enumerates `.harmonik/worktrees/`, finds both R1 and R2 subdirectories.
- For R1: lock's `pid = D1` (the old crashed daemon, not the restarted D1), and R1 is not in D1's new in-memory run registry — sweep.
- For R2: lock's `pid = D2` (still live). HC-044a check: argv is a harmonik daemon for THIS project. With the Scenario-5 amendment, the rule says "live, is THIS project's daemon." Under the unamended rule: "live, harmonik-shaped argv → don't sweep." Correct answer for both: don't sweep R2; it's D2's worktree.

**Is this safe?** Conditional. WM alone can't arbitrate two-daemon races because WM is per-workspace; cross-project arbitration is PL-INV-001's job. WM's defensive posture is correct: don't sweep other daemons' locks. The gap is in **two-daemon-same-project** races that PL-INV-001's flock does not reliably prevent on NFS/SMB.

**Spec coverage.** WM defers to PL-INV-001, which is adequate for local filesystems but has the NFS caveat. Not a WM fix.

**Recommendation.** No WM change needed. Flag for PL's OQ registry: **if the daemon is permitted to run against NFS-mounted project directories, PL-INV-001's pidfile-lock mechanism needs a stronger arbitrator (lease server, advisory lock with TTL, or explicit documentation that NFS mounts are unsupported).** Track under [process-lifecycle.md §OQ-PL-002 or an adjacent OQ].

---

### Scenario 7 — `git worktree add` partial failure

**Affected requirements.** WM-003 (line 152), §8 error taxonomy `WorktreeCreationFailed` (line 777).

**What the spec says happens.** §8: "`WorktreeCreationFailed` | `git worktree add` returns non-zero | (remains in initial) | orchestrator routes to run-create failure."

**Trace through the crash.** `git worktree add` can partially fail in several ways:
- Writes `.git/worktrees/<run_id>/HEAD` but crashes before writing the worktree's `.git` file → the worktree is half-registered.
- Writes the worktree's `.git` file but crashes before `.git/worktrees/<run_id>/` registration is complete.
- Returns non-zero after partial progress (disk full during checkout).

Git's modern (≥2.20) implementation of `worktree add` is atomic at the registration level — it writes the worktree's admin dir + the worktree's `.git` pointer in an order that is recoverable by `git worktree repair` or `git worktree prune`. But "atomic" for git here is best-effort and doesn't cover SIGKILL during `git-checkout` mid-way through materializing the tree.

**Is this safe?** Mostly, but the spec doesn't say. The outcome depends on git's internal atomicity, which WM defers to without checking. A half-materialized worktree (some files present, others absent, `.git` pointer valid, registration valid) passes WM-013c step (b) but contains inconsistent file state — an agent launched against it will see phantom missing files.

**Spec coverage.** Silent. WM-003 says "MUST create a worktree via `git worktree add`" but does not name the recovery rule for a partial creation. §8's `WorktreeCreationFailed` assumes the non-zero return propagates cleanly, but SIGKILL mid-operation leaves no return to observe.

**Recommendation.** Add to WM-003 or as a new WM-003a:

> **WM-003a — Worktree-creation atomicity check.** On daemon startup, for every worktree discovered per WM-013c step (a)–(b), the workspace manager MUST verify the worktree's admin state via `git worktree repair --dry-run` (or equivalent integrity check). A worktree whose admin state is inconsistent MUST be classified as integrity-violation evidence per [reconciliation.md §8.11 Cat 6a] with evidence-type `worktree-admin-corrupt`. The sweep rule of WM-033 MUST NOT auto-delete such worktrees; operator intervention (via `git worktree remove --force` + re-claim) is required.

Cross-spec: reconciliation detector schemas should recognize `worktree-admin-corrupt` as a Cat 6a evidence type.

---

### Scenario 8 — Disk full during merge-back commit

**Affected requirements.** WM-019 (line 327, squash + commit), WM-020 (line 334, non-ff merges + conflict resolution), [execution-model.md EM-016 checkpoint atomic-sequence].

**What the spec says happens.** WM-019: "The merge-back operation MUST produce exactly one commit on the integration branch per completed task." No requirement names what happens if the commit itself fails from ENOSPC.

**Trace through the crash.** `git merge --squash` succeeds (index is staged); the subsequent `git commit` fails ENOSPC during tree-write or object-write. The task-branch tip is unchanged; the integration-branch tip is unchanged; the index on the merge worktree is staged but not committed. No `workspace_merge_status` with `status=merged` has been emitted.

On restart (daemon crashed simultaneously):
- WM-013c finds the worktree, lock present (assuming the merge node is running inside the existing lease per WM-018), `state` was `merge-pending` in memory but not persisted.
- Reconciliation detects: task-branch tip advertises `Harmonik-Run-ID` without `Harmonik-Verdict-Executed`; integration-branch tip has no corresponding merge commit. Classifies as Cat 2 (non-idempotent in-flight) per [spec.md §8.3].
- Investigator verdict: `resume-with-context` or `reset-to-checkpoint` — but the worktree's index is in a staged-merge state from the previous attempt. The investigator's `git status --porcelain` will see a merge in progress.

[reconciliation.md §8.11 Cat 6a (line 199)] detector explicitly names "Worktree has in-progress git operation (rebase, merge, cherry-pick, bisect) … detected by checking for `.git/rebase-merge`, `.git/rebase-apply`, `.git/MERGE_HEAD` …" — this ALREADY covers the squash-merge index state via `MERGE_HEAD`. So a crashed squash-merge routes to Cat 6a, not Cat 2. Good.

**Is this safe?** Conditionally. Cat 6a investigator must know how to recover: `git merge --abort`, then re-dispatch the merge node. Reconciliation RC-019 (line 421) requires WIP capture before `reopen-bead` but merge-recovery is `resume-here` or `reset-to-checkpoint`, which don't require WIP capture. The investigator playbook for Cat 6a with `MERGE_HEAD` evidence MUST call `git merge --abort` before allowing re-dispatch — this is NOT named in reconciliation or WM.

**Spec coverage.** Partial. Cat 6a detector recognizes the state but no playbook obligates merge-abort before re-dispatch.

**Recommendation.** Cross-spec note for reconciliation's Cat 6a playbook:

> The Cat 6a investigator for `MERGE_HEAD`-evidence workspaces MUST execute `git merge --abort` (or equivalent) before returning any `resume-here` / `resume-with-context` / `reset-to-checkpoint` verdict. Failing to abort leaves the worktree in a staged-merge state that blocks subsequent node dispatch.

Not a WM-local fix; belongs in reconciliation's RC-015/RC-016 playbook obligation. Flag for R3 reconciliation integration.

---

### Scenario 9 — Interrupt storm

**Affected requirements.** WM-037 (line 491), WM-037a (line 499), WM-038 (line 505), WM-039 (line 511), WM-040 (line 519), §4.10.

**What the spec says happens.** WM-038: "The workspace manager MUST be the SOLE writer of the `interrupt_state` field." Transitions are driven by (a) operator-control events, (b) reconciliation verdicts. "No other subsystem may mutate the field; cross-subsystem requests to change interrupt_state MUST route through the event bus."

**Trace through the crash.** Operator issues `harmonik stop --immediate` rapidly twice, then a daemon-crash-suspected signal arrives from reconciliation all within ~100ms. The bus serializes events; WM consumes them in order. But WM is a single-threaded state-machine in the daemon's Go core (assumed), so concurrent mutations don't happen within-process.

Across a crash boundary: event 1 (`operator_stopping`) lands in JSONL (class O, not fsync'd per EV §8.7 — need to check EV classes for operator events).

<quick check of EV §8.7 operator events>

Event 2 and 3 might not have landed. On restart, WM's in-memory state is gone; `interrupt_state` must be reconstructed from JSONL. But EV-017 (line 367) says ordinary events between fsync boundaries MAY be lost — so WM's `interrupt_state` reconstruction from JSONL alone is not reliable. The spec has no on-disk persistence of `interrupt_state`; WM-INV-005 forbids a registry.

**Is this safe?** Uncertain. The `interrupt_state` field's durability is UNSPECIFIED. If reconstruction is from JSONL (observational), it's lossy. If it's from Beads (not currently named), it'd be durable but requires a new Beads column. If it's not reconstructed at all and WM defaults to `interrupt_state = none` on restart, then **crash during an operator-pause transparently resets the field to `none`** — the operator's pause is silently lost.

WM-040 (line 519): "Clearing `interrupt_state` back to `none` MUST be driven by either (a) an `operator_resuming` event … or (b) a reconciliation verdict." If a crash loses the prior pause event, the restart's default-`none` IS a silent clear — violating WM-040. But WM-040's sensor is "reconciliation's transition-audit pass per RC-010" which only fires if the mutation is observable. An unobserved reset (default on restart) has no audit trail.

**Spec coverage.** Silent on `interrupt_state` durability across daemon restart. WM-039 says "update the record atomically with the causing input … such that the new state is durable and observable by the reconciliation detector" — but "durable" is not mechanized. In-memory only? In Beads? In a WM-owned sidecar?

**Recommendation.** Two resolutions possible:

(a) **Durability is in JSONL, lossy-acceptable.** `interrupt_state` is derivable from the reconciliation detector re-running on restart (the Cat 6 detector per [spec.md §8.11] observes lost-lease and re-asserts `daemon-crash-suspected`). Operator-driven interrupt loss is accepted as a secondary observability gap. WM-039 is amended to say "observable" is JSONL-best-effort; the authoritative recovery is reconciliation's re-detection.

(b) **Durability is explicit.** Add a WM-013a-style small file at `${workspace_path}/.harmonik/interrupt.state` (or carry in the lock file's JSON as an optional field) that is fsync-rewritten on every transition. Cost: one extra fsync per interrupt transition; benefit: `interrupt_state` is authoritative at restart.

Route (a) is more aligned with [execution-model.md §4.7] (state reconstruction uses git + Beads, JSONL is observational). But §4.10 doesn't have a clean git or Beads reconstruction path for `interrupt_state` — it's a record-local flag. Route (b) adds a new fsync artifact.

Favor route (a) with explicit amendment:

> **WM-039 [amendment].** `interrupt_state` is NOT durable across daemon restart. On restart, WM MUST initialize `interrupt_state = none` for every discovered workspace; reconciliation's Cat 6 detector ([reconciliation.md §8.11]) is the authoritative recovery path that will re-assert `daemon-crash-suspected` for any workspace whose lease was not cleanly released. Operator-driven interrupts (`operator-paused`, `operator-stopped-*`) lost across crash are reasserted by the operator's next control action per [operator-nfr.md §4.3]; there is no silent inheritance of pre-crash interrupt state.

Cross-link to OQ-WM-004, which asks about mixed operator + crash paths — this is the same boundary.

---

### Scenario 10 — Git worktree prune race (external tool intervention)

**Affected requirements.** WM-013c (line 258), WM-031 (line 432, failed-run persistence), WM-INV-003 (line 545, append-only task branch).

**What the spec says happens.** WM-031: "A worktree whose run reached a terminal failure state MUST persist on disk with its branch intact." WM-013c discovers workspaces by enumerating `.harmonik/worktrees/` + `git worktree list --porcelain`. No requirement forbids external `git worktree prune` or manual deletion.

**Trace through the crash.** Operator runs `git worktree prune` from their shell (a routine git maintenance command). Every worktree whose admin dir `.git/worktrees/<name>/` has a `locked` file is preserved; every other admin dir is pruned if the worktree's filesystem path is missing or if certain criteria are met. **`git worktree add` does NOT by default create a `.git/worktrees/<name>/locked` file.**

An operator deletes `.harmonik/worktrees/<run_id>/` via `rm -rf` (on a failed run they thought they didn't need). Next `git worktree prune` sees the admin dir pointing to a missing path and cleans up the admin registration. The run's branch (`run/<run_id>`) still exists in the repo's refs.

On daemon restart: WM-013c step (a) doesn't find the run_id subdirectory (deleted); step (b) wouldn't fire because there's nothing to enumerate. The workspace is silently gone from WM's view. But the run's task branch still exists in git, the run's bead may still have records, and the reconciliation detector will find the branch without a matching worktree.

[reconciliation.md §8.11 Cat 6a (line 199)] detector includes "bead `in_progress` with workspace missing without a run-terminal marker" — maps to Cat 6a. This is the right category.

**Is this safe?** Yes, with caveats:
- If the operator deleted a **terminally-failed** run's worktree (per WM-031, persistence is an audit convenience, not a safety requirement), no harm — the bead is terminal, no in-flight run, Cat 5 "nothing in-flight."
- If the operator deleted a **live** worktree mid-run, Cat 6a fires → investigator → `reopen-bead` likely → fresh worktree.

**Spec coverage.** WM relies on operators NOT deleting worktrees mid-run. No mechanical enforcement. WM-031 asserts persistence but provides no lock or filesystem permission to prevent external deletion.

The realistic threat isn't malice; it's **harmonik-adjacent tooling** (IDE plugins, backup scripts, CI cleanups) reading `.harmonik/` and doing the wrong thing.

**Recommendation.** Add a note to WM-031:

> **WM-031 [amendment / note].** Protection against external deletion of worktrees is NOT declared by this spec; the `.harmonik/worktrees/` directory is a conventional location visible to any filesystem operator. Operators accidentally deleting an in-flight run's worktree cause the run to route to [reconciliation.md §8.11 Cat 6a] with evidence-type `workspace-missing-live-run`; recovery is a `reopen-bead` verdict. Operators MAY mark critical worktrees with git's own `git worktree lock <path> --reason "harmonik run <run_id>"` for defense-in-depth; WM does NOT currently issue `git worktree lock` calls, and adoption is tracked as a post-MVH enhancement.

Optional amendment: have WM-003's `git worktree add` call follow up with `git worktree lock --reason "harmonik run ..."` to make `prune` skip in-flight worktrees. Cost: a lock file per worktree, managed via git's own mechanism. Benefit: external `git worktree prune` no longer silently destroys harmonik state.

---

## 3. Atomicity-claims audit

Every "MUST" in v0.3.0 that implies atomicity, checked against real filesystem semantics:

| Claim | Location | Mechanism declared | Atomicity holds? |
|---|---|---|---|
| "MUST write the lease-lock file atomically (write-to-temp + rename)" | WM-013a line 243 | write-tmp → fsync(tmp) → rename → [fsync(parent-dir)?] | **Partial.** Temp+rename gives single-file atomicity. Spec does NOT mandate `fsync(2)` on the parent directory after rename, which is required by POSIX for the rename to survive power loss. `fsync` on the tempfile alone does not propagate the rename durability on ext4 before journal commit. |
| "MUST fsync the file before emitting `workspace_leased`" | WM-013a line 243 | fsync(2) of lock file | File-level fsync holds; parent-dir fsync unspecified (see above). |
| "MUST write the first session's metadata sidecar BEFORE emitting `workspace_leased`" | WM-027 line 404 | ordering only | Ordering claim is correct; atomicity of sidecar write itself is NOT declared. See Scenario 3. |
| "MUST emit exactly one commit on the integration branch per completed task" | WM-019 line 327 | delegates to `git merge --squash + commit` | Git's commit is atomic at the ref-update level (per EM-016). Holds. |
| "MUST update the record atomically with the causing input" (interrupt_state) | WM-039 line 513 | unspecified | **In-memory only.** Not durable; see Scenario 9. |
| "MUST preserve the sessions directory inside the merged branch" | WM-030 line 423 | delegates to git commit | Holds if `.gitignore` excludes are absent (WM-030 flags this). Gap: no mechanical sensor. |
| "MUST NOT auto-delete failed-run worktrees beyond the startup orphan sweep" | WM-031 line 432 | negative constraint | Negative-constraint atomicity is not applicable; the constraint is on absence of behavior. Scenario 10 shows external tools can violate the outcome. |
| `workspace.state = "leased"` then `emit_event(...)` ordering in §7.2 line 740–741 | WM-016, §7.2 | single-threaded in-memory | In-memory ordering is atomic within the daemon's event loop. But `workspace.state` is NOT persisted to disk (see Scenario 9). |
| `release_lease_lock` after terminal emit | WM-013b line 253, §7.2 line 756/764 | unlink(2) | `unlink` is per-directory atomic. Scenario 4 shows the pairing with emit is broken for `O`-class events. |
| "MUST reject any attempt to reuse a prior `run_id`" | WM-013d line 266, WM-034 line 461 | unspecified detection | **Relies on implicit canonical-path collision only.** If operator deleted prior worktree, detection fails. See Finding 6 / Scenario below. |

**Findings.**
- Parent-directory fsync-after-rename is load-bearing for WM-013a's atomicity claim under power loss and is not named. Add to WM-013a.
- Sidecar atomicity is asserted in prose (WM-027 ordering) but the sidecar write itself has no temp+rename discipline. See Scenario 3.
- `workspace.state` and `interrupt_state` have no on-disk persistence; "atomic mutation" language in WM-039 is a category error for a purely in-memory field.

---

## 4. Fsync discipline

Durability-critical writes in WM and their fsync status:

| Write | Fsync'd? | Where declared |
|---|---|---|
| Lease-lock file creation | Yes (file), ambiguous (parent dir) | WM-013a line 243 |
| Lease-lock file removal (unlink) | Not applicable (unlink) but `fsync(parent-dir)` after unlink required for power-loss durability | **Unspecified.** WM-013b line 253 doesn't name parent-dir fsync after unlink. |
| Sidecar JSON write | Yes (file), ambiguous (parent dir), **atomicity undeclared** | WM-016 line 300, WM-026 line 397 |
| Session directory mkdir | Implicit (inherited from git's atomicity) | §7.2 line 722 |
| Workspace.state mutation | N/A (in-memory) | §7.2 |
| interrupt_state mutation | N/A (in-memory) | WM-039 (category error) |
| `workspace_created` event write | Not fsync'd (class O) | EV §8.5.1 |
| `workspace_leased` event write | Not fsync'd (class O) | EV §8.5.2 |
| `workspace_merge_status` event write | Fsync'd (class F) | EV §8.5.3 |
| `workspace_discarded` event write | Not fsync'd (class O) | EV §8.5.4 |
| `merge_conflict_escalation` event write | Not fsync'd (class O — checked EV §8.5.6) | EV §8.5.6 |

**Findings.**
1. **`workspace_discarded` is class `O`.** Scenario 4. WM-013b's "flushed to JSONL" contract is vacuous for this event. Either reclassify to `F` in EV (cross-spec OQ) or amend WM-013b (as proposed in Scenario 4).
2. **`workspace_leased` is class `O`.** The event that WM-016 spends four steps gating emission on is not durably persisted. On power loss right after emit, the event is lost, but the underlying filesystem state (lock, sidecar, worktree) IS durable because each was fsync'd. On restart, reconciliation will see the lease-lock and correctly re-derive that the workspace is leased — the event loss is recoverable. This is probably acceptable (the event is observability, the state is authoritative), but WM does not say so. Worth a one-line note.
3. **`merge_conflict_escalation` is class `O`.** Lost on power loss. The escalation is a cross-boundary operator-facing signal; losing it means the operator is never notified the run went terminal with a conflict. The underlying bead + git state are authoritative (Cat 6a detection will refire on restart), but the operator's immediate signal is lost. Candidate for `F` reclassification (OQ for EV).
4. **Parent-directory fsync after rename / unlink is unspecified.** This is the difference between "file contents durable" and "name durable." POSIX-conformant filesystems require parent-dir fsync for rename/unlink to survive power loss. Named in neither WM nor EV (to my reading of both).

---

## 5. Recovery-rules coverage

Does every failure scenario have a reconciliation category?

| Failure shape | Reconciliation cat | Covered? |
|---|---|---|
| Bare worktree, no lock, no sessions (Scenario 1) | Cat 3 generic (by elimination) | **Not explicitly mapped.** No detector rule in [reconciliation/schemas.md §6.3] names `bare-worktree-no-lease`. |
| Sidecar present, no lock (Scenario 2) | Cat 3 generic (by elimination) | **Not explicitly mapped.** Same gap. |
| Torn sidecar JSON (Scenario 3) | Cat 6b (integrity, unrecoverable) | Covered by detector rule "JSONL unparseable past a byte offset" ... but sidecar is not JSONL; same principle should apply. Not explicitly mapped. |
| Stale lock, dead PID (Scenario 5 simple) | WM-033 sweep → Cat 5 | Covered. |
| Stale lock, PID recycled to unrelated daemon (Scenario 5 complex) | WM-033 does NOT sweep → Cat 6a (integrity) | Covered but sub-optimal. |
| MERGE_HEAD after crashed squash (Scenario 8) | Cat 6a (in-progress git op detector) | Covered by [reconciliation.md §8.11 Cat 6a line 199]. Playbook obligation missing. |
| Worktree deleted by external tool (Scenario 10 failed-run) | Cat 5 (nothing in-flight) | Covered. |
| Worktree deleted by external tool (Scenario 10 live run) | Cat 6a (bead in_progress, workspace missing) | Covered. |
| `interrupt_state` lost across restart (Scenario 9) | Cat 6 (re-detect crash) | Covered by re-detection, not by spec text. WM-039 is silent. |
| `reopen-bead` verdict while canonical path still exists from prior run (edge of Scenario 10) | WM-013d rejects at create → `RunIdReuseForbidden` (wrong; should be `WorkspaceAlreadyExists`) | **See Finding 6.** |

**Findings.** Two gaps: (a) `bare-worktree-no-lease` + `sidecar-without-lease` — Scenarios 1 and 2 — have no named detector. (b) Torn sidecar JSON — Scenario 3 — has no explicit reconciliation category.

---

## 6. Temp-file + rename pattern consistency

Where the pattern is applied:

- **Lease-lock file (WM-013a line 243).** Explicit. Temp + rename + fsync. Parent-dir fsync ambiguous.
- **Sidecar (WM-026).** Not named. §7.2 `write_json_fsync` is an abbreviation, not a discipline. **Asymmetry flagged.**
- **Session directories (mkdir in §7.2 line 722).** `mkdir` is atomic at the directory-entry level; no rename needed.
- **Checkpoint commits (delegated to git / EM).** Git handles its own atomicity (EM-016).
- **Integration branch commits (delegated to git via WM-019).** Same.

**Recommendation.** Close the sidecar asymmetry per Scenario 3's amendment. Add explicit parent-directory-fsync language to WM-013a for cross-platform power-loss safety.

---

## 7. Retry-vs-re-run discipline

Does WM's retry behavior match EM's failure-class routing?

WM's retry surface is narrow — the spec's `idempotency` axis on operations in §4.a (h) is mostly `non-idempotent`:

| Operation | Idempotency | Retry-safe? |
|---|---|---|
| `create_workspace` | non-idempotent | No. Relies on `RunIdReuseForbidden` to reject reuse. |
| `stamp_session_metadata` | non-idempotent | No. Sidecar overwrite would race the handler. |
| `acquire_lease` | non-idempotent | No. Lock exists after first call. |
| `release_lease` | **idempotent** | Yes. Unlink-if-exists is safe to call twice. |
| `merge_back` | non-idempotent | No. Git commits create distinct SHAs on retry. |
| `redispatch_implementer_for_merge_conflict` | non-idempotent (cognition-tagged) | No (LLM). |

EM's failure-class routing (per the EM adversary review §8 classes: `transient | structural | deterministic | canceled | budget_exhausted | compilation_loop`) does not have a clean mapping into WM's filesystem-mutation surface. WM's §8 error taxonomy (line 773) names seven classes:
- `WorkspaceAlreadyExists` — orchestrator caller-error; no retry.
- `RunIdReuseForbidden` — caller-error; no retry.
- `WorktreeCreationFailed` — structural or transient depending on cause; spec doesn't distinguish.
- `LeaseLockHeldByOrphan` — transient (sweep then retry) or structural (foreign daemon).
- `SidecarWriteFailed` — transient (disk full) or structural (permissions); spec doesn't distinguish.
- `MergeConflictUnresolvable` — deterministic; no retry.
- `InterruptOnTerminalWorkspace` — caller-error; no retry (silent reject).

**Findings.**
- `WorktreeCreationFailed` conflates ENOSPC (transient) with "bad parent_commit" (structural/deterministic). Operator-observability gets one class for heterogeneous root causes; retry policy cannot be derived. EM's adversary review flagged the same gap for checkpoint ENOSPC (EM Scenario 2); WM inherits the defect for worktree creation.
- `SidecarWriteFailed` similarly conflates.
- No retry obligations are declared. Every failed operation routes to run-create-failure or reconciliation. This is acceptable for MVH but loses opportunities to recover transient failures without escalation.

**Recommendation.** Align with EM's proposed ENOSPC handling:

> **WM-003 [amendment] / §8 [amendment].** ENOSPC, EIO, and EDQUOT during `git worktree add` or sidecar write MUST be classified as `transient` with a bounded retry cap (default 3 attempts with exponential backoff). Cap exhaustion reclassifies to `structural` and routes to reconciliation. Other non-zero returns from git (bad parent_commit, ref-name violation, concurrent-lock) are `structural` / `deterministic` from the first failure and do not retry.

This mirrors EM's Scenario-2 recommendation (EM adversary review line 42–45). Cross-spec consistency.

---

## 8. Reconciliation coordination audit

Every crash scenario should cite `[reconciliation.md §4/§8]`. Coverage per scenario:

| Scenario | Cites reconciliation? | Correct category? |
|---|---|---|
| 1 — Bare worktree | NO (gap) | Should cite Cat 3 generic; detector rule missing |
| 2 — Sidecar no lock | NO (gap) | Should cite Cat 3 generic; detector rule missing |
| 3 — Torn sidecar | NO (gap) | Should cite Cat 6b; detector rule missing for sidecar-specific torn JSON |
| 4 — Crash between terminal emit and unlink | NO (implicit via WM-033) | Cat 5 is correct but not explicitly cited |
| 5 — Stale lock dead PID | WM-033 line 448 cites PL-006 + HC-044a | Correct |
| 5 complex — PID recycled | Implicit Cat 6a | Not explicitly cited; should route via WM-033 amendment |
| 6 — Two daemons | Defers to PL-INV-001; not a WM fix | OK |
| 7 — Partial `git worktree add` | NO (gap) | Should cite Cat 6a; detector rule `worktree-admin-corrupt` missing |
| 8 — Disk full during merge | Cat 6a via MERGE_HEAD detector (already in reconciliation) | Covered; playbook gap |
| 9 — Interrupt storm | WM-039 implicit (Cat 6 re-detection) | Should make explicit per Scenario 9 amendment |
| 10 — External prune | NO (gap) | Cat 5 / Cat 6a (depending on terminal status) |

**Findings.** Five scenarios (1, 2, 3, 7, 10) have no explicit reconciliation cross-reference in the current spec. Each needs either:
(a) a WM-side one-line "routes to [reconciliation.md §X Cat Y] with evidence-type Z", or
(b) a reconciliation-side detector rule update in [spec.md §8.x] or [schemas.md §6.3].

---

## 9. Requirements that held up well under crash pressure

A few WM-013–WM-034 requirements survive every adversarial probe cleanly; worth naming for R2 reviewers:

- **WM-013b's ordering rule for merge-terminal** (emit `workspace_merge_status` then unlink lock). For the `F`-class merge-status event, the ordering IS durability-correct. Crash between emit-return and unlink leaves a stale lock, which WM-033 sweeps. Safe.
- **WM-013d's forbidding path-reuse.** The canonical-path invariant (WM-INV-005) survives even malicious reopen-bead sequencing because the `run_id` mints fresh. The only loophole is external worktree deletion (Scenario 10), which is an external-tool problem not a spec defect.
- **WM-016 ordering gate (worktree → branch → sidecar → lock → emit).** The order is correct under every crash scenario I traced. A crash between any two steps leaves filesystem state that the restart can detect and classify (the only gap is Scenario 1's "classify as what" — a labeling gap, not an ordering gap).
- **WM-034's fresh-run_id on `reopen-bead`.** Decisively closes the prior-run-vs-new-run identity question under crash recovery; prior-run worktree persistence (WM-031) never conflicts with new-run creation (WM-002) because `run_id`s differ.
- **WM-037a terminal-interrupt-reject rule.** Crash during interrupt of a terminal workspace is semantically OK: reject silently, no transition. Scenario not probed above because it's trivially safe.
- **WM-INV-005 no-registry invariant.** Makes every crash recoverable by filesystem walk; avoids the crash-recovery mismatch between a registry and the filesystem truth. The cost (inability to durably persist `interrupt_state` or `state`) is called out under Scenario 9 as a gap, but the overall discipline is right.
- **WM-022 mechanical implementer-identification.** Works correctly under crash: the task-branch tip + trailers + sidecar are all durable (git commits are fsync'd; sidecar is fsync'd per WM-016). Re-dispatch after merge-pending crash is deterministic.

---

## 10. Hidden assumptions about crash safety

Surfaced during the trace; each is a latent hazard worth recording before finalize.

1. **Git's `worktree add` is atomic at the granularity WM needs.** Scenario 7. It is approximately atomic for modern git, but SIGKILL mid-checkout leaves partial state that `git worktree repair --dry-run` can detect. WM does not name this check.

2. **Parent-directory fsync after rename/unlink is implicit.** The spec asserts file-content fsync (WM-013a) but silently depends on parent-directory fsync for the name mutation to survive power loss. On ext4 `data=ordered` the rename survives via journal; on other configurations it's not guaranteed. Named in neither WM nor EV.

3. **`workspace_leased` being `O`-class is safe because lease state is reconstructible.** True but unstated. A note in WM-013a / WM-016 would prevent an implementer from "upgrading" to `F` to chase a false durability concern.

4. **The sidecar is atomic because `write_json_fsync` is atomic.** False. Scenario 3. The pseudocode abbreviation hides the temp+rename discipline WM-013a explicitly mandates and WM-026 silently omits.

5. **`interrupt_state` survives daemon restart.** False. Scenario 9. WM-039's "durable and observable" language is wrong as written; field is in-memory.

6. **Operators do not delete `.harmonik/` directories.** Scenario 10. A reasonable assumption for MVH; worth a defense-in-depth suggestion (`git worktree lock`).

7. **UUIDv7's time-ordering makes `run_id` reuse unobservable.** Technically true by birthday-bound, but WM-013d's rejection rule has no detection mechanism beyond canonical-path collision. The rejection is a safety property stated without a safety mechanism. Scenario below.

8. **`reopen-bead` always follows prior-run terminal transition.** WM-034 asserts fresh run_id for `reopen-bead`; but if reconciliation issues `reopen-bead` against an in-flight run (bypassing the normal terminal path — reconciliation's RC-028 says `reopen-bead` "MUST clear the in-flight tracking"), does WM's fresh-worktree creation race the in-flight worktree's teardown? Spec is silent on the teardown-before-new-worktree ordering.

9. **The bead-ID-to-ref-safe transformation is idempotent under concurrent reopens.** WM-006a specifies the transformation; nothing names the idempotency. Two concurrent reopens of a bead whose ID contains illegal ref chars each apply the transformation independently — the result SHOULD be identical, but WM does not mandate "the transformation MUST be a pure function of the bead ID."

10. **The `implementer_handler_ref` field, once set at merge-pending entry, survives crash.** False. §6.1 includes the field in the Workspace record but §7.2 has no persistence. On restart, `implementer_handler_ref` is reset; re-derivation requires re-running WM-022's trailer walk. This is actually fine (re-derivation is deterministic) but should be stated.

---

## 11. Finding: UUID reuse detection (Scenario-12 equivalent, cross-reference to prompt item 12)

**Affected requirements.** WM-013d line 266, WM-034 line 461, §7.2 line 695 (`reuse_of_prior_run_id` predicate).

**The gap.** WM-013d says "A released workspace's canonical path MUST NOT be re-leased by a subsequent run." WM-034 says new runs get fresh `run_id`s. Neither declares the **detection mechanism** for a reuse attempt. §7.2's `reuse_of_prior_run_id(run_id)` is an abstract predicate without implementation.

UUIDv7 collision is astronomically improbable (time-ordered 128-bit space with 62 bits of randomness per [event-model.md §4.1 EV-002]), but not zero. More pressing: a **buggy orchestrator that re-generates a `run_id` from an observed seed** (e.g., on a re-dispatch path) could produce reuse that UUIDv7's design doesn't prevent.

WM-INV-005 forbids a registry. So the detection path is ONLY filesystem collision: a new `run_id` whose canonical path already exists triggers `WorkspaceAlreadyExists`. But:

- If the prior run's worktree was operator-deleted (Scenario 10), the path is gone; reuse is undetected.
- If the prior run's worktree was never created (crashed before `git worktree add`; Scenario 1 deeper), the path is gone; reuse is undetected.
- If UUIDv7 collides (impossible in practice), the path collision catches it via git's own "branch already exists" error — the branch `run/<run_id>` still exists after the prior run's worktree is deleted (WM-031 preserves the branch). So **branch collision is the residual detection**.

**Is this safe?** Mostly. Branch collision catches the collision path. Operator-delete-worktree-but-preserve-branch leaves the branch as a reuse detector. The spec's rejection of reuse is safer than first-glance reading suggests — but the **reason it's safe is never stated**.

**Recommendation.** Amend WM-013d and §7.2 to name the detection mechanism:

> **WM-013d [amendment].** Re-use detection relies on two mechanical checks: (a) the canonical path `<repo>/.harmonik/worktrees/<run_id>/` exists → `WorkspaceAlreadyExists`; (b) the task branch `run/<run_id>` exists in the repository → `RunIdReuseForbidden`. Check (b) is the residual detector when the worktree directory has been externally removed per WM-031 note; the task branch is NOT auto-deleted by any harmonik mechanism (WM-031 last sentence + WM-INV-003 append-only rule) and serves as a persistent reuse sentinel. Implementations MUST run both checks before calling `git worktree add`.

This also closes a Scenario-1 loophole: a bare-worktree-but-no-lock state, if cleaned up by the operator, still leaves the branch — so a bug that tries to re-mint the same `run_id` would catch via branch collision.

---

## 12. Proposed amendments (consolidated)

Priority-ordered; cross-reference the scenario each addresses.

| New/amended ID | Section | Shape | Scenarios fixed |
|---|---|---|---|
| `WM-013c [amend]` or `WM-013e` | §4.3 | Classify `(registered, lock-absent)` as evidence-type `bare-worktree-no-lease` / `sidecar-without-lease`; route to Cat 3 | 1, 2 |
| `WM-026 [amend]` | §4.7 | Sidecar write MUST use temp+rename+fsync+parent-dir-fsync | 3 |
| `WM-013a [amend]` | §4.3 | Parent-directory fsync after rename/unlink | 4 (partial), atomicity audit |
| `WM-013b [amend]` | §4.3 | Acknowledge `O`-class terminal event is emit-best-effort; accept event-loss per EV-017 | 4 |
| `WM-033 [amend]` | §4.8 | Staleness rule prefers lock JSON `pid`/`created_at` over mtime; project-scoped argv check | 5 |
| `WM-003a` (new) | §4.1 | `git worktree repair --dry-run` on startup; `worktree-admin-corrupt` evidence type | 7 |
| `WM-003 [amend]` / §8 [amend] | §4.1 / §8 | ENOSPC/EIO/EDQUOT → `transient` with bounded retry cap | (retry discipline) |
| `WM-031 [note]` | §4.8 | Defense-in-depth: `git worktree lock` on create; external-delete routes to Cat 6a | 10 |
| `WM-039 [amend]` | §4.10 | `interrupt_state` NOT durable across restart; relies on Cat 6 re-detection | 9 |
| `WM-013d [amend]` | §4.3 | Reuse detection: canonical-path collision OR task-branch collision | 11 |
| Cross-spec OQ | cross-spec | `workspace_discarded`, `merge_conflict_escalation` → consider class `F` | 4 (observability), fsync audit |
| Cross-spec OQ (reconciliation) | reconciliation §8 | Playbook obligation: Cat 6a investigators MUST `git merge --abort` before `resume-*` verdicts | 8 |
| Cross-spec OQ (reconciliation) | reconciliation §8, schemas §6.3 | Detector rules for `bare-worktree-no-lease`, `sidecar-without-lease`, `worktree-admin-corrupt`, `sidecar-torn-json` | 1, 2, 3, 7 |

Every WM amendment is mechanism-tagged and fits within the existing §4 grouping. No new cross-subsystem obligations are introduced beyond what OQ-WM-005 / OQ-WM-006 already track. No locked-in decision is pressured.

---

## 13. Strongest single finding

**Scenarios 1 and 2 combined.** The v0.3.0 integration delivered a comprehensive lease-lifetime story (WM-013a–d) and a tight emission-ordering gate (WM-016), but the very sequencing protected by that gate has an unaddressed failure window: between `git worktree add` returning and the lock / sidecar fsync sequence completing, the system can reach a state that WM-013c's four-step discovery cannot classify. The state is not theoretical — it's the ordinary shape of any SIGKILL or power loss during run startup. The fix is a one-line evidence-type addition to WM-013c plus a reconciliation-side detector rule, but without it, the intensive lease-lifetime work of R1 integration is outflanked by a race at worktree-creation time.

Secondary: **Scenario 3 (sidecar atomicity asymmetry).** WM-013a explicitly requires write-to-temp + rename for the lock file, and WM-026 silently omits the same discipline for the sidecar written adjacent to it in the same §7.2 function. The asymmetry is the strongest signal that WM-026's atomicity is an oversight rather than a principled choice; the fix is two lines of spec text.

---

## Appendix: Unprobed scenarios from the prompt list

- **11. Clock skew (NFS mtime vs daemon wall clock).** Touched tangentially in Scenario 5; recommendation there covers the load-bearing piece (prefer lock JSON `created_at` over mtime). A deeper NFS-specific probe would require EV's HWM-durability story, which is EV-territory.
- **12. UUID collision.** Covered in Finding §11.
- **13. Concurrent `reopen-bead` verdicts.** [reconciliation.md RC-028] says `reopen-bead` MUST clear in-flight tracking and a subsequent claim produces a fresh run. The race between two concurrent verdicts on the same bead is a reconciliation concern, not a WM concern — WM only observes the post-clear claim. Adversary: reconciliation's RC-028 is silent on two concurrent verdicts; WM inherits whatever arbitration reconciliation provides. Not a WM fix.
- **14. Merge-conflict agent crash.** WM-024 says re-dispatch per handler-contract with a fresh LaunchSpec and fresh budget; handler-contract's HC-044a fail-fast check prevents an orphaned prior session from blocking. Re-dispatch after re-dispatch-agent crash is supported by WM-023's "If the re-dispatched implementer … cannot resolve within its budget … emit `merge_conflict_escalation`." An intermediate crash (neither resolution nor budget exhaustion) would re-dispatch again per WM-024 on the next orchestrator pass, with a fresh budget. The spec is silent on the maximum re-dispatch attempts; could unboundedly loop on a deterministically-crashing handler. Flag as post-MVH concern (OQ-WM-007 partially covers skill registration; cap is a separate question).
- **15. Git worktree prune race.** Covered in Scenario 10.

---

**Reviewer note.** Ten explicit findings across Scenarios 1–10, plus two atomicity-audit-surfaced issues (parent-dir fsync, sidecar asymmetry), plus one detection-mechanism clarification (Finding §11). The integration v0.3.0 delivered is structurally crash-safe at the lease-lifetime boundary thanks to WM-013a–d; the remaining gaps are all one-line evidence-type or atomicity-discipline amendments. No locked-in decision is challenged. Recommend R2 integration applies the eight `[amend]` amendments in §12; the cross-spec OQs (observability classes for `workspace_discarded` / `merge_conflict_escalation`; reconciliation detector-rule additions) can be deferred to R3 of the owning spec.

**Strongest single finding reiterated.** Scenarios 1 + 2 (bare worktree / sidecar without lock). The v0.3.0 lease-lifetime story is otherwise tight; the worktree-creation window is the last silent unreachable state.
