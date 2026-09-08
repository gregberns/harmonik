# Kernel first slice (VC-12) — refined, dispatch-ready task plan

**Refines:** the Opus planner draft (kept in session scratchpad).
**Design of record:** `05-CORRECTION-the-real-design.md` + `07-operator-amendments-2026-09-07.md` (A1/A2/A3) + `08-verification-first-plan.md` + `06-vetting-ledger.md`; layout per `../2026-09-07-harmonik-restructure/07-segmented-structure-and-day1-standards.md`.
**Staffing:** Opus implementer agents in worktrees (operator directive; overrides Codex-first for the kernel track). Planning drafted by Opus, refined/reviewed by a Fable model.
**Settled constraints (not revisited):** subprocess plugins over a local socket even on one box; kernel carries opaque `[]byte`, no domain nouns; four channel types in the contract; fault-injection verification from commit one; segmented `go.work` modules, no `clean/`; hub/spoke competing-consumers is core (its consumer lands in Slice B).
**The slice gate:** VC-12 — publish 100 msg/s at `echo`, reload the plugin mid-stream, journal count == messages sent **exactly** (no loss, no duplicates), daemon PID unchanged, reload completes in single-digit ms. One lost message is a hard stop.

---

## Part 1 — Critique of the Opus draft

The draft's shape is sound: it reconciles A1/A2/A3, keeps hub/spoke core, puts the assessor gate at the end, and its critical path (T1→T2→T5→T7→T8→T9) is right. Nine weaknesses:

1. **T1 breaks the live build as written.** `cmd/harmonik` is TODAY a package inside the existing root module (`github.com/gregberns/harmonik`) — the live daemon, embedded skills (`//go:embed`), everything. Carving it into its own module in slice A rips the current build apart for zero VC-12 benefit. The restructure doc's `cmd/harmonik` module is the *end state*; staging must not touch it. Fixed in K1/K7: the slice-A composition root is a new binary inside the kernel segment (`kernel/cmd/harmonikd`); existing `cmd/harmonik` and root `go.mod` are untouched except joining `go.work`.
2. **No acceptance that the *existing* build survives `go.work`.** A repo-root `go.work` makes every `go` command workspace-aware for the current module too. Added: existing `make fast` / `make full` must stay green after `go.work` lands.
3. **T3 over-scoped.** ROADMAP M1's skeleton is ~5 RPCs; nothing in VC-12 or echo touches REQUEST_REPLY or LOOKUP. This traces to a real internal inconsistency in `08`: Slice 1 says "build the four skeleton RPCs" but "verify VC-13 channel conformance" — you cannot conformance-test four types with RPCs reaching one. Resolution: all four types stay in the contract enum from day one; slice A implements PUBSUB only, others return a clean typed error; VC-13 in slice A is scoped to shipped types; full four-type conformance is a named Slice B gate.
4. **T4 carries two things VC-12 never exercises.** KV has no slice-A consumer; the batching-drainer matters at logtail volumes, not 100 msg/s. Both cut to the comms slice; T4 gains a `kill -9` durability probe.
5. **T5 contradicts the source's deferral list.** ROADMAP M1 defers crash budget/backoff. Cut from slice A; keep crash detection only. The darwin pre-warm re-measurement becomes a T5 acceptance item — this box is a Mac and VC-12's ms bar dies on the ~500 ms first-exec signature check without the measured 505→9 ms pre-warm win.
6. **T8's acceptance ("= VC-12, not a unit test") leaves the bead uncommittable.** K8 gets its own integration-level acceptance; VC-12 is the slice verdict (K9).
7. **The zero-loss invariant is never stated.** The kernel-held subscription + queue (design/22 §3.1) must survive the reload and buffer during drain; a naive at-most-once PUBSUB drops mid-reload messages. Stated explicitly in K4/K8.
8. **OQ-2 recommendation right but under-argued.** The measured drain gate needs a per-message unary completion signal; pure server-stream pull has none. Push model stands as a *minimal recorded amendment* to the authoritative proto (operator-visible). See D2.
9. **No codenames, no kerf work, no `specs/` step.** Added as K0 and folded into K2.

Nothing in the draft contradicts the source docs beyond items 1, 3, 5.

---

## Part 2 — The refined task set

**One kerf work, one codename for every bead in this slice: `codename:kernel-vc12`.** All work lands on the integration branch; review trailers on every non-trivial commit; `make fast` while working, `make full` before acceptance; the slice is done only when K9's chaos gate holds.

Shared context block (paste into every bead):

