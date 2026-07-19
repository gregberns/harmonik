# M6 — controlled-testing harness — DETAILED PLAN (with outcome tasks)

> **STATUS: DRAFT plan (admiral/planner, 2026-07-16).** Turns `M6-PROBLEM-SPACE.md` into
> build-ready outcome tasks. Session constraints: daemon OFF, NO beads, signoffs waived,
> tracked in `COORD.md`. **NOT started — HOLDING for the operator's explicit "go."**
> This plan does NOT reopen the operator-locked decisions in problem-space §4.
>
> **Provenance:** synthesized from three grounded planning passes (WS1+WS2, WS3, WS4+WS5),
> each of which verified the problem-space's file:line claims against the live repo. Corrections
> to the problem-space are called out in §0 below. **Independently reviewed (2026-07-16):
> APPROVE-WITH-CHANGES — faithful to all four locked decisions, no directive reopened; the 2
> must-fixes + 6 should-fixes are applied in this revision.**

---

## 0. Corrections to the problem-space (verified against the repo)

The problem-space was fact-checked at draft time, but three of its premises have since drifted or
were imprecise. The plan is built on the corrected facts:

1. **`make agent-review` is NOT an exit-0 stub anymore.** The reviewer skill is installed and
   executable (`.claude/skills/agent-reviewer/run` + `scripts/check-verdict.sh`); `Makefile:417-433`
   runs the real reviewer and fails on non-APPROVE. Lines 434-436 are dead fallback. **The real gap
   is that nothing *forces* it** — no git hook / required CI job invokes it. WS1 reframes from
   "replace the stub" → "wire the already-working gate."
2. **`continue-on-error: true` is at `.github/workflows/scenario.yml:31`, not `:32`.** Content exact.
3. **The credential problem in §4 is HALF-solved.** `scripts/scratch-daemon.sh:237-240` implements
   "credfence" (`env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN …`), which resolves the D2
   **no-API-key / subscription-billing** half — real-agent runs bill the subscription pool via the
   CLI's logged-in state, never an API key. **Still unbuilt:** getting the logged-in `~/.claude` auth
   state *into* an isolated/docker env. WS4 inherits credfence but WS4-0/WS2 still own the auth-mount.

All other problem-space claims CONFIRMED (in-process harness boots real `daemon.Start`; `SSHRunner{Host}`
seam; `check-full` omits `./internal/daemon/...`; `check-short` skips E2E; PR #20 / `core-loop-proof`
partially built + conflicting; assessor designed-but-unwired; zero twin↔real parity tests for any agent).
One load-bearing caveat surfaced: **the localhost-SSH remote E2E `t.Skipf`s to green when `ssh localhost`
is unavailable** — a false gate (WS1.3 closes this).

**New structural finding (not in problem-space):** WS3 needs a **shared foundation task F1** — a
normalized-event canonicalizer + stream-equivalence library — that gates all three parity slices.
Added below as WS3-F1.

---

## 1. Workstream dependency graph (the sequencing spine)

```
WS1.2 (check-full gap) ──► WS1.1 (CI gate flip*) ──► WS1.5 (gate map + risk tiers)
WS1.3 (no-silent-skip) ──────────────────────────────┘        ▲
WS1.4 (force agent-review) …………………………………………………………………………│
                                                              │
WS2.1 (daemon image) ─┐                                       │
WS2.2 (worker image) ─┼─► WS2.3 (compose E2E) ─► WS2.5 (doc) ─┤
WS2.4-smoke (early) ──┘   WS2.3 ─► WS2.4-docker                │
                                 ▼                            │
WS3-F1 (equiv library) ─► WS3-{Claude,codex,pi} parity ──────┤ (assessor heavy tier)
                                                              │
WS4-1 (branch reconcile, INDEPENDENT) ─┐                      │
WS4-0 (env+cred decision) ─────────────┴─► WS4-2 (reseat on WS2 env)
   ─► WS4-3 (pi+codex green) ─► WS4-4 (claude cells) ─► WS4-5 (forced LT command)
                                                              ▼
WS5-1 (delete bead-count) ─► WS5-2 (schema v2) ─► WS5-3 (launcher) ──┐
WS5-4 (personality) ┐                            WS4-5 + WS5-1 ─► WS5-7 (wire 3 legs)
WS5-5 (good-enough) ─┼─► WS5-6 (admiral authority) ──────────────► WS5-8 (capstone dry-run)
                                                  WS5-3/4/6/7 + WS4 ─┘
```
`*` WS1.1 (making the CI check *required*) needs repo-admin — **operator-only**, flagged in §4.

