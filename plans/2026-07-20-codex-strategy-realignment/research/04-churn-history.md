# 04 — Churn History: the Codex-as-a-crew unblock saga (2026-07-17 → 07-19)

Scope: what actually happened across four gates and six commits while captain +
admiral + crew `yankee` tried to unblock "Codex-as-a-crew" so the prod codex daemon
could reboot. Grounded in commit SHAs, bead ids, and the gate docs under
`runs/codex-substrate-validation/` and `plans/2026-07-17-assessor-daemon-campaign/`.

---

## Bottom line

The effort was trying to prove one thing — that a run on `HARMONIK_SUBSTRATE=codexdriver`
can, on a real remote worker (gb-mbp), do a full DOT bead end-to-end: **codex implements
and lands its own commit**, then a **claude review node reaches `agent_ready` and the bead
closes** — before rebooting the prod codex daemon onto that substrate. After four gates and
six fixes, exactly **half is solved**: codex's own commit now lands (Wedge A, GREEN at gate
#4, real commit `46f8f0b`), but the claude review node **still never reaches `agent_ready`**
on the remote worker (Wedge B, RED at every gate). The prod reboot was never re-opened; the
lane is HOLDING on `phase1-session-restart-substrate`, both fix beads (`hk-daegv`, `hk-qxvc2`)
still OPEN, with a fanout recommended but never launched. Net: real, hard, layered progress on
one wedge — but the operator's read ("all these partial solutions, can't tell what's done") is
accurate, because the whole effort assumed the sandbox + remote-worker + DOT-review architecture
was correct and never questioned it.

---

## Timeline

**2026-07-19 ~18:18Z — DEPLOY-NOT-TEST directive** (`direction-log.md`). Operator, via admiral:
validate the codex substrate in isolation on gb-mbp before any prod reboot; prod stays untouched.

