# C2 — Path & Identity Parameterization — spec-draft (target: specs/process-lifecycle.md)

> Pass 5 (`spec-draft`) of `fleet-portability`. Component C2 per `02-components.md`, grounded in `04-design/path-identity-design.md` (the authoritative synthesized design; an earlier `04-design/C2-path-identity.md` proposal variant is superseded).
> This draft contains ONLY the new/changed normative clauses for `specs/process-lifecycle.md` — not a re-paste of the whole file, not a diff. Requirement IDs touched: **PL-006a** (AMENDED), **PL-006d** (GENERALIZED + retitled), and **PL-030** (NEW — provisional ID; the integration pass confirms the next free PL number, see Cross-spec notes).
> C2 edits the SAME spec file as C1 but DIFFERENT requirements: C2 owns the per-project session-naming extension of PL-006a, the orphan-sweep exemption generalization of PL-006d, and the `$HARMONIK_PROJECT` skill-resolution convention (PL-030). The integration pass (06) reconciles C2's PL-006a/PL-006d edits with any C1 edits to the same file and finalizes the PL-030 ID.

---

#### PL-006a — Project hash and provenance marker (AMENDED)

> AMENDMENT: clause (a) below is extended to ENUMERATE the launch-layer session names that carry the `harmonik-<project_hash>-` prefix. All other PL-006a content (the SHA-256-of-realpath hash definition, the provenance-marker env-var + PGID mechanics, the `Setsid` discipline, the darwin/Linux match rules, OQ references) is UNCHANGED.

The daemon MUST compute a stable `project_hash` at startup as the first 12 hexadecimal characters of `SHA-256(realpath(project_root))` (case-fold ambiguity remains tracked under OQ-PL-008). The hash MUST be stable across restarts (the same project root yields the same hash). The hash is used to:

(a) Scope tmux session names (`harmonik-<project_hash>-<session_name>`). The launch-layer session names that MUST carry this prefix are, at minimum:
  - the daemon's own spawn-target session (`-default`) and per-run implementer/reviewer sessions (`-flywheel`), as established by PL-006a / PL-019;
  - **crew sessions: `harmonik-<project_hash>-crew-<name>`** (replacing the prior unqualified `hk-crew-<name>`), where `<name>` is the crew name;
  - **the supervisor session: `harmonik-<project_hash>-daemon-supervise`** (replacing the prior `hk-daemon-supervise` constant) — SEE the supervisor-prefix OPEN ITEM in Cross-spec / integration notes; this name is the C2 candidate and is NOT yet agreed with C3;
  - **the captain session: `harmonik-<project_hash>-captain`**;
  - **keeper sessions: `harmonik-<project_hash>-keeper-<role>`**, where `<role>` distinguishes per-role keepers.

  The single hashing scheme of this clause MUST be reused for every name above; no second hashing scheme is permitted. The canonical builder is `lifecycle.TmuxSessionName(project_hash, session_name)` = `"harmonik-" + project_hash + "-" + session_name`. The captain and keeper names are minted by the C3 launch tooling and only become subject to this clause once that tooling lands (see PL-006d and the Cross-spec notes); until then captain/keeper retain their current outside-the-prefix names so nothing regresses.

(b) Scope a provenance marker on every handler subprocess spawned by the daemon.

The provenance marker MUST be implemented by BOTH of the following to permit disambiguation across OS and tool differences: (i) setting the environment variable `HARMONIK_PROJECT_HASH=<project_hash>` on every spawned subprocess (readable via `/proc/<pid>/environ` on Linux); (ii) setting the subprocess's process group (PGID) to a deterministic per-project value as concretized in the unchanged PGID/`Setsid` mechanics of this requirement.

The orphan sweep (PL-006) MUST match on the environment variable on Linux and on the PGID on darwin (where `/proc/<pid>/environ` is not available); darwin-specific fallback mechanics are tracked as OQ-PL-008.

> NOTE (window-naming carve-out): the `hk-<hash6>-` WINDOW-name prefix used for `$TMUX`-reuse mode per [workspace-model.md §4.1 WM-002a] / PL-021b §4 is a SESSION-INTERNAL window sentinel and is UNAFFECTED by this amendment. PL-006a clause (a) concerns SESSION names only. The crew WINDOW name inside a crew's own session (`hk-crew-<name>`) likewise stays session-scoped and is NOT project-qualified (a session-internal window cannot collide across projects and carries no `hk-<hash6>-` window sentinel, so the window-orphan sweep cannot reap it).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-006d — Orphan-sweep exclusion for LIVE-OWNED launch-layer tmux sessions (GENERALIZED, was: coordinator/orchestrator tmux sessions)

