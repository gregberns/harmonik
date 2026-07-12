# Harmonik Codebase Census — Decision-Grade Synthesis

> Run 2026-07-12 by admiral. 10 Fable assessors reading real code + 10 adversarial
> challengers + synthesis. 21 agents, ~900k tokens, 0 errors. **Every verdict was
> UPHELD under adversarial challenge — no classification was overturned.**

## 1. The honest verdict

This codebase is recoverable, and the operator's instincts are correct on every count
except one: the problem is **not** that the domain logic is slop — the queue model, the
lifecycle sweeps, the harness axes, and ~466 incident-pinned regression tests are genuine,
hard-won value. The root problem is that **two architectural decisions were never made**:

(a) the daemon has **no internal boundaries** — 55k LOC, 85-field god-struct, 2,400-line
dispatch function in one flat package, so every fix is threaded through shared mutable state
and 80% of fix commits land in one blast radius; and

(b) both IO boundaries to agents (tmux paste-injection) and to remote workers (fresh
SSH-string-through-login-shell per op, coordinated by box-A mutexes) are **ack-free channels**
that can never be made reliable, only re-caulked.

The mutexes-equal-bugs feeling is real but is a *symptom*: mutexes over network/build IO exist
because there is no protocol, no state machine, and no single writer anywhere state is shared.
Additionally, ~50k LOC of "tests" (operatornfr, specaudit) never execute the product, which is
exactly why "I don't know if what it's fixing is real" — the green suite was partly theater.
None of this requires a ground-up rewrite; it requires two rebuilds behind existing seams and
one large extraction, under the regression suite that already exists.

## 2. Keep / Simplify / Rebuild / Delete

| Area | Final call | Reason | Confidence |
|---|---|---|---|
| **daemon-workloop** (workloop/dot_cascade/reviewloop) | **Rebuild** (staged, strangler) | Three mega-functions + 780-line deps struct + ~910 incident caulks; 63 fix commits/20d proves the structure can't absorb change | High |
| **daemon-godpackage** (internal/daemon structure) | **Simplify** | ≥8 coherent subsystems trapped in one namespace; extraction is relocation, not redesign | High |
| **daemon-harness** (multi-harness integration) | **Simplify** | Right abstraction axes, best tests in the package — but claude bypasses its own interface and the codex WAL guard is 380 lines of symptom-treatment | High |
| **remote** (SSHRunner, remotematerialize, worktree-create) | **Rebuild** | Stringly-typed RPC through remote login shells, no worker-side agent, box-A mutexes owning worker state, 92 dual-path branches, 166 fix commits since 06-20 | High |
| **keeper** | **Simplify** | Root cause (multi-writer gauge) already fixed, sanctioned deletion COMPLETE; remaining work is flattening two god-files | High |
| **core-eventreg** | **Simplify (cut deeper)** | Registry's decode/validate surface is production-dead; 388-line compat table all-vacuous with zero consumers | High |
| **lifecycle-reconcile** | **Simplify** | Substrate sweeps are earned; the Class A–D matrix compensates for a dual-source-of-truth write path — extend the half-existing intent log (BI-031), don't build new | High |
| **tmux-io** (pasteinject/tmuxsubstrate) | **Rebuild** (input channel only) | Ack-free paste into a TUI: 44 incident beads, 4 generations of workaround-on-workaround, ~48 sleep sites | High |
| **queue** | **Simplify** | The one mutex-free, spec-pinned, well-tested island — but its HandlerAdapter is being colonized by daemon knobs and queue.json has two writers with a live lost-update path | High |
| **test-bloat** (operatornfr/specaudit/scenario) | **Simplify (mostly delete)** | operatornfr asserts its own constants; specaudit is 37k LOC of markdown-regex; only scenario's harness + ~11 files run real code | High |

## 3. Root structural problems, ranked

**1. The daemon god-package + god-function core.** 85-field `workLoopDeps` passed *by value*
(race-safety by convention only) into a 2,380-line `beadRunOne` with 17 params, a `*bool`
out-param, and mutable closure flags; no per-run state machine, so every lifecycle fix is
hand-threaded through shared locals. 598 hk- annotations in workloop.go alone; 80% of all
fix-class commits land here. **This is the treadmill itself** — the 63-fix/20d rate is the rent.

**2. Ack-free IO boundaries — tmux input and SSH remote.** Two channels where the daemon
cannot know an op succeeded. Paste-inject: exit 0 ≠ TUI accepted input, so correctness rests on
750ms sleeps, blind Enter retries, and screen-scraping. Remote: fresh `ssh -- '<string>'` per op
through the remote *login shell*, ControlMaster deliberately disabled, distributed state serialized
by box-A mutexes (mergeMu held over network fetch stalls the whole daemon), and a 68-line Python
flock script embedded as a Go string to patch a race the architecture created. 44 incident beads in
one file; 166 remote fix commits. **This is where "everything keeps breaking" comes from** — each
fix cites the previous fix as its cause.

**3. No single writer anywhere state is shared.** queue.json has two writer paths (confirmed live
lost-update at rpc.go:1016); beads ledger and queue file both record "is this done" with divergence
as *documented steady-state* (Class B fires ~83×/session on normal restarts). Cost: the entire
QM-002b class matrix + two overlapping crash-recovery mechanisms for one ambiguity.

**4. Test-mass theater masking real exposure.** ~50k LOC that never `exec` the product —
tautological constant-assertions and spec-prose regex — inflating the green-suite signal while the
daemon bled unprotected. For these packages, "is it testing anything real" = no.

