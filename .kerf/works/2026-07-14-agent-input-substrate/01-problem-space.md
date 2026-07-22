# 01 — Problem Space: `agent-input-substrate` (M2 of the code-revamp)

> Pass 1 (problem-space) for the M2 phase of the 2026-07-13 code-revamp. The **design pass
> is DEFERRED** — P1 (`session-restart-substrate`) must prove the reactor/replay seam on a
> real vertical before M2's design is drawn. This doc + `02-components.md` (decompose) are
> the front matter; work stops after decompose. Signoffs waived for these two passes.
>
> All `file:line` citations verified against the tree on 2026-07-14 (not trusted from the
> dossier blindly). Primary dossier: `plans/2026-07-13-code-revamp/research/05-io-boundaries.md`
> Part A. Plan framing: `PLAN.md` §2 area #3, §3a, §4 (M-input); `ROADMAP.md` M2 row.

> **ADDENDUM — SUPERSEDED framing (COORD c019 + c021, operator-ratified 2026-07-14; design note
> `plans/2026-07-13-code-revamp/M2-RESCOPE-hook-sourced-ack.md`).** This pass-1 problem-space predates
> the re-scope. Three of its framings are now SUPERSEDED for the **Claude path**: (1) "tmux becomes
> observation-only" and "the pasteinject/tmuxsubstrate input stack is deleted" (Summary §"The change",
> Goal 3, SC3/SC4) — WRONG for Claude: **tmux paste is Claude's FIRST-CLASS input transport** (a peer
> input method, not demoted/deleted); observation-only + wholesale deletion apply ONLY to the
> structured **Codex** driver path. (2) The "structured-protocol driver (claude headless `stream-json`
> / Agent SDK stdin)" of Goal 2 — there is NO claude structured driver (subscription-first forbids
> `-p`/API-key); the structured driver is the **Codex app-server** driver, proven & subscription-
> compatible. (3) The ack: the tmux/Claude path's positive ack is now SOURCED from the
> **Claude-hook-bridge** (`outcome_emitted` on `Stop`, `agent_ready` on `SessionStart` start/resume;
> [specs/claude-hook-bridge.md]), NOT pane-scraping and NOT a Claude wire protocol; the resume-hang
> fix is "wait for the hook signal under a bound, emit `agent_input_stale` on silence." The Non-goal
> "NOT the hook-bridge transport" still holds (M2 does not rebuild the bridge) — but the bridge is now
> the ack SOURCE, not merely an incidental async signal. The load-bearing spec (`agent-input.md` AIS)
> and the Tasks pass (`07-tasks.md`) carry the corrected contract; this pass is retained as-authored
> for provenance.

---

## Summary — what is changing and why

The channel by which the daemon pushes input into a hosted agent (the initial task seed, the
resume brief, the submit Enter) is **ack-free and out-of-band**. Today:

- `handler.Substrate` — the clean, depguard-safe seam the daemon composition root injects a
  subprocess-host through — has **exactly one method, `SpawnWindow`**
  (`internal/handler/substrate.go:30`), and its returned handle `SubstrateSession`
  (`:101`) is deliberately narrow: **no input/write method.** The doc comment states it
  outright: *"SendInput and CloseStdin are not part of this interface; the substrate owns
  the child's stdin"* (`internal/handler/substrate.go:97`). The `substrateSessionAdapter`
  that wraps a session back into a `handler.Session` makes `SendInput` and `CloseStdin`
  **hard no-ops returning nil** (`substrate.go:140`, `:173`).
- Because the seam gives the daemon no typed input channel, agent input flows entirely
  **out-of-band** via a set of **optional side-interfaces the daemon type-asserts for** —
  `enterSender`, `paneCapturer`, `quitSender`, `paneLivenessChecker`, `paneOutputSizer`,
  `commandRunnerProvider` — all declared in `internal/daemon/pasteinject.go` (`:187`, `:206`,
  `:236`, `:254`, `:280`, `:493`), none of them methods on `Substrate`/`SubstrateSession`.
  Delivery is a per-run tmux dance (`load-buffer` → `paste-buffer` → `send-keys Enter`)
  against the pane captured at spawn (`tmuxsubstrate.go:2218` `WriteLastPane`; osadapter
  verbs at `internal/lifecycle/tmux/osadapter.go:379/405/464/486/512/541`).
- **`exit 0 ≠ TUI accepted.`** `tmux load-buffer`/`paste-buffer`/`send-keys` return 0 once
  tmux hands the buffer/keys to the pane, **not** once claude's React/ink TUI renders or
  submits it — the code says so at `pasteinject.go:131`. There is **no ack on the write.**
