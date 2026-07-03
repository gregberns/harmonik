# Pi + OpenRouter Harness — Change Design

> **Codename:** pilot (`codename:pilot`) · **Epic:** hk-ag97p · **Author:** kynes (design crew)
> **Source brief:** `plans/2026-06-23-pi-openrouter-harness/README.md` (admiral, 2026-06-23)
> **Status:** design draft — PENDING the reviewers-with-critics gate (§9). Not build-ready until critics clear.
> **Supersedes scope of:** the older `pilot` bench artifacts (see § Divergence note below).

---

## Divergence note — relationship to the existing `pilot` bench

The `pilot` codename was first used for a **different** initiative: "Pi-driven dispatch & control
plane" — a daemon-lifecycle / queue-control design (`cognition-loop`, `execution-model`,
`queue-model`, `operator-nfr` components on the bench). That work is about *which beads get
dispatched and when* (quiet-boot daemon, eager-refill curation, `supervise pause/resume` verbs).

**This design is orthogonal to it.** It designs **Pi-the-coding-agent as a per-bead implementer
harness** (mirroring codex), plus a later crew runner. It does **not** touch, and does **not**
overwrite, the older components:

- **Reused from the old bench (cited, not modified):** the mechanism/cognition byte-clean
  separation (`01-problem-space.md` C1), the idempotency-keying discipline (C2), the
  two-phase-done verification pattern (`04-design/cognition-loop-design.md` CL-051 — reused as the
  Pi commit-completion gate, §3.5), the single-source-of-truth principle (C4), and the prose
  conformance-scenario house style (`02-components.md`).
- **NOT touched (old bench owns these):** EM-066 quiet-mode topology, QM-054 queue pause
  semantics, CL-071 dispatch curation, ON-056/057 pause producers, CL-051 reconciliation Tier-2
  routing. These are daemon-side; a per-bead harness is a child process and produces no
  control-plane events.

New component files written under this codename are prefixed `pi-harness`/`crew-shim` so they sit
beside the old components without collision.

---

## 1. Objective & shape

Open harmonik to **any model/provider Pi supports** (OpenRouter free + paid, direct
Anthropic/OpenAI/Google APIs) by adding a **Pi implementer harness**, then a **Pi crew runner**.
The win is provider-agnosticism and the ability to **trade tokens for time** (cheap/weak
implementers wrapped in the existing heavy test+review loop) on capacity *off* the Anthropic
budget — but **only for mechanical, deterministically-checkable beads** (hard scope boundary, §8).

The de-risking order is locked by the brief and preserved here:

| Phase | What | Blast radius | Gated on |
|---|---|---|---|
| **0** | Pi as a per-bead implementer harness (mirrors codex) | Low — extension point already exists | — (do first); **build-ready after review** |
| **1** | One mechanical crew on Pi via a resident Pi harness (pilot: gurney) | **High — DESIGN SPIKE REQUIRED** (§4) | Phase 0 + the §4.2 spike resolving named unknowns |
| **2** | Generalize crew launch through a provider abstraction | High | Phase 1 proving one crew |

Each phase MUST prove out before the next starts. The phases are independently shippable.

---

## 2. What already exists (so Phase 0 is mostly wiring)

The harness extension point is built and codex is the working template:

- **`Harness` interface** — `internal/handlercontract/harness.go:169–203`, **8 methods** (table in §3.1).
- **4-tier resolver** — `internal/daemon/harnessresolve.go:53–118` (tier-1 `harness:<type>` label →
  tier-2 queue default [stub hk-4x3rg] → tier-3 DOT node attr [stub hk-u67of] → tier-4
  `Config.DefaultHarness`). **No silent fallback to claude on an unknown/unregistered selector** —
  `HarnessRegistry.ForAgent` returns a hard error (`handlercontract/harnessregistry.go:119–137`).
- **`AgentTypePi = "pi"`** already declared, unimplemented — `internal/core/agenttype.go:19`.
- **Two declared *classes* of seam** (harness-blind shared loop, doc comment `harness.go:160–166`):
  (a) the launchSpecBuilder/registry lookup, and (b) the `Completion()` gate. NB the doc comment's
  "`dot_cascade.go:643`" is a **stale in-comment reference** — the live `Completion()` gates are
  value-gated at ~8 sites (`workloop.go:3999/4080/4232`, `dot_cascade.go:1493/1577/1674`,
  `reviewloop.go:679/767`). They key on the `Completion()` **value**, not the agent type, so a second
  ProcessExit harness reuses every one with **no new branch** — but an implementer should not go
  hunting for literally two `if`s. Everything else (worktree create/merge/remove, commit-detection via
  `git rev-parse HEAD`, DOT cascade, review-loop) is harness-blind.