> GENERALIZATION: PL-006d previously exempted only the supervisor flywheel session from the orphan sweep via a single supervisor sentinel. Because PL-006a now places crew, supervisor, captain, and keeper sessions inside the swept `harmonik-<project_hash>-` namespace, the exclusion is generalized to ANY prefix-matched session whose owner is provably live, via three owner-proof mechanisms. The supervisor/flywheel sentinel mechanism is UNCHANGED.

The orphan sweep of §PL-006 MUST NOT kill tmux sessions or windows that are actively owned by a provably-live launch-layer owner. Without this exclusion, a daemon restart's sweep would inadvertently kill a live coordinator pane (e.g. the flywheel pane `harmonik-<project_hash>-flywheel`, or a crew pane mid-bead), terminating in-flight cognition or crew work. The original flywheel defect is tracked as hk-hc3qq.

**Two sweepers, two kill rules (why this exclusion is load-bearing).** A prefix-matched session is reachable by BOTH of the daemon's session sweepers, which have DIFFERENT kill rules:
1. The UNCONDITIONAL session sweeper (`lifecycle.SweepOrphanTmuxSessions`) kills EVERY prefix-matched session not present in the exclude set; it applies NO orphan test. This path is lethal to a live crew mid-bead.
2. The orphan-TEST session sweeper (`ltmux.SweepOrphanTmuxSessions`) kills only a session classified orphaned by `sessionIsOrphaned` (all windows running only `zsh`, OR a dead first-pane PID). A crew idle at a `zsh` prompt BETWEEN tasks is classified orphaned and would be killed by this path.

The exclude set MUST be consulted BEFORE either sweeper acts AND before any `sessionIsOrphaned` classification. Therefore an excluded LIVE session is never reaped by either path, even when it is idle-at-`zsh`. This is exactly the guarantee by which the live flywheel survives the sweep regardless of its window state.

**Owner-proof mechanisms.** For each candidate session matching the `harmonik-<project_hash>-` prefix, the sweep MUST add the session to `excludedTmuxSessions` if and only if one of the following owner-proofs holds at sweep time:

(i) **Supervisor / flywheel (UNCHANGED).** A sentinel file `.harmonik/cognition/supervisor.sentinel` (content: `schema_version=1\n`) is present AND `kill(supervisor_pid, 0) == 0` for the PID in `.harmonik/cognition/supervisor.pid`. `harmonik supervise start` writes the sentinel before `tmux new-session`; `harmonik supervise stop` / the watch-shim removes it on clean exit; it is re-written on `restart`. A present-but-stale sentinel (supervisor PID no longer live) is NOT an owner-proof: the session is treated as an ordinary orphan and killed, and the sweep MUST remove the stale sentinel via `unlink` + `fsync(parent_directory_fd)`.

(ii) **Captain / keeper (NEW; C3-written).** A sentinel file `.harmonik/cognition/captain.sentinel` is present AND `kill(captain_pid, 0) == 0` for the PID in `.harmonik/cognition/captain.pid`. These sentinel files are written by the C3 launch tooling (captain-launch.sh) — generalizing the existing `supervisor.sentinel`/`supervisor.pid` probe to a `(sentinel_name, pid_name, session_name)` triple. UNTIL the C3 writer lands, no `captain.sentinel`/`captain.pid` exists, the probe finds nothing, and this owner-proof is a NO-OP (no false skip, no regression); captain/keeper sessions correspondingly retain their current outside-the-prefix names and are not yet in the swept namespace.

