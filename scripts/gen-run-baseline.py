#!/usr/bin/env python3
# scripts/gen-run-baseline.py — synthesize the frozen daemon-run baseline log
# and the two RSM9 acceptance fixtures (RT10).
#
# There is no committed real events.jsonl to extract from (the daemon baseline
# log is a gitignored work-project artifact). This generator is the PROVENANCE
# for a small, self-contained, deterministic baseline that exercises every
# run-lifecycle stratum so the corpus + goldens are reproducible in CI. Re-run
# after a real baseline capture to replace the synthetic source (the extractor
# and Go checkers are agnostic to the source's origin).
#
# Emits (deterministic — fixed UUID seeds, fixed timestamps):
#   testdata/daemon-runs/baseline-2026-07-14/source/events.jsonl
#   testdata/daemon-runs/fixtures/clean-run.jsonl   (resumed → terminal; RSM9 passes)
#   testdata/daemon-runs/fixtures/hung-run.jsonl    (resumed → silence; RSM9 flags)
#
# run_id and event_id are real UUIDs so the Go replay harness (core decode +
# eventbus.ScanAfter) round-trips them. event_id high bytes encode a global
# sequence so the harness's EventID-sort is deterministic.
import json
import os
import uuid

BASE_TS = "2026-07-14T12:00:00Z"
SEQ = [0]


def eid():
    SEQ[0] += 1
    b = bytearray(16)
    b[0] = (SEQ[0] >> 8) & 0xFF
    b[1] = SEQ[0] & 0xFF
    b[6] = 0x70  # version 7 nibble (cosmetic; harness sorts on raw bytes)
    b[8] = 0x80  # RFC 4122 variant
    return str(uuid.UUID(bytes=bytes(b)))


def rid(n):
    # Deterministic run_id from a small integer.
    b = bytearray(16)
    b[15] = n
    b[6] = 0x70
    b[8] = 0x80
    return str(uuid.UUID(bytes=bytes(b)))


def ev(etype, payload):
    return {
        "event_id": eid(),
        "schema_version": 1,
        "type": etype,
        "timestamp_wall": BASE_TS,
        "source_subsystem": "internal/daemon",
        "payload": payload,
    }


def run_started(r, mode):
    return ev("run_started", {
        "run_id": r, "workflow_id": rid(200), "workflow_version": "v1",
        "workspace_path": "/w", "input_ref": "ref", "workflow_mode": mode,
    })


def launch(r):
    return ev("launch_initiated", {"run_id": r, "claude_session_id": "sess"})


def ready(r):
    return ev("agent_ready", {"run_id": r, "claude_session_id": "sess"})


def resumed(r, it):
    return ev("implementer_resumed", {
        "run_id": r, "workflow_mode": "review-loop", "session_id": rid(210),
        "claude_session_id": "sess", "iteration_count": it,
        "prior_verdict_summary": "prior",
    })


def rl_complete(r, it):
    return ev("review_loop_cycle_complete", {
        "run_id": r, "workflow_mode": "review-loop",
        "final_iteration_count": it, "completion_reason": "approved",
    })


def completed(r, mode):
    return ev("run_completed", {
        "run_id": r, "terminal_state_id": rid(220),
        "ended_at": BASE_TS, "workflow_mode": mode,
    })


def failed(r, reason, cls):
    return ev("run_failed", {
        "run_id": r, "failure_class": cls, "ended_at": BASE_TS,
        "reason": reason, "last_checkpoint": "",
    })


def ready_timeout(r):
    return ev("agent_ready_timeout", {
        "run_id": r, "claude_session_id": "sess", "timeout_ms": 150000,
    })


def run_stale(r):
    return ev("run_stale", {
        "run_id": r, "bead_id": "hk-x", "age_seconds": 600,
        "last_event_type": "agent_ready", "emit_count": 1,
    })


# --- The baseline corpus: one run per stratum, all CLEAN (post-fix streams). ---
baseline = []
# single (clean)
r = rid(1)
baseline += [run_started(r, "single"), launch(r), ready(r), completed(r, "single")]
# review-loop-resume (clean: resumed → review_loop_cycle_complete terminal)
r = rid(2)
baseline += [run_started(r, "review-loop"), launch(r), ready(r), resumed(r, 2), rl_complete(r, 2)]
# dot (clean)
r = rid(3)
baseline += [run_started(r, "dot"), launch(r), ready(r), completed(r, "dot")]
# merge-failure (clean terminal: run_failed with a merge reason)
r = rid(4)
baseline += [run_started(r, "review-loop"), launch(r), ready(r),
             failed(r, "merge conflict on main", "structural")]
# run-stale (failure-class discharges liveness; a valid terminal-family signal)
r = rid(5)
baseline += [run_started(r, "single"), launch(r), ready(r), run_stale(r)]
# hung-relaunch (resumed then agent_ready_timeout → fail-closed; RSM9 CLEAN
# because the failure-class event discharges the liveness obligation)
r = rid(6)
baseline += [run_started(r, "review-loop"), launch(r), resumed(r, 2), ready_timeout(r)]

# --- The two RSM9 acceptance fixtures. ---
# clean: resumed run that reaches a terminal → RSM9 passes.
cr = rid(100)
clean_fixture = [run_started(cr, "review-loop"), launch(cr), ready(cr), resumed(cr, 2), rl_complete(cr, 2)]
# hung: resumed run with NO terminal and NO failure-class event → RSM9 flags
# (the resume-hang gap the fix closes; "silence is forbidden").
hr = rid(101)
hung_fixture = [run_started(hr, "review-loop"), launch(hr), ready(hr), resumed(hr, 2)]


def write_jsonl(path, events):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        for e in events:
            fh.write(json.dumps(e, separators=(",", ":"), sort_keys=True) + "\n")


write_jsonl("testdata/daemon-runs/baseline-2026-07-14/source/events.jsonl", baseline)
write_jsonl("testdata/daemon-runs/fixtures/clean-run.jsonl", clean_fixture)
write_jsonl("testdata/daemon-runs/fixtures/hung-run.jsonl", hung_fixture)
print("wrote baseline (%d events, 6 runs) + 2 fixtures" % len(baseline))
