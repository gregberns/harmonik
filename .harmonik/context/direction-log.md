<!-- TIER: 2 (operational state, sequencing intent across direction changes)
     LOADED BY: admiral + captain @ boot, AFTER tier-3 (project.yaml) + tier-2 (captain-lanes.md), BEFORE acting.
     OWNER: admiral. APPEND-ONLY. ONE entry per direction CHANGE (never a status update, never per-tick, never by crews).
     Newest-first. ~3-5 lines/entry. Capped ~10 entries / ~60 lines; delete oldest on overflow (no archive).
     Four load-bearing fields per entry: WHAT / WHY / RETURN-PATH(sequence) / expires:.
     ON EXPIRY the DEFAULT is LAPSE -> revert to the standing autonomous posture, NEVER a hold.
     The admiral audit OWNS flagging an expired-but-present entry: re-confirm with the operator or strike it.
     See .harmonik/context/AGENTS.md for the full forced-write/forced-read discipline. -->

# Direction log — temporal sequencing intent across direction changes

> The one thing no other doc holds: WHY we paused X for Y and IN WHAT ORDER we resume.
> This is what a fresh /clear destroys. Read the newest RETURN-PATH as ground truth for sequencing.

## 2026-07-06 — captain (operator-directed clean slate) · expires: 2026-07-09T00:00:00Z
WHAT: Captain's crew (jessica, duncan, stilgar, watch) TORN DOWN — sessions stopped, registry
      cleared, keepers killed, dead flywheel session killed. Live registry = admiral + shannon +
      schmidhuber only (admiral's new initiative). No work orphaned (fleet HELD, zero runs).
WHY:  Operator wants a clean deck before the admiral's significant new initiative; the old lanes'
      critical-path work had largely landed. Fresh captain missions to be written on revive.
ORDER: hold clean-slate → admiral stands up its initiative → operator directs new captain crew.
RETURN-PATH: revive from plans/2026-07-06-crew-teardown/PARKED-INITIATIVES.md (per-crew open threads);
             open item = eval-metrics epic hk-9jdid still OPEN (confirm scope vs close).

## 2026-07-05 ~06:30Z — captain (executing 18:10Z directive) · expires: 2026-07-08
WHAT: hk-hs7ex concurrency-split LANDED (salvaged off a hung gb-mbp remote consolidate → main 60708048) + daemon
      REDEPLOYED to 60708048 (split-gate + bounded-reap + keeper hk-5266t). CORRECTED a handoff error: gb-mbp was
      actually enabled:true max_slots:6 (not disabled), which is why the critical bead hung remotely. Dialed gb-mbp
      back to max_slots:1 for a serialized re-validate on the fixed binary before re-ramping.
WHY:  the deployed binary lacked the reap bound → the remote consolidate hung silently; now fixed. Serialized-first
      re-validate follows the 18:10Z "quiet-window re-validate before ramp" order rather than trusting the prior 6.
ORDER: serialized (1-slot) gb-mbp canary on 60708048 → confirm full remote path lands incl consolidate → ramp 1→6
       → drive to 10 → fill remote slots. Split-gate keeps local ≤4 independently. Local pinned 4.
RETURN-PATH: resume by checking: gb-mbp canary landed clean on 60708048? max_slots ramped back toward 6/10? remote slots filling?

## 2026-07-04 ~18:10Z — operator (via admiral) · expires: 2026-07-08
WHAT: CORRECTION to the ~17:00Z posture, part (2) THROUGHPUT. The LOCAL box can run ONLY 4
      concurrently — that is a HARD ceiling, NOT a disk-pressure artifact and NOT to be raised.
      Do NOT `set-concurrency 10` locally and do NOT bump config max_concurrent past 4. The
      5-10 concurrency target lives ENTIRELY on gb-mbp (the remote machine): run 10 THERE.
      That makes gb-mbp safe re-validation the SOLE path to higher throughput — it is now the
      throughput critical path, not an optional offload. Local stays pinned at 4.
WHY:  operator states the local machine cannot sustain more than 4 concurrent runs; pushing past
      it just re-trips failures. Remote gb-mbp is the capacity. Disk recovering (28 GiB free) does
      NOT change this — it was never the real local cap.
ORDER: keep local at 4 -> bring gb-mbp back SAFELY (root-cause the idle-hang/launch-gap, serialized
       quiet-window re-validate) -> once stable, drive concurrency to 10 ON gb-mbp -> fill remote
       slots from the READY backlog. gb-mbp = throughput critical path.
RETURN-PATH: directed captain over comms (topic directive). Resume by checking: local still 4?
      gb-mbp re-validated + running toward 10 concurrent? remote slots filled from backlog?

## 2026-07-04 ~17:00Z — operator (via admiral) · expires: 2026-07-08
WHAT: Day's operating posture (medium-term, 5 parts). (1) AGENT-MANIFEST: roll out + STABILIZE by ~noon PDT
      (19:00Z). Remediation epic hk-bl93n is 3/4 landed (skill-bodies f8f09a28, manifest-fields 2a5ff76d,
      comms-instructions in flight); GATE = crew-boot wiring hk-ncg9m (wire `start crew` -> `agent brief`)
      still in flight w/ jessica — throw MORE crews at it if it stalls. THEN verify a real crew boots from
      soul.md + run the behavior observation (plans/2026-07-04-agent-manifest-rollout-retro). (2) THROUGHPUT:
      keep the system running 5-10 jobs CONCURRENTLY — raise daemon max_concurrent 4->10 (config.yaml:33
      durable) AND lift the spawn cap (live `set-concurrency 10` currently returns spawn_cap_exceeded); fill
      slots from the READY backlog. (3) MODEL SPREAD (intelligent, a little each): Claude + Codex run
      CONCURRENTLY via per-bead `harness:` label. dgx/ornith is LIVE + VERIFIED (ssh gb@dgx OK + curl
      http://dgx.local:8551/v1/models returns model `ornith`, 2026-07-04 — NOT gated, the old SSH-gate is
      CLEARED). NOTE the Pi harness is currently ONE model daemon-global per pass (MiniMax openrouter
      minimax/minimax-m3 vs dgx/ornith), so today they rotate; whether that single-model limit is config-only
      or a systematic restriction is UNDER INVESTIGATION (operator: mid-priority; problematic if systematic).
      (4) REMOTE gb-mbp: bring it back online SAFELY — captain disabled it TODAY 17:14Z after the idle-hang/
      launch-gap RECURRED (4 runs silent 18-60m); root-cause + serialized quiet-window re-validate BEFORE
      volume. (5) FOCUS: finish partially-complete epics before opening NEW initiatives (many open P1/P2 half-done).
WHY:  operator wants sustained multi-model throughput at 5-10 concurrency, the manifest actually reaching
      crews, existing initiatives DRAINED rather than more started, and dgx/ornith in the model mix now.
ORDER: manifest gate hk-ncg9m -> raise concurrency+spawn-cap & fill slots (Claude || Codex) -> gb-mbp safe
       re-validate -> dgx/ornith into Pi rotation (unblocked) + investigate the single-model limit (mid-pri)
       -> observation scoring. Finish-before-start throughout.
RETURN-PATH: directed captain over comms (topic directive). Resume by checking: crew boots from soul.md?
      max_concurrent=10 + 5-10 active? Claude+Codex both dispatching? gb-mbp re-validated? dgx in rotation?
      Pi single-model verdict in? partial-epic burn-down vs new-epic starts. Full state in HANDOFF-admiral.md.

## 2026-07-03 ~10:30Z — operator (via admiral) · expires: 2026-07-07
WHAT: Pi+DGX ornith PROVEN (operator thrilled). Two tracks now: (A) CAPTAIN OWNS the granular end-to-end +
      sandbox close-out (operator: 'push it all onto the captain') — fix Claude-reviewer ErrMalformed (e2e
      blocker), fix hk-r4p0l (srt no-op: pi runs tagged claude-code so sandbox never engages -> make it
      actually sandbox + TEST), land the api code-fix, then a clean e2e SANDBOXED ornith run. (B) ADMIRAL
      designed the EVAL PROGRAM via 5 subagents -> kerf work codename:eval-program, 23 beads / 6 workstreams:
      ws:metrics (P1 FOUNDATIONAL — product always extracts per-run time+tokens at emitDone; codex/pi
      parsers currently DROP tokens, only claude parsed), ws:matrix (same problem run per model combo:
      Claude Sonnet/Opus concurrent via node model=, Pi+minimax / Pi+ornith / Codex = sequential config-swap
      passes), ws:quality (blind rubric+judge), ws:problems (6 new HARD tasks + 8 existing = 14), ws:dgx
      (model-swap + monitoring + load-ramp to find max queue-slots — GATED on operator SSH), ws:tools
      (Terminal-Bench/Aider later). 14-task set + hybrid tool strategy. OPERATOR-GATED: DGX SSH pubkey
      (hk-eval-prog-dgx-ssh-x7tzo) — admiral surfaced the pubkey; blocks most of ws:dgx.
WHY:  operator wants a rigorous cross-model + DGX-scaling eval to compare models on time/tokens/quality +
      size the DGX. Metrics-infra is the reusable product feature under it all.
ORDER: close-out FIRST (e2e depends on it) -> ws:metrics(P1) + ws:problems(P1, independent, parallel) ->
       ws:matrix + ws:quality -> ws:dgx (when SSH opens) -> ws:tools. Design docs: plans/2026-07-03-eval-program/.
RETURN-PATH: captain handed the bead set (topic eval-program) + owns close-out. Resume by checking: e2e green?
      ws:metrics started? DGX SSH authorized (unblocks ws:dgx)? Full state in HANDOFF-admiral.md.

## 2026-07-02 ~06:15Z — operator (via admiral) · expires: 2026-07-04
WHAT: OVERNIGHT AUTONOMOUS OP (operator asleep, 8h target). P1 (must-deliver): sandbox implemented+tested
      + Pi running IN it with NUMEROUS test runs against the DGX local model. DGX VERIFIED: dgx.local:8551/v1,
      model 'ornith' (Ornith-1.0-35B, vLLM/OpenAI-compat, 256K), dummy key OK. Critical path: leto finishes
      sandbox (hk-6596l config -> hk-i0377 acceptance) ‖ gurney builds Pi base_url passthrough (hk-z13jz,
      elevated P1, via CLAUDE) -> wire Pi->ornith (openai-compat, base_url dgx.local, dummy key) + sandbox
      backend=srt harnesses:[pi] + redeploy -> run 8 eval fuel beads (codename:eval) capturing pass/fail +
      wall-time. P2: eval/grading harness DESIGNED + kerf'd (codename:eval-harness, 5 beads EH1-5; DOT that
      generates->grades->judges->records toward a task->model ROUTER). P3: distributed-fleet planning
      (plans/2026-06-30-distributed-fleet). AUTONOMY: genuine decisions via 3-AGENT CONSENSUS -> proceed,
      never stall-and-wait; admiral is the manual stall-watch overnight.
