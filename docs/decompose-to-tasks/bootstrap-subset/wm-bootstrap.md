# WM Bootstrap-Subset Enumeration

**Date:** 2026-05-05
**Cluster:** B-WM (Workspace + checkpoint substrate, WM portion)
**Epic:** `hk-8mwo` — Workspace Model spec — implementation
**Total beads:** 1 epic + 71 children = 72 (verified `br epic status` + `br show hk-8mwo`)
**Inputs:** `wm-pilot.md` v0.1.1, `wm-pilot-data.yaml` v0.1.1, `bootstrap-subset-opening.md`.
**User Qs applied:** Q1 twin IN, Q2 Pi OUT, Q4 S07 IN.

## 1. Counts

- **INCLUDE: 45** beads
- **EXCLUDE: 26** beads
- **Ratio:** 45 / 71 ≈ **63%**. Above the opening pass's 25–30 estimate; the working-def steps 3 (merge-back) and 4 (sidecar+trailer commit) force §4.5 + §4.7 in their entirety, plus all 5 schemas are load-bearing. (Mapping pilot-mnem→`hk-8mwo.NN` derived by aligning pilot §2 ordering to the epic's dependent-list; verified on samples `hk-8mwo.1`=wm-env-001, `.16`=wm-011, `.26`=wm-016, `.45`=wm-033, `.59`=Workspace, `.65`=worktree-fixture.)

## 2. INCLUDE — by §-section

### §4.0 Subsystem envelope + git pin (2)
- `hk-8mwo.1` (wm-env-001) — WM subsystem envelope; wires WM into daemon subsystem set.
- `hk-8mwo.2` (wm-env-002) — git ≥ 2.34 startup pin (gates `worktree add`, trailers, ort merge).

### §4.1 Worktree primitive (5)
- `hk-8mwo.3` (wm-001) — Workspace record fields.
- `hk-8mwo.4` (wm-002) — canonical worktree path `.harmonik/worktrees/<run_id>/`.
- `hk-8mwo.5` (wm-003) — `git worktree add -b <branch> <path> <parent_commit>` primitive.
- `hk-8mwo.6` (wm-003a) — partial-crash classification (BareWorktreeNoLease / SidecarWithoutLease).
- `hk-8mwo.7` (wm-004) — `workspace_id = "ws-" + run_id` (registry-free).

### §4.2 Branch naming (5)
- `hk-8mwo.8` (wm-005) — `run/<run_id>` task branch.
- `hk-8mwo.9` (wm-005a) — sub-workflow does NOT branch (negative-declaration; cheap shape-guard).
- `hk-8mwo.10` (wm-006) — `harmonik/integration` default + parent-bead-derived target.
- `hk-8mwo.11` (wm-006a) — ref-safe substitution via `git check-ref-format`.
- `hk-8mwo.12` (wm-007) — three-level branching (task→integration→main).

### §4.3 Lease (8)
- `hk-8mwo.15` (wm-010) — lease held by run, not agent.
- `hk-8mwo.16` (wm-011) — one active agent at a time inside a workspace.
- `hk-8mwo.17` (wm-012) — one run per bead at a time.
- `hk-8mwo.18` (wm-013) — workspace_id discoverable from run_id (no separate index).
- `hk-8mwo.19` (wm-013a) — lease-lock canonical path + JSON + atomic write+fsync.
- `hk-8mwo.20` (wm-013b) — lease release on terminal transitions.
- `hk-8mwo.21` (wm-013c) — startup discovery via filesystem walk + lock-content read (Cat 0 restart).
- `hk-8mwo.23` (wm-013e) — `.gitignore` hygiene write-or-fail (control-plane paths must be ignored).

### §4.4 State machine + emission (3)
- `hk-8mwo.24` (wm-014) — workspace state machine.
- `hk-8mwo.25` (wm-015) — emission obligations / WHEN.
- `hk-8mwo.26` (wm-016) — `workspace_leased` 4-step ordering (worktree→branch→sidecar→lock).

### §4.5 Merge-back (6)
- `hk-8mwo.27` (wm-018) — merge-back inside same lease.
- `hk-8mwo.28` (wm-018a) — merge-node dispatch contract (non-agentic OR agentic; non-agentic suffices).
- `hk-8mwo.29` (wm-019) — squash-merge `--strategy=ort` + trailers + author/committer split.
- `hk-8mwo.30` (wm-019a) — scratch merge-worktree (preferred mechanism; 7-step lifecycle).
- `hk-8mwo.31` (wm-020) — squash-merge non-fast-forward by construction.
- `hk-8mwo.32` (wm-021) — `workspace_merge_status` status=merged emission.

