# Design Review — codex-app-server (Pass 4 change-design)

> Independent adversarial review · codename:codex-app-server · reviewer: design-review gate
> Scope reviewed: `04-design/orchestrator-session-model-design.md` +
> `04-design/keeper-verdict-design.md`, checked against `01-problem-space.md`,
> `02-components.md`, and the five `03-research/*/findings.md`.
> Read-only review. No files edited.

## Verdict: **APPROVE-WITH-CHANGES**

The design is well-grounded, honestly hedged on the headline keeper bet, and the un-park
path is concrete. No BLOCKING defect: the placement verdict is justified by a cited invariant,
the keeper verdict carries its relocation caveat, and every open question is enumerated with
what would close it. The changes below are mostly precision/consistency fixes plus one tracking
gap — none reopens the design's core reasoning.

---

## Findings

### 1. [SHOULD-FIX] Daemon-proxied presence is asserted as settled; C5 leaves it open, and it can mask a hung orchestrator
`orchestrator-session-model-design.md` §"Presence is daemon-proxied" states the daemon/sidecar
"emits the crew's `agent_presence` beats — strictly more durable than today's fragile client
stream" as a settled property. C5 §4 explicitly flags this as an OPEN QUESTION on two axes:
(a) *where* the proxy loop lives, and (b) *what presence means* — "thread liveness (server says
thread alive) vs. agent responsiveness (last completed turn)". This is not cosmetic: the same
doc's own failure-mode table lists "app-server hang (alive, not answering)" and the idle-unload
behavior (thread evicts from memory at 30 min while the 120s presence TTL keeps beating "online").
A daemon that beats presence off mere process/socket liveness will report a hung or unloaded
thread as healthy. **Fix:** adopt C5's recommendation verbatim — presence beat = "daemon can reach
the thread AND the queue is being serviced" — and carry the "where does the proxy loop live"
question into the open-questions list, rather than presenting durable presence as a free win.

### 2. [SHOULD-FIX] The sidecar-not-in-process verdict leans on an invariant the design's own thesis weakens
`orchestrator-session-model-design.md` §Integration justifies "supervised sidecar, not in-process"
primarily via `crewstart.go:283-296` — "an in-process client would regress [surviving daemon
redeploy] on every routine daemon redeploy." That invariant was forged for *Claude* crews, where
the crew process *is* the reasoning state, so losing it on redeploy loses the orchestrator. But
this design's central thesis (C1 §5; §"Restart continuity COLLAPSES") is that conversational state
lives **server-side** and a restart is a **cheap `thread/resume` reconnect**. By the design's own
logic, an in-process client dying on daemon redeploy costs only a reconnect — not a lost
orchestrator — so the cited invariant is materially *weaker* for a Codex crew than for a Claude
crew. The verdict is still defensible (blast-radius isolation + near-verbatim `DaemonWatchdog`
reuse, both in C4 §1), but it is currently **led** by the argument the thesis undercuts. **Fix:**
lead the placement justification with blast-radius isolation and watchdog reuse; demote the
survive-redeploy argument to a secondary point and acknowledge server-side state reduces its force.

### 3. [SHOULD-FIX] The real-cost subsystem (JSON-RPC client + sidecar) has no bead — the un-park is concrete for the small pieces, untracked for the large one
`orchestrator-session-model-design.md` §Integration table and C4 §5 both correctly identify the
persistent JSON-RPC client + supervised app-server sidecar as "the real cost… no worker- or
crew-path analog," yet it is the one row marked "*(net-new, no bead yet)*". Success criterion 3
(01 §"Success criteria") is that the path be named precisely enough to un-park `hk-l63b9` — that is
met for hk-l63b9/lrf30/8efdl/0ysh3/6z72r, all mapped to concrete files+lines matching C4 §2/§5.
But un-parking the seam while the largest, riskiest work item remains beadless invites exactly the
"seam lands, core subsystem drifts untracked" failure. **Fix:** file a bead for the JSON-RPC-client/
sidecar subsystem (or note explicitly it is deferred to the implementation kerf as a named work
item) before hk-l63b9 un-parks, so the un-park doesn't imply the hard part is done.

