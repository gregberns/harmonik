# Unit E4d — remote orchestration embedded in workloop.go

> ## DECIDED 2026-07-22 — land E4d-0/1/2, **PARK E4d-3**
>
> The operator delegated rulings (A) and (B) to the driving agent, with one decisive new fact:
> **remote support is going to be rebuilt, and relatively soon harmonik will not be SSH-ing into remote
> hosts at all.**
>
> That settles it. E4d-3 exists to lift `ssh -N -R` reverse-tunnel establishment into
> `internal/transport/tunnel`. It is the only one of the four slices that is SSH-shaped, and it is
> precisely the code the rebuild deletes.
>
> **Ruling (A) — answered NO, for now.** The question was whether an already-fenced *stateless* leaf may
> own the lifetime of a per-run process and hand cleanup closures back to daemon. Answering *yes* sets a
> package-charter precedent that would outlive the code it was granted for. If the remote rebuild wants
> that pattern it should adopt it deliberately, with the new transport design in view — not inherit it
> from a legacy SSH slice that is on its way out. Precedents are cheap to grant and expensive to revoke.
>
> **Ruling (B) — moot.** With E4d-3 parked, no non-pure-move transport slice ships, so no §5.4 runtime
> proof waiver is needed. The E4a/E4b runtime-proof debt in `PROGRESS.md` §5 stands unchanged.
>
> **What still ships, and why it survives the rebuild:**
>
> | Slice | Why it is worth landing anyway |
> |---|---|
> | **E4d-0** — seven `ReopenBead` calls through `LedgerPort` | Nothing to do with SSH. It is about ledger writes, and it **drains debt E5 RT16 would otherwise pay**. Pure win. |
> | **E4d-1** — hoist `remoteBeadCtx` to package scope | Gives the per-run remote context a *name*. This actively **helps** the rebuild: an addressable, testable package-level type is a better starting point than a type hidden inside a 2,220-line function body. |
> | **E4d-2** — collapse the duplicated SSH runner literal | Shrinks the SSH surface that the rebuild has to delete, and ships a required regression test for a hazard that "compiles clean and breaks remote". |
>
> Net: the three prep slices pay for themselves in RT16 debt and in leaving the remote path in a better
> shape to be replaced. E4d-3 would have added ~30–35 lines of soon-to-be-deleted code in a new location,
> plus a fence to re-reason about, plus a charter precedent — i.e. it would have made the rebuild *harder*.
>
> **Unpark criterion:** if the remote rebuild lands and still wants tunnel-establishment logic owned by a
> transport leaf, re-open ruling (A) against that design rather than this one.

**Status:** **NEEDS PREP SLICES** for steps 0–2 → **BLOCKED-NEEDS-OPERATOR-DECISION** for step 3 (the
only step that moves code out of `internal/daemon`).

Concretely: three preparatory slices are executable and independently releasable, and they are worth
landing whether or not E4d ever completes. The extraction step itself needs two operator rulings before
anyone writes code (§4). E4d is **no longer "DO NOT ATTEMPT"** — but it is also **not a 619-LOC unit like
E4a+E4b**. Honest size of the move: **~30–35 lines of executable code** (~100 lines including the
comment mass it carries).

> **Line numbers in this document were measured on `phase1-session-restart-substrate` with the working
> tree dirty (2026-07-22).** `internal/daemon/workloop.go` is 6,854 lines and is currently being edited
> by the in-flight `internal/runmerge` carve-out (see §7). Every citation was re-read against the tree at
> the time of writing; they are ~1 line off the numbers in `E4-ssh.md` and in the recon feeds. Re-derive
> before editing.

---

## Why the first plan refused this unit

**The finding, stated plainly and re-verified:** `type remoteBeadCtx struct` is declared at
`internal/daemon/workloop.go:3535`, **inside the body of `beadRunOne`** (function opens at `:3175`,
2,220 lines long). A function-local type has no name outside that body. There is no file, no
package-level declaration, and no symbol to `git mv`. `README.md:103` records the consequence:
*"it cannot compile as a move."*

**The new analysis CONFIRMS the fact and OVERTURNS the conclusion.**

Confirmed, by direct read:

```
internal/daemon/workloop.go:3535	type remoteBeadCtx struct {
internal/daemon/workloop.go:3541	var rbc *remoteBeadCtx
internal/daemon/workloop.go:3547	rbc = &remoteBeadCtx{      // pre-selected worker
internal/daemon/workloop.go:3570	rbc = &remoteBeadCtx{      // fallback SelectWorker
```

Overturned, because *"nothing to `git mv`"* and *"nothing can be extracted"* are different claims, and
this repo has already refuted the second one three times:

- **socket-router SR-2a (`9d7d56a3`)** gave package-level names to 25 `case` bodies that lived inside
  `handleSocketConn`'s 420-line switch — purely additive, zero behavior change, nothing on the hot path
  called the new methods. The next commit flipped the switch.
- **boot-config B2–B6** de-embedded a ~2,000-line `startWithHooks` by inventing package-level state
  structs (`internal/daemon/bootstate.go:31`, `internal/daemon/bootreconcile.go:25`) and extracting phase
  helpers *in place*. Five of six slices created no package at all.
- **RT7 (`53c524b4`)** extracted from `beadRunOne` **itself** — its commit message reports
  `beadRunOne: 2377 -> 2283 lines`.

And the type is trivially liftable once it has a name: all four field types (`workers.Worker`,
`tmuxpkg.CommandRunner`, `*exec.Cmd`, `string`) are already package-visible in `workloop.go`, and the
identifier `remoteBeadCtx` collides with nothing anywhere in `internal/` — the only other occurrence in
the tree is a doc comment at `internal/transport/tunnel/tunnel.go:15` that already names it.

**What actually blocks E4d** is four different things, three of which the first plan did not record:

1. **A source-text conformance floor.** `internal/daemon/conformance_m4c7_test.go:346-349` asserts
   `strings.Count(readRepoFile("internal","daemon","workloop.go"), "rbc != nil") >= 8`, and `:273-296`
   asserts the literal `rbc != nil` appears within the 200 characters preceding
   `hasAPIKeyInEnv(spec.Env)` (the D2 fail-closed guard at `workloop.go:4379` that refuses to forward
   `ANTHROPIC_API_KEY` to a remote worker). Both read the file **by path**. Neither is a compile error.
   Current count: **20** `rbc != nil` + 13 `rbc == nil`, so there are twelve predicates of headroom — the
   floor forbids *collapsing* the dual path, not extracting from it. It does forbid renaming `rbc` and
   forbids moving the D2 guard out of `workloop.go`.
2. **Four frame-scoped defers** whose lifetime is `beadRunOne`'s 2,220-line frame.
3. **Three bare `return`s** out of a *named* return value (`succeeded`).
4. **`RunPorts` is an intra-package seam** — every port interface is declared `package daemon`
   (`internal/daemon/runports.go:18`), so no package outside daemon can call through it.

---

## 1. What E4d actually is

E4d is the remote-execution orchestration still embedded in `beadRunOne`
(`internal/daemon/workloop.go:3175-5395`). **It is not a remote branch.** There is exactly ONE genuine
two-armed `if/else` in the whole run path (the worktree factory, `:3758-3786`). All other `rbc`
predicates are single-armed decorations of one shared linear pipeline — `if rbc != nil { set 1–5 fields }`
— where the local path is the **zero value**, not a parallel implementation.

The useful split is **production** vs **consumption**.

### 1a. Production regions — 6 contiguous blocks, ~243 LOC

| Region | Lines | Code (non-comment) | Extractable? |
|---|---|---:|---|
| `remoteBeadCtx` decl + `var rbc` | `3521-3541` | 7 | **yes** — hoist |
| Worker selection: pre-selected + fallback | `3542-3597` | 35 | **no** — mutates local accounting |
| **Tunnel establish + readiness gate** | `3622-3716` | 55 | **yes** — the movable core |
| `notifyWorkerOffline` closure | `3718-3729` | 9 | maybe (needs only `workers.EmitFunc` + `*workers.Registry`) |
| `preMergeSync` closure | `3731-3753` | 13 | maybe |
| Remote worktree factory arm | `3758-3786` | 21 | **already behind `WorktreePort`** (`:3790`) |
| `EnsureBaseOnWorker` in the merge critsec | `3809-3825` | 17 | **no** — race-fused, see below |

### 1b. Consumption regions — 14 sites, 1–10 lines each, already seam-clean

`:4020-4028` (review-loop 5-tuple) · `:4164-4188` (DOT 5-tuple + the hk-hd2w6 local test-seam arm) ·
`:4299-4303` (`ResolveAgentDaemonSocket`) · `:4339-4342` (launch-spec `rc.Runner`) · `:4379-4385` (D2
fail-closed) · `:4401-4431` (per-run substrate + the inverted `rbc == nil` local-only guard) ·
`:4671-4683` (cold-start semaphore) · `:4904` (`effectiveAgentReadyTimeout`) · plus the two amend-trailer
suppressors at `:4057` and `:4217` and the `run_started` worker fields at `:3916-3920`.

These need **no extraction**. The remote target already crosses to the sub-drivers as a flat 5-tuple of
plain values — `(runner tmux.CommandRunner, workerBinaryPath, workerHookSock, workerSessionName,
workerSessionCwd string)` — declared as a parameter block on `runReviewLoop` (`reviewloop.go:222-223`),
`driveDotWorkflow` (`dot_cascade.go:213-214`), `dispatchDotAgenticNode`, `dispatchDotGateNode`
(`dot_gate.go:99-100/:220-221/:257-258`) and `sub_workflow_runner.go:59-60`. Below that, ~40 `…Via(runner)`
helpers each fork on `runner == nil`. The local/remote distinction is **already fully absorbed by the
pre-existing `tmux.CommandRunner` seam** everywhere below establishment.

> **`runner != nil` is NOT equivalent to remote.** `workloop.go:4179-4181` has
> `else if deps.runner != nil { dotRunner = deps.runner }` — the hk-hd2w6 `Config.Runner` test seam
> (field documented at `:762-769`). A LOCAL run can carry a non-nil `CommandRunner`. Any refactor that
> conflates the two predicates is a behavior change. This killed one candidate design (§2.2).

### 1c. Size

**233 non-comment lines of genuinely remote-specific logic** across 17 disjoint regions inside a
2,220-line function = **10.5% of `beadRunOne`**. Comment density in the region is 52%. Only two regions
are contiguous and self-contained; the rest are 15 scattered 2-to-23-line insertions threaded through
~1,200 lines of shared run logic.

### 1d. The four hard structural facts

**Defers.** Four register on `beadRunOne`'s frame from inside remote blocks —
`deps.workerRegistry.ReleaseSlot()` (`:3582`), `tunnelpkg.ReleasePort(tunnelPort)` (`:3636`), the tunnel
`Kill`+`Wait` closure (`:3683-3688`), and `releaseSpawnSlot()` (`:4682`) — plus `relLocalSlot` (`:3196`)
and `relWorkerSlot` (`:3216`) at the top. LIFO means Kill+Wait fires **before** ReleasePort. Verified:
**no other defer is registered between `:3636` and `:3688`**, so folding those two into one returned
cleanup preserves order exactly. `plans/2026-07-16-giant-retirement/boot-config-REVIEW-seam.md` finding 1
rates this hazard class HIGH ("boot-breaking, not a nit"); `internal/daemon/bootstate.go:28-30` is the
in-tree precedent for the alternative answer (leave lifetime defers in the outer shell).

**Bare returns.** `:3663` (socket path too long), `:3714` (readiness gate failed), `:3848`
(`return succeeded`, base-sync failed), plus `failRun`+return at `:4384` and `:4678`. A callee cannot
return from its caller — it must hand back a decision. Declarable logic delta.

**Enclosing-scope mutation.** The fallback selection block at `:3562-3597` acquires the remote slot **and**
writes `deps.localInFlight.Add(-1)`, `relLocalSlot = false` (a `beadRunOne` local read by the defer at
`:3196`), and `RunHandle.Remote.Store(true)` in the same statements. This is the one genuinely entangled
region. It does not move.

