# Keeper Critique — Objective & Requirements Lens

**Critic:** Objective & requirements coherence (does the keeper solve the RIGHT problem; is its goal even coherent?)
**Date:** 2026-06-20
**Verdict (one line):** The keeper's *core need* is real, but its *stated objective is incoherent across artifacts* and its *requirements are demonstrably unstable* — this is not a buggy implementation of a clear goal, it is a moving target with three conflicting sources of truth.

---

## 1. What is the keeper's objective? (And is it stated consistently?)

The keeper has **one defensible kernel**: prevent a long-lived interactive Claude session from overflowing its context window before it can be checkpointed. Every artifact agrees on that sentence. The disagreement is in everything downstream of it, and it is severe.

### 1a. The two specs describe two different products

| Dimension | `session-keeper-spec.md` (original) | `keeper-identity-and-liveness.md` (redesign) |
|---|---|---|
| Framing | A **feature spec**: phased build (Phase 1 warn-only → Phase 2 cycle), gauge → watcher → cycle → anti-loop → compaction backstop (§§3–8). | A **deletion spec**: net-negative LOC, a "deletion checklist" of 11 artifacts + 7 knobs to rip out (§3.2). |
| Identity | Gauge-derived / heuristic-recoverable: re-bind to a new `session_id` minted by `/clear` (§5.3a, §7.2 cond. a). The new id *is* the anti-loop signal. | **Authoritative, launch-supplied, never inferred** (§2). Explicitly forbids deriving the id from the gauge, transcript, or tmux (§I2.3). Directly *contradicts* the original's §7.2(a). |
| `.managed` | Watcher may re-resolve/auto-recover identity. | **Single-writer, written ONCE, no auto-clear ever** (§I1, §I2.2). Bans the exact recovery loop the original implies. |
| Threshold defaults | warn 80 / act 90 (pct), 270k/300k abs (§10). | warn **270k / 0.70**, act **300k / 0.85**, force **340k / 0.95** — pinned as operator HARD-NO (§4). |

These are not two drafts of one spec. The redesign spec exists **to undo** an architecture the original spec specified and the implementation built. The redesign even names the failure mode by which the original was built: a "fix-of-fix pattern" of stacked heuristic recovery layers (§3.2 — "a change that ADDS a layer without DELETING… is NON-CONFORMING"). That is an admission, in normative spec text, that the requirements were wrong and got patched rather than corrected. **A subsystem whose newer spec's primary content is a list of the older spec's mistakes to delete does not have a coherent objective — it has a contested one.**

### 1b. A THIRD source of truth disagrees with both

