# Track B (data-integrity) + M1 (test-theater) — Ready-to-Execute Scope

> Authored 2026-07-13. Scopes two seam-independent code-revamp streams into executable
> work. **Daemon is OFF for the rebuild — do not enable it.** All line refs verified
> against the working tree on branch `phase1-session-restart-substrate`.
>
> - Track B = two small, direct data-integrity fixes → **beads, not a kerf work**.
> - M1 = test-theater keep/delete classification → needs **operator sign-off on the
>   ambiguous set** (census Q4) before bulk delete; reconciles into `testing-strategy-uplift`.
>
> Sources reconciled: `research/06-architecture-and-fp.md §4`, `research/04-event-system.md §6`,
> `plans/2026-07-12-codebase-census/{REPORT §4, PLAN STEP-0b/M1/Q4}`, `ROADMAP.md`, and the P1
> `session-restart-substrate` bench (`04-design/00-decisions.md D6`, `04-design/events-design.md §4`).

---

## Headline reconciliation finding (READ FIRST)

**The "dead event-registry path" is NO LONGER DEAD — P1 adopted it. Do NOT delete it.**
Census §4 / dossier 04 §6 both classified `DecodePayload` / `DispatchObservational` /
`DispatchSynchronous` / `ValidateEnvelopeSchemaVersion` / `pertypecompat` as production-dead
and slated them for deletion in Move 1. That was true on `main`. It is **false on the current
branch**: P1 (`session-restart-substrate`, T4 `feat(replay)`) landed `internal/replay/replay.go`,
which actively consumes the typed-decode read path (`DecodePayloadStrict`, `DispatchObservational`,
`ValidateEnvelopeSchemaVersion`, `LookupPayloadCompatEntry`) — `DispatchSynchronous` is adopted as
normative-not-called (retained under D6/EV-048, not invoked by `replay.go`) — and bench decision
**D6 `[OPERATOR-LEAN CONFIRMED]`** says verbatim *"ADOPT … Do NOT delete."* See the DELETE recommendation reversal in
§M1.4 below. This is exactly the collision the task warned about; M1 must NOT delete these symbols.

---

# Track B — data-integrity fixes (beads, no kerf)

## B1 — queue.json two-writer / lost-update

**Precise nature of the bug.** `queue.json` and the in-memory `QueueStore` have two writers that
do not share a lock:

- **Writer A — daemon workloop** (`internal/daemon/workloop.go`) mutates dispatch state under the
  `queueMu` write lock via `QueueStore.LockForMutation()` → `LockedQueueStore`
  (`internal/daemon/queuestore_hkj808w.go:69` decl; `:274` `LockForMutation`), then
  `queue.Persist` + `LockedSetQueueByName`.
- **Writer B — queue-append RPC** (`internal/queue/rpc.go`, `HandlerAdapter.HandleQueueAppend`
  at `:993`, backed by pure `HandleQueueAppend` at `:342`) does the whole read-modify-write
  **outside `queueMu`**:
  - reads the queue **fresh from disk**: `Load(ctx, projectDir, …)` at `rpc.go:356`/`:374`
    (via the pure fn — bypassing the live in-memory store entirely),
  - mutates it (`AppendItems`, `append.go` — includes a `ledger.BlocksEdge` round-trip, so a
    non-trivial window),
  - `Persist(ctx, a.projectDir, mutated)` at **`rpc.go:1009`**,
  - `a.qs.SetQueue(mutated)` at **`rpc.go:1016`** — clobbers the store snapshot.

If Writer A mutates + persists dispatch statuses during B's window, B's `Persist(mutated)`
overwrites A's fresh statuses on disk AND `SetQueue(mutated)` overwrites A's live in-memory queue
— even though B never held `queueMu`. Two writers, one file, no shared mutex → last-write-wins.
This violates the single-writer discipline (QM-060, cited `rpc.go:54`) and read-then-write
serialisation (QM-064, `queuestore_hkj808w.go:254`), both of which are enforced *inside* the
`LockedQueueStore` that the append path never enters.

**Concrete fix approach.** Route the append adapter through the existing lock, and read the
**live in-memory store under the lock** instead of a stale disk `Load`:

1. In `HandlerAdapter.HandleQueueAppend` (`rpc.go:993`), acquire `lq := a.qs.LockForMutation();
   defer lq.Done()` around the read-modify-write.
2. Feed the append computation the queue snapshot taken under that lock
   (`lq.QueueByName(name)` / `lq.Queue()`) rather than `Load()` from disk. This makes the pure
   `HandleQueueAppend` operate on authoritative live state, not a disk copy that may already be
   stale relative to Writer A.
3. `Persist(...)` **while still holding the lock**, then write the mutated queue back through the
   locked view (`lq.SetQueueByName` / `LockedSetQueueByName`) + `Wake()` — using the
   `LockedQueueStore` no-wake set to avoid the double-persist race the store already documents
   (`queuestore_hkj808w.go:233-251`).