**First wave (start on "go"):** WS1.2 → WS1.1/WS1.3, in parallel with **WS3-F1 → WS3-Claude-A/B**
(the operator's flagged top priority) and WS4-1 (branch reconcile, independent). WS5-1 (delete the
bead-count arbiter) is also a zero-dependency first move.

---

## 2. Outcome tasks by workstream

Each task: **ID — outcome** · **Accept:** verifiable criteria · **Files:** real seams · **Deps** ·
**Decision** (open design choice, with a recommendation — none silently deferred).

### WS1 — Make the controlled-E2E a REAL gate

- **WS1.2 — `check-full` runs the localhost-SSH remote E2E.** *(do first — unblocks WS1.1)*
  - **Accept:** `make check-full` executes `./internal/daemon/...` scenario tests; `-v` shows
    `TestScenario_RemoteSubstrate_Localhost_E2E` RUN (not absent) on an ssh-capable box.
  - **Files:** the scenario line omitting `./internal/daemon/...` is `Makefile:338` — have `check-full`
    invoke `$(MAKE) test-scenario` (`Makefile:100`, which includes `internal/daemon`) rather than
    duplicate the package list, so the two can't drift again.
  - **Deps:** none. **Decision:** reuse `test-scenario` vs inline list → **reuse** (single source).

- **WS1.3 — A skipped remote E2E cannot masquerade as a pass.**
  - **Accept:** an env flag (e.g. `HARMONIK_REQUIRE_REMOTE_E2E=1`) turns `rsb12SSHAvailable`'s
    `t.Skipf` into `t.Fatalf`; with the flag set and sshd down, the run exits non-zero.
  - **Files:** `internal/daemon/scenario_remote_substrate_localhost_test.go` (skip guard + `rsb12SSHAvailable`)
    and its `_dot_` sibling.
  - **Deps:** complements WS1.5; WS2 makes ssh-localhost moot in docker. **Decision:** required-mode
    host = dev-box-ssh now (cheap, closes false-green) → docker (WS2) once available.

- **WS1.1 — In-process scenario suite blocks merges in CI.**
  - **Accept:** `scenario.yml` no longer has `continue-on-error: true` for the `test-scenario` step;
    a PR with a failing scenario shows a red **required** check and cannot merge; branch protection
    lists it.
  - **Files:** `.github/workflows/scenario.yml:29-31`; branch-protection (**operator/admin action**).
  - **⇒ SEQUENCED LAST (operator decision 2026-07-16):** do NOT flip this until every M6 test tier is
    built and integrated and green — you don't make a gate *required* until what it guards exists and
    passes. WS1.1 is the closing step of M6, not a first-wave task.
  - **Deps:** WS1.2 first. **Decision:** the required CI check names a **concrete non-ssh invocation**
    — `go test -tags=scenario ./test/scenario/...` **only** (NOT `make test-scenario`, which also
    bundles `./internal/daemon/...` where `RemoteSubstrate_Localhost_E2E` `t.Skipf`s green on
    sshd-less runners — that would reintroduce the exact false-green this task exists to kill). The
    localhost-SSH + docker tiers are the assessor's forced-local gate, never the CI required check.

