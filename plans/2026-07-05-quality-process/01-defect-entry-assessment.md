# Where Defects Enter the Harmonik Factory — A Process Assessment

**Date:** 2026-07-05
**Framing:** The operator's complaint is "the code output is riddled with bugs." This is a
root-cause assessment of WHERE defects enter the plan→spec→implement→review→merge pipeline and
WHY the process lets them through. It is deliberately NOT about verification mechanics. It goes
beyond the 11 "fabricated green checkmark" enforcement gaps already found in
`plans/2026-07-04-quality-loop/PLAN.md` — those are gaps in *proving* work is done; this is about
gaps in the *process that produces the work*.

Evidence base: ~95 real shipped-bug signatures in the session memory index, `docs/known-workarounds.md`,
incident/postmortem docs, the kerf jig specs (`~/.kerf/jigs/{spec,bug}.md`), the `agent-reviewer`
skill, and `build-practices.md`.

---

## 1. Defect taxonomy — what this factory actually ships

Classifying the real historical bugs by dominant root cause (some span two classes; each filed once):

| Rank | Class | ~Count | What it is |
|---|---|---|---|
| 1 | **State / lifecycle drift** | ~24 | Daemon's model of "what's running / what branch / is this bead claimed" diverges from reality across restart, /clear, worktree teardown |
| 2 | **Mis-diagnosis / false-signal** | ~20 | Not a product bug — a bug in how the operator/agent READS the product, triggering wrong recovery (kills, resets) |
| 3 | **Concurrency / race / startup-only** | ~15 | Multi-writer, TOCTOU, read-config-once-at-boot |
| 4 | **Integration / boundary / wire-format** | ~13 | Two components agree on a field NAME but disagree on its format/quoting/encoding |
| 5 | **Environment / PATH / disk / resource** | ~12 | The box, not the code: disk watermark, PATH inheritance, env-strip, fork-bomb |
| 6 | **Spec gap / missing-config** | ~9 | Hardcoded thresholds, required-keys landmine, "new EventType needs wantCount bump" |

**Headline finding: the two largest classes (1 + 2, ~46% of shipped failures) are NOT logic
errors.** They are distributed-state truth problems and human perception problems. The factory's
worst failures come from state drift and misreading live signals — not from the LLM writing a
wrong algorithm. Any "the agents write buggy code" mental model is aiming at classes 3–6, which
are the minority.

### Real signatures per class (representative)

- **State/lifecycle:** `in_progress + claim-skip flood (hk-l2xd1)` — bead stuck in_progress with no
  live run, manual reset double-dispatched a mid-commit run · `mid-flight cancel → phantom run`
  blocks resubmit · `remote gate loop = stale origin/main` · `daemon checkout reverts uncommitted
  tracked worktree files`.
- **Mis-diagnosis:** `tmux window/mtime = false wedge signal` · `live-pane spinner overrules wedge
  diagnosis` · `CPU-saturation masquerades as daemon flakiness` · `queue status:active != running
  worker`.
- **Concurrency:** `concurrent runs shared-file merge-race` · `remote worker registry built at
  startup only` (live config edit is a no-op) · `cache-reaper proactive TOCTOU wipes shared cache`
  · `remote agent_ready breaks under concurrent slots (max_slots>1)`.
- **Integration/boundary:** `SSHRunner #{pane_id} comment-truncation` (unquoted tmux argv
  space-joined by ssh → remote shell reads it as a comment → window never created) · `Pi/ornith
  api wire-format` (wrong enum → 4.5s exit-0 no-commit that mimics an auth hang) · `remote
  review.json ErrMalformed = quit-watchdog kills claude mid-write`.
