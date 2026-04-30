# Round 1 Critic Review — process-lifecycle.md v0.2.0

## Verdict summary

The spec is structurally on-thesis and gets the big calls right: per-project daemon, ntm-as-adapter-not-dependency, deterministic Go daemon with no LLM, orphan-sweep-before-classification, named obligations (upgrade, silent-hang) held at the boundaries. The load-bearing softness is downstream of that frame. Five findings dominate:

1. **Cross-reference corruption is systemic.** The spec's §4.* layout is authoritative, but every cite to `operator-nfr.md` uses `§7.*` (which resolves to operator-nfr's state-machine table, not the requirement bodies), every cite to `beads-integration.md` uses `§10.*` (which resolves to operator-nfr-style conformance, not the write/skill surfaces), every cite to `reconciliation.md §9.*` points at Cross-references rather than taxonomy or detectors, and dependent specs all back-cite `process-lifecycle.md §8.*` — which is empty. The v0.2 cleanup note claims to have migrated legacy architecture.md anchors; it did not fix the operator-nfr / reconciliation / beads-integration citations, and never checked the reverse direction. This is not typo-level — it breaks RC-012's "transition to `degraded` per [process-lifecycle.md §8.2]" because §8.2 does not exist.
2. **"Ready-state" predicate silently excludes reconciliation-dispatch failure.** PL-009 names five conditions for `ready`, one of which is "reconciliation dispatch has completed its synchronous action-mapping." What happens when a synchronous auto-resolver (Cat 3a, Cat 3b, Cat 3c, Cat 4) fails? The spec provides no predicate. If the auto-resolver raises, is the daemon permanently blocked from `ready`, does it route the run to degraded, or does it proceed with a partial classification? This is the first-day implementer-blocking question.
3. **`degraded` is a side state for Cat 0 only; no provision for post-`ready` degradation.** PL-010 enters `degraded` exclusively on Cat 0 pre-check failure. But RTO breach per ON-032 emits `daemon_degraded{reason=rto_breach}`, watcher-wedge per HC-011 is observable via health-check, and silent-hangs (PL-017) route to reconciliation. The spec's status enum has `degraded` as pre-`ready` only. Either rename to `infrastructure_unavailable` to reflect the narrow scope, or widen to cover post-ready degradation — currently it leaks through event-model §8.7.5's unconstrained `reason` enum.
4. **Orphan sweep is load-bearing for reconciliation correctness but its completeness predicate is not mechanically testable.** PL-007 asserts "no harmonik-owned process from a prior daemon instance is alive." The test surface for that predicate is PID-namespace-scoped and inherently racy on a shared machine (a newly-launched user process could legitimately match the handler-binary pattern). PL-INV-003 elevates this to a spec-wide invariant; the detection rule is incomplete enough to produce false positives (innocent reparented processes killed) or false negatives (handler rebranded mid-flight, process not matched).
5. **ntm-absence is never named as a failure mode.** §4.7 treats the ntm adapter as the spawning path but never says what happens if ntm is absent, incompatible, or not on PATH. MVH probably requires it; the spec doesn't say so, and OQ-PL-003 (the `.harmonik/` directory question) is raised while this much more load-bearing question is silent.

**Verdict: proceed with revisions.** None of these findings requires architectural re-work, but finding #1 (the xref corruption) is a blocker for finalize — every downstream spec's re-cite will need edits too. Findings #2, #3, #4 require concrete requirement additions; finding #5 needs an Open Question or one sentence in §4.7. Rework budget is ~50–100 lines of spec edit plus cross-spec cleanup.

The spec's scope discipline (§2.2) is exemplary — every out-of-scope item names the owner and is honest. The obligation-naming pattern (§4.6 daemon-vs-orchestrator; §4.9 upgrade; §PL-017 silent-hang) is a model for other specs. The crash-semantics triple (PL-024 daemon crash, PL-025 crash-during-startup-reconciliation, PL-026 agent-subprocess crash) is the sharpest part of the draft.

## Challenges (7 load-bearing items)

### Challenge 1 — Cross-reference anchors are systematically wrong

- **Challenge** — Every `[operator-nfr.md §7.N]`, `[beads-integration.md §10.N]`, and `[reconciliation.md §9.N]` in this spec points to a section that is either empty, non-existent, or semantically mismatched. Dependent specs then back-cite `[process-lifecycle.md §8.N]` — which is also empty. The spec cannot finalize in this state.