WHY:  operator wants to know if the local ornith model is any good for agentic coding + build the eval/router
      capability; sandbox is the safe-isolation unlock for running the (untrusted) local model.
RETURN-PATH: captain driving P1 (leto sandbox-finish + gurney base_url, wires integration on both-land);
      admiral running P2/P3 planning + heartbeat-monitoring the P1 tracks. Full state in HANDOFF-admiral.md
      (🌙 OVERNIGHT OP section). Resume by checking: base_url + sandbox landed? Pi wired to ornith? eval
      runs done (pass/fail + speed)? Deliver the morning report.

## 2026-07-02 ~03:05Z — operator (via admiral) · expires: 2026-07-06
WHAT: Pi PROVEN end-to-end (hk-d5q5l ran on agent_type=pi, gpt-5.4-mini, committed+merged in 2min) — the
      operator's 'test Pi' goal is DONE. Capability finding: gpt-5.4-mini commits trivial doc beads but
      exits-without-committing on real Go code (below the ceiling for the scavenger drain). OPERATOR
      DIRECTIVE: HOLD all Pi running until the SANDBOX is built; then push the DGX + test a couple local
      models via the sandbox; model/provider choice deferred to then. Also: Pi auth is on OpenRouter
      (sk-or- key), NOT the operator's OpenAI subscription (no openai key configured) — resolve at model-
      setup time. NEW DIRECTION: SANDBOX is now THE priority (unlock for Pi+DGX). RETASK leto (clean
      restart, fixes its keeper-missing+97%-ctx) from pilot -> pi-sandbox lane, queue sandbox-q, SPIKE
      FIRST (hk-f39ny). pilot -> parked (proven). gurney stays reserved for operator's incoming real work
      -> gb-mbp. CORRECTED (operator feedback): stop justifying model choice as 'off the Anthropic budget'
      — money is money; metric = cost-per-landed-outcome + capability-fit, any vendor.
