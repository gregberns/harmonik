# Spec-draft changelog — `fleet-portability`

> Pass 5 (`spec-draft`) changelog. One row per (target spec file × component), with status
> (new/modified), what changed, and the motivating change design (`04-design/{component}-design.md`).
> The integration pass (pass 6) reconciles the two components that edit the SAME file
> (`specs/process-lifecycle.md`: C1 adds PL-029/PL-004b, C2 amends PL-006a/PL-006d and adds PL-030)
> and resolves the one genuine cross-component seam (the supervisor-session prefix).

---

## Minted / amended requirement IDs (collision-checked against the live tree 2026-06-13)

| Component | Spec | Requirement | Status | Verified-free? |
|---|---|---|---|---|
| C1 | process-lifecycle.md | **PL-029** — Portable project initialization contract | NEW | yes (max top-level was PL-028) |
| C1 | process-lifecycle.md | **PL-004b** — Operational config resolution (workflow_mode / max_concurrent / target_branch) | NEW (after PL-004a) | yes |
| C2 | process-lifecycle.md | **PL-006a** — Project hash & provenance marker | AMENDED (enumerate launch-layer session names) | n/a (amend) |
| C2 | process-lifecycle.md | **PL-006d** — Orphan-sweep exclusion (retitled "live-owned launch-layer sessions") | AMENDED/GENERALIZED | n/a (amend) |
| C2 | process-lifecycle.md | **PL-030** — Portable skill-resolution convention (`$HARMONIK_PROJECT`) | NEW | yes |
| C3 | operator-nfr.md | **ON-058** — Multi-tenant global-surface isolation | NEW | yes (highest existing ON-057) |

**No ID collision.** C1 took PL-029, C2 deliberately took PL-030 (leaving PL-029 for C1). Both
verified against the live spec. The integration pass re-confirms PL-029/PL-030/PL-004b/ON-058 are
still free at finalization (a parallel spec work could mint one first).

---

## C1 — Init provisioning completeness

Draft: `05-spec-drafts/init-provisioning.md` (target: `specs/process-lifecycle.md`). Design: `04-design/init-provisioning-design.md`.