**Race-fused critical section.** `:3809-3825` runs `codesync.EnsureBaseOnWorker` and `rp.Worktree.Create`
inside ONE `mport.Submit()` exclusion domain. The 8-line rationale at `:3792-3804` (hk-lt091) explains
why: with `-o ControlMaster=no` each SSH command is an independent TCP connection, so a sibling bead's
fetch-base can race `git worktree add` at the remote-OS level and leave an uninitialised HEAD that no
retry fixes. Splitting it reopens a race the code exists to close.

---

## 2. Approaches considered

Three shapes were designed and independently adversarially judged. All three judgements are recorded here
because the reasoning is the deliverable.

### 2.1 `promote-in-place` — **WON** (judge verdict: *acceptable*)

**What it was.** Hoist the type to package scope in daemon (retiring the stated blocker); optionally evict
it to `tunnel.Session` in the already-landed, already-fenced `internal/transport/tunnel`; then lift the
tunnel establish sequence into `Prepare` + `Start` returning always-non-nil cleanups; then dedupe the SSH
runner literal.

**What the judge confirmed.** Every compile-level claim survived contact with the tree: no name collision,
no new non-stdlib imports needed (`tunnel.go` already imports `tmuxpkg` and `workers`), no import cycle,
the four field types mutually unassignable so a mis-wired rename is a compile error, the defer ordering
argument correct, the conformance floors correct, and — checked independently — **no test asserts any of
the five tunnel-path stderr format strings**, so moving them with their now-wrong `daemon: workloop:`
prefix byte-verbatim is safe (the `c3fff27d` precedent).

The judge also confirmed the awkward three-part split (`Prepare` → *daemon validates* → `Start`) is
**forced, not stylistic**: `.golangci.yml:334-343` allows `internal/transport` only `$gostd`,
`lifecycle/tmux`, `workers`, `workspace`, and the transport prefix. `internal/lifecycle` is **absent**, so
`lifecycle.ValidateSocketPathLength(daemonHookSock)` at `:3654` cannot move and must sit between the two
calls — which is exactly what preserves the I/O-then-validate-then-spawn ordering.

**What the judge broke (and this plan fixes).**

| Judge finding | Fix applied here |
|---|---|
| "E1b/E1c still queued, RT15 is weeks out, E4d has a free window" — **false.** `db2f4752` (E1b), `66e0041d` (E1c), `5f7762c3` (E1a) all landed; the design read a stale slice table in `PROGRESS.md` that its own phase header contradicts. **RT15 is unblocked today.** | §7 rewritten as a live contention, not a free window. |
| "RT13 has not started" — **false.** `internal/runmerge/` exists with 7 non-test source files, is imported by `workloop.go`, is `AM`/untracked in `git status`, has **zero commits**, and `workloop.go` is already down to 6,854 lines. RT13 is in flight *right now*. | §5 gate: **nothing starts until `runmerge` commits.** |
| Payoff overstated ~2.5× ("block shrinks 99 → 33"). Measured: ~46 lines move, ~46 stay (the two failure arms with their comments), block goes ~95 → ~52. | Headline restated as **~30–35 executable lines**, ~100 with comments. |
| Green criterion `git grep -n 'ControlMaster=no' internal/daemon` → 0 hits **fails on a correct implementation** — there are 4 hits and 2 are prose in the hk-lt091/hk-zexsj rationale comments. | Criterion rewritten to grep `tmuxpkg.SSHRunner{` (§5, E4d-2). |
| Unnamed trap: `conformance_m4c7_test.go:319-325` reads `internal/transport/tunnel/**tunnel.go**` by filename and requires the literals `ReverseTunnelRunner` and `BuildArgs` in **that specific file**. | Recorded as a do-not in §5 and §6. |
| Gate-4 (drive one remote bead) is *not* an inherited waiver: E4a/E4b shipped without it because they were verified **pure moves**; E4d-3 is not. | Escalated as operator ruling **(B)** in §4. |

### 2.2 `ride-e5-ports` — **LOST** (judge verdict: *acceptable*, but the endgame is self-contradictory)

**What it was.** Do not extract E4d at all. Fold it into E5: remoteness becomes a `LaunchPort`
implementation daemon injects; the run drivers stop taking the anonymous 5-tuple and read `ports.Launch`;
`SharedHandles.AgentSpawnSem` / `Workers` / `LocalInFlight` / `RunRegistry` (dead code today at
`runports.go:323-329`) absorb the cold-start semaphore and the selection accounting. The function-local
type stops mattering because RT20 moves `beadRunOne` **whole**.

**Why it lost.** The judge found a contradiction at the load-bearing claim. The design says both
(a) *"RT20 moves `beadRunOne` whole (`:3175-5395`); a function-local type travels with its enclosing
function for free"* and (b) *"the tunnel establishment (`:3622-3716`) stays in `internal/daemon` — and that
is correct."* `:3622-3716` is **inside** `:3175-5395`. Both cannot be true. If establishment travels,
`internal/runloop`'s draft allow-list needs `internal/transport/tunnel` + `internal/lifecycle` (the design
itself flags them as missing). If it stays, a lifted `beadRunOne` must call an unexported daemon function
— forbidden by Go and by the runloop→daemon depguard deny. The only escapes are an unnamed restructure
(hoist establishment out into `runWorkLoop`, relocating a reopen path and three frame defers) or a new
"establish remote exec target" port. So `requires_new_seam: false` is true for its slices and false for the
endgame those slices exist to reach: **the escalation is deferred, not removed.**

Second kill: its P5 folds `effectiveAgentReadyTimeout` into `LaunchPort.ReadyTimeout()`. Verified, four of
its five call sites (`reviewloop.go:586`, `reviewloop.go:1367`, `dot_cascade.go:1772`, `dot_gate.go:475`)
pass `runner != nil`; only `workloop.go:4904` passes `rbc != nil`. Under a local run carrying the
hk-hd2w6 injected runner, two of those resolve **today** to the 210 s remote window and would flip to the
local window. The design names this exact hazard in its own risk register and then commits it. Undeclared
delta.

**What was salvaged from it.** Its P0 (route the in-`beadRunOne` reopens through the existing
`LedgerPort`) is real, verified byte-identical, and is adopted here as **E4d-0** — with the judge's
correction that it must convert **all seven** in-function sites, not the three near remote code, or it
manufactures a legibility defect where sibling calls in the same function use two different idioms.