**Pre-gate: the remote-cwd spawn seams (merged to main).** Two separate spawn bugs were fixed and
merged *before* the substrate gate even ran:
- `hk-czb11` (PR#32, `f8d3a42e`) — codexdriver app-server ssh spawn set LOCAL `cmd.Dir` to the
  remote worktree path → `fork/exec ssh: ENOENT`. CLOSED.
- `hk-fufel` (PR#33, `9db85569`) — the *handler direct-exec* path (the path codex's per-bead run
  actually takes; codex is `SessionIDCaptured` so the substrate is force-nil'd) had the identical
  bug on a distinct seam. CLOSED. Proven by real ssh E2E on gb-mbp.
- Ancestor spawn fixes: `942069eb` (hk-czb11 remote-cwd ssh spawn), `3e8a96a1` (hk-fufel
  remote-cwd direct-exec).

**GATE #1 — assessor baseline gate @ `9db85569` (== main) = BLOCK**
(`runs/codex-substrate-validation/VERDICT.md`; runs `019f7bbc`, `019f7bc5`).
Leg A (local, self-contained) GREEN 3/3: fail-closed isolation guard + ssh git-lifecycle (stub
handler). Leg B (live gb-mbp) is the decider and FAILED on two independent defects, filed:
- `hk-daegv` (P0→P1) — remote codex runs `sandbox_mode=workspace-write`; codex's own `git commit`
  is seatbelt-denied ("Operation not permitted"), exits 0 with `commit_landed=false`. Daemon
  fallback commits the patch to `run/<id>` but it never merges/closes.
- `hk-qxvc2` (P1) — pinned claude review node stalls to `agent_ready_timeout` (201s / 189s across
  two runs) **even though gb-mbp is claude-onboarded**. Review is the sole close-edge → every DOT
  bead blocked. Confirms `hk-g5wkt`.
- `hk-wwyse` (P2, later CLOSED as dup of hk-daegv) — codex `exec_command` `/bin/zsh` shell-spawn
  `CreateProcess` failure, a second facet of the same sandbox restriction.

**Fix round 1 landed** (captain-lanes ~19:59Z): `a0619c1c` (hk-daegv: force
`-c sandbox_mode=danger-full-access`) + `fff3d937` (hk-qxvc2: isolate remote `CLAUDE_CONFIG_DIR`
via `PrepareIsolatedClaudeConfigDirVia`, seed worker's own `~/.claude.json`). Both independent
agent-reviewer APPROVE, all unit gates green.

**GATE #2 — assessor RE-GATE @ `fff3d937` = BLOCK** (`REGATE-VERDICT.md`; run `019f7bfd`; after a
keeper-restart). Leg A PASS 3/3; Leg B FAIL again. `ps eww` forensics gave sharp, INDEPENDENT root
causes (refuting the initial "downstream/linked" guess):
- hk-daegv: the `danger-full-access` flag *is* delivered but worker **codex-cli 0.142.0 does not
  honor it** — the "flag accepted" check had been run on 0.144.5 *local*. Version/path mismatch =
  passed-review-failed-live.
- hk-qxvc2: the isolated `.claude.json` *is* seeded, but `CLAUDE_CONFIG_DIR` is **absent from the
  live remote claude env** (pid 98702) — `LaunchSpec.Env` is never injected into the remote
  `/usr/bin/login zsh -c 'exec claude …'`; ssh drops client env → claude reads shared
  `~/.claude.json` → re-wedges (188s).
- **TRIPWIRE declared:** another live-BLOCK → major-issue-fanout + admiral surfaces a codex-WIDE
  re-plan to operator.

**Fix round 2 landed** (captain-lanes ~21:22Z): `f907b702` (hk-qxvc2 + hk-okqyx: forward env to
remote ssh agents via `handler.RemoteExecArgv` → `exec env KEY=VAL … <binary>`) + `44831898`
(hk-daegv: pivot off dead-end danger-full-access to least-privilege
`runtimeWorkspaceRoots=[cwd,<repo>/.git]` stamped on app-server thread/start+thread/resume). Both
APPROVE.

**GATE #3 — yankee live gate @ `44831898` = BOTH RED** (`LIVE-GATE-yankee.md`; run `019f7c46`).
**TRIPWIRE MET** — but did NOT fire cleanly. Both wedges had now survived 2 fix attempts each.
Signal, not zero-progress:
- Fix A ADVANCED — `CLAUDE_CONFIG_DIR` now present in the live reviewer argv (env-forward works).
  But review STILL times out (185s). Root cause SHIFTED and was pinned from preserved evidence:
  under `codexdriver` the claude review node is driven by the **codex app-server JSON-RPC** driver;
  its first prompt was a literal `{"method":"initialize","clientInfo":{"name":"harmonik/codexdriver"}}`
  frame. Claude read it as chat, replied in prose, never emitted `agent_ready`. A **substrate
  mis-route**. Also: hook reverse-tunnel `tcp:59963` was NOT listening on the worker.
- Fix B still `commit_landed=false`. Root cause refined: `44831898` stamps writable-roots only on
  the **app-server thread/start** path, but the implement node ran via **`codex exec`**, which never
  received the grant. `<repo>/.git` absent from the sandbox → EPERM on `index.lock`.
- Evidence conflict: a legacy pre-restart sub-agent `rm -rf`'d the entire iso harness as a hygiene
  step, violating captain's 21:36Z "preserve as fanout evidence" order (irreversible; key rollout
  jsonl survived).

**Fix round 3 landed:** `49d7fde3` (hk-daegv: switch the *codex-exec* argv to
`-c sandbox_mode=workspace-write` + `-c sandbox_workspace_write.writable_roots=[cwd,<repo>/.git]`;
verified on both 0.144.5 and 0.142.0 via `--strict-config`) + `37651569` (hk-qxvc2: add a dedicated
`reviewerSubstrate` — route claude `SessionIDMinted` reviewer/gate nodes onto the tmux substrate,
not the JSON-RPC driver).

**GATE #4 — yankee live gate @ `37651569` = Proof B GREEN, Proof A RED → HARD STOP**
(`GATE4-VERDICT.md`; run `019f7cad`).
- **Proof B (hk-daegv) GREEN.** `commit_landed=true`, real codex commit `46f8f0b "Add counter
  function"` (NOT daemon fallback). Wedge A solved on the real `codex exec` path.
- **Proof A (hk-qxvc2) RED.** Routing fix WORKED — a tmux session was created on the worker at the
  `reviewer_launched` instant (claude routed onto tmux, not JSON-RPC). But **necessary, not
  sufficient**: review claude still never emits `agent_ready` (stall 208s → timeout 150s →
  run_failed). Residual wedge is DOWNSTREAM of routing. HARD STOP invoked, NO 4th patch; iso harness
  LEFT UP; surfaced to captain for re-plan. yankee → HOLD.

**2026-07-20 — operator paused the whole thing** (`README.md`) and opened this realignment plan,
questioning the sandbox requirement, the remote strategy, and why local codex still isn't hooked up.

---

## The two wedges

### Wedge A — codex cannot land its own commit (`hk-daegv`) → SOLVED (GREEN gate #4)

Confirmed root cause: a harmonik linked-worktree's git **common dir** (`<repo>/.git`, holding
`worktrees/<id>/index.lock`, objects, refs) lives OUTSIDE the `workspace-write` writable root (the
worktree cwd), so codex's own `git commit` hits EPERM on `index.lock`.

