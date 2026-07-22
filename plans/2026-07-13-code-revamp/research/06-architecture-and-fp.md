# Dossier 06 — Current Architecture & FP/Typed Discipline (factual)

> Concrete inventory for the code-revamp plan. What EXISTS today: package mass, the
> depguard layering matrix, the queue "island", the rpc.go two-writer path, the FP
> patterns already in the tree, and the enforcement mechanisms the repo config actually
> uses. No recommendations — inventory only.
> Collected 2026-07-13. All file:line refs are against the working tree at that time.

---

## 1. Package inventory — where the mass is

`find internal -maxdepth 1 -type d` returns 45 top-level packages. LOC per package
(`wc -l` over `*.go`, split non-test vs `*_test.go`, recursive so subpackages roll up).
Ranked by non-test LOC:

| Rank | Package | non-test LOC | test LOC |
|---|---|---:|---:|
| 1 | **daemon** | **55,611** | 145,057 |
| 2 | **core** | **31,191** | 72,947 |
| 3 | lifecycle | 8,676 | 31,415 |
| 4 | workspace | 6,465 | 21,951 |
| 5 | **queue** | 6,098 | 9,601 |
| 6 | keeper | 5,975 | 20,126 |
| 7 | scenario | 4,792 | 17,298 |
| 8 | handlercontract | 4,531 | 12,076 |
| 9 | brcli | 3,750 | 12,273 |
| 10 | workflow | 3,639 | 14,139 |
| 11 | handler | 3,206 | 6,440 |
| 12 | eventbus | 2,174 | 3,623 |
| 13 | workers | 2,139 | 2,712 |
| 14 | digest | 1,851 | 2,330 |
| 15 | sentinel | 1,724 | 4,034 |
| 16 | supervise | 1,379 | 1,289 |
| 17 | codexwire | 1,339 | 254 |
| 18 | schedule | 1,134 | 755 |
| 19 | workflowvalidator | 1,104 | 2,856 |
| 20 | operatornfr | 1,081 | 13,598 |
| — | (25 smaller pkgs) | < 1,100 each | — |
| — | testhelpers | 1,070 | 1,127 |
| — | specaudit | 9 | 37,646 |

Full tail (non-test LOC): agentmanifest 881, usage 785, hooksystem 725, watch 701,
sessiondata 647, hookrelay 608, presence 577, structuredlog 485, cognition 474,
release 469, codexreactor 360, scratchpad 313, branching 282, codexdigitaltwin 277,
crew 268, dashboard 229, agentlaunch 189, apptap 145, goalstate 140, run 128,
specaudit 9, t6probe/t5probe/codextest 0 (test-only).

**The mass is bimodal.** `internal/daemon` (55.6k) + `internal/core` (31.2k) = **~57% of
non-test code lives in two packages.** Everything else is a long tail. `internal/daemon`
alone is ~9x the next-largest non-daemon subsystem (lifecycle, 8.7k).

Within `internal/daemon` the top-level directory holds **115 non-test `.go` files**
(54,109 LOC before subpackages). The largest single files:

```
8184  internal/daemon/workloop.go     <- one file, larger than any whole subsystem except lifecycle/workspace/queue/keeper
2735  internal/daemon/tmuxsubstrate.go
2633  internal/daemon/pasteinject.go
2625  internal/daemon/dot_cascade.go
2346  internal/daemon/daemon.go
2205  internal/daemon/reviewloop.go
2027  internal/daemon/projectconfig.go
1189  internal/daemon/stalewatch.go
1049  internal/daemon/orphansweep.go
1024  internal/daemon/quiesce.go
```

`workloop.go` (8,184 lines) is the single densest artifact in the tree.

---

## 2. Dependency structure — depguard is the layering enforcement

