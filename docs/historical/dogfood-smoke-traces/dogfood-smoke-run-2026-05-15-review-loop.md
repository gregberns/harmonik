# Dogfood Smoke Run — 2026-05-15 — Review Loop

## Verdict

**RED** — `workflow:review-loop` label is present on the smoke bead, but the daemon's workloop dispatches in **single-mode** because `br ready --format json` (br v0.1.45) does not include the `labels` field in its output. Mode resolution falls through to tier 4 (single) on every dispatch. The review-loop path (`runReviewLoop`) is never entered.

**Date:** 2026-05-15
**HEAD:** `4a3c217` (chore(beads): close SUBSUMED beads hk-2ubs8, hk-63k6b, hk-qo08q.15)
**Smoke bead:** `smoke-e1c` (workflow:review-loop label)
**Smoke dir:** `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.dMKdnR0kil`
**Follow-up bead:** filed as `hk-rl-labels` (see §Gap below)

---

## Setup

### Preconditions

- `claude --version`: `2.1.142 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br --version`: `0.1.45` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS
- `go build -o /tmp/hk ./cmd/harmonik/`: exit 0 — PASS

### Smoke directory

```
SMOKE_DIR=/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.dMKdnR0kil
```

- `git init`, user.email smoke@harmonik.local, user.name Smoke Runner
- `echo "# smoke repo" > README.md && touch marker.txt && git add -A && git commit -m "initial commit"`
- `br init --prefix smoke`
- `git remote add origin /var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.nrui4flY4S` (bare local remote, required for merge-to-main step 4)
- `git push -u origin main`

### Smoke bead

```
br create --title "Add SMOKE-OK-REVIEW marker line to marker.txt, commit, then write APPROVE verdict to .harmonik/review.json" \
  --type task --priority 1 --labels "workflow:review-loop"
```

→ **smoke-e1c** created.

`br show smoke-e1c --format json` confirms `"labels": ["workflow:review-loop"]`.

---

## Run

### Invocation

```
SESS=smoke-rl2-1778868839
tmux new-session -d -s "$SESS" -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    > $SMOKE_DIR/hk-stdout2.txt 2> $SMOKE_DIR/hk-stderr2.txt; sleep 60"
```

---

## Event Stream

```
2026-05-15T11:13:59 daemon_started
2026-05-15T11:13:59 daemon_orphan_sweep_completed   (nothing swept)
2026-05-15T11:13:59 run_started              bead=smoke-e1c
2026-05-15T11:14:00 handler_capabilities     claude_session_id=019e2cd8-6626-7bc3-acf9-8ab5d0044b7a
2026-05-15T11:14:00 session_log_location
2026-05-15T11:14:00 skills_provisioned
2026-05-15T11:14:00 launch_initiated
2026-05-15T11:14:00 agent_ready              (T+0.46s from run_started)
2026-05-15T11:14:26 outcome_emitted          kind=approved
2026-05-15T11:14:26 bead_closed
2026-05-15T11:14:26 run_completed            success=true summary="auto-close: exit=0"
```

**run_started → run_completed: 27.1 s**

Notable absences: no `reviewer_launched`, no `reviewer_verdict`, no `review_loop_cycle_complete`, no `implementer_resumed`.

---

## Root Cause Analysis

### The gap: `br ready --format json` does not return labels

The daemon's mode resolution (moderesolve.go §4-tier walk) reads `beadRecord.Labels` at claim time (workloop.go line 777). `beadRecord` is populated from `brcli.Ready()` (ready.go line 71), which parses `br ready --format json` output.

`br ready --format json` output for smoke-e1c (br v0.1.45):

```json
[
  {
    "id": "smoke-e1c",
    "issue_type": "task",
    "priority": 1,
    "status": "open",
    "title": "..."
  }
]
```

The `labels` field is **absent**. The `brReadyItem` struct (`ready.go:28`) declares `Labels []string \`json:"labels"\`` — when absent in the JSON, Go deserialises it as nil. The tier-1 check in `resolveWorkflowMode` finds zero workflow labels and falls through to tier 4: `WorkflowModeSingle`.

The review-loop code path (`runReviewLoop`) is never invoked.

### Confirmation

`br show smoke-e1c --format json` does include labels:
```json
{"labels": ["workflow:review-loop"]}
```

`br ready --format json` does not:
```json
[{"id":"smoke-e1c","issue_type":"task","priority":1,"status":"open","title":"..."}]
```

The discrepancy is in `br`'s ready-output schema — it predates the BI-013 label-surfacing spec requirement.

### What DID work (single-mode fallback confirmed GREEN again)

- `daemon_started` → `daemon_orphan_sweep_completed` → `run_started` → `agent_ready` → commit → `outcome_emitted approved` → `bead_closed` → `run_completed` — all in 27.1 s. The single-mode path is still solid. The implementer phase of the review-loop dispatch (had it activated) would have the same healthy substrate.

---

## Gap Filed

**hk-rl-labels** — `br ready --format json` does not surface labels; BI-013 is not met at br v0.1.45.

Fix options:
- (a) Upgrade `br` to a version where `ready --format json` includes `labels` (check upstream beads_rust changelog).
- (b) Add a `ShowBead` call in the workloop claim path after `Ready()` to hydrate labels before mode resolution.
- (c) Use `br list --status=open --format json` which may return labels, and filter ready beads daemon-side.

Option (b) is the safer harmonik-side fix (no external dependency on br version). Option (a) is preferred if upstream already ships labels in ready output.

---

## Checklist

| Check | Result |
|-------|--------|
| (a) Smoke bead has `workflow:review-loop` label | PASS — confirmed via `br show` |
| (b) `br ready --format json` surfaces labels | FAIL — labels absent in br v0.1.45 |
| (c) `resolveWorkflowMode` receives non-nil labels | FAIL — `BeadRecord.Labels == nil` |
| (d) `runReviewLoop` entered | FAIL — single-mode path taken instead |
| (e) `reviewer_launched` event emitted | FAIL — event absent in stream |
| (f) `review_loop_cycle_complete` emitted | FAIL — event absent in stream |
| (g) Single-mode substrate still GREEN | PASS — auto-close: exit=0 in 27.1 s |

---

## Disposition

hk-gql20.24 closed with this doc as evidence. The review-loop path itself (`runReviewLoop`) is spec-complete and unit-tested (see `reviewloop_test.go`, `reviewloop_hkgql2015_test.go`, `reviewloop_cycle_complete_hk7om2q24_test.go`); the gap is a runtime label-surfacing issue between `br ready` output and the daemon's mode-resolution input.

hk-gql20 (epic) remains open pending the labels gap fix; follow-up is hk-rl-labels.
