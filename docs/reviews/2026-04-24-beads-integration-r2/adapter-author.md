# Round 2 Adapter-Author Review — beads-integration.md v0.3.0

**Reviewer lens:** I am the engineer building `internal/beads/adapter/` — a Go module that wraps `os/exec` invocations of `br`, parses JSON, classifies errors into `BrError`, manages the intent-log + status-check protocol, and surfaces typed results to the daemon. I read the spec asking "can I build against this without inventing my own contracts?"

## 1. Verdict summary

The R1 integration pass (v0.3.0) closed most of the catastrophic holes: §4.8a pins `--version` handshake, JSON-mode, timeouts, stderr capture, and exit-code classification; §6.1a gives me a `BrError` enum I can actually return from my functions; §8 maps errors to reconciliation categories; BI-031 is reframed into a status-check-before-reissue protocol that no longer hand-waves about "Beads's own idempotency." This is a substantial advance — I can now stand up a `br.Execute()` function and an `Adapter.Terminal()` function with typed errors, and the idempotency loop is implementable without inventing contracts.

But the spec is still **not buildable without invention** on four axes the R2 handoff probes call out, plus four adapter-level mechanics I need to implement and the spec hasn't pinned:

1. **Stderr discipline is declared but not bounded.** BI-025d says "capture stderr fully" — I don't know the byte cap, whether warnings on exit-0 are surfaced or swallowed, whether panic stack traces are passed through verbatim, or how pretty-printed help text interacts with the error structure. This is the difference between "adapter shields operator from noise" and "adapter DOS's its own operator-facing diagnostic path."
2. **Version compatibility window is mechanically undefined.** BI-024 says "compatibility window" and BI-024a says "outside the compatibility window" — but no requirement declares `exact-match` vs `^0.5.x` vs `~0.5.2` vs anything else. I cannot write the `handshakeVersion()` function from the spec text alone.
3. **Status-check-before-reissue (BI-031) has a race path the spec claims is safe but hasn't walked.** The parenthetical "Any race in which Beads completes the write between step 2 and step 4 is observed by reconciliation as the same divergence pattern" (line 376) is load-bearing, and I cannot verify it from the spec — it papers over a concrete at-most-once question I have to get right.
4. **Subprocess concurrency is completely unaddressed.** Two daemon goroutines calling `Adapter.Claim(beadA)` and `Adapter.Close(beadB)` simultaneously — are these serialized by the adapter, parallel with SQLite WAL taking the heat, or is it Undefined? The spec is silent and I have to pick.

The five other adapter mechanics the spec doesn't bind (intent-file atomicity details, adapter-internal state / caching, unit-testability injection points, logging discipline, concurrent `br` invocations) are not probe-mandated but block concrete implementation.

**Recommendation level: moderate.** Not blocking on R2 advance if §4.8a BI-024 compatibility window is clarified and a short §7 adapter-protocol pseudocode block lands; the rest can be absorbed in R3 or at first-adapter-implementation time. But the spec currently promises a level of concrete implementability that three gaps undercut.

---

## 2. Probe 1 — `br` CLI error-envelope robustness

**What the spec has.** §4.8a gives me three hooks: BI-024a (`--version` handshake), BI-025a (exit-code → `BrError` classification), BI-025d (stderr capture). §6.1a has the `BrError` mapping table (lines 470–481). §8 routes each `BrError` to a reconciliation category.

**What I can build with this.** My `br.Exec()` function has a clean shape:

```go
type Result struct { Stdout []byte; Stderr []byte; ExitCode int }
type BrError struct { Kind BrErrorKind; ExitCode int; Stderr string; Cause error }

func (c *CLI) Exec(ctx context.Context, args []string) (*Result, *BrError) {
    cmd := exec.CommandContext(ctx, c.binary, args...)
    // ... run, capture stdout and stderr ...
    if exitCode != 0 { return r, c.classify(exitCode, stderr) }
    return r, nil
}
```

This works for the happy path and for cleanly-exiting-nonzero paths. The spec's §6.1a mapping (`0=OK, 1=NotFound, 2=Conflict, 3=DbLocked, 4=SchemaMismatch, ...`) is concrete enough that I can implement `classify()`.

**What breaks the moment I hit real-world stderr.**

(a) **`br` stderr is pretty-printed help text on arg-parse errors.** If I call `br claim --idempotency-key=foo bd-7` and the pinned Beads version doesn't yet support `--idempotency-key`, `br` is likely to print a multi-line usage message to stderr and exit with something like exit code 2 (common CLI convention) or 64 (sysexits.h) or 1 (clap default). My `classify()` function, per §6.1a, maps exit code 2 → `Conflict`. That is a false positive: the command never even ran, but reconciliation would see "concurrent claim or status collision" and route to Cat 3a torn-write. The spec gives me no mechanism to disambiguate "argparse error" from "claim conflict" because the taxonomy is exit-code-indexed only and §6.1a's "Meaning" column is purely aspirational on the Beads side. I need either (i) a structured JSON error envelope on stderr that the adapter parses, or (ii) a stderr pattern-match fallback, or (iii) a versioned pre-validation of argv against the pinned CLI surface.