- Two **half-measures** compensate, and both are non-authoritative:
  1. **Screen-scrape seed verification** — `injectAndVerifySeed` (`pasteinject.go:1708`)
     does a `capture-pane` scrape after each write and `strings.Contains(pane, marker)`.
     It is skippable: if capture infra fails on every attempt it returns success anyway
     (`:1773`), and a substrate with no capture capability returns success after the first
     write (`:1729`).
  2. **Blind submit-Enter ×3** — `sendSubmitEnterWithRetry` (`pasteinject.go:1795`) sends
     the submit Enter, then re-sends it `resumeSubmitRetries` (2) more times on a fixed
     400ms delay **with no check that submission happened** — the design comment concedes
     *"we cannot positively confirm submission"* (`pasteinject.go:100`).

The only positive signal that input was accepted is **indirect and async**: the first
`agent_heartbeat` over the hook-bridge socket, or a new git commit; failing those, blind
timeouts (`launchHeartbeatTimeout` 180s → kill) clean up. There is no synchronous
confirmation the keystroke/paste was consumed.

**The change:** rebuild the agent-input channel behind a **real structured-protocol driver
on the `handler.Substrate` seam** — a second `Substrate` implementation that owns a typed
input method returning a **real ACK**, so exit-0 stops lying. tmux becomes
**observation-only**; the `pasteinject`/`tmuxsubstrate` input stack is deleted; and a
**live capture tee** (`internal/apptap`, currently test-only) is spliced onto the input path
so the stream is recorded for replay. This is the second instantiation of the
`internal/substrate` record→replay seam that P1 builds and proves.

### Why now / why this is worth a rebuild (honest framing)

The case rests on **ack-freeness + incident density**, *not* on a large sleep footprint.
The census's motivating "~48 sleep sites" was a **test-file count**; the live path
(`tmuxsubstrate.go` + `pasteinject.go`) has **~1 production `time.Sleep`** — the blind waits
are `time.After`-in-`select` (10 sites), behaviourally identical but not literal `Sleep`
(`05-io-boundaries.md` §A3; census `PLAN.md:186`). The real weight is **44 tmux incident
beads across 4 generations of workaround-on-workaround** (census `REPORT.md:40`) piled on a
boundary that structurally cannot confirm acceptance. Rebuilding gives the boundary a real
protocol ack + a recorded, replayable stream — the same quality mechanism codex and P1 have.

---

## Goals

1. **`handler.Substrate` gains a real, typed input method with an ACK.** Input stops flowing
   through type-asserted side-interfaces; the seam itself carries a first-class
   `SendInput`/`Submit`-shaped operation whose return means "the agent accepted this," not
   "tmux queued a buffer."
2. **A structured-protocol driver** (claude headless `stream-json` / Agent SDK stdin, per
   census `PLAN.md:190`; codex app-server already proves the shape) becomes the production
   input path — a second `Substrate` implementation injected at the same composition-root
   seam, side-by-side with the tmux impl during the bake window.
3. **tmux becomes observation-only.** Its `capture-pane` read seam may survive as a
   human-facing observation window; its `load-buffer`/`paste-buffer`/`send-keys` **write**
   path is retired.
4. **A live capture tee records the input stream for replay.** `internal/apptap.Tap`
   (`InCapture`/`OutCapture`, protocol-agnostic, `tap.go:48/58/63`) gets its **first
   production consumer** on the input path — closing the ROADMAP orphan "apptap never wired
   to a production capture path" (`ROADMAP.md:69`).
5. **The input path gets an L0–L3 replayable test taxonomy** — the same fault-injecting
   harness bar M4 must meet (census `PLAN.md:197`): a stalled-agent injection asserts the
   new substrate emits **output-or-stale**, plus N consecutive full bead runs on the
   structured driver with **zero sleeps and zero capture-pane scraping** in its path.
6. **Delete the input stack** — `pasteinject.go` (2633 LOC), the input portions of
   `tmuxsubstrate.go` (2735 LOC), and (where they served only paste-injection)
   `internal/lifecycle/tmux/` — **after** a defined bake window, never on a single ported run.

---

## Non-goals (explicit)

- **NOT the remote/SSH channel.** The fresh-`ssh -- '<string>'`-per-op, ControlMaster-off,
  event-dark worker boundary (`05-io-boundaries.md` Part B) is **M4 — remote-substrate**
  (`ROADMAP.md:56`), reconciling the existing `remote-substrate` + `remote-substrate-phase2`
  works. M2 touches only the agent-input (local-pane) path. Where the tmux path is shared
  local/remote (the `runner==nil`/`runner!=nil` arms), M2 rebuilds only the input verbs; the
  remote transport rebuild is M4's.