**Consequence:** making Pi a `CompletionProcessExit` harness (like codex) auto-disables — with zero
new branches — the agent-ready wait (`workloop.go` ~3999), the bracketed paste-inject
(`pasteinject.go`, skipped when `Completion()==ProcessExit`), and `pasteInjectQuitOnCommit`
(`dot_cascade.go` ~1493, `reviewloop.go` ~679). Pi receives its task via **argv**, not pane paste.

---

## 3. Phase 0 — Pi per-bead implementer harness

### 3.1 The 8 interface methods (Pi values vs codex)

| # | Method | Codex | **Pi** |
|---|---|---|---|
| 1 | `AgentType()` | `codex` | `core.AgentTypePi` (`"pi"`) |
| 2 | `LaunchSpec(rc)` | `codex exec …` | `pi --mode json …` (§3.2) |
| 3 | `Seed(sess,rc)` | no-op | **no-op** (task via argv) |
| 4 | `Retask(sess,fb,rc)` | no-op | **no-op** (feedback via argv on resume) |
| 5 | `Teardown(sess)` | best-effort Kill | **Kill** (load-bearing — Pi may not self-exit, §3.4) |
| 6 | `DetectReady(ev)` | true iff `agent_ready` | satisfy **HC-041**: never true on `launch_initiated` (§3.3) |
| 7 | `SessionIDPolicy()` | `SessionIDCaptured` | **`SessionIDCaptured`** — capture from NDJSON `session` header (§3.3) |
| 8 | `Completion()` | `CompletionProcessExit` | **`CompletionProcessExit`** + an `agent_end` watcher (§3.4) |

New files (mirror codex): `piharness.go`, `pilaunchspec.go`, `picommit.go`, `adapter_pi.go` + unit
tests. Edits (2): register the harness in `harnessregistry.go:39–48`; register the adapter at
startup (mirror `RegisterCodex`).

### 3.2 Launchspec (`pilaunchspec.go`) — argv / env / cwd

Pi's CLI surface (external research, written up at `03-research/pi/findings.md` — confidence labels
there are load-bearing):
`pi [options] [@files] [messages]`; `--mode json` = NDJSON; `--provider`, `--model provider/id`;
task as a **positional arg**; resume via `--session <id>` / `-c`; keys via env (`OPENROUTER_API_KEY`,
`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`) or `--api-key`. The NDJSON event shapes
(`session` header, `agent_end`) are asserted from docs and carry a **confirm-by-test** obligation in
Phase 0 (findings.md §2).

```
# Initial turn
pi --mode json --provider <prov> --model <prov/id> "<seed-prompt>"
# Resume turn (iteration ≥ 2)
pi --mode json --session <captured-session-id> "<feedback-prompt>"
```

- **WorkDir** = the run's worktree (Pi's `read/write/edit/bash/grep/find/ls` operate on CWD;
  NDJSON `session` header echoes `cwd` — confirm-only). **No `--sandbox` flag** — Pi is unsandboxed,
  so unlike codex it *can* `git commit` itself; the seed prompt instructs it to (commit fallback in
  §3.5 is the safety net).
- **Seed prompt** mirrors codex's template: "Read `.harmonik/agent-task.md`, implement the bead,
  commit with a `Refs: <bead-id>` trailer." The daemon writes `agent-task.md` (harness-agnostic).