The live code constants are **200k warn / 215k act** (`internal/keeper/thresholds.go:35-37`). But:
- `keeper-identity-and-liveness.md §4.1–4.2` pins **300k/270k** as an un-moveable operator HARD-NO, with a "defaults-PIN test… RED if any constant moves."
- `.claude/skills/keeper/SKILL.md` (the agent-facing operating contract) documents **270k/300k/340k** in its threshold table and config block.
- The recovery report (`README.md` Bug #1, Bug D) says operator *intent* was a **300k cap**, while `captain-launch.sh` passes pct flags equating to **300k/350k on a 1M window**.

So there are at minimum **four** numbers in circulation (200/215 code, 270/300 spec+skill, 300 intent, 300/350 launch script). The skill file even instructs `--warn-pct 25 --act-pct 30` in its quick-reference (line 375) while simultaneously stating those flags are "inert on [1m] models" (lines 177-178). **An agent reading the canonical operating contract is given a self-contradicting instruction in the same document.** This is the single clearest evidence that the objective is not just inconsistently *specified* but inconsistently *deployed*, and that nobody can currently state what the keeper is supposed to do at what fill level.

---

## 2. Is the core premise sound? (Separate process + tmux paste of /clear→/resume)

The *need* is sound; the *chosen mechanism* is the most fragile option available, and the framing smuggles in unnecessary risk.

- **The premise conflates two unlike problems.** "Don't overflow context" and "drive an interactive UI by pasting keystrokes into a tmux pane" are independent. The first is a state-management problem. The second is a brittle UI-automation problem (timing-sensitive bracketed-paste; the recovery report Bug #3 documents it *live-looping* and *truncating the handoff to 0 lines*). The keeper has welded a hard problem (reliably puppeteering a terminal) onto an easy one (noticing a number crossed a threshold). The hard half is where every operator-reported failure lives (`README.md` §2 bugs 2/3/A/B; memory: "/clear works but is UNRELIABLE — TIMING").
- **It races a human.** The cycle injects destructive `/clear` keystrokes into a pane a human may also be typing into — requiring an "operator attached → warn-only" gate (`cycle.go:128-137`, SKILL §warn-vs-act). A design that must constantly check "is a human about to collide with me" is fighting its own substrate.
- **It depends on a gauge that is OFF by default.** The entire objective is silently inert unless a global, destructive `keeper enable --yes-destructive` has wired a statusLine hook (SKILL §KNOWN DRIFT; `known-workarounds.md:57` — "NOT DEPLOYED FOR CREWS"). An objective that defaults to *not happening* and whose docs openly disagree on whether it is armed is not a stable requirement; it's an aspiration with a manual workaround (`crew stop/start`) that is what people actually use.

**The right framing question the keeper never asks:** is the goal "keep one session alive forever via in-place clears," or is it "make sessions cheap to checkpoint and restart so overflow doesn't matter"? The redesign spec's §6 state machine and the recovery report's preferred fix (`89852bb3`: rip out the marker/poll/journal/nonce state machine, act *directly and synchronously*) both lean toward "make restart simple." The original spec's elaborate anti-loop/cycle-journal/half-cycle-recovery machinery (§5–7) leans toward "keep it alive at all costs." **The project has never committed to which problem it is solving, which is exactly why the requirements keep shifting.**

---

## 3. Are the requirements stable? (No — and instability is itself the flaw)

Documented requirement reversals, each cited:

1. **Identity model reversed.** Original: re-bind to `/clear`-minted id, gauge-derived (§5.3a/§7.2a). Redesign: launch-authoritative, *never* inferred, delete all derivation paths (§I2.3, §I1.2). A 180° reversal of the load-bearing invariant.
2. **Recovery machinery built, then mandated for deletion.** The redesign's deletion checklist D1–D11 removes ~150 LOC of latch/auto-clear/flap-cooldown/suppress/self-recovery (`watcher.go:664-888`) plus 7 config knobs (K1–K7) — all of which were *added* to satisfy earlier requirements. Building then deleting the same machinery is the canonical signature of unstable requirements.
3. **Thresholds churned and contested.** 200/215 (code) vs 270/300 (spec/skill) vs 300 (intent), with an *operator HARD-NO* invoked to *freeze* them (§4.5) — you only need a "HARD-NO, do not move this" invariant when something keeps moving it. The memory note "operator HARD-NO on widening the band; real fix is captain restart-now" confirms this was a live, repeated fight.
4. **restart-now mechanism reversed.** Original/current: write a marker, watcher polls and consumes it on a fresh-gauge tick (SKILL §restart-now). Recovery `89852bb3`: that whole marker→poll→nonce→journal→freshness-gate path is "ripped out"; restart-now acts directly. The marker architecture was a requirement; now its removal is the fix.
5. **Liveness requirement discovered late.** Bug B (`README.md`): restart-now reports success even when *no keeper exists to consume the marker*. The ACK-handshake (§4 of recovery) is a brand-new requirement — "prove the keeper is alive" — that the original objective never contemplated. A core requirement surfacing this late means the objective was under-specified from the start.

Five reversals on the load-bearing requirements (identity, recovery, thresholds, restart mechanism, liveness) is not iteration — it is an objective that was never pinned down.

---

## 4. Is the keeper scope-crept? (Yes — it is at least four subsystems wearing one name)

Enumerated from the code surface (brief §"Code surface") and SKILL command table, the single `harmonik keeper` owns:

1. **A gauge** (statusLine hook writing `.ctx`) — observability.
2. **Session-identity binding** (`.managed`, sessionid.go, the entire redesign spec) — identity management.
3. **tmux target resolution** (tmuxresolve.go, provenance convention).
4. **Threshold evaluation** (thresholds.go, gates.go) — policy.
5. **An interactive-pane puppeteer** (injector.go bracketed-paste) — UI automation.
6. **A handoff→clear→resume state machine + crash recovery + anti-loop journal** (cycle.go §5–7) — workflow orchestration.
7. **Dispatch-state coordination** (set-/clear-dispatching markers) — cross-agent coordination with the daemon queue.
8. **A respawn supervisor** (respawn.go, `--respawn-cmd`) — process supervision, overlapping `internal/supervise`.
9. **A compaction backstop** (PreCompact hook, §8).
10. **Statusline rendering** (keeper-statusline.sh).
11. **Per-role behavior matrices** for captain vs crew vs flywheel vs orchestrator (SKILL §warn-vs-act, §crew, §captain) — each with different warn text, nonce flows, and self-restart contracts.

The CLI alone has **~1900 LOC across three command files** (`keeper_cmd.go` 569, `keeper_enable_doctor_cmd.go` 1163, `goalkeeper_cmd.go` 221). The `enable`/`doctor` pair (1163 LOC) is essentially a *settings.json migration tool* that has nothing to do with watching context — it's bundled because the gauge can't function without it. **These do not cohere into one responsibility.** Observability (gauge), identity, policy (thresholds), UI automation (inject), and workflow (cycle) are five separable concerns; the redesign spec implicitly concedes this by trying to amputate identity-management cleanly out of the rest. A subsystem you can cleanly delete 150 LOC + 7 knobs + a whole CLI command from (`keeper rebind`, D11) without touching the kernel was carrying weight that was never part of the objective.

---

## 5. Could the underlying need be met WITHOUT a keeper at all?

Plausibly yes, and the evidence inside these very artifacts points to cheaper designs:

- **The fallback already works.** `known-workarounds.md:57` says the *actual* operational practice when a crew fills is `crew stop` → `crew start` with a fresh mission. That is the need met with zero keeper, zero tmux puppeteering, zero `/clear` race. The keeper's elaborate in-place cycle is competing with a two-command manual procedure that already satisfies the objective.
- **The daemon already owns durable state.** SKILL §crew-restart confirms in-flight work is durable in beads (`assignee == crew_name`) and the named queue keeps draining on the daemon independent of the session. If the session is *already* disposable and re-hydratable from durable state, then "keep this exact session alive across /clear" is solving a problem the architecture already dissolved. The need is "don't lose intent," and a handoff file + clean restart delivers that without puppeteering a live pane.
- **PreCompact backstop alone** (§8) plus a handoff hook arguably covers the overflow case: block auto-compaction, write a handoff, let a supervisor restart. That removes the gauge, the watcher poll, the threshold policy, and the tmux inject — the four most failure-prone components — in favor of an event-driven boundary the harness already provides.

The keeper, in short, may be an *over-engineered substitute for "checkpoint + restart,"* preserved because early on a session restart felt expensive. The recovery report's own conclusion ("sound core, no full rewrite warranted, surgical simplification") is the *implementation* verdict; the *objective* verdict is harsher: **the most complex parts of the keeper exist to avoid a restart that the rest of the system already makes cheap.**

---

## 6. Severity & summary judgment

- **Objective coherence: FAILING.** Two specs that contradict on the load-bearing invariant (identity), a skill file that contradicts itself on threshold flags, and four live threshold values. No single artifact states "the keeper does X at Y, full stop."
- **Requirement stability: FAILING.** Five documented reversals on load-bearing requirements; a "HARD-NO defaults-PIN" invariant whose existence proves the churn.
- **Scope: FAILING.** ≥11 responsibilities, ≥4 separable subsystems, ~1900 LOC of CLI of which the largest file is unrelated to the watch objective.
- **Premise: PARTIAL.** The need is real; the tmux-puppet mechanism is the most fragile realization of it and conflates UI automation with state management.

The keeper is not primarily failing because of bugs. It is failing because **no one has ever locked what it is for**, so each fix re-litigates the objective, stacks a heuristic, and ships a new contradiction. The correct next move is not another fix — it is to *decide the objective first*: in-place-keep-alive vs. checkpoint-and-restart. Until that is decided, every implementation will keep breaking, because the spec it's built against keeps reversing.