Go forbids compile-time import cycles, so there are **no cycles today** (the code
compiles). The interesting enforcement is the depguard **component matrix** in
`.golangci.yml`, which encodes the *intended* DAG of allowed edges. depguard is enabled
at `.golangci.yml:40` (`- depguard  # import-graph + component-layer rules`).

### 2.1 Verbatim depguard block (`.golangci.yml:64`–`390`)

The matrix has **two global bans** (apply to every file) and **~20 component rules**
(each scoped by a `files:` glob with `allow:`/`deny:` edge lists). Quoted in full:

**Global ban — Beads direct-SQL access** (`.golangci.yml:79`):
```yaml
beads-direct-access-ban:
  files:
    - "**/*.go"
    - "!**/internal/brcli/**"
    - "!**/internal/testhelpers/**"
  deny:
    - { pkg: "database/sql", desc: "BI-002: Beads access must route through internal/brcli; direct SQL access is forbidden (specs/beads-integration.md §4.2)" }
    - { pkg: "github.com/mattn/go-sqlite3", desc: "BI-002: ..." }
    - { pkg: "modernc.org/sqlite", desc: "BI-002: ..." }
    - { pkg: "crawshaw.io/sqlite", desc: "BI-002: ..." }
    - { pkg: "zombiezen.com/go/sqlite", desc: "BI-002: ..." }
```

**Global ban — no LLM SDK in the daemon closure** (`.golangci.yml:96`):
```yaml
llm-sdk-ban:
  files: ["**/*.go"]
  deny:
    - { pkg: "github.com/anthropics/", desc: "PL-INV-002: daemon must not import any LLM SDK" }
    - { pkg: "github.com/openai/", desc: "PL-INV-002: daemon must not import any LLM SDK" }
    - { pkg: "github.com/sashabaranov/go-openai", desc: "PL-INV-002: ..." }
    - { pkg: "github.com/liushuangls/go-anthropic", desc: "PL-INV-002: ..." }
```

**Component rules** (each `files:`-scoped; edges quoted):

