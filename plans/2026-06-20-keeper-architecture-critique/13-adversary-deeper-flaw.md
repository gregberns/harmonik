# 13 — Adversarial Verifier: The Deeper Flaw

**Stance (assigned):** Landing the stranded fixes (`89852bb3` + ACK protocol) will NOT fix the keeper. The design is the wrong *shape*; incremental fixes will keep regressing. The synthesis's #1 recommendation is **too soft**.

**Verdict up front:** The synthesis is **TOO SOFT, with one load-bearing factual error that inverts its own recommendation.** Rec #1 ("land the stranded fixes first; it may resolve most symptoms") is built on a misread of what `89852bb3` actually does. The commit does **not** close the open loop and does **not** touch the path that produces the operator's worst live failure. Landing it is fine — it is a genuine improvement to the *manual* command — but selling it as "may resolve most symptoms before any redesign" is the exact "looked right at the time" pattern the project's own review gate exists to stop. The real fix is the radical one (report 11 Alternative A), and the radical path's only cited "fatal flaw" dissolves under inspection.

---

## 1. Why "land the stranded fixes first" is the wrong lead — by the diff, not by vibes

The synthesis treats `89852bb3` as "the −542-line identity + ACK-handshake fix … closes the open loop and collapses identity to one authoritative source." I read the diff. **Three of those four claims are false or overstated:**

### 1a. It does NOT close the open loop. The ACK is never read back.
The "ACK handshake" injects a line and **never reads it.** From `internal/keeper/restartnow.go` (the entire ACK surface in the commit):

```go
if err := inject(ctx, cfg.TmuxTarget, AckLine(nonce, "restart")); err != nil { ... }
...
if err := inject(ctx, cfg.TmuxTarget, "/clear"); err != nil { ... }
...
resumeCmd := fmt.Sprintf("/session-resume %s", handoffPath)
if err := inject(ctx, cfg.TmuxTarget, resumeCmd); err != nil { ... }
```

`grep` for any `capture-pane` / scrollback read / ack-poll in the commit returns **nothing**. The only thing checked is whether `inject()` (a `tmux load-buffer` + `paste` + Enter) returned an OS error — i.e. "did tmux accept the keystrokes," **not** "did the REPL execute them." That is *precisely* report 06's Hazard A: `InjectText` succeeding ≠ `/clear` running. The commit's own comment admits the ACK is for **the agent** to verify ("so the agent can verify receipt"), not the keeper. **It is an open loop with a friendlier label.** The closed loop the synthesis credits it with does not exist in the code.

What `89852bb3` *actually* fixes is narrower and real: it removes the marker→poll→wrong-CWD indirection (the silent no-op, C5) and makes the **CLI exit non-zero** on a reachability failure. That converts a *silent* no-op into a *loud* one. Genuinely good. But "fails loudly" is an operator-ergonomics fix, not a reliability fix — the destructive `/clear`+`/session-resume` are still fired into an unobserved REPL fenced by the same `submitSettle` guesses (report 06 Hazard B/C/D, untouched).

### 1b. It does NOT touch the path that produces the worst live failure.
`89852bb3` rewrites **only the manual `restart-now` / `ping` path.** I confirmed `MaybeRun`, `runCycle`, and `RunForPrecompact` are **unchanged on main** (cycle.go:536/706/976 still present; the commit deletes only `RunOnDemand`/`runOnDemandCycleTail`/the marker watcher branch). But the operator's live-2026-06-18/20 pain — failure class **C4, the ACT-when-idle loop that re-fires `cycle_id …-000001→-000006` and truncates the handoff to 0 lines** (report 07) — runs through `MaybeRun`→`runCycle`, **the automatic path the commit never touches.** So the headline regressing failure is *outside the blast radius of the stranded fix entirely.* Landing `89852bb3` and declaring "most symptoms may be resolved" would leave the single most-cited recurring architectural failure (C4) running on the exact open-loop machinery report 06 indicts. The synthesis's own evidence (report 07 row C4) says the unmerged fix addresses this; the diff says it does not.

### 1c. "Collapses identity to one authoritative source" — partially, in one command only.
`restartnow.go` reads `ReadCtxFile` + `IsPrimarySID` and refuses non-primary SIDs. Cleaner, yes — but the multi-writer `.ctx/.sid/.managed` plumbing and the heuristic re-bind that `MaybeRun`/the watcher use (failure class C1) are **still on main.** Identity is collapsed *for the manual command's pre-flight check*, not for the subsystem.

