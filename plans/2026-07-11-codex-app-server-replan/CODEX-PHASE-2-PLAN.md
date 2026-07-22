# Codex-as-Crew — Phase-2 plan (for admiral gate)

> **Epic:** hk-g0ror · **codename:** codex-app-server · **Date:** 2026-07-18
> **Status:** review-ready. Written for the admiral to gate the sidecar build (hk-160yb).
> **Premise proof:** SPIKE-B-VERDICT.md (yankee, 🟢 PASS base+stretch).
> **Sources:** SPIKE-B-VERDICT.md · PHASE-1-tap-serializer-reactor.md ·
> `.kerf/works/codex-app-server/04-design/{orchestrator-session-model,keeper-verdict}-design.md` ·
> `07-tasks.md` · `internal/codexdriver/{driver,session}.go`.

---

## 1. Premise & gate status

**The bet:** run a harmonik crew orchestrator on a resident `codex app-server` thread —
conversational state held server-side — so a Codex crew can retire ~70–80% of the per-session
keeper/handoff machinery Claude crews need. Phase-1 built the wire-facing layers
(tap / serializer / reactor / twin + L0–L2 tests) to de-risk the contract before any resident
build. **Spike B was the load-bearing live proof gating Phase-2.**

**Spike B PASSED (base + stretch).** A headless resident `codex app-server` (codex-cli 0.144.5),
driven over JSON-RPC NDJSON in an isolated scratch git repo:

- **Turn 1** → real shell exec → committed `ORCHESTRATED.md` (exact requested message, reviewable
  via `git show`).
- **Turn 2, same thread** → saw turn-1's file, appended, re-committed cleanly. **Multi-turn
  resident context works with no re-seed** — the server-side-context premise the whole design rests
  on is confirmed live, not just from docs.

**What Spike B did NOT prove** (must not be read as settled by this gate):

- **Multi-crew concurrency** — whether one app-server can front many crew threads in true parallel,
  or turns serialize per model/quota (design OQ-3). The "front the whole fleet" throughput claim
  stays provisional; shared-vs-per-crew is unresolved.
- **Long-run token economics** — a resident session outlives its context window; management
  *relocates* to server-side compaction (design OQ-1: auto vs. caller-triggered), it is not
  eliminated. No long-run cost/compaction data yet.
- **Reconnect / thread-resume under real fault** — proven offline against the twin in Phase-1;
  never exercised against a live server crash + `thread/resume` reattach.
- **Backend auth for a resident session** — how app-server authenticates to the model backend and
  whether it inherits `~/.codex/` (design OQ-2); gates the mid-session re-auth story.

**The real gate finding — it's the sandbox, not the wire.** Spike B established that the load-bearing
config is `sandbox_mode=danger-full-access`. Under `workspace-write`, tool-exec and multi-turn both
work but `.git` writes are sandboxed out: the file is written, left untracked, **no commit lands** —
a silent no-op. Only `danger-full-access` (plus `approval_policy=never`) produces reviewable commits.
This reframes Phase-2 security posture (§3) as the central design concern.

---

## 2. Work breakdown

Three live child beads under hk-g0ror. All P2, all `codename:codex-app-server`.