```yaml
crew:              # internal/crew  — leaf durable-state
  files: ["**/internal/crew/**"]
  allow: [$gostd, internal/core, internal/crew]
  deny:  [internal/daemon "crew MUST NOT import daemon (c2-spec.md §3.3)"]

keeper:            # internal/keeper
  files: ["**/internal/keeper/**"]
  allow: [$gostd, internal/core, internal/eventbus, internal/presence,
          internal/keeper, internal/dashboard, internal/digest]
  deny:  [internal/daemon, internal/workloop]   # MUST NOT import daemon/workloop

core:              # THE LEAF
  files: ["**/internal/core/**"]
  allow: [$gostd, github.com/google/uuid]
  deny:  [internal/  "core is a leaf; no subsystem imports"]

queue:
  files: ["**/internal/queue/**"]
  allow: [$gostd, internal/core]               # <-- core ONLY (see note 2.4)

schedule:
  files: ["**/internal/schedule/**"]
  allow: [$gostd, internal/schedule]
  deny:  [internal/daemon]

agentmanifest:
  files: ["**/internal/agentmanifest/**"]
  allow: [$gostd, gopkg.in/yaml.v3, internal/agentmanifest]
  deny:  [internal/daemon]

handler-brcli-ban:  # BI-004: handler subsystem must not import brcli
  files: ["**/internal/handler/**", "**/internal/handlercontract/**"]
  deny:  [internal/brcli]

lifecycle-tmux:
  files: ["**/internal/lifecycle/tmux/**"]
  allow: [$gostd, internal/core, internal/lifecycle]

eventbus:
  files: ["**/internal/eventbus/**"]
  allow: [$gostd, internal/core, internal/eventbus, github.com/google/uuid]

policy:            # (declared; package does not exist yet)
  files: ["**/internal/policy/**"]
  allow: [$gostd, internal/core]

presence:
  files: ["**/internal/presence/**"]
  allow: [$gostd, internal/core, internal/eventbus, internal/presence]
  deny:  [internal/daemon]

handler-contract:
  files: ["**/internal/handlercontract/**"]
  allow: [$gostd, internal/core, internal/handlercontract, github.com/google/uuid]

handler-contract-lifecycle:
  files: ["**/internal/handlercontract/lifecycle/**"]
  allow: [$gostd, internal/handlercontract/lifecycle]

adapter-br:        # (declared; internal/adapter/br does not exist — brcli is the real one)
  files: ["**/internal/adapter/br/**"]
  allow: [$gostd, internal/core]
adapter-ntm:
  files: ["**/internal/adapter/ntm/**"]
  allow: [$gostd, internal/core]

workspace:
  files: ["**/internal/workspace/**"]
  allow: [$gostd, internal/core, internal/eventbus, internal/adapter/br,
          internal/handlercontract, github.com/google/uuid, internal/lifecycle/tmux]

agentrunner:       # (declared; package does not exist yet)
  files: ["**/internal/agentrunner/**"]
  allow: [$gostd, internal/core, internal/eventbus, internal/handlercontract, internal/adapter/ntm]

hook:              # (declared; package does not exist — real one is hooksystem/hookrelay)
  files: ["**/internal/hook/**"]
  allow: [$gostd, internal/core, internal/eventbus]

memory:            # (declared; package does not exist yet)
  files: ["**/internal/memory/**"]
  allow: [$gostd, internal/core, internal/eventbus]

handler-impls:     # internal/handler/{claudecode,pi,twin} — none exist yet (handler code is flat)
  files: ["**/internal/handler/claudecode/**", ".../pi/**", ".../twin/**"]
  allow: [$gostd, internal/core, internal/handlercontract]

# scenario:  COMMENTED OUT / DEFERRED — 12 real violations (scenario imports
#            daemon, queue, lifecycle, workspace directly). Follow-up hk-uyxg0.
# improvement / orchestrator:  COMMENTED OUT — packages do not exist yet.

daemon:            # COMPOSITION ROOT — allow-all
  files: ["**/internal/daemon/**"]
  allow: [$gostd, "go/", "github.com/gregberns/harmonik/internal/",
          github.com/google/uuid, gopkg.in/yaml.v3,
          github.com/stretchr/testify/, go.uber.org/goleak]
  # NO deny list.

cmd:               # COMPOSITION ROOT — allow-all internal/
  files: ["**/cmd/**"]
  allow: [$gostd, "github.com/gregberns/harmonik/internal/",
          "github.com/gregberns/harmonik/cmd/", github.com/google/uuid, gopkg.in/yaml.v3]
```

### 2.2 Packages with NO boundary rule (unconstrained)

Of the 45 real `internal/` packages, only ~13 have a depguard rule (core, queue, crew,
keeper, schedule, agentmanifest, presence, eventbus, workspace, handler*, lifecycle/tmux,
plus the two global bans). The rest have **no component rule at all** and may import any
internal package the global bans don't forbid. That set includes most of the mass:
**lifecycle, brcli, workflow, workers, digest, sentinel, supervise, codexwire,
workflowvalidator, operatornfr, usage, hooksystem, hookrelay, watch, sessiondata,
structuredlog, cognition, release, codexreactor, scratchpad, branching,
codexdigitaltwin, dashboard, agentlaunch, apptap, goalstate, run, scenario**.

**`internal/daemon` is explicitly the god-package.** Its rule (`.golangci.yml:368`) is
`allow: ["github.com/gregberns/harmonik/internal/"]` with **no `deny`** — it may import
every subsystem, and nothing constrains what logic accumulates inside it. The layering
matrix protects the *leaves* from importing daemon; it does nothing to stop daemon from
absorbing responsibility (workloop.go, tmuxsubstrate.go, reviewloop.go, dot_cascade.go,
etc. are all business logic living in the composition root).

### 2.3 The matrix describes a partly-imaginary tree

