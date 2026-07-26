# CR-LEG FINDINGS — baseline-35e4b3b9 (cold independent code review)

Delegated per the D1 model: 5 adversarial bug-hunt subagents over the highest-churn source
clusters of `phase1-session-restart-substrate` (merge-base c9372014 → HEAD 35e4b3b9), findings
folded + independently spot-verified by the assessor. Static-confirmed unless noted; dynamic
repro pending for the live XT/LT suites (several have live cross-checks: G8 SSH-drop, S6
memory/fd growth, S5/G9 keeper sleep-gate).

## Filed beads (this session)

| Bead | P | File:line | Class | Status |
|---|---|---|---|---|
| hk-l5saf | P1 | workloop.go:2899 | dispatch: local-only queue item stranded 'dispatched' by hk-hs7ex guard → permanent group wedge | static-confirmed (control-flow read) |
| hk-3hozm | P1 | workloop.go:3493 | lifecycle: remote worker slot leak — refuse-before-launch early returns bypass ReleaseSlot defer → remote dispatch wedge after MaxSlots refusals | static-confirmed |
| hk-nddg1 | P1 | bootreconcile.go:157 | crash-recovery: main-only provenance → crew-queue double-dispatch after SIGKILL. **LATENT/pre-existing at merge-base — not a branch regression; I judge NON-gating for this branch; admiral adjudicates** | static-confirmed (subagent trace) |
| hk-cjqyn | P2 | tmuxsubstrate.go:2706 | **false-green**: remote runWait maps ANY WindowPanePID error → exitCodeClean(0); SSH drop mid-run → auto-close incomplete remote work (G8/H4/H5). Tier-0 category, remote-gated → P2; tier settled by live G8 | subagent repro |
| hk-9ngiv | P2 | watcher_hc011.go:429 | handshake: version-negotiation failure loses ErrProtocolMismatch sentinel (wrapped ErrStructural) → retry-spin | subagent repro |
| hk-hi53s | P2 | sessioncontext_chb023.go:421 | resource-leak: SessionIDInterceptor retains entire post-handshake stdout in memory (repro 330000/330083 bytes) | dynamic repro |
| hk-13ff4 | P2 | scrub.go:35 | secret-leak: scrub value group stops at first raw quote → secret tail with an escaped quote leaks into capture corpus | live repro |
| hk-bzol4 | P2 | watcher.go:1380 | keeper: self-hint injection bypasses the M3 sleep gate → wakes a parked session (warn advisory IS gated; hint isn't) | static-confirmed |
| hk-nqkoz | P3 | scrub.go:35 | over-redaction: unanchored 'auth'/'token' substrings redact author/authority/stop_token → lossy replay corpus | live repro |
| hk-n8yha | P3 | shell.go:236 | cycler: clearing deadline repeating-ticker-as-one-shot → hot-spin+gauge storm under non-aligned ClearConfirmBackstop (shipped defaults avoid) | subagent (medium) |
| hk-3edb1 | P3 | gitignorehygiene.go:414 | branch state: hygiene commit switches HEAD to harmonik/gitignore-init, never restores. LATENT-unwired | subagent (medium) |
| hk-btl1n | P3 | tmuxsubstrate.go:2516 | remote-kill asymmetry: no forceful remote pane-PID kill vs hardened local path (H8 itself correct) | subagent (medium) |
| hk-o66xy | P3 | sessioncontext_chb023.go:471 | handshake: caps with empty claude_session_id never fires callback → version_selected ACK never sent → possible 150s hang (narrow reach) | subagent (medium) |
| hk-3dn16 | P3 | keeper cycle_test.go | TEST-FRAGILITY: keeper wall-clock-margin tests flake under full-pkg -race (SystemClock not FakeClock). Confirms green-tree keeper flakes; deflake via FakeClock | subagent-confirmed (high) |

## Noted, NOT filed (low-confidence / latent-unwired / plausibly-intended)

- **leaselock.go:96** (os.Link rejects in-place renewal/re-claim) — no production caller; plausibly the intended single-holder contract. Low conf.
- **verdictexecutor_rc025a.go:181** (planErr path emits no reconciliation_verdict_malformed) — only unreachable because Valid() rejects first; the two must stay in lockstep. Low conf. (TestExecuteVerdict_LockReleasedOnError is satisfied — lock IS released by the top-level defer; not a bug.)
- **sessioncapture.go:113** (retention prune can delete current session dir on same-SessionID reopen) — self-limiting (fresh-UUID SessionIDs); low conf.
- **codexwire.go:372** (auto-declined server-response omits "jsonrpc":"2.0") — codex's own responses also omit it (non-strict peer); likely tolerated symmetrically. Low conf.

## Positive confirmations (subagents verified CORRECT — evidence for the verdict)

- **Merge-vs-strand / DOT gate wiring INTACT** — rlResult/dotResult.success→ModeSuccess else reopen; gate fires via gateHook; trailers amended. No false-green/collapse regression. Gate-efficacy + resume-reseed sources essentially untouched by the diff.
- **H13 warm-resume lost-wakeup NOT present** — keeper warm-resume is poll/level-triggered (no agent_ready channel handshake), so no lost-wakeup class exists. (Two independent subagents.)
- **Socket-bind-before-backoff ordering CORRECT** — bind goroutine (bootsocket.go:294) before sleepBootBackoff (daemon.go:1058).
- **hk-ky7ye supervisor fixes CORRECT** — errors.Is(os.ErrNotExist) unwrap + cold-start config gate; no adjacent non-unwrap regressions. PID-reuse SIGKILL re-verification + reconcile-lock flock-until-unlink both correct.
- **Remote-fault handling CORRECT where checked** — H8 remote Kill never signals a local PID; 5s caps timeout fires (no hang); negotiateWireVersion set-membership correct; duplicate agent_ready handled; HC field drift OK.
- **Keeper green-tree test failures = LOAD FLAKES** (confirmed: pass -race -count=20 isolated; only wall-clock-margin tests fail under full-package -race).

## Overall CR-leg read
High-quality, heavily-tested refactor (RT8/RT9 machine extraction + workspace hardening); most
changes IMPROVE correctness. The dispatch-loop P1s (hk-l5saf, hk-3hozm) are the sharpest
concerns — narrow-trigger but core-loop wedges. No merge-vs-strand or DOT-collapse false-green
regression found in cold review. Live XT/LT to confirm the dispatch P1s and the remote false-green.
