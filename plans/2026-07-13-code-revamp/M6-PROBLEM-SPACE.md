# M6 — controlled-testing harness — PROBLEM-SPACE (DRAFT)

> **STATUS: DRAFT problem-space (planner, 2026-07-16).** Operator-directed milestone
> (this session). Session constraints in effect: daemon OFF, NO beads, signoffs waived,
> tracked in `COORD.md`. Un-gates the M4 real-remote proof: **no live-remote testing until
> this milestone lands** (operator directive, 2026-07-16).

---

## 1. Problem statement

We are one step away from alpha-testing the rebuilt core against a **real second machine**
(`gb-mbp`), and the controlled-testing floor underneath that step is not solid enough to catch
what would break there first. Concretely:

- **Our good tests gate nothing.** The scenario/E2E suite — the tests that actually exercise the
  full daemon and the remote path — runs in CI with `continue-on-error: true`
  (`.github/workflows/scenario.yml:32`), so it blocks no merge. The per-commit `agent-review`
  hook is a **stub that exits 0** (`Makefile:434-436`). The only thing gating a merge today is
  `go test -short -race` (CI `check-short`), which explicitly **skips** real-daemon E2E. So the
  strongest coverage we own is optional.
- **The twin can silently lie.** The controlled-E2E harness swaps real agents for compiled
  **twins** (`cmd/harmonik-twin-claude`, `internal/codexdigitaltwin`) for speed and determinism —
  correct design, but **no test asserts the twin behaves like the real agent** (verified: zero
  twin↔real parity/conformance tests exist for claude, codex, or pi). A twin that drifts from
  real behavior turns every green E2E into a false assurance. We have had a history of
  real-vs-observed behavior divergences; this is the structural hole that lets them through.
- **No forced pre-release gate.** The "assessor" — a fully *designed and authored* gate agent
  (`.harmonik/agents/assessor/`) meant to answer *"is the system ready to release?"* — was
  **never wired into a launcher** and has never run. There is no mechanism that *forces* a
  comprehensive check before a release; readiness is asserted by hand.
- **No hermetic controlled environment.** The remote path is exercisable on one box today (via
  `SSHRunner{Host:"localhost"}`), but only if the dev box's `ssh localhost` happens to be set up.
  There is no Dockerized harness — designed repeatedly across `plans/`, never built — so the
  controlled loop is not reproducible or CI-portable.

The goal of M6 is to make the controlled-testing floor **load-bearing**: fast, hermetic,
forced-to-run, and honest about twin fidelity — *before* we spend a single cycle against a real
remote box.

---

## 2. What already exists (the floor we build on — do NOT rebuild)

- **A fast full-daemon harness.** `test/scenario/harness_test.go` boots the **real `daemon.Start`
  in-process** (goroutine, real Unix socket) and drives beads end-to-end with compiled twin
  agents. Subprocess boot was *deliberately rejected as too slow*; sub-minute in-process is the
  design. This IS controlled-E2E — it just isn't containerized or gated.
- **The remote path runs on one box.** `SSHRunner{Host}` (`internal/lifecycle/tmux/runner.go:92`)
  wraps commands as `ssh <host> -- …`; `Host:"localhost"` is the whole local-vs-remote delta.
  `TestScenario_RemoteSubstrate_Localhost_E2E` exercises the full remote lifecycle (worker
  registry, code-sync, `git worktree add` over ssh, box-A fetch over ssh, merge-back, reverse
  tunnel) against a real localhost sshd, no real Claude, no API key.