(iii) **Crew (NEW; registry-record + live-pane-PID).** The crew has a durable registry record `.harmonik/crew/<name>.json` (written BEFORE spawn — the "this name is ours" marker) AND its tmux pane PID is live at sweep time. Because the crew `Record` carries NO stored PID field, liveness MUST be a LIVE pane-PID probe (no schema change): probe the session's first-pane PID (`adapter.WindowPanePID(WindowHandle(session_name + ":"))`) and test `kill(pid, 0)`:
  - **alive (`0` / `EPERM`)** → the crew session is excluded from BOTH sweep paths.
  - **dead (`ESRCH`), `pid <= 0`, or the session is absent from the live session list** → the crew session is NOT excluded (it is a genuine orphan and is reaped by the normal sweep path), AND its stale registry record MUST be garbage-collected (`crew.Remove(<name>)`), mirroring the stale-sentinel removal of mechanism (i).
  - **session launch-in-flight (record present, session not yet enumerated by this sweep's snapshot)** → exclude conservatively; it is not enumerated by the snapshot anyway.

  This is the PL-006d sentinel pattern with the crew registry record playing the sentinel-file role and the LIVE pane PID playing the `supervisor.pid` role — one mechanism, no new file format, no new directory.

**Impact on `daemon_orphan_sweep_completed` event.** The payload MUST gain two new integer fields in addition to the existing `coordinator_sessions_skipped`:
  - `crew_sessions_skipped: <integer ≥ 0>` — count of live crew sessions excluded by mechanism (iii).
  - `captain_sessions_skipped: <integer ≥ 0>` — count of live captain/keeper sessions excluded by mechanism (ii).
Both additions are additive payload extensions consistent with the PL-021c precedent (`tmux_windows_killed`) and the `coordinator_sessions_skipped` precedent; consumers MUST tolerate unknown integer fields per [event-model.md §6.3] N-1 compatibility.

**Determinism (PL-007 preserved).** Each owner-proof is read-only liveness at sweep time (the same `kill(pid, 0)` discipline already used by `sessionIsOrphaned` and mechanism (i)). The exclusion decision is therefore deterministic given the filesystem + process state, preserving PL-007. Two projects with the same crew name produce distinct project hashes → distinct session names (`harmonik-<hashA>-crew-<name>` vs `harmonik-<hashB>-crew-<name>`); each daemon's sweep filters on its OWN `harmonik-<project_hash>-` prefix and never sees the other's, so cross-project reap is impossible.

Cross-spec coordination: [event-model.md §8.7.14] `daemon_orphan_sweep_completed` payload schema requires the `crew_sessions_skipped` and `captain_sessions_skipped` field additions (additive, N-1-tolerant — same handling as the existing `coordinator_sessions_skipped` and `bead_in_progress_reset` additions).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
Refs: hk-hc3qq (orphan sweep kills flywheel pane on daemon restart).

#### PL-030 — Portable skill-resolution convention (`$HARMONIK_PROJECT`)

> NEW requirement. ID `PL-030` is PROVISIONAL: the current max sequential PL ID is PL-028 (with letter-suffixed variants up to PL-028d), so PL-029 is the next free number. PL-030 is reserved here to leave PL-029 available for C1's draft of the same file; the integration pass (06) confirms or reassigns the final non-colliding ID. See Cross-spec / integration notes.

Shipped agent skills, agent-readable docs, and launch scripts that reference a path inside the project root MUST resolve the project root portably and MUST NOT contain a literal hard-coded absolute project path (e.g. `/Users/gb/github/harmonik`).

(a) **Resolution source.** Such references MUST resolve the project root from the environment variable `$HARMONIK_PROJECT` in shell- and agent-readable contexts, or from the phrase `<project root>` in prose. `$HARMONIK_PROJECT` is already injected by every fleet launch path (keeper hooks, the crew launch spec = `"HARMONIK_PROJECT=" + project_dir`, and read by the promote command); this convention adds NO new injection point. The documented fallback chain is `$HARMONIK_PROJECT → git rev-parse --show-toplevel`, so a hand-launched session that forgot to export the variable still resolves the root.

(b) **Project-local-NOT-global disambiguation MUST survive as prose.** Each reference to a PROJECT-LOCAL skill or file (under the project root's `.claude/skills/…`) MUST preserve, as prose at the reference site, the disambiguation that it is the project-local artifact and explicitly NOT the global `~/.claude/skills/…` copy — for example: "Read `$HARMONIK_PROJECT/.claude/skills/captain/STARTUP.md` — the PROJECT-LOCAL skill, NOT `~/.claude/skills/captain/`." The "NOT `~/.claude/skills/`" warning MUST survive; the literal absolute path MUST NOT. (This preserves the disambiguation intent originally encoded by hardcoding the absolute path in commit 7b84264d, which prevented a captain with a populated GLOBAL `~/.claude/skills/` from misreading the relative project-local skill as the global copy.)

(c) **Scope.** This convention governs published agent-facing artifacts (skills, agent docs, launch scripts). References to genuinely global artifacts (e.g. `~/.claude/captain-tools/…` scripts owned outside the project) are out of scope and are left as-is.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

---

## Cross-spec / integration notes

1. **[OPEN ITEM — the one genuine C2/C3 seam the integration pass MUST resolve] Supervisor session prefix.** There is a real disagreement on the supervisor session name:
   - **C2 candidate:** `harmonik-<project_hash>-daemon-supervise` — INSIDE the swept `harmonik-<project_hash>-` namespace, exempted from the sweep via the generalized PL-006d sentinel mechanism (i).
   - **C3 candidate (D8):** `hk-<project_hash>-daemon-supervise` — deliberately OUTSIDE the swept `harmonik-<project_hash>-` namespace, so it needs NO sentinel exemption at all (the same reasoning the live `hk`-prefixed keeper-launch session already uses to stay out of the sweep).
   The choice is consequential: a `harmonik-`-prefixed supervisor REQUIRES the PL-006d mechanism-(i) exemption; an `hk-`-prefixed supervisor does NOT (it is never prefix-matched by the sweep). DO NOT pick a winner in this draft. The crew clause (`harmonik-<project_hash>-crew-<name>`) and the captain/keeper clauses (`harmonik-<project_hash>-captain` / `-keeper-<role>`) are AGREED by both designs (both put crew/captain/keeper under the `harmonik-` prefix) and are written normatively above; only the supervisor prefix is unresolved. The integration pass MUST resolve this seam and, if it lands on the `hk-` prefix, must (i) replace the supervisor name in PL-006a clause (a) above and (ii) drop the supervisor from PL-006d mechanism (i)'s session-name set (the flywheel sentinel itself stays — it still exempts the `-flywheel` session).

2. **`harmonik project-hash` subcommand — OWNED by C3, CONSUMED by C2.** The shell launch layer (C3 scripts) needs the same 12-char project hash the Go core computes, without re-implementing `sha256-of-realpath` in bash. A read-only subcommand `harmonik project-hash [--project DIR]` provides it. Contract (so PL-006a's session-naming clause can reference it): read-only; prints `<project_hash>` followed by a single newline; exit 0; `--project` defaults to CWD. C3's design ALSO claims this subcommand and needs it FIRST; RECORD that **C3 OWNS** the subcommand and **C2 CONSUMES** it. The name is `harmonik project-hash` (NOT `print-project-hash` as an earlier C2 variant called it). Defer ownership to C3; the integration pass aligns the citation.

3. **[REVIEW GATE — published-contract changes] The 29 skill edits (PL-030) and the `crewSessionName` rename (PL-006a) are PUBLISHED-CONTRACT changes** and require an independent reviewer gate before merge. The skill edits change agent-launch context; the `crewSessionName` rename changes a name that C3's `crewlog.sh` resolves by, so `crewlog.sh` MUST update in lockstep (a C2↔C3 coordination contract). Per project policy, published-field/contract renames are not landed without an independent review pass.

4. **WM-002a window-naming is UNTOUCHED.** [workspace-model.md §4.1 WM-002a] / PL-021b §4 govern WINDOW names (the `hk-<hash6>-` `$TMUX`-reuse window sentinel). This draft changes only SESSION names. The window sentinel discipline stays as-is — state this explicitly so the integration pass does not assume a WM-002a edit is implied.

5. **event-model.md §8.7.14 — additive payload-field coordination.** The `daemon_orphan_sweep_completed` payload schema in [event-model.md §8.7.14] gains `crew_sessions_skipped` and `captain_sessions_skipped` (integers ≥ 0), additively, with N-1 tolerance — exactly the handling already applied to `coordinator_sessions_skipped` and `bead_in_progress_reset`. If §8.7.14 requires a schema bump rather than additive-tolerance, a companion EV revision item should be filed (mirroring the existing `hk-iuaed.5` precedent for the `bead_in_progress_reset` addition).

6. **Provisional PL ID.** PL-030 is provisional; PL-029 is the actual next-free numeric ID (max is PL-028 + letter variants). PL-029 is intentionally left for C1's draft of this same file to avoid a collision; the integration pass finalizes both numbers.