- **What the spec says** — Line 44: "Named obligation for the `harmonik upgrade` contract (owned by [operator-nfr.md §7.5])." Line 82: "exit with a specific error code … per [operator-nfr.md §7.1]." Line 177: "bounded by the operator-configurable drain timeout per [operator-nfr.md §7.7]." Lines 212, 225, 290 cite `[reconciliation.md §9.2]`, `§9.3`. Lines 212, 322 cite `[beads-integration.md §10.8]`, `§10.9`. §9.1 Depends-on (lines 397–420) is entirely built on these wrong anchors. Every one of these resolves to a section that does not own the referenced content:
  - operator-nfr.md §7.1 is the state-machine *table*, not exit codes (those are §4.1 / ON-001–004 and §8).
  - operator-nfr.md §7.3 is the *drain-protocol pseudocode*, not operator-control state semantics (those are §4.3 / ON-007–014).
  - operator-nfr.md §7.5, §7.7, §7.8, §7.10 do not exist (§7 has §7.1, §7.2, §7.3 only).
  - reconciliation.md §9 is Cross-references; the detectors are §4.3 and the taxonomy is §8.1–§8.11a.
  - beads-integration.md §10 is Conformance; the intent log is §4.10 and the Beads-CLI skill is §4.9.
  - Dependent specs cite `[process-lifecycle.md §8.1]` (reconciliation line 27, 75, 185, 336, 496, 685; event-model line 918, 823), `§8.2`, `§8.3`, `§8.4`, `§8.6` — but this spec's §8 is "This spec does not own a failure taxonomy" (one paragraph).

- **Is the justification adequate?** — no. The v0.2 revision history (line 490) describes migrating legacy `architecture.md` anchors from §1.N to §4.N; the migrator did not extend the same discipline to operator-nfr, beads-integration, or reconciliation, and no one checked that dependent specs' back-pointers still resolved. Separately, the Depends-on block (lines 397–420) is a copy-paste of the wrong anchors and never compiled against the actual target specs.

- **Stronger alternative** — A one-pass fix across this spec and its dependents:
  - Rewrite every `operator-nfr.md §7.N` → the correct §4.N anchor OR the specific requirement ID (ON-NNN). Example: `[operator-nfr.md §7.1]` for exit-code taxonomy → `[operator-nfr.md §4.1 ON-001]` or `[operator-nfr.md §8]` (the exit-code table). `§7.5` → `[operator-nfr.md §4.6]` (upgrade contract). `§7.7` → `[operator-nfr.md §4.7 ON-027]` (graceful-shutdown ordering). `§7.8` → `[operator-nfr.md §4.8 ON-031]`.
  - Rewrite every `beads-integration.md §10.N` → `§4.N`. `§10.8` (intent log) → `§4.10 BI-041-ish` (or a specific BI requirement). `§10.9` (Beads-CLI skill) → `§4.9 BI-036`.
  - Rewrite every `reconciliation.md §9.N` → either `§4.N` (for normative requirements) or `§8.N` (for categories). `§9.1a` → `§4.1 RC-002`. `§9.2` → `§8.12` (action-mapping) or `§4.2` (dispatch). `§9.2a` → `§4.2 RC-008`. `§9.3` → `§4.3 RC-012` (Cat 0 pre-check) or `§8.1` (Cat 0 taxonomy entry). `§9.5b` → `§4.5 RC-025`.
  - Update every dependent spec's `[process-lifecycle.md §8.N]` back-pointer to the actual section (§4.1, §4.2, §4.3, §4.4, §6.1). This requires edits in reconciliation/spec.md (6 occurrences), event-model.md (2 occurrences), operator-nfr.md (6 occurrences), plus a grep-sweep of the corpus for completeness.
  - Add a footnote-style footer to §12 Revision history noting the xref sweep, and a Conformance-checklist lint obligation ("every cross-reference resolves to an existing section heading").

- **How load-bearing** — blocking. The spec cannot be finalized with broken cross-references. Every reviewer who follows a link hits a dead section. Every downstream spec that back-cites PL needs to be updated in lockstep.

### Challenge 2 — `ready` predicate is silent on reconciliation-dispatch failure

- **Challenge** — PL-009 lists five conditions for `ready` but defines no behavior when the "reconciliation dispatch (§PL-005 step 7) has completed its synchronous action-mapping" condition fails for a specific run.

- **What the spec says** — PL-009 (lines 149–156): "The daemon MUST transition status to `ready` only when ALL of the following conditions hold: … Reconciliation dispatch (§PL-005 step 7) has completed its synchronous action-mapping for every in-flight run; dispatched investigator workflows MAY remain in-flight and MUST NOT block the `ready` transition." Then PL-010 handles only Cat 0 failures via `degraded`. Nothing handles Cat 1 auto-resolver crash, Cat 3a adapter re-issue failure, Cat 3c close-write failure, or Cat 4 retry-dispatch failure. Reconciliation/spec.md §4.2 RC-009 auto-resolver protocol says these MUST be a deterministic implementation, but does not define what happens if the implementation errors.

- **Is the justification adequate?** — no. "Completed its synchronous action-mapping" is an event, not a predicate. What distinguishes "action-mapping errored but we continue" from "action-mapping cannot proceed without human intervention"? The spec assumes action-mapping cannot fail; this is wrong (a Cat 3a adapter re-issue can hit `br` errors per reconciliation §8.4a, a Cat 3c close-write can hit the same). If such a failure leaves the run unclassified, the daemon reaching `ready` with an uncategorized in-flight run violates PL-INV-003 in spirit (the invariant is "orphan sweep completes before classification"; it should extend to "every in-flight run is classified before `ready`").

