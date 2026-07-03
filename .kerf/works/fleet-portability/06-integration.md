# Fleet Portability — Integration (Pass 6)

> Cross-reference consistency check across all `05-spec-drafts/` changes and the existing
> `specs/` corpus. Reconciles the cross-component seams named in the design + changelog,
> resolves the one genuine decision (supervisor-session prefix), and pins the implementation
> ordering. Verified against the live tree 2026-06-13.

## 0. Spec corpus examined

Modified by this work: `specs/process-lifecycle.md` (C1+C2+C3), `specs/operator-nfr.md` (C3),
`specs/release-pipeline.md` (C3), `specs/event-model.md` §8.7.14 (C2 additive payload).
Read-but-unchanged for contradiction sweep: `specs/execution-model.md` (EM-012a workflow-mode
precedence chain that PL-004b must not contradict), `specs/workspace-model.md` (WM-002a window
naming, WM-005b branching), `specs/workflow-graph.md`, `specs/beads-integration.md`,
`specs/handler-contract.md`, `specs/architecture.md`. No new spec file is created (C4 is doc-only).

---

## 1. Requirement-ID allocation (collision-checked)

| Spec | New/amended ID | Component | Collision? |
|---|---|---|---|
| process-lifecycle.md | PL-029 (init contract) | C1 | none — top-level max was PL-028 |
| process-lifecycle.md | PL-004b (config resolution) | C1 | none |
| process-lifecycle.md | PL-006a (amend) | C2 | n/a (amend) |
| process-lifecycle.md | PL-006d (amend/generalize) | C2 | n/a (amend) |
| process-lifecycle.md | PL-030 (skill-resolution) | C2 | none — C2 deliberately skipped PL-029 |
| process-lifecycle.md | PL-031 (project-hash subcommand) | C3 | **ASSIGNED HERE** (see §3) |
| operator-nfr.md | ON-058 (multi-tenancy invariant) | C3 | none — highest existing ON-057 |
| event-model.md | §8.7.14 payload fields (no new req ID) | C2 | n/a |

**Resolution — the `harmonik project-hash` subcommand needs a real requirement ID.** Both C2 and
C3 reference the subcommand but neither minted a PL ID for it. C3's draft recommends its home is the
process-lifecycle CLI surface (the hash is a PL-006a-owned concept). Integration ASSIGNS it
**PL-031 — `harmonik project-hash` read-only subcommand**, placed in the PL §4.4/§4.x CLI-surface
group adjacent to PL-006a. Contract per C3's draft: read-only; prints the 12-hex PL-006a
`project_hash` + newline; exit 0; `--project` defaults to CWD (resolved to realpath before hashing);
side-effect-free (no daemon, no `$TMUX`); non-zero exit + empty stdout on error so the shell guard
`HASH="$(harmonik project-hash --project "$P" 2>/dev/null || true)"` degrades cleanly. The
underlying accessor is the SAME `lifecycle.ComputeProjectHash` / `tmuxStartHashDir` the Go core uses
— one hashing scheme. (Update both C2's and C3's drafts to cite PL-031 at finalization.)

Final non-colliding set: **PL-004b, PL-029, PL-030, PL-031** (new), **PL-006a, PL-006d** (amended),
**ON-058** (new). Re-confirm free at `kerf finalize` time in case a parallel spec work mints one first.

---

## 2. SEAM RESOLVED — the supervisor-session prefix (the one genuine cross-component decision)

C2's draft wrote `harmonik-<project_hash>-daemon-supervise` (inside the swept namespace, requiring a
PL-006d sentinel exemption). C3's draft (D8) wrote `hk-<project_hash>-daemon-supervise` (outside the
swept namespace, needing NO exemption). Both designs agree crew/captain/keeper go under `harmonik-`;
only the supervisor diverges.

**DECISION (integration): the supervisor session uses the `hk-<project_hash>-daemon-supervise`
prefix — OUTSIDE the `harmonik-<project_hash>-` orphan-sweep namespace.** Rationale:

- It is the simpler, lower-risk option: the supervisor is the process that RESTARTS the daemon,
  so it MUST survive a daemon-restart sweep. Keeping it OUT of the swept prefix removes any chance a
  sweep reaps its own restarter, with no dependency on a correctly-written/live sentinel.
- It matches the EXISTING precedent already in the tree: `scripts/hk-keeper.sh` deliberately uses
  the non-`harmonik`-prefixed `hkdkeeper` session for exactly this reason. The supervisor joins that
  established "launcher sessions stay on `hk-`, outside the sweep" convention.
- The flywheel session (`harmonik-<project_hash>-flywheel`, PL-019) STAYS inside the swept namespace
  and KEEPS its PL-006d mechanism-(i) sentinel exemption — that is the live cognition pane the sweep
  must not reap; the sentinel already protects it. Only the SUPERVISOR session moves to `hk-`.