WHY:  Pi works but weak/cheap models can't do real code — so the sandbox (which enables safe local-model
      testing on the DGX) is the true unlock, and running Pi more now (on a too-weak model, low-pri drain)
      just burns budget. Prove isolation first, then test capable models behind it.
RETURN-PATH: captain parking Pi (after in-flight claude runs finish) + restarting leto onto the sandbox
      spike hk-f39ny (report spike findings before proceeding). Resume by checking: sandbox spike GREEN
      (sandboxed proc reaches local daemon + one API call, Go-CLI-TLS resolved)? then the srt build order.

## 2026-07-02 ~01:20Z — operator (via admiral) · expires: 2026-07-06
WHAT: Fleet warm-up + re-alignment after a ~2-day quiet run; operator delegated crew-allocation authority
      then stepped away. Both CORE lanes reached DONE: remote-hardening (hk-gx0dl CLOSED, 98 gb-mbp jobs,
      2 hard blockers fixed+validated, proven conc-3) and pilot BUILD (hk-94c3t CLOSED). NEW DIRECTION:
      (1) pilot RETASKED build->TESTING — leto proves Pi live on openai/gpt-5.4-mini (web-verified current;
      gpt-4o was stale) via the FILE-KEY auth path (hk-sv3vg fix + redeploy), then feeds scavenger beads;
      queue pi-q. (2) remote BANKED; route REAL incoming work to gb-mbp (skip synthetic volume test);
      gurney STOOD DOWN. (3) v0.4.0 SHIPPED — admiral made SSH signing key (~/.harmonik/releases/signing.key),
      chani cut+signed+pushed at eaaa390d (Good sig gregberns@gmail.com), release flow triggered, chani
      closed. STANDING RULE: captain+admiral self-serve releases/daemon/signing, never stall the fleet on an
      operator-only call (park-don't-halt). (4) SANDBOX (plans/2026-07-02-pi-sandbox/HANDOFF.md, srt, both
      platforms mac-first, v1 network=OPEN=admiral call) KICKED INTO KERF (codename:pi-sandbox, 7 beads,
      spike hk-f39ny gates rest) — planning done, NOT staffed (on-deck, gated for operator review/details).
      CREW LOCKED: leto=Pi test, gurney=stand-down, thufir=parked-as-fuel, watch=on-call, paul=stopped.