### 2.3 `thin-transport-facade` — **REJECTED** (judge verdict: *reject*; the designer also refused it)

**What it was.** Leave the orchestration in `workloop` but move every remote *decision* (is-remote,
which worker, readiness deadline, cold-start bound) behind a `transport.RemotePolicy` object, so workloop
holds no remote knowledge.

**Why it lost — the ownership argument, which survived every refutation the judge tried.** The transport
depguard charter (`.golangci.yml:314-322`) scopes the unit to *"tunnel owns the per-run `ssh -N -R`
reverse tunnel + its readiness gate; codesync owns the DD1 worker↔box-A git sync."* A `RemotePolicy`
annexes three concerns from three different owners: worker selection belongs to `internal/workers`
(`registry.go:50,74`); the agent-ready window is handler-contract policy (HC-056,
`specs/handler-contract.md` §4.9, implemented at `agentready.go:94`); the spawn bound is daemon dispatch
capacity whose designated home already exists and is named (`runports.go:326`). Relocating the timeout
would also **add** a `daemon → transport` edge from `dot_gate.go`, which imports no transport package
today. That inverts the layering P2 exists to establish.

Second: **E4a already exhausted the transport-decision surface.** `SSHHostOpts` (`tunnel.go:270`),
`WorkerTCPEndpoint` (`:111`), `WaitWorkerSocketLive` (`:346`), `WorkerSocketReadyTimeout` (`:84`),
`ResolveAgentDaemonSocket` (`:290`) are all already exported. A policy object would own nothing that is
legitimately transport's.

**What the judge corrected in the refusal itself,** and this plan adopts: leading with the conformance
floor as a wall is overstated — the floor is 8 against a count of 20, twelve predicates of headroom, and
it forbids only total collapse. The ownership argument is the kill; the CI floor is a constraint.

**The one real hazard it surfaced, which now binds E4d-2.** `internal/transport/tunnel/tunnel.go:271`
does `if sr, isSSH := r.(tmuxpkg.SSHRunner); isSSH` — a **concrete value-type** assertion — and
`workloop.go:3668` feeds it `rbc.sshRunner`. A dedup constructor returning `*tmuxpkg.SSHRunner`, or any
wrapper type, **compiles cleanly**, makes `SSHHostOpts` return `ok=false`, and silently builds the tunnel
argv with no host opts. On a path `PROGRESS.md` §5 records as never having been driven end-to-end.

---

## 3. Recommended path

**Take `promote-in-place`, corrected by its judge, split so that everything not needing a ruling ships
first.**

```
  (wait)  internal/runmerge / RT13 commits              ← hard precondition, §7
    │
    ├─ E4d-0  route 7 in-beadRunOne reopens through LedgerPort   no ruling needed
    ├─ E4d-1  hoist `type remoteBeadCtx` to package scope        no ruling needed  ← retires the blocker
    ├─ E4d-2  dedupe the two SSHRunner literals                  no ruling needed
    │
    ├─ (operator rulings A + B, §4)
    │
    └─ E4d-3  lift establish+gate into internal/transport/tunnel  ← the only step that moves code out
```

**E4d-0/1/2 are the recommendation regardless of what the operator rules.** They are three no-op
refactors inside `internal/daemon`; each is independently green and releasable; each shrinks whatever
comes next (E4d-3 *or* E5 RT15/RT20). E4d-1 alone retires `README.md:103`'s *"cannot compile as a move"*
rationale and is work RT15 must do anyway — RT15 cannot put this type in a signature until it has a name.

**Be honest about what they are not.** E4d-0/1/2 move **zero lines out of `internal/daemon`**, create
zero depguard edges, and add zero freeze gates. By P2's own charter they are prep, not extraction. Do
not book them against the shrink ledger.

**E4d-3 is the extraction, and it is small.** ~46 lines move (of which ~30 are executable), ~46 stay —
the two failure arms with their comment mass, which must stay because they do ledger writes through
daemon-internal state. The argument for doing it is **not** LOC:

1. **P3 API.** A container/remote dispatcher can then establish a reverse tunnel by calling two functions
   instead of re-deriving an eight-call sequence, without linking the 50k-LOC monolith.
2. **It pays down an E5 RT20 blocker.** If establishment lives in `internal/transport/tunnel` *before*
   RT20 lifts `beadRunOne`, the lift carries only a call site. If it does not, RT20 must widen
   `internal/runloop`'s allow-list to include `internal/transport/tunnel` **and** `internal/lifecycle` —
   the gap `ride-e5-ports`' judge identified and that E5's own draft omits.
3. **It splits a monolithic "DO NOT ATTEMPT"** that currently hides a shippable third.

**Explicitly out of scope for E4d, permanently — relabel these to E5 in `E4-ssh.md` §4:**

| Concern | Lines | Why it is E5's |
|---|---|---|
| Fallback worker selection | `:3562-3597` | mutates `relLocalSlot` / `localInFlight` / `RunHandle.Remote` in the same statements that acquire the remote slot |
| Base-sync in the merge critsec | `:3809-3825` | hk-lt091 race-fused with `rp.Worktree.Create`; rides daemon-internal `MergePort` |
| Cold-start semaphore | `:4671-4683` | dispatch-capacity policy; home already declared at `runports.go:326` (`SharedHandles.AgentSpawnSem`, dead code awaiting RT15) |
| `effectiveAgentReadyTimeout` remoteness | `:4904` | handler-contract policy (HC-056), 5 consumers across 4 files; `E4-ssh.md:61` already ruled it stays |
| `deps.substrate.(*tmuxSubstrate)` reach-ins | `:4026`, `:4177`, `:4419` | `LaunchPort.Substrate()` — E5 RT16 |
| The anonymous 5-tuple → named DTO | 6 files, ~114 refs | consumption half; E5 RT16/RT17/RT18 |

---

## 4. Does this require a new seam?

**E4d-0, E4d-1, E4d-2: NO.** `LedgerPort` landed in RT4 (`runports.go:45-52`, adapter `:67-69`, doc says
byte-identical to the pre-port call sites). The hoist stays inside `package daemon`. The dedup constructor
stays inside `package daemon`.

**E4d-3: YES — and it needs operator sign-off before any code is written.**