| Target spec file | Status | What changed | Motivating design |
|---|---|---|---|
| `specs/process-lifecycle.md` | modified | **PL-029** (new): `harmonik init` from an INSTALLED binary on a FOREIGN repo MUST exit 0 and produce a bootable, self-consistent project — provisions the 8 fleet skills from a binary-embedded `embed.FS` (excludes the 3 reviewer/scaffold skills; ~180KB), reads the AGENTS template from the embed (not a project-relative `os.ReadFile`), renders a self-consistent AGENTS.md (3 committed scaffolds `AGENT_INDEX/STATUS/TASKS` + a pruned foreign-repo template variant; NOT a knowledge-base seed), creates runtime dirs `.harmonik/{comms,crew,keeper,queues}` + gitignores them, is idempotent (skip-unless-`--force`, never clobbers sibling skill dirs), and REMOVES the phantom `--target-branch` guard (the merge-retarget capability already ships and is enforced at the daemon's hk-sul12 boot guard). **PL-004b** (new, after PL-004a): the three operational scalars `workflow_mode`/`max_concurrent`/`target_branch` resolve via `explicit flag > config.yaml(`daemon:` block)/branching.yaml > built-in default`; closes the pre-existing PL-004a conformance gap (the loader never read the config `workflow_mode`); `workflow_mode` validated through `core.WorkflowMode().Valid()` and held to the PL-004a `dot→review-loop` floor; `max_concurrent ≤ 0` ⇒ not-configured; `target_branch` defers to `branching.yaml lands_on`. | `04-design/init-provisioning-design.md` (Decisions a–e) |

C1 entries are additive (`schema_version` unchanged). PL-004b implements a config read PL-004a
already mandates → complements, does not contradict, the four-tier precedence chain (per-task →
per-project → daemon-level → built-in `dot`, owned by EM-012a). Ongoing maintenance cost recorded:
the embedded `cmd/harmonik/assets/skills/` is a `go:generate` snapshot of `.claude/skills/` guarded
by a CI diff-check (same discipline as the embedded `standard-bead.dot`).

---

## C2 — Path & identity parameterization

Draft: `05-spec-drafts/path-identity.md` (target: `specs/process-lifecycle.md`). Design: `04-design/path-identity-design.md`
(authoritative synthesized design; `04-design/C2-path-identity.md` is a superseded proposal variant).

| Target spec file | Status | What changed | Motivating design |
|---|---|---|---|
| `specs/process-lifecycle.md` | modified | **PL-006a** (amended): clause (a) now ENUMERATES the launch-layer session names carrying the `harmonik-<project_hash>-` prefix — crew `crew-<name>`, supervisor `daemon-supervise` (prefix is the one OPEN seam, see integration), captain `captain`, keeper `keeper-<role>` — all via the single existing `lifecycle.TmuxSessionName` builder (no second hashing scheme); the WM-002a window-name sentinel is explicitly UNTOUCHED. **PL-006d** (generalized + retitled "live-owned launch-layer tmux sessions"): exclusion now applies to any prefix-matched session with a provably-live owner via three owner-proofs — (i) supervisor sentinel UNCHANGED, (ii) captain/keeper `captain.sentinel`+`captain.pid` (NEW, C3-written, no-op until C3 lands), (iii) crew registry-record + LIVE pane-PID probe (NEW); excludes BEFORE `sessionIsOrphaned` so an idle-at-zsh live crew is never reaped by either sweeper; dead crews are reaped + their stale registry record GC'd; PL-007 determinism preserved; payload gains `crew_sessions_skipped` + `captain_sessions_skipped`. **PL-030** (new): portable skill-resolution convention — shipped skills/docs/scripts resolve the project root from `$HARMONIK_PROJECT` (fallback `git rev-parse --show-toplevel`), contain NO literal `/Users/gb/github/harmonik`, and preserve the project-local-NOT-global disambiguation as prose. | `04-design/path-identity-design.md` (D1–D6, M1–M5) |
| `specs/event-model.md` | modified | §8.7.14 `daemon_orphan_sweep_completed` payload gains `crew_sessions_skipped` and `captain_sessions_skipped` (integers ≥ 0), additive / N-1-tolerant — same handling as the existing `coordinator_sessions_skipped` and `bead_in_progress_reset`. (Cross-spec coordination flagged by the PL-006d edit; the integration pass confirms whether §8.7.14 takes additive-tolerance or needs a companion EV revision item, mirroring the `hk-iuaed.5` precedent.) | `04-design/path-identity-design.md` (M5) |

C2 entries are additive (`schema_version` unchanged). The PL-006a/PL-006d edits are amendments to
load-bearing requirements — the orphan-sweep behavior is EXTENDED (more sessions exempted), not
relaxed; the live-flywheel guarantee is unchanged and the new crew/captain exemptions follow the same
sentinel pattern. **REVIEW GATE:** the 29 skill edits (PL-030) and the `crewSessionName` rename
(PL-006a) are published-contract changes requiring an independent reviewer; `crewlog.sh` (C3) must
update in lockstep with the rename.

---

## C3 — Multi-tenant settings & global tooling

Draft: `05-spec-drafts/multi-tenant-state.md` (single combined draft, sectioned per target spec: operator-nfr.md / release-pipeline.md / process-lifecycle.md).
Design: `04-design/multi-tenant-state-design.md`.

| Target spec file | Status | What changed | Motivating design |
|---|---|---|---|
| `specs/operator-nfr.md` | modified | **ON-058** (new, recommended §4.12): multi-tenant global-surface isolation invariant — harmonik's contributions to shared surfaces (`~/.claude/settings.json` keeper hooks, `~/.claude/captain-tools/` scripts, `/tmp/hk-*` state) MUST be project-namespaced so N fleets coexist, merges additive. (a) keeper Stop/PreCompact hook groups deduped on `(script basename, HARMONIK_PROJECT=<projectDir>)` and coexist as sibling array entries; the statusLine scalar is a SINGLE project-agnostic stanza resolving project from each session's inherited `$HARMONIK_PROJECT`; (b) captain-tools scripts versioned in `scripts/captain-tools/`, embedded, provisioned by `harmonik init` only-if-absent, resolving project+hash at runtime with no literal path; (c) per-project daemon state (last-good binary, daemon log, supervisor session) under the project's own `.harmonik/` or carrying the `<project_hash>` qualifier; `harmonik supervise` is the canonical per-project supervisor. | `04-design/multi-tenant-state-design.md` (D1, D2, D4, D5–D7, D9) |
| `specs/release-pipeline.md` | modified | §7.2 amended: pre-1.0 last-good path → `<projectDir>/.harmonik/state/last-good-binary` (realizes the section's own post-1.0 target); `hk-daemon-supervise.sh` references → `harmonik supervise` (the in-binary supervisor-of-record); a de-hardcoded `scripts/hk-supervise.sh` fallback is named for out-of-band operators (no hardcoded PROJECT/BIN; session `hk-<project_hash>-daemon-supervise` pending the prefix seam). | `04-design/multi-tenant-state-design.md` (D5, D7) |
| `specs/process-lifecycle.md` | modified | C3 note: `harmonik supervise` (per-project flywheel session + `.harmonik/cognition/`) is the canonical per-project supervisor; the `/tmp/hk-daemon-supervise.sh` script is retired/legacy. Cross-references C2's PL session-naming extension (no duplication). PLUS the **`harmonik project-hash [--project DIR]`** subcommand contract (read-only; prints the 12-hex PL-006a `project_hash` + newline; exit 0; `--project` defaults to CWD; side-effect-free, no daemon/`$TMUX` needed; non-zero+empty-stdout on error for shell-guard degradation). OWNED by C3, CONSUMED by C2. Recommended home: PL CLI surface (the hash is PL-006a-owned) — integration confirms. | `04-design/multi-tenant-state-design.md` (D3, D7, D8) |

C3 entries are additive (`schema_version` unchanged). ON-058 is the post-MVH realization of the
operator-nfr §2.2 explicitly-deferred multi-tenancy concern → additive, not in tension. **REVIEW
GATE:** versioning `captain-launch.sh` (session-id + name minting) intersects the published Captain &
Crew contract → independent reviewer alongside C2.

---

> C4 (multi-repo dispatch) draft: `05-spec-drafts/multi-repo-dispatch.md` — doc-only, no normative spec text (adopts hk-3r3).

## Cross-component reconciliation the integration pass (pass 6) MUST handle

1. **Two components edit `specs/process-lifecycle.md`.** C1 (PL-029, PL-004b) and C2 (PL-006a amend,
   PL-006d amend, PL-030) + C3's small supervisor note touch the SAME file in DIFFERENT requirements.
   No ID collision (PL-029=C1, PL-030=C2). Integration produces ONE updated file splicing all three.
2. **The supervisor-session PREFIX seam — the one genuine decision.** C2 wants
   `harmonik-<project_hash>-daemon-supervise` (inside the swept namespace → needs the PL-006d
   sentinel exemption); C3 (D8) wants `hk-<project_hash>-daemon-supervise` (outside the namespace →
   no exemption, like the existing `hkdkeeper` session). Both designs AGREE on crew/captain/keeper
   under `harmonik-`; only the supervisor diverges. **C3's recommendation is the `hk-` prefix**
   (sweep-immune, simpler — no sentinel needed for the supervisor). Integration MUST pick one and
   align (a) `SupervisorSessionName(projectDir)` const→func, (b) `scripts/hk-supervise.sh` session
   name, (c) whether PL-006d mechanism (i) lists the supervisor session. The flywheel sentinel itself
   stays regardless.
3. **`harmonik project-hash` ownership.** OWNED by C3, CONSUMED by C2. Both derive from the one
   underlying `lifecycle.ComputeProjectHash` / `tmuxStartHashDir` accessor → one hashing scheme.
   Home recommended: PL CLI surface.
4. **C1↔C3 provisioning seam.** ON-058(c)'s "provisioned by `harmonik init` only-if-absent" obligation
   lands in C1's `init` implementation; the embedded captain-tools assets + `scripts/captain-tools/`
   source live in C3. Integration confirms `init` (C1) calls the C3-provided provisioning step.
5. **C2→regenerate→C1 hard sequencing.** The embedded `assets/skills/` is a `go:generate` snapshot of
   `.claude/skills/`; 6 of the 8 fleet skills still carry the literal `/Users/gb/github/harmonik`. C2's
   skill de-hardcoding MUST land before C1 regenerates+embeds the assets, else `init` ships hardcoded
   paths to foreign repos. This is the load-bearing task ordering.
6. **event-model.md §8.7.14 additive payload.** C2 adds `crew_sessions_skipped` +
   `captain_sessions_skipped`. Integration confirms additive-tolerance vs a companion EV revision item.
7. **C4 (multi-repo dispatch) is doc-only.** No spec-draft file: C4 adopts the existing open bead
   hk-3r3 as the named fleet boundary. The portability statement (daemon lands only changes to its
   supervised repo; worktrees rooted at `<projectDir>/.harmonik/worktrees/`; no per-bead repo override)
   lands as a "known boundaries" note in the integration pass / a portability appendix, referencing
   hk-3r3. Carried forward to pass 6.
