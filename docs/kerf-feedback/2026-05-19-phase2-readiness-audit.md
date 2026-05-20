# Phase-2 readiness audit — "all work through harmonik?" — 2026-05-19

Source: strategic adversarial audit pre-decision to route ALL bead work through `harmonik run --beads`. Sample = the 6 prior dogfoods (hk-x0y2k, hk-75rij, hk-pxrv6, hk-vqoh2, hk-o0yft, hk-cd92e). All were **P2 spec-edit / docs-only** beads. None exercised the riskier surfaces below.

## Ready to dispatch through harmonik

- **Pure spec-text edits, single file, no Go code touched.** All 6 dogfoods. Path is solid end-to-end including commit on worktree branch.
- **Single-bead `harmonik run <id>`** at `--max-concurrent 1`. Exit-path now clean post-hk-5dewt (retry-cap 2s→15s landed 2298cab).
- **Auto-close + bead state reconciliation.** Defense-in-depth (.br_history rotation + WAL pre-flight + retry-cap) holds for the dogfooded shape.
- **HandlerPause policy goroutine** — wired pre-Seal at `daemon.go:510` and scenario-tested (hk-6f1uj 49f1d73, hk-qxtbq 918bdf8).
- **Reviewer-phase nil watcher / substrate** — scenario-tested (hk-3aqtb, hk-t5j2w, hk-nfhqd) landed last 2 days.

## Untested / Risky

1. **Code-touching beads (Go source under internal/, cmd/)** — workload class: any P0/P1 fix or feature. **Why untested:** all 6 dogfoods edited `specs/` or `docs/` only; agent never compiled, ran tests, or hit a build error that fed back into its own session. Failure modes: (a) handler agent shells `go test` and exceeds `agentReadyTimeout` while test runs; (b) compile errors → agent loops trying to fix → no /quit ever typed → daemon-quit-on-commit fires on broken commit. **Cheapest probe:** route hk-5mjrs (the bare-label migration — touches `.beads/issues.jsonl` only, no Go) first, then a one-line internal/ rename bead. **Severity: HIGH.**

2. **`--max-concurrent N > 1` in production `harmonik run --beads`** — workload class: parallel multi-bead dispatch. **Why untested:** every recorded dogfood used implicit N=1; `t11_throughput_test.go` uses a twin handler, not real claude. Real concurrency exercises: shared `.br_history` rotation race, shared brAdapter under contention, two real claude tmux sessions, pidfile lock unaffected but `.beads/.br_history/` flock unproven. **Cheapest probe:** 2 trivial spec-edit beads + `--max-concurrent 2`. **Severity: HIGH** (load-bearing for Phase-2 throughput claim).

3. **Bead that itself calls `br` during execution** — workload class: implementer mutates beads (creates sub-beads, closes others, updates labels). **Why untested:** the agent-task.md's "Bead Lifecycle (CRITICAL)" section tells handlers NOT to close beads from inside the worktree, but reality is they sometimes do (HANDOFF v50). When a real-code bead has dependencies, the implementer naturally wants `br create` for follow-ups. Worktree `.beads/issues.jsonl` is stale-at-fork → re-create-under-new-ID pattern → orphan beads in main. **Cheapest probe:** route hk-rp48p (already names `br` interaction) and inspect leaked sub-beads. **Severity: MEDIUM.**

4. **Reviewer-phase / `--review-loop` dogfood** — workload class: two-agent (impl + reviewer) flow. **Why untested:** docs say "single-mode is the confirmed path" (operational-green caveat #3). `paste-inject` ordering in reviewloop.go path has scenario tests but ZERO end-to-end dogfood. **Cheapest probe:** any single dogfooded bead with `--review-loop` flag. **Severity: MEDIUM.**

5. **Bead whose work spans multiple files / requires running tests / requires `go build`** — workload class: realistic Go feature work. **Why untested:** all 6 dogfoods were single-file spec markdown. The composition-root reviewer-miss directive (v49 NEW in HANDOFF) lists 3 production-breaking misses (hk-37zy8, hk-yjduq, hk-2hb2y) — Phase-2 must catch its OWN misses through dogfood, not rely on operator review. **Cheapest probe:** trivial one-line Go change (e.g., a comment fix in `cmd/harmonik/run.go`) + dispatcher verifies the agent's commit compiles. **Severity: HIGH.**

6. **Concurrent claim ordering / priority-aware claim** — workload class: multi-bead queue with non-uniform priorities. **Why untested:** hk-rp48p (open, P1) explicitly names that claim-path "ignores priority order — claimed P1 IN_PROGRESS stale bead instead of P0 ready bead." Phase-2-for-all-work means many simultaneous beads of varied priority; claim determinism is broken. **Cheapest probe:** queue 3 beads at P0/P1/P2, observe claim order. **Severity: HIGH** (would silently misroute the queue).

7. **Stale `in_progress` bead reset on daemon restart** — workload class: agent crash mid-flight. **Why untested:** every dogfood reached `run_completed=success`. `processDead` false-negative was fixed (hk-ry3be) but PL-006 sixth-bullet (BeadResetter for stale `in_progress`) has unit test only — no scenario exercise where an agent dies during real Phase-2 dispatch and the next `harmonik run` resets it. **Cheapest probe:** SIGKILL claude pane mid-run, re-invoke harmonik; expect bead reset to `open`. **Severity: MEDIUM.**

8. **Branching config / non-default `lands_on` per bead** — workload class: bead specifies a target branch other than `main`. **Why untested:** every dogfood landed on main via squash. `branching.go` parses YAML fenced blocks; no dogfood exercised `lands_on:` other than default. **Severity: LOW** (most Phase-2 work targets main).

9. **`.harmonik/queue.json` recovery after crash** — workload class: daemon crashes between dispatch and completion. **Why untested:** PL-005 step 8a `queue.json` recovery path has unit tests; no end-to-end crash-recovery rehearsal. `paused-by-failure` queue status is plumbed but `harmonik run` does not yet recover-and-resume. **Severity: MEDIUM.**

10. **Worktree-stale-at-fork bead-ID leak (KNOWN, observed ~5×/session)** — workload class: orchestrator creates a bead AFTER worktree fork. HANDOFF explicitly flags this. Phase-2-for-all-work AMPLIFIES the frequency. **Cheapest probe:** none needed; document as expected friction with a manual reconciliation step. **Severity: LOW** (annoying, not breaking).

## Recommendation

**Not yet safe for unconditional "all work."** Before lifting the gate, run 2 specific validations: (i) **one trivial Go-touching bead** end-to-end (probe #1+#5) to confirm code-bead dispatch works, and (ii) **a 2-bead `--max-concurrent 2` dogfood** (probe #2) to confirm parallel dispatch under real claude. If both green, Phase-2 is safe for **all spec/docs/Go beads under `--max-concurrent ≤ 2`**; HIGH-severity probes #3 (br-from-agent) and #6 (priority-claim, hk-rp48p) remain known-broken and should be excluded from auto-dispatch until fixed.