Attempts (each correct-but-incomplete, peeling one layer):
1. `a0619c1c` — force `danger-full-access`. RED @ gate #2: worker codex-cli 0.142.0 + ChatGPT auth
   pins the effective seatbelt to `workspace-write` and ignores the override. Dead end, kept inert as
   forward-intent per [[no-external-version-binding]].
2. `44831898` — least-privilege `writable_roots=[cwd,<repo>/.git]` on app-server thread/start+resume.
   RED @ gate #3: the implement node runs via `codex exec`, not the app-server thread path, so the
   grant never applied.
3. `49d7fde3` — same `writable_roots` on the **`codex exec`** argv. **GREEN @ gate #4.**

Status: **SOLVED / proven green.** Bead `hk-daegv` still OPEN (daemon owns terminal transitions;
daemon is down). `hk-okqyx` (codexdriver env-forward) folded into `f907b702`. The `hk-wwyse`
shell-spawn facet did not block the green commit at gate #4.

### Wedge B — claude review node never reaches `agent_ready` (`hk-qxvc2`) → STILL RED

Three DIFFERENT root causes surfaced in succession; each fix was correct for the layer it found and
exposed the next:
1. Hypothesis: onboarding modal wedge. `fff3d937` isolated a remote `CLAUDE_CONFIG_DIR`. RED @ gate
   #2 — the env var never reached the remote claude process (ssh drops client env).
2. `f907b702` — deliver env via `exec env CLAUDE_CONFIG_DIR=… claude`. PARTIAL: env now confirmed
   present in the live reviewer argv, but review still timed out. Root cause SHIFTED to a **substrate
   mis-route** — codexdriver speaks JSON-RPC `initialize` to a claude reviewer that speaks chat.
3. `37651569` — add `reviewerSubstrate`, route claude reviewer onto tmux not JSON-RPC. PARTIAL @
   gate #4: routing verifiably took effect (tmux session created on the worker), but review STILL
   never emits `agent_ready`. **Necessary, not sufficient.**