Net: the append becomes one serialized read-then-write under `queueMu`, identical to the
discipline Writer A already follows. (Small caveat for the bead: the pure `HandleQueueAppend`
currently does its own `Load`; the adapter refactor must pass the locked snapshot in, or the pure
fn must accept a pre-loaded queue — a signature tweak, still small.)

**Regression test (out-of-pipeline, isolated).** A `-race` concurrency test in `internal/daemon`
(where `QueueStore` lives) that: seeds a queue in the store, then concurrently runs
(A) a goroutine performing a `LockForMutation` → mutate-a-status → `Persist` → set cycle, and
(B) the append adapter appending an item, N iterations. Assert **no lost update**: after both
settle, the persisted `queue.json` + the in-memory store contain BOTH A's status mutation AND B's
appended items. Must fail on the current code (reproduces the clobber) and pass after the fix.
Name: `queuestore_append_lostupdate_hkXXXXX_test.go`.

**Proposed bead title:**
> `fix(queue): route append RPC through LockForMutation — close queue.json two-writer lost-update (rpc.go:1006-1016)`

---

## B2 — noChange / Cat-3c subsumption false-close (census STEP-0b)

**Precise nature of the bug.** The orphan-sweep Cat-3c auto-close path closes an `in_progress`
bead when it believes a merge commit "for" that bead exists on the target branch. "Exists" is
decided by `GitMergeCommitScanner.HasMergeCommitForBead`
(`internal/lifecycle/orphansweepbeads.go:230-247`), which shells out to:

```go
git -C <dir> log -1 --grep "Harmonik-Bead-ID: <beadID>" --format=%H <branch>
```

Two defects make this fabricate a close:
1. **`--grep` matches the whole commit message body as a regex, not the git trailer.** Any commit
   whose message merely *mentions* the string `Harmonik-Bead-ID: hk-xxxx` — e.g. a docs / captain-
   lanes edit that quotes an intent-log line or a prior commit — matches. This is exactly the
   `hk-2hfyt` incident (REPORT Addendum): its ID appeared in unrelated docs commit `32dc13f7` and
   the bead was closed with the fix ABSENT.
2. **No fix-content evidence.** A docs-only commit that carries/quotes the trailer qualifies even
   though it touched zero implicated source files.