WHY:  the two multi-week priorities landed; capacity frees for proving Pi end-to-end (never run live) and
      the operator's incoming real workload (the true remote load-test). Autonomy fix: the 40h chani
      release-stall must never recur — self-serve anything we're authorized to do.
RETURN-PATH: v0.4.0 PUSHED (done). leto driving Pi canary hk-nxjwo on pi-q (file-key auth, gpt-5.4-mini).
      sandbox in kerf (on-deck, spike gates rest). Resume by checking: Pi canary GREEN? then route scavenger
      to pi-q; await operator real workload (->gb-mbp) + sandbox details/staffing green-light.

## 2026-06-30 ~20:40Z — operator (via admiral) · expires: 2026-07-04
WHAT: NEXT-PHASE TRIGGER for gurney's remote lane (gurney's separate-daemon pivot is making progress —
      scratch-daemon.sh harness up, conc 3, hardening). ONCE the gb-mbp proof is solid (beads reliably +
      3-concurrent execute ON gb-mbp, not local-fallback), gurney routes a LARGE BATCH of the scavenger/
      orphan backlog (~120 ready) through THIS separate daemon -> gb-mbp at volume (start conc 3, push
      higher to find the break point). Purpose: drain real backlog through remote AND load-test remote to
      surface issues (launch-gap stall, review.json truncation, worktree races, worker overload, tunnel
      flake). thufir/codex STAYS paused on the main daemon — the backlog WORK moves to gurney's separate
      remote daemon, NOT a revived scavenger crew.
