# Chief-Architect Review — P1 / P2 / P3 (pre-kerf)

**Reviewer:** chief-architect pass (Fable), 2026-07-21. Design-only (daemon/comms DOWN).
**Scope:** `p1-kernel-fabric/_plan.md`, `p2-extraction/_plan.md`, `p3-distributed-execution/_plan.md`
against DECISIONS.md (frame + C1–C6 + kernel notes), INPUTS.md, research A2–A5, and — for
cross-consistency — `2026-07-21-codex-first/_plan.md` + realignment DECISIONS (D1–D4).
All spot-checks below were run against the live tree on `phase1-session-restart-substrate`.

---

## Verdicts

- **P1 — NEEDS-REVISION (minor).** Honors C1–C6 faithfully; the A2 recast is accurate and the
  drop/reuse split is right. Two fixes: state the LOOKUP deferral as a *contract with P3*
  (P3 currently marks it REQUIRED — see coherence #1), and resolve the channel-ownership
  ambiguity in §3 (see below). One of its operator questions (Q-2) is already answered by
  locked C3 and should be deleted.
- **P2 — NEEDS-REVISION (specific factual fixes).** Best-grounded of the three — churn numbers
  verified exact (workloop 316/90d, codexlaunchspec 15, dot_cascade 94; E1 = exactly 16 files
  / 4,766 LOC). But the E1 "zero deep-daemon-internal calls, **verified**" claim is **false**
  (evidence below), the proposed depguard allowlist is missing `lifecycle/tmux`, the E4 scope
  contradicts D4, and one headline metric is mislabeled.
- **P3 — NEEDS-REVISION (one internal contradiction + one unresolved design fork).** Strong on
  C1/C5/landmine ring-fencing. But §3 contradicts §4 *and* P1 on LOOKUP; the plan never decides
  whether the container is a kernel node or a worker-supervised dumb process; and the v1
  dispatch protocol — the exact crack A5 R4 warned about — is not yet anyone's named deliverable.

None of the three violates a locked decision. All three are kerf-ready **after** the must-fix
items in the bottom line.

### P1 specifics

- **(a) Locked decisions:** honored. C1 (§1 "not the data plane", Q-5 proposes enforcing it),
  C3 (§3 two-layer answer, verbatim), C5 (§4 durability tools), C6 (§2 recast is clean and the
  consequences — channels-for-streams, namespace-scoped handle, no go-plugin — are correctly
  derived). The kernel-design note (reserve resource seam, build State only) is implemented
  exactly (§2 `Resources`).
- **(b) Grounding:** the substrate-v2 reuse/drop list (§6) matches A2's own
  reuse-vs-drop-with-premise section item for item. `internal/kernel` does not exist yet
  (verified) — genuinely greenfield, no squatting package.
- **(c) "Resolved" questions:** genuinely resolved, with one exception — "Minimal P3 slice →
  §4 table … LOOKUP and real mesh deferred" is *not* resolved, because P3 §3.2 declares
  ephemeral LOOKUP join/leave "**P3 REQUIRES this from P1**." One of the two plans is wrong
  (resolution in coherence #1).
- **(d) Scope:** right-sized. In-memory-first + transport double (§5) is the correct
  anti-big-bang structure (A5 R1). The refusal list and `[]byte` rule carry over intact.
- **(e) Kerf surprise:** §3 says the primary runs a "**dispatch plugin**" and every other
  daemon runs a "**worker plugin**." Under §2's manifest rule a namespace "owns
  `<namespace>.*`" — so a separately-namespaced worker plugin cannot publish on
  `dispatch.status` or subscribe-as-group on `dispatch.work` unless cross-namespace channel
  *use* (vs. storage) is explicitly legal. substrate-v2 avoided this because the same plugin
  (comms) ran on every box. **Fix:** state that dispatch is ONE plugin deployed on every
  daemon with `role: primary|worker` config (same namespace everywhere), or define
  publish/subscribe rights on channels you don't own. Kerf will hit this in the first week
  otherwise.

### P2 specifics

- **(a) Locked decisions:** honors the steady-stream execution style, C4 parallelism, and the
  hard rule (§4). Non-goals correctly exclude P1 work.
- **(b) Grounding — mostly excellent, three errors:**
  1. **E1 blast-radius claim is overstated.** "zero deep-daemon-internal calls, verified" —
     imports are indeed clean (verified), but all 16 E1 files share `package daemon`, and a
     symbol sweep finds **same-package calls into 6 non-E1 daemon files**:
     `{claude,pi}launchspec.go → validateModel/validateEffort` (`modelpreference.go`);
     `{codex,pi}launchspec.go → implementerResumeSeedPrompt` (`agentseedprompt.go`);
     `{codex,pi}commit.go → resolveWorktreeHEADVia` (`pasteinject.go`);
     `pi_profile_resolve.go → emitBeadLabelConflict` (`moderesolve.go`);
     `claudeheartbeat/codex+pibillingguard → EmitWithRunID` (`workloopeventsource.go`);
     plus error types in `branching.go`. Still LOW-risk — these are small helpers/ports —
     but E1 needs an explicit helper-disposition step (move to `internal/harness/shared`,
     inject as a port, or move the helper file), and the "one mechanical move" framing must
     be softened. This is exactly the kind of surprise the plan claims cannot happen.
  2. **The proposed depguard allowlist is wrong.** §3.1 proposes
     `allow: [$gostd, core, handler, handlercontract, workspace, self]` — but
     `claudelaunchspec.go`, `codexcommit.go`, and `picommit.go` import
     `internal/lifecycle/tmux` (verified). Add `lifecycle/tmux` (a clean seam package,
     fan-in 11) or the fence fails on day one.
  3. **"631 non-test `.go` files" is a mislabel.** Measured: 641 total `.go` files recursive
     (incl. tests), **134 non-test**; the 56,583 LOC figure is the top-level non-test count
     and is correct. Fix the label — a "measured ground truth" section can't lead with a
     wrong number.
- **(c) Resolved questions:** the four §6 resolutions are sound; "no cross-harness code
  coupling (only comments)" verified for the harness files proper.
- **(d/e) Scope + surprises:** ordering (cold-first, E6 parked) is right per A4/A5 R3. Two
  sequencing hazards not in the plan: (i) **codex-first edits the exact files E1a moves**
  (`codexlaunchspec.go` sandbox flip, writable-roots deletion, `substrate_select.go`) — E1a
  must land *after* codex-first Steps 1–3 or the "cold file" premise is void for the one
  sub-release that matters most; (ii) §4/Q5 says run the P2 crew on Codex "once **E1a** proves
  Codex operational" — E1a (a file move) proves nothing; the gate is the **codex-first plan's
  Step-2 live bead**. Fix the reference.

### P3 specifics

- **(a) Locked decisions:** C1 made literal (§2.2/§2.4 — reference-not-payload, git-branch
  home), C5 honored and extended sensibly (L1–L9 with L5 mandatory), C3 topology restated
  correctly in §4, C6 restated correctly in §3. Codex-first (Priority-0) and the
  hk-bkd6h/hk-2hfyt/warm-cache ring-fence (§6) are exactly right — §6.1 elevating hk-2hfyt to
  #1 pre-work is the single best call in the plan.
- **(b) Grounding:** A3 claims (sidecar in-band, golden image, ProxyJump, one-worker cap)
  reproduced faithfully; `PrimaryWorkerIndex`/`ErrTooManyWorkers` and the workloop
  tunnel-always-built behavior verified in code.
- **(c) "Resolved" that isn't:** "Addressing across the two hops → … for the kernel path, the
  container reaches its worker over the kernel transport" (§7) — this quietly asserts the
  container is a **kernel node**, which contradicts §2.3 (driver at home, dumb container) and
  detonates P1's LOOKUP/roster deferral (ephemeral membership). Not resolved; see coherence #2.
- **(d) Scope:** right-sized otherwise; serialized-first (§4.3) and deferred
  leases/scheduling are correct per C5.
- **(e) Kerf surprises:** (i) the v1 dispatch protocol state machine is not designed and not
  assigned (coherence #3); (ii) L7 says it "extends hk-5h759's fail-closed guard" — but the
  codex-first plan **deletes** that guard (3a: `requireBoundary=false`, refused-argv0 removed,
  `CodexRequireIsolationBoundary` plumbing stripped in Step 3). L7 is a *new* guard at the
  container-spawn seam, not an extension of a deleted one; (iii) the scaffold's own flagged
  simplification — gate reverse-tunnel construction so a codex container run isn't failed by a
  tunnel it never uses — is an edit **inside `internal/daemon/workloop.go`**, which collides
  with the hard rule as written (coherence #5).

---

## Cross-plan coherence issues (ranked)

**#1 — LOOKUP: P1 defers what P3 marks REQUIRED. (Fix in P3, plus one line in P1.)**
P1 §4 defers "LOOKUP / dynamic worker registration" out of the day-one slice, citing C3.
P3 §3.1 lists LOOKUP as one of six day-one primitives ("worker/container registers itself"),
§2.5 advertises "registers itself via kernel LOOKUP; N workers," and §3.2.1 says in bold
"**P3 REQUIRES this from P1**." Meanwhile P3 §4 says topology is *static config* — workers
name the primary, "not dynamic discovery (that's a later capability)." P3 §3 and P3 §4
cannot both be true. **Resolution: the static-config reading wins** (it is what C3 locked):
v1 has no LOOKUP consumer — worker addresses are static, and containers are supervised by
their local worker (L2 process polls), so nothing ephemeral needs fabric-visible identity.
Rewrite P3 §3.1/§3.2/§2.5 to move LOOKUP + ephemeral join/leave to "first post-v1 increment,"
and have P1 name the concrete trigger ("LOOKUP lands when dynamic worker join is scheduled;
the in-memory kernel may implement `Lookup` trivially from day one since the interface ships").
Without this fix, kerf will scope P1 and P3 against contradictory contracts.

**#2 — The container-as-kernel-node fork is undecided, and three plans depend on the answer.
(Fix in P3; ripples to P1 roster scope and P2's E1 rationale.)**
Two incompatible v1 architectures coexist in P3: (a) **sidecar model** (§2.3): container runs
only `codex app-server`; the driver stays outside; container is a dumb supervised process;
only daemons are kernel nodes. (b) **Option-3 model** (§7 addressing, §2.2 "agent-runner",
golden image carrying "the harmonik worker binary"): the container runs harmonik code and
reaches its worker "over the kernel transport" — i.e. it IS a kernel node. The plans price
these very differently: (b) requires ephemeral roster/LOOKUP membership (breaking P1's
deferral) and is what makes P2's "E1 IS minimal-harmonik-in-a-container" claim literal; (a)
needs neither, but then the open question is **who hosts the codex driver for a remote
worker** — if the driver stays on the *primary's* host, the JSON-RPC wire bypasses the worker
entirely (primary↔container direct ssh), which contradicts the worker-supervises model. The
coherent v1 answer: **(a), with the driver moving to the worker** — the worker binary is the
"minimal harmonik" (links `internal/harness/codex` + kernel + dispatch-plugin worker side,
never `internal/daemon`), and the container stays dumb. P3 must state this and define the
worker-binary package closure; that is also what turns P2 E1's P3-enablement claim from
narrative into a checkable link-set.

**#3 — The durable-dispatch layer falls through the crack A5 R4 predicted. (Fix in P3:
name it as a kerf deliverable.)**
P1 §4: leases/redelivery/orphan recovery are "P3's design, not P1's" but "must be designed
with the same rigor as the kernel interface." P3 §3.2.4/§4.2: full leases are DEFERRED; v1 is
"simple mark-failed-and-requeue." The L1–L9 table is a *control list*, not a protocol: nobody
has designed the v1 state machine — offer → claim-ack (REQ_REPLY) → running → done/failed →
requeue, its KV/journal schema, `message_id` dedupe, and crucially the **re-offer loop**: on
an at-most-once fabric, a bead Published to `dispatch.work` with no live subscriber silently
evaporates (Interest is a hint, never an ACK — P1 §2), so even the "simple" version needs the
primary to hold every bead durably in KV until acked and re-offer on timeout. That is lease
machinery in embryo, and it is currently in neither plan's deliverables. **Fix:** P3 adds a
named design artifact ("v1 dispatch protocol: message flows + durable-state schema + requeue
rules") as a kerf task gating implementation. P1's four-durability-tools slice is sufficient
for it — no P1 change needed beyond #1.

**#4 — hk-5h759: codex-first deletes the guard P3 plans to reuse. (Fix in P3 §5/§4.2-L7.)**
P3 §5.1 "Reuse the landed seams: … the fail-closed spawn guard (hk-5h759)" and L7 "extends
hk-5h759 fail-closed guard." The codex-first plan (Priority-0, lands first) removes that
guard's enforcement points and plumbing (its §3a, Step 3). P3 should say: *build a new*
container-reachability fail-closed check at the container-spawn seam (connect-probe model per
A3), preserving the hk-5h759 *principle* (never fall back to local unsandboxed), not the
deleted mechanism.

**#5 — The hard rule ("nothing new for P3 in `internal/daemon`") is honored in spirit but
imprecise in letter, and the scaffold breaks the letter. (Fix wording in P2 §4 + P3 §5.)**
P2's freeze tripwire enforces it mechanically post-E1/E4 — good. But P3's Option-4 scaffold
needs at least one daemon edit *before* E4 lands: gating reverse-tunnel construction off the
codex path (P3 §2.3 flags it), plus ProxyJump config threading through the existing remote
path — all of which lives in `workloop.go`. As written, the rule makes the scaffold illegal.
**Fix:** define the rule as "the KERNEL execution path (dispatch plugin, container supervisor,
worker binary) is born outside daemon; the time-boxed scaffold MAY make minimal edits to the
EXISTING remote path it rides, and those edits die with it at demolition." Also assign the
tunnel-gating edit to exactly one owner (recommend: P3 scaffold crew, since it blocks their
E2E; P2 E4 then extracts-or-deletes what remains).

**#6 — P2 E4 partially contradicts D4: it relocates code that is scheduled to die. (Fix in
P2 E4.)**
D4 scraps ssh-per-node; P3 §2.5 exists so it doesn't get rebuilt; the codex path uses no
reverse tunnel; Claude-in-container is out of v1. Yet E4 proposes moving `reversetunnel.go` →
`internal/transport/tunnel` and teasing the tunnel-readiness logic out of workloop as an
extraction. Extracting doomed code is wasted motion and risks re-legitimizing it. **Re-scope
E4 as "extract-or-DELETE, deletion preferred":** keep and extract what P3's scaffold actually
reuses (`CommandRunner`/`SSHRunner`/`CommandInDir`, `workers.Registry` health payloads);
delete (behind the demolition trigger) the reverse-tunnel/hook-relay machinery rather than
rehoming it. E4's real P3-unblocking content is the CommandRunner-threading extraction, not
tunnel relocation — the plan's own §4 argument supports this.

**#7 — Ownership check: no orphans found, two clarifications.** (i) `ErrTooManyWorkers` is
retired *by the kernel dispatch path*, not lifted in the old registry — no one should build
workers.yaml v2; P3 should say so in one line (its §1 "lift the cap" reads as if the registry
gets fixed). (ii) The codex-first plan leaves `codexWorkerRoutingRunner` inert "for P3 to
delete" while P2 E4 owns transport disposition — assign final deletion to E4's
extract-or-delete pass to avoid double-claim.

**#8 — P1's exposed slice vs P3's needs, primitive by primitive (post-#1):** P2P work channel
with groups ✓ (P1 `Publish(groupKey)`/`Subscribe(group)`), REQUEST_REPLY kickoff/ack ✓,
PUBSUB status ✓, roster for *worker* liveness ✓ (static peers suffice for v1), KV-CAS +
journal ✓ (`Resources.State()`), `Info`/payload ceiling ✓. LOOKUP ✗ by design once #1 is
fixed. **No other gap found** — the slice is minimal-but-sufficient for a static-worker v1.

---

## Consolidated operator questions

Raw count: P1×5, P2×5, P3×5, codex-first×3 = 18. After dedup + resolving what the locked
decisions already answer: **3 genuinely need the operator**, 1 is a defaults-FYI batch.

### NEEDS-OPERATOR-JUDGMENT (3)

**OQ-1 — Ratify the kill-criteria / boundary-test guardrail package.**
*(Dedupes P1 Q-3 + P2 Q4 + P3 §3.2 guardrail + P2 Q2 severity.)* DECISIONS.md lists this as
"proposed — to ratify," so it is explicitly reserved for you. Package: dep-allowlist test,
vocabulary/boundary test, `[]byte`-payload rule, kernel-size tripwire, "two plugins needing
new kernel verbs = stop," P2's freeze tripwire — with the freeze tripwire as **hard CI
failure** (a warning will not survive ~900 daemon commits/60d).
**Recommendation: ratify the whole package, hard-fail severity.** One yes covers four raw
questions across three plans.

**OQ-2 — Sign off the C5 liveness set L1–L9.** *(P3 Q5.)* C5 reserved "enumerate the handful"
for your review. L1–L4 are your own controls; L5 is C5's non-skippable minimum; L6–L9 are
each deterministic, cheap, and close a distinct strand-the-bead hole (box-death requeue-all,
spawn fail-closed, DOA timeout, result-return watchdog).
**Recommendation: adopt all nine for v1** — trimming any of L6–L9 re-opens a known hole for
negligible savings.

**OQ-3 — Review-token posture: does Claude keep reviewing codex-implemented beads?**
*(Codex-first Q1; touches P2 Q5's crew-staffing economics.)* Codex-implements/Claude-reviews
spends some Claude on every offloaded bead but keeps the independent-eyes review gate strong
while Codex is unproven. **Recommendation: keep Claude-reviews for now; revisit after Codex
crews have cleared a meaningful batch cleanly** (then consider codex-reviews for low-risk
bead classes first).

### RESOLVABLE-WITHOUT-OPERATOR (answered here; veto if any answer looks wrong)

- **Transport product choice (P1 Q-1):** defer; ship in-memory kernel first, decide
  NATS-embed vs owned-TCP when the first *cross-machine* P3 run is scheduled. The 6-method
  seam makes this genuinely deferrable.
- **Static-primary first cut (P1 Q-2):** already locked by C3 verbatim ("static config, not
  dynamic discovery yet"). Delete the question; record "leaderless is contracted, demonstrated
  later" as a known scope line.
- **Live-reload loss (P1 Q-4):** accepted consequence of locked C6 (in-proc plugins ⇒ daemon
  restart to swap). FYI, not a question; reopen only if hot-swap ever becomes a requirement.
- **Payload ceiling (P1 Q-5):** hard low ceiling (order 256KB) — it converts locked C1 from
  discipline into mechanism. Nothing in P3 needs large payloads (bead id + SHA).
- **Harness package home (P2 Q1):** top-level `internal/harness/{claude,codex,pi}` — follows
  directly from the container-link goal; a daemon sub-path defeats the point.
- **E6 timing (P2 Q3):** park — name the core type-families, cut none until one proves a
  hot-spot post-E5. (Plan already recommends this; A4/A5 concur.)
- **P2 crew on Codex (P2 Q5):** already decided by PRIORITY-0 ("staff them on Codex once it's
  up"). Only fix: the gate is codex-first Step-2's live bead, not E1a.
- **Scaffold demolition (P3 Q1):** condition-based trigger is enough — a bead blocked-by the
  kernel plugin's first green container run, reviewed at each P1 milestone. A calendar date
  adds nothing the tracking bead doesn't.
- **v1 concurrency (P3 Q2):** serialized single-container first, measure, then increment —
  this is C5's keep-it-simple applied; no target number needed up front.
- **Ephemeral-vs-warm owner (P3 Q3):** measure first; if numbers demand a resident warm-pool
  worker (a second lifecycle), bring the numbers to you *then*. Not a blocking decision now.
- **Security posture (P3 Q4):** v1 stays inside the trusted 3-owned-boxes/Tailscale boundary
  (A2's scope). Untrusted-machine execution is a separately-scoped later effort.
- **Inert ssh seam (codex-first Q2):** leave inert in codex-first; P2 E4's extract-or-delete
  pass owns final disposition (see coherence #7).
- **Verification bead (codex-first Q3):** Captain's call; prefer a real, trivial backlog bead
  over a throwaway (proves the whole loop and clears real work).

---

## Bottom line

**Yes — all three proceed to kerf after the must-fix list below.** No locked decision is
violated, the frame is honored, scope is right-sized everywhere, and the cross-plan
architecture is fundamentally coherent — the defects are contract mismatches and grounding
errors of exactly the kind this review exists to catch before they calcify.

**Must-fix before kerf (in order of blast radius):**
1. **P3:** resolve the LOOKUP contradiction (static v1; LOOKUP → post-v1 increment) — and P1
   adds the matching trigger line. *(Coherence #1.)*
2. **P3:** decide the container-node fork — recommend dumb container + driver-on-worker; define
   the worker-binary package closure. *(Coherence #2.)*
3. **P3:** add the v1 dispatch-protocol design (state machine + durable schema + re-offer loop)
   as a named kerf deliverable. *(Coherence #3.)*
4. **P2:** correct E1's grounding (same-package helper coupling + disposition step; add
   `lifecycle/tmux` to the allowlist; fix the 631-files label) and sequence E1a after
   codex-first Steps 1–3.
5. **P2:** re-scope E4 to extract-or-delete per D4; **P2+P3:** write the scaffold carve-out
   into the hard rule and assign the tunnel-gating edit to one owner. *(Coherence #5/#6.)*
6. **P1:** resolve dispatch-plugin channel ownership (one plugin, role-configured); drop Q-2
   from the operator list.
7. **P3:** re-word L7 / §5.1 — new fail-closed guard, not an extension of the deleted hk-5h759
   mechanism. *(Coherence #4.)*

Then put OQ-1/OQ-2/OQ-3 (only) in front of the operator, with the RESOLVABLE list attached as
adopted defaults he can veto.
