# A3 — Incus Container Remote Mode & Scheduling Assessment (context absorption)

> Faithful capture of two prior operator planning docs, as input to the platform-architecture / distributed-execution effort. No new solutioning — this records what those docs decided, framed, and left open. All `file:line` refs are to the two source docs:
> - **I** = `plans/2026-07-19-incus-container-remote-mode/_plan.md`
> - **S** = `plans/2026-07-09-scheduling-assessment/ASSESSMENT.md`

## Bottom line

The container substrate is a concrete two-hop chain — macOS host `gb-mbp` → a lima VM `fleet` (Ubuntu) → **incus** containers launched inside `fleet` — proposed as a NEW remote transport rather than a stretch of the existing single-static-SSH-worker registry (I:9-13, I:123-124). The high-value, low-risk first step is already largely built: a Codex crew routed to a remote worker runs `codex app-server` on the worker and streams its JSON-RPC events back IN-BAND over the ssh stdout pipe — no reverse tunnel, so the container story for Codex is mostly addressing + provisioning, provable with zero new transport code (I:340-379, I:458-466). The scheduling assessment finds harmonik has the dispatch *hooks* but almost none of the *signals* a real scheduler needs, and the operator has firmly ordered the work data-first: build+log signals, build an analysis tool, and only then a **declarative, hot-reloaded** policy plugin (S:9-23, S:190-215). Both docs collide on the same load-bearing constraint — the worker registry is hard-capped at ONE worker (`ErrTooManyWorkers`) — which any multi-container fleet or multi-machine scheduler must lift (I:32-35, S:54-55, S:157).

---

## Incus / container remote mode (source: I)

### Setup available — the concrete substrate we have NOW
- **Topology (firm, operator-described):** macOS `gb-mbp` runs the harmonik daemon → a **lima VM named `fleet`** (Ubuntu) → **incus** containers launched inside `fleet` as ephemeral workers (I:9-13).
- **The exact hand-launch command that works today:** `limactl shell fleet -- sudo incus launch agent-golden agent-03 --profile default --profile agent` (I:13, restated I:459-460). A golden image `agent-golden` already exists and carries pi/claude/codex (I:221).
- **Current remote substrate is single-static-SSH-worker, not containers** — this is what exists in code and what the container model diverges from (I:14-16, I:26).

