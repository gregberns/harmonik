# Quality Process — Staff-Level Assessment & Shift-Left Plan

**Date:** 2026-07-05 · **Author:** admiral (fleet), on operator directive · **Status:** DRAFT for operator review

> This is the answer to the question the operator actually asked on 2026-07-04:
> *"Our output is riddled with bugs — the process is fundamentally flawed. Can a smart model figure out
> how to build **reliable software**?"* — a **process** diagnosis, prevention-first.
>
> The prior artifact (`plans/2026-07-04-quality-loop/PLAN.md` + the A/B/C methodology experiment) answered
> a **different, narrower** question — "how do we prove a bug was fixed" — because the scope collapsed. The
> forensic trace of that collapse is `00-scope-collapse-forensics.md`; read it first. This plan replaces the
> narrow frame; the A/B/C work survives only as a small post-bug tail (§5).

Companion docs in this dir: `00-scope-collapse-forensics.md` (how the miss happened) ·
`01-defect-entry-assessment.md` (where bugs enter) · `02-shift-left-and-fast-follow-design.md` (the two lanes).

---

## 1. The finding that reframes everything

We classified ~95 real shipped-bug signatures from the memory index + incident docs. **This factory does
not primarily ship buggy algorithms.** The two largest classes — ~46% of all failures — are not logic errors:

| Class | ~count | What it is |
|---|---|---|
| **State/lifecycle drift** | ~24 | daemon's model of "what's running / claimed / which branch" diverges across restart, /clear, worktree teardown |
| **Mis-diagnosis / false-signal** | ~20 | bugs in how operators/agents *read* the system (tmux mtime, spinner, `queue status:active`) → wrong kills/resets |
| Concurrency / race / startup-only | ~15 | multi-writer, TOCTOU, config-read-once-at-boot |
| Integration / boundary / wire-format | ~13 | e.g. the SSH `#{pane_id}` argv bug whose unit test *encoded the bug as correct* |
| Environment / PATH / disk | ~12 | self-inflicted collisions (disk-watermark cache-wipe), env-strip |
| Spec-gap / missing-config | ~9 | |

**Why this matters:** the A/B/C red→green methodology would barely touch the top ~46%. Those bugs are not
"a fixed defect that might regress" — they are **failure modes nobody named before implementation.** A
regression net catches a bug you already understand; it does nothing for the bug you never anticipated. That
is the exact gap between what the operator asked for (prevention) and what the fleet built (post-bug verification).

## 2. Root cause — why the process passes these through

Beyond the 11 *enforcement* gaps the earlier audit found (suppressed CI, fail-open gates — real, but
downstream), the **process** causes are:

1. **The failure mode is never named before implementation.** Specs are not required to state failure modes,
   boundaries, or concurrency semantics. A pure happy-path spec passes kerf `square`/`finalize`. The
   implementer never handles the failure because the spec never mentioned it. *(Hits ~57% of defect mass.)*
2. **The reviewer shares the author's blind spot by construction.** `agent-reviewer` is self-review, handed
   the same spec+bead the author used; `REQUEST_CHANGES` is author-waivable — only `BLOCK` stops a merge.
3. **Design review is a step, not a gate.** Same-orchestrator, auto-advances after N rounds. No one is forced
   to ask "what shared state/resource do these two features both touch?" — so state/lifecycle drift is invisible.
4. **"Done" = structurally complete + git-merged, never proven-to-work.** `square`/`finalize` run zero tests;
   even reproduce-before-fix is satisfied by a typed test-ID string, not a green run. **Merge is mistaken for
   correctness.**