- **Stronger alternative** — Add a requirement PL-009a:
  > **PL-009a — Auto-resolver failure during startup dispatch.** If a synchronous action-mapping auto-resolver (Cat 1, Cat 3a, Cat 3b, Cat 3c, Cat 4 per [reconciliation.md §8.12]) fails during §PL-005 step 7, the daemon MUST:
  > (a) emit `reconciliation_category_assigned` with the original category per [reconciliation.md §4.3 RC-013];
  > (b) re-classify the run into Cat 3 generic per [reconciliation.md §8.4];
  > (c) dispatch an investigator workflow per [reconciliation.md §4.2 RC-008];
  > (d) permit the daemon to proceed to `ready` with the investigator workflow in-flight (per PL-009).
  > The daemon MUST NOT block on `ready` due to auto-resolver failure, but it MUST NOT leave an in-flight run unclassified at `ready` emission.

  And amend PL-009 to add the condition: "Every in-flight run has received a category assignment emission per [reconciliation.md §4.3 RC-013]." Currently the predicate is "synchronous action-mapping completed," which is under-specified.

- **How load-bearing** — blocking. The first real-world adapter-failure bug will produce a daemon stuck between `reconciling` and `ready` with no rule for what to do.

### Challenge 3 — `degraded` is pre-`ready` only; post-ready degradation has no state

- **Challenge** — §6.1 defines `degraded` as "Cat 0 prerequisite failing; classification halted" — a pre-`ready` side state only. But dependent specs emit `daemon_degraded` with `reason ∈ {rto_breach, reconstruction_notify, other}` (event-model §8.7.5), and `degraded` health-check status is cited from `post-ready` surfaces (ON-036, ON-037 liveness, HC-INV-001 watcher-invariant). This spec owns the status enum but treats `degraded` as a one-way entry gate.

- **What the spec says** — §6.1 (lines 344–353), the enum entry `degraded -- Cat 0 prerequisite failing; classification halted`. PL-010 (line 162): "When the Cat 0 pre-check (§PL-005 step 3) fails, the daemon MUST transition to `degraded` status and remain there until all prerequisites clear." The state-machine table (line 380) shows only `starting` → `degraded` and `degraded` → `reconciling`. No `ready` → `degraded` transition is specified.

- **Is the justification adequate?** — partial. The spec's scope carve (§2.2: "operator command semantics beyond daemon state-prefix is owned by operator-nfr.md") implies post-ready degradation belongs to operator-nfr, but operator-nfr doesn't own the daemon-status enum — this spec does (§6.1). Event-model.md §8.7.5 emits `daemon_degraded` with RTO-breach and reconstruction-notify reasons — both of which occur post-`ready` (RTO breach is measured to-`ready`; reconstruction-notify is a post-ready observability signal per ON-032 criterion 3).

- **Stronger alternative** — Two options, both acceptable:
  - (a) Rename `degraded` to `infrastructure-unavailable` (matching the event name) and explicitly declare it pre-`ready`-only. Then add a note: "Post-`ready` degradation is a health-check-surface concern owned by [operator-nfr.md §4.9 ON-036], not a state transition of this enum."
  - (b) Widen `degraded` to a general-purpose reentrant state: add `ready` → `degraded` transition with Guard = "subsystem health aggregation returns `failed`" (per ON-036) and Emits = `daemon_degraded{reason=<from health surface>}`. Then specify the return path: `degraded` → `ready` on health recovery. This aligns with how event-model.md already uses `daemon_degraded` and resolves the "what status is the daemon in during a silent-hang?" ambiguity.
  - Either way, line 349 should be rewritten. Currently the enum comment says "Cat 0 prerequisite failing" — either make that literally the only trigger (option a) or remove the over-specific comment (option b).
  - Add an `OQ-PL-NNN` if the decision is deferred: "Post-`ready` degradation: infrastructure-prereq-only vs general-purpose-degraded."

- **How load-bearing** — important. This creates a mismatch between the spec's enum and the event-payload enum in event-model.md §8.7.5 — consumers of the daemon's status surface (`harmonik status`, the attach UI) will observe `degraded` with no clear rule for which subset of causes triggered it.

### Challenge 4 — Orphan sweep's completeness predicate is not mechanically testable

- **Challenge** — PL-007 declares "no harmonik-owned process from a prior daemon instance is alive" after the sweep. The detection rule in PL-006 matches on "processes re-parented to init whose binary path matches a handler binary under the project's expected launch path" — neither condition is reliably decidable in the presence of: (a) handler binaries named identically but launched by a different harmonik project or by a human, (b) handler binaries at different paths post-upgrade, (c) PID collisions on long-running machines, (d) users with multiple shells where handler-like processes are legitimately running.

- **What the spec says** — PL-006 third bullet (line 126): "The daemon MUST identify processes that have been re-parented to init (parent pid 1) whose binary path matches a handler binary under the project's expected launch path, and kill them via SIGTERM followed by SIGKILL with a bounded interval between them." PL-007 (line 135): "The orphan sweep MUST be deterministic given the filesystem + process state. After the sweep completes, no harmonik-owned process from a prior daemon instance is alive…"

