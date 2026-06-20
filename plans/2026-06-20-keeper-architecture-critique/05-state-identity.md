# Keeper Critique — State Management & Identity Lens

**Verdict:** The keeper has **no canonical, authoritative identity**. Session identity is *inferred at runtime from four competing files that must be kept in sync imperatively*, and the "fix" (the keeper-redesign spec) is **specced but not implemented** — the heuristic machinery it orders DELETED is still on `main`. This is the structural root of the recurring identity bugs.

---

## 1. The identity resolution chain (and where it desyncs)

There is no single identity. A session is named by **four** artifacts that the watcher must reconcile every tick:

```
  tmux pane  ──(SHA256 of projectDir + agentName)──►  tmux session name
       │                                              "harmonik-<hash12>-<agent>"
       │ statusline hook writes pct + its OWN session_id
       ▼
  <agent>.ctx  (gauge)   ── multi-writer: statusline.sh + keeper heartbeat ──┐
       │  .SessionID                                                          │
       │                                                                      │
  <agent>.sid  (SessionStart hook, "single-writer")  ── PRIMARY identity ────┤
       │  folded into ctx.SessionID by ReadCtxFile if isPrimarySID()         │
       ▼                                                                      ▼
  <agent>.managed  ◄── watcher LATCHES first-seen id ──► cycler REBINDS post-/clear
       (the "binding")        (WriteManagedSessionID)      (waitForNewSessionID → SetManagedSessionFn)
```

Desync points — every one is a live bug class:

- **Gauge `.ctx` is multi-writer.** `keeper-statusline.sh` writes one `session_id`; `heartbeat.go:maybeHeartbeat` (`WriteCtxFile`) writes another. Two same-agent sessions both write `.ctx` → last-writer wins (`keeper.go:121` cites hk-igt: "two same-agent sessions writing to .ctx"). The whole `.sid` channel (`sessionid.go`) exists *only* to paper over this multi-writer race — it is a workaround, not a design.
- **`.managed` vs `.ctx.SessionID` mismatch** (`watcher.go:684`) is interpreted three different ways at runtime: same-agent-post-/clear (adopt), truly-foreign (reject as absent), or read-error (fall through). The disambiguator is *another* re-read of `.sid` (`watcher.go:692`). Three files, one mismatch, three branches.
- **UUID-version routing.** `isPrimarySID`/`isUUIDv4` (v4=interactive) vs `isUUIDv7` (v7=daemon implementer) vs `isUppercaseUUID` (transcript-dir id). Identity correctness depends on guessing *which kind of UUID* leaked into the gauge — `cycle.go:1401`, `heartbeat.go:161`, `watcher.go:502`. This is identity-by-heuristic.

There is **no canonical identifier**. The "primary" `.sid` is itself derived (written by a hook scraping Claude's env), and the watcher still latches the gauge value when `.sid` is absent/malformed (`ctxFile.go` fallback path, `watcher.go:713`).

---

## 2. KNOWN BUG: `--session-id` dies after first `/clear`

**Root in code:** `harmonik keeper` **has no `--session-id` flag at all** (`cmd/harmonik/keeper_cmd.go:51-71` — only `--agent`, `--tmux`, pct/abs/window, `--respawn-cmd`, `--force-restart`). The keeper never *accepts* an authoritative identity; it always *infers* it from the gauge and **latches** the first id it sees into `.managed` (`watcher.go:713-721`). This is the **bind-and-delete antipattern**: on `/clear` the gauge `session_id` flips, `cycle.go:waitForNewSessionID` (`cycle.go:1383`) polls the gauge for the *new* id, and `SetManagedSessionFn` rewrites `.managed` (`cycle.go:847`, `cycle.go:1286`). If the post-clear gauge briefly carries a UUIDv7/uppercase/absent value, the rebind binds wrong or the latch points at a dead id.

**Is "re-resolve from gauge" implemented or just specced?** Implemented — but as the *opposite* of what the redesign wants. The redesign spec (`keeper-identity-and-liveness.md` §I2.2/§I1.1) says: write `.managed` **exactly once** from a launch `--session-id`, then **read** it forever; **I1.2: no auto-clear; I1.3: no latch/flap recovery.** Current code does the inverse: `waitForNewSessionID` re-resolves from the gauge on *every* cycle (`cycle.go:837,1280`), auto-rewrites `.managed` (`cycle.go:781` clears it to `""`; `:847`/`:1286` rebinds; `watcher.go:697` adopts). The spec's authoritative-launch identity **does not exist in code**.

---

## 3. KNOWN BUG: stale `.managed` foreign_session

**Ownership determination** (`watcher.go:681-712`): `.managed` "owns" the agent if its stored id equals the gauge's `session_id`. A mismatch is provisionally "foreign," then re-checked against `.sid`: if `.sid` is a valid UUIDv4 equal to the gauge id → **adopt** (rewrite `.managed`); else → **reject as foreign** and emit `no_gauge:foreign_session`, `continue` (no warn/act runs).

**Is the auto-recovery robust?** No — it is a **band-aid with a permanent-stall failure mode**. The MEMORY note says stale `.managed` "auto-recovers after ~3 ticks + `keeper rebind`." But:
- `keeper rebind` **no longer exists** (`grep rebind cmd/harmonik/keeper_cmd.go` → 0 hits). The manual escape hatch the operator's recovery procedure depends on is gone, yet the redesign deletion-checklist (D11) still lists it as a thing to remove — the docs, memory, and code disagree about whether `rebind` exists.
- Recovery hinges on `.sid` carrying a valid UUIDv4 that *matches the gauge*. Under remote-control/mobile or right after `/clear`, the statusline skips writes (`heartbeat.go:18-24`), so the gauge is stale/empty — adoption can't fire and the session sits in `foreign_session` indefinitely. The heartbeat (`maybeHeartbeat`) can paper the gauge, but it stamps the **managed** id (`heartbeat.go:193`), so if `.managed` is itself wrong the heartbeat *propagates the wrong identity*.

---

## 4. KNOWN BUG: gauge "not wired for crews"

**Confirmed structurally.** The gauge writer (`keeper-statusline.sh`) and hooks resolve the agent name from `$HARMONIK_AGENT` with a `$HARMONIK_KEEPER_AGENT` back-compat alias (`scripts/keeper-statusline.sh:43-48`, `keeper-precompact-hook.sh:69-76`). Whether a crew's gauge updates depends entirely on that env var being set in the crew's Claude process AND the statusLine being wired in that pane's settings. There is no code path that *guarantees* a crew pane writes `.ctx` — it is convention + hook wiring, validated only at `keeper doctor` time (`keeper_enable_doctor_cmd.go:611-628` warns on gauge/`.sid` drift but cannot fix it). The session→gauge resolution is **fundamentally fragile**: it relies on an env var propagating through `crew start` → tmux → Claude → statusLine hook, with no end-to-end assertion. (I could not find crew-side `--session-id` threading the redesign's §I2.6 requires — `crewstart.go resolveSessionID` is referenced by the spec but no `--session-id`/keeper threading surfaced in grep.)