| Bead | What | Build / design | Effort | Risk |
|---|---|---|---|---|
| **hk-5h759** | Set `sandbox_mode=danger-full-access` + `approval_policy=never` in `codexdriver.Options` (spawn args). | **BUILD now** | Small (~1 config seam) | Low mechanically; see §3 for the security weight it carries |
| **hk-160yb** | Persistent supervised app-server **sidecar** — resident multi-turn JSON-RPC client + reconnect / `thread/resume` / backpressure / watchdog. ("hk-nzzos residual" — the design's *real cost*.) | **BUILD — HELD** pending this gate | Large (net-new subsystem) | High — no worker/crew analog; only in-tree JSON-RPC is the daemon's own *server* side |
| **hk-f8wtm** | Route `harmonik crew start` through **per-crew harness selection** (flag → mission front-matter → per-crew config → default) so a crew can be launched on Codex. ("hk-l63b9 residual".) | **DESIGN-only until sequencing clears** (§4) | Medium | Low-medium; substrate-neutral seam, but touches crew-start + keeper branch |

**Sequencing / dependencies:**

```
hk-5h759  (config unblocker)  ──►  concrete prerequisite for any real headless crew commit
hk-f8wtm  (crew-start routing) ──►  design now; build gated on Track A (§4)
hk-160yb  (resident sidecar)   ──►  HELD; build gated on THIS admiral plan-gate
```

- **hk-5h759 is the smallest, highest-leverage step** and the concrete unblocker: without it a
  crew launched "as-wired" today would have its tools declined and/or its commits sandboxed out.
  Today `codexdriver.Options.Args` defaults to just `["app-server"]` (`driver.go:121`) — **no
  sandbox/approval flags**, and `session.go:861` auto-declines any approval request with `-32601`.
- **hk-160yb is the real cost.** The current `codexdriver` is a **worker-turn** substrate (one child
  per turn), **not** a resident orchestrator sidecar. hk-160yb is the net-new resident-client
  subsystem. It is HELD pending admiral greenlight (this plan). A **Spike-B FAIL would have HALTED
  it** (anti-sunk-cost); Spike B passed, so the premise no longer blocks it — the remaining gate is
  the admiral's cost/priority call.
- **hk-f8wtm** can be designed in parallel but its keeper-branch touches the surface fenced by §4.

The design already maps each residual to concrete files/seams (orchestrator-session-model-design.md
§Integration): `Harness` on `CrewStartRequest` + a crew-scoped resolver, a sibling
`crewcodexlaunchspec.go`, JSON-RPC boot-seed replacing `pasteCrewMissionToSession`, `thread_id`
capture into the crew registry, and a **parallel `CrewSubstrate`/`Orchestrator` seam** (the worker
`Harness` interface is worker-turn-shaped and *cannot* express a resident session — do NOT overload
it).

---

## 3. Sandbox / security posture (the load-bearing constraint)

To produce reviewable commits, a Codex crew's app-server **must** run
`sandbox_mode=danger-full-access` + `approval_policy=never`. `danger-full-access` means the child has
**unsandboxed filesystem + network + exec** on the host — no OS-level guardrail between an autonomous
LLM turn and the machine. This is a real blast-radius expansion and the crux of the gate.

**Why it's forced:** `workspace-write` sandboxes `.git` writes, so commits silently vanish (§1).
`approval_policy=never` is required because the driver does not negotiate approvals — it auto-declines
(`session.go:861`), which would otherwise block every exec/apply-patch. So the choice is *negotiate
approvals* (large, deferred) or *run unsandboxed* (Spike B's path).

**Bounding the blast radius — options for the admiral to weigh:**

1. **External sandbox (recommended default).** Run the app-server inside an OS/container boundary
   (the design's stated "external-sandbox assumption") so `danger-full-access` is full access *within
   a jail*, not the host. This keeps the workspace-write trap out of `.git` while containing exec.
2. **Per-crew scratch/worktree isolation.** Each crew's app-server is confined to its own worktree
   (harmonik already isolates crew work in worktrees). Limits filesystem blast radius even absent a
   container, and aligns with the existing crew-worktree invariant.
3. **Credential handling.** A resident session **outlives its auth token** — a new failure mode with
   no launch boundary to re-run the billing-guard on (design §"new failure modes"). Credential-strip
   + `buildCodexEnv` reuse is *provisional* pending OQ-2 (backend auth). Posture decision: confirm
   what secrets are reachable from an unsandboxed turn and strip/scope them at sidecar spawn.

**Recommendation:** couple hk-5h759 (which *turns on* danger-full-access) to an explicit
isolation decision — do not ship the flag without at least per-crew worktree confinement, and prefer
an external sandbox for the resident build. The flag is a one-line change; the *posture* it commits
to is the load-bearing part.

---

## 4. Sequencing against xray Track A (fence — load-bearing)

The Phase-2 keeper-retirement bet (Codex crews skip the keeper window and the
handoff→`/clear`→`/session-resume` cycle) **touches the same keeper surface** as the active fleet
keeper-reliability initiative. xray is driving that redesign fully through kerf as work
**`keeper-restart-delivery`** (Track A — leader-session timing+delivery: deliver the nudge as a comms
message not a terminal paste, hold-if-operator-present, carry the self-restart command).

**Rule (from admiral-initiatives.md): Codex keeper-retirement stays spike/design-only until Track A
lands or the admiral explicitly sequences it.** The two must **NOT edit keeper code in parallel** —
concurrent edits to one surface is exactly the collision this fence prevents.

**Practical effect on Phase-2:**

- hk-5h759 (sandbox config) and hk-160yb (sidecar subsystem) do **not** touch keeper code → **not
  fenced**; they can proceed on their own gate.
- hk-f8wtm's **keeper-branch** (skip the keeper window for a codex crew) and the reshaped
  token-pressure trigger (design OQ-1) **are** keeper-surface work → **design-only until Track A
  lands.** Design the branch now; do not merge keeper edits until the fence lifts.
- **Trigger to re-evaluate:** when `keeper-restart-delivery` reaches READY (kerf tasks finalized),
  the admiral delegates it to build; once it lands, the Codex keeper-branch is unfenced.

---

## 5. Open questions for the admiral to rule on

1. **Greenlight hk-160yb (the sidecar build)?** This is the primary gate. Spike B removed the
   premise risk; the remaining call is cost/priority — it is the largest single piece of Phase-2 and
   a net-new subsystem. Yes / no / defer-behind-what.
2. **Shared app-server vs. per-crew app-server** (design OQ-3/OQ-6). One process fronting many
   crew threads (cheaper, shared blast radius, parallelism *unconfirmed*) vs. one app-server per crew
   (isolation, higher cost). Spike B did not settle parallelism — this may need its own spike before
   the sidecar commits to a topology. Which way do we design hk-160yb?
3. **Sandbox posture (§3)** — is an external sandbox required before enabling `danger-full-access`,
   or is per-crew worktree confinement acceptable for the first build? This gates *how* hk-5h759
   ships, not whether.
4. **Token-budget guardrail.** Long-run economics are unproven (§1). Do we require a resident-session
   token ceiling / compaction policy (OQ-1) as an explicit acceptance criterion on hk-160yb, or ship
   the sidecar first and instrument economics after?
5. **Confirm the Track A fence timing (§4)** — proceed with hk-f8wtm keeper-branch as design-only
   now, build-gated on `keeper-restart-delivery` landing? (Default assumption unless the admiral
   sequences otherwise.)

---

## 6. Recommended next action

**Land hk-5h759 now; design hk-160yb and hk-f8wtm; hold the sidecar *build* for the admiral gate.**

Concretely, in order:

1. **Ship hk-5h759** — set `sandbox_mode=danger-full-access` + `approval_policy=never` in
   `codexdriver.Options`, **coupled to a per-crew worktree confinement decision** (§3). Smallest,
   highest-leverage, unblocks every downstream real-crew commit. Not keeper-fenced.
2. **Design hk-160yb and hk-f8wtm** off the existing design docs (resident `CrewSubstrate` seam,
   crew-start harness resolver) — write, don't build the keeper-branch. Surface the §5 open questions
   with the design so the admiral can rule on topology (Q2) and sandbox posture (Q3) before code.
3. **Await the admiral gate on hk-160yb** (Q1) and the Track A fence lift (§4) before building the
   resident sidecar and the codex keeper-branch respectively.

This keeps motion on the proven, unfenced, low-risk unblocker while the two expensive/fenced pieces
wait on the one operator-only call (greenlight the sidecar) and the one sequencing dependency
(Track A lands).