### 4. [SHOULD-FIX] Billing-guard reuse is presented as unconditional but is gated on the unresolved backend-auth question
`orchestrator-session-model-design.md` §"Reuse ledger" lists `billing-guard` under "do not
reinvent," and the failure-mode table adds mid-session re-auth. But C4 §3 shows the billing guard
*materializes* `forced_login_method=chatgpt` and fail-closed asserts a ChatGPT plan, and C1 §5 +
OQ-2 leave *how app-server authenticates to the model backend* (ChatGPT login vs API key; does it
inherit `~/.codex/`?) **unconfirmed**. Whether the existing guard even applies to an app-server
child is therefore contingent on OQ-2. **Fix:** annotate the billing-guard reuse as "contingent on
OQ-2 (backend auth model)" rather than a clean carry-over.

### 5. [NIT] Concrete method names (`turn/steer`, `thread/compact/start`, `thread/tokenUsage/updated`) are used as normative in the session model though C1 flags them summarized-from-fetch
The session model drives `turn/steer` ("appends to an in-flight turn for fast follow-ups") and the
keeper trigger off `thread/tokenUsage/updated` as if wire-normative. C1 §"source-confidence"
(and consolidated OQ-4) warns these names were summarized by the fetch model and must be
re-verified against raw `codex-rs/app-server/README.md`/schema before being treated as a normative
contract. The design *does* carry this as open-question 7, so the gap is disclosed — but the body
prose reads as settled. Low risk; a one-line inline "(pending OQ-7 wire re-verification)" at first
use would keep the caveat attached to the claim.

### 6. [NIT] Cross-doc inconsistency on hk-6z72r: "a deletion, not an addition" vs. "primarily a deletion + a conditional additive trigger"
The session-model table states hk-6z72r is "a deletion, not an addition." The keeper-verdict doc is
more precise: "primarily a deletion/branch… the only additive piece is the conditional
token-pressure trigger, and only if OQ-1 resolves to manual compaction." The flat "not an addition"
in the session-model table drops the conditional additive trigger that the companion doc (and C3
RESHAPES) preserves. Align the two to the more precise phrasing.

### 7. [NIT] "Single app-server could front the whole crew fleet" glosses OQ-3 at point of claim
§"Target session model" asserts a single app-server "could front the whole crew fleet" (many
concurrent threads, C1 §6 — confirmed) but fleet throughput actually depends on OQ-3 (do
concurrent threads execute turns in true parallel or serialize per quota?). The design flags OQ-3
separately, so this is disclosed; just avoid implying fleet-scale front-ending is free before the
parallelism question resolves.

---

## What's solid (do NOT churn these)

- **The keeper verdict is honest.** `keeper-verdict-design.md` correctly isolates the ~70–80%
  client-window mass that deletes, ties the RESHAPE band strictly to OQ-1 (auto-vs-manual
  compaction), and — critically — carries the "window exhaustion **relocates**, not eliminated"
  caveat in a dedicated load-bearing section. It does **not** overstate "retire keeper" as a
  magic-wand elimination. This is exactly the honesty the prompt asked to stress-test, and it holds.
- **Server-side context is not over-claimed.** The design marks server-side history/token-accounting
  as CONFIRMED (matches C1 §4 verdict) while quarantining *only* the auto-vs-manual compaction
  trigger as OQ-1. It does not treat the open trigger question as settled. Correct.
- **The `/clear`-cycle-is-gone claim is genuinely settled, not an over-reach.** It holds regardless
  of OQ-1, because C1 confirms there is no client-side growing buffer either way — so retiring the
  handoff→`/clear`→resume cycle does not rest on an open question.
- **Non-goals respected.** No implementation (design + flagged impl-kerf choices only); orchestrator
  behavioral contract treated as fixed with only the substrate changing; comms/queue seam used as
  the invariant integration point, not altered; Option A/B not resurrected.
- **Placement verdict is justified, not merely asserted** (subject to finding #2's re-ordering): it
  cites the `crewstart.go:283-296` invariant and the `DaemonWatchdog` reuse from C4 §1 rather than
  hand-waving.
- **The hk-l63b9 un-park path is concrete for the five existing beads** — each child bead maps to
  named files/lines that match C4 §2/§5 (CrewStartRequest field + crew-scoped resolver;
  crewcodexlaunchspec.go sibling; JSON-RPC boot-seed; RPC-result thread_id capture + registry field;
  keeper-window skip). The mappings are sound. Only the beadless core subsystem (finding #3) is the gap.
- **New failure modes are enumerated with correct machinery mapping** (respawn-then-`thread/resume`,
  JSON-RPC reconnect as genuinely-new, per-turn deadline distinct from the worker run-ceiling,
  last-good/yanked-binary correctly ruled N/A) — faithful to C4 §4.
</content>
</invoke>