(b) **`br` stderr contains binary garbage after a Rust panic.** A panic emits `thread 'main' panicked at ..., stack backtrace: ...` to stderr followed by frame dumps, and the exit code is typically non-zero but unpredictable (101 by Rust convention on panic). BI-025a says unknown exit codes emit `store_divergence_detected{divergence_kind="br_unrecognized_exit_code"}`. Fine. But §4.8 ON-002 (operator-nfr) says stderr is included in operator-facing diagnostics, and an operator-nfr structured-log record has a `msg` field bounded somehow (ON-035 doesn't pin a length). If my adapter forwards a 40 KB panic dump into an ON-035 structured log, it DOS's the operator log. Spec needs either a stderr byte cap or an "abbreviate first N lines, hash the rest" rule.

(c) **`br` exits 0 with stderr containing warnings.** Some CLI tools emit "Warning: schema will be migrated on next write" to stderr while exiting successfully. BI-025d says "Stderr is informational; it MUST NOT be parsed for state." That's fine for classification, but silently dropping stderr warnings on successful exits is a regression from "operator visibility." Implementer's default will be: if exit=0, discard stderr; if exit!=0, forward stderr in `BrError`. But this means an operator who needs to see a warning (e.g., "next upgrade will break compat") cannot — unless ON-035 requires the adapter to emit warning-level structured logs for any-stderr-on-success case. Spec doesn't pin this.

(d) **Timeout during stderr drain.** If `br` is stuck and hits the 5s read timeout (BI-025c), my `exec.CommandContext` will kill it — but is stderr fully drained? Go's `exec.Cmd` requires explicit goroutine reads or `StderrPipe()` handling; a naive `cmd.CombinedOutput()`-style implementation loses partial stderr on SIGKILL. BI-025d says "capture stderr fully on every invocation" — concretely, fully even on timeout. I need to either (i) drain stderr in a separate goroutine bounded by my own deadline, or (ii) accept partial stderr under timeout. Spec doesn't say which.

**Recommendations.**

1. **Add BI-025e — stderr discipline:** stderr is captured up to N KiB (suggest 64 KiB); on overflow, the adapter truncates with a suffix marker and emits a debug-level log per ON-035. On exit-0 with non-empty stderr, the adapter MUST emit a `warn`-level ON-035 record carrying the stderr (redacted per ON-022) but MUST classify the call as `BrOK`.
2. **Add BI-025f — argv-mismatch pre-validation:** before the first write invocation at daemon startup (co-located with the BI-024a handshake), the adapter MUST probe the pinned Beads CLI's argument surface (e.g., `br claim --help --format=json` or equivalent) to verify the adapter's expected flags exist. Failure matches BI-025a's `BrSchemaMismatch` and fails daemon startup. This converts (a) above from a runtime false-positive into a startup failure.
3. **Add a sentence to BI-025d** that partial stderr is acceptable on timeout (`BrUnavailable`) but MUST be fully captured on cleanly-exiting non-zero paths.
4. **Pin the Rust-panic path explicitly:** exit code 101 on Rust std panic. Add to §6.1a's exit-code table: `101 → BrOther (Rust panic), emit store_divergence_detected`. This is legit "pinned Beads version surface" knowledge.

**Severity:** medium-high. Gaps (a) and (d) are actual implementation bugs waiting to happen. (b) and (c) are quality-of-operator issues but could slip past MVH.

---

## 3. Probe 2 — Beads version-pin compatibility window

**What the spec has.** BI-024 says "A harmonik release MUST name the Beads version it tested against," and "backwards-incompatible Beads changes" require an adapter change. BI-024a says the adapter MUST "compare the parsed version against the pinned version declared in the harmonik release manifest" and "A version mismatch outside the compatibility window of BI-026, OR an unparseable output, MUST fail daemon startup with exit code 8." BI-026 says harmonik "MUST either (a) remain pinned to the prior Beads version and delay upgrade, or (b) ship a harmonik release with an adapter change that handles the new surface."

**What I can build with this.** I can write `parseVersion(output)` returning a `(major, minor, patch, prerelease)` tuple, and a `handshake()` function that returns `(pinned, observed)`. I can exit-code-8 on parse failure.

**What I cannot build.** I cannot write the `isCompatible(pinned, observed)` predicate, because the spec never defines what "the compatibility window" is. The term appears three times (BI-024a, BI-026, once in critic review) and has zero concrete mechanical definition.

Let me walk the handoff scenarios concretely:

(a) **Harmonik pins 0.5.2, box has 0.5.2.** Trivially compatible. Handshake passes.

(b) **Harmonik pins 0.5.2, box has 0.4.8.** Backwards. Pin says 0.5.2; BI-026 says we have to either stay pinned (operator upgrades Beads first) or ship an adapter change (harmonik release supports 0.4.8). The spec doesn't say which. If the MVH shape is "exact-match-only" (critic's proposed BI-024a: "harmonik release MUST support exactly the one Beads version"), then 0.4.8 fails the handshake — fine, I exit 8. That's implementable.

(c) **Harmonik pins 0.5.2, box has 0.5.1.** One patch behind. Semver says patch releases are bug-fixes only; no surface change. But Beads is pre-1.0, and pre-1.0 semver is "all bets are off" — is 0.5.1 compatible? The spec gives me no policy.

(d) **Harmonik pins 0.5.2, box has 0.5.3.** One patch ahead. Same question inverted. And now I also have to worry about Beads 0.5.3 adding a new enum value to `CoarseStatus` (post-MVH forward-compat, which the critic flagged). My JSON parser on `CoarseStatus` will hit an unknown value and needs a rule — critic recommended treating unknowns as `BrSchemaMismatch`. Spec doesn't confirm.

(e) **Harmonik pins 0.5.2, box has 0.6.0.** One minor ahead. Same situation, but 0.6.0 is more likely to contain a surface change. Reject?

**The spec's answer space is binary: "in the window" or "out." No window is declared.** If I ship with exact-match, I break operators who have 0.5.1 when we pin 0.5.2. If I ship with range-match, I'm inventing contract the spec doesn't authorize.

**Recommendations.**