### What already works in the current remote codebase (firm, grounded in code)
- **Worker registry:** `.harmonik/workers.yaml` loaded by `internal/workers/workers.go`; missing file ⇒ local-only (I:37-39). The live worker is `gb-mbp`, `enabled:false` since the 2026-07-12 incident (I:47-48).
- **Version-1 invariant: at most ONE worker** (`currentVersion=1`; `ErrTooManyWorkers` when >1) — the single most important constraint the container pool collides with (I:32-35). Registry is **boot-only**: live enable/disable mutates in-memory but slot count/host set is fixed at daemon boot (I:44-46).
- **Fail-closed isolation boundary (hk-5h759, CLOSED):** `HARMONIK_SUBSTRATE=codexdriver` runs `danger-full-access` + `approval_policy=never`, safe ONLY inside a real isolation boundary; an enabled ssh worker IS the boundary (I:52-56). `codexWorkerRoutingRunner.requireBoundary` makes the spawn seam **fail closed** at spawn time (race-free, closes admission→spawn TOCTOU) via a deliberately non-existent argv0 — no enabled worker ⇒ refuse, never local unsandboxed fallback (I:57-64). Empirically proven by hk-g0ror.4 (I:63-64).
- **Remote-cwd fix (hk-czb11 / hk-fufel, CLOSED, landed main 2026-07-19 PR#32/#33):** `SSHRunner` tunnels `ssh host -- <cmd>`; a remote worktree path set as the LOCAL `exec.Cmd.Dir` ENOENTs — fixed by `CommandInDir` emitting `ssh host -- cd <dir> && exec <cmd>` (I:68-77).
- **Event tunnel (hk-ege6, CLOSED):** per-run long-lived `ssh -N -R` reverse tunnel; original UNIX-socket `-R` bind was root-owned mode-0600 under macOS root sshd → hook `connect: permission denied` → `agent_ready_timeout@90s` → `run_failed`. **Fix = TCP loopback** `ssh -R 127.0.0.1:<port>:<daemonSock>` (no filesystem perm bits); hookrelay keys off `tcp://` prefix; readiness gate hardened from `test -S` to a real connect probe `waitWorkerSocketLive` (I:88-101). Concurrency-hardened with per-run ephemeral ports + dedicated non-multiplexed ssh connection (hk-cnp17) (I:102-105).

### Proposed design (this is a PLANNING SCAFFOLD, not a spec — I:4)
- **New mode, not registry stretch (recommendation):** add `transport:"incus"` on the existing `Worker.Transport` field, reuse `SSHRunner`/`RemoteCwdRunner` + the fail-closed guard unchanged; new = transport + lifecycle/pool layer + two-hop addressing/relay adapter. Likely needs a **workers.yaml version 2** to lift the one-worker cap and add a pool/profile block (I:122-136).
- **Provisioning lifecycle:** three options — per-run ephemeral (launch→run→destroy), warm pool, hybrid reset-or-recycle. **Recommendation: start per-run ephemeral**, add warm pool only if launch+materialize latency proves to be the bottleneck; owned by a new pool/provision manager inside the daemon (not the fast work loop), fronted by a `harmonik worker provision` / `harmonik fleet …` CLI (I:138-158). No `harmonik worker provision` exists today — provisioning is a manual runbook `docs/remote-substrate/WORKER-SETUP-macos.md` (I:111-116).
- **Addressing across two hops:** (A) nested `limactl shell fleet -- sudo incus exec agent-03 -- <cmd>` (needs a new `IncusRunner`, tty/pipe semantics unverified, no stable host:port to reverse-tunnel to) vs (B) direct SSH to the container bridge IP. **Recommendation: (B) with ProxyJump through `fleet`** — `ssh -J <fleet> <container-ip>` via `SSHRunner.Opts`, reuses transport + `CommandInDir` + reverse-tunnel with least new code (I:160-184). Later sharpened to **DECIDE ProxyJump-SSH** — for Claude it's essentially forced (the reverse tunnel needs a stable host:port that nested `incus exec` can't give); for codex it's not required but still reuses the seams (I:435-440).
- **Golden image (hk-u6qu6):** pi+claude+codex + the **harmonik worker binary at a known path** (so the hook relay fires) + pre-warmed Go build cache + pre-cloned repo; must stage pi runtime files (`models.json`, `.harmonik/pi-agent/`, `PI_CODING_AGENT_DIR`) into the container — DD1 code-sync may exclude `.harmonik/` (I:220-235). Version the image and pin the harmonik binary SHA, but do NOT hard-pin external tool versions (degrade gracefully) (I:233-235).

### Constraints / findings (firm risks called out)
- **hk-go6nq (OPEN P2, regression-suspect):** a review-node `agent_ready_timeout` with the SAME symptom as hk-ege6; operator wants it kept moving because remote load is coming; gates the addressing+relay phase (I:106-109, I:213-215, I:320).
- **The Linux `.git` / socket-perm question the caller asked about:** the doc frames the hk-ege6 class as the analogue risk in containers — the #1 empirical check is whether the `-R` TCP-loopback listener the container's sshd binds is **connectable by the unprivileged agent user**. On macOS-sshd-as-root it was root-owned 0600; **Linux sshd normally runs the session as the target user, so it is *likely* fine — but MUST be verified** (I:200-206, I:412-419). Keep the TCP-loopback invariant — never a UNIX socket for `-R` across the hops (I:207-210).
- **Worktree-create bug (hk-2hfyt, chronic, substrate-independent):** the daemon's ssh-runner-wrapped `git worktree add` no-ops (HEAD doesn't resolve) on the worker even when a manual `git worktree add` there succeeds — recurred 2026-07-07/07-11/07-12; a **daemon-side runner-wrapper defect, not the tunnel**; hits codex AND claude remote runs and **will replay in containers**. Flagged as possibly the bigger relay risk; must be root-caused before any container E2E is trusted (I:79-83, I:425-431, I:466). *(This, not `.git` sandbox/bubblewrap, is the `.git`-related regression the doc actually names; there is no mention of a bubblewrap sandbox in these two docs.)*
- **Auth / overnight OAuth wall (hk-bkd6h, OPEN P2):** a fresh Claude session does NOT inherit operator OAuth — it hits an interactive `claude.com/oauth/authorize` wall and never joins comms; **fatal for ephemeral Claude containers** (every launch is fresh) (I:252-258). Resolution: **DECIDE codex-only for unattended container crews** — codex token/subscription auth is headless; keep Claude oversight on a persisted-login host; this scopes hk-bkd6h out rather than solving headless Claude OAuth (I:259-267, I:448-451).
- **Fail-closed extension (new requirement):** a launched-but-UNREACHABLE container must ALSO fail closed — the spawn-time check must verify reachability of the *specific* container, not merely that a transport is configured; `waitWorkerSocketLive` connect-probe is the model (I:237-250).