- **Is the justification adequate?** — no. The detection rule is under-specified; "handler binary under the project's expected launch path" is ambiguous (what IS the project's expected launch path? a configured path? `$HARMONIK_HOME/handlers/`? whatever the last launch used?). The spec does not reserve a cookie/marker (e.g., a PGID, an environment variable, a file in `/proc/<pid>/cwd/.harmonik-project-id`) to disambiguate "our orphan" from "another harmonik instance's live process" from "user's unrelated process." On a multi-project machine with per-project daemons (explicitly supported per PL-001), two daemons doing orphan sweep on their own start-up will race: daemon-B's sweep on project-B startup could kill daemon-A's live handler in project-A if the handler binary lives at a shared path.

- **Stronger alternative** — Tighten PL-006 with a mandatory provenance marker:
  > **PL-006 amendment — Orphan identification requires a project-scoped provenance marker.** The daemon MUST launch every handler subprocess with a per-project provenance marker: either (a) a process-group (PGID) set to a deterministic value derived from the pidfile's project path, (b) an environment variable `HARMONIK_PROJECT_HASH=<hash>` readable via `/proc/<pid>/environ` on Linux (or equivalent), or (c) a file at a deterministic path (`/tmp/harmonik-<project-hash>-<pid>.lock`) whose existence declares the pid's project. The orphan sweep MUST identify candidates by matching this marker against the current project hash, NOT by binary path. Candidates without a valid project-scoped marker MUST NOT be killed. On darwin (no `/proc`), candidate (c) or macOS-specific `proc_pidinfo` is normative; foundation does not pick one here — OQ-PL-NNN defers.

  And tighten PL-007 to require the marker-based detection:
  > **PL-007 amendment.** The orphan sweep is deterministic given the filesystem + process state AND the per-project provenance marker. The sweep MUST NOT kill a process lacking a valid project-scoped marker, and MUST NOT match on binary path alone.

  Open Question OQ-PL-NNN: "macOS provenance-marker mechanism (no `/proc` available)."

- **How load-bearing** — blocking. Multi-project scenarios are declared supported (PL-001); an orphan sweep that cross-kills is a data-loss hazard and breaks PL-INV-001's "one daemon per project" independence.

### Challenge 5 — ntm absence / fallback is undeclared

- **Challenge** — §4.7 treats the ntm adapter as the spawning path but nowhere names what happens if ntm is not installed, not on PATH, or incompatible. MVH almost certainly requires ntm for handler subprocess management in tmux panes (per the command surface PL-028 `harmonik runner` and locked decision #4 inspectability); post-MVH may replace ntm. The spec is silent on detection, fallback, and versioning.

- **What the spec says** — §3 Glossary (line 67): "**ntm adapter** — the thin layer that consumes ntm's process/tmux capabilities…" PL-014 (line 205): "spawn agent processes as children of the daemon process (via the ntm adapter or equivalent — see §PL-020)." PL-021 (line 256): "the ntm adapter layer MUST consume only the following ntm capabilities: (a) agent process spawning in a tmux pane…"

- **Is the justification adequate?** — no. "ntm adapter or equivalent" is hand-waved; what is "equivalent"? The spec never names version-pinning, absence-detection, or error-code routing for a missing-ntm case. Given that PL-008 obligates a startup failure-mode catalog for every prerequisite, ntm absence is one — but §4.7 does not cite it. `harmonik runner` (PL-028) literally requires opening tmux sessions; its behavior when ntm/tmux is absent is undefined.

- **Stronger alternative** — Two requirements:
  > **PL-021a — ntm version pin.** The daemon MUST version-pin ntm per the external-inputs protocol (see [operator-nfr.md §4.4 ON-017] for the Beads-parallel pattern). Supported ntm versions MUST be declared in the release manifest. An ntm version outside the supported set MUST produce a specific startup exit code (per [operator-nfr.md §8] ntm-unsupported entry to be added) and MUST be classified as a Cat 0 prerequisite failure per [reconciliation.md §8.1].
  >
  > **PL-021b — ntm absence is a startup prerequisite failure.** `ntm` unavailable (not on PATH, fails version probe, tmux absent on Linux/darwin) MUST be detected during §PL-005 step 3 Cat 0 pre-check and MUST produce `infrastructure_unavailable{failed_prerequisite=ntm_unavailable}` per [event-model.md §8.7.15]. The daemon MUST NOT attempt to spawn handler subprocesses without a working ntm adapter.

  And amend PL-028 `harmonik runner` (line 311): if ntm is absent, `harmonik runner` MUST exit with the ntm-unavailable exit code rather than silently degrading to non-tmux mode. The solo-dev ergonomics carve-out depends on tmux inspectability (locked decision #4); silently dropping tmux would violate that decision.

  Add OQ-PL-NNN: "Post-MVH replacement of ntm with a harmonik-native tmux-management layer — decision framing and swap rules."

- **How load-bearing** — important. Silently depending on a tool that might be absent, especially one that implements locked-decision #4, is how MVH releases ship with surprising dependency errors.

### Challenge 6 — `harmonik upgrade` mechanics obligation is named but co-ownership boundary is blurry

- **Challenge** — PL-027 declares the upgrade contract "owned by [operator-nfr.md §7.5]" (wrong anchor — see Challenge 1; correct is §4.6). But operator-nfr §4.6 ON-020 then declares five sub-obligations. Three of these sub-obligations — socket/client-CLI retry, exec-replacement mechanics, schema-version-on-disk check — are fundamentally daemon-internal, not operator-facing. Owning them in operator-nfr is a category error; they belong here in process-lifecycle, which is the daemon-mechanics spec.

- **What the spec says** — PL-027 (line 299): "This spec NAMES the `harmonik upgrade` contract obligation. The contract itself is owned by [operator-nfr.md §7.5] … and MUST specify: (a) binary-source mechanism, (b) operator-supplied expected-commit-hash check procedure, (c) drain-vs-reconciliation interaction, (d) cross-version state contract, (e) socket/client-CLI retry behavior during exec-replacement." Operator-nfr.md ON-020 also lists all five identically.

- **Is the justification adequate?** — partial. The operator-facing parts (binary-source flag, hash check, drain interaction) reasonably live in operator-nfr. But (e) socket/client-CLI retry and the mechanical shape of exec-replacement are daemon-internal — they are "how does the daemon preserve socket identity across exec?" which is unambiguously PL's domain (PL-003 owns the socket). Operator-nfr is not the spec that knows "same socket path after exec-replace."

- **Stronger alternative** — Split the sub-obligations:
  > **PL-027 restated.** This spec owns the daemon-internal mechanics of `harmonik upgrade`:
  > (i) **Exec-replacement semantics**: the new daemon binary MUST replace the old via `execve` (or platform-equivalent), preserving the socket file and the pidfile lock (the new process MUST take the lock immediately on startup).
  > (ii) **Socket continuity**: the new daemon MUST re-bind `.harmonik/daemon.sock` within a bounded window T_rebind (default: 2s) after exec-replacement. Clients experiencing socket-EOF during this window MUST retry the connection on the same path.
  > (iii) **Intermediate daemon state**: between exec and re-bind, the daemon has no status; `harmonik status` MUST report "upgrading" by filesystem signal (a marker file `.harmonik/daemon.upgrading`) rather than querying the socket.
  > (iv) Operator-facing semantics — binary-source mechanism, hash-check procedure, drain interaction, cross-version compat — owned by [operator-nfr.md §4.6 ON-020].

  Then operator-nfr's ON-020 should drop sub-obligations (a)(b)(c)(d) as the authoritative slice and reference PL-027's (i)(ii)(iii) for daemon-internal mechanics.

- **How load-bearing** — important. The "named obligation" shape is correct in principle, but the assignment-to-owner is wrong in specifics: the owner of the daemon socket is this spec, not operator-nfr. Operator-nfr should not normatively constrain socket behavior.

### Challenge 7 — Command surface (PL-028) is under-specified: `harmonik runner` is a semantic mode, not sugar

- **Challenge** — PL-028 calls `harmonik runner` "sugar on top of `daemon` + `attach`, NOT a distinct execution mode." Except it IS a distinct mode — it additionally spawns tmux sessions, coordinates multi-pane layouts, and "optionally spawns an orchestrator-agent session" (which is a whole separate process tree, not a tmux decoration). Treating it as sugar under-specifies it.

- **What the spec says** — PL-028 `harmonik runner` entry (line 311): "convenience wrapper for solo-dev ergonomics: starts the daemon (if not running), opens a tmux session showing all agent processes (per the ntm inspectability requirement — locked decision #4), and optionally spawns an orchestrator-agent session. `runner` is sugar on top of `daemon` + `attach`, NOT a distinct execution mode."

- **Is the justification adequate?** — no. Claiming "sugar on top of daemon + attach" while also saying it "optionally spawns an orchestrator-agent session" is contradictory. The orchestrator-agent is explicitly a separate Claude Code process per PL-019; spawning it is a distinct lifecycle concern with its own error surface ("what if Claude Code isn't installed?" "what if the orchestrator-agent session fails to start — does the daemon stay up or die?"). An "optional" spawn with no specified behavior on failure is hand-waved.

- **Stronger alternative** — Rewrite PL-028's runner entry:
  > **`harmonik runner`** — solo-dev convenience command. Executes the following in order:
  > (1) if no daemon is running for the project, start the daemon (equivalent to `harmonik daemon &`);
  > (2) wait for `daemon_ready` per §PL-009 (bounded timeout per OQ-PL-002 defaults);
  > (3) open a tmux session under the project's harmonik naming convention (prefix `harmonik-<project-hash>-`) with one pane for the daemon's log output and N panes per active handler session (per ntm inspectability, locked decision #4);
  > (4) if the `--orchestrator-agent` flag is supplied, spawn a Claude Code session in a separate tmux pane with the orchestrator-agent prompt and CLI access per §PL-019. On Claude Code unavailable, exit with a specific error code (orchestrator-agent-unavailable per [operator-nfr.md §8]).
  >
  > `harmonik runner` is a distinct entry point with its own exit-code surface; it is NOT a shell alias for `daemon` + `attach`.

  This also interacts with Challenge 5 (ntm absence): step (3) requires ntm/tmux to work or must error cleanly.

- **How load-bearing** — important. `harmonik runner` is the solo-dev primary entry point per locked decision #4 and problem-space framing. Under-specifying its error surface means the first day of solo-dev usage hits undefined behavior.

## Scope leaks

Requirements that violate §2.2's declared scope boundaries.

1. **PL-028 describes `harmonik status` behavior** (line 312): "`harmonik status` MUST report the current `degraded` state (Cat 0) per §PL-010." But §2.2 says "Operator command semantics (pause/stop/upgrade behavior beyond daemon state-prefix) — owned by [operator-nfr.md §7.3]." Reporting `degraded` is a status-command semantic. Either this is a scope leak, or the scope carve should read "Operator command semantics BEYOND harmonik status's status-reporting behavior is owned by operator-nfr." Currently the boundary is unclear.
  - **Fix**: tighten §2.2 carve: "Operator command semantics for pause / stop / upgrade — owned by [operator-nfr.md §4.3, §4.6, §4.7]. `harmonik status`'s reporting of daemon-status-enum values is owned by this spec (§6.1); reporting of semantic content beyond status-enum is owned by operator-nfr."

2. **PL-006 "stale intent files" detection routes to Cat 3a per reconciliation** (line 127): "each stale entry triggers a Cat 3a detector invocation per [reconciliation.md §9.3]." But §2.2 says "Reconciliation classification, category taxonomy, investigator contract, and verdict vocabulary — owned by [reconciliation.md §9.2, §9.3, §9.4, §9.5]." PL-006 is invoking a reconciliation detector — which is a boundary cross. This may be correct in effect (the orphan sweep discovers stale intents; reconciliation classifies them), but the spec conflates "detect an orphan" with "invoke a detector." The mechanism should be decoupled:
  - **Fix**: PL-006 "stale intent files" bullet: "The daemon MUST enumerate `.harmonik/beads-intents/` for entries older than the current daemon's start time; stale entries MUST be preserved in the filesystem for classification by the Cat 3a detector per [reconciliation.md §4.3 RC-014-ish] on its normal pass (§PL-005 step 7)." The orphan sweep clears live processes and locks, not stale intent files; those persist and are classified by reconciliation normally. This eliminates the boundary cross.

3. **PL-008 obligates content of the startup failure-mode catalog** (line 141): "The catalog MUST enumerate every prerequisite failure (git bad state, Beads SQLite state, schema-version mismatch, stale-pidfile race, filesystem-unwritable, disk-full during checkpoint commit)." But §2.2 carves "Startup failure-mode catalog content (exit codes per prerequisite failure) — owned by [operator-nfr.md §7.1] spec-draft obligation." PL-008 is leaking into the catalog's content.
  - **Fix**: PL-008 should state only: "This spec DEPENDS on the normative startup failure-mode catalog produced by [operator-nfr.md §4.1 ON-003]. The Cat 0 pre-check (§PL-005 step 3) consumes the catalog; contents are owned by operator-nfr." Drop the parenthetical list. Or: leave the parenthetical but mark it `> INFORMATIVE: These are expected entries; authoritative list in operator-nfr.md.`

## First-plausible-answer findings

Requirements where the author picked the first answer without naming the tradeoff.

1. **PL-006 "≤2 seconds" tmux-kill wait.** The bound is given as 2 seconds without justification. Why 2s and not 500ms or 10s? OQ-PL-002 defers "bounded timeouts for orphan-sweep sub-steps" but then the requirement has a hard 2s in the normative text. Either move the 2s into the OQ default-if-unresolved, or name the tradeoff: "2s is chosen because [tmux kill propagation latency] + [OS reaping budget] fits under the p95 RTO of 30s (ON-032) with 28s headroom."

2. **PL-013 daemon does not exit on queue-empty, re-queried at configurable cadence.** Why not exit and get re-started by a supervisor? The locked decision (#12 no DTW, centralized controller) implies "the daemon is always there," but the spec doesn't name the tradeoff. A stated rationale: "Exiting on queue-empty would make the pidfile-lock handoff load-bearing for every enqueue; the re-query cadence trades background idle CPU for no-lock-churn. Configuration knob defers the exact cadence." Currently the requirement reads as if exit-on-empty wasn't considered.

3. **PL-015 socket-only communication, no alternate transports.** Why Unix socket and not (a) shared memory, (b) named pipes, (c) loopback TCP? The decision is probably right (Unix socket is the simplest and has proven fsync-timing characteristics), but the rationale should be named — "Unix socket is chosen because (a) it supports local bidirectional byte streams with OS-level delivery guarantees, (b) socket path is a natural per-project identifier matching the pidfile discipline, (c) tcp-loopback adds firewall and port-allocation surface, shared-memory adds OS-version-specific edge cases."

4. **PL-028 attach detaching MUST NOT kill daemon.** Correct, but the converse isn't stated: "Multiple simultaneous attaches MUST be supported." What's the tradeoff — is there an attach-count limit? Is there priority? Locking (OQ-ON-004 raises concurrent-operator-attach arbitration in operator-nfr). The requirement assumes unlimited concurrent attaches; that assumption deserves to be named or OQ'd.

5. **§6.1 daemon-status enum is an ordinary untagged enum.** No schema version, no reserved values for unknown, no extensibility rule (§6.3 hand-waves "additions are backward-compatible"). For a cross-subsystem enum consumed by event payloads (§8.7) and operator-nfr's state machine (ON-011 adds `pausing`, `paused`, `resuming`, etc.), an extensibility rule is load-bearing. The first answer is "oh we'll just add new values later" — but then consumers have to handle unknown values defensively, and the rule for that is not written.

## Invariant audit (per §5 selection test)

Template §5 test: "If the rule fits inside one subsystem's §4 without reference to others, it is a requirement, not an invariant."

- **PL-INV-001 — One daemon per project (lines 320–324).** Claims to span operator-nfr, beads-integration, workspace-model. Genuinely cross-subsystem — these consumers really do assume a single-writer daemon. **HOLDS.**
  - Sensor: pidfile lock held + pidfile PID matches (§PL-002). Named.
  - Note: citations inside the invariant body reference `[operator-nfr.md §7.3]` (wrong — see Challenge 1) and `[beads-integration.md §10.8]` (wrong — should be §4.10). Fix with Challenge 1.

- **PL-INV-002 — Daemon is deterministic (lines 326–330).** Claims to span architecture.md §4.1, §4.9, and "every subsystem spec's Go-package declaration." **BORDERLINE — leans to HOLDS.** It's a cross-subsystem property (every subsystem is implicitly constrained by "no LLM in the daemon") but its enforcement is a single build-time lint (per §10.2). Architecture.md §4.9 centralized-controller principle covers it; this invariant is the cross-subsystem projection of that architectural principle.
  - Sensor: `go-arch-lint` on `internal/daemon` imports (named in §10.2). OK.
  - Risk: this is a duplicate of the centralized-controller principle at architecture.md §4.9. Consider re-citing rather than restating. If kept, sharpen the sensor declaration.

- **PL-INV-003 — Orphan sweep completes before reconciliation classification (lines 332–336).** Claims cross-subsystem with reconciliation. **HOLDS** — reconciliation detectors genuinely depend on this ordering (reconciliation §8 preamble explicitly states "detectors below assume the orphan sweep of [process-lifecycle.md §8.2 step 1a] has completed").
  - Sensor: absent. The invariant has no declared mechanism for verifying it at runtime. A scenario test in §10.2 covers it negatively, but AR-042 requires every invariant to have a sensor. Alternatives: a daemon-state flag `orphan_sweep_complete_at: Timestamp` that every detector MUST check before classifying, or a daemon-internal assertion that fires if any detector runs before the flag is set.
  - **Fix**: add a sensor sentence: "Sensor: the daemon maintains an internal flag `orphan_sweep_complete_at`; every §PL-005 step 7 reconciliation dispatch MUST verify the flag is set non-nil before invoking any detector (per [reconciliation.md §4.3])."

**Missing invariants that should exist:**

- **Socket singularity.** The spec declares §PL-003 (one socket per project) and the socket-bind-cleanup rule, but no invariant ties together "for each project, at most one bound socket at `.harmonik/daemon.sock` at any time." Parallel to PL-INV-001 but for socket identity. Candidate: **PL-INV-004 — Socket-path exclusivity.**

- **Agent-subprocess parentage.** PL-014 requires "children of the daemon process." No invariant captures "for every live handler session, the subprocess's parent PID is the daemon's PID OR init (re-parented post-crash)." This is load-bearing for the orphan sweep; without the invariant, the sweep's detection rule is ungrounded. Candidate: **PL-INV-005 — Every handler subprocess has the daemon as initial parent.**

## MUST/SHOULD discipline

Places where keyword choice is wrong or permissive language hides a real requirement.

1. **PL-006 "bounded; ≤2 seconds"** (line 124). The `≤2 seconds` is MUST-shaped but lives in parenthetical prose. Fix: promote to its own requirement or name the bound in PL-006's body explicitly as a MUST.

2. **PL-006 "bounded interval between them"** (line 126). The SIGTERM→SIGKILL interval is declared bounded but the bound is not given. OQ-PL-002 takes a default of 5s; the requirement body says "bounded" with no number. Either promote the OQ default into the normative text or weaken to SHOULD with a named rationale.

3. **PL-012 "On `stop --immediate` … MUST still attempt steps 4–7 on a best-effort basis"** (line 190). Best-effort MUST is a classic anti-pattern; best-effort is a SHOULD. Either (a) upgrade to MUST with a failure-defined path ("if step 4 fails, the daemon MUST exit with code 1") or (b) downgrade to SHOULD.

4. **PL-019 "MUST NOT share process space with the daemon"** (line 240). Fine requirement; but "process space" is informal. Sharpen: "The orchestrator-agent MUST NOT run as a thread or a sub-module within the daemon process; it MUST be a separate OS process with its own PID."

5. **PL-028 "Multiple simultaneous attaches MUST be supported"** (line 310). MUST without bound — is there an upper limit? If not, state "with no foundation-imposed upper limit" explicitly; otherwise an implementation that supports 1,024 is conforming but one that supports 8 is also conforming.

6. **PL-INV-002 "MUST contain no LLM invocations and no cognition-bearing components"** (line 328). "Cognition-bearing" is a term of art from architecture.md §4.2 ZFC classification; cite it explicitly: "…no cognition-bearing components per [architecture.md §4.2]."

## Events (§6.5) and §7.1 state machine

- **Event WHEN rules (§6.2)** are correctly shaped — they name the emission trigger and cede payload to event-model. Good. All three events (`daemon_ready`, `daemon_orphan_sweep_completed`, `infrastructure_unavailable`) are present in event-model.md §8.7 with declared payloads. **HOLDS.**

- **Missing event emission rules for §7.1 transitions.**
  - `starting` → `reconciling` transition: no emission rule (table line 378 emits `daemon_orphan_sweep_completed` on the step). The transition itself has no event. Should this be `daemon_started`? event-model.md §8.7.1 has `daemon_started` declared — is this spec the emitter? If yes, declare: "`daemon_started` — emitted at transition (init) → `starting` per §7.1; payload schema in [event-model.md §8.7.1]." Currently this spec is silent on `daemon_started` even though event-model lists it as `daemon-core`-emitted.
  - `any` → (crash): no event. Crash events are emitted on restart, not at the moment of crash (since the daemon has died). OK but name it: "Crash produces no emission from the crashed instance; next startup emits `daemon_started` and the crash-recovery path is inferred from prior state."
  - `draining` → `stopped`: no declared emission. Should this emit `daemon_shutdown` (event-model.md §8.7.3)? Same owner question.

- **State-machine table row "ready | SIGTERM / `stop --graceful`" emits `operator_pausing`** (line 383). Is that right? On SIGTERM the daemon is draining-to-exit, not pausing. Event-model.md §8.7 has `operator_pausing` but that's owner-owned-by `operator-nfr` (per §6.5 there). Emitting `operator_pausing` on SIGTERM conflates graceful-shutdown with operator-pause. The draining transition should emit either a new event `daemon_draining` or nothing — the `operator_stopped` event (emitted on entry to `stopped`, per operator-nfr ON-013) covers the terminal case. **Fix**: change the emits cell to `daemon_shutdown` (event-model §8.7.3) or empty, and remove `operator_pausing` from the PL-owned transition row.

## Recommendation

**Return to draft for R2.** The spec's shape is sound and most content survives revision, but Challenge 1 (cross-reference corruption) alone requires a cross-spec edit pass that will touch at least 4 other specs. Combining that with Challenges 2 (ready-predicate-on-auto-resolver-failure), 3 (degraded-state-scope), 4 (orphan-sweep-provenance-marker), and 5 (ntm-absence) is ~5 new requirements plus text edits to 6 existing ones. Challenges 6 (upgrade co-ownership) and 7 (runner mode) are scoped to this spec.

Specific R2 revisions, prioritized:

- **Blocking (must land in R2):**
  - Fix every cross-reference anchor. Do a corpus-wide sweep; each citation must resolve to a heading that actually exists.
  - Add PL-009a (auto-resolver failure during startup dispatch) or amend PL-009's conditions.
  - Tighten PL-006/PL-007 with a project-scoped provenance marker; add OQ-PL-NNN for macOS path.
  - Add PL-021a/b for ntm version-pin + absence-detection.
  - Decide `degraded` scope (option a: rename + narrow; option b: widen with `ready` → `degraded` transition). Align §6.1 enum comment, the state-machine table, and event-model.md §8.7.5.

- **Important (should land in R2, acceptable to defer with OQ):**
  - Rewrite PL-028 `harmonik runner` entry with explicit steps and error-surface.
  - Split PL-027 sub-obligations: mechanical (PL-owned) vs operator-facing (ON-owned).
  - Add PL-INV-004 (socket singularity) and PL-INV-005 (subprocess parentage).
  - Promote orphan-sweep timeouts from OQ-PL-002 into normative text (or add a reviewer-cited rationale for the parenthetical values).

- **Minor:**
  - Fix MUST/SHOULD discipline items listed above (items 1–6).
  - Add `daemon_started` and `daemon_shutdown` emission rules to §6.2 (this spec owns them per event-model.md §8.7.1, §8.7.3 being `daemon-core`-sourced).
  - Correct state-machine table row (ready → draining SIGTERM emits wrong event).
  - Remove the v0.2 revision-history's over-specific bullet list and replace with a one-line summary noting the cleanup pass had incomplete corpus-wide coverage (a pre-R2 acknowledgment that Challenge 1's fix is incoming).

R2 budget estimate: 80–120 lines of normative-text edits to this spec + ~30 lines of cross-reference updates across operator-nfr, reconciliation, event-model, beads-integration. The verdict-executed commit (PL-025's reconciliation-idempotence appeal) is the tightest piece of the draft and should not be touched.