- **`StdinDevNull: true`** (codex's `LaunchSpec` field, NFR7). NB: Pi issue #4303 is a `/dev/null`-
  stdin `epoll_wait` hang — the §3.4 `agent_end` watcher makes that hang **harmless** (we kill on the
  event, never wait on Pi's process exit). Phase-0 tests must exercise this exact stdin path.
- **Env (`buildPiEnv`)** — mirror codex `buildCodexEnv` (`codexlaunchspec.go:221–264`) but with
  **allowlist, not denylist, strip semantics** (review B1): codex strips a *fixed 2-key list* because
  it bills one of two pools; Pi exposes the **full open provider set** (§6.1), so an enumerated
  denylist can never be complete — a `MISTRAL_API_KEY`/`GROQ_API_KEY`/`DEEPSEEK_API_KEY`/custom key
  would survive the strip. So: empty-override **every** credential var matching a maintained
  provider-key table / `*_API_KEY` pattern **except** the selected `api_key_env`, then **inject only
  the selected provider's key** from the operator environment. (Severity depends on whether Pi
  autodetects a provider from env vs honoring `--provider` authoritatively — an UNCONFIRMED Phase-0
  item, findings.md §4; the allowlist strip is correct belt-and-suspenders either way.) Plus the
  oh-my-zsh prompt-suppression vars codex sets. **No key value is ever written to config and never
  passed as `pi --api-key <value>`** (closes the ps/argv leak) — config names the env var; the value
  lives in the operator's environment (§7). A single shared key-resolution helper feeds **both**
  `buildPiEnv` and the guard (§3.6) so they can never disagree (codex shares `resolveCodexHome` for
  the same reason).

### 3.3 Session-id capture & ready (`piharness.go`)

- Pi emits, as the **first NDJSON line**, `{"type":"session","version":3,"id":"<uuid>","cwd":…}`.
  `SessionIDPolicy()=SessionIDCaptured`; capture `id` exactly as codex captures `thread_id` from its
  first `thread.started` event, store it in run artifacts, and pass it as `PriorSessionID` →
  `--session <id>` on the resume turn.
- `DetectReady` MUST satisfy **HC-041**: explicitly return `false` for `launch_initiated`; never
  synthesize ready from other signals. For a ProcessExit harness the agent-ready *wait* is skipped
  anyway, but the method must still be HC-041-correct (enforced by `adapterreadydetect_hc041_test`).

### 3.4 Completion — **the one genuine divergence from codex** (Open Q1)

**Decision: `Completion()` returns `CompletionProcessExit`.** That gives free, branchless reuse of
the whole shared loop (skip agent-ready/paste-inject/quit-on-commit; rely on `sess.Wait` + the 90m
`commitHardCeiling`).

**But Pi's process exit is unreliable.** Documented bugs (#4303 `--mode json` hangs in `epoll_wait`
with `/dev/null` stdin; #161, #4942 "does not exit after `main()`") mean a finished Pi can sit
forever — which under pure ProcessExit semantics would burn the **full 90 minutes** per bead before
the ceiling fires. Unacceptable.

**Mitigation — the `agent_end` watcher, hooked into the existing SessionIDCaptured NDJSON path
(corrected per codex-fidelity review):**

1. Pi's terminal NDJSON event is **`agent_end`** (`{"type":"agent_end","messages":[…]}`).
2. The only live consumer of a ProcessExit harness's NDJSON stdout is the per-harness
   `StdoutWrapper`/`SessionIDInterceptor`, assigned in **shared launch code** (`reviewloop.go:430`
   and the workloop analog) inside the block already gated on `implIsSessionIDCaptured`. The
   `agent_end` observer **extends that same interceptor** — it does *not* add a new shared-loop
   branch, but neither is it "outside the shared loop": it rides the hook codex already established
   for `SessionIDCaptured`. On `agent_end` it invokes `Teardown(sess)`→`Kill`, converting unreliable
   process-exit into reliable **event-driven** completion; the 90m ceiling is a backstop only.
3. **Load-bearing: Pi MUST inherit codex's forced-exec-substrate posture.** For `SessionIDCaptured`
   harnesses the launch code *forces* `implSpec.Substrate = nil` (`reviewloop.go:367–383`) because
   the tmux substrate returns `Stdout()==nil`, so the StdoutWrapper is never called. If Pi runs on
   the tmux substrate, **both** session-id capture (§3.3) **and** the `agent_end` watcher silently
   no-op. PI-012a makes the forced-exec posture a normative requirement.
4. Confirm-by-test that `agent_end` always precedes the hang (findings.md §3).

> Rejected alternative: `CompletionEventStreamThenQuit`. Pi has no interactive `/quit` REPL in
> `--mode json`; choosing it would wrongly enable paste-inject/quit-on-commit against a pane Pi
> doesn't present. ProcessExit + the `agent_end` watcher is the correct pairing.

### 3.5 Commit fallback (`picommit.go`) — routed through the run's runner

Reuse codex's `ensureCodexRefsTrailer` pattern verbatim as `ensurePiRefsTrailer`
(`codexcommit.go:204–255`). Pi *should* self-commit (unsandboxed), but a weak free model may not, or
may omit the trailer. The fallback (gated at the existing `Completion()==ProcessExit` seam in
`workloop.go` ~4230) decides:

```
HEAD has Refs: trailer                 → no-op
HEAD advanced, no trailer              → amend HEAD to add trailer
HEAD == parentSHA, worktree dirty      → stage all + commit with trailer
HEAD == parentSHA, worktree clean      → no commit fabricated (run yields no_commit)
```

**Load-bearing: every git op routes through the run's `runner`** (`runner.Command(ctx,"git","-C",
wtPath,…)` when non-nil, local `exec` when nil) so the **remote SSH substrate** works
(`codexcommit.go:129–149/161–175/266–291/302–338`). This is the bit that bit codex; Pi inherits the
fix for free by copying the routed form. This also realizes the old bench's **two-phase-done** idea
(CL-051): completion requires both a `run_completed` signal *and* a `Refs:`-trailered commit — the
fallback guarantees the second.

### 3.6 Billing/auth guard (`pibillingguard.go`) — fail-CLOSED, **inverted** from codex (Open Q4)

Codex's guard *forces* the ChatGPT plan and refuses if an API key is present (don't bill the
API pool). Pi is a **universal gateway**, so the guard's job **inverts** — it refuses if the
configured provider's key is **absent**, and refuses if provider/model is unset:

1. Resolve `{provider, model, api_key_env[, fallback]}` from config via a fail-loud `ResolvePiConfig`
   (§7) — aggregates **all** missing keys, refuses to start, names the dotted yaml paths, points at
   `harmonik pi config --example`. **No baked default provider/model/key anywhere.**
2. Pre-flight assert (at the `pilaunchspec` build site, mirroring `codexlaunchspec.go:179–183`): the
   env var named by `api_key_env` is present & non-empty in the launch env. Absent → **return a typed
   error, refuse to launch** (fail closed) *before* `agent_ready`.
3. Strip every *other* provider key via the **allowlist** strip (§3.2) so a mis-set env can't silently
   bill elsewhere.
4. **On-disk credential check (review B2).** Codex's guard has a second leg — it reads `auth.json`
   from disk because a persisted credential bills independently of the env. Whether Pi persists a
   login/key to disk (a `~/.pi`-style store) is UNCONFIRMED (findings.md §4). The guard MUST therefore
   either (a) establish-and-cite that Pi holds no persisted on-disk credential surviving the env
   strip, or (b) add a disk-state assertion mirroring codex's `authIndicatesAPIKeyLogin`. Until (a)
   is confirmed, the spec assumes (b) is required (PI-042).
5. Emit guard events (`PiBillingGuardAllowed`/`Denied`) that name the env-var **name, never its
   value** (codex's payload carries no key value); error `Reason` strings likewise.
6. **`skipBillingGuard` test-escape MUST be false in production** (codex carries the same MUST), with
   a wiring test asserting it — otherwise a `harness:pi` bead could reach launch with no key.
7. **A Pi config/guard failure → `run_failed` + bead reopen, NEVER a silent claude fallback.** The
   tier-4 resolver only falls back to claude when *no* tier resolves; a `harness:pi` label resolves
   hard at tier-1, so a Pi config failure fails the run — it does not re-route to claude
   (`harnessresolve.go:71–117`). Stated as a contractual invariant (PI-043).

The "free-OpenRouter trains on data — fine for this open-source repo" point is a **per-provider
informational note** surfaced by `pi config --example`, **not** a guard blocker.

### 3.7 Selection fence (Open Q5, fence half)

- **Tier-1:** `harness:pi` bead label selects Pi (works the moment the harness registers).
- **Tier-2/3:** queue-default / DOT-node-default land when hk-4x3rg / hk-u67of land; until then,
  per-bead label or tier-4 `daemon.default_harness: pi`.
- **The mechanical-only fence is operator discipline + the existing DOT test+review gate, not code.**
  The harness cannot judge "is this bead mechanical." The design's rule: **route only mechanical,
  deterministically-checkable beads (grep=0 / failing-test→green) to a dedicated `pi` queue/label**,
  and let the DOT test+review gate reject bad output. §8 makes this a hard boundary.

### 3.8 Phase-0 unit-test plan

Mirror codex's tests, plus the Pi-specific ones: (a) `pilaunchspec` argv for initial vs resume;
(b) `buildPiEnv` strips all non-selected provider keys + injects only the selected one; (c)
`DetectReady` HC-041 (false on `launch_initiated`); (d) session-id capture from the `session`
header; (e) **`agent_end` watcher fires Teardown even when the process never exits** (simulate the
#4303 hang); (f) `ensurePiRefsTrailer` decision table incl. the **runner-routed remote** path; (g)
`pibillingguard` refuses launch when `api_key_env` is unset/empty (fail-closed); (h) `ResolvePiConfig`
aggregates all missing keys and refuses with the `--example` pointer.

---

## 4. Phase 1 — one mechanical crew on Pi: **DESIGN SPIKE REQUIRED**

> **Status change after review (crew-shim critic, REQUEST_CHANGES).** Phase 1 is **not** a build task
> of known shape. Its foundational premise is unverified, three subsystems it leans on do not work on
> a non-claude pane, and what the brief calls a "shim" is in fact a **new ~Claude-Code-equivalent
> resident interactive harness**. PI-080/081 are therefore **goals, not normative MUST-clauses**, and
> Phase 1 MUST run its own design spike (resolving the unknowns below) before any Phase-1 spec is
> build-ready. The phase-gate already prevents premature start; **Phase 0 ships value alone.**

### 4.1 What's actually easy vs hard (framing corrected)

The `--remote-control` *protocol* is the **easy** part, not the risk: the daemon passes it as a
cosmetic picker label only (`crewlaunchspec.go:108–114`), mints the session-id itself before launch
(`crewstart.go:218/230`), seeds via **pure tmux bracketed-paste** (`crewstart.go:437–474`), and never
reads any Claude session store or validates liveness post-spawn (`HandleCrewStart` returns at
`crewstart.go:354` with no readiness check). So a non-claude PTY process *can* mechanically stand in.

The **hard, under-specified** part is the agent *inside* the pane: a resident PTY process that blocks
on pane input, translates pasted-text + Enter into Pi turns, maintains a multi-turn conversation
thread, injects standing context per turn, streams output back to the pane, **and** runs the full
crew operating loop (claim epic, mirror `--assignee` on every adopt, dispatch to the named queue,
monitor runs, dual-channel status on cadence, triage `run_failed`, re-submit-once→escalate,
re-hydrate on restart). That is a substantial new program. The crew **data plane** is model-agnostic
(every tool is a shell CLI parsed with `jq`; no Anthropic function-calling/MCP; ~12–15K standing
context) — but the **orchestration judgment** is the risk, and bead-mechanicalness does not reduce it.

### 4.2 Named unknowns the spike MUST resolve (before PI-080/081 become normative)

1. **`pi --mode rpc` resident-server semantics — UNVERIFIED (load-bearing).** Only `--mode json`
   (one-shot, the Phase-0 path) is confirmed; that the rpc mode is a *persistent multi-turn
   request/response server* is asserted, not proven (findings.md §"Spec-relevant summary"). If it is
   not resident, the shim must re-invoke `pi --mode json --session <id>` per turn and rebuild
   conversation state each time — a materially larger design. **Resolve first.**
2. **The keystroke→Pi→pane translator.** Who turns pane keystrokes + bracketed paste into Pi rpc
   messages and streams Pi output back to the PTY? This is the new interactive harness; spec it.
3. **Context-fill trigger WITHOUT a Claude gauge.** The keeper's gauging is 100% Claude-Code-hook
   dependent — `keeper-statusline.sh` reads Claude's `.context_window.used_percentage`, the restart
   action pastes literal `/clear` + `/session-resume` (`internal/keeper/cycle.go:1027/1056`). On a Pi
   pane **none of these fire**: the gauge never updates (keeper blind), and `/clear`/`/session-resume`
   arrive as junk unless the shim interprets them. So "rely on the sibling keeper window; the shim
   does nothing special" (original draft) is **false**. The shim needs its **own** token-tracking +
   self-restart trigger, and must interpret-or-emulate the `/clear`+`/session-resume` cycle. Worse,
   the crew-start keeper probe (`crewstart.go:608 probeKeeperLiveness`) only checks the watcher's
   flock — it reports "live" while the watcher is blind, **masking** the failure.
4. **Post-spawn shim-liveness probe.** Crew-start returns success with no readiness check; if the
   shim exits its REPL the crew looks alive in the registry but never runs. The spike must add a
   shim-specific liveness probe (NOT the blind flock probe).
5. **`HandlerBinary` is not yet wired from config** (`daemon.go:116–123`) — so Phase 1 silently
   depends on a Phase-2 capability (per-crew binary selection). Resolve by either a **narrow
   global-binary override** for the Phase-1 pilot, or move that wiring earlier. The phase table's
   "Phase 1 before Phase 2" is inconsistent until this is decided.

### 4.3 Pilot target & its limits

- **Pilot: gurney** (cmd/harmonik tools lane, 2 deterministic beads) — strongest candidate per the
  brief. **But honest scoping:** a crew is a **judgment role** regardless of how mechanical its beads
  are. gurney's 2 deterministic beads make the *workload* easy but leave the *orchestration loop*
  (the 80% risk) un-de-risked. The pilot proves at most "a weak model can run a crew with a short
  queue," not "a weak model can run a crew." Treat it as a spike datapoint, not a green light.
- **Candidates** (later): leto, jamis, chani (SSH-triage caveat), the `_TEMPLATE` shape.
  **Must stay Claude:** paul (kerf design), stilgar (architecture), admiral (oversight), irulan
  (root-cause) — and the **captain unconditionally** (judgment/oversight work breaks weak models).

### 4.4 Gate

Strictly gate on Phase 0 proving the harness wiring **and** the chosen Pi model's per-bead
reliability on real mechanical beads — AND on the §4.2 spike resolving the named unknowns. If Phase 0
shows the model can't reliably land a deterministic bead, Phase 1 is moot — do not start it.

---

## 5. Phase 2 — generalize the crew launch path

Today crew launch hard-codes `claude` at `crewlaunchspec.go:100` + `captain.go:208` and bypasses the
resolver. Phase 2: route crew launch through a **provider/harness abstraction** the way the per-bead
path already is —

- Introduce a crew-launch capability on the harness abstraction (an interactive-session launch spec)
  or a parallel `CrewHarness`, resolved by the same tier mechanism.
- Wire `HandlerBinary`/`claudeBinary` from config/CLI so the shim binary is selectable **per-crew /
  per-lane** (not a global swap).
- Then widen Pi crews to other mechanical lanes. **Captain stays Claude unconditionally.**

Gate on Phase 1 proving one crew end-to-end.

---

## 6. Configuration (Open Q5, config half) — fail-loud, no hardcoded defaults

Add a top-level **`harnesses:`** block (sibling to `agents:`/`daemon:`/`keeper:` in
`internal/daemon/projectconfig.go`):

```yaml
schema_version: 1
harnesses:
  pi:
    # Canonical example uses a PAID model (paid-first, §8). A `:free` model is the labelled
    # hand-attended experiment option (e.g. model: openrouter/qwen/qwen3-coder:free) — NOT the default.
    provider: openrouter                       # REQUIRED — no default
    model:    openrouter/qwen/qwen3-coder      # REQUIRED — no default (free string; full Pi range, §6.1)
    api_key_env: OPENROUTER_API_KEY            # REQUIRED — names the env var; the KEY VALUE is never in config
    fallback:                                  # OPTIONAL — paid-fallback target (V1 manual flip; §8 PI-072)
      provider: anthropic
      model:    anthropic/claude-haiku
      api_key_env: ANTHROPIC_API_KEY