- **A partially-built acceptance oracle.** `core-loop-proof` (the {claude,codex,pi}×{local,remote}
  live-loop matrix) was built on `origin/integration/core-loop-proof` (pi/codex cells green,
  PR #20) before the `quality-system` kerf work stalled at `status: tasks`. Revive, don't restart.
- **A complete assessor design.** `.harmonik/agents/assessor/{soul.md,operating.md,manifest.yaml}` +
  `specs/assessor-handoff-schema.md`: an admiral-spawned, single-shot gate executor with three
  legs — **LT** (live-verify the real loop = the acceptance oracle), **XT** (exploratory
  break-testing), **CR** (independent cold code review) — that posts one PASS/BLOCK verdict and
  self-terminates. Manifest passed review; only the wiring is missing.
- **A twin-fidelity map.** `docs/twin-parity-audit-2026-05-14.md` already enumerates where the
  Claude twin can and cannot match reality (2 stages — tmux splash-dismiss, physical-Enter/pane
  targeting — are irreducibly real-only). This is the spec input for the parity work.

---

## 3. Scope — five workstreams

| WS | What | Why it's needed | Rough size |
|---|---|---|---|
| **WS1 — Make the controlled-E2E a REAL gate** | Flip `scenario.yml:32` `continue-on-error`; add the suite as a required branch-protection check (or a forced local pre-push tier if CI can't host it); replace the `make agent-review` stub; **fix the `check-full` coverage gap** (`Makefile:335` omits `./internal/daemon/...`, so `check-full` does NOT run the localhost-SSH remote E2E that `test-scenario`/CI do). | The tests exist and pass; they just don't block. Highest value, lowest effort. | **small** (config + one Makefile fix) — **but its "forced" property depends on `ssh localhost` being reachable on whatever host runs the gate; see open-Q2. WS2 (docker) is what makes the gate host-independent.** |
| **WS2 — Dockerized + subprocess controlled-E2E** | Two images (daemon; worker = sshd+git+tmux+twin) + compose wiring `SSHRunner{Host:"worker"}` across the container network + a test entrypoint. Hermetic, reproducible, seconds, no dev-box ssh setup, no Claude billing. **Also add a subprocess daemon-boot test** — the operator wants BOTH in-process (exists) AND subprocess (real `harmonik` daemon started as a separate process) coverage; the containerized run IS the subprocess variant, plus a non-docker subprocess smoke. | Operator floor: *no real-remote testing without a docker harness that already runs.* No new production code — the SSHRunner seam is done. | **small–medium** (~2 Dockerfiles + compose + entrypoint + subprocess boot test) |
| **WS3 — Twin↔real parity harness ⭐ HIGHEST-VALUE CORRECTNESS WORK** | Agent-runnable, run at the same (heavier, real-Claude-invoking, token-spending — **operator: acceptable**) cadence as docker testing. Run one scenario through BOTH twin and live agent; assert equivalence on the daemon-observed normalized event stream (ordered event types, ack/hook timing within tolerance, terminal outcome). **Per-agent:** **(a) Claude — the priority.** The Claude twin is scripted-only today = the biggest blind spot; the operator flags this as *possibly the most important thing we do* (an unrealistic twin masks prod issues). Build the real-session replay mode AND make the twin **property/fuzz-testable**: inject varied timings and assert the daemon+keeper behave correctly across them. **(b) Codex — keep the corpus fresh over time:** periodic live re-capture + diff, not a one-time frozen snapshot (today's canary only checks a frozen corpus). **(c) pi — needs a twin built from scratch AND a real-agent test** (has neither today). | Closes the silent-lie hole — the structural gap that lets real-vs-twin divergence reach prod. | **large** (Claude replay+fuzz ‖ codex fresh-capture-diff ‖ pi twin-from-scratch — three independent slices) |
| **WS4 — Acceptance oracle (core-loop-proof), forced** | Revive the `origin/integration/core-loop-proof` harness (partially built, PR #20) onto the rebuilt M2/M3/M4 seams — **embed it in the plan and keep it moving, don't let it re-stall**; the {claude,codex,pi}×{local,remote} matrix proving bead→queue→correct-model→real-change→verdict→terminal. Becomes the assessor's LT leg. | Operator: MUST HAVE, comprehensive, **the system must force it to run** — not a green check that does nothing. | **medium** (revive + reseat) |
| **WS5 — Wire the assessor (release-readiness AGENT)** | Stand up the assessor as a launchable **LLM agent** the admiral spawns on demand (*"is the system ready?"* → PASS/BLOCK → cut release). It runs the three legs (LT live-verify / XT exploratory-break / CR cold code-review) AND **actively reconciles what was claimed done against actual commits, diffs, test results, and reviews — explicitly checking beads-vs-reality alignment** — then returns a **reasoned judgment**. The verdict is the agent's judgment, **NOT a mechanical bead-count** (the authored design's `br list` P0/P1 tally is REPLACED — beads drift, and in daemon-off mode there are none → false PASS). Beads, when present, are one input + the durable defect record, never the arbiter. It is an agent + a launcher, NOT code in `internal/`. | Operator: readiness must be an agent digging through everything, because beads/commits drift constantly. Turns WS1/2/4 into a forced release gate under the admiral. | **medium** (launcher + mission schema + the judgment/reconciliation contract) |

**Explicitly out of scope:** any real-`gb-mbp` run (that's the M4 gate, gated behind this whole
milestone); new production code in the remote path (the seam is complete); rebuilding the
in-process harness or the core-loop-proof matrix from scratch.

---

## 4. Decisions (operator-locked 2026-07-16 — do NOT re-open)

1. **The assessor JUDGES; it does not count beads.** The authored design computed PASS/BLOCK as a
   deterministic bead query (open P0/P1 `found-by:*` → BLOCK). **REPLACED.** Beads drift out of sync
   constantly, and in daemon-off mode there are none → an empty query = a false PASS. Instead the
   assessor is an **LLM agent whose verdict is its own reasoned judgment** over commits, diffs, test
   results, and reviews; **reconciling beads-vs-reality (do the beads/commits/claims actually agree?)
   is an explicit DUTY of the agent**, not something it trusts. Beads are an input + durable record,
   never the arbiter.
2. **The gate is split CI / local, and that split is made explicit.** Assume GitHub CI can host only
   PART of the suite (the pure-Go units + anything not needing `ssh localhost` / docker). The
   heavier tier (localhost-SSH remote E2E, docker controlled-E2E, twin↔real parity) is a **forced
   LOCAL pre-merge discipline**. WS1/WS2 must produce a clear, documented map of *what runs in CI vs
   what must run locally.* **Docs-only changes to `main` don't need the heavy gate** (skip it).
3. **Twin parity invokes real Claude Code — acceptable.** Building/running parity spins up a real
   Claude (and real codex/pi) — the tmux physical-delivery layer (splash-dismiss, physical Enter,
   pane targeting) can't be twinned, so parity covers the wire/event layer and the real agents cover
   the rest. Parity runs at the **same heavier cadence as docker testing** (slower, some token spend
   — operator: acceptable), not on every commit.
4. **Milestone bureaucracy.** Under no-beads this rides the plan docs (like M5). A formal
   `codename:controlled-testing` kerf bench + beads gets created when the daemon/beads come back on.

**Tracked separately (NOT part of M6 — a future investigation, placeholder on the ROADMAP):**
a better pi interaction seam. Pi is open and modifiable; instead of driving it through tmux
keystrokes we could **fork pi or write a plugin for a codex-style structured handler** — more
scalable and more testable. Placeholder only; scope later.

---

## 5. Recommended sequence

1. **WS1 + WS3-Claude start together.** WS1 (days) — flip the gate + fix the `check-full` coverage
   gap — is the fast structural win. **WS3-Claude** (real-session replay + property/fuzz the twin) is
   the highest-value *correctness* work (operator: possibly the most important thing we do) and is
   independent of WS1 — run them in parallel from the start.
2. **WS2 ‖ WS3-codex ‖ WS3-pi.** Docker/subprocess harness, codex fresh-capture-diff, and the pi
   twin-from-scratch are mutually independent — run concurrently.
3. **WS4** — revive the acceptance oracle onto the as-built seams; keep it moving so it doesn't
   re-stall. Feeds WS5.
4. **WS5 last** — wire the assessor as the release-readiness agent, composing WS1/WS2/WS4 as its
   legs. The capstone that makes the whole thing a forced release gate.
5. **THEN** — and only then — the M4 real-`gb-mbp` proof.

Each workstream carries its own design pass + independent-reviewer gate (signoffs waived) before
build, and migrates/adds tests to prove itself, per the standing merge recipe.