> Design of record: `plans/2026-09-07-harmonik-bus/05-CORRECTION-the-real-design.md`, `07-operator-amendments-2026-09-07.md`, `08-verification-first-plan.md`; layout: `plans/2026-09-07-harmonik-restructure/07-segmented-structure-and-day1-standards.md`. Authoritative interface: `plans/2026-07-15-agent-substrate-v2/design/25-reconciled-proto/fleet/kernel/v1/{kernel,plugin}.proto` (as amended by D2). Plugin/reload mechanics + measurements: `design/22-plugin-system.md`. Slice definition: substrate-v2 `ROADMAP.md` §2. Kernel names no domain noun (bead/run/session/agent/echo/ping); payloads are opaque `[]byte`; plugins are stateless across reload — state lives in the kernel.

### K0 — Ratify decisions, open the kerf work *(process — captain/operator, not an implementer bead)*
Record D1–D3 outcomes in `DECISIONS-PENDING.md`; `kerf new` the work with `bead_filter` on `codename:kernel-vc12`; record the D2 proto amendment as a dated note beside the contract so the deviation from design/25 is provenanced. **Blocks:** K1 dispatch (staffing gate only — D1), K2 content (D2).

### K1 — Segmented module scaffold + boundary guardrails
**Segments:** repo root (`go.work`), `contract/`, `kernel/`, `tools/echo/`. **Deps:** none (dispatchable immediately; D1 is a staffing gate).
Create `go.work` (members: existing root module, `contract/`, `kernel/`, `tools/echo/`) and the three empty modules with per-module full-strength `.golangci.yml`, **empty allow-list**. No `shared/` module (no second consumer yet). No new `cmd/harmonik` module. Wire into `make full`: (a) import-closure — no segment module's `go list -deps` reaches `.../internal`; (b) tool-isolation — no `tools/*` requires another tool or `kernel`; (c) kernel vocabulary — grep asserts no domain noun in `kernel/`; (d) a `make chaos` target for `//go:build chaos` tests (empty for now).
**Acceptance:** `make full` green on empty modules AND the existing module unchanged; a deliberate `kernel → tools/echo` require makes the isolation check FAIL (prove it can fail, then remove); a planted domain noun in `kernel/` fails check (c).

### K2 — The wire contract (`contract/`)  **Deps:** K1; **D2 decided.**
Port `design/25-reconciled-proto/.../{kernel,plugin}.proto` into `contract/`: proto package `harmonik.kernel.v1`; `go_package` `.../contract/gen/...`; D2 amendment — add one unary `Deliver(DeliverRequest) returns (DeliverResponse)` to `PluginService`, commented with why + that it deviates from the reconciled proto by recorded decision. Keep all four `ChannelType` values and all 19 `KernelService` RPCs in the contract. `buf` lint STANDARD, `buf breaking` baseline at PACKAGE, generated Go+gRPC **committed** with a regen-and-diff step in `make full`. Copy the contract into `specs/` on finalize.
**Acceptance:** `buf lint` clean; breaking baseline recorded; all `go.work` modules build; generated code cross-compiles `CGO_ENABLED=0 GOOS=linux GOARCH=arm64`.

### K3 — Kernel state: the SQLite journal  **Deps:** K2. Parallel with K4/K5/K6.
Box-local State on `modernc.org/sqlite` (pure Go, WAL): `JournalAppend` (multi-record, `sync` per call — naive fsync-per-append is correct at this volume) and `JournalRead` (`after_seq` exclusive, `limit`, `follow`). Journal identity `(caller namespace, journal name)`; namespace from kernel-registered plugin identity, never a request field. Seq monotonic per journal. **Cut:** KV.
**Acceptance:** seqs strictly monotonic across restart; `sync=true` append then real `kill -9` is present after restart; `follow` delivers a post-read append; builds `CGO_ENABLED=0`.

### K4 — Kernel transport: in-memory PUBSUB + kernel-held subscriptions  **Deps:** K2. Parallel with K3/K5/K6.
In-memory single-box channel layer: manifest channel registration (first (name,type) wins; conflicting type rejected at registration); `Publish` on PUBSUB with kernel-stamped `origin_node`/`origin_time`/`message_id`/`origin_seq`/`producer` (caller values overwritten); `Info` (`max_payload_bytes` ~256 KiB via Info, never hardcoded). **Load-bearing (VC-12 depends on it):** a subscription + its pending-delivery queue are kernel state keyed by manifest, independent of any plugin process; the queue outlives the process. Other three channel types return a clean typed "not implemented in this slice" error, never a silent no-op.
**Acceptance:** opaque-payload round-trip proves the kernel never parsed the bytes; caller-stamped fields overwritten; `origin_seq` monotonic per (node, channel); vocabulary check green with real code; queue-outlives-consumer test (subscribe, buffer N, drop consumer, reattach, receive N).