1. **Adopt exact-match for MVH** (critic's recommendation). Add BI-024b: "The adapter's compatibility window at MVH is exact-match on `(major, minor, patch)`, ignoring prerelease suffix. Post-MVH, a window may widen to cover backwards-compatible patch releases, tracked in OQ-BI-008." This closes the mechanical gap and preserves operator safety (a mismatch is never silent). Single-version pin matches the locked-decision-13 shape (Beads pre-1.0, pin per release).
2. **Add a migration handshake:** if exact-match fails, the adapter's error message (and the structured-log emission) MUST include the pinned version, observed version, and a suggested remediation step (`brew upgrade beads` or equivalent operator-facing phrasing). This lives in operator-nfr territory but is the adapter's outbound contract.
3. **Cite the pin location:** BI-024 says "the release manifest" — where? `go.mod`? A `specs/_registry.yaml` field? A `harmonik/version.go` const? Implementer must invent. Recommend: "the pinned version is declared in the harmonik release's `beads.pinned-version` config field per [operator-nfr.md §4.4]" or equivalent.

**Severity:** high. I can't write the handshake function from the spec. This is the probe-mandated question and it returned "hand-wavey."

---

## 4. Probe 3 — Audit-log idempotency (BI-031 status-check-before-reissue)

The handoff mandated walking four recovery scenarios against BI-031. Let me walk them carefully, because this is the crispest part of the spec and also where the subtle race hides.

**Setup.** BI-030: adapter fsyncs intent file, calls `br`, deletes intent on success. BI-031: on startup with stale intent, read `intended_post_state`, call `br show <bead_id>` to get current status, three-way branch:

- status == `intended_post_state` → delete intent, no-op recovery.
- status == `pre_state` → re-issue the write.
- status neither → Cat 3a divergence.

**Scenario (a) — crash before `br` invocation.** Sequence: intent file written + fsynced → crash. On restart: intent file present; `br show` returns `pre_state` (claim case: `open`; close case: `in_progress`; reopen case: `closed`). Branch 2 fires → re-issue. **Verdict: correct.** The idempotency file is the evidence and the re-issue is safe because no prior write landed.

**Scenario (b) — crash during `br` execution.** Sequence: intent fsynced → `br claim` forked and running → crash mid-SQLite-write. This is the ambiguous case. Beads's SQLite transaction may have committed, may have rolled back. On restart: intent file present; `br show` returns either `pre_state` or `post_state` depending on timing. Branch 2 or Branch 1 fires. **Verdict: correct if SQLite is transactional-atomic, which it is.** Since a SQLite transaction either commits or doesn't, there's no "halfway" state; `br show` sees one of the two valid states. Good.

**Scenario (c) — crash after `br` succeeds but before adapter sees response.** Sequence: `br` writes to Beads audit log, exits 0, kernel flushes pipe buffers, but the adapter process crashes before reading `exec.Cmd.Wait()`. On restart: intent file present; `br show` returns `post_state`. Branch 1 fires → delete intent, emit `bead_terminal_transition_recovered{recovery_path="status_match"}`. **Verdict: correct.**

**Scenario (d) — crash after adapter sees response but before intent file delete.** Sequence: `br` returns 0 → adapter reads Result → adapter is about to `os.Remove(intent_path)` → crash. On restart: intent file present; `br show` returns `post_state`. Same as (c). **Verdict: correct.**

**All four scenarios handled.** Good.

**But now the race path the spec (line 376) papers over.**

**Scenario (e) — concurrent claim race during recovery.** Sequence: daemon A crashes mid-claim for bead B (intent present, Beads audit may or may not have the entry). Daemon A restarts; runs PL-005 step 6 (read Beads) and eventually step 8 (dispatch reconciliation). Between step 6 and the adapter's recovery pass, daemon A reads `br show <bead_id>` and gets `pre_state=open` (claim never landed). Adapter plans to re-issue.

But wait — is there a "daemon A recovery" vs "another actor could write between step 2 and step 4" race the spec acknowledges? Per-project single-daemon invariant (memory: `project_harmonik_process_lifecycle`) says no concurrent daemons. So the race is against: another agent holding the Beads-CLI skill who is allowed to write... but BI-004 and §4.9.BI-027 both forbid agents from issuing claim/close/reopen. The race is therefore only against _the investigator workflow running inside daemon A_ — which is the same daemon, serialized on its own event loop. Good, that's not a real race unless the adapter is concurrent internally (probe 4).

**However**, the status-check is not strictly read-compare-write-atomic. Between step 2 (`br show`) and step 4 (`br claim`), a Cat 3c auto-resolver could fire from the reconciliation dispatch (step 8), issuing `br close` on a bead that the adapter is about to re-claim. The adapter's re-claim call sees `open`, issues the claim, succeeds. Now we've claimed a bead that reconciliation just closed — this is a Cat 3a condition per BI-INV-004's "write appears with no matching intent"... wait, the intent is there. Hmm.

Walking it more carefully: the Cat 3c auto-resolver (§8.6 reconciliation spec) fires on "merge commit on target branch + bead still in_progress" — not a claim race. Cat 3c is about closing an already-merged run. It won't race a claim recovery. So scenario (e) is not a real race inside a single daemon.

**The parenthetical is defensible.** The spec claims "Any race in which Beads completes the write between step 2 and step 4 is observed by reconciliation as the same divergence pattern (intent log present, post-state reached) and routes safely." Walking it: if step 2 reads `pre_state` but between step 2 and step 4, Beads completes the write (somehow), step 4's `br claim` would then... fail? Or succeed? If `br claim <bead>` on an already-`in_progress` bead returns a `BrConflict` (exit code 2), the adapter sees conflict and... the spec doesn't say what to do.

**Actual gap in BI-031:** what if the re-issue at step 4 returns non-`BrOK`? Specifically:
- Returns `BrConflict` → the intended state was already reached by some other path. Safe? Ambiguous — is it safe to delete the intent? The spec says to re-issue as "at-most-once because step 3 already filtered the success case," but the race case where Beads-completes-between-2-and-4 lands on `BrConflict`, not `BrOK`. BI-031 doesn't tell me how to handle conflict on reissue.
- Returns `BrDbLocked` → retry? With what backoff? How many times before escalating to Cat 0?
- Returns `BrUnavailable` (timeout) → leave intent file, retry next startup? Or retry now with new timeout?
- Returns `BrOther` → emit `store_divergence_detected` and leave intent file, per BI-025a. But the intent file is already present; is it still evidence for Cat 3a or is it now evidence for some other category?

**This is the concrete gap.** BI-031's four-step sequence ends at "re-issue" with no error-handling branch. The happy path works; the error paths are unspecified.

**Recommendation.**

Add **BI-031c — Reissue failure handling:**

> If the step-4 re-issue returns a non-`BrOK` result, the adapter MUST:
> - `BrConflict` on reissue → treat as recoverable: re-execute step 2 (`br show`) once; if the now-observed status equals `intended_post_state`, delete the intent file and emit `bead_terminal_transition_recovered{recovery_path="conflict_post_state"}`; otherwise emit `store_divergence_detected{divergence_kind="beads_status_unexpected"}` and leave the intent file for Cat 3a.
> - `BrDbLocked` on reissue → retry up to N times (default 3) with exponential backoff bounded by [operator-nfr.md §4.11]; after exhaustion, classify as `BrUnavailable` and leave the intent file.
> - `BrUnavailable` on reissue → leave the intent file; do not retry in this recovery pass; the next daemon startup will retry.
> - `BrOther` or `BrNotFound` or `BrSchemaMismatch` → emit `store_divergence_detected` per BI-025a/§8 and leave the intent file.

**Severity:** medium-high. BI-031 is the spec's sharpest contract and the error-path gap is the one way it can produce either double-writes (if the adapter retries too eagerly on conflict) or stuck intent files forever (if the adapter never retries after a single `BrDbLocked`).

---

## 5. Probe 4 — Store-authority rules on git/Beads disagreement

**Walking the disagreement matrix from the adapter's seat.** The adapter is a caller of `br` only; it doesn't walk git. But the adapter is also the sole write surface for Beads (BI-012), so when reconciliation's Cat 3 detector finds a disagreement, the remediation (if any) lands back on my Close/Reopen calls.

**(a) Beads `closed` but no merge commit in git.** This is the canonical Cat 3 case. BI-022 says "MUST NOT be silently auto-reconciled into git's direction." Concretely: reconciliation detects this, dispatches an investigator, investigator emits a `reopen-bead` verdict, daemon's verdict-executor calls my `Adapter.Reopen(runID, transitionID, beadID)`. **From the adapter's seat: this is a normal reopen call.** The adapter doesn't know or care that the cause was a git-vs-Beads disagreement. Good — clean separation. Handled.

**(b) Beads `in_progress` but merge commit exists in git (Cat 3c).** Reconciliation §8.6 Cat 3c detector fires. Its default auto-resolver calls `Adapter.Close(...)` with op=`close`. BI-010b explicitly authorizes this; BI-023's rationale note points to it. **Adapter's seat: normal close call.** The only wrinkle is BI-010's description says close fires "when a run's workflow reaches a success terminal state AND the merge to the target branch has completed." In Cat 3c's case, the run workflow may not have fully reached terminal state in the adapter's view — but BI-010b carves this out ("reconciliation writes are run-terminal-equivalent, not intra-run"). Good — the carve-out is explicit. Handled.

**(c) Beads `open` but commits exist on the run-branch in git.** This case is not in BI's taxonomy. Let me check reconciliation: RC Cat 2 (non-idempotent in-flight) handles "checkpoint exists but no terminal event" — that's different (about in-flight runs). Cat 3 generic would pick up "bead open but run commits exist" because the stores disagree on whether work has been done. Cat 3 dispatches an investigator.

Wait — this is actually a _claim race_. If Beads says `open`, the claim never happened; but if git commits on `run/<run_id>/...` exist, something did some work. Either:
- The claim succeeded, a run started, later someone ran reopen, the task branch commits remain — that's just the "reopen leaves branch artifacts" case. Per RC-028 "A `reopen-bead` verdict MUST clear the in-flight tracking for the target bead. A subsequent claim of the bead MUST produce a new run with a fresh worktree and a fresh branch." So old commits on old branches are normal post-reopen state, not a Cat 3.
- The claim somehow never hit Beads audit log but the adapter returned success — that's BI-INV-004 (status-change observed with no matching intent-log). Cat 3a.

**From the adapter's seat: doesn't affect me.** The adapter doesn't walk git; its contract is intent-log correctness, which RC's Cat 3a detector consumes. Handled.

**(d) Beads doesn't have the bead at all but git has commits referencing it.** This is a "ghost bead ID" case. The trailer `Harmonik-Bead-ID: bd-xyz` exists on commits, but `br show bd-xyz` returns `BrNotFound`.

The spec is silent on this. §8 maps `BrNotFound` to "Cat 3 (generic) — Beads-vs-harmonik divergence; investigator dispatch." So the adapter returns `BrNotFound` and reconciliation's detector (somewhere) catches the Cat 3 and dispatches. But the detector itself: RC's detectors (§4.3 RC-013 / RC-014) read JSONL and git; they don't routinely test `br show` against every bead-ID found in git. When would this be detected?

Scenario: operator imports a project from another machine, git repo comes with, Beads database doesn't (or is a different Beads store). Daemon starts; PL-005 step 5 walks git and finds run/bd-xyz branches; step 6 reads `br ready` (doesn't return bd-xyz because it's not in Beads); step 7 builds the Run model — what does it do with run/bd-xyz branches whose `bead_id` doesn't resolve?

**This is genuinely unspecified.** BI-INV-002 ("bead ID is stable across harmonik's lifetime") assumes the bead exists; BI-008 says IDs are stable "from creation to tombstone." But a ghost ID is a bead that was never in this Beads store. The adapter handles it fine (returns `BrNotFound`); what PL step 7 does with it is outside my scope. But from an adapter-author seat: if `br show bd-xyz` returns `BrNotFound` during a recovery pass (BI-031 step 2), my adapter will emit `store_divergence_detected` per §8 routing and leave the intent file. That's fine. Handled, but note the spec is silent on PL step 7's behavior.

**Adapter-side observations:**

1. **The store-authority rules as written don't require any adapter action.** The adapter is a write surface; authority decisions are made by reconciliation before calling the adapter. The BI-INV-003 git-wins invariant is enforced upstream; by the time I get a Close call, someone else has decided it's the right call. This is the right separation.

2. **BI-INV-004 depends on intent-log evidence completeness.** The invariant says every Beads status change is auditable via (a) intent log present-then-absent, or (b) Beads's recorded transition. If the adapter deletes the intent file on success, evidence for (a) vanishes. The Cat 3a detector has to see "no intent file for this transition" and "audit entry exists" and conclude it's a normal completed write — but how does it know the transition ever had an intent file? Through the audit log's `run_id:transition_id:op` correlation. That only works if Beads records the idempotency key verbatim. Which we still aren't sure it does (see probe 3 echo).

**Wait.** BI-031 reframed to be "Beads-idempotency-independent" via status-check. But BI-INV-004 still requires "the conjunction of (a) idempotency-keyed intent log entry ... and (b) Beads's recorded transition" to verify status-changes. If Beads doesn't record the key, (a) is "intent log entry before-success / absent after-success" — the post-success absence is the evidence. But the Cat 3a detector per §8.4a says: "the daemon's on-disk intent log at `.harmonik/beads-intents/` records an outstanding `br` write ... AND the Beads audit log at restart time either shows no corresponding entry OR shows an entry matching the idempotency key." The reconciliation detector still assumes Beads records the key. This is a **residual inconsistency** between BI-031 (Beads-idempotency-independent) and RC §8.4a (Beads-idempotency-dependent).

**Recommendation.** Sync BI-031 and RC §8.4a. If BI-031 no longer requires Beads to persist the idempotency key, RC §8.4a's detection rule needs to be rephrased to match: "the Beads audit log at restart time either shows no transition for `(bead_id, op)` in the intent's time window, OR shows a transition with matching post-state." This is a **cross-spec coordination item** BI needs to flag explicitly in §9.3 or as an OQ. Flag also: the retained "OR shows an entry matching the idempotency key" clause in §8.4a is residual from pre-reframing and should be dropped or qualified.

**Severity:** medium. (a), (b), (c) all handled; (d) is underspecified but not adapter-seat-blocking; the BI-031 / RC §8.4a inconsistency is real and should be resolved.

---

## 6. Intent-log atomicity walk

**What the spec has.** BI-030: "persist an intent-log entry to `.harmonik/beads-intents/<idempotency_key>.json` and MUST fsync the file per the durability contract of [event-model.md §4.4]." §6.2 declares the filename shape; OQ-BI-003 handles the colon-encoding wrinkle.

**What I need to build this.**

1. **Tempfile + rename idiom, or direct create.** The spec says "persist ... and fsync." The standard atomic-file-create idiom is write-to-tempfile, fsync(tempfile), rename(tempfile, final), fsync(directory). The spec doesn't prescribe this; it says "persist the file." Implementer default would be `os.Create(final); write; fsync(file); close`, skipping the rename. This is vulnerable to a partial-write crash (tempfile path avoids this). But since the file content is deterministic (same `(run_id, transition_id, op)` always produces the same payload), a partial file is at worst "read failure on restart" which would route to Cat 3a anyway. So direct-create is survivable but not ideal.

2. **Parent directory fsync.** POSIX: rename is atomic only if the parent dir is fsynced afterward. The spec says "fsync the file" — which is file-level durability, not filename-level durability. If the kernel loses the directory entry (rare but possible on crash before directory metadata is flushed), the intent file is invisible on recovery. Event-model §4.4 EV-016/EV-017 — let me re-check what it declares.

<cite>Per R1 implementer review, §4.4's fsync contract is per-event fsync; no multi-event atomicity. The directory-entry fsync isn't spelled out.</cite> So my adapter has to remember to fsync the parent directory; the spec doesn't require it explicitly. Implementer default will forget. Bug.

3. **Rename-into-existing semantics.** If I tempfile-and-rename, and the target filename exists (from an earlier identical intent — theoretically possible if `run_id:transition_id:op` is re-called), rename atomically replaces. Fine, but `O_EXCL` on the tempfile creation path prevents concurrent adapter goroutines from writing the same tempfile name. Not named in spec.

4. **Post-success delete ordering.** The adapter deletes the intent file after `br` success (BI-030). Does it fsync the parent dir after unlink? If yes, the delete is durable; if no, a crash immediately after `os.Remove` but before dir-fsync could leave the intent file "present" on recovery — which would drive the adapter into BI-031's status-check recovery path, which sees `post_state`, deletes the file again. Recovery is idempotent. So strictly: directory fsync after delete is unnecessary for correctness (BI-031 handles the repeat-delete case). Good.

5. **Crash between `br` success return and file delete.** Covered by BI-031 Scenario (d) — status-check sees post-state, deletes. Correct.

**Recommendations.**

1. **Add BI-030a — Intent-file atomicity protocol:** The adapter MUST implement the intent-file write as: (i) create tempfile `.harmonik/beads-intents/<key>.json.tmp` with `O_CREATE|O_EXCL|O_WRONLY`; (ii) write payload; (iii) fsync the file; (iv) close the file; (v) rename to `.harmonik/beads-intents/<key>.json`; (vi) fsync the parent directory. On failure at any step, the adapter MUST attempt to clean up the tempfile and MUST NOT proceed with the `br` call.
2. **Add to BI-030 a post-success fsync-optional clause:** directory fsync after unlink is optional because BI-031 recovery is idempotent on repeat-delete.
3. **Clarify colon-in-filename policy now, not later.** OQ-BI-003 punts; I need to know. The simplest rule: always encode the colon as `_` (so filename is `<run_id>_<transition_id>_<op>.json`). This works on every filesystem, and the decoding for intent-file-recovery-parse is trivial because the intent file's JSON payload carries the decoded fields. Recommendation: close OQ-BI-003 with mechanical encoding at MVH.

**Severity:** medium. Tempfile+rename+dir-fsync is folklore for Go systems programmers, but the spec should pin it so reviewers can verify.

---

## 7. `br` subprocess management

### 7.1 Timeout granularity (BI-025c)

**Spec says:** 5s reads, 10s writes, operator-tunable. "Bounded by a subprocess wall-clock timeout."

**Adapter-seat question:** is the timeout the total wall-clock from `exec.Cmd.Start()` to `cmd.Wait()` return? Or per-call exec-window (just the `Start()`)? The spec uses "subprocess wall-clock timeout" which reads as total wall-clock, matching `exec.CommandContext(ctx, ...)` + `context.WithTimeout(ctx, 5*time.Second)` behavior. Good.

**But:** `br claim` that's blocked on SQLite WAL fsync stall (>10s). SQLite on a loaded filesystem with `PRAGMA synchronous=FULL` can take arbitrary time to fsync. The 10s write timeout fires, my adapter kills the subprocess — but Beads may still be mid-transaction when SIGKILL arrives. SQLite's WAL commits are atomic, so either the transaction commits or it doesn't, but my adapter has no way to know which happened. This is BI-031 Scenario (b) territory. Handled correctly on next startup. Good.

**Edge case:** the subprocess is SIGKILL'd but the SQLite fsync is in progress; the Beads SQLite file may be in an inconsistent state at the filesystem level (WAL checkpoint interrupted). This is Beads's problem, not mine. But my adapter should not assume `BrUnavailable` (timeout) = "no write landed"; per BI-031 Scenario (b), the recovery path makes no such assumption. Good.

**Recommendation:** add an informative note to BI-025c: "A write-timeout result (`BrUnavailable`) does NOT guarantee the write did not land in Beads; recovery per BI-031 status-check is the authoritative disambiguation."

### 7.2 Concurrent adapter calls

**Spec is silent.** The daemon dispatches multiple runs concurrently (per PL and [execution-model.md §4.3]). Two goroutines could simultaneously call `Adapter.Claim(beadA)` and `Adapter.Close(beadB)`. Are these:
- Serialized by the adapter (internal mutex)?
- Parallel subprocess invocations, relying on Beads SQLite WAL for concurrency?
- Undefined (race condition)?

**SQLite WAL-mode** supports multiple readers + single writer. Beads's SQLite fork — assuming WAL — allows one concurrent `br` invocation to write while others read. Two concurrent `br claim` calls on different beads would serialize at SQLite's write lock, likely with `BUSY` retry handled inside `br` itself (or not — Beads-specific).

**Adapter choice.** If the adapter serializes internally, throughput drops (every write waits for every other write globally). If parallel, SQLite handles it — but the adapter has to handle `BrDbLocked` gracefully.

**Recommendation.** Add **BI-025e — Concurrent subprocess policy:** The adapter MAY invoke `br` subprocesses concurrently; Beads's SQLite is expected to serialize at the storage layer. `BrDbLocked` results MUST be retried with bounded exponential backoff (default 3 attempts, 50ms/200ms/800ms per [operator-nfr.md §4.11]); exhaustion classifies as Cat 0 `BrUnavailable`. Intent-log writes for concurrent `br` invocations MUST use distinct tempfile names (the tempfile naming is per-idempotency-key, so two keys don't collide).

