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
| **WS2 — Dockerized controlled-E2E** | Two images (daemon; worker = sshd+git+tmux+twin) + compose wiring `SSHRunner{Host:"worker"}` across the container network + a test entrypoint. Hermetic, reproducible, seconds, no dev-box ssh setup, no Claude billing. | Operator floor: *no real-remote testing without a docker harness that already runs.* No new production code — the SSHRunner seam is done. | **small–medium** (~2 Dockerfiles + compose + entrypoint) |
| **WS3 — Twin↔real parity harness** | Agent-runnable, before-release / after-twin-change cadence. Run one scenario through BOTH twin and live agent; assert equivalence on the daemon-observed normalized event stream (ordered event types, ack/hook timing within tolerance, terminal outcome). | Closes the silent-lie hole. Codex nearly buildable (diff a fresh live capture's reactor-action sequence vs the corpus the twin replays). Claude needs the unbuilt replay mode + accept tmux-physical layer stays real-only. pi needs a twin first. | **medium–large** (codex ≈ medium; **claude needs the unbuilt replay mode; pi needs a twin built from scratch** — each is its own slice) |
| **WS4 — Acceptance oracle (core-loop-proof), forced** | Revive the `origin/integration/core-loop-proof` harness onto the rebuilt M2/M3/M4 seams; the {claude,codex,pi}×{local,remote} matrix proving bead→queue→correct-model→real-change→verdict→terminal. Make it the assessor's LT leg. | Operator: MUST HAVE, comprehensive, **the system must force it to run** — not a green check that does nothing. | **medium** (revive + reseat) |
| **WS5 — Wire the assessor** | A launcher + admiral-invoked flow (*"Assessor, is the system ready?"* → PASS/BLOCK → cut release). Compose WS1/WS2/WS4 as its LT/XT/CR legs. **Design a beads-independent, fail-closed BLOCK path for daemon-off mode** (see §4.1). NOT code in `internal/` — it's an agent + a gate mechanism. | Turns everything above into a forced release gate under the admiral's control. | **medium** (wiring + the daemon-off BLOCK design) |

**Explicitly out of scope:** any real-`gb-mbp` run (that's the M4 gate, gated behind this whole
milestone); new production code in the remote path (the seam is complete); rebuilding the
in-process harness or the core-loop-proof matrix from scratch.

---

## 4. Load-bearing open questions (need the operator / resolved at design)

1. **Daemon-off BLOCK signal — the silent-false-PASS.** The assessor's PASS/BLOCK is entirely a
   beads query (open P0/P1 `found-by:*` = BLOCK). With the daemon off and no beads created, that
   query is empty → the gate falsely says PASS. The gate needs a **beads-independent, fail-closed**
   verdict path (e.g. verdict written to a file/COORD the gate reads, + a daemon-liveness
   precondition that BLOCKs if it can't trust the bead ledger). **Resolve at WS5 design; flag now
   because it's load-bearing for how we run today.**
2. **Can the controlled-E2E gate live in CI, or is it local-only?** The scenario suite includes
   the localhost-SSH remote E2E, which needs `ssh localhost` reachable on the runner. If GitHub's
   runner can't host that (or the docker harness), WS1's "forced" gate becomes a **local pre-push /
   pre-merge discipline**, not a CI check. That changes the "how do we force it" answer — decide
   which at WS1/WS2 design. **(Operator explicitly raised this — "may need to run those locally".)**
3. **Twin-parity scope.** Accept that the tmux physical-delivery layer (splash-dismiss, physical
   Enter, pane-ID targeting) is **irreducibly real-Claude-only** and cannot be twinned (per the
   2026-05-14 audit)? If yes, parity asserts equivalence on the *wire/event* layer only, and those
   two stages stay covered by the `e2e_real_claude` path. **Recommend yes.**
4. **Milestone bureaucracy.** Under no-beads this rides the plan docs (like M5). A formal
   `codename:controlled-testing` kerf bench + beads gets created when the daemon/beads come back on.

---

## 5. Recommended sequence

1. **WS1 first** (days) — flip the gate + fix the `check-full` coverage gap. Immediate value: the
   controlled-E2E we already own starts actually blocking merges.
2. **WS2 ‖ WS3** (parallel) — docker harness and the parity harness are independent; run concurrently.
3. **WS4** — revive the acceptance oracle onto the as-built seams (depends on nothing above, but
   naturally feeds WS5).
4. **WS5 last** — wire the assessor, composing WS1/WS2/WS4 as its legs and resolving the daemon-off
   BLOCK path. This is the capstone that makes the whole thing a forced release gate.
5. **THEN** — and only then — the M4 real-`gb-mbp` proof.

Each workstream carries its own design pass + independent-reviewer gate (signoffs waived) before
build, and migrates/adds tests to prove itself, per the standing merge recipe.