Callers of the false signal (single scanner, two call sites — one fix covers both):
`orphansweepbeads.go:583` (the sweep's exclusion (c) / Cat-3c auto-close at `:589-595`) and
`internal/daemon/reconciliationcadence_rc020a.go:193`.

**Concrete fix approach.** Replace the substring `--grep` with genuine-trailer + non-docs-diff
evidence, in `GitMergeCommitScanner.HasMergeCommitForBead`:

1. **Parse the actual git trailer, require exact value equality.** Enumerate candidate commits and
   extract the trailer via
   `git log --format='%H %(trailers:key=Harmonik-Bead-ID,valueonly=true)'` (the same mechanism
   already used at `internal/lifecycle/activerun_em031a.go:192`), and accept only a commit whose
   trailer value **equals** `beadID` — not a body substring. This rejects mentions-in-prose.
2. **Require fix-content evidence.** For the matched commit, confirm its diff touches non-docs
   files (i.e. not exclusively `*.md` / docs / captain-lanes paths) via `git show --name-only` /
   `git diff-tree --name-only`. A docs-only commit carrying the trailer must NOT qualify.
3. Keep the conservative failure mode intact: a scan error still returns `(false, nil)` — missing
   a real Cat-3c is re-detected next restart; a false positive skips a needed reset or fabricates a
   close, which is the worse outcome (`orphansweepbeads.go:219-223`).

**Regression test (out-of-pipeline, isolated).** A table test in `internal/lifecycle` against a
seeded temp git repo, asserting both directions (census 0b DoD):
- **must NOT close:** a docs-only commit whose body contains `Harmonik-Bead-ID: hk-xxxx` (mention)
  → `HasMergeCommitForBead == false`;
- **must NOT close:** a commit carrying the genuine trailer but touching only `*.md` → `false`;
- **must still close (legit subsumption preserved):** a real fix commit with the genuine
  `Harmonik-Bead-ID:` trailer that touches source files → `true`.
Verify the exit condition by a **diff-content assertion against the seeded commits**, never by the
reconcile path's own "closed" status (Oracle #3 — the close path cannot be its own oracle).
Name: `orphansweepbeads_falseclose_hkXXXXX_test.go`.

**Proposed bead title:**
> `fix(lifecycle): Cat-3c false-close — require genuine Harmonik-Bead-ID trailer + non-docs diff, not a git-log --grep body mention (orphansweepbeads.go:230)`

---

# M1 — test-theater deletion: keep/delete classification

Verified sizes (working tree, `wc -l` split test vs non-test):

| Surface | non-test | test LOC | test files |
|---|---:|---:|---:|
| `internal/specaudit` | 9 (doc.go only) | 37,646 | 132 |
| `internal/operatornfr` | 1,081 (8 files) | 13,598 | 37 |
| `internal/scenario` | 4,792 (34 files) | 17,298 | 42 |
| dead event-registry path (`internal/core`) | — | — | — |

## M1.1 Classification table

| package / path | test LOC (approx) | classification | rationale |
|---|---:|---|---|
| `internal/specaudit/` | ~37.6k | **RELOCATE (safe)** | 9 LOC of non-test (only `doc.go`, which executes no product code). 129 of the 132 test files are markdown/spec-prose regex assertions over `specs/`; **3 test files import product packages and require a carve-out** (they are NOT bulk-moved with the rest). Collapse the remaining spec-prose files into **one CI lint script outside `go test`**. Removing them from the suite loses no product coverage; the spec-drift check survives as CI lint. Minor operator-free judgment: the 3-file carve-out disposition. |
| `internal/operatornfr/` — **keep set** | (subset) | **KEEP** | `commandcodes.go`, `exitcode.go`, `securitypolicy_on006_on026.go`, `sandboxinvariant_on024.go` + their real tests exercise real invariants (census-verified). Keep as-is. |
| `internal/operatornfr/` — **self-assert set** | ~most of 13.6k | **AMBIGUOUS — operator call** | The fixture-mirror tests that assert the package's own constants/fixtures (tautological — never `exec` the product) are DELETE candidates, but the three remaining non-keep source files — `commitintegrity_on005.go`, `reviewloopstatus_on035a.go`, `upgradingmarker_on020a.go` — plus their tests need a per-file keep/delete call before bulk delete (census confidence is "mostly delete," not "all"; deletion is hard to reverse). **Operator sign-off required.** |
| `internal/scenario/` — **harness (34 non-test files)** | — | **KEEP** | 4,792 LOC of real harness code (`asserteval`, `crashrecovery`, `orchdrive`, network-sandbox, fixture bootstrap/teardown, leak sensors). This is the one test-adjacent package that drives real product code end-to-end. Keep. NOTE: it has 12 depguard violations (imports daemon/queue/lifecycle/workspace, follow-up `hk-uyxg0`) — orthogonal to M1, do not conflate. |
| `internal/scenario/` — **test files (42)** | ~17.3k | **AMBIGUOUS — operator call** | Census: prune to "the harness + ~11 behavioral files." Which of the 42 `_test.go` are behavioral (exec real code) vs theater is a judgment call. Lower stakes than operatornfr, but still needs a one-pass classification before pruning. |
| dead event-registry: `DecodePayload`, `DispatchObservational`, `DispatchSynchronous`, `ValidateEnvelopeSchemaVersion`, `pertypecompat` (`internal/core`) | — | **KEEP — do NOT delete (reversed from census DELETE)** | See §M1.4. P1 adopted this path; deleting it breaks `internal/replay`. |

## M1.2 What is safe-delete vs needs sign-off

- **Safe, mechanical (no operator judgment):**
  - `specaudit` → **RELOCATE 129** spec-prose test files to one CI lint script, remove from `go test`;
    the **3 product-importing files STAY under `go test`** (product-import carve-out — NOT bulk-moved).
    The relocated 129 execute no product code; zero coverage lost.
  - The `operatornfr` **keep set** is retained verbatim — no action beyond keeping.
- **Needs operator sign-off (census Q4 — the ambiguous set):**
  - `operatornfr` self-assert test mass minus the four keep-files, **and** the disposition of
    `commitintegrity_on005.go` / `reviewloopstatus_on035a.go` / `upgradingmarker_on020a.go`.
  - `scenario` test-file pruning (which of 42 are behavioral vs theater).
  The risk both share: a genuinely load-bearing regression test hidden among the theater. Mitigation
  (per PLAN M1 DoD): per-file keep/delete classification → operator ratifies the ambiguous set →
  delete in reviewable batches; every *retained* `_test.go` package must show non-zero product
  coverage under `go test -coverpkg=./internal/...`.

## M1.3 DoD (mechanical, per Oracle #4)

1. Explicit **allowlist** of the `specaudit` files moved to the CI-lint script.
2. **Coverage gate:** every retained `_test.go` package shows non-zero product coverage under
   `-coverpkg=./internal/...`.
3. Any deleted symbol has **zero remaining references** (`grep` clean).
4. **The event-registry path is NOT in the delete set** (see §M1.4).

## M1.4 Dead event-registry path — firm DELETE-or-ADOPT recommendation (RECONCILED with P1)

**Recommendation: ADOPT / KEEP. Do NOT delete any of these symbols.**

Evidence (verified on this branch, not `main`):
- `internal/replay/replay.go` is a **live production reader** of the whole surface:
  `ev.DecodePayloadStrict()` (`replay.go:196`), `core.DispatchObservational(ev)` (`:205`),
  `core.ValidateEnvelopeSchemaVersion(ev)` (`:184`), `core.LookupPayloadCompatEntry(ev.Type)`
  (`:187`). Landed by P1 T4 (`5262aa48 feat(replay): internal/replay SR invariant-checking harness`).
- Bench decision **D6 `[OPERATOR-LEAN CONFIRMED]`** (`04-design/00-decisions.md:170`): *"ADOPT
  `DecodePayload` / `ValidateEnvelopeSchemaVersion` / `DispatchObservational` / `DispatchSynchronous`
  / `pertypecompat` as the replay harness's decode+assert layer. Do NOT delete."*
- Spec amendment **EV-048** (`05-spec-drafts/event-model-amendment.md:293`) declares the typed-decode
  registry *"ADOPTED (not dead, not to be deleted)"*; **EV-049** adds an **additive**
  `DecodePayloadStrict` (already present at `eventregistry.go:249`) — a strict `DisallowUnknownFields`
  variant. The `pertypecompat` rows are now **mandatory**, enforced by
  `pertypecompat_hqwn38_test.go`.

The census's "production-dead → delete" verdict was correct **as of `main` 2026-07-12** but is
**superseded by P1's adoption**. M1 must strike this item from the deletion list. (If anyone re-runs
the census grep on `main` and sees zero readers, that is a stale snapshot — the readers live in
`internal/replay`, which is on the P1 branch feeding into the revamp.)