WHY:  decoupled separate daemon makes a high-volume real-load remote stress test cheap + safe (no prod
      restart); doubles as backlog drain. Volume is how the remaining intermittent remote bugs surface.
ORDER: gurney harden harness -> prove gb-mbp reliable+concurrent -> THEN volume scavenger-through-separate
       -daemon (report throughput + failure-mode tally + break-point concurrency).
RETURN-PATH: queued to gurney (topic remote, no-wake) + captain heads-up. Resume by checking whether the
      gb-mbp proof holds yet, then whether the volume backlog batch is flowing through the separate daemon.

## 2026-06-30 ~20:20Z — operator (via admiral) · expires: 2026-07-04
WHAT: REMOTE APPROACH CHANGE + slot reallocation (operator, frustrated — ~2wk, can't get remote right).
      STOP testing remote by toggling gb-mbp in the LIVE primary daemon's workers.yaml + restarting it
      (done 8+ times, each a fresh failure + revert; risks health-window false-revert). INSTEAD: gurney
      runs a SEPARATE standing test daemon (own worktree/clone, own .harmonik/sock/workers.yaml) PINNED
      to gb-mbp (enabled there only, max_slots:3, concurrency 3) and HAMMERS small THROWAWAY no-merge
      beads to exercise the remote path fast + concurrently — decoupled from production. SLOTS: gurney=3,
      leto=1, codex/scavenger PAUSED (thufir-q paused). DATA backing the change: only 3 clean single-run
      gb-mbp proofs ever (proof-dot/rfix/ssh-fetch); ZERO runs ever through a separate test daemon; every
      concurrent attempt fell back to local or hung in the launch_initiated->agent_ready gap (hk-1s1or).
WHY:  the main-daemon restart-toggle loop is slow + brittle + risks bricking production; a standing
      remote-pinned test daemon turns a 2-week stall into a tight reproduce-and-fix loop, no merge needed.
ORDER: gurney aborts STAGE-2 primary-daemon cycle NOW -> stands up remote-pinned test daemon ->
       throwaway-bead hammer until gb-mbp execution is reliable + concurrent -> THEN fold fixes back.
RETURN-PATH: directed gurney directly (--wake) + captain (topic remote). Resume by checking the test
      daemon's gb-mbp landings/30min + which failure mode recurs (launch-gap stall / review.json / wt-HEAD).

## 2026-06-30 ~14:50Z — operator (via admiral) · expires: 2026-07-04
WHAT: Operator SETTLED the keeper-architecture open question (synthesis item f). Decision = HYBRID:
      keep the per-crew DETERMINISTIC keepers (skeleton) AND add a PROBABILISTIC overseer ON TOP that
      intervenes when a keeper fails. "Centralized" = overseer sits on top of keepers, not instead of.
      The hk-u5tgh fix = route the overseer/watchdog restart THROUGH the daemon crew-start path
      (HandleCrewStart -> spawnCrewKeeperWindow) so the keeper window survives a restart instead of
      being stripped. This UN-GATES hk-u5tgh (P1). Also: killed the dead orphan ctx-watchdog tmux
      session (never seeded, wrong CWD, ran no loop — half-failed boot spawn).
WHY:  per-crew keepers are reliable locally but a tmux-level restart bypasses them; the overseer adds
      a recovery layer, and daemon-routed restarts make that layer durable rather than hand-armed.
ORDER: paul finishes hk-xxcv9 (crew-boot auto-arm) → takes hk-u5tgh (daemon-routed overseer restart);
      keeper lane otherwise unchanged. Interim seeded overseer optional, lower priority than the fix.
RETURN-PATH: hold:operator-design-decision label removed from hk-u5tgh; lanes.json keeper gate -> null;
      settled design relayed to captain over comms (topic keeper). Resume by checking paul's hk-u5tgh progress.

## 2026-06-30 ~12:00Z — operator (via admiral) · expires: 2026-07-04
WHAT: Operator has codex/ChatGPT session tokens available — MAXIMIZE implementer work through the
      codex harness to offload cost OFF the Anthropic budget. ADDITIVE + LOWER PRIORITY than the 3
      lanes: must NOT disrupt remote/pilot/keeper staffing (operator: "hold off if it gets in the way").
      Two fits (captain's call): (a) route file-disjoint build/bugfix beads through the codex harness
      as implementer; (b) revive the codex-first scavenger crew (thufir, queue thufir-q, 1-item serial
      + DOT review) to drain the ~120-bead backlog on ChatGPT tokens. CAVEAT: codex asleep ~4 days →
      RE-CANARY one local run before leaning on it. Codex is LOCAL-only (not gb-mbp), bills ChatGPT.
WHY:  cost-per-landed-outcome + model-fit: ChatGPT-billed throughput is free against the Anthropic
      budget; the backlog is starved (~120 ready) so codex is pure additive throughput.
ORDER: 3 lanes first (unchanged) → codex re-canary → route codex-eligible work / revive scavenger.
RETURN-PATH: relayed to captain over comms (topic directive). Resume by checking whether codex is
      re-canaried + running (leto-codex / thufir-q active with codex harness), throughput off-budget.

## 2026-06-30 ~11:40Z — operator (via admiral) · expires: 2026-07-04
WHAT: Fleet woke from a ~4-day operator-directed sleep onto the security-fix daemon (7a9bf2e5,
      deploy daemon-20260630-01). Operator confirmed STAFF 3 LANES, remote #1: (1) remote-worker
      e2e proof (hk-nepva, blocker hk-t1t00 now CLOSED) — but VERY THOROUGH LOCAL testing FIRST via
      the L0–L5 pyramid / isolated test-daemon (NO live-daemon restart needed); gb-mbp is UP for the
      live portion. (2) Pi-harness core build (hk-4rmj1, codename:pilot) — operator-UNGATED now. (3)
      Keeper reliability (hk-u5tgh + hk-xxcv9). All 2026-06-25 priority blocks EXPIRED → history.
WHY:  remote reliability is the unlock to raise concurrency 4→8; pi-harness adds a 2nd implementer
      harness; keeper-less crew restarts are a recurring fleet-reliability tax. Local-first remote
      testing keeps blast radius low and the live daemon untouched.
ORDER: remote LOCAL pyramid → remote live on gb-mbp ‖ pi-harness build ‖ keeper fixes (file-disjoint).
RETURN-PATH: captain spawned (harmonik-a3dc45482890-captain); admiral relayed via comms. Resume by
      checking the captain's crew/queue staffing for the 3 lanes + nepva's local-pyramid progress.

## 2026-06-25 ~21:29Z — operator (via admiral) · expires: 2026-07-02
WHAT: TWO additions elevated INTO the active remote lane (out of parked/on-deck). (1) CONCURRENT
      LOCAL+REMOTE: routing mechanism already LANDED today (hk-f10xl — Queue.LocalOnly/WorkerTarget
      gate SelectWorker); remaining = LIVE concurrent-run proof + live worker on/off toggle (hk-xjbvi).
      (2) TEST-DAEMON HARNESS: operator clarified the long-misread "two daemons" idea = a STANDING
      isolated test daemon in a separate worktree/clone pinned to remote (submit issues to main daemon)
      = MOVE ① (scratch-clone), NOT the skipped move ④ (two daemons on the SAME repo dir). PROMOTE
      from scope-only to BUILT.
WHY:  per-queue routing landed makes "run on both at once" a validation+polish task, not a build; the
      standing test-daemon is the fast-loop unblock that ACCELERATES the remote last-mile (move ① was
      always the #1 do-now), so building it now serves the headline rather than competing.
ORDER: remote reviewer-consistency last-mile → live concurrent local+remote proof (same path) ‖ build
      test-daemon harness (parallel, scratch-clone). hk-xjbvi toggle folds in. Multi-remote scheduler = later.
RETURN-PATH: captain scoping both (directive comms 019f00af); admiral-initiatives.md STALE on routing
      (listed "not designed/on-deck" — it's LANDED) → admiral to correct. Resume by checking captain's
      kerf-work + bead set for the test-daemon harness + the concurrent-validation bead.