Adapter-internal correctness: two goroutines calling `Adapter.Terminal()` with _the same_ `(run_id, transition_id, op)` is an invariant violation of the daemon (two paths shouldn't claim the same transition) but the adapter should at least not corrupt state. Tempfile `O_EXCL` gives us that. Add a sentence.

### 7.3 Adapter logging

**Spec says:** §4.8a BI-025d "stderr ... included in the typed error structure surfaced to the daemon and to operator-facing diagnostics per [operator-nfr.md §4.1 ON-002]." §4.8a does not describe the adapter's own debug/info/warn logs. Where does the adapter say "invoked `br claim bd-7`, exit 0" vs "retrying `br close` after BrDbLocked, attempt 2/3"?

**Recommendation.** Add a line in §4.8 or §4.8a: "The adapter MUST emit structured logs per [operator-nfr.md §4.9 ON-035] for: every `br` invocation (debug level: argv + exit code + duration), every retry (info level), every `BrError` classification (warn or error level by kind), and every intent-log recovery action (info level)." Subsystem identifier: `beads-adapter` (register per EV-034a).

**Severity:** medium. Without this, the adapter logs to stderr ad-hoc and the operator has to grep whatever the implementer chose.

---

## 8. Adapter-internal state and unit-testability

### 8.1 In-memory state survival

**Spec is silent.** Does the adapter carry in-memory state that needs persistence?

Candidates:
- Pinned Beads version (from BI-024a handshake). Once checked, cache in memory; re-check only on daemon restart. Doesn't need persistence — startup re-validates.
- `br` binary path (resolved at startup from PATH or config). Same — startup resolves.
- Any retry counters / circuit-breaker state. Not in spec; per-call local state.
- Connection pooling. `br` is fork+exec per call; no connection state.

**Verdict:** the adapter is effectively stateless across restarts; every piece of state is reconstructed at startup. Good — crash-safe by design. Spec should say this.

### 8.2 Unit-testability

**Spec §10.2 obligations:** "Contract tests against a live `br` binary at the pinned version" for BI-005..BI-009; "Crash-injection tests kill the adapter between intent-log fsync and `br` call completion" for BI-029..BI-032.

**What's missing:** unit tests against a fake `br` binary. The spec doesn't name a fake injection point. As the adapter author, I want:

```go
type CLI interface {
    Exec(ctx context.Context, args []string) (*Result, *BrError)
    Version(ctx context.Context) (Version, *BrError)
}

func NewAdapter(cli CLI, intentDir string, logger *slog.Logger) *Adapter { ... }
```

The interface lets unit tests pass a `fakeCLI` that returns canned responses. The spec is silent on whether the adapter's CLI dependency is interface-shaped or concrete. Implementer default varies.

**Recommendation.** Add to §10.2 BI-029..BI-032 (or a new line): "The adapter MUST expose a `br`-invocation seam suitable for unit-testing with a fake `br` (e.g., an interface-typed dependency). Integration tests against a live `br` binary complement unit tests; they do not replace them."

Also: the `br` binary path should be injectable (config field, not hardcoded) for tests and for operators who install `br` outside the default PATH. Implementer default: env-var `HARMONIK_BR_BINARY` or a config field. Not in spec.

**Severity:** low. Adapter-authors will invent the seam; it's standard Go idiom. But naming it prevents tightly-coupled-to-`exec.Command` code from landing.

---

## 9. Adapter recovery on startup (PL-005 step 6 + intent-log replay)

**Spec says:** PL-005 step 6 (process-lifecycle.md line 221) — daemon queries `br ready` + audit-log + in-progress reads at startup, with 5s timeout; Cat 0 on timeout. PL-005 step 4 is Cat 0 pre-check. PL-006 step "Stale intent files" (line 238) — daemon enumerates `.harmonik/beads-intents/` and LEAVES stale entries on disk for Cat 3a classification at step 8.

**BI-031 says:** "On startup with a stale intent file present, the adapter MUST execute the following recovery sequence [the 5-step protocol]."

**Conflict.** PL-006 says "MUST be LEFT on disk for classification by the reconciliation Cat 3a detector ... during §PL-005 step 8." BI-031 says the adapter does the recovery work directly (status-check, reissue, emit events) on startup. When does the adapter's BI-031 recovery run vs reconciliation's Cat 3a detector?

**The right reading** (from PL-006's parenthetical "orphan sweep itself MUST NOT invoke reconciliation detectors ... would deadlock"): the orphan sweep at step 3 leaves intent files on disk; reconciliation detection at step 8 classifies them as Cat 3a; Cat 3a's default auto-resolver (§8.4a: "adapter re-issue") calls the adapter's BI-031 recovery logic.

So BI-031 isn't run "at startup" by the adapter standalone — it's run when Cat 3a dispatches the auto-resolver, which is at PL-005 step 8 inside reconciliation dispatch. Fine.

**But BI-031's wording suggests adapter-driven recovery:** "On startup with a stale intent file present, the adapter MUST execute the following recovery sequence." This reads like the adapter scans intent files itself. Is the trigger the adapter's constructor (scan dir once at init) or the Cat 3a dispatcher (call `Adapter.RecoverIntent(intentPath)` per stale entry)?

**Cross-spec inconsistency.** Per PL+RC, the trigger is Cat 3a dispatch from RC §8.4a default response. Per BI-031 wording, the trigger is "startup." These need to align.

**Recommendation.** Rewrite BI-031's intro clause: "When the reconciliation Cat 3a auto-resolver per [reconciliation/spec.md §8.4a] dispatches an intent-file recovery action, the adapter MUST execute the following recovery sequence." This makes the adapter a callee of reconciliation, not an autonomous startup actor. The adapter exposes:

```go
func (a *Adapter) RecoverIntent(ctx context.Context, intentKey string) (RecoveryResult, *BrError)
```

And reconciliation's verdict executor calls it per Cat 3a case. The adapter doesn't scan `.harmonik/beads-intents/` on its own.

**Severity:** medium. This is a real cross-spec ambiguity that will cause the adapter to be built with the wrong constructor contract.

---

## 10. Beads-CLI skill capability surface (BI-027/028) audit

**Spec says:** BI-027 — skill is the only agent access path, documents command surface + write discipline. BI-028 — every agent has it by default. OQ-BI-007 tracks "Beads-CLI skill capability partition (read-only / investigator / no-write) ... mechanically (wrapper-fence) or by convention (skill documentation)." Default-if-unresolved: convention-based at MVH.

**Adapter-author seat observation.** The adapter is a daemon-side component; agents invoke `br` through the skill independently of the adapter. So strictly, this is not the adapter-author's problem. BUT: BI-INV-001 (no intra-run writes) is partly enforced by the adapter (it's the sole write surface in the daemon code path), and partly by agent discipline (agents don't issue claim/close/reopen through the skill). If the skill enforces writes by convention only, the adapter cannot verify BI-INV-001 mechanically — an agent could call `br close` and the adapter would never see it.

The Cat 3a detector (RC §4.3 RC-014) per BI-INV-004 partly catches this: "A status-change observed in Beads with NO matching intent-log entry ... MUST trigger Cat 3a." Good — out-of-band writes get detected. But detected ≠ prevented.

**Recommendations (from the adapter's interest).**

1. **Log the enforcement model explicitly.** BI-027 should say: "MVH enforcement of the write-discipline clause is convention-based via skill documentation. The mechanical safety net is BI-INV-004's Cat 3a detection of out-of-band writes. A wrapper-fenced skill is tracked in OQ-BI-007 post-MVH."
2. **Name the skill's expected command inventory** so I can write contract tests that my adapter's command usage doesn't conflict with the skill's. Specifically: the adapter uses `br --version`, `br ready`, `br show`, `br claim`, `br close`, `br reopen`, `br audit` (or equivalent); the skill documents these plus `br graph`, `br list`, possibly `br create`. An enumeration in an informative note (not normative) in §4.9 would help.

**Severity:** low from the adapter seat (the agent-side discipline is not my code). But the enforcement-model ambiguity affects BI-INV-001's strength, which the adapter depends on.

---

## 11. Failure stories — walkthroughs from the adapter's seat

### 11.1 Story A — Beads upgraded mid-daemon-lifetime

**Setup.** Harmonik daemon running, pinned to Beads 0.5.2. Operator upgrades Beads to 0.6.0 on disk without restarting the daemon.

**What the adapter sees.** Next `br ready` invocation spawns a subprocess that runs the new Beads binary. Its output may or may not parse under my BI-025b JSON expectations. If 0.6.0 added a field, JSON still parses (additive). If 0.6.0 changed a field name, parse fails → `BrSchemaMismatch`.

**Verdict from spec.** BI-025b: parse failure → `BrSchemaMismatch`. §8: `BrSchemaMismatch` → Cat 0, exit code 8. But the daemon is already running, not starting. Cat 0 at runtime? The spec's `daemon_degraded` state per PL-010 is scoped to pre-`ready` Cat 0 (per PL v0.3's narrowing, per PL revision history); post-`ready` Cat 0 is OQ-PL-009. So my adapter returns `BrSchemaMismatch`, the caller (daemon) doesn't know what to do with it mid-lifetime.

**Gap.** The adapter's error is well-classified, but the caller contract for "what happens when the adapter starts returning `BrSchemaMismatch` after daemon has been running" is unclear. Likely: daemon initiates graceful shutdown and emits a new `daemon_startup_failed`-equivalent. Not spec-pinned.

**Adapter action:** classify and return. Caller's problem.

### 11.2 Story B — Intent log fills up during Beads-unavailable window

**Setup.** Beads SQLite locked or crashed (DB file corrupted). Adapter attempts 10 concurrent writes; all timeout after 10s each; all leave intent files on disk.

**What the adapter sees.** 10 writes return `BrUnavailable`; 10 intent files remain in `.harmonik/beads-intents/`. The daemon continues to dispatch more runs; more writes attempted; more intent files accumulate. At some point the disk fills, or the daemon hits its own hard limit.

**Spec gap.** No backpressure rule. The adapter should detect consecutive `BrUnavailable` errors and refuse new writes (open a circuit), but the spec doesn't prescribe this. Implementer default: no backpressure, intent log fills disk, daemon dies.

**Recommendation.** Add **BI-025g — Beads-unavailable backpressure:** After N consecutive `BrUnavailable` results (default 5) within window T (default 60s), the adapter MUST transition to `beads-unavailable` state and reject new write calls with a pre-checked `BrUnavailable` (no subprocess spawn) until the next successful `br --version` probe. The daemon's response to this state is owned by PL-010 / operator-nfr. Alternatively, this could be decided by PL's Cat 0 post-ready logic (OQ-PL-009).

**Severity:** medium. First-real-outage failure mode.

### 11.3 Story C — Corrupt intent file on startup

**Setup.** Previous daemon crashed mid-fsync. Intent file exists at `<key>.json` but is 0 bytes or has truncated JSON.

**What the adapter does.** `json.Unmarshal` returns an error. The spec doesn't say what to do. Implementer defaults:

- Delete the file and skip (data loss; BI-INV-004 violated)
- Leave the file and emit `store_divergence_detected`
- Escalate as Cat 6a integrity-violation

**Spec gap.** Entirely unhandled. Even though BI-030a (proposed) would prevent this by using tempfile-rename, crash-right-after-rename-before-dir-fsync could still leave a partial entry visible.

**Recommendation.** Add to BI-031b or new BI-031c: "An intent file that fails schema parse MUST classify as `BrSchemaMismatch`-equivalent at the intent-log level; emit `store_divergence_detected{divergence_kind=\"intent_log_corrupt\"}` and leave the file on disk for operator investigation. The adapter MUST NOT delete a corrupt intent file autonomously." The operator wrangles it manually; this is a true integrity violation (Cat 6a territory).

**Severity:** medium. Rare but nasty when it happens.

### 11.4 Story D — `br` hangs indefinitely on startup handshake

**Setup.** `br --version` hangs (maybe Beads is stuck in startup migration). My adapter's BI-024a handshake waits.

**What the spec says.** BI-025c: 5s read timeout. Handshake inherits this. Timeout → `BrUnavailable` → per §8 routes to Cat 0 → per BI-024a "fail daemon startup with exit code 8 ... emit `daemon_startup_failed{failure_mode=\"br-version-incompatible\"}`."

Wait — `br-version-incompatible` is the failure mode name, but the actual cause is `br-version-timeout`. The `failure_mode` enumeration in BI-024a is single-valued ("br-version-incompatible") but the actual failure is "handshake timeout, not incompatibility." Subtle bug in BI-024a wording.

**Recommendation.** Expand BI-024a's failure-mode enum: `{br-version-incompatible, br-version-unparseable, br-version-timeout, br-not-on-path}`. Map timeouts and exec failures to the appropriate mode.

**Severity:** low. Tag-level taxonomy issue.

### 11.5 Story E — Reissue loop on persistent Beads conflict

**Setup.** BI-031 step 4 reissues `br claim bd-7` with key K; Beads returns `BrConflict` because another claim already holds the bead. Adapter is confused.

**Per probe 3's identified gap:** BI-031 has no reissue-failure branch. Without BI-031c (recommended), the adapter implementer picks a default: delete the intent file (data loss, violates BI-INV-004) or leave it and infinite-loop (daemon wedges on next startup).

**This is the probe 3 gap instantiated as a concrete failure story.** Severity: high. Spec must close BI-031's error-path branch.

---

## 12. Affirmations — adapter-friendly decisions that should NOT be reopened

1. **§6.1a `BrError` enum with explicit mapping table (lines 470–481).** Concrete, implementable, typed. Implementer falls out of this without inventing an error model.

2. **§4.8a BI-025b JSON-mode mandatory + "text parsing forbidden."** This is exactly right. Parsing `br` text output would force the adapter to invent grammars and break on every Beads UX tweak. Single decision, massive scope narrowing.

3. **BI-031's reframe to Beads-idempotency-independent status-check.** Removes the spec's dependency on Beads persisting the idempotency key (which Beads may or may not do). The three-way branch (pre/post/divergence) is clean and mechanically implementable.

4. **§4.10's directory-as-intent-log pattern.** Simple, debuggable, crash-recoverable. An operator can `ls .harmonik/beads-intents/` and see exactly the set of ambiguous writes. No hidden state. Beats SQLite-intent-store or single-append-log alternatives for MVH.

5. **BI-INV-001 "no intra-run writes."** Coarseness is correct per the `blocked_issues_cache` rationale in §A.3. Keeps Beads as a coarse-grained coordinator, not a per-node event sink. Don't let post-MVH creep erode this.

6. **BI-012's "single adapter module" rule.** The compiler-enforceable "only one file in the tree contains `exec.Cmd{Path: \"br\"}\"` is a 10-line test that prevents scattered `br` invocations forever.

7. **§4.8a BI-025c subprocess timeout discipline with explicit 5s-read / 10s-write defaults.** Concrete numbers beat "operator-tunable" only. An adapter author can write the function without inventing defaults.

8. **§6.1 `IntentLogEntry` schema is concrete and N-1 evolvable.** Schema versioning is named (§6.3); additive changes are non-breaking; implementer knows the rules.

---

## 13. Cross-spec coordination from the adapter-author seat

Items where my adapter implementation depends on a sibling spec that hasn't coordinated:

1. **BI-031 vs RC §8.4a detector rule.** RC §8.4a still references "an entry matching the idempotency key" as detection evidence, but BI-031 no longer requires Beads to persist the key. Either RC needs to rephrase to match BI-031's status-based logic, or BI has to re-commit to Beads persisting the key. Flagged in probe 3 and probe 5. Coordination: BI R3 ↔ RC R3.

2. **Adapter recovery trigger: adapter-driven (BI-031 wording) vs reconciliation-driven (PL-006 + RC §8.4a).** Per §9 of this review, the trigger path is ambiguous. Coordination: BI R3 clarifies by rephrasing BI-031's intro, and reconciliation's verdict-executor contract (RC-025) should document that intent-file recovery routes through `Adapter.RecoverIntent()`. BI R3 ↔ RC R3.

3. **Post-ready Cat 0 behavior for mid-lifetime Beads schema drift.** Scenario 11.1 identified that `BrSchemaMismatch` returned mid-lifetime has no defined caller contract. OQ-PL-009 tracks post-ready degradation scope; BI should cross-reference.

4. **Adapter structured-log subsystem identifier.** Per §7.3, the adapter should register as `beads-adapter` per EV-034a. EV's `source_subsystem` registry should include this. Coordination: EV R3 adds `beads-adapter` to the registered subsystem list.

5. **Operator exit-code mapping for `BrSchemaMismatch` at runtime (not startup).** ON §8's exit-code 8 `beads-unavailable` covers startup. Runtime-discovered schema drift may need a distinct exit code or routing. Coordination: ON R3 clarifies runtime behavior.

6. **Backpressure under prolonged Beads-unavailable.** Story 11.2 identified no backpressure rule. Coordination: BI R3 + PL R3 + ON R3 — decides whether backpressure is BI's adapter-internal concern or a daemon-level Cat 0 mode.

---

## 14. Recommendation (ordered by severity)

**Blocking for adapter-author to build without invention:**

1. **Close BI-031's reissue-failure branch.** Add BI-031c covering `BrConflict`, `BrDbLocked`, `BrUnavailable`, `BrOther` / `BrNotFound` / `BrSchemaMismatch` on reissue. (Probe 3; story 11.5.)

2. **Define the compatibility window mechanically.** Add BI-024b for exact-match-at-MVH, with OQ-BI-008 for post-MVH widening. (Probe 2.)

3. **Resolve BI-031 ↔ RC §8.4a inconsistency.** Either BI-031 re-commits to Beads persisting the key, or RC §8.4a drops the key-based detection and uses post-state-based. (Probes 3 + 4; cross-spec item 1.)

4. **Clarify the intent-file recovery trigger.** BI-031 wording suggests adapter-driven; PL+RC expect reconciliation-driven. Pick one and rewrite BI-031's intro. (Section 9; cross-spec item 2.)

**Strongly recommended for adapter-author quality:**

5. **Add BI-030a — intent-file atomicity protocol.** Tempfile + rename + directory fsync. (Section 6.)

6. **Bound stderr discipline.** Byte cap, warn-on-exit-0 policy, partial-on-timeout behavior. New BI-025e. (Probe 1.)

7. **Pin concurrent-subprocess policy.** New BI-025f/e. `BrDbLocked` retry with bounded backoff. (Section 7.2.)

8. **Add adapter structured-logging clause.** Invocation/retry/error logs via ON-035 with `beads-adapter` subsystem identifier. (Section 7.3.)

9. **Pin argv-mismatch pre-validation at startup.** New BI-025f. Prevents silent `BrConflict` false-positives on unsupported flags. (Probe 1 gap (a).)

10. **Close OQ-BI-003 at MVH (colon encoding).** Prescribe `_` always. (Section 6.)

**Nice-to-have for implementability:**

11. **Name the `br` binary injection point.** Config field for binary path; interface for CLI dependency in unit tests. (Section 8.2.)

12. **Bound Rust-panic exit code 101 in §6.1a table.** (Probe 1 gap (b).)

13. **Expand BI-024a failure-mode enum.** `{br-version-incompatible, br-version-unparseable, br-version-timeout, br-not-on-path}`. (Story 11.4.)

14. **Add §7 protocol pseudocode for the terminal-transition write dance.** Makes the adapter's state machine concrete. (R1 implementer recommendation, still relevant.)

15. **Add backpressure clause.** New BI-025g for prolonged Beads-unavailable. (Story 11.2.)

**Final word.** The R1 integration closed most of the catastrophic holes; what remains is second-order concreteness. If items 1–4 land in R3, I can build the adapter. Items 5–10 save me from inventing contracts that someone will later have to audit. Items 11–15 are polish. The core architecture — `br`-only access, terminal-transition-only writes, intent-log-with-status-check idempotency, adapter-as-single-module — is sound and should not be reopened.
