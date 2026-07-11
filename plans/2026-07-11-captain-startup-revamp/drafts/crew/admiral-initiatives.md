<!-- DRAFT — proposed replacement for .harmonik/crew/admiral-initiatives.md
     (startup-doc revamp Stage 2 companion, per 02-cutover §0.3 / step 2.3 + 00-SYNTHESIS §6).
     Trim per synthesis (266 → ~55 lines):
     - L53–169 quality-system program narrative + MR table: the program detail already lives in
       plans/2026-07-06-quality-system/ (00-SYNTHESIS there is the map) — verify nothing unique
       is lost before deploy; MR2/MR3 survive below as one KNOWN line so a named operator
       initiative cannot silently vanish.
     - L170–210 ACTIVE/ON-DECK/GATED tables: CUT — stale 06-25-era rows the live header itself
       called "STALE historical context", superseded by the ★★ 07-11 operator order. Git is the archive.
     - L212–266 six audit-marker journals: CUT — audits report over comms + the admiral handoff,
       never journal here (the file's own one-line-per-initiative charter).
     Carried forward: PRE-DEPLOY E2E GATE standing rule (verbatim); status vocabulary (autonomy
     semantics — load-bearing); IRON RULE; the ★★ table updated to the 07-11 ~04:26Z reconcile
     (codex-as-crew row updated: Option B killed, piter on app-server research). NEW: pending-
     operator-decisions section — the admiral SURFACES these when the operator is present
     (03-operator-decisions.md Q2: decisions must not just sit unraised).
     Banner removed on deploy; deploys with captain-lanes.md + lanes.json + the direction-log
     compaction (cutover step 2.3). -->

# Admiral — Major-Initiatives Registry

> **STANDING RULE (operator-mandated 2026-07-05) — PRE-DEPLOY E2E TEST GATE.** No daemon deploy
> ships without new end-to-end tests, added that deploy, that reproduce the changed behavior on
> a real launch path IN ISOLATION from the live daemon (never test on the primary daemon; green
> units are not the gate). Enforce every deploy. Canonical: orchestrator-rules §"PRE-DEPLOY
> END-TO-END TEST GATE" + `docs/daemon-redeploy.md` GATE 0 + admiral mission Hard bounds. Ties
> to the `codename:daemon-testbed` epic (hk-zk0v2) — that harness is what makes this gate cheap.

> **PRINCIPLE — a snapshot of the big rocks, never a journal.** ALL major initiatives + which
> are active/on-deck/parked, one line each; detail lives in the bead/epic/plan each line points
> at. Every audit reconciles against ground truth (`harmonik digest` + kerf next +
> captain-lanes.md) and REWRITES in place — findings report over comms + the admiral handoff,
> never accrete here. Git is the archive. Cap ~50 lines. Complements captain-lanes.md (which
> crew is on which lane now); THIS file ranks the rocks.
>
> **Status vocabulary:** ACTIVE (crew/queue working it now) · ON-DECK (next to staff, no
> blocker) · PARKED (zero ready beads now — a FACT not a hold; self-resumable when work appears)
> · GATED (held by a NAMED/DATED/OWNED/EXPIRING gate, mirrors `lanes.json.gate`; no live gate =
> KNOWN/resumable, orchestrator-rules §Autonomy) · DONE (landed; kept briefly for context).

updated: 2026-07-11T04:26:00Z (admiral boot audit — pi flagship close + pi-provider-switch landed folded in)

## ★★ OPERATOR-SET PRIORITY ORDER — authoritative (2026-07-11)

> **This is THE priority. Documented so it stops being re-litigated (operator, 2026-07-11).**
> Run the ACTIVE lanes IN PARALLEL — one crew each, file-disjoint, every non-conflicting slot full.
>
> **IRON RULE: NEVER hold up the whole work pipeline for one bead or one initiative.** A stuck
> leg goes through the normal review path at its own pace. Nothing deploys unassessed — that is
> a per-item gate, NEVER a fleet-wide freeze.

| # | Initiative | Plain description | Status |
|---|---|---|---|
| 1 | **Pi** | Pi harness green in-daemon + model/provider switch. | flagship DONE (hk-hcrvb CLOSED 07-11, deployed + prod-canary-green 59089968) · pi-provider-switch LANDED (hk-m6uu2.*) · ACTIVE on follow-up harness beads (kynes; hk-cdpxu next) |
| 2 | **Remote** | Remote macOS SSH worker — buggy, needs a LOT of testing (`remote-substrate` + `remote-hardening`). | ACTIVE (hawat) · 41/47 + hardening · gb-mbp live re-enable OPERATOR-HELD |
| 3 | **Codex-as-crew** | Crew orchestrator on Codex to offload when needed (epic hk-q3ovr). | ACTIVE-RESEARCH (piter) · Option B KILLED by operator 07-11 (hard no, never revisit) → `codex-app-server` kerf work; design at ratification gate; hk-l63b9 PARKED until ratified |
| 4 | **Quality-enforcement** | Every gate fail-closed so nothing merges/deploys unassessed (`quality-enforcement`). | ACTIVE (stilgar) · 10/18 |
| 5 | **comms-test-harness** | Harden the inter-agent bus with real L0/L1/L2 tests. | ACTIVE (yueh) · 20/27 · B1 hk-8xspi + B2 hk-qw63o ratified + dispatched |

**DEPRIORITIZED — do NOT staff:** eval-program (10/23) · flywheel (30/39, needs a COMPLETE
re-assessment first) · dehardcode (5/9).

**KNOWN, unstaffed** (so a named initiative can't vanish — verify before resurrecting): MR3
dispatch-time model selection + auto Claude-throttle (`plans/2026-07-05-model-selection/`); MR2
concurrent multi-provider pi (largely met by pi-provider-switch, confirm remainder). Quality-
system program detail: `plans/2026-07-06-quality-system/`.

## Pending operator decisions — the admiral SURFACES these whenever the operator is present

> Decisions must not sit unraised (operator, 2026-07-11). Raise them when the operator
> interacts; strike each line the moment it is settled.

- **codex-app-server ratification** (surfaced 07-11 06:47Z): (a) ratify+build now · (b) ratify
  but gate on a backend-auth spike [captain rec] · (c) send back. hk-l63b9 parked meanwhile.
  Design: `~/.kerf/projects/gregberns-harmonik/codex-app-server/04-design/`.
- **hk-0639** (Codex local-soak epic) — done-by-charter; captain recommends CLOSE.
- **hk-4u1mb** (reviewer diff-budget) — conflicts with shipped heartbeat contract; leaning DEFER.
- **Governor** `liveness_no_progress_n`=10 (observe-only) — stands unless operator says 0.