**Edits this decision forces (carried into pass 7 tasks):**
- **PL-006a clause (a):** REMOVE `daemon-supervise` from the enumerated `harmonik-<project_hash>-`
  session list (it is no longer in that namespace). Keep crew/captain/keeper there. Keep `-flywheel`.
- **PL-006d:** the supervisor session is NOT listed among the prefix-matched candidates (it is never
  prefix-matched). Mechanism (i)'s sentinel continues to exempt the `-flywheel` session only.
- **`SupervisorSessionName(projectDir)` (const→func, on the C2/C3 boundary):** returns
  `"hk-" + project_hash + "-daemon-supervise"`. This single function is OWNED BY C2 (it owns
  launch-layer session naming) and CONSUMED by C3's `scripts/hk-supervise.sh`. ONE component edits
  `internal/lifecycle/tmux/subcommand.go:182` — assign it to C2 to avoid a double-edit.
- **`scripts/hk-supervise.sh` (C3):** session name `hk-<project_hash>-daemon-supervise`, matching.
- **release-pipeline.md §7.2 (C3):** the named supervisor session is `hk-<project_hash>-daemon-supervise`.

> Captain note: this is a published session-naming choice. It is the throughput-maximizing, lower-risk
> option and is consistent with the live `hkdkeeper` precedent. Surfaced in the report for awareness;
> not held pending approval.

---

## 3. C2 ↔ C3 ownership split (so two components never edit the same file region)

| Surface | Owner | Notes |
|---|---|---|
| The 6-of-8 skills' de-hardcoding (`$HARMONIK_PROJECT`, PL-030) | **C2** | published-contract → review gate |
| `crewSessionName` rename → `harmonik-<hash>-crew-<name>` (`tmuxsubstrate.go:982`) | **C2** | published-contract → review gate; `crewlog.sh` updates in lockstep |
| Crew session-naming + crew sweep-exemption (PL-006a/PL-006d crew clauses) | **C2** | `internal/daemon/orphansweep.go`, `internal/crew/registry.go` |
| `SupervisorSessionName` const→func (`subcommand.go:182`) | **C2** | per §2 decision; one editor only |
| captain/keeper session-naming in `captain-launch.sh` + `captain.sentinel`/`captain.pid` writers | **C3** | published Captain&Crew contract → review gate with C2 |
| `harmonik project-hash` subcommand (PL-031) | **C3** | shared shell↔Go hash source; C2 consumes |
| keeper Stop/PreCompact hook dedup-key change (`keeper_enable_doctor_cmd.go`) | **C3** | ON-058(a) |
| statusLine single-agnostic-stanza (`keeper_enable_doctor_cmd.go`) | **C3** | ON-058(a) |
| captain-tools versioning + embed + init-provisioning (`scripts/captain-tools/`, `init_assets.go`) | **C3** (source/embed) + **C1** (calls the provision step) | ON-058(b); C1↔C3 seam |
| `/tmp` globals (last-good, daemon log, supervise fallback) | **C3** | ON-058(c), `lastgood.go`, `hk-keeper.sh`, `hk-supervise.sh` |
| init provisioning, embedded AGENTS template, scaffolds, config-precedence, phantom-guard removal | **C1** | PL-029, PL-004b |

No two components edit the same code region. The only shared file is `subcommand.go:182`
(`SupervisorSessionName`) — assigned solely to C2 per §2. `keeper_enable_doctor_cmd.go` is C3-only.
`init_cmd.go` is C1-only (C3's captain-tools provisioning is a step C1 calls into a C3-provided helper).

---

## 4. Implementation ordering (HARD sequencing — carried into pass 7 deps)

1. **C2 skill de-hardcoding lands FIRST.** 6 of the 8 fleet skills still carry the literal
   `/Users/gb/github/harmonik`. The embedded `cmd/harmonik/assets/skills/` is a `go:generate`
   snapshot of `.claude/skills/`. If C1's embed runs before C2's de-hardcoding, `init` ships
   skills with the literal harmonik path baked in → violates SC3 (no hardcoded path) on every
   foreign repo. **C2's skill edits MUST precede the C1 go:generate snapshot.**
2. **C3 `harmonik project-hash` (PL-031) lands before** the C3 shell scripts and C2's shell-facing
   session-name peers that call it.
3. **C2 `SupervisorSessionName` const→func lands before** C3's `hk-supervise.sh` references the
   qualified name (or they land together — same review batch).
4. **C3 captain-tools source + embed land before** C1's init-provisioning step calls them (the C1↔C3
   provisioning seam) — or C1's provisioning step is written to no-op gracefully if the embed asset
   is absent at build time (preferred: land C3's `scripts/captain-tools/` + embed, then C1's call).
5. **C1 init/embed lands LAST among the code changes** so it snapshots the already-de-hardcoded
   skills and the already-versioned captain-tools.

Spec-text edits (PL/ON/EM) can land independently of the code ordering (specs are normative-first),
but the IMPLEMENTATION beads must respect 1→5.