---

## Operator-decision section (minimal keep/delete calls needing the human)

Each with a recommended default:

| # | Decision | Recommended default |
|---|---|---|
| O1 | `operatornfr` self-assert test mass: delete the fixture-mirror tests that assert the package's own constants? | **DELETE** the tautological self-assert tests; **KEEP** the four named source files + their real tests. |
| O2 | `operatornfr` source files `commitintegrity_on005.go`, `reviewloopstatus_on035a.go`, `upgradingmarker_on020a.go` — keep or delete (with tests)? | **KEEP pending a per-file exec check** — only delete a file+test pair if its test asserts constants and never drives product code. Err toward keep (hard to reverse). |
| O3 | `scenario` — prune which of the 42 test files? | **KEEP the harness (34 non-test files) + the ~11 behavioral test files that `exec` real code**; delete the rest in a reviewable batch after a one-pass classification. |
| O4 | `specaudit` relocation target | **RELOCATE the 129** spec-prose files to one CI lint script outside `go test`; the **3 product-importing files stay under `go test`** (product-import carve-out, B3). Safe; no product coverage lost — no operator judgment strictly required, listed for visibility. |
| O5 | dead event-registry path | **KEEP — already reconciled; no operator action.** Listed so the census's DELETE line is explicitly retired. |

**The genuinely operator-gated set is O1–O3** (census Q4). O4/O5 are mechanical/settled and shown
for completeness.

---

## Reconciliation note — M1 → `testing-strategy-uplift` kerf work (advisory, do not modify it)

Per ROADMAP: M1 "reconciles into `testing-strategy-uplift` (integration)." Mapping:

- **M1 is the deletion/relocation slice; it is NOT a new kerf work** (deletion introduces no new
  contract — ROADMAP: "M1 … no (direct)"). Do the classification + deletion as **beads under Track B/M1
  hygiene**, gated on operator sign-off (O1–O3).
- `testing-strategy-uplift` owns the **positive** side — the L0–L3 test taxonomy also being defined in
  P1's substrate spec. M1's output (the trustworthy test/code ratio, the coverage-gate allowlist, the
  "green means product ran" baseline) is an **input** to that work, not a competitor.
- **Per operator-approved DECISIONS B5: `testing-strategy-uplift` is SUPERSEDED — do NOT re-open it as
  a separate work.** Harvest its 5-layer (L0–L3 + drift) test taxonomy **INTO M1**; M1's coverage-gate
  result is the input/starting baseline. (This supersedes the earlier "fold into `testing-strategy-uplift`,
  do not supersede" framing.)
- Adjacent, do NOT conflate with M1: the `scenario` depguard violations (`hk-uyxg0`) and the
  event-registry ADOPT (P1's `session-restart-substrate`) are owned elsewhere.

---

## Summary of counts

- **Track B:** 2 beads (B1 queue two-writer, B2 Cat-3c false-close). Titles above.
- **M1 test packages — DELETE/RELOCATE-safe: 1** (`specaudit`). **AMBIGUOUS (operator sign-off): 2**
  (`operatornfr`, `scenario`). **Reversed to KEEP: 1** (dead event-registry path — P1 adopted).