### Ruling (A) — may an already-fenced *stateless leaf* grow a stateful DTO and a lifetime-owning entry point?

**What would be added** to `internal/transport/tunnel` (a new file `establish.go`; no new depguard block
— the `**/internal/transport/**` prefix rule at `.golangci.yml:335` already covers it):

```go
type Session struct {                 // exported DTO, no methods
    Worker         workers.Worker
    Runner         tmuxpkg.CommandRunner
    Cmd            *exec.Cmd
    WorkerHookSock string
}

func Prepare(ctx context.Context, s *Session, beadID, runID string) (port int, releasePort func())
func Start  (ctx context.Context, s *Session, port int, daemonHookSock, beadID, runID string) (teardown func())
```

**Why this is not "inventing a port."** No interface. No injected closure. No eighth entry in `RunPorts`.
The seams actually crossed — `lifecycle/tmux.CommandRunner` and `workers.Worker`/`Registry` — are
pre-existing and are already crossed at these exact lines today. Every field type is already inside
transport's depguard allow-list, so **no fence widens**. `internal/transport/tunnel/tunnel.go:15` already
names `remoteBeadCtx` in its own doc comment: this returns the type to the package that already documents
it.

**Why it still needs a ruling.** `internal/transport/tunnel` today holds **only** stateless free functions
and constants (11 exports). Growing it a per-run stateful DTO plus functions that own an `ssh -N -R`
process lifetime and hand back cleanup closures **changes what kind of package it is**.
`E5-dot-runloop.md:177-182` sets the precedent that *widening a port to its already-documented scope is in
charter; adding an eighth port is not* — but `tunnel` has no documented charter naming per-run state, so
the rule does not resolve it.

**The question, in one sentence:** *may an already-fenced leaf package own the lifetime of a per-run
process and hand cleanup closures back to daemon?*

**Do not be fooled by the "no-DTO fallback."** A plain-value variant —
`Start(...) (cmd *exec.Cmd, teardown func())` — dodges the struct but still hands a process lifetime out
of the leaf. **The charter question is lifetime ownership, not the struct.** If the answer to (A) is no,
E4d-3 has no shape and E4d closes at E4d-2; say so and re-park it rather than inventing a port.

### Ruling (B) — a gate-4 (runtime proof) waiver, requested explicitly rather than inherited

`_plan.md` §5.4 asks every unit with a run surface to *drive one remote bead end-to-end*. `PROGRESS.md` §5
already records this as an **unpaid debt from E4a and E4b**, deferred because the daemon is down. E4a/E4b
were licensed to ship without it because they were verified `is_pure_move: true`. **E4d-3 is not a pure
move** — it carries three declared deltas, all in process lifetime and defer ordering, which is exactly
the class `boot-config-REVIEW-seam.md` finding 1 rated HIGH before implementation.

**What the operator is being asked:** ship the first *non-pure-move* transport slice on unit tests plus a
static equivalence argument, with the remote path never once driven? Or hold E4d-3 until a live daemon can
drive one remote bead? Recommendation: **hold E4d-3 for the runtime proof if a daemon will be up within
the cycle; otherwise waive explicitly and name the bead that owns the deferred proof.** Never record a
§5.4 pass that was not run.

---

## 5. Prep slices

**Hard precondition for all of them:** `internal/runmerge` (E5 RT13) must be **committed** first. It is
staged-and-untracked in this tree with zero commits, it already imports into `workloop.go`, and
`README.md:111-114` forbids starting a unit against a half-landed tree. `workloop.go` took 331 commits in
90 days — single-writer, one slice at a time, land the same day it starts.

---

### E4d-0 — route the in-`beadRunOne` bead reopens through `LedgerPort`

**What.** Rewrite **all seven** `deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg,
runID, reopenTID, beadID, reason)` calls inside `beadRunOne` — at `workloop.go:3371`, `:3428`, `:3461`,
`:3491`, `:3661`, `:3712`, `:3843` — to `rp.Ledger.ReopenBead(ctx, runID, reopenTID, beadID, reason)`.
Leave `:6749` alone: it is an outer-loop adoption site with a different context (`bgCtx`) and no `rp`.

Convert all seven, not just the three near remote code — a partial conversion leaves sibling calls in the
same function using two different idioms with no discoverable reason.

**Why it is byte-identical.** `beadRunOne` takes `deps workLoopDeps` **by value** (`:3175`);
`rp := deps.runPorts()` at `:3184` binds `daemonLedger{deps: &localCopy}` via the pointer receiver
(`runports.go:76`); `daemonLedger.ReopenBead` dereferences `deps.intentLogDir` / `deps.brTimeoutCfg`
**at call time** from that same copy. `rp.Worktree` is reassigned at `:3790` but that is a different field
of a struct value and does not disturb `rp.Ledger`. Keep the `_ =` discard form at six sites and the
`if reopenErr :=` form at `:3843` verbatim; change no reason string.

**Also drains debt E5 RT16 would otherwise pay.**

```bash
go build ./internal/... ./cmd/... && go vet ./internal/... ./cmd/...
go vet -tags scenario ./internal/daemon/...
grep -c 'brAdapter\.ReopenBead' internal/daemon/workloop.go        # must be 1
git diff -U0 -- internal/daemon/workloop.go | grep -E '^[-+].*"' | grep -v 'brAdapter\|rp\.Ledger'
                                                                    # must be EMPTY (no string changed)
grep -c 'rbc != nil' internal/daemon/workloop.go                    # must still be 20
go test ./internal/daemon -run 'TestM4C7' -count=1
```
Plus the §6 differential run.

---

### E4d-1 — hoist `type remoteBeadCtx` to package scope  ← *retires the blocking finding*

**What.** Create `internal/daemon/remoterun.go`. Move `type remoteBeadCtx struct { … }`
(`workloop.go:3535-3540`) plus its rationale comment (`:3521-3534`) to package scope. **Name and all four
field names unchanged and still unexported.** `workloop.go` keeps `var rbc *remoteBeadCtx` (`:3541`) and
both `&remoteBeadCtx{…}` literals (`:3547`, `:3570`) verbatim.