---

## 5. Cross-reference & contradiction checks

| Check | Result |
|---|---|
| PL-004b vs EM-012a (execution-model.md workflow-mode 4-tier precedence) | CONSISTENT. PL-004b implements the config-file READ that PL-004a already mandates ("read exactly once at PL-005 step 0"); it adds the flag>file>default precedence for the daemon-level tier without touching EM-012a's per-task→per-project→daemon→fallback walk. PL-004b explicitly defers per-task/per-project resolution to EM-012a and honors the `dot→review-loop` floor. No contradiction. |
| PL-029 vs WM-005b / hk-sul12 (branching / target-branch enforcement) | CONSISTENT. PL-029 REMOVES the redundant init-side `--target-branch` guard; the real fail-closed enforcement stays at the daemon boot guard (hk-sul12) reading `branching.yaml lands_on` (WM-005b). The phantom `hk-m8vy2` is NOT re-created (the gated capability already ships). |
| PL-006a amend vs WM-002a (window naming) | CONSISTENT. PL-006a/PL-006d change SESSION names only; WM-002a's `hk-<hash6>-` WINDOW sentinel is explicitly untouched (stated in both the C2 draft and §2 here). |
| PL-006d amend vs event-model.md §8.7.14 | ADDITIVE. `crew_sessions_skipped` + `captain_sessions_skipped` follow the existing `coordinator_sessions_skipped` / `bead_in_progress_reset` N-1-tolerant precedent. Integration confirms additive-tolerance is the path (no schema bump); if §8.7.14 requires a bump, file a companion EV item (mirrors hk-iuaed.5). Carried to pass 7 as a small EV-payload task. |
| ON-058 vs operator-nfr §2.2 (MVH multi-tenancy deferral) | CONSISTENT. §2.2 explicitly DEFERS multi-tenancy; ON-058 is its post-MVH realization → additive, not in tension. |
| release-pipeline §7.2 amend vs PL-019 (`harmonik supervise` + `.harmonik/cognition/`) | CONSISTENT. §7.2 renames the supervisor-of-record from the retired `/tmp` script to `harmonik supervise`, which PL-019 already establishes. The per-project last-good path (`<projectDir>/.harmonik/state/last-good-binary`) tightens §7.2's own already-named post-1.0 target. |
| Supervisor prefix `hk-` vs PL-006 sweep prefix `harmonik-<hash>-` | CONSISTENT after §2 decision. `hk-`-prefixed supervisor is never prefix-matched by the sweep — same as the existing `hkdkeeper`. |
| `harmonik project-hash` (PL-031) vs PL-006a hash definition | CONSISTENT. Same first-12-hex-of-SHA-256(realpath) accessor; one hashing scheme. |
| Terminology: `project_hash` (spec) vs `hash6`/`hash` (design shorthand) | RECONCILED. The spec term is `project_hash` = first 12 hex of SHA-256(realpath). The designs' `hash6`/`hash` shorthand refers to the SAME value; all DRAFT spec text uses `project_hash`. (The `<hash6>` token in WM-002a window names is a DIFFERENT, window-only sentinel and is untouched.) |

No unresolved contradiction. All cross-references in the drafts point at requirements that exist
(or are minted by this work). The changelog (`05-changelog.md`) matches the drafts, with the one
addition reconciled here: the `harmonik project-hash` subcommand gets requirement ID **PL-031**
(the changelog listed it without an ID).

---

## 6. C4 — Multi-repo dispatch boundary (doc-only adoption)

C4 produces no spec-draft file. Integration records the portability boundary as a "known boundaries"
note to land in the portability narrative (and as a one-line cross-reference appended to the
process-lifecycle / operator-nfr scope sections at finalization, NOT a new requirement):

> **Known boundary — single supervised repo.** The daemon lands only changes to the repo it
> supervises: worktrees are rooted at `<projectDir>/.harmonik/worktrees/`, a bead touching another
> repo cannot satisfy the managed-worktree prefix check, and no per-bead repo override exists (no
> `repo_url`/`target_repo`/`repoPath` field on the bead record). Cross-repo fixes are applied
> out-of-band. Tracked as open bead **hk-3r3**; designing the multi-repo dispatch mechanism is a
> separate work.

No code, no requirement ID. The pass-7 task for C4 is a documentation task that adopts hk-3r3.

---

## 7. Final assessment

The drafted changes are internally coherent and consistent with the unchanged corpus. The single
genuine cross-component decision (supervisor-session prefix) is RESOLVED to `hk-` (§2). The
`harmonik project-hash` subcommand is assigned PL-031 (§1). Ownership is split with no overlapping
file regions (§3) and the C2→regenerate→C1 hard ordering is pinned (§4). All amendments are additive
(`schema_version` unchanged across PL/ON/EM/release-pipeline). Ready to advance to the tasks pass.