- **Environment:** `disk<10GiB → daemon's own go clean -cache wipes shared build cache
  mid-build → merge_build_failed` · `ENV-STRIP billing` (inherited ANTHROPIC_API_KEY burned all
  credit in ~2h) · `commit_gate go: command not found` (shell node didn't inherit daemon PATH) ·
  `pi flywheel-ext fork-bomb`.

---

## 2. Where each class enters — and why the process lets it through

The pipeline has FOUR quality gates: (a) the kerf spec passes, (b) a Change-Design review sub-agent,
(c) the per-commit `agent-reviewer`, (d) `kerf square`/`finalize` + git-merge = "done." Here is how
each defect class walks straight through all four.

### Class 1 & 3 (state-drift + concurrency) — enters at DESIGN, never reviewed for it
These are contract bugs: "two writers to one state field," "config read once at boot," "two features
touch one shared resource." They are invisible in a single-path reading of the code and only appear
under restart / N>1 concurrency / feature-interaction.

Root causes:
- **The kerf spec is not required to state concurrency semantics, failure modes, or boundary
  conditions.** The spec-draft pass gate (`~/.kerf/jigs/spec.md`) checks completeness-of-file,
  normativity, and cross-references — never "does this spec enumerate races, failure modes, or
  shared-resource interactions." A pure happy-path spec passes `square` and `finalize` cleanly. So
  the implementer never sees the failure mode because *the spec never named it.*
- **There is a design-review pass, but it is same-orchestrator + bounded-advisory.** The
  Change-Design reviewer is a sub-agent spawned by the same orchestrator, reading the same
  problem-space/research files — it shares the author's framing by construction. Worse, it
  auto-advances after N rounds "even without approval" (jig-system.md). It is a step, not a gate.
- **No one is asked the one question that catches these:** "what shared resource / state field do
  these two features both touch, and who owns the write?" The disk-watermark cache-wipe, the
  proactive cache-reaper TOCTOU, and the live-disable-then-restart-re-enable bug are all
  self-inflicted *feature collisions* — catchable ONLY at design review by that question.

### Class 4 (integration/boundary) — enters at IMPLEMENTATION, tests encode the bug
These need a real second process (ssh, tmux, the model API, a file being read by another writer) in
the loop. Root cause: **testing is happy-path against MOCKED boundaries.** The SSH argv-quoting bug
had a unit test (`TestSSHRunner_NewWindowArgv`) that had *encoded the buggy behavior as correct* — a
mock-boundary test that was worse than no test. Nothing in the process requires a real-process
round-trip, and nothing requires adversarial input at a boundary. The reviewer's test-adequacy check
only asks for "edge cases the bead body implies" — i.e., edge cases the author already thought of.

### Class 2 (mis-diagnosis) — enters at OPERATION, no canonical liveness contract
Not preventable by code review at all — it's the operator reaching a wrong conclusion from a
non-authoritative signal (tmux mtime, pane spinner, queue status:active). Root cause: **there is no
single canonical "is this alive/making-progress" contract**, so every session improvises a liveness
heuristic and half of them are wrong. (The major-issue-fanout skill now encodes "events.jsonl is
authoritative, tmux/mtime/spinner are not" — that contract needs to be the ONE source, everywhere.)

### Class 5 & 6 (env + spec-gap) — enters at IMPLEMENTATION, no spawn-hygiene / registry checklist
PATH inheritance, env-strip, and "adding a new EventType needs a wantCount bump in another file" are
all *checklist-catchable at implementation* but there is no spawn-environment-hygiene checklist and
no "adding X requires touching registries {A,B,C}" landmine doc set (only ad-hoc ones written after
each incident).

### The cross-cutting root cause: the reviewer shares the author's blind spot, and "done" ≠ "works"
- **`agent-reviewer` is self-review by construction.** Its own skill says it reviews "the agent's
  OWN work product," and it is handed *the same spec sections and same bead body the author worked
  from.* It inherits the happy-path framing. It cannot flag a failure mode the spec never named. And
  its main verdict (`REQUEST_CHANGES`) is author-WAIVABLE by writing a rationale sentence in the
  commit body — only `BLOCK` is a hard stop.
- **"Done" is defined structurally, never behaviorally.** `kerf square`/`finalize` run ZERO tests —
  they check that files exist and deps resolve. The daemon closes a bead on git-merge. So "done" =
  "structurally complete + merged," never "the assembled feature was proven to work end-to-end."
  Even the one adversarial-ish requirement — reproduce-before-fix + a scenario test (bug jig +
  `build-practices.md`) — is verified by a *typed test-ID string in a file*, not by a green run
  (PLAN #9, kerf's own incident `hk-37zy8`). The blind spot is baked into the *definition of done*,
  upstream of the fabricated-green enforcement layer.

---

## 3. The highest-leverage PROCESS changes (shift-left)

Ranked by defect-mass addressed. These are structural/process changes, not test-verification tooling.

### P1 — Make the spec enumerate FAILURE MODES + CONCURRENCY, as a required pass gate
Add a required spec artifact: a "Failure modes, boundaries & shared-state" section that must
enumerate (a) each error/edge case with expected behavior, (b) every state field this change writes
and who else writes it, (c) every shared resource two features could contend on. Gate `square` on its
presence and non-triviality. **Hits classes 1, 3, 5, 6 (~57% of defect mass)** by forcing the failure
mode to exist ON PAPER before an implementer or reviewer ever looks — you cannot fix the "reviewer
shares the author's blind spot" problem downstream; you fix it by naming the blind spot in the spec.

### P2 — Make design review INDEPENDENT and BLOCKING for state/concurrency changes
For any change touching daemon state, the RunRegistry, config-load, or a shared resource, the
Change-Design reviewer must be a *genuinely independent context* (different orchestrator / not handed
the author's research framing) whose single mandated question is "what shared state/resource does
this touch and who owns the write?" — and it must be pass/fail, not auto-advance-after-N-rounds.
**Directly targets the largest class (state-drift) and the self-inflicted feature collisions.**

### P3 — Require a REAL-BOUNDARY / adversarial test for any change crossing a process boundary
Ban mock-boundary tests as the sole coverage for ssh/tmux/model-API/multi-writer code; require a
round-trip against the real second process (or a faithful stand-in) AND at least one adversarial
input. **Kills class 4** — and specifically prevents the "test encodes the bug as correct" failure
that made the SSH bug worse than untested.

### P4 — Establish ONE canonical liveness/progress contract and forbid ad-hoc heuristics
Declare events.jsonl heartbeat the single authoritative liveness signal; document that tmux mtime,
pane spinner, and queue status:active are NON-authoritative; route all "is it wedged?" decisions
through it. **Kills the second-largest class (mis-diagnosis, ~20 incidents)** — which is pure wasted
operator cycles and wrong kills, and is invisible to every code gate.

### P5 — Redefine "done" behaviorally: an epic-acceptance run, not structural + merge
`square`/`finalize`/merge must not equal done. Require an epic-level acceptance step that actually
exercises the assembled feature (and, for bug fixes, proves the reproducing scenario test goes
red→green as a *run*, not as a typed ID). Make `REQUEST_CHANGES` non-waivable without an independent
sign-off. **This closes the definitional hole that sits upstream of all 11 fabricated-green points.**

---

## Bottom line

The factory does not primarily ship "buggy algorithms." It ships **state-drift and
feature-collision defects that were never named in a spec, passed through a reviewer that shared the
author's happy-path frame, and were declared "done" by structural completeness + merge rather than by
being proven to work.** Half the remaining pain is operators misreading non-authoritative live
signals. The three cheapest, highest-leverage fixes are all shift-left and all about *naming the
failure before implementation*: (P1) require failure-modes/concurrency in the spec, (P2) make
state-change design review independent and blocking, (P4) one canonical liveness contract. None of
them are test tooling.