### K5 — Plugin host: launch state machine (no drain yet)  **Deps:** K2. Parallel with K3/K4/K6.
Subprocess host on `hashicorp/go-plugin`: DISCOVERED → VERIFIED (sha256 vs manifest; mismatch refuses to exec) → PREWARMED (exec once and discard at registration — moves macOS ~500 ms sig check off the reload path) → LAUNCHING → HANDSHAKING → DESCRIBING (validate manifest: namespace owns its channel names; undeclared channel/interest rejected) → REGISTERED → STARTING (`Start` with node, kernel endpoint, caller_id) → RUNNING. Kernel dispatches deliveries as unary `Deliver` (D2). Child death surfaces `Unavailable` — **detection only; crash-loop budget/backoff deferred**.
**Acceptance:** a stub plugin runs as a distinct PID and receives a `Deliver`; sha256 mismatch refuses to exec; out-of-namespace channel rejected at registration; kill mid-call yields `Unavailable`; **measured on this darwin box:** first-exec vs post-pre-warm exec timing recorded in the bead close comment (if pre-warm does not move the ~500 ms, STOP and raise — VC-12's ms budget is broken).

### K6 — The `echo` plugin  **Deps:** K2. Parallel with K3/K4/K5.
`tools/echo/` (library core + adapter + `cmd/echo/main.go`): manifest declares namespace `echo`, one PUBSUB channel `echo.ping`, one interest; on each `Deliver`, call `JournalAppend(journal="seen", records=[payload], sync=true)` and return. ~80 lines; if it needs any kernel RPC outside the contract, STOP — the API is wrong. Stateless across reload. Builds `CGO_ENABLED=0` as its own binary; byte-reproducible or version-stamped so K9 can produce a "new" binary.
**Acceptance:** under the K5 host, a published byte lands in `seen`; imports only `contract/` (+ std/go-plugin) — K1 isolation enforces it.

### K7 — Composition root: `harmonikd` + client verbs  **Deps:** K3, K4, K5, K6.
One new binary (`kernel/cmd/harmonikd`, name per D3) wiring transport (K4) + state (K3) + host (K5), registering the `echo` launch spec (path + sha256) from config, serving `KernelService` on loopback so an SDK-less caller can `curl` it. Client verbs on the same binary (`publish <channel> <payload>`, `journal read <name>`, `plugin reload <name>`). Does NOT touch the existing `cmd/harmonik` daemon; the two run alongside until Slice C.
**Acceptance:** split demo end-to-end on one box — `publish echo.ping <bytes>` → `journal read seen` returns them byte-for-byte; VC-14 grep over `kernel/` zero hits; a second publish while echo is stopped is observably queued in the kernel.

### K8 — The drain gate: reload without loss  **Deps:** K5, K7.
The ~40-line kernel-side gate (design/22 §3.3; go-plugin's own shutdown hard-stops — a 3 s call died at 504 ms without the gate). On `plugin reload <name>`: set draining → new `Deliver` dispatches rejected at `enter()` and kept in the kernel-held queue (never dropped, never retried-while-in-flight); cancel long-lived streams (never wait — a subscription never ends by itself); wait in-flight unary `Deliver`s to a deadline; then `Kill()` → VERIFIED (new sha256) → relaunch via K5; diff the new manifest — persisting channels keep their queues untouched (reload ≠ re-subscribe); resume dispatch. **Invariant, verbatim in the code comment:** *every message is either never-dispatched (still kernel-held, delivered after reload) or drained to completion (journaled before Kill); there is no third state — this is what makes VC-12's exact-count assertion satisfiable.* Distinguish `Canceled` (deliberate) vs `Unavailable` (died).
**Acceptance (bead-level; slice verdict is K9):** a deliberately slow in-flight `Deliver` completes across a reload (design/22 TEST-4); mid-drain publishes all present after reload, no duplicates; a hung plugin falls through to `Kill()` at the deadline; deliberate stop logs `Canceled`, `kill -9` logs `Unavailable`.

### K9 — The VC-12 chaos gate *(assessor-owned)*  **Deps:** K7, K8.
Standing harness under `//go:build chaos`, run by `make chaos` only: a load generator ≥100 msg/s at `echo.ping` with a counted total, **sequence-stamped distinguishable payloads**; mid-stream `plugin reload echo` pointing at a *different* echo binary; assertions — journal row count equals messages sent exactly (loss AND duplication fail; treat as set-equality, not just count); `harmonikd` PID unchanged; reload latency single-digit ms on this darwin box. Plus VC-13 scoped to shipped types (PUBSUB conformance) and VC-14 grep as a `probe` case. Cases in `test/exploratory/cases/`; dated `assessments/YYYY-MM-DD-HHMM-kernel-vc12/` created from `_TEMPLATE` before the run, written as it happens, pinned to the slice commit. Any lost/duplicated message is a hard stop put to the operator.
**Acceptance:** the gate holds against the live `harmonikd`, three consecutive runs; re-runnable by one command. Slice B does not start until this holds.

---

## Part 3 — Dependency graph, critical path, parallelism

```
K0 (decisions/kerf)
 └─ K1 (scaffold)
     └─ K2 (contract)  [needs D2]
         ├─ K3 (journal)   ─┐
         ├─ K4 (transport) ─┼─► K7 (composition) ─► K8 (drain gate) ─► K9 (VC-12 gate)
         ├─ K5 (host)      ─┤        ▲
         └─ K6 (echo)      ─┘────────┘   (K8 also needs K5)
```

- **Critical path:** K1 → K2 → K5 → K7 → K8 → K9. K5 is the largest of the parallel four and highest external-library risk; staff the strongest implementer there, start it first among the four.
- **Fully parallel after K2:** K3, K4, K5, K6 (independent, no shared files).
- **K1 dispatchable today** (D1 is a staffing gate, not content). K2 needs only D2.

---

## Part 4 — Decisions

### Blocking dispatch

| # | Decision | Who | Blocks | Recommendation |
|---|---|---|---|---|
| **D1** | Program framing: re-grounding of P1/P2/P3 vs formal supersession | **Operator** | crew staffing (not K1 content) | **Ratify re-grounding** — operator is already amending locked decisions (C6→A1); only provenance changes either way. |
| **D2** | Plugin data-flow: add unary `Deliver` to `PluginService` (push) vs keep reconciled proto's pure pull | **Architect proposes, operator signs** | K2 | **Add `Deliver`** — the measured zero-loss drain gate needs a per-message unary completion signal; pure server-stream pull has none and forces an invented ack protocol against the proto's no-bidi rule. `KernelService` untouched, so curl-with-no-SDK survives. |
| **D3** | New daemon/CLI naming (`harmonikd` vs `fleetd`/`fleetctl`) | **Operator** (30 s) | K7 naming only | **`harmonikd`**, client verbs on the same binary; runs alongside the existing `harmonik` daemon until Slice C. |

### Resolved in this plan (architect-level; operator may veto, none waits)
- **Module staging:** exactly `contract/`, `kernel/`, `tools/echo/` + `go.work` with the existing root module. No `cmd/harmonik` module, no `shared/` module in slice A.
- **Subprocess mechanism:** `hashicorp/go-plugin` — settled by A1's own wording; every reload/drain measurement is evidence for it.
- **Proto toolchain:** package `harmonik.kernel.v1`; `go_package` under `.../contract/gen/`; `buf breaking` PACKAGE; generated code committed with regen-and-diff in `make full`.
- **VC-13 scope in slice A:** shipped-types only; full four-type conformance is a Slice B gate (resolves the `08` §Slice 1 inconsistency — flag in the K9 assessment record).

### Resolvable in-task (do not escalate)
Socket/endpoint paths; SQLite file location under the kernel data dir; `max_payload_bytes` exact value (via `Info`); drain deadline default; queue depth cap during drain (be generous); go-plugin handshake config details.

### Explicitly deferred (trigger named)
RSS/memory bound (observe; bound when a plugin misbehaves); crash-loop budget/backoff (first non-toy plugin); KV + batching drainer (comms slice); M0 real-sleep, cross-machine transport choice, replicated LOOKUP, dynamic worker join (Slice C).

---

## Part 5 — Slices B and C (unchanged from the draft; refine when slice A's gate holds)

- **Slice B** — full POINT_TO_POINT competing-consumers; two in-mem kernels via an in-process transport double; the `dispatch` plugin (role primary|worker, one namespace); roster as pure local functions. Gates: 10 jobs → 5+5 exactly-once load-balanced; worker-dies → in-flight requeues (C5 minimum); VC-12 re-run vs a stateful `dispatch` reload; four-type channel conformance.
- **Slice C** — cross-machine transport (NATS vs owned TCP); M0 real overnight macOS sleep (VC-4 — mesh re-forms on wake, else project stop); comms plugin migration (VC-6/7/8/9/11 + VC-12 across the box boundary). Deferred with triggers: replicated LOOKUP, dynamic worker join, registry/logtail/ssh plugins.

---

## Residual risks
- The D2 amendment is a real deviation from the reconciled proto — record it where the contract lives, or a future agent "corrects" it back and reintroduces the pull-model loss window.
- Single-digit-ms reload on darwin rests on one July measurement (505→9 ms via pre-warm); K5 re-measures early.
- `go.work` into a large live module is low-risk but wide (gopls/lint/CI behave workspace-aware); K1 covers `make fast`/`make full`; watch tool friction the first few days.
- The reload-window queue is unbounded in slice A (fine at this volume; bounded by VC-11 in the comms slice).
- VC-12 asserts count; use sequence-stamped payloads so the check is also set-equality (catches reorder/corruption).