### Why THIS fix is not different from the 69% that regressed
Report 07's headline: ~69% fix-of-fix regression on the identity/liveness machine, and **the loop only stopped when commit 93f7000e *deleted* the inference layer.** The mechanism of that regression is structural: every fix *adds a branch to the same open-loop, multi-writer paradigm.* `89852bb3` for the manual path **stays inside that paradigm** (still fire-and-forget paste; still `.ctx`-derived SID; still wall-clock settles). It happens to also delete one indirection — which is good and is why it is the *right kind* of commit — but for the automatic ACT-loop it changes nothing, so there is no reason to expect C4 to stop regressing. The one commit that actually broke the regression streak (93f7000e) is cited by everyone as a *deletion*, not a patch. `89852bb3` is a deletion for `restart-now` and a no-op for the loop. **The synthesis generalizes a manual-path improvement into a subsystem-wide cure. That is the soft read.**

---

## 2. Steelman: DELETE the in-place /clear cycle, don't fix it

Report 11 Alternative A and report 01 §5 both arrive here independently; I push it harder.

**The keeper's hardest, most-failing component (the in-place `/clear`→`/session-resume` paste cycle) exists to avoid a thing the team *already does manually and cheaply*: stop the session, start it from HANDOFF.** Three citations, all from the artifacts:

- `known-workarounds.md:57` (per report 01 §5): the *actual operational practice* when a crew fills is `crew stop` → `crew start` with a fresh mission. Zero keeper, zero paste, zero `/clear` race. **The fragile mechanism is competing with a two-command procedure that already works.**
- Report 01 §5 + SKILL §crew-restart: in-flight work is durable in beads (`assignee == crew_name`), the named queue keeps draining on the daemon independent of the session, and identity re-anchors from the HANDOFF block + `HARMONIK_AGENT`. **The session is already disposable.** "Keep this exact session alive across /clear" solves a problem the architecture dissolved.
- Report 11 §A: a from-scratch respawn (`respawn.go` / `ForceRestartFn`) has a **deterministic, observable outcome** (new pid, new session boots reading HANDOFF) — the opposite of a blind paste into a live REPL. `respawn.go` already exists and is already wired for the dead-pane self-heal.

So the radical move is not speculative new machinery — it is **deleting the open-loop cycle and routing overflow through the restart path the keeper already has and the operators already trust.** In-place `/clear` is fundamentally the wrong mechanism because it is *irreducibly open-loop on an interactive REPL*: you cannot get a synchronous, observable "the command executed" out of a tmux paste without reading the pane back, and even then you are screen-scraping a UI. Checkpoint-and-restart replaces "did my keystrokes land?" (unobservable) with "did a process exit and a new one boot?" (a pid check). It trades a UI-automation problem for a process-lifecycle problem — and harmonik is *already a process-lifecycle engine* (the daemon, the supervisor, the respawn path). **The keeper is doing UI automation in a system whose entire competence is process supervision.** That is the shape error.

---

## 3. The simpler truth the critics under-weighted: does the keeper need to exist for MOST sessions?

Report 11 demotes the keeper to "warn-only + restart-on-overflow." Report 01 §5 asks whether it's needed at all. I'll state the sharper version: **For the warn function the keeper is barely needed, and for the act function it is actively harmful relative to the alternatives.**

- **Native auto-compaction now exists** (report 11 §A: Claude Code interactive auto-compaction; server-side `compact-2026-01-12`; context-editing GA-beta on the current model line). The keeper's `PreCompact` hook **exits 2 to BLOCK it** so the custom cycle can run. So the keeper is spending its entire complexity budget *fighting* a native mechanism that ships for free — on the unproven premise that custom `/clear`+handoff beats native summarization.
- **Intent does not live in the conversation.** It lives in beads + comms + HANDOFF + events.jsonl — all durable, all survive any compaction or restart. The keeper's whole reason to puppeteer the live pane (preserve conversation fidelity across `/clear`) is over-indexed on the *one* store that is reconstructible from the other four.

**Irreducible value of a keeper, if any:** exactly one thing — a **gauge + a hard-overflow dead-man's-switch.** Notice when context is about to wedge the pane and, if nothing else has acted, kill-and-respawn from HANDOFF. That is report 11's Alt-A safety net, and it is ~30 lines of "read pct, compare, call existing respawn." Everything else (the cycle, the anti-loop state machine, the SID re-resolution, the operator-attached gate, the gates, the journal, crash-recovery) exists **only to make the in-place cycle work** — delete the cycle and all of it evaporates (report 11 §A "what it eliminates"). The keeper that *should* exist is a watchdog, not an actuator.

---

## 4. "Govern with a spec" is necessary but NOT sufficient — and could entrench the error