**Verified free.** No `remoteBeadCtx` identifier anywhere else in `internal/`; all four field types
already imported at `workloop.go` package scope; `beadRunOne` is not generic; the type references no
`beadRunOne` local; no test constructs it. Zero execution-path change. This is socket-router SR-2a
(`9d7d56a3`) applied to one type instead of 25 method bodies.

**What it buys, precisely.** A name — not a move. The type is still in `package daemon`. But it retires
`README.md:103` and `E4-ssh.md:97`, and RT15 needs it named to put it in a signature.

```bash
go build ./internal/... ./cmd/... && go vet ./internal/... ./cmd/...
go vet -tags scenario ./internal/daemon/...
gofmt -l internal/daemon | grep -q . && echo FMT-FAIL
grep -c 'rbc != nil' internal/daemon/workloop.go                    # must still be 20
git diff --stat                                                     # 1 new file; workloop.go net ≈ −14
go test ./internal/daemon -run 'TestM4C7' -count=1
```

---

### E4d-2 — collapse the duplicated SSH runner literal

**What.** Add to `internal/daemon/remoterun.go`:

```go
// workerSSHRunner builds the per-run remote CommandRunner. hk-zexsj: ControlMaster=no /
// ControlPath=none make every SSH command an independent TCP connection — load-bearing,
// and the reason the hk-lt091 merge critical section at workloop.go:3792 exists.
//
// MUST return a tmuxpkg.SSHRunner VALUE. internal/transport/tunnel/tunnel.go:271 asserts the
// concrete value type (`r.(tmuxpkg.SSHRunner)`); a pointer or wrapper compiles, silently
// returns ok=false from SSHHostOpts, and builds the tunnel argv with no host opts.
func workerSSHRunner(host string) tmuxpkg.CommandRunner {
	return tmuxpkg.SSHRunner{Host: host, Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}}
}
```

Rewrite `workloop.go:3550` and `:3580` to call it. Move the hk-zexsj rationale to the constructor doc;
**leave the hk-lt091 prose at `:3797-3800` where it is** (it explains the merge critsec, not the runner).

**New test, required** — this is the one hazard in the whole plan that compiles clean and breaks remote
runs silently:

```go
func TestWorkerSSHRunner_IsConcreteSSHRunnerValue(t *testing.T) {
	host, opts, ok := tunnelpkg.SSHHostOpts(workerSSHRunner("box-b"))
	if !ok || host != "box-b" || len(opts) != 4 { t.Fatalf("SSHHostOpts lost the host: %q %v %v", host, opts, ok) }
}
```

```bash
go build ./internal/... ./cmd/... && go vet -tags scenario ./internal/daemon/...
grep -c 'tmuxpkg\.SSHRunner{' internal/daemon/workloop.go           # must be 0
grep -c 'tmuxpkg\.SSHRunner{' internal/daemon/remoterun.go          # must be 1
grep -c 'rbc != nil' internal/daemon/workloop.go                    # must still be 20
go test ./internal/daemon -run 'TestWorkerSSHRunner|TestM4C7' -count=1
```
> Do **not** use `grep -c 'ControlMaster=no' internal/daemon/workloop.go == 0` as the criterion — two of
> the four hits are prose in the hk-lt091 comment block and correctly stay.

---

### E4d-3 — lift establish + readiness gate into `internal/transport/tunnel`  🔒 **GATED ON RULINGS A + B**

**What moves** (from `workloop.go:3622-3716`, the `if rbc != nil` block) into a new
`internal/transport/tunnel/establish.go`:

- `Prepare`: `AllocatePort` (`:3625`) + `WorkerTCPEndpoint` (`:3632`) + the `ReleasePort` closure
  (`:3636`) + `EnsureWorkerHarmonikDir` (`:3639`) → ~16 lines.
- `Start`: `SSHHostOpts` + host fallback (`:3668`) + `BuildArgs` (`:3672`) + `ReverseTunnelRunner` +
  `.Start()` + nil-on-error (`:3673-3681`) + the `Kill`/`Wait` closure (`:3683-3688`) → ~25 lines.

**What stays in `workloop.go`, and why:**

- the whole `if rbc != nil` guard and every one of the 20 `rbc != nil` predicates (conformance floor);
- `lifecycle.ValidateSocketPathLength(daemonHookSock)` at `:3654` — **`internal/lifecycle` is not in
  transport's allow-list**, which is what forces the three-part `Prepare` → validate → `Start` shape and
  preserves the I/O-then-validate-then-spawn ordering;
- both failure arms verbatim: `EmitWorkerTunnelFailedEvent` + `tidGen.Next` + `rp.Ledger.ReopenBead`
  (post-E4d-0) with the identical reason string `"reverse-tunnel not ready: %v"` + the bare `return`;
- `WaitWorkerSocketLive` at `:3705` — the gate itself, because its failure action is a ledger write.

**Three declared logic deltas — every one must be named in the PR body or the pure-move review rejects:**

1. `defer tunnelpkg.ReleasePort(tunnelPort)` was registered **only** when `portErr == nil`;
   `defer releasePort()` becomes unconditional with `releasePort` a no-op on alloc failure. Verified no
   other defer registers between `:3636` and `:3688`, so LIFO order against the Kill+Wait defer and
   against `relLocalSlot` (`:3196`) / `relWorkerSlot` (`:3216`) / `runpkg.Remove` / `wtCleanup` is
   preserved **provided the caller defers the returned cleanup BEFORE branching on failure** — reversing
   those two statements leaks the reserved port on the socket-path-too-long path.
2. The teardown closure reads `s.Cmd` through the pointer **at fire time**, not by capture, so
   `rbc.tunnelCmd = nil` on start failure still disarms it identically.