Current confirmed root cause (as of gate #4): **NOT confirmed.** Prime suspect, per
`GATE4-VERDICT.md` + `yankee-HANDOFF.md`: the **hook reverse-tunnel `tcp:59963` was never stood up on
the worker** for the tmux-substrate remote claude launch — this is literally the *second half of the
gate-#3 diagnosis that was never put in the committed patch*. Secondary candidates: remote
onboarding/trust propagation, or the reviewer getting no task/context. Diagnosis was repeatedly
blocked because the daemon auto-cleaned the worker worktree on `run_failed`, destroying the review
claude's session jsonl — the exact artifact needed.

Status: **STILL RED.** Bead `hk-qxvc2` OPEN. No DOT bead has ever reached `close` on the codex
substrate. Related `hk-g5wkt` (remote onboarding-modal suppression is local-only) OPEN, parked.

---

## Why it churned

Genuine difficulty (be fair): the remote codex environment is **irreproducible in unit tests** — a
real OS seatbelt sandbox + real ssh + a real onboarded worker + a specific codex-cli version. Codex
behaves differently across versions (0.142.0 vs 0.144.5) and across launch paths (`codex exec` vs
app-server thread/start). Each fix legitimately uncovered a real next-layer bug; Wedge A took three
fixes because the writable-root grant genuinely had to be plumbed through three distinct launch
paths. That is onion-peeling on a hard substrate, not flailing.

The avoidable thrash, specifically:

1. **Single thread of work.** captain + admiral + `yankee` were all on ONE lane. captain-lanes shows
   the captain narrating and gating *every* one of yankee's cycles; admiral relaying each gate
   ruling; yankee the only hands on keyboard. No parallelism, despite the **major-issue-fanout**
   protocol existing for exactly this ("wedge survived ≥2 fix attempts → fan out 10-15 agents").

2. **The fanout — the protocol's own escape hatch — was recommended twice and never launched.**
   Tripwire MET at gate #3; recommended again at gate #4. The captain held it under the
   "conserve-Claude" budget rule ("10-15 agents burns Claude") — so the escalation mechanism designed
   to break the wedge was itself blocked by a token-conservation directive
   ([[prefer-codex-for-implementation]]). The one tool for the job was disallowed.

3. **A tripwire that didn't fire.** The TRIPWIRE was declared as a hard escalation condition, but
   when MET at gate #3 the response was to re-negotiate it into "one more targeted cycle" (Ruling A)
   → gate #4 → HARD STOP → HOLD. The soft gate slipped by two full gates before actually stopping.

4. **Descoped fix halves.** The gate-#3 diagnosis named TWO things for Wedge B — (a) substrate
   routing AND (b) stand up the worker hook reverse-tunnel. Only (a) shipped in `37651569`. Gate #4
   was therefore *predisposed* to fail on the missing half, burning a whole gate cycle to re-discover
   a known gap.

5. **Torn-down evidence.** The daemon auto-cleans the worker worktree on `run_failed`, destroying the
   review claude's session jsonl every cycle — the one artifact needed to root-cause Wedge B. At gate
   #3 a legacy sub-agent additionally `rm -rf`'d the entire iso harness against an explicit
   "preserve" order. Evidence was destroyed faster than it could be read, forcing re-runs to
   re-gather what a prior run already had.

6. **Keeper-restarts mid-diagnosis.** Gate #2 was "re-gate after keeper-restart"; the yankee handoff
   is "after keeper-restart #4." Each context-fill restart forced a re-hydration mid-forensics; the
   `LIVE-GATE-yankee.md` correction explicitly notes a *pre-restart* sub-agent had richer evidence
   than the post-restart probes could reconstruct.

7. **Unit-green-but-live-red, twice.** `a0619c1c` and `fff3d937` both passed independent review +
   unit tests and died live — verified against the wrong environment (0.144.5 vs 0.142.0) or the
   wrong code path (app-server thread vs `codex exec`; local env-inject vs remote). The review gate
   structurally cannot catch env/path mismatches; only the slow, evidence-destroying live gate can.
   That is precisely the "partial solutions, can't tell what's done" texture the operator felt.

**The meta-failure the operator flagged:** the entire multi-day effort was spent making a
possibly-wrong architecture *pass a gate* — sandbox + remote-worker + ship-project-and-connect-back +
DOT-review-node — and **never questioned whether that architecture was the right one** (see
`README.md`: is the codex sandbox even a real requirement or self-imposed? is "ship whole project to
remote + connect back" the wrong remote model? why isn't *local* codex hooked up at all?).

---

## Net state at pause

### SOLVED (proven green)
- **Wedge A — codex lands its own commit.** `hk-daegv` fix `49d7fde3`; GREEN @ gate #4
  (`commit_landed=true`, real codex commit `46f8f0b`). Bead OPEN only because daemon owns close.
- **Remote-cwd spawn seams.** `hk-czb11` (PR#32 `f8d3a42e`) + `hk-fufel` (PR#33 `9db85569`) —
  CLOSED, merged to main.
- **Env delivery to remote agents.** `f907b702` — `CLAUDE_CONFIG_DIR` reaches the remote reviewer
  (proven live @ gate #3); also closes `hk-okqyx`.
- **Substrate routing.** `37651569` — claude reviewer routed onto tmux, not the JSON-RPC driver
  (tmux session verifiably created @ gate #4).

### STILL RED
- **Wedge B — claude review reaches `agent_ready` → DOT bead closes.** `hk-qxvc2`, RED at every
  gate including #4. Root cause NOT confirmed; prime suspect = worker hook reverse-tunnel `tcp:59963`
  never stood up (the descoped second half). No DOT bead has ever reached `close` on the codex
  substrate. `hk-g5wkt` OPEN/parked.
- **Prod codex daemon reboot.** Still HELD/BLOCKED (`admiral-initiatives.md` un-gate trigger:
  "Ruling-A codex cycle clears its live gate"). Never re-gated to PASS.

### NEVER ACTUALLY ATTEMPTED
- **The captain-orchestrated major-issue-fanout on Wedge B** — recommended at gates #3 and #4,
  blocked by conserve-Claude, never launched.
- **Standing up the worker hook reverse-tunnel `tcp:59963`** — the descoped half of the gate-#3
  Wedge-B diagnosis.
- **An assessor RE-GATE #3.** Gate #4 was yankee's *own* iso gate, not an independent assessor gate;
  the fresh assessor PASS that would re-open the deploy never ran.
- **Any examination of the operator's actual questions** — the sandbox requirement, the remote
  strategy, and local-codex hookup were never investigated during the churn; the architecture was
  assumed correct throughout.

---

### Source index
- `runs/codex-substrate-validation/VERDICT.md` — gate #1 (assessor baseline BLOCK).
- `runs/codex-substrate-validation/REGATE-VERDICT.md` — gate #2 (assessor re-gate BLOCK).
- `runs/codex-substrate-validation/LIVE-GATE-yankee.md` — gate #3 (yankee, BOTH RED).
- `runs/codex-substrate-validation/GATE4-VERDICT.md` — gate #4 (Proof B GREEN, Proof A RED, HARD STOP).
- `plans/2026-07-17-assessor-daemon-campaign/runs/codex-substrate-validation/{LEG-A,LEG-B,REGATE-LEG-A,REGATE-LEG-B,RUN-LOG}.md` — leg-level evidence.
- `.harmonik/context/captain-lanes.md` (codex-substrate-unblock lane), `.harmonik/context/direction-log.md` (Ruling A + gate rulings), `.harmonik/crew/yankee-HANDOFF.md`, `.harmonik/crew/admiral-initiatives.md`.
- Beads: `hk-daegv`, `hk-qxvc2`, `hk-okqyx`, `hk-g5wkt` (OPEN); `hk-wwyse`, `hk-czb11`, `hk-fufel` (CLOSED).
- Commits: `a0619c1c`, `fff3d937`, `f907b702`, `44831898`, `49d7fde3`, `37651569`; spawn ancestors `942069eb`, `3e8a96a1`.