Rec #4 ("finalize ONE spec into specs/ with RED-tested invariants before further keeper work") is correct *as sequencing* but dangerous *as content if applied to the current shape.* Report 01 §1 is decisive here: there are already **two specs that contradict on the load-bearing invariant (identity)**, a skill file that contradicts *itself* on threshold flags in the same document, and **four live threshold numbers** in circulation. The problem was never an absence of spec text — it was that **the objective itself was never decided**: in-place-keep-alive vs. checkpoint-and-restart (report 01 §2, §6).

**A spec written before that fork is decided just codifies the wrong architecture with more authority.** RED-tested invariants for an open-loop paste cycle would lock in the open-loop paste cycle — and then every future deletion (the thing that actually fixed the regression, 93f7000e-style) becomes a *spec violation* requiring re-litigation. Governance applied to the wrong shape is *worse* than no governance: it raises the cost of the deletion that the evidence says is the cure.

The correct ordering is: **(1) decide the objective (the fork in report 01 §2 / report 11's premise question), (2) delete to that objective, (3) THEN spec the small thing that remains.** The synthesis lists #4 (spec) and #3 (premise question) as co-equal recommendations with #3 framed as "the real fork to be pressure-tested." That ordering is backwards: #3 is a *prerequisite* for #4, not a parallel option. Speccing first risks freezing the architecture before the fork is resolved.

---

## 5. Is there a fatal flaw in the radical path? (I went looking — there isn't one that survives)

The synthesis's strongest defense of "keep the keeper" is the operator's original objection: *native compaction is too lossy.* I tried to make this fatal and could not:

- It is **the operator's stated objection, not demonstrated evidence.** Report 11 §A correctly reclassifies it from "a given" to "a testable hypothesis." No artifact in this corpus shows native compaction losing intent that beads/comms/HANDOFF didn't already hold.
- The radical path **de-risks it for free**: Alt-A keeps the HANDOFF write and the restart-from-HANDOFF net, so even if native compaction is lossy, the fallback is a *clean restart from a file the agent authored* — strictly better than a half-landed in-place `/clear` that loses the conversation AND fails to resume.
- The one real cost — a hard-overflow restart loses uncommitted in-flight REPL state — is **identical to the risk the current force-clear already carries** (report 11 §A "what it costs"). So it is not a *new* loss; it is the *same* loss with an observable outcome.

The only genuine residual risk is **B (self-handoff) being probabilistic** — but report 11 already scopes B as an optional happy-path complement, not the load-bearing piece. Alt-A's safety net is fully deterministic (gauge read + respawn). **No fatal flaw found.** The radical path is more conservative than it looks because it leans entirely on mechanisms that already ship and already work (warn inject, respawn, durable stores).

---

## Verdict & what I would actually do

**The synthesis is TOO SOFT.** It correctly diagnoses the architecture (the open-loop / multi-writer / infer-don't-be-told root) but then recommends a remedy (#1) that does not act on its own diagnosis: `89852bb3` neither closes the loop (ACK is never read back) nor touches the automatic ACT-loop (C4) that is the live recurring failure. Promoting it to "may resolve most symptoms" repeats the project's documented #1 anti-pattern (the "looked right at the time" merge the review gate exists to catch).

**Concretely, in priority order:**

1. **Land `89852bb3` — but bill it correctly:** "removes the silent-no-op from the *manual* `restart-now` and makes it fail loudly." NOT "fixes the keeper." Do not let it close the investigation. (Reversible, low-risk, real — agreed with synthesis on the *action*, not the *framing*.)
2. **Decide the objective NOW (don't defer it as a parallel "open decision"):** in-place-keep-alive is refuted by its own failure history; pick **checkpoint-and-restart** (report 11 Alt-A). This is the fork in report 01 §2; it gates everything else.
3. **Delete the in-place cycle.** Strip `runCycle`/`MaybeRun`'s act-path, the anti-loop state machine, SID re-resolution, the gates, the journal, crash-recovery, and the PreCompact exit-2 block. Keep: gauge, warn-line inject, and the existing `respawn.go` hard-overflow net. This is the 93f7000e move applied to the *whole* cycle, not just `restart-now`.
4. **Run the validation gate from report 11 §A** (one captain/crew full session, cycle disabled, native compaction allowed; confirm re-grounding via comms/beads) BEFORE writing any spec.
5. **THEN spec the residue** — a ~30-line watchdog — into `specs/`. Speccing before step 2 entrenches the wrong shape.

The keeper should end as a **watchdog over a process-lifecycle system, not a UI-automation actuator fighting native compaction.** Landing the stranded fix is a footnote on the way there, not the headline.