```

- **`ResolvePiConfig`** mirrors `ResolveKeeperConfig` (`cmd/harmonik/resolve_keeper_config.go`):
  aggregate **every** missing required key into one `*PiConfigMissingError`, **refuse to start**,
  name the dotted yaml paths, point at `harmonik pi config --example`. Cite the R1 de-hardcode
  mandate verbatim. **The product imposes ZERO baked Pi default** (provider, model, or key).
- **Selection granularity:** global block above is the lane/queue default; a bead overrides the
  *harness* via `harness:pi` (tier-1). Per-bead *model* override rides the handler-contract `model`
  field, which is **shape-validated, not value-validated** (HC-055a, regex `^[A-Za-z0-9._:/-]+$`,
  ≤128 chars).

### 6.1 Expose Pi's FULL provider/model range — no curated subset

HC-055a's value-opacity invariant is the enabling rule: harmonik validates the *shape* of `model`,
never its *value*; "a closed enum would prevent forward-compatibility." So Pi's `--model provider/id`
passes through untouched and **every** Pi-supported provider/model is selectable by config. The
authoritative compatibility check is **handler-side launch failure** (non-zero exit / typed error
before `agent_ready`), not a harmonik allowlist. Free OpenRouter (e.g. `…:free`) is one selectable
option, never the default.

---

## 7. Credentials principle (operator directive, restated as a rule)

Provider + model + credentials are **operator config**; the harness **fails loud** when unset. The
key **value** is never stored in config — config names the **env var**, the operator supplies the
value in the environment, and `pibillingguard` (§3.6) asserts presence fail-closed at launch. This
is consistent with the no-hardcoded-thresholds mandate and the R1 de-hardcode project.

---

## 8. Rate limits & fallback — **paid-first** (rewritten after the BLOCK on this axis)

The original draft answered a per-minute *request-rate* problem with a *concurrency* cap. That is
numerically unsound and was correctly blocked: **harmonik has no per-request-rate throttle** — a
named queue's `Workers` cap bounds **concurrent in-flight runs only** (`internal/queue/types.go`,
enforced in `workloop.go`), and **nothing limits requests *inside* a single run**. A mechanical bead
of 30–60 tool-turns (each tool-turn = one model request) issues **20–60 req/min by itself**, so
worker-cap-1 cannot keep even one bead under OpenRouter's 20/min free cap, and gurney's 2-bead pilot
(~60–120 requests) **exceeds the 50/day free tier outright**. The fix is to stop hinging the design
on free-tier viability:

> **Production substrate = a PAID provider/model** (operator config), whose rate limits are high
> enough that the existing concurrency controls suffice. This *is* the brief's actual goal — "free
> OpenRouter is one option among many; the broader win is provider-agnosticism + trading tokens for
> time on capacity **off the Anthropic budget**." **Free OpenRouter is an explicitly-labelled,
> hand-attended *experiment* lane — never the unattended-fleet path.**

| Risk | Plan |
|---|---|
| **Per-minute rate** (20/min free) | **Paid lane:** rely on the paid provider's far-higher limit + the dedicated-queue `Workers` cap. **Free experiment lane:** worker-cap-1 and accept a single bead may still breach 20/min — free is for hand-attended trials, not throughput. **No claim** that a worker cap bounds request rate. A real in-run token-bucket throttle is a possible follow-on, scoped OUT of V1 and named as such. |
| **Daily cap** (50/day, 1000/day after one-time \$10) | **Explicit budget accounting:** the free experiment lane requires the **1000-day tier as the floor** for even a 2-bead pilot; the 50/day tier can be exhausted by a *single* bead. Documented, not glossed. The paid lane has no such cap. |
| **Dedicated queue cap is fail-loud** | Pi MUST run on a dedicated named queue with an **explicit `Workers` cap**; unset, it silently inherits global `max_concurrent` (`DefaultWorkers` in `queue/rpc.go` returns the global cap on `<=0`) → 4× the request rate. PI-070 makes an explicit cap **required, fail-loud if absent**. |
| **Global tuner isolation (load-bearing)** | A free-tier Pi 429 MUST NOT throttle the **paid Claude fleet**. The existing `bandwidthtuner.NotifyRateLimit()` snaps **global** `max_concurrent` to 1 on a rate-limit event. Pi's `DetectRateLimit` signal MUST be **isolated** from the global tuner (per-queue backoff only). PI-073. |
| **No SLA** (429 / 404 at peak) | `adapter_pi.DetectRateLimit` reads the 429/404 signal from Pi's NDJSON (`auto_retry_*`/error events — **UNCONFIRMED channel, confirm before implementing**, findings.md §7). **Classify the 429 by retry-after magnitude:** a *minute-window* 429 → short backoff+retry, delay coupled to retry-after (an immediate re-submit re-lands in the same window); a *day-window* 429 (retry-after = hours) → **fail the run fast, do NOT idle to the 90m ceiling** → escalate. 404/"no endpoints" → transient → re-submit once → escalate. |
| **Fallback is honest about V1** | The `fallback:` config block (§6) exists, but **V1 has no automatic fallback** — on free-cap exhaustion the operator flips the lane to the paid provider. So the free lane **strands work until a human acts** and is **hand-attended only**; the **paid lane is the unattended path** (proper limits, no stranding). Auto-fallback on free-cap exhaustion is a named follow-on, OUT of V1. PI-072 states this plainly rather than overselling "mandatory paid fallback." |
| **Caching moot on free tier** | The plan is **not** justified on caching savings. Stated so no reviewer assumes it. |
| **Trade-tokens-for-time only for mechanical beads** | **Hard scope boundary.** Pi is fenced (§3.7) to mechanical, deterministically-checkable beads behind the DOT test+review gate; the fence is operator discipline (the harness can't judge "mechanical"), so a mislabeled ambiguous bead can thrash the review loop (150–300+ requests). The paid lane absorbs this; on the free lane it is an expected hand-attended failure mode. Judgment lanes & the captain stay Claude. |
| **Stranded `in_progress`** | Daemon auto-resets stranded beads (hk-l2xd1). Bound with **re-submit-once → escalate**; a bead failing twice is not re-dispatched without captain escalation (`--topic error`). |

---

## 9. Review gate — reviewers WITH critics (MANDATORY before any build)

Per the brief and the mission, this design is **not build-ready** until it passes ≥2 independent
reviewers AND ≥2 adversarial critics, on these distinct angles:

1. **Codex-fidelity / harness-gate correctness** — are the 8 methods, the `Completion()` seam, the
   commit-fallback runner-routing, and the registry/adapter wiring faithful to the codex template
   and the two declared seam points?
2. **Config fail-loud + security/billing** — does `pibillingguard` truly fail closed; is the
   no-hardcoded-default principle actually enforced; can a mis-set env bill the wrong provider?
3. **Rate-limit + fallback realism** — is the modest-concurrency + paid-fallback plan realistic
   against 20/min + no-SLA; is the re-submit-once→escalate bound right?
4. **Crew-shim feasibility (Phase 1)** — is emulating the `claude --remote-control` interactive
   REPL around `pi --mode rpc` actually tractable, or is Phase 1 underestimated?

**Critics may overrule a too-rosy reviewer synthesis.** Verdicts go to the captain; the design is not
declared "ready for build" until blocking objections are resolved. Only then does the captain staff
an **implementer** crew for Phase 0. **kynes does not build the harness.**

### 9.1 Gate record (round 1, 2026-06-25)

Ran 2 reviewers + 2 adversarial critics against the live code. Outcome:

| Angle | Verdict | Disposition |
|---|---|---|
| Codex-fidelity / harness-gate | REQUEST_CHANGES | **Resolved** — agent_end watcher reworded to ride the SessionIDCaptured `StdoutWrapper` hook; **forced-exec substrate made load-bearing** (§3.4, PI-012a); seam-count wording + stale `:643` citation fixed (§2). |
| Config fail-loud + billing/security | REQUEST_CHANGES | **Resolved** — **allowlist** strip not denylist (B1, §3.2/PI-021); **on-disk credential check** added (B2, §3.6/PI-042); shared key-resolver, no-`--api-key`, production-guard MUST, run_failed-not-claude invariant (PI-043). |
| Rate-limit + fallback realism | **BLOCK** | **Resolved** — §8 rewritten **paid-first**: request-rate ≠ concurrency stated; free = hand-attended experiment lane; **global-tuner isolation** (PI-073); **fail-loud queue cap** (PI-070); retry-after 429 classification + day-window fast-fail; honest V1-no-auto-fallback (PI-072). |
| Crew-shim feasibility (Phase 1) | REQUEST_CHANGES | **Resolved** — Phase 1 reclassified **DESIGN SPIKE REQUIRED**; PI-080/081 demoted to goals; remote-control framing corrected; keeper-blind-on-non-claude-pane + named unknowns enumerated (§4.2, PI-085). |

### 9.2 Gate record (round 2 — confirmation, 2026-06-25)

Re-ran the two critic axes against the revised docs + live code:

| Axis | Round-2 verdict | Notes |
|---|---|---|
| Rate-limit + fallback (was BLOCK) | **APPROVE** | BLOCK genuinely resolved by the paid-first reframe (not papered over). Verified live: `bandwidthtuner.go:187` global snap (warrants PI-073), `rpc.go:804` `DefaultWorkers` (warrants PI-070), tier-1 `harness:pi` resolve invariant (PI-043). Two refinements folded in: PI-071 **no-retry-after safe-degradation** (treat all 429s fail-fast if magnitude unavailable); §6 config example moved off a `:free` model. |
| Crew-shim feasibility | **APPROVE** | All 5 objections genuinely resolved; Phase 1 honestly a spike; dangling citation fixed; keeper-blindness named as an unknown; gurney caveat in. |

**Gate outcome: PASSED.** **Phase 0 (per-bead harness) is BUILD-READY** — the captain may staff an
implementer crew for Phase 0. **Phase 1 is NOT build-ready** — it requires its own design spike
(§4.2 / PI-085) before any Phase-1 spec or build. Phase 2 follows Phase 1. **kynes does not build.**

---

## 10. Open-questions ledger (all 5 answered)

1. **Completion mode** → `CompletionProcessExit` + an `agent_end` NDJSON watcher (riding the
   SessionIDCaptured `StdoutWrapper` hook, on the **forced-exec substrate** — load-bearing) that kills
   the process (Pi's self-exit is unreliable, #4303/#161/#4942). §3.4 / PI-012a/PI-014.
2. **Pi NDJSON session-id + done signal + exit codes** → session id = `id` in the first
   `{"type":"session",…}` line (→ `SessionIDCaptured`); done = **`agent_end`** event; **exit codes
   undocumented & unreliable — do not key on them**, key on `agent_end`. §3.3–3.4.
3. **Shim contract (Phase 1)** → the `--remote-control` protocol is easy (cosmetic label, tmux
   bracketed-paste, no liveness check); the **resident interactive harness** inside the pane is the
   hard, unverified part. Answer is now scoped as a **design spike** (§4.2 / PI-085), NOT a settled
   contract — `pi --mode rpc` resident semantics, the keystroke→Pi→pane translator, the
   keeper-blind-on-non-claude-pane context trigger, shim-liveness, and `HandlerBinary` wiring are all
   open. §4.
4. **Billing/auth guard** → `pibillingguard`, **inverted** from codex: fail closed when the selected
   provider's `api_key_env` is absent or provider/model unset; **allowlist**-strip all other provider
   keys; on-disk credential check until Pi's persistence is confirmed. §3.6.
5. **Config surface** → top-level `harnesses.pi` block (provider/model/api_key_env[/fallback]),
   `ResolvePiConfig` fail-loud aggregating all missing keys, full Pi range exposed via HC-055a
   value-opacity, per-bead `harness:pi` + tier defaults. §6–7.