3. Two `fmt.Fprintf(os.Stderr, "daemon: workloop: reverse-tunnel …")` format strings move to a non-daemon
   package **with the now-wrong `daemon:` prefix kept byte-verbatim**. Grep-verified: no test in
   `internal/` or `test/` asserts any of them. `c3fff27d` set this precedent explicitly ("keeping them is
   what makes this a defensible pure move"); the hygiene sweep is deferred.

**Preserved quirk — reproduce, do not repair.** On `AllocatePort` failure, `tunnelPort` stays 0,
`WorkerHookSock` stays `""`, no release is registered, and execution **continues** into `BuildArgs(0, …)`
and an `ssh` start that is guaranteed useless; the readiness gate then fails on the empty endpoint and the
bead reopens. That is shipped behavior. "Fixing" it changes which event the operator sees on port
exhaustion. It gets a dedicated pinning test.

**Do NOT** relocate `ReverseTunnelRunner` or `BuildArgs` out of `internal/transport/tunnel/tunnel.go`
while tidying — `conformance_m4c7_test.go:319-325` reads **that filename** and requires both literals in
it. Failure message: *"seam deleted: reverse-tunnel symbol … missing"*, pointing nowhere near the change.

**New tests, in `internal/transport/tunnel`:**
`TestPrepare_AllocFailureLeavesEmptyEndpointAndNoopRelease` (pins the fall-through quirk) ·
`TestPrepare_ReleaseIsIdempotent` · `TestStart_TeardownKillsThenWaits_AndIsNoopWhenStartFailed` (pins
delta 2, via a `RecordingRunner` and a stubbed `ReverseTunnelRunner`).

```bash
go list -deps ./internal/transport/... | grep 'gregberns/harmonik/internal/daemon' && echo BOUNDARY-FAIL
.tools/golangci-lint run ./internal/transport/... ./internal/daemon/...
scripts/transport-freeze-gate.sh
grep -c 'rbc != nil' internal/daemon/workloop.go                    # must still be 20
go test ./internal/transport/... -count=1
go test ./internal/daemon -run 'TestM4C7' -count=1
```
Plus the full §6 gate. **Post-change the `if rbc != nil` block is ~52 lines, not ~33** — both failure arms
and their comment mass stay.

---

## 6. Verification gate

Run from `/Users/gb/github/harmonik`. This is `README.md` §4 with the E4d-specific additions.

```bash
# ── 0. PRECONDITIONS ─────────────────────────────────────────────────────────
git status --porcelain internal/runmerge          # MUST be empty — RT13 committed
git status --porcelain internal/daemon/workloop.go # MUST be empty — you are the single writer
df -h .                                            # MUST show > 10 GiB free (see below)

# ── 1. BEFORE: differential baseline, IDENTICAL SCOPE, serialized ────────────
go test ./internal/daemon/... ./internal/transport/... -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > /tmp/p2-e4d-before-failures.txt

# ── 2. BUILD / VET / BOUNDARY ────────────────────────────────────────────────
go build ./internal/... ./cmd/...                  # NOT ./... — plans/ has an untagged main
go vet   ./internal/... ./cmd/...
go vet -tags scenario        ./internal/daemon/...
go vet -tags specaudit       ./internal/specaudit/...
go vet -tags e2e_real_claude ./internal/daemon/...
go list -deps ./internal/transport/... | grep 'gregberns/harmonik/internal/daemon' \
  && { echo "BOUNDARY FAIL"; exit 1; } || echo "BOUNDARY OK"
.tools/golangci-lint run ./internal/transport/... ./internal/daemon/...   # FULL run, not --new-from-rev

# ── 3. E4d-SPECIFIC STATIC FLOORS (invisible to go build) ────────────────────
grep -c 'rbc != nil' internal/daemon/workloop.go   # MUST be 20 (floor 8, conformance_m4c7_test.go:346)
go test ./internal/daemon -run 'TestM4C7' -count=1 # D2 chokepoint + seam-survival + tunnel.go path pin

# ── 4. AFTER: differential green ─────────────────────────────────────────────
go test ./internal/daemon/... ./internal/transport/... -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > /tmp/p2-e4d-after-failures.txt
comm -13 /tmp/p2-e4d-before-failures.txt /tmp/p2-e4d-after-failures.txt   # MUST be empty
make specaudit-lint && make test-scenario && make fmt-check

# ── 5. BUG SCAN + FREEZE ─────────────────────────────────────────────────────
ubs $(git diff --name-only HEAD | grep '\.go$')
scripts/transport-freeze-gate.sh                   # E4d-3 only
```

### Traps from `00-test-oracle-baseline.md` that apply directly here

- **Disk watermark (Addendum §3).** The daemon pauses dispatch below 10 GiB free
  (`disk-check: available=… watermark=10240MiB`). Below it, dispatch/throughput/timing tests fail for
  reasons unrelated to any code change. **Check `df -h` before trusting a differential run.**
- **Identical package scope (PROGRESS.md:80).** Before and after runs must use the **same** package
  scope, back-to-back. A recorded run comparing `./internal/...` (123 failures) against
  `./internal/daemon/` (20) was uninterpretable: the extra packages generate temp-dir churn that drives
  the very disk metric the watermark gates on. Scope is a hidden independent variable on this repo. Use
  `./internal/daemon/... ./internal/transport/...` for **both** sides of every E4d slice.
- **Never test the shared working tree.** `go test` compiles other agents' uncommitted files. Run the
  differential suite against a clean detached worktree at your own HEAD.
- **Confirm flakes with the right procedure.** The five load-sensitive flakes are confirmed by re-running
  **in isolation** (isolated pass = load artifact). `TestMergeToMain_RealConflictWithBeadsLedger_Escalates`
  is the inverse — it passes in the full suite and fails in isolation; confirm it **in the full suite**.
  Applying the wrong procedure inverts the answer.
- **`internal/daemon` is already red at HEAD.** The gate is *no NEW failures*, never zero.
  `TestThroughput_TenBeadsAtMaxFour` is a hard pre-existing failure and is out of P2 scope — do not "fix"
  it inside an extraction commit.

---

## 7. Interaction with E5

**Correction to both plan files first.** E4-ssh.md says E4d *"depends on E5's run-machine lift"*;
E5-dot-runloop.md:10 lists E4 as E5's own prerequisite. This reads circular and is **not** — the E4 that
E5 means is E4a/E4b (`20dd8`/`222f`, both landed). More importantly:

> **E5's stated prerequisites are ALL SATISFIED TODAY.** E1a `5f7762c3`, E1b `db2f4752`, E1c `66e0041d`,
> E4a `20dd8`, E4b `222f` — all landed. The claim *"E5 is not staffable yet"* (`README.md:45`) is stale
> and must be corrected. **RT15 is unblocked now.** So E4d-vs-E5 is a live contention the operator
> arbitrates, not a free window.

**Where they collide — totally.** RT15 re-signatures `beadRunOne` to
`(ctx, env RunEnv, ports RunPorts, shared SharedHandles)`; RT20 lifts `workloop.go:3167-5397` wholesale to
`internal/runloop/run.go`. That range **strictly contains** every line E4d touches. RT18 additionally
drops `deps workLoopDeps` from `runReviewLoop` and `driveDotWorkflow`. Anything E4d leaves in that region
is rewritten by RT15.

**RT13 is in flight right now, not a future step.** `internal/runmerge/` exists with 7 non-test source
files, is imported by `workloop.go` and `sessioncontext_chb023.go`, is staged/untracked with **zero
commits**, and `workloop.go` is already down to 6,854 lines. Its cut is line-disjoint from E4d
(`:6397-8011` vs `:3521-4904`) but that does **not** license concurrency: `workloop.go` is strict
single-writer at 331 commits/90d.

### Ordering

1. **RT13 commits.** Hard gate. Nothing in E4d starts before this.
2. **E4d-0, E4d-1, E4d-2** — three cheap daemon-internal slices, ideally same day, one at a time. They
   shrink RT15's job: RT15 must name this type to put it in a signature, and E4d-1 does it for free.
3. **Operator rulings A + B.**
4. **E4d-3** *before* RT15/RT20 if it is done at all. It rebases against RT15 over ~250 lines whose
   measured churn is 10–11 commits/90d, versus 34 for the consumption region at `:4290-4440` that E4d
   deliberately leaves alone — E4d rebases the **cold third of a hot function**. And it pays down the
   `internal/runloop` allow-list gap RT20 would otherwise hit (§3, point 2).
5. **RT14 note:** RT14 deletes `agentready.go`. E4d does not touch it (the ready-timeout is explicitly out
   of scope, §3), so there is no conflict — but whoever plans RT14 should know E4d deliberately left
   `effectiveAgentReadyTimeout(…, rbc != nil)` at `:4904` alone.

**What breaks if they run concurrently.** Merge-conflict warfare in the single hottest file in the tree,
and a broken conformance floor that no one can attribute: if RT15's rewrite and an E4d slice both touch
`rbc` predicates, the first `go test` failure says *"dual-path collapsed"* and points at neither change.
**One crew. One slice. Landed the same day it starts.**

### Two stale rows to fix while here

- `E4-ssh.md` §3a lists `tidGen.Next` as a blocker. It is not: `workloop.go:274` types it
  `*core.TransitionIDGenerator`, an `internal/core` type available to any package whose allow-list
  includes core. **Downgrade that row.**
- `E5-dot-runloop.md` §1b still lists `internal/daemon/codesync_rs_b8.go` (258 LOC) as a future mover.
  E4b already took it (`222ff36f`); the file no longer exists. **Drop the row.**

---

## 8. Risks

**R1 — Defer lifetime. The one that breaks production, not tests. (E4d-3, HIGH.)** Four defers register
on `beadRunOne`'s 2,220-line frame from inside remote blocks. A callee that registers them fires them at
*its* return, tearing down the SSH tunnel before the agent ever launches — on a path no unit test
exercises because the daemon is down. `boot-config-REVIEW-seam.md` finding 1 rates this class HIGH,
"boot-breaking, not a nit." Mitigation: return cleanup closures, defer them at the original positions,
defer **before** branching on failure, and put a per-defer disposition table in the PR body. *A variant of
this plan that omits that table should be rejected at review.*

**R2 — No behavior-identity oracle for the remote path. (E4d-3, HIGH, unmitigable in-tree.)**
`_plan.md` §5.4's "drive one remote bead" has never been run for any transport slice; `PROGRESS.md` §5
records it as unpaid debt from E4a/E4b. socket-router's design is explicit that inherited suites are not
an identity proof ("byte-BLIND"). E4a/E4b were licensed by `is_pure_move: true`; E4d-3 is not. This is
operator ruling (B).

**R3 — The silent SSHRunner type trap. (E4d-2, MEDIUM, fully mitigated.)**
`internal/transport/tunnel/tunnel.go:271` asserts the **concrete value type** `tmuxpkg.SSHRunner`. A dedup
constructor returning a pointer or wrapper compiles, disables `SSHHostOpts`, and builds the tunnel argv
with no host opts. Mitigated by the required test in E4d-2. This hazard was invisible to two of the three
designs.

**R4 — Static source-text audits that fail far from the change. (all slices, MEDIUM.)** Two floors in
`conformance_m4c7_test.go` pin `workloop.go` by path (the `rbc != nil` count and the D2 guard proximity),
and one pins `internal/transport/tunnel/tunnel.go` by filename. None is a compile error; all three produce
messages that point nowhere near the edit. Every slice keeps the count at 20 and never touches the D2
guard at `:4379`. **RT20 will break both `workloop.go` audits when `beadRunOne` leaves the file** — E4a
already did the equivalent re-point for a different audit, so there is precedent, but it must be a
numbered step in RT20 or the lift lands red.

**R5 — Merge conflict against the hottest file. (E4d-1, MEDIUM, scheduling not code.)** E4d-1's diff is
14 lines, but it lands in `workloop.go` while RT13 is uncommitted and RT15 is unblocked. Mitigation is
sequencing (§7), not code.

**R6 — Payoff smaller than "E4d" implies. (all, disclosed.)** ~30–35 executable lines leave
`internal/daemon`, ~100 counting comment mass, against 619 for E4a+E4b. E4d-0/1/2 move **zero**. Anyone
reading "E4d landed" should not expect a shrink-ledger entry comparable to E4a. The case is the P3 API and
draining E5's remote coupling, and it must be argued on that basis or not at all.

**R7 — Scope creep back toward the rejected shapes. (process.)** The two dead ends both looked reasonable
at design time: a "remote policy object" (§2.3) annexes worker selection + handler-contract timeout policy
+ dispatch capacity into a transport leaf, and "just fold it into E5" (§2.2) contradicts itself at RT20
and silently unifies `rbc != nil` with `runner != nil`. If a future implementer finds themselves widening
E4d's scope toward either, re-read §2 before writing code.