---

## 5. Structural smells

1. **Identity is an afterthought bolted on.** It is literally a *separate* kerf work (`keeper-redesign`) producing a *competing* spec against the original `session-keeper` spec. The redesign's existence — and its §3.2 "EXPLICIT DELETION TARGET, net LOC MUST go DOWN" framing naming D1–D11 / K1–K7 — is an admission that identity was accreted as a "fix-of-fix" pile, not designed. The −542-line approved-but-**unmerged** commit `89852bb3` (prior investigation README) is the redesign trying to land and *not yet on `main`*. So `main` carries the old machinery AND a spec ordering its deletion.
2. **No single canonical identity — four competitors kept in sync by hand:** tmux-name (derived), `.ctx.SessionID` (multi-writer), `.sid` (hook-scraped "primary"), `.managed` (latched/rebound). Each has its own writer, its own staleness, its own UUID-shape guard.
3. **Identity correctness depends on UUID *version* sniffing** (v4 vs v7 vs uppercase) scattered across `sessionid.go`, `keeper.go:144-160`, `watcher.go:502`, `cycle.go:1401`, `heartbeat.go:161`. The redesign §I2.4 says "no UUID-version branching" — every one of these is a violation still present.
4. **The single-writer invariant (§I1.1: write `.managed` once) is violated in ≥4 places**: `watcher.go:697` (adopt), `:717` (latch), `cycle.go:781` (clear to ""), `:847` & `:1286` (rebind). The atomic-rename machinery in `WriteManagedSessionID` (`keeper.go:178-222`, temp+fsync+rename, citing hk-b5e2) exists *because* multiple processes write `.managed` — itself proof the single-writer invariant is not held.

---

## 6. Testability

Identity logic is imperatively interleaved in the 53KB `watcher.go` tick loop (the binding block is `watcher.go:665-721`, inside a `for{}` with `continue` short-circuits). It is reachable in unit tests only through injected `ReadManagedSessionFn`/`WriteManagedSessionFn`/`ReadSidFn` stubs — so tests assert the *stub contract*, not the on-disk multi-writer race that actually breaks. The prior investigation confirms this: "the consume tests use an injected in-memory stub, not a marker the write-side wrote to disk… the two halves meet only at the struct shape." The redesign's §6 reduces the tick to a 6-step branch-free path precisely because the current branchy form is what evades tests — and is why it keeps breaking.

---

## Cited anchors

- `sessionid.go:30,61,70` — `.sid` channel + UUIDv4 gating (the workaround for multi-writer `.ctx`).
- `gauge.go:18,59-61` — `.ctx` schema; `.sid` override fold.
- `heartbeat.go:18-35,160-165,193` — multi-writer gauge; heartbeat stamps managed id (propagates wrong identity if managed is wrong).
- `keeper.go:106-142,144-160,178-222` — `.managed` read/write; UUID-version helpers; atomic-rename = multi-writer admission.
- `watcher.go:665-721` (binding block), `:502` (`isUppercaseUUID`) — runtime latch/adopt/reject heuristic.
- `cycle.go:781,847,1286,1383-1406` — auto-clear + rebind + gauge re-resolve (violates §I1.1/§I1.2/§I1.3).
- `cmd/harmonik/keeper_cmd.go:51-71` — **no `--session-id` flag** (identity never authoritative).
- `scripts/keeper-statusline.sh:43-48`, `keeper-precompact-hook.sh:69-76` — crew gauge wiring via `$HARMONIK_AGENT` (convention, not guaranteed).
- `keeper_enable_doctor_cmd.go:611-648` — drift only *reported*, never fixed; `keeper rebind` no longer exists.
- Redesign spec `keeper-identity-and-liveness.md` §1 (single-writer), §2 (`--session-id` authoritative), §3.2 (deletion checklist D1–D11/K1–K7) — **specced, NOT implemented on `main`**.
- Prior investigation `plans/2026-06-20-keeper-investigation-recovery/README.md` — −542-line redesign commit `89852bb3` approved but **unmerged**.