### §4.7 Session-log + sidecar (4)
- `hk-8mwo.37` (wm-025) — session-log dir at `.harmonik/sessions/<session_id>/`.
- `hk-8mwo.38` (wm-026) — `harmonik.meta.json` atomic write (tmp+fsync+rename+parent-fsync).
- `hk-8mwo.39` (wm-027) — sidecar precedes `workspace_leased` for first session.
- `hk-8mwo.40` (wm-028) — `bead_id` propagates into session metadata when present.

### §4.8 Orphan sweep — minimum (1)
- `hk-8mwo.45` (wm-033) — startup orphan sweep stale lease-locks (content-first staleness; `git worktree prune`). First self-build cycles WILL crash; restart cannot require manual cleanup.

### §6 Schemas (5)
- `hk-8mwo.59` (wm-schema.workspace) — Workspace record §6.1.
- `hk-8mwo.60` (wm-schema.lease-lock-file) — LeaseLockFile record §6.1.
- `hk-8mwo.61` (wm-schema.workspace-state) — 7-value WorkspaceState enum.
- `hk-8mwo.62` (wm-schema.interrupt-state) — 5-value enum; default `none`. Field is non-optional on Workspace — cheap to declare even though §4.10 logic is excluded.
- `hk-8mwo.63` (wm-schema.session-metadata-sidecar) — SessionMetadataSidecar record §6.1.

### §8 Error taxonomy (1)
- `hk-8mwo.64` (wm-error.taxonomy) — 12-class typed-sentinel set; atomic per §2.6 / F-pilot-WM-2. INCLUDE reqs cite ~8 of 12 (RefNameInvalid, GitignoreWriteForbidden, GitVersionTooOld, BareWorktreeNoLease, SidecarWithoutLease, LeaseLockHeldByOrphan, SidecarWriteFailed, RunIdReuseForbidden).

### §10.2 Test infrastructure (5 of 7)
- `hk-8mwo.65` worktree-primitive + crash-evidence fixture.
- `hk-8mwo.66` branch-naming + ref-safe substitution fixture.
- `hk-8mwo.67` lease-lifecycle + crash-recovery fixture.
- `hk-8mwo.68` merge-back + scratch-worktree fixture.
- `hk-8mwo.70` session-log + atomic sidecar fixture.

## 3. EXCLUDE — by category

26 total. Reconciled: INCLUDE 45 + EXCLUDE 26 = 71 ✓.

- **Cat-1 — Conflict resolution (deferred; happy-path has no conflicts) — 4:** `.33` wm-022, `.34` wm-022a, `.35` wm-023, `.36` wm-024.
- **Cat-2 — Failed-run + verdict-driven re-run (RC Cat 1+ deferred) — 6:** `.43` wm-031, `.44` wm-032, `.46` wm-034, `.47` wm-035, `.48` wm-036, `.13` wm-008 (parentless-run policy needs CP-037 + ON file).
- **Cat-3 — Interrupt state (operator pause/stop deferred per `core-scope.md`) — 6:** `.49` wm-037, `.50` wm-037a, `.51` wm-038, `.52` wm-038a, `.53` wm-039, `.54` wm-040.
- **Cat-4 — Sensor invariants (audit-time; not bootstrap-blocking) — 4:** `.55` wm-inv-001, `.56` wm-inv-002, `.57` wm-inv-003, `.58` wm-inv-005 (sensor IS already-INCLUDED wm-013c).
- **Cat-5 — Polish reqs — 2:** `.14` wm-009 (N-1 stable naming, post-MVH), `.42` wm-030 (post-merge log retention default).
- **Cat-6 — Memory-layer / S08 — 1:** `.41` wm-029 (read-only by S08; S08 deferred).
- **Cat-7 — Path-reuse subtle sensor — 1:** `.22` wm-013d (released-path re-use forbidden).
- **Cat-8 — Test fixtures tracking excluded reqs — 2:** `.69` conflict-resolution harness, `.71` failed-run+interrupt harness.

## 4. Cross-cluster edges OUT (INCLUDE → other clusters)

Resolved against pilot §3.1 (active edges) + §3.2 (forward-deferred) + INCLUDE-set filter:

- **EM (Cluster F): 5 active.** `em-014` bead-anchor (cited by `.3`, `.7`); `em-017` trailer schema (`.29`, `.40`); `em-023` checkpoint-cadence (`.8`); `em-034`/`em-035` sub-workflow (`.9`, `.38`); `em-schema.commit-range` CommitSHA alias (`.59`).
- **HC (Cluster C): 5 active.** `hc-007` wire-protocol (`.19`, `.21`); `hc-010` session_log_location (`.37`); `hc-schema.handler` + `hc-schema.launch-spec` + `hc-schema.session-id` (`.28`, `.37`, `.59`).
- **AR: 3 active.** `ar-024` agent-type (`.38`, `.63`); `ar-052`/`ar-053` envelope (`.1`).
- **CP: 0 active in bootstrap.** Both `cp-037` cites (wm-008, wm-024) are on EXCLUDED beads. CP can defer for the WM slice. (wm-002's CP-037 cite is supporting per F-pilot-WM-5.)
- **PL: 3 forward.** `pl-006` orphan-sweep coordination (`.21`, `.45`); `pl-021` ntm-version pin (`.2` per OQ-WM-015).
- **BI: 3 forward.** `bi-017`/`bi-018` bead_id propagation (`.3`, `.40`); `bi-014` parent-bead query (`.10`).
- **RC: 1 forward.** Cat-3 routing for orphan sweep (`.45`). All other RC forwards land on EXCLUDED beads.
- **ON: 0 hard.** Only forward from INCLUDE is `wm-env-001 → on` (ON-018/027 inherited NFRs) — supporting framing, not blocking.
- **EV: informational only** per F-pilot-EV-3 — no edges, but EV cluster D must implement the 5 WM-emitted events (workspace_created/leased/merge_status/discarded/merge_conflict_escalation — last is excluded path) for §4.4 to compile.

**Tally: 13 active + 7 forward = 20 cross-cluster out-edges from WM bootstrap.**

## 5. Cross-cluster edges IN (other clusters → WM INCLUDE beads)

Per backfill pass logs (HANDOFF.md §"WM fully landed"):

- **EV → WM: ~7.** Events 5+ co-owned consume-produce edges plus `session_log_location` and `lease_released` reciprocals; targets `.25`, `.26`, `.32`, `.37`.
- **EM → WM: ~5.** EM-017 trailer registry needs `.59` + `.63`; EM-023 needs `.8`; EM-034a sub-workflow node-id namespacing needs `.38`.
- **HC → WM: ~3.** HC-044a watcher liveness needs `.19`; HC-010 session_log_location needs `.37`; LaunchSpec consumers reference `.63`.
- **PL → WM: 0** (resolved one-way WM→PL per PL backfill 0/3 cycles).
- **RC → WM: ~2.** Cat-3 detector cites `.6` + `.45`. Most RC→WM IN edges target EXCLUDED beads.
- **BI → WM: 0.** BI's parent-bead query is consumed by WM, not vice versa.
- **ON → WM: 0** (operator-NFR pause/resume hits EXCLUDED interrupt-state beads).
- **AR → WM: sensor-only** via wm-env-001 cite of AR-052/053 (no inbound edge).

**Tally: ~17 cross-cluster IN edges to WM bootstrap-INCLUDE.**

## 6. Open questions / ambiguities

1. **wm-005a (sub-workflow no-branch).** Sub-workflows are OUT of bootstrap, so wm-005a's negative declaration is technically scope-creep. Kept INCLUDE because it's a one-line assertion that prevents shape mistakes; could flip EXCLUDE if synthesis pass prefers minimum.
2. **wm-error.taxonomy stub-the-rest.** All 12 sentinels in one bead per §2.6 (BI-shape atomic). Bootstrap reqs cite ~8. Synthesis pass should rule whether the bead's exit criterion is partial-class-coverage or full.
3. **wm-013e gitignore auto-write side-effect.** First-run daemon writes operator's `.gitignore` and commits on `harmonik/gitignore-init` branch. May surprise operators in first self-build cycles. Could downgrade to fail-fast-only.
4. **WM-INV-001/002/003/005 audit beads.** All EXCLUDED on the basis that they're conformance-time audits, not runtime gates. Consistent with opening §3 deferral of AR/EM sensor beads. If first self-build cycle includes a sensor-validator workflow, flip to INCLUDE.
5. **Cluster-A (PL) parentless-run policy.** wm-008 is EXCLUDED because it requires CP-037 precedence + ON operator-policy file. If PL synthesis includes a minimal config-precedence layer, wm-008 could flip back IN; coordinate with cluster-A author.
6. **EM portion overlap.** Cluster B-EM agent will own checkpoint trailer registry (`em-017`, `em-schema.checkpoint-trailers`) — WM merge-back commit (`.29`) consumes that schema. Verify cluster-B-EM enumerates these so the joint cluster B is self-contained.