- **NOT the daemon god-function.** `beadRunOne` (~2,366 lines) and the `mergeMu` boundary are
  **M3 — run-state-machine** (`ROADMAP.md:54`). M2 injects at the already-clean
  `handler.Substrate` seam and **does not reach into `beadRunOne` internals** — the panel
  confirmed M2-before-M3 precisely because the seam isolates it (census `PLAN.md:194`).
- **NOT a rewrite of the reactor/replay seam.** `internal/substrate` (`EventSource[E]`,
  `Effector[A]`, `Run[E,A]`, `ReplayCodec[E]`, `Twin[E]`, `FaultConfig`, `ClockPort`,
  `FakeEffector`, `SyntheticSource`) is **built and green** as of P1's T-series. M2
  **instantiates** it for the input vertical; it does not re-extract it.
- **NOT the crew-spawn path.** `handler.Substrate` also spawns crew/consolidate windows
  (`crewstart.go:180/290/308`). M2 rebuilds the **agent-input write** channel; spawning
  windows via the seam is unchanged (see Constraints — deletion scope).
- **NOT the hook-bridge / `agent_heartbeat` transport.** The async acceptance signal stays;
  M2 adds a *synchronous* ack in front of it, it does not replace the heartbeat.

---

## Constraints

1. **Out-of-pipeline.** The daemon is stopped for the whole revamp (`ROADMAP.md` "operating
   fact"). All M2 work is single-writer, human-reviewed, merged to a branch; nothing flows
   through the live pipeline until the DOGFOOD gate. This work does not enable the daemon.
2. **Must not regress current dispatch while rebuilding.** The new driver runs **side-by-side**
   with the tmux input stack; the tmux escape hatch is **not deleted until a defined
   abort/rollback + bake window passes** (census `PLAN.md:202`). A wrong ack/liveness
   contract on a substrate whose escape hatch is already gone would re-import the resume-hang
   class — so the bake window + fault-harness are the guard, not optimism.
3. **The input protocol must produce a real ACK** — a synchronous, protocol-level
   confirmation that the agent accepted the input, so `exit 0` (and the async
   heartbeat/commit) stop being the only acceptance signals. The ack contract must tie to a
   **bounded-liveness / output-or-stale** guard (the M2 analog of P1's SR9 and STEP-0a),
   defined in the deferred design pass.
4. **Reuse the proven seam, don't fork it.** The driver must be an `EventSource[E]` /
   `Effector[A]` instantiation over the generic `Run[E,A]` loop, replayed by `Twin[E]` with
   `FaultConfig`, timed through `ClockPort` — no bespoke replay engine
   (`internal/substrate/{seam,replay,clock,doubles}.go`).
5. **depguard boundary preserved.** `handler` never imports `internal/lifecycle/tmux`; the
   concrete substrate is injected by the daemon composition root (`substrate.go:5`,
   `PL-021b`/`HC-054`). Any new input method added to the seam must keep that inversion — the
   handler declares the port; the daemon supplies the driver.
6. **Carry forward the honest-probe discipline** where the input path relies on it, and fold
   the codex **daemon-harness WAL-guard** (380 lines of symptom-treatment, census
   `REPORT.md:35`; `ROADMAP.md:73` homes it to M2) into the rebuild rather than leaving it as
   a bolt-on.
7. **Spec-first.** M2 is kerf-first (`codename:agent-input-substrate`): spec the input
   framing, ack, liveness, and observation-only-tmux contract into `specs/` before
   implementing. The spec-file naming/prefix is a design-pass decision (P1 used
   `specs/replay-substrate.md` / RS + `specs/*` SK for the vertical).

---

## Success criteria (concrete, testable)

The specs + code, when M2 is done, satisfy:

- **SC1** — `handler.Substrate` (or its session handle) defines a **real input method with an
  ack**; the `substrateSessionAdapter` no-op `SendInput`/`CloseStdin` (`substrate.go:140`,
  `:173`) are gone or backed by a genuine implementation on the structured driver.
- **SC2** — The **type-asserted side-interfaces** (`enterSender`, `paneCapturer`,
  `quitSender`, `paneLivenessChecker`, `paneOutputSizer`, `commandRunnerProvider`) are no
  longer the input mechanism; input is a first-class seam method.
- **SC3** — **tmux is observation-only**: no `load-buffer`/`paste-buffer`/`send-keys` on the
  production input path; at most `capture-pane` survives as a read/observation window.
- **SC4** — **`pasteinject.go` and the input portions of `tmuxsubstrate.go` are deleted**
  (after the bake window), along with `internal/lifecycle/tmux/` write verbs that served only
  paste-injection.
- **SC5** — A **live capture tee** (`apptap.Tap`) records the input stream in production for
  replay — apptap's first production consumer.
- **SC6** — The input path has an **L0–L3 replayable test taxonomy** (zero-token L0/L1/L2 +
  one env-gated live L3 + drift canary), including a **fault-injection integration test**
  where a stalled agent forces **output-or-stale** (never silence), plus **N consecutive**
  full bead runs on the structured driver with **zero sleeps and zero capture-pane scraping**
  in its path.
- **SC7** — Defined **abort/rollback criteria + bake window** exist before any deletion, and
  the tmux escape hatch survives until they pass.

---

## Affected areas

**Code (verified):**
- `internal/handler/substrate.go` — the seam (`Substrate` :30, `SubstrateSession` :101, the
  no-op adapter :129–199) gains a real input method.
- `internal/daemon/pasteinject.go` (2633 LOC) — deleted; the side-interfaces + inject/verify
  logic live here.
- `internal/daemon/tmuxsubstrate.go` (2735 LOC) — input portions deleted; `perRunSubstrate`
  / `WriteLastPane` (`:2218`) retired; observation (`CapturePane`) may survive.
- `internal/lifecycle/tmux/{osadapter.go,runner.go}` — write verbs (`LoadBuffer` :379,
  `PasteBuffer` :405, `SendKeysEnter` :464, `SendKeysQuit` :486, `WriteToPane` :541) retired;
  `CapturePane` :512 may survive for observation.
- `internal/daemon/workloop.go` — composition-root wiring of `deps.substrate`
  (`:489`, `:4346`, `extractTmuxAdapterFromSubstrate` :8064); the new driver is injected here.
- `internal/daemon/crewstart.go` — spawn path via the seam (unchanged for spawn; input path
  affected if crew windows are seeded).
- `internal/apptap/tap.go` — gains its first production consumer.
- `internal/substrate/*` — instantiated (not modified) as the reactor/replay/clock host.
- The codex daemon-harness WAL-guard (census `REPORT.md:35`) — folded in.

**Specs (design-pass detail):** a new input-protocol / substrate-driver spec in `specs/`
defining input framing, the ack, the liveness/output-or-stale contract, and the
observation-only-tmux boundary; likely amendments to `specs/process-lifecycle.md` (PL-021b
family) and `handler-contract.md` (HC-054) where the seam contract widens.

---

## Relationship to other works

- **P1 — `session-restart-substrate` (Ready; T6–T14 remaining):** the **hard dependency and
  the template.** P1 builds and proves `internal/substrate` (the generic Go-generics seam —
  design decisions D1 ReplayCodec-as-generics, D2 fused `ReplayCodec[E]`, D3 generic fault
  vocabulary, D4 `ClockPort` in substrate, D13 trace-driven-Twin measurement; bench
  `.kerf/works/session-restart-substrate/04-design/00-decisions.md`). M2 is the **second
  instantiation** of that seam and must not begin its design until P1 proves the method
  generalizes (`ROADMAP.md:88`). The seam packages already exist and are green
  (`internal/substrate/{seam,replay,clock,fakeclock,doubles}.go`).
- **M4 — remote-substrate (sibling IO boundary):** M2 and M4 are the two "ack-free channel"
  rebuilds from the same dossier (`05-io-boundaries.md` A vs B). Same DoD bar (fault-injecting
  harness). M4 hard-depends on M3's merge-queue; M2 does not. Keep the two scopes disjoint:
  M2 = local agent-input write; M4 = remote/SSH transport.
- **M3 — run-state-machine:** M2 runs **before** M3 and does not touch `beadRunOne`. The
  clean seam is what allows that ordering (census `PLAN.md:194`).
- **`handler-pause` (problem-space, 56d):** overlaps the handler/substrate seam
  (`codename:handler-pause`) — its pause-checker interacts with the input path but is a
  distinct concern (pausing dispatch, not the input protocol). Reconcile scope at design; do
  not fold it into M2.
- **`codex-app-server` (tasks) + `scripted-twin` / `keeper-test-harden`:** the codex stack is
  the *shape* M2 copies (dossier 03); `scripted-twin` and the taxonomy works inform the L0–L3
  harness. No duplication — M2 reuses the extracted `internal/substrate`, not the
  codex-specific packages.
- **`testing-strategy-uplift` / `validation-net` / `quality-system`:** the enforcement +
  taxonomy homes (`ROADMAP.md` Track C, M1). M2's L0–L3 taxonomy composes with the L0–L3
  policy those works define; SC6's "zero sleeps / zero scraping" is an enforcement ratchet
  item.
