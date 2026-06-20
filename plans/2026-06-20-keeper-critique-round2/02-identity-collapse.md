# Keeper Critique R2 — Identity Collapse Lens

**Lens:** the one open architectural item — collapse the keeper's competing identity sources to ONE authoritative `.sid`.
**Date:** 2026-06-20. Built on R1 `05-state-identity.md` + `02-architecture.md`.

**Verdict up front:** This is **NOT a safe deletion today.** `.sid` is authoritative *for ReadCtxFile's overlay only*. The watcher's latch/adopt/foreign branches and the cycler's gauge re-resolution remain **load-bearing** for two paths that have no `.sid`: (a) the cycler's own post-`/clear` re-resolution, which polls the *gauge*, not `.sid`; (b) any session where the SessionStart hook didn't write `.sid` (crews, mobile/remote-control where statusline skips, malformed/uppercase id). Collapsing safely requires *first* making `.sid` the single read source in **both** the watcher AND the cycler — a moderate refactor, not a delete.

---

## 1. The identity resolution chain as it exists NOW

Four sources still exist; the overlay narrowed *who reads which* but did not remove any:

| Source | Writer | Read by | Role today |
|---|---|---|---|
| `.sid` (`sessionid.go`) | SessionStart hook only | `ReadCtxFile` overlay (`gauge.go:59-61`); watcher re-check (`watcher.go:692`); `restartnow.go:94` (via ReadCtxFile) | **Authoritative WHEN present + UUIDv4** |
| `.ctx.SessionID` (gauge) | `keeper-statusline.sh` + `heartbeat.go` (multi-writer) | everywhere via `ReadCtxFile`/`ReadGaugeFn` | FALLBACK identity + the value the cycler polls |
| `.managed` (`keeper.go`) | watcher latch/adopt (`watcher.go:697,717`); cycler clear/rebind (`cycle.go:838,904`) | watcher ownership check (`watcher.go:681`); heartbeat (`heartbeat.go:161`) | the persisted binding |
| tmux session name | derived (SHA) | `tmuxresolve.go` | pane targeting only — not contested for *session* identity |

**Key point:** `ReadCtxFile` (`gauge.go:59`) folds `.sid` into `cf.SessionID` **only if `isPrimarySID` (lowercase UUIDv4)**. When `.sid` is absent/malformed, `cf.SessionID` stays the raw gauge value. So everything downstream still consumes the *gauge fallback* on the unhappy path. `.sid` is authoritative for the **happy path only**.

### Competing/heuristic paths still in `watcher.go`

- **Foreign-session branch** (`watcher.go:684-712`): on `.managed` ≠ `cf.SessionID`, re-reads `.sid` (`ReadSidFn`, line 692) to decide adopt-vs-foreign. Three files, one mismatch, three branches — exactly R1's finding, **unchanged**.
- **Adopt** (`watcher.go:697`): rewrites `.managed` from live `.sid`.
- **Latch** (`watcher.go:713-721`): first-seen `cf.SessionID` → `.managed`. This is the value the **gauge** supplied if `.sid` was absent.
- **`isUppercaseUUID`** (`watcher.go:502`) — still present; still referenced by `heartbeat.go:161`.
- **`isUUIDv7`** guard in cycler `waitForNewSessionID` (`cycle.go:1209`) and `heartbeat.go:161`.

### The cycler re-introduces a gauge identity path

`waitForNewSessionID` (`cycle.go:1191-1214`) polls **`ReadGaugeFn`** (default `ReadCtxFile`) and accepts `cf.SessionID != prevSID && !isUUIDv7(...)`. Because it goes through `ReadCtxFile`, it *does* get the `.sid` overlay — but only when `.sid` already flipped to the new UUIDv4. Right after `/clear` the SessionStart hook hasn't necessarily re-fired, so this commonly resolves the **raw gauge** value. Then `SetManagedSessionFn` (`cycle.go:904`) writes that into `.managed`, and the abort path *clears* `.managed` to `""` (`cycle.go:838`). So the single-writer invariant for `.managed` is still violated in ≥4 places (watcher adopt+latch, cycler rebind+clear) — **unchanged from R1**.

---

## 2. Dead code, or load-bearing?

**Load-bearing — not dead.** Concretely:

- **Latch (`watcher.go:713`) fires whenever `.sid` is absent/malformed.** Crews: R1 §4 confirmed crews have no guaranteed `.sid`/gauge wiring (env-var convention, no SessionStart-hook guarantee). For those, the *only* way `.managed` ever gets bound is the gauge-fed latch. Delete it and crews never bind → permanent `no_gauge`/no monitoring.
- **Adopt (`watcher.go:697`) is the documented stale-`.managed` recovery** (MEMORY: "auto-recovers after ~3 ticks"). Removing it strands a session in `foreign_session` after any external `/clear` that the cycler didn't drive.
- **Cycler gauge re-resolution (`cycle.go:1209`) is the post-`/clear` rebind.** `.sid` cannot replace it directly because the hook timing isn't guaranteed within `ClearSettle`.