- **WS1.4 — The per-commit `agent-review` gate is actually enforced.**
  - **Accept:** a pre-commit/pre-push hook or required CI job invokes `make agent-review`; a diff
    with no APPROVE is rejected (`check-verdict.sh` non-zero). *Wiring task — the reviewer already works.*
  - **Files:** `Makefile:417-433` (functional); `scripts/check-verdict.sh`; new `.githooks/pre-commit`
    + `core.hooksPath`, or a CI job. **Do NOT** touch dead lines 434-436.
  - **Deps:** independent. **RESOLVED (operator 2026-07-16): NO git hook.** The operator dislikes git
    hooks generally. `agent-review` stays a manually/assessor-invoked target; the assessor owns rigorous
    review at the gate — there is no per-commit block. Accept = "the mechanism is documented and the
    assessor runs it as part of its CR leg." Do NOT wire a pre-commit/pre-push hook.

- **WS1.5 — Checked-in CI-vs-local test map + risk-tiering rule.**
  - **Accept:** one authoritative doc contains the CI-vs-local table (§3) and the risk-tiering rule
    (§3); any change class maps to its required tier.
  - **Files:** `docs/methodology/TESTING.md` §"Gate tiers & risk-tiering" (file exists).
  - **Deps:** consumes WS1.1-1.3 + WS2. **Decision:** who tiers a change? → path-glob **floor**
    (auto-Tier-1 on `internal/daemon`/`internal/lifecycle` diffs) that the assessor can only *raise*.

### WS2 — Dockerized + subprocess controlled-E2E

- **WS2.1 — `daemon` container image builds & runs the real `harmonik` binary.**
  - **Accept:** `docker build -f test/docker/Dockerfile.daemon .` succeeds; `docker run` boots the
    daemon (socket appears); twins present on PATH.
  - **Files:** new `test/docker/Dockerfile.daemon`; reuses `Makefile` `build-all`/`twins`.
  - **Deps:** none. **Decision:** multi-stage in-image build (hermetic) vs mount host artifacts →
    **multi-stage** (reproducible, CI-portable).

- **WS2.2 — `worker` container image = sshd + git + tmux + twin, reachable over the container net.**
  - **Accept:** from the daemon container, `ssh worker true|git --version|tmux -V` all succeed
    passwordless; writable clone path for `git worktree add`.
  - **Files:** new `test/docker/Dockerfile.worker`; mirrors the origin.git/boxA/worker topology in
    `scenario_remote_substrate_localhost_test.go`.
  - **Deps:** none (‖ WS2.1). **Decision:** key mgmt → **generate-at-compose-up** via entrypoint +
    shared volume (no secret in git); `StrictHostKeyChecking accept-new`.

- **WS2.3 — Compose runs the remote-substrate E2E across the container net with `Host:"worker"`.**
  - **Accept:** `make test-docker-e2e` brings up both containers, runs the remote lifecycle over ssh,
    exits 0/non-0, seconds–low-minutes, **no host `~/.ssh` setup**.
  - **Files:** new `test/docker/compose.yml` + Makefile target; SSHRunner seam unchanged.
  - **Deps:** WS2.1+WS2.2. **Decision:** how the test targets `worker` not hardcoded `localhost` →
    **(a)** parametrize via `HARMONIK_E2E_SSH_HOST` for the Go-test path **and (c)** a real
    subprocess+CLI drive for the subprocess smoke (complementary, see WS2.4).

- **WS2.4 — A subprocess daemon-boot test (real `harmonik` as a separate process).**
  - **Accept:** (1) the WS2.3 containerized run IS the subprocess variant; (2) a non-docker smoke that
    `exec`s the built binary, waits for the socket, submits one bead via CLI, asserts terminal outcome.
  - **Files:** new test under `internal/daemon/` or `cmd/harmonik/` (new `-tags subprocess`); reuses
    the socket-probe from `cmd/harmonik/supervise_integration_hkqx702_test.go`.
  - **Deps:** non-docker smoke independent (start early); docker form needs WS2.1-2.3. **Decision:**
    new build tag (vs reuse `scenario`) → **new `subprocess` tag** (independent selection). Twin = `generic-twin` (billing-free).

- **WS2.5 — Harness documented + slotted into the gate map; leaves the WS4 credential mount point.**
  - **Accept:** `test/docker/README.md` documents build/run/key handling + assessor invocation;
    WS1.5's map cites docker as the localhost-SSH-independent required tier; a documented (stubbed)
    auth mount point for WS4's real-agent cells (never `ANTHROPIC_API_KEY`).
  - **Files:** `test/docker/README.md`; cross-link `docs/methodology/TESTING.md`.
  - **Deps:** WS2.1-2.4.

