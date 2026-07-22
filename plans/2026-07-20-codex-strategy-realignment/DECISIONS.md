# Decisions — Codex + Remote Strategy Realignment

## D1 (2026-07-20) — DROP the "no local Codex / fail-closed-to-remote-worker" rule

**Decision:** Remove the hard rule that Codex may only launch when bound to an enabled
remote ssh worker (the fail-closed isolation guard — bead `hk-5h759`,
`substrate_select.go` + `workloop.go:3626-3638`). Codex MUST be runnable locally on
the host, like Claude.

**Why:** The rule was agent-originated (an admiral-*agent* ruling 2026-07-18), not an
operator mandate. `danger-full-access` Codex is the *same host posture* as the
unsandboxed Claude the operator runs every day, so a Codex-only fence is inconsistent.
(Operator notes he likely did nominally approve it, but the write-up was too convoluted
to evaluate — rubber-stamping it was the mistake, now corrected.)

**Still open (separate):** whether Codex should run WITH a sandbox at all — see below.

## D-framing (2026-07-20) — harness + mode support matrix

- **Claude, Codex, and Pi must ALL be fully-supported harnesses** — for running beads
  now, and for running as crew later.
- **Codex must be supported BOTH for running beads AND for running as a crew.**
- **These are SEPARATE discussions** — (a) Codex-for-beads, (b) Codex-as-crew. Do not
  conflate them.

## Open — feeds "Unit 2: Codex for beads"

Whether Codex runs with a sandbox is gated on three sub-questions the operator raised:

1. **Who actually commits in the DOT flow** — the daemon/DOT process (committing each
   node's output) or the agent itself? If the daemon commits, Codex needs no commit
   permission and no `danger-full-access`. *[VERIFYING — this is the linchpin.]*
2. Can we run beads with `approval=never` (Claude-style "don't ask") AND still keep a
   real filesystem sandbox (`workspace-write`)?
3. Is running a *crew* under a sandbox even feasible, or is it left to the end user to
   configure per need? *[belongs to the Codex-as-crew discussion]*

**Strategic option on the table:** disable each harness's NATIVE sandbox and instead
apply ONE uniform harmonik-level sandbox config across ALL harnesses (Claude/Codex/Pi),
rather than per-harness sandbox settings.

## D3 (2026-07-20) — Native harness sandboxes OFF; security solved uniformly & IN PARALLEL

**Decision:** Turn OFF every harness's NATIVE sandbox. Every harness (Claude, Codex, Pi)
runs **unsandboxed on the host**, uniform — Codex runs `danger-full-access`, exactly the
way Claude already runs. This kills the per-harness × per-OS sandbox-nuance matrix, and
it **dissolves the entire Codex commit/worktree/shell-step problem** — every one of those
failures was an artifact of Codex's native `workspace-write` sandbox (see `research/05`,
`research/06`). The `49d7fde3` `.git`-writable-root workaround becomes unnecessary for the
strategic path.

**Why not per-harness native sandboxes:** the maintenance surface is N harnesses × M OSes,
each with its own mechanism (Codex = seatbelt/bubblewrap, and they already disagree:
the `.git` fix works on macOS seatbelt but REGRESSES on Linux bubblewrap — `research/06`).
Operator: "I don't want to spend time doing that."

**Security is NOT dropped — it's re-homed.** A single uniform sandbox for ALL beads (with
an ad-hoc per-bead disable) is a **PARALLEL workstream**, not "later": we finish this
realignment, then discuss sandbox/security design, then implement it together. See
`FOLLOWUP-TOPICS.md` §A.

**Governing security principle (operator):** build it so *"it just works AND is secure."*
Security that gets in the way gets turned off — so the system must be put in a position
where the secure path is also the easy path. Three goals: (1) runnable, (2) not painful to
run, (3) secure to run — and (3) must not undermine (2).

**Supersedes:** the D2-direction recommendation below (workspace-write + `.git` carveout as
the Codex posture). D2's *findings* still hold (daemon already commits the diff; the carveout
is safe); its *recommendation* is replaced by D3 (native sandbox off entirely).

## D2-direction (2026-07-20) — commit path is NOT the blocker; carveout approved  [RECOMMENDATION SUPERSEDED BY D3; findings still valid]

- **Verified:** in the DOT flow the daemon has a fallback that commits the agent's diff
  from OUTSIDE the sandbox. On deployed Codex 0.142.0 (self-commit fails 100%) that
  fallback is the *de-facto* committer — so **landing the code diff does NOT require
  Codex to commit, and does NOT require danger-full-access.** (Operator's Question 1
  confirmed. Evidence: `research/05`, gate logs "auto-committed by daemon fallback".)
- **Operator approves** granting `<repo>/.git` as a `workspace-write` writable root — it
  is a tight, safe carveout (git object store only; no network, no full-FS, no arbitrary
  exec) and is NOT a concern for the operator. This is the least-privilege commit fix
  (already coded, `49d7fde3`), superseding danger-full-access.
- **The real remaining sandbox question for Codex-for-beads** is NOT commits — it is that
  `workspace-write` also blocks Codex's own `exec_command` shell steps (running tests,
  `git status`, multi-step git). Whether Codex can do its own verification in-sandbox is
  the open item (`research/05` finding #13, root cause UNVERIFIED).

## Open research — Codex native worktree support

Operator wants to KEEP using git worktrees. The commit wall exists because a linked
worktree's real gitdir lives in the main repo's `.git` (outside the worktree cwd), so a
cwd-scoped sandbox can't write commits. Question: does Codex NATIVELY handle worktrees
(auto-grant the git common dir, or a config flag), so we don't hand-inject it? Newer
codex versions may. → `research/06-codex-worktree-support.md`.

## D4 (2026-07-21) — SCRAP ssh-per-node remote execution

**Decision:** The current remote model — ship the whole repo to a worker, run the entire
bead over ssh, connect back via a per-run reverse tunnel — is a bad architecture and is
being SCRAPPED, not patched. The multi-day-churn wedges that live in it (hk-qxvc2 review
stall, hk-daegv codex commit, the reverse-tunnel/env-forward fixes) are therefore MOOT —
do not spend more effort on them.

**Why:** It stretches the LOCAL worktree+agent execution model across a network node-by-
node over ssh; every wedge is a symptom of "make ssh behave like local" (dropped env,
dead tunnel, per-OS sandbox nuance, global-substrate entanglement). See `research/03`.

**The replacement is NOT decided here.** The main agent's pull-based-worker proposal was a
DIRECTION, not a design — and the operator (rightly) rejected locking a direction without
answering the control-plane questions (kickoff, status-back, status-check). The remote /
distributed execution design is ESCALATED to a NEW, larger effort:
`plans/2026-07-21-platform-architecture/` (problem-alignment first). Unit 3's remote
question is answered THERE, not here.

**Net for THIS plan:** Units 1-3 established WHAT is wrong (agent-invented sandbox fence;
codex-for-beads needs no danger-full-access; ssh remote is the wrong shape) and locked the
harness/sandbox decisions (D1-D3) + scrap-ssh (D4). The forward architecture design moves
to the platform-architecture plan.