**5. Incident-driven accretion as the de facto design process.** Files named by bead ID, magic
constants frozen from incidents (`agentSpawnSem = 3`), invariants living in comments instead of
types. The codebase is a sediment log; nobody (including agents) can tell load-bearing caulk from
dead scar tissue — so "so much slop" is accurate perception even where the logic is sound.

## 4. Where to start — first three moves, in order

**Move 1 — Delete the test theater and re-baseline (days).** Strip operatornfr's self-asserting
fixture-mirror tests (keep exitcode.go, securitypolicy, sandboxinvariant + their real tests);
collapse specaudit into one CI lint script outside the test suite; prune scenario to the harness +
~11 behavioral files. Delete the dead event-registry surface (pertypecompat table, DecodePayload /
ValidateEnvelopeSchemaVersion — zero production consumers).
*Done when:* `go test ./...` runs only tests that execute product code, and the test/code ratio is a
number you can trust. **First because every later move relies on knowing which green means something.**

**Move 2 — Rebuild the agent input channel behind the existing Substrate seam (~5 days).** Build a
structured-protocol driver (claude headless stream-json / Agent SDK stdin; codex-app-server already
under kerf) as a second `handler.Substrate`, tmux retained only as an observation window. Port one
harness end-to-end, run side-by-side, delete the splash-sleep/paste-verify/enter-retry/pgrep stack.
*Done when:* one full bead run completes on the structured driver with zero sleeps and zero
capture-pane scraping in its path.

**Move 3 — Extract the run lifecycle state machine from beadRunOne.** Define an explicit `Run` struct
+ state machine (claim → worktree → launch → monitor → gate → merge → close) in its own sub-package,
replacing the 17 params, out-params, closure flags. Keep every hk- regression test green throughout —
the 466-file suite is the net that makes this a refactor, not a rewrite. Fold in the merge-coordinator
split (mergeMu becomes an explicit merge queue, never held over network/build IO) as the first phase.
*Done when:* beadRunOne is a thin driver over the state machine, no mutex is held across
git push/build/SSH, and internal/daemon has runexec + merge sub-packages with depguard boundaries.

**Move 4 (after Move 3) — Remote rebuild:** worker-resident agent, real protocol, worker-owned
worktree lifecycle — slots into the Substrate interface Move 3 produces, collapsing the 92
`runner != nil` dual-path sites into Local/Remote implementations of one interface.

### Addendum — live exemplar caught during the freeze (2026-07-12 19:12Z)

A concrete instance of root-problem #3 ("no single writer / reconciler-as-sync") and of the
operator's "I don't know if what it's fixing is real," caught by hawat while the fleet was frozen:

**bead hk-2hfyt showed CLOSED with its fix NEVER APPLIED.** The daemon's noChange-subsumption
close-path matched the literal bead-ID string `hk-2hfyt` in an *unrelated docs commit* (32dc13f7,
a captain-lanes edit) and closed the bead — the honest-probe fix is verifiably ABSENT from
createworktree.go (no rev-parse --verify / test -e .git guard). Consequence: the gb-mbp
fleet-down probe bug is still LIVE behind a "closed" status. The reconcile subsumption matches
bead-ID **mentions**, not fix **content** — so "closed" is not a trustworthy signal.

This widens the "green means nothing" finding beyond test-theater: the **close/reconcile path
itself can fabricate done-status.** Feeds the fix queue on resume as (a) reopen the real fix under
a clean ID, (b) fix the subsumption to require fix-content evidence, not a string mention.

**Second exemplar — the resume-hang (2026-07-12 19:23Z, stilgar).** The opposite failure mode of
the same root problem: a run completes its FIRST implement (commit lands in worktree) then goes
DEAD SILENT at `implementer_resumed` — no output, no heartbeat, no run_stale — so **in-progress
never resolves.** Wedges ~5/5 recent local runs identically (hk-2i36s 74min, hk-nxcvi, hk-bl4d6
26min, hk-zeo5y 45min, hk-6629b in-flight); the lane only advanced via manual salvage. Correlates
with the newly-added QA-execution-gate workflow (implement→commit_gate→review→qa→close, ~0adb6551)
hanging at agent-relaunch. Together with the false-close: **neither done-status NOR in-progress-status
from the run/reconcile pipeline is trustworthy.**

**Sequencing consequence (updates §4).** The resume-hang wedges the very pipeline the carve would
run through — a Move-1 bead dispatched normally would hit the same daemon run→QA-gate→resume wedge.
So **step zero = fix the resume-hang / QA-execution-gate** (stilgar's domain, internal/daemon, no
hawat collision), OR run the earliest carve work OUTSIDE the daemon pipeline (direct agent + manual
land) until it is fixed. Do not assume Move 1 can flow through the current pipeline.

## 5. The hard call

**Freeze-and-carve is the right move.** Freeze feature work on internal/daemon now. The proven core
to carve out and protect: the queue model (spec-pinned, mutex-free, genuinely tested — fix its
two-writer path, evict the HandlerAdapter grab-bag, treat as load-bearing), the lifecycle substrate
sweeps, the harness interface axes, and the regression corpus. The two things that get **rebuilt —
not patched again** — are the agent input channel and the remote substrate, both as async,
protocol-based substrates behind seams that already exist. **No big-bang rewrite:** the 466
regression tests encode 20 days of invariants a rewrite would throw away, and the challenge passes
confirmed the domain logic under the scar tissue is mostly correct. "I can't build the system with
itself" resolves in that order — cut the incident source (Moves 2, 4), give fixes a bounded blast
radius (Move 3), and the system becomes stable enough to dogfood. **Weeks of disciplined structural
work, not months, every phase behind green regression tests.**