### WS3 — Twin↔real parity harness ⭐ (highest-value correctness work)

> **Spec input for the whole WS3 (do NOT skip):** `docs/twin-parity-audit-2026-05-14.md` enumerates
> where each twin can/cannot match reality. It flags **2 irreducibly-real-only stages** — tmux
> splash-dismiss and physical-Enter/pane-targeting — that CANNOT be twinned. Parity is therefore
> scoped to the **wire/event layer**; the real agents cover the physical-delivery layer (per locked
> decision #3). F1/Claude-A must cite this audit and its exclusions explicitly.

- **WS3-F1 — Normalized-event canonicalizer + equivalence assertion library.** *(gates all slices — build first)*
  - **Accept:** helper reads `.harmonik/events/events.jsonl`, canonicalizes each record (strip
    timestamps/UUIDs/PIDs/paths), exposes `AssertStreamEquivalent(twin, real, opts)` (ordered-subsequence
    on event kinds + whitelisted stable payload fields) and `AssertTimingWithinTolerance(lines, edges, tol)`;
    a mutated fixture (dropped `hook_fired`, reordered `agent_ready`) fails; unit-tested with **zero**
    real-agent invocation.
  - **Files:** new `internal/twinparity/`; reuses projection logic from `test/scenario/harness_test.go:290-317`;
    kind vocabulary = `core.EventType` + `handlercontract.ProgressMsgType`.
  - **Deps:** none. **Decision:** equivalence granularity → **kind + whitelisted stable payload fields**
    (kind-only misses payload drift; full-payload too brittle), whitelist extended as drift is found.
  - **Concrete assertion targets** (found in-code): terminal set `outcome_emitted`/`hook_fired`/
    `bead_closed`/`agent_completed`/`run_completed`; timing edges `agent_ready→outcome_emitted`,
    `outcome_emitted→hook_fired`, `hook_fired→bead_closed`; anomaly emitters `agent_ready_timeout`,
    `post_agent_ready_hang`, `agent_warning_silent_hang`, `agent_resumed_after_warning`.

- **WS3-Claude (priority) — A→B→C→D.**
  - **Claude-A — real-session capture format + `CLAUDE_LIVE=1` capture harness.** Accept: `make
    capture-claude-fixtures` writes `testdata/twin-parity/claude/<scn>/{wire.ndjson, events.jsonl, meta.yaml}`
    with a complete `handler_capabilities…agent_completed` stream + terminal `bead_closed`/`run_completed`;
    reference captures committed for happy-path + review-loop. Files: new `testdata/twin-parity/claude/`;
    wire-tap at `internal/handlercontract/watcher_hc011.go`; shares `e2e_real_claude` tag + credfence.
    Decision: tap the watcher reader (raw tee) vs reconstruct from events.jsonl → **raw tee** (events.jsonl is lossy).
  - **Claude-B — twin `--replay-path` mode.** Accept: twin replays a Claude-A capture and yields an
    `events.jsonl` that `F1.AssertStreamEquivalent` finds equivalent to the capture's own (round-trip
    identity); malformed capture → exit 1; existing modes unchanged. Files: `cmd/harmonik-twin-claude/main.go`
    + new `replaydriver.go`. Deps: Claude-A, F1. Decision: replay timing → **parameterized** (`--preserve-timing`).
  - **Claude-C — property/fuzz harness over timing.** Accept: property test, N≥50 timing draws;
    inv-1 terminal event set identical across draws; inv-2 no anomaly events inside tolerance bands;
    inv-3 exactly the matching anomaly outside them; shrinks to a minimal failing timing vector; keeper
    co-observed via `internal/keepertwin`. Files: new test + per-step delay knobs in twin `scriptdriver.go`;
    daemon timeout emitters. Deps: Claude-B (or scripted baseline), F1. Decision: per-event delay in twin
    YAML vs external pacer → **twin YAML** (twin already owns `startup_delay_ms`).
  - **Claude-D — the parity gate.** Accept: `make test-twin-parity-claude` passes when twin & real
    produce equivalent ordered kind-sequences + terminal outcome with hook-timing within tolerance; a
    drifted twin script fails with a first-divergence diff. Files: new `internal/twinparity/claude_parity_test.go`.
    Deps: F1, Claude-A/B. Decision: live-vs-live each run vs twin-vs-reference-capture → **routine =
    twin-vs-capture (cheap/deterministic) + periodic live re-capture** (catches real-Claude drift).