So the heuristics are *redundant on the happy path* (where `.sid` is present) but *sole-path on the unhappy paths*. That is why this is a refactor, not a deletion.

---

## 3. Concrete minimal change to collapse to ONE source

The collapse must (a) make `.sid` the single identity *read* and (b) make the SessionStart hook the single identity *write*, then delete the gauge-derived branches. Do it in this order:

**Step A — guarantee `.sid` is written for every watched session (prerequisite).**
The collapse is only safe once `.sid` is *always* present. Wire the SessionStart hook for crews (the gap in R1 §4) and/or have `keeper enable`/`crew start` seed `.sid` from the launch session id. Until `.sid` is guaranteed, the gauge fallback CANNOT be deleted. *(This overlaps the crews-have-no-watcher item — see sibling report; the two should land together.)*

**Step B — single accessor.** Introduce `keeper.ResolveIdentity(projectDir, agent) (sid string, ok bool)`: reads `.sid` only; returns `ok=false` when absent/not-`isPrimarySID`. Make `ReadCtxFile` stop overlaying identity (keep it for pct/tokens only) — identity no longer rides the gauge struct.

**Step C — watcher.** Replace the binding block (`watcher.go:665-721`) with: `sid, ok := ResolveIdentity(...)`; if `!ok` → treat as no-gauge `continue`; else compare against `.managed`, adopt-on-mismatch (still write-once-on-change), no gauge latch. **Delete:** the `else if managedSID == ""` gauge-latch arm (713-721) and the `ReadSidFn` re-read at 692 (identity already came from `.sid`).

**Step D — cycler.** `waitForNewSessionID` polls `ResolveIdentity` (i.e. `.sid`) instead of `ReadGaugeFn`. Now post-`/clear` rebind reads the same authoritative source. **Delete** the `isUUIDv7` guard there (a non-`isPrimarySID` value is already rejected).

**Step E — delete now-orphaned heuristics:** `isUppercaseUUID` (`watcher.go:502`) and the `isUUIDv7`/`isUppercaseUUID` guards in `heartbeat.go:161` once heartbeat stamps the resolved `.sid` instead of `.managed`.

**Tests that guard it:** `sessionid_test.go` (isPrimarySID matrix — keep), `watcher_test.go` foreign/adopt/latch cases (rewrite to assert via `.sid` not gauge), `sessionstart_hook_integration_test.go` (extend to crews — the new prerequisite), `cycle_twin_*_integration_test.go` (post-`/clear` rebind now via `.sid`). Add one test: `.sid` absent → watcher emits no_gauge and does NOT latch a gauge id.

**Risk: MODERATE.** Touches the two hottest, most-bead-scarred files (`watcher.go`, `cycle.go`). The hard gate is Step A: deleting the gauge fallback before `.sid` is universally written would silently kill crew/mobile monitoring. Step A is itself the unsolved crews item, so this collapse is *blocked on* the crew-watcher work, not independent of it.

---

## 4. Does restart-now / await-ack reintroduce an identity path?

- **restart-now** (`restartnow.go:94-104`): reads identity via `ReadCtxFile` (so it gets the `.sid` overlay) and *requires* `IsPrimarySID(sid)` — refuses otherwise. This is **the correct model already**: identity-from-`.sid`, hard-reject the fallback. It does NOT add a new source; it's actually a template for Steps B/C. Good.
- **await-ack** (`awaitack.go`): no identity reads at all (grep clean) — pure pane ACK handshake. No reintroduction.

So the recently-merged code is **net-neutral-to-positive** on identity: restart-now demonstrates the target accessor pattern; await-ack adds nothing to undo.

---

## Cited anchors
- `gauge.go:49-61` — `.sid` overlay (happy-path-only authority).
- `sessionid.go:30,52,61,70` — `.sid` read + `isPrimarySID`/`isUUIDv4`.
- `watcher.go:502` (`isUppercaseUUID`), `:665-721` (binding block: foreign 684-712, adopt 697, latch 713-721, `ReadSidFn` re-check 692).
- `cycle.go:838` (clear `.managed` to ""), `:904` (rebind), `:1191-1214` (`waitForNewSessionID` polls gauge + `isUUIDv7` 1209).
- `heartbeat.go:161` — `isUUIDv7`/`isUppercaseUUID` guards; stamps `.managed`.
- `restartnow.go:94-104` — identity-from-`.sid` + hard `IsPrimarySID` reject (target pattern).
- `cmd/harmonik/keeper_cmd.go` — still **no `--session-id` flag** (identity never injected at launch; R1 finding unchanged).