### The Codex sidecar — ALREADY runs remotely (the key de-risking finding)
- The "sidecar" is the **`codex app-server` child process**; the **driver stays on box A** (`codexdriver`/`codexwire`/`codexinput`/`resident.go` all run in the daemon on gb-mbp) (I:340-350).
- When a codex crew routes to an enabled ssh worker, only `codex app-server` + worktree + credentials run on the worker; its JSON-RPC NDJSON wire flows back **over the ssh stdout pipe**; **readiness is in-band** (`initialize`→`thread/start` handshake off stdout) — **no `agent_ready` hook, no hook relay, no reverse tunnel on the codex path** (I:351-369). For a container the picture is identical (I:358-360).
- **Therefore the reverse-tunnel / hk-ege6 class does NOT gate codex** — but the workloop still *builds* the tunnel + runs `waitWorkerSocketLive` for every remote run, so a codex run can be failed by tunnel setup it never uses. Flagged simplification (worth a bead): gate reverse-tunnel construction on the substrate actually needing hooks (I:186-193, I:370-379).

---

## Scheduling (source: S)

### Problem framed
As harmonik grows to dispatch across local + remote machines with a mix of models (Claude / Codex / Pi-DGX / Pi-OpenRouter), each with different capacity and cost, it enters a **classic constrained-scheduling regime**. Question: what internals would a real scheduler require, and what do we have today? (S:4-8).

### Current state (firm — grounded in dispatch code)
- Dispatch is a **flat counter-based admission loop**: one function `runWorkLoop` (`workloop.go` ~1459), three counter-based steps per tick — pick queue round-robin (`selectNextQueue`, `workloop.go:1357`), admit-or-sleep on a split capacity gate (`workloop.go:1689-1711`), place local-vs-remote via `SelectWorker` (`workloop.go:2887-2906`) — where remote is a **fallback, not a choice**, with no scoring of which box is best (S:24-39).
- **Concurrency = two independent integer gates**: fleet `max_concurrent` (local, runtime-mutable, no restart) + per-queue `Workers` cap; remote bounded separately by `max_slots`; remote runs do NOT count against `max_concurrent`, so ceiling = local `max_concurrent` + remote `max_slots` (today 4+6=10) (S:41-45).
- **Harness & model resolved AFTER placement and independently** — two static 4-tier precedence walks (`resolveHarness` `harnessresolve.go:53`; `ResolveModelPreference` `modelpreference.go:190`); the gate does not know which model a bead will use (S:47-52).
- **Structural ceiling: registry hard-capped at ONE worker** (`workers.go:282`, `ErrTooManyWorkers`); no N-machine fleet abstraction (S:54-55). Hardcoded knobs a scheduler would want: cold-start spawn semaphore =3 (`workloop.go:1145`), agent_ready timeout =150s no override (`agentready.go:64`), poll cadence, round-robin policy (S:56-59).

### Dimensions a real scheduler must model (S:60-74, the table)
| # | Dimension | Unit | State today |
|---|---|---|---|
| 1 | Per-box throughput/volume | slots per machine | **Partial** — static `max_slots` + local `max_concurrent`; registry holds ONE worker |
| 2 | Live load pressure per box | load5/ncpu, mem/swap/disk | **Telemetry built, not acted on** — only drives binary enable/disable, never placement |
| 3 | Instances of a model per box | sessions per (box×model) | **Absent** — no (box,model) capacity anywhere |
| 4 | Provider cost model | quota headroom (Claude/Codex) / $/token (Pi/OR) | **Absent as live signal** — one live meter is a Claude-only output-bytes proxy |
| 5 | Bead/test weight | per-task cost estimate | **Absent** — every bead =1 slot; operator's sharpest constraint, zero modeling |
| 6 | Quality bar per task category | quality score vs bar | **Designed-only** — eval harness measures, no routing consumes it |
| 7 | Fleet + per-queue concurrency gate | run-registry counters | **Built** — the two-level gate |