Several rules target paths that **do not exist**: `internal/policy`,
`internal/agentrunner`, `internal/hook`, `internal/memory`, `internal/adapter/br`,
`internal/adapter/ntm`, `internal/handler/{claudecode,pi,twin}`,
`internal/orchestrator`, `internal/improvement`. These are the *intended* subsystem names
from `subsystem-organization.md` (§7). The real tree diverged: handler logic is flat in
`internal/handler/`, the br adapter is `internal/brcli` (not `internal/adapter/br`), and
the orchestration logic that was meant for `internal/orchestrator` lives in
`internal/daemon`. Rules against non-existent paths are inert (zero files matched, zero
enforcement).

### 2.4 Latent gap — queue rule vs actual import

The `queue` rule (`.golangci.yml:151`) allows only `$gostd` + `internal/core`. But
`internal/queue/rpc.go:32` imports `github.com/google/uuid` (used at rpc.go:223,
`uuid.NewV7()`). With a depguard allow-list, anything not listed is denied — so uuid in
queue is an apparent rule/code mismatch (either an uncaught violation or lint is not
gating this path). Flagging as observed; not verified against a live `golangci-lint run`.

---

## 3. The queue island — why it's good

The census calls `internal/queue` "the one mutex-free, spec-pinned, well-tested island."
Concretely:

**(a) Mutex-free / no shared mutable state.** `grep` for `sync.Mutex|sync.RWMutex|Lock()`
over the queue package's non-test files returns **nothing**. Contrast: `internal/daemon`
has **47** `sync.Mutex`/`sync.RWMutex` declarations. The queue package holds no
long-lived mutable state; concurrency control (the `queueMu sync.RWMutex`) lives in the
daemon's `QueueStore` (`internal/daemon/queuestore_hkj808w.go:69`), not in queue.

**(b) Pure-function handlers.** The design is stated at the top of
`internal/queue/rpc.go:5`:
```go
// Each handler is a pure function: it receives the parsed request and the
// in-memory queue state, runs the appropriate pipeline, and returns a typed
// response plus an optional *RPCError. The daemon's socket dispatcher (in
// internal/daemon/socket.go) owns I/O, context propagation, and event emission.
```
This is functional-core / imperative-shell by convention: queue computes, daemon does
I/O and side effects. `AppendItems` (append.go), `Validate` (validation.go), the
`Handle*` functions all take state in and return `(newState, events, error)`.

**(c) Ports as narrow interfaces, defined at the consumer.** Queue does not import
daemon; instead it declares the *minimal* interfaces it needs and the daemon's types
satisfy them structurally (Go structural typing = dependency inversion for free). The
four queue interfaces:
```go
// rpc.go:56 — the write side of the daemon's QueueStore
type QueueSetter interface {
    SetQueue(q *Queue)
    ClearQueueByName(name string)
}
// rpc.go:66 — minimal bus port; matches handlercontract.EventEmitter
type EventEmitter interface {
    Emit(ctx context.Context, eventType core.EventType, payload []byte) error
}
// validation.go:144 — the Beads read port
type BeadLedger interface { ... }        // e.g. BlocksEdge(ctx, a, b) (bool, error)
// validation.go:170
type HandlerPauseChecker interface { ... }
```
The comment at rpc.go:38 names the reason explicitly: *"minimal interface so
HandlerAdapter can update the daemon's in-memory QueueStore without importing
internal/daemon (cycle prevention)."* This is textbook ports-and-adapters: the port is
owned by the core, the adapter (daemon) implements it.

**(d) Spec-pinning.** doc.go points at the normative spec
(`internal/queue/doc.go:2`: *"It is defined normatively in [specs/queue-model.md]"*).
Production code carries **41 distinct QM-/EM- spec-rule citations** (e.g. `QM-050`,
`QM-063`, `QM-029b`, `EM-065`). The depguard rule itself is pinned to the spec
(`.golangci.yml:149` → `specs/queue-model.md §1`).

