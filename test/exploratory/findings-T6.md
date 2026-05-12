# Exploratory Testing T6 — Scale and Shape Stress

**Tester:** T6 (agent `ad5c0f07f42a910da`)
**Date:** 2026-05-12
**Branch:** `worktree-agent-ad5c0f07f42a910da`
**Spec references:** `specs/event-model.md` EV-001; `specs/process-lifecycle.md` PL-020; `MVH_ROADMAP.md`
**Test file:** `internal/daemon/t6_scale_shape_test.go`

---

## Scenario coverage

| Scenario | Test | Result | Wall-clock |
|---|---|---|---|
| S1: 10 ready beads — sequential drain | `TestT6_10BeadSequentialDrain` | PASS | 4.97s (0.50s/bead) |
| S2: Large bead body (100KB at br ceiling) | `TestT6_1MBBeadBody` | PASS | 0.80s |
| S3: Empty / near-empty bead body | `TestT6_EmptyAndNearEmptyBody` | PASS | 1.36s |
| S4: Unicode-heavy body (CJK, emoji, RTL) | `TestT6_UnicodeHeavyBody` | PASS | 3.50s |
| S5: Large worktree base (1000 subdirs) | `TestT6_LargeWorktreeBase` | PASS | 2.37s |
| S6: Concurrent `br create` while daemon running | `TestT6_ConcurrentBeadCreate` | PASS | 7.85s (4 beads) |

All 6 tests pass. Build clean (`go build ./...`), `gofmt -d` empty diff, `go vet` clean.

---

## Findings

### F-T6-001 — INFO: JSONL envelope missing EV-001 required fields (duplicate of T5 F-001)

**Bead:** `hk-d000q` (CLOSED as DUPLICATE of `hk-0pyuk` filed by T5)
**Severity:** INFO for T6 scope — already filed as HIGH by T5 as `hk-0pyuk`.

T6 independently confirmed via the envelope-format probe: 4 JSONL lines written for a 1-bead run, none containing `type`, `event_id`, or `schema_version`. See T5 findings (F-001) for full root cause analysis. The finding was independently reproduced here; bead closed as duplicate.

**Impact on T6 tests:** The `t6CountJSONLEvents` helper cannot use event type strings to detect events in JSONL. Worked around by checking distinctive payload field names:
- `run_started` -> `"workspace_path"` (workloopRunStartedPayload)
- `run_completed` -> `"auto-close"` or `"auto-reopen"` in the summary field

---

### F-T6-002 — INFO: `br` enforces a 100KB body validation ceiling; 1MB test blocked by two limits

**Bead:** `hk-4oyc2` (OPEN, labels: `exploratory-finding,tester-T6`)
**Severity:** INFO — known platform constraint. No crash or data loss.

**Repro:**
```
br create "test" --body "$(python3 -c "print('A' * 1048576, end='')")"
# -> fork/exec: argument list too long  (OS ARG_MAX = 1048576)

br create "test" --body "$(python3 -c "print('A' * 524288, end='')")"
# -> Error: Validation failed: description: exceeds 100KB
```

The originally scoped 1MB body test is blocked by two independent limits:
1. **macOS `ARG_MAX` = 1,048,576 bytes.** A 1MB `--body` argument approaches the per-execve argument-list size limit. `exec` returns `E2BIG`.
2. **`br` enforces a 100KB body ceiling.** Rejects anything over 100KB with `Validation failed: description: exceeds 100KB`.

**Test resolution:** S2 was retargeted to 100KB (exactly at the `br` ceiling). The 100KB body round-trips correctly through `br show --format json` (102,400 bytes in the `description` field).

**Recommendation:** `br create --body-file <path>` would bypass both limits for large work specs.

---

### F-T6-003 — INFO: `br show --format json` uses `description` field, not `body`

**Bead:** `hk-nmiww` (OPEN, labels: `exploratory-finding,tester-T6`)
**Severity:** INFO — naming inconsistency, not a data-integrity issue.

`br create` accepts `--body` (or `--description`) as the flag name for the bead body text. `br show --format json` returns that same text under the key `"description"`, not `"body"`. The two halves of the create/show round-trip use different names for the same field.

**Impact:** Handlers that call `br show --format json` to read the bead work spec must use `.description`, not `.body`. Caught two test assertions in T6.

---

### F-T6-004 — INFO: Daemon work loop never reads the bead body

**Bead:** `hk-33tcf` (OPEN, labels: `exploratory-finding,tester-T6`)
**Severity:** INFO — architecturally intentional; documented here for completeness.

The `runWorkLoop` dispatch path calls `brAdapter.ClaimBead(beadID)` but never calls `br show <beadID>` to retrieve the body content. The handler subprocess receives only its binary + working directory (the git worktree path). The handler is responsible for calling `br show <beadID>` itself to obtain the work spec.

**Verified:** A bead with a 100KB body and a bead with an empty body both dispatch and close in ~0.75s — body content has no effect on dispatch latency.

---

## Performance observations

All timings from darwin/arm64.

| Metric | Value | Notes |
|---|---|---|
| Per-bead dispatch (happy path) | ~0.50-0.80s | ClaimBead + git worktree add + handler.sh exit=0 + CloseBead |
| 10-bead sequential drain | 4.97s total | Linear; no regression between bead 1 and bead 10 |
| git worktree add with 1000 subdirs | 2.37s total | No stall observed; 30s warning threshold not triggered |
| Late-arriving bead pick-up latency | <=2.0s after creation | Bounded by workloopPollInterval=2s; beads created 1s after daemon start appeared within next poll cycle |

**Wall-clock is acceptable for MVH.** The 2-second poll interval is the dominant latency factor.

---

## Methodology

- Binary built: `go build ./...` -- clean
- Tests run: `go test ./internal/daemon/ -run "TestT6_" -v -timeout 180s`
- Pattern from: `internal/daemon/smoke_test.go` -- fixture helpers reused in `t6FixtureDir`
- Fixture corpus: `test/exploratory/fixtures/seed.sh --count 10` verified before running
- No source files modified. Test file added: `internal/daemon/t6_scale_shape_test.go`

---

## Bead summary

| Bead ID | Severity | Status | Summary |
|---|---|---|---|
| `hk-d000q` | INFO | CLOSED (dup) | JSONL envelope missing EV-001 fields -- duplicate of T5 `hk-0pyuk` |
| `hk-4oyc2` | INFO | OPEN | br 100KB body ceiling + ARG_MAX blocks 1MB test |
| `hk-nmiww` | INFO | OPEN | br show JSON uses `description` not `body` |
| `hk-33tcf` | INFO | OPEN | Daemon work loop never reads bead body content |