The placement cell is a **(machine × model-instance) slot**; load on a cell is a **per-bead weight** (unmodeled); global constraints are **provider quota/budget + marginal $/token**. Today the system only counts (machine)-slots (S:71-74).

### Model / proposed shape
- **Objective is already effectively chosen** — MR3's stated policy: "fill Pi + local-DGX first, then spread to Claude + Codex; when Claude tokens run low, auto-cut Claude" = fill-cheapest-substrate-first + respect-Claude-quota. The work is not to invent an objective but to build the **four missing signals** and fold model+machine selection into one placement decision (S:20-23, S:130-141).
- **Prior art BUILT (hooks):** two-level concurrency gate; worker telemetry (WR1-5 + breach PB1-4, but never run against a live worker, thresholds unvalidated, observability-only); retrospective cost accounting (`harmonik usage`, batch, Claude-transcript-only); live budget-cutoff hook (`spendmeter_hkk3f8g.go`, but cost basis = output-bytes proxy, Claude-only); dispatch-time harness/model seam (`resolveHarness` + per-DOT-node `model=` — the router's slot already exists); Pi/OpenRouter/ornith(DGX) plumbing (S:78-89).
- **Prior art DESIGNED-ONLY:** AIMD autoscaler (`hk-e6gs`, deliberately deferred — operator chose "keep hardcoded max until real load data"); dispatch-time model routing (static category→tier table, proposals only); MR program (MR3 = `hk-vlvyg`, none shipped); token-audit Phases 2-3 (real `token_usage` events + budget meter on real tokens — unbuilt, "single biggest missing input") (S:91-102).

### Decided vs open
**DECIDED — binding operator design constraints (2026-07-09):**
1. **Objective must be a PLUGIN, hot-reloadable with NO binary rebuild and NO daemon restart, and DECLARATIVE not imperative** — a structured yaml/json policy schema the daemon loads + hot-reloads, plus a small evaluator; MR3's policy is the first policy *file*, not baked-in code. Design the schema first; the "language" of the policy IS the deliverable (open research: what the yaml/json actually looks like) (S:106-120, S:204-212).
2. **SIGNALS BEFORE OBJECTIVE, and signals are DATA-FIRST** — collect telemetry/load + token usage, **log durably**, build an analysis tool to model the systems, THEN design the placement engine on validated data. Do not skip to routing before the data exists (S:121-126).
3. **Token-usage caution** — per-provider token data is sensitive; scope collection/storage/exposure before any live path (S:127-128, S:198-199).

**Revised lane plan (S:190-215):**
- **Lane A — signal collection + logging (START HERE):** route worker-report/breach payloads to a durable store (not in-memory + kill-switch); finish token-audit toward real per-provider `token_usage` events, logged durably (scope carefully). Done = load+token data per run in a queryable log.
- **Lane B — analysis tool:** read Lane-A logs to model per-(box,model) throughput, per-provider cost/usage, per-bead-class weight distributions. Retires "unvalidated thresholds" + "hand-typed slots".
- **Lane C — objective as declarative hot-reloaded plugin (LATER, after A+B):** policy schema (fill-order, per-provider budget caps, cut-thresholds, weights, tie-breaks) + evaluator folding model+machine into one placement step. Do NOT build until A+B give validated data.

**OPEN (explicitly):** the exact policy yaml/json shape (S:120, S:212); the tie-break "when cheap substrate saturated, is lighting up an expensive metered slot worth it?" — depends on per-bead weight, a knob for the operator not a fixed answer (S:139-141). This is an ASSESSMENT, not a committed plan (S:186-188).

---

## What we can build on NOW vs aspirational

### Firm capability / constraint — build on NOW
- **Container substrate physically exists:** lima `fleet` VM + incus + `agent-golden` image; one container launches by hand with a known command (I:13, I:459-460).
- **Codex remote sidecar already works** over ssh stdout in-band with no reverse tunnel — the fastest GO signal; a container is just addressing + provisioning away (I:340-379, I:458-466).
- **Reusable seams landed on main:** `SSHRunner`/`CommandInDir` remote-cwd fix (hk-czb11/hk-fufel, PR#32/#33), TCP-loopback reverse tunnel + `resolveDialTarget` (hk-ege6), fail-closed spawn guard (hk-5h759) (I:328-330).
- **Dispatch hooks that a scheduler plugs into:** two-level concurrency gate, dispatch-time harness/model seam (`resolveHarness`+`model=`), retrospective `harmonik usage`, live budget-cutoff mechanism, worker telemetry payloads (S:78-89) — all real, just under-fed.
- **Hard constraint to respect from day one:** `ErrTooManyWorkers` one-worker registry cap (I:32-35, S:54-55) and boot-only registry (I:44-46).

### Aspirational / not yet built
- **Container mode:** no `transport:"incus"`, no `IncusRunner`, no pool/provision manager, no `harmonik worker provision`/`harmonik fleet` CLI, no workers.yaml v2, no harmonik-specific golden image (only manual runbook + `agent-golden`) (I:111-116, I:122-136, I:220-235).
- **Two-hop reverse tunnel through ProxyJump** is unproven; hk-go6nq (OPEN) and hk-2hfyt (chronic worktree-create) are unresolved gates (I:106-109, I:272-283, I:425-431).
- **Scheduler:** no per-bead weight, no (box×model) capacity, load telemetry disconnected from dispatch (AIMD deferred `hk-e6gs`), no live per-provider spend/quota signal, no router over live capacity, placement+model still separate — all four missing signals absent (S:143-157).
- **Everything in Lanes A/B/C is greenfield;** the declarative hot-reloaded policy schema is genuinely unknown/open (S:120, S:190-215).

---

## Connections to containerized + distributed beads

### (a) Running beads in containers
- **Per-run ephemeral incus container = the natural isolation boundary** for the danger-full-access codex posture, letting a bead's codex run land its commit inside the boundary and destroy the container clean (I:152-158, I:308-311, I:316-318). The fail-closed guard extends to "no boundary OR unreachable container ⇒ refuse, never local fallback" (I:237-250).
- **Fastest path is codex-only, per-run ephemeral containers**, Claude oversight kept on a persisted-login host — this scopes out the OAuth wall (hk-bkd6h) and the whole two-hop reverse-tunnel burden for v1 (I:383-397, I:448-455).
- **Container-specific bead blockers:** golden-image staging of pi runtime files (hk-u6qu6 analogue, I:226-230); the substrate-independent worktree-create defect (hk-2hfyt) that will replay in containers and must be root-caused before any container E2E (I:425-431); auth headless-ness (I:252-267).
- **E2E gate** mirrors hk-g0ror.4 rigor: a codex crew runs sandboxed inside an ephemeral container, commit lands, run reaches `agent_ready`/`Ready` + completes review, container destroyed clean (I:308-311).

### (b) Distributing work across machines
- **The same one-worker registry cap blocks BOTH** a container pool and an N-machine scheduler — lifting `ErrTooManyWorkers` (workers.yaml v2 / a fleet abstraction) is the shared prerequisite (I:32-35, I:277-279, I:441-447; S:54-55, S:157, S:172).
- **A container pool IS a multi-machine fleet in miniature** — N containers each a placement target maps directly onto the scheduler's "(machine × model) slot" cell (S:71-74); scheduling dimension 1 (per-box slots) and dimension 3 ((box×model) capacity) are exactly what a container pool instantiates.
- **Sequencing tension worth noting (captured, not resolved):** the incus doc pushes to **prototype the container transport now** (codex sidecar is low-risk, I:458-466), while the scheduling doc mandates **signals-and-data first, placement engine later** (S:121-126, S:190-215). A container fleet is both a consumer of the future placement engine and a producer of the load/throughput signals Lane A/B want to log. The incus prototype also keeps the registry as "one logical container-transport worker" and defers the cap fight (I:441-447) — which supports only *serialized* single-container dispatch until v2, aligning with "measure single-container throughput first" before committing to concurrent containers / a full scheduler.
- **hk-go6nq is the named canary** for two-hop relay under container load; the operator wants it kept moving precisely because container/remote load is what makes it load-bearing (I:106-109, I:212-215).