**(e) Test discipline.** 23 `_test.go` files against 11 production files;
**11 test files are named after the exact bead/spec-rule they pin**, e.g.
`validation_em065_hkxizhl_test.go`, `queue_cancel_hk0mmy4_test.go`,
`validation_qm025_false_defer_hkgf59k_test.go`, `set_concurrency_spawncap_hkvfeeo_test.go`.
The test name encodes the regression it guards.

**Contrast with `internal/daemon` (the swamp).** Same structural axes, opposite scores:
- Mutable state: 47 mutexes; a `QueueStore` with `queueMu sync.RWMutex` and a
  `LockedQueueStore` locking protocol (QM-064 read-then-write serialisation).
- Purity: workloop.go interleaves I/O, event emission, subprocess spawning, tmux, and
  state mutation in one 8,184-line file.
- Interfaces: 63 interface declarations, but many are internal glue rather than
  consumer-defined ports.
- Boundary rule: allow-all, no `deny` — the layering matrix imposes no ceiling on it.
- Result: one package is a small pure kernel with a spec and named regression tests; the
  other is the composition root that grew into the application.

---

## 4. rpc.go two-writer / lost-update path (~:1016)

The two writers to `.harmonik/.../queue.json` are the **queue-RPC handler** and the
**daemon workloop**, and they do NOT share a lock.

**Writer B — the append RPC handler** (`internal/queue/rpc.go`, `HandleQueueAppend`
adapter at :993, backed by pure `HandleQueueAppend` at rpc.go:342). It:
1. Loads the queue **fresh from disk**, bypassing the locked store:
   `loadedQ, loadErr := Load(ctx, projectDir, ...)` (rpc.go:356 / rpc.go:374).
2. Mutates it in memory — `AppendItems` appends in place:
   `g := &q.Groups[groupIndex]; g.Items = append(g.Items, newItems...)` (append.go:166).
3. Persists and then pushes the whole envelope into the in-memory store:

```go
// internal/queue/rpc.go:1006
// Persist the mutated queue (QM-063: persist before emit) and update the
// in-memory QueueStore so the workloop sees the appended items (hk-lzs8r).
if mutated != nil {
    if persistErr := Persist(ctx, a.projectDir, mutated); persistErr != nil {
        return nil, &RPCError{
            Code: -32099, Message: "internal_error",
            Detail: map[string]any{"error": fmt.Sprintf("persist queue after append: %v", persistErr)},
        }
    }
    if a.qs != nil {
        a.qs.SetQueue(mutated)      // <-- rpc.go:1016  clobbers the store snapshot
    }
}
```

Crucially, `grep` for locking in the entire queue RPC path returns **"NO LOCKING in
queue/rpc.go"** — the append handler never acquires `queueMu`.

**Writer A — the daemon workloop** (`internal/daemon/workloop.go`, 8,184 lines). It holds
`queueMu` via `LockForMutation`/`LockedQueueStore`, mutates its own in-memory `liveQ` as
items dispatch/complete, and persists through the same `queue.Persist`:
```go
// internal/daemon/workloop.go:2526 (one of several sites; also :2150, :2424)
lq.LockedSetQueueByName(snapQueueName, liveQ)
if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil { ... }
// and workloop.go:6170
lq.SetQueue(q)
```

**The lost update.** Writer B reads queue.json at rpc.go:356, does validation + append
(which involves ledger round-trips — `ledger.BlocksEdge`, append.go:136 — so a
non-trivial window), then persists its `mutated` copy and calls `SetQueue(mutated)`
(rpc.go:1016). If Writer A (holding `queueMu`) mutated dispatch state and persisted during
B's window, B's `Persist(mutated)` overwrites A's fresh statuses on disk, and
`SetQueue(mutated)` overwrites A's live in-memory queue in the store — even though B never
held the lock A uses. The single-writer discipline (QM-060, cited at rpc.go:54;
read-then-write serialisation QM-064 at queuestore_hkj808w.go:254) is enforced *inside*
the `LockedQueueStore` but the queue-RPC path enters through `Load()` + `Persist()` +
`SetQueue()` **outside** that lock. Two writers, one file, no shared mutex → last-write-wins.