- **WS3-codex — A→B(→C).**
  - **codex-A — `CODEX_LIVE=1` fresh re-capture job.** Accept: a **new** `make recapture-codex-corpus`
    target (modeled on the existing `capture-fixtures`, `Makefile:145-149` — do not conflate) emits a
    timestamped fresh capture, ≥10 frames, zero `FrameKindRaw`, does not overwrite the frozen reference.
    Files: `internal/codextest/l3_live_hkoe86p_test.go`, new Makefile target. Deps: F1. Decision: coverage →
    **small matrix** (happy/turn-failed/token-usage), not single session.
  - **codex-B — fresh-vs-frozen drift-diff gate.** Accept: diff passes when the fresh capture introduces
    no method outside `codexwire.methodRegistry` + the reactor event-kind set matches; fails on a new/renamed
    method, printing it + fix pointer. Files: new `internal/codextest/livedrift_*_test.go`. Deps: codex-A, F1.
    Decision: auto-promote fresh→frozen vs human-gate → **human-gate** (a corpus is a pinned oracle).
  - **codex-C — twin↔real parity (optional, folds into B's job).** Accept: `F1.AssertStreamEquivalent`
    between the codex twin's replay and the live session. Deps: codex-A, F1.

- **WS3-pi — A→B→C (twin from scratch + real test; pi has neither today).**
  - **pi-A — `PI_LIVE=1` real-agent test (build the oracle first).** Accept: `make test-pi-live` drives
    real pi single-turn, asserts terminal sequence, writes `testdata/twin-parity/pi/<scn>/{ndjson, events.jsonl}`;
    default-skipped. Files: `internal/handler/adapter_pi.go`, `internal/daemon/pijsonlparser.go`, new
    `pi_live_*_test.go`. Deps: F1. Decision: pi auth in isolated harness → **mount pi provider auth**
    (per `plans/2026-06-23-pi-openrouter-harness`), resolved with WS2/WS4 credential decision.
  - **pi-B — pi twin binary from scratch.** Accept: `cmd/harmonik-twin-pi` emits pi's `--mode json`
    lifecycle (`session`→`agent_end`+usage) deterministically; drives `pijsonlparser.go` to the same
    normalized `events.jsonl` a real pi does; maps the reserved `AgentTypePiTwin` constant
    (`internal/core/agenttype.go:21`). Files: new `cmd/harmonik-twin-pi/` (model on `harmonik-twin-codex/`).
    Deps: pi-A, F1. Decision: dedicated binary vs pi-mode of generic twin → **dedicated binary** (pi NDJSON
    dialect differs enough; mirrors codex precedent).
  - **pi-C — pi twin↔real parity gate.** Accept: `make test-twin-parity-pi` passes on equivalent kinds +
    terminal within tolerance; drifted twin fails with first-divergence diff. Deps: F1, pi-A/B.

### WS4 — Revive the live core-loop check (`core-loop-proof`), forced

- **WS4-0 — Resolve run-environment + credential decision.** *(design gate, blocks WS4-2)*
  - **Accept:** a written decision naming the default env, the fallback, the exact auth-mount path, and
    an explicit "NEVER `ANTHROPIC_API_KEY`" (D2) line. **RESOLVED (operator 2026-07-16): default =
    Docker** (subprocess daemon in WS2's docker harness — most reproducible); fallback = subprocess in a
    scratch worktree via `scratch-daemon.sh` (works today, for quick local runs); reject on-box/in-process.
    Credentials = **reuse credfence** (`scratch-daemon.sh:237-240` already unsets API keys) — remaining
    work is making the docker image see the mounted `~/.claude` auth dir.
  - **Files:** new WS4 design note in `plans/2026-07-13-code-revamp/`; reads `scripts/scratch-daemon.sh`, WS2 compose.

- **WS4-1 — Reconcile `integration/core-loop-proof` onto the as-built M2/M3/M4 seams.** *(independent — start early)*
  - **Accept:** `mergeable` flips CONFLICTING→clean; Tier-2 `check` green; `go test -short -race ./...`
    passes; no `codename:quality-system` daemon change silently dropped (diff-audit the merge call sites in
    the branch's `known-red.md`).
  - **Files:** `internal/daemon/workloop.go`, `…/mergetomain_perbead_target_hklgykq_test.go`,
    `…/scenariogate_test.go`, `cmd/harmonik/harness.go`, `.github/workflows/ci.yml`.
  - **Deps:** none. **Decision:** rebase vs merge-main-in → **merge-main-into-branch** (preserves T1–T10
    lineage + PR #20 review history).

- **WS4-2 — Reseat the matrix runner onto WS2's subprocess env.**
  - **Accept:** `core-loop-matrix.sh <scratch> --harnesses pi,codex --substrates local` runs green against
    a subprocess daemon from WS2's harness; remote substrate resolves a `tcp://worker` cell; a fixtureless
    cell is loud-PENDING, never green.
  - **Files:** `scripts/core-loop-matrix.sh`, `scripts/core-loop-seed.sh`, `scripts/scratch-daemon.sh`,
    `scenarios/core-loop-proof/cells.json`. **Deps:** WS2, WS4-0, WS4-1.

- **WS4-3 — Regreen pi + codex cells on the as-built seams.**
  - **Accept:** `core-loop-matrix.sh --assert --specs scenarios/core-loop-proof/cells.json` exits 0 with
    pi+codex cells green, **zero PENDING**; gap-1..5 assertions fold green; model-per-family no-leak holds.
  - **Files:** `scenarios/core-loop-proof/{cells.json, testdata/*.ndjson}`, `scripts/core-loop-assert*.{jq,sh}`.
  - **Deps:** WS4-2.

- **WS4-4 — Enable the claude cells (real-agent, credfenced, heavy cadence).**
  - **Accept:** `core-loop-matrix.sh --enable-claude` drives a real Claude bead→`agent_ready`→real
    change→terminal `pass` against the subprocess daemon, subscription-billed (no `ANTHROPIC_API_KEY` in
    the daemon env); the claude cell is loud-SKIP when auth is unmounted (never false-green).
  - **Files:** `scripts/core-loop-matrix.sh` (`--enable-claude`), the WS4-0 mount, `testdata/claude-*.ndjson`.
  - **Deps:** WS4-0, WS4-3. **Decision:** default-on in the assessor LT? → **default-ON for the assessor
    gate, default-OFF in plain CI** (D2 heavy-tier-is-local).

- **WS4-5 — Forced, single-entry LT-leg command.**
  - **Accept:** one entrypoint returns non-zero on ANY red OR any PENDING (T9 zero-PENDING gate), emits a
    machine-readable per-cell grid the assessor folds into its verdict, and is named in the assessor's LT
    step; listed in the CI-vs-local map as forced-local. **Files:** `scripts/core-loop-matrix.sh`, gate-map
    doc, assessor `operating.md`. **Deps:** WS4-3 (+WS4-4 for the claude leg).

- **WS4-6 — Design pass, review gate, unstall the kerf record.**
  - **Accept:** WS4 design reviewed by independent eyes; PR #20 lands or is superseded by a fresh
    integration branch preserving T1–T10; `.kerf/works/quality-system/spec.yaml` reconciled (advanced or
    annotated "subsumed by M6 WS4"). **Deps:** WS4-5.

### WS5 — Wire the assessor (release-readiness AGENT) + admiral↔assessor signoff  *(agent/launcher/prompt work, NOT `internal/` code)*

- **WS5-1 — Rewrite the operating model: reasoned judgment, NOT bead-count (implements D1).** *(zero-dep first move)*
  - **Accept:** `soul.md:6` and `operating.md:19-25` no longer contain the `br list … --label-any found-by:*`
    block query; `operating.md` gains an explicit "verdict = reasoned judgment" step + a "reconcile
    claimed-done vs actual commits/diffs/tests/reviews" duty; no path returns PASS from an empty bead query.
  - **Files:** `.harmonik/agents/assessor/{soul.md, operating.md}`. **Decision:** keep filing findings as
    beads (record) while dropping the bead-*count* verdict → **yes** (preserves the regression-corpus duty).

- **WS5-2 — Rewrite the mission/handoff schema (v2).** Accept: `schema_version` → 2; `found_by_sources`
  (§4), the §6 durable-epic-mirror rationale, and §8 empty-union rejection removed/repurposed; schema names
  the reasoned PASS/BLOCK, references the good-enough principles (WS5-5) + the admiral↔assessor `--topic gate`
  signoff channel; worked example = a live M6 gate. **Also reconcile the assessor's existing DEPLOY-GATE
  (GATE-0 / 24h rule) in `operating.md`** — the schema-v2 rewrite must either keep it consistent with the
  reasoned-verdict model or explicitly drop it, so `operating.md` is not left internally inconsistent.
  Files: `specs/assessor-handoff-schema.md`, `.harmonik/agents/assessor/operating.md`. Deps: WS5-1.

- **WS5-3 — Stand up the assessor launcher.** Accept: the admiral spawns the assessor through a
  registry-protected path (writes crew Record + `crew-<name>` tmux session, orphan-sweep-safe per hk-zeo5y);
  valid mission → boots, joins comms, reaches the gate; invalid mission → posts `--topic error` and idles.
  Files: `cmd/harmonik/start.go`; `internal/crew/registry.go` already resolves the type folder; manifest
  trigger `spawn:manual`. Deps: WS5-2. **Decision:** first-class `start assessor` role vs generic `crew start`
  → **first-class role** (oversight role needs guaranteed registry protection, not "remembered" — hk-zeo5y).

- **WS5-4 — Refinable 30–50-line assessor personality file.** Accept: a 30–50-line file distilling
  critic+QA+architect ("final quality gate; a false approval costs 10-100×; evaluate what ISN'T present";
  "if the code is crap it doesn't go through; if something that worked is now broken it doesn't go through"),
  explicitly refinable; a distilled *seed*, not a re-paste of the 21KB critic. Files: new
  `.harmonik/agents/assessor/personality.md`; seeds from the installed oh-my-claude
  `{critic,qa-tester,architect,code-reviewer}.md`. Deps: WS5-1.

- **WS5-5 — "What is good enough" principles.** Accept: a doc stating the release bar (LT matrix green incl.
  required cells; XT no unmitigated critical; CR no BLOCK-class defect; claimed-done reconciles) tied to D2's
  risk-tiering; referenced by WS5-2 + WS5-6. Files: new `.harmonik/agents/assessor/good-enough-principles.md`.
  Deps: WS5-1; consumes WS1's risk-tiering rule.

- **WS5-6 — Update the admiral's instructions with explicit final-signoff authority.** Accept:
  `.harmonik/agents/admiral/{soul.md, operating.md}` state that the admiral spawns the assessor at a gate,
  receives its reasoned PASS/BLOCK + concerns over `--topic gate`, discusses against the good-enough
  principles, and **makes the final release decision** — without violating the admiral's "I direct, I do not
  edit repo files / dispatch beads" bound (the release call is authority, not a repo edit). Deps: WS5-5.

- **WS5-7 — Wire the three legs + subagent-delegation model.** Accept: `operating.md` names the LT leg as
  the WS4 forced command (WS4-5), XT as an exploratory-break fan-out on WS2's env, CR as an independent cold
  diff review; explicitly delegates each to subagents (D1) and folds evidence into one reasoned verdict;
  independence bound preserved (never grades work it helped build). Deps: WS4-5, WS5-1.

- **WS5-8 — End-to-end dry-run + review gate (capstone).** Accept: a live dry-run (gating the M6 branch
  itself) — admiral launches assessor (WS5-3) → assessor delegates → runs LT/XT/CR → reasoned PASS/BLOCK
  citing evidence + a beads-vs-reality reconciliation → posts `--topic gate` → admiral records the final
  decision against the principles; independent-reviewer sign-off on WS5. Deps: WS5-3/4/6/7 + WS4 green.

---

## 3. CI-vs-local test map + risk-tiering rule (Decision #2 deliverable)

**CI-vs-local split rule:** GitHub CI hosts everything with **no host-level external dependency** (pure-Go
units + the in-process scenario suite with compiled twins). Anything needing `ssh localhost`, Docker, or a
real agent CLI/credentials is the **assessor's forced local heavy tier.**

| Suite | Where today | Gates merge? | Note |
|---|---|---|---|
| `make check-short` (`-short -race`, vet, build, lint) | GitHub CI | **Yes** | pure-Go, already there |
| in-process scenario suite (`test-scenario`, compiled twins) | GitHub CI (`scenario.yml`) | **No → WS1.1 flips it** | can run on ubuntu-latest; only config blocks it |
| `RemoteSubstrate_Localhost_E2E` | wherever scenario runs, **iff `ssh localhost`** | No | silently skips green without sshd → WS1.3 / move to docker |
| WS2 docker controlled-E2E | *(to build)* local/assessor | Assessor-forced | host-independent replacement for localhost-ssh tier |
| WS3 twin↔real parity / WS4 live core-loop | *(to build)* local/assessor | Assessor-forced | real agents, credfenced, token spend — never shared CI |

**Risk-tiering rule** (WS1 owns; path-glob floor the assessor may only *raise*):
- **Tier 0 — CI-only:** docs/prose (`docs/**`, `*.md`, `plans/**`, `specs/**`), comments, test-only edits to
  already-covered packages. Gate = `check-short` + in-process scenario. Merge to `main` on green CI.
- **Tier 1 — heavy tier required:** any `internal/**` or `cmd/**` change touching the work loop,
  lifecycle/tmux (`SSHRunner`, worktree), queue/dispatch, merge-back, or the remote/substrate path.
  Gate = Tier 0 + localhost-SSH E2E (WS1.3 required-mode) + WS2 docker E2E.
- **Tier 2 — full assessor gate:** release cuts, changes to the agent-interaction seam (twins, keeper, hook
  bridge), or anything the author/admiral flags high-risk. Gate = Tier 1 + WS3 parity + WS4 live core-loop +
  assessor CR/XT legs.

---

## 4. Open items requiring the operator (surfaced, not decided here)

1. **Explicit "go"** to start the first wave (WS1.2/1.3 ‖ WS3-F1→Claude-A/B ‖ WS4-1 ‖ WS5-1).
   *(Operator 2026-07-16: not ready to implement yet — no go given.)*
2. **WS1.1 — RESOLVED (operator 2026-07-16): sequenced LAST**, after all M6 test tiers are built,
   integrated, and green. The required-check flip is the closing step of M6, not first-wave. Still an
   operator/admin GitHub action when we get there.
3. **WS1.4 — RESOLVED (operator 2026-07-16): NO git hook.** `agent-review` stays manual/assessor-invoked;
   the assessor owns rigorous review at the gate. No per-commit block.
4. **WS4-0 — RESOLVED (operator 2026-07-16): Docker default**, scratch-worktree fallback for quick local
   runs; auth-mount into the isolated env is the only remaining piece (credfence handles the no-API-key half).

Everything else in this plan is an implementer/design-pass decision with a recommendation on record.

---

## 5. What this plan deliberately does NOT do

- Does not start any build (holding for go).
- Does not reopen the four operator-locked decisions (problem-space §4).
- Does not run any real-`gb-mbp` proof (that's the M4 gate, gated behind this whole milestone).
- Does not rebuild the in-process harness or the core-loop-proof matrix from scratch (revive, don't restart).