5. **No canonical liveness contract.** Every session improvises its own wedge heuristic; ~half are wrong —
   which *manufactures* the entire mis-diagnosis class (#2 above).

**The blunt line:** defects enter because the failure mode is never named before implementation, a reviewer
sharing the author's frame waves it through, and merge is mistaken for correctness.

## 3. The shift-left process changes (prevention — the headline)

Ranked by defect-mass leverage. None of these is test tooling; they are structural/process changes.

- **P1 — Failure-modes are a gated spec artifact.** A spec (and each non-trivial bead) must name its failure
  modes, boundaries crossed (shell/exec/git/fs/tmux/ssh), and concurrency/shared-state it touches. Kerf
  `square` blocks without it. Addresses ~57% of defect mass at the cheapest point in the lifecycle.
- **P2 — State-change design review is independent and blocking.** For any bead touching daemon
  dispatch/claim/merge/lifecycle/worktree state, a *different-context* reviewer must answer "what shared
  state does this touch, and what breaks on restart/concurrency?" — blocking, not author-waivable.
- **P3 — Real-boundary / adversarial tests required for cross-process code.** Code whose correctness depends
  on a counterpart re-interpreting its output (a shell re-parsing argv, ssh joining, tmux formats) must be
  tested through the *real* counterpart, never by asserting self-produced bytes. Golden-self tests banned at
  boundaries. *(This is where A/B/C's real-boundary insight is genuinely useful — see §5.)*
- **P4 — One canonical liveness contract.** A single documented, tested definition of "is this session/agent
  alive/wedged," used everywhere. Kills the improvised-heuristic mis-diagnosis class at the source.
- **P5 — "Done" redefined as an epic-acceptance run** (see Lane 1). All-beads-closed stops being acceptance;
  a green end-to-end run on an isolated daemon is.

## 4. The two lanes (mechanism for P5 + the small-merge path)

Full design in `02-shift-left-and-fast-follow-design.md`. Both build on primitives that already exist
(~80% shipped): `scripts/scratch-daemon.sh`, `internal/schedule`, the kerf impl jig, the comms bus.

**Lane 1 — Epic on a branch + acceptance gate.** Epics live on `epic/<codename>`; beads merge to that
branch; `main` is reached by **one human PR at epic end**, not bead-by-bead. A four-bead acceptance block
extends the kerf jig:
- **AT** — expected outcomes / acceptance criteria, **authored up front by planner + captain, NOT the
  implementer** (closes the self-grading hole; this is the contract fixed before code).
- **CR** — independent (different-context) code review.
- **LT** — live-verify: run the epic branch on a **separate scratch daemon in an isolated environment**
  (separate clone / socket / pidfile / tmux / worktrees, keyed off a scratch project-hash — `guard_path` +
  `assert_not_supervised` make targeting the fleet daemon *impossible*; **the production daemon is never
  stopped**), check each AT assertion in the event stream.
- **XT** — exploratory break-testing: 3–5 adversarial agents (concurrency, malformed input, lifecycle-kill,
  boundary, CLI-abuse) actively try to break the running system; findings become **new beads** (deduped);
  P0/P1 block the PR.

**Lane 2 — Async fast-follow (no blocking build).** One `every@15m` scheduler job runs `fast-follow.sh` on
the scratch clone: changed-package `go test -short` + the orphaned `harmonik smoke` (finally given a home),
target <3 min. Failures → deduped P1 bead (`found-by:fast-follow`) + `comms --topic quality` to the captain.
**Non-blocking** — the fleet never stops; a fix-up is dispatched. A small merge that keeps going red is
evidence it was mis-scoped and gets promoted to a Lane-1 epic on the ≥2×-recurrence signal.

**Routing (no new decision):** reuse kerf's existing "is this big enough for a work?" threshold — kerf work
→ Lane 1 epic; trivial/kerf-skip → Lane 2 fast-follow.

## 5. Where A/B/C actually fits (demoted, not deleted)

The A/B/C red→green mechanism is **the post-bug tail, not the headline.** Its correct, bounded home:
- The **DOT bug-repro flow** (operator suspects the DOT bug-handling file exists but is unused — confirm):
  when a bug *is* found, first write a test that shows it red, fix, show it green. A/B/C's "a machine must
  observe the flip" is the right bar *for that step*.
- **P3's real-boundary insight** — test through the real counterpart — is A/B/C's one genuinely
  process-level contribution; it's promoted into P3 above.
- Adopt the lightest sufficient form (Besides "A hardened"): a small proven-regression corpus for bugs we've
  already shipped, run in the fast-follow / release gate. It is a **regression net for known defects** — nothing
  more. It does not prevent the unanticipated failure mode, which is 46%+ of our actual pain.

## 6. Mapping back to the operator's six failure planes (2026-07-04)

| Operator's plane | Addressed by |
|---|---|
| No rigorous kerf test gate | P1 (spec failure-modes) + P5/Lane-1 acceptance gate |
| Weak unit-test rigor | P3 (real-boundary tests) |
| No epic scenario acceptance | Lane 1 (AT + LT beads) |
| No system-level "does it work" | Lane 1 LT (scratch-daemon run) + XT exploratory |
| Broken release testing | Lane 2 fast-follow + smoke; prior PLAN Phase 2 |
| Scenario tests maybe never run | prior PLAN Phase 1 + scheduled gb-mbp run |

## 7. Sequencing

1. **Lane 1 can start now** — reads the event stream directly; scratch harness + scheduler already ~80% built.
   Prove it on ONE real epic before generalizing.
2. **P1 (spec failure-modes)** is the single highest-leverage change and cheap — pilot it on the next kerf work.
3. **Lane 2 reds are only trustworthy after** the prior `2026-07-04-quality-loop` PLAN Phase 0/1 un-suppresses
   the fabricated green (else the async job reads a lie). Sequence Lane 2 behind that.
4. P2/P4 are process-doc + gate-wiring; P3 rides on P1.
5. Do NOT file this as a single mega-epic. Pilot P1 + Lane 1 on one epic, learn, then scope the rest.

## 8. What we are explicitly NOT doing

- Not treating A/B/C red→green as the answer — it's the post-bug tail (§5).
- Not stopping the production daemon to test — isolated scratch daemon only (Lane 1 LT).
- Not a 10–20 min blocking build on small merges — async fast-follow (Lane 2).
- Not filing a mega-epic before piloting P1 + Lane 1 on one real epic.