---

## 5. Existing FP / typed patterns — where the code already does this, and where not

**No monadic types.** `grep` for `type Result|Option|Either|Maybe` and `Result[`/
`Option[` over non-test code finds **zero** functional-error containers. `internal/brcli/
adapter.go:110` has a `Result` struct but it is a subprocess-result record, not a monad.
Go generics are barely used: only **2** generic (type-parameterised) functions exist in
the whole `internal/` tree. The error idiom is uniformly Go-native `(T, error)` tuples
with `%w` wrapping (errorlint enforced, §6).

**Pure-core / imperative-shell — done well, by CONVENTION not by type.** The discipline is
real and documented in comments, concentrated in the leaf/near-leaf packages:
- `internal/core` — the verdict/policy state machine is a set of pure functions:
  `CheckVerdictRetryCap` (verdictretrycap_rc026a.go:19, *"pure function mapping an
  existing record and a cap"*), `PolicyRequiresConfirmation` / `ApplyVetoPromotion`
  (verdictoverride_rc027.go:28), `DiscoverVerdictExecution`,
  `SynthesizeBudgetExhaustionFallbackVerdict` — each documented "pure function", each with
  a matching `_test.go`.
- `internal/queue` — the whole package (§3), "Each handler is a pure function"
  (rpc.go:5).
- `internal/schedule` — `Decide` is "a pure function of the [clock]" (clock.go:241).
- `internal/handler` — `classify_hc023.go:9` classifier is "a pure function over typed
  inputs; the result is one of the five [outcomes]"; claudehandler_chb006_024.go:23
  factors "the handler-process responsibilities … as a set of pure functions" wrapped by
  one orchestrating shell function.
- `internal/release/ledger.go:3` — "pure functions that implement ledger state
  transitions."

**Ports-and-adapters — present, consumer-defined interfaces.** The strongest examples are
queue's `QueueSetter`/`EventEmitter`/`BeadLedger` (§3c) and `internal/core`'s 7
interfaces. `internal/eventbus` (8 interfaces) and `internal/handlercontract` (6
interfaces, incl. the `Handler` contract + `EventEmitter` that queue's port matches
structurally) are the shared-contract packages.

**Where it is NOT done — `internal/daemon`.** 63 interfaces but the package is the
imperative shell that never got thin: workloop.go (8.2k) mixes pure decisions with tmux
subprocess I/O, event emission, and mutation. The functional core is diffused into the
shell rather than extracted. Interface count is high but many are internal seams, not a
ports boundary. This is the structural inverse of the queue island: the leaf packages
achieved pure-core-by-convention; the composition root did not, and there is no
depguard/lint ceiling forcing it to.

**Summary:** the codebase already practices functional-core/imperative-shell and
consumer-owned ports **as a coding convention in its leaf packages** (core, queue,
schedule, handler classifiers, release), enforced socially via "// pure function"
comments + spec-pinned tests. It does NOT use typed FP containers (no Result/Option/
Either, almost no generics). The convention breaks down exactly where the layering matrix
stops constraining: `internal/daemon`.

---

## 6. Enforcement mechanisms — what EXISTS in the repo

Inventory of what `.golangci.yml` + Go tooling currently provide (no proposals):

**(a) depguard — layering / no-cross-boundary-imports.** The primary structural
enforcement. Component matrix quoted in §2. Enforces: leaf-purity (core imports nothing
internal), per-subsystem allow/deny edges, two global bans (no direct Beads SQL; no LLM
SDK in the daemon closure). Gaps: ~32 of 45 packages unconstrained; daemon/cmd allow-all;
several rules target non-existent paths (§2.2, §2.3).

**(b) Interface-based ports.** Go structural typing means consumer-defined interfaces
(queue's `QueueSetter` etc.) invert dependencies with no framework. Used well in leaves;
diffuse in daemon.

**(c) `go vet` + govet linter.** Enabled (`.golangci.yml:10`, `- govet`). Stdlib
correctness analyzers.

**(d) Correctness/idiom linter suite** (`.golangci.yml:8`–39, `default: none` then
explicit enable): errcheck (with `check-type-assertions: true`, `check-blank: true`,
line 56), staticcheck, ineffassign, unused, errorlint (`%w` + errors.Is/As), nilerr,
copyloopvar, testifylint (enable-all, line 394), errchkjson, **exhaustive** (missing enum
cases in switch; `default-signifies-exhaustive: true`, line 391 — a real typed-discipline
gate), bodyclose, rowserrcheck, sqlclosecheck, contextcheck, noctx, containedctx,
fatcontext, gocritic, revive (rules: exported, package-comments, var-naming,
error-return, error-naming, if-return — line 57), misspell, unparam, unconvert, prealloc,
nakedret (naked returns > 20 lines), nolintlint (`require-explanation: true`,
`require-specific: true`, line 59), gosec, **forbidigo** (bans `fmt.Print*` → "use the
structured logger", and bans `panic` → "return an error; panics only in main/init",
line 60).

**(e) Cyclomatic-complexity linter — NOT configured.** `grep -iE
'cyclo|cyclop|funlen|complexity|gocognit'` over `.golangci.yml` returns **NONE FOUND**.
There is **no gocyclo, no cyclop, no funlen, no gocognit** in the enabled set. Therefore
the operator's question — "does workloop.go violate it and is it suppressed?" — resolves
to: **there is no complexity threshold to violate.** workloop.go (8,184 lines) is not
flagged by any complexity gate because none is enabled; nothing is suppressed for
complexity. (For reference, `internal/daemon` carries 202 `//nolint` directives total,
but the two in workloop.go are for `errcheck` and `gosec:G306` — not complexity.)

**(f) Test-coverage gate — NOT configured in `.golangci.yml`.** No coverage threshold in
the lint config. `issues: { max-issues-per-linter: 0, max-same-issues: 0 }` (line 395)
means "report everything" but is not a coverage gate. (Coverage discipline, if any, is
social/CI-side, not encoded here.)

**(g) `internal/` visibility.** Standard Go: nothing outside the module can import
`internal/*`. This is the coarse boundary the depguard matrix refines (§2, and
subsystem-organization.md:91 explicitly notes `internal/` alone is insufficient).

**(h) Structural runtime guards (belt-and-suspenders, in test code).** Some boundary
rules are backed by a structural `_test.go` in addition to depguard — e.g. BI-004's
`internal/handler/bi004_handler_gate_test.go` (referenced at .golangci.yml:190) and the
depguard fixture `testdata/depguard/beads_direct_access_fixture.go` (`//go:build ignore`,
referenced at :76) that verifies the ban actually fires.

---

## 7. subsystem-organization.md — intended layout vs reality

Source: `docs/foundation/project-level/subsystem-organization.md`.

**Key intended rules (quoted):**
- §Decisions.1 (line 7): *"Single Go module, single binary — module path
  `github.com/gregberns/harmonik`, one `go.mod`, one primary binary."*
- §Decisions.3 (line 9): *"Shared types in `internal/core` — … live in one leaf package
  with no imports from any subsystem."* Reinforced line 80: *"`internal/core` imports
  nothing from `internal/*` subsystems … This prevents the common 'shared types drift into
  subsystem-specific types' failure."*
- §Decisions.4 (line 10): *"Dependency layering enforced by `depguard` v2 (single tool
  for both lint rules and component-graph enforcement)."*
- §Decisions.6 (line 12): *"External adapters are packages, not subsystems —
  `internal/adapter/br` (Beads CLI), `internal/adapter/ntm` — thin shells."*
- §Dependency layering (line 91): *"`internal/` alone remains insufficient … the
  component graph below is what prevents forbidden cross-subsystem imports."*
- §Decisions.4-⚑ / ⚑4 (line 207): *"`internal/daemon` as composition root — all wiring
  (DI, startup, socket listener, shutdown) lives here so subsystems stay mutually unaware.
  The daemon package is the only one allowed to import most subsystems."*
- The intended package map (§7, lines 30–74) names: `orchestrator` (S01), `policy`
  (S02), `eventbus` (S03), `agentrunner` (S04), `hook` (S05), `workspace` (S06),
  `scenario` (S07), `memory` (S08), `improvement` (S09), `handler/{contract,claudecode,
  pi,twin}`, `adapter/{br,ntm}`, `daemon`.
- The doc also carries the full intended allowed-edge **matrix** (lines 181–199) with
  `daemon` importing everything and `core` importing nothing.

**Where reality violates the intended layout:**

1. **`internal/daemon` is a composition root in name only.** The doc (⚑4, line 207)
   scopes daemon to *"wiring (DI, startup, socket listener, shutdown)"*. In fact daemon
   holds 55.6k LOC / 115 top-level files of business logic — workloop.go (8.2k),
   tmuxsubstrate.go, reviewloop.go, dot_cascade.go, pasteinject.go, quiesce.go,
   orphansweep.go, stalewatch.go. The work-loop, review loop, and substrate that the map
   intended to live in `internal/orchestrator`/`internal/agentrunner` never got extracted;
   they accreted in the composition root. The depguard `daemon` rule (allow-all, no deny)
   permits this — nothing structural caps it.

2. **The intended subsystem packages largely don't exist.** `internal/orchestrator`,
   `internal/policy`, `internal/agentrunner`, `internal/hook`, `internal/memory`,
   `internal/improvement`, `internal/adapter/br`, `internal/adapter/ntm`,
   `internal/handler/{claudecode,pi,twin}` are all absent. Their depguard rules match zero
   files (§2.3). The real tree grew a different, flatter set (daemon + ~44 topical
   packages; brcli instead of adapter/br; flat handler/).

3. **`core` leaf-purity — holds** (depguard `core` rule denies `internal/`; core is
   31k LOC but structurally still a leaf). This intended invariant is the one the code
   most faithfully keeps.

4. **The scenario harness violates its intended rule** and the rule is commented out /
   deferred (.golangci.yml:328): scenario imports daemon, queue, lifecycle, workspace
   directly (12 violations, follow-up hk-uyxg0) — exactly the "harness may need relaxed
   rules" caveat the doc flagged (line 216).

Net: the intended design is a clean layered DAG with a thin composition root; the actual
tree kept the leaf discipline (core, queue, the declared leaves) but let the composition
root become the application, and never created most of the mid-layer subsystems the
matrix was written for.

---

## Appendix — commands run

```
find internal -maxdepth 1 -type d | sort                      # 45 packages
per-pkg: find <pkg> -name '*.go' [!]-name '*_test.go' | xargs wc -l   # LOC table §1
grep -iE 'cyclo|cyclop|funlen|complexity|gocognit' .golangci.yml     # NONE FOUND (§6e)
grep 'sync.Mutex|sync.RWMutex|Lock()' internal/queue/*.go            # none (§3a)
grep -c 'sync.Mutex|sync.RWMutex' internal/daemon/*.go               # 47 (§3a)
grep 'Persist(' internal/queue/*.go ; grep 'SetQueue(' internal/**   # writer paths §4
grep -E 'type (Result|Option|Either|Maybe)' internal/**             # zero monads (§5)
grep 'func .*\[[A-Z]' internal/**                                    # 2 generic fns (§5)
```
