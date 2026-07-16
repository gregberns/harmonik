# core-loop-assert.jq — the core-loop-proof assertion library (T2, hk-1yxhh).
#
# THE LOAD-BEARING CONTRACT. Consumes a captured event stream + an expected-cell spec
# and emits typed per-gap pass/fail records. This is the contract the Phase-2 scripted-
# twin must satisfy, so it is kept clean and additive-only.
#
# INVOCATION (the matrix runner and the per-gap tasks T4-T8 both call it this way):
#   jq -n \
#     --slurpfile events <captured.ndjson> \
#     --argjson  spec    "$(cat <cell-spec.json>)" \
#     -f scripts/core-loop-assert.jq
#
#   $events      — the array of NDJSON event objects captured for this cell (subscribe
#                  --json); --slurpfile binds one array element per NDJSON line.
#   $spec        — one expected-cell object (see scenarios/core-loop-proof/expected-cell.example.json).
#   $ref_events  — REQUIRED (may be null). The LOCAL reference stream (as a JSON array)
#                  used by gap2 remote==local parity; null for non-remote cells or when no
#                  tcp:// worker was reachable. Pass `--argjson ref_events null` when unused.
#
# OUTPUT — a JSON array of result records, one per gap the spec lists:
#   { "gap": "gap1", "verdict": "pass"|"fail"|"pending", "detail": "<human string>" }
#
#   pass    — the gap's contract held.
#   fail    — the gap's contract was violated (a real red).
#   pending — the gap's assertion is not implemented on THIS branch yet (T4-T8 land them).
#             pending is NEVER a pass; the matrix runner counts it distinctly so a partial
#             assertion set can never masquerade as full green (T9 gates on zero pending).
#
# SELECTION RULE: when multiple events of a type match (e.g. a review-loop retries and
# re-emits harness_selected/model_selected), the LAST one wins (`[-1]`) — it reflects the
# effective final launch. T4-T8 extending this contract should preserve last-wins.
#
# GAP MAP: gap1 model-reaches-harness (T2, DONE) · gap2 remote==local (T7) ·
#          gap3 provider-through-sandbox (T6) · gap4 dispatch field fidelity (T5) ·
#          gap5 claude worktree->agent_ready (T8).

# --- helpers ---------------------------------------------------------------
def events: $events;

# all events of a given type
def of_type($t): events | map(select(.type == $t));

# the payload of an event object (payload wrapper is optional per emitter)
def pl: (.payload // .);

# result-record constructor
def result($gap; $verdict; $detail): { gap: $gap, verdict: $verdict, detail: $detail };

# --- gap1 — model reaches the harness per family (C4) -----------------------
# Contract:
#   (a) a harness_selected event for the seed bead exists, with agent_type == the
#       expected harness family and tier == the expected precedence tier;
#   (b) a model_selected event exists whose harness == expected, and whose model ==
#       the expected model when the spec pins one (codex pins none → skip the model check).
#   (c) NODE-PIN NO-LEAK (T4, hk-qa1oo): no model_selected event on this cell's harness
#       family carries a FOREIGN family's node-`model=` pin. spec.expect.model_selected
#       .no_leak_models lists the models that must never reach this harness (e.g. a pi
#       cell forbids "claude-opus-4-8" — the exact hk-lfrub/hk-pkugu regression where a
#       claude node model= pin leaked into a pi launch). Any hit is a leak → fail.
def assert_gap1:
  ($spec.expect.harness_selected // {}) as $eh
  | ($spec.expect.model_selected  // {}) as $em
  | ($em.no_leak_models // []) as $forbidden
  | (of_type("harness_selected") | map(pl) | map(select(.bead_id == $spec.seed_bead))) as $hs
  | (of_type("model_selected")   | map(pl) | map(select(.harness == ($em.harness // $eh.agent_type)))) as $ms
  | ($ms | map(.model) | map(select(. as $m | $forbidden | index($m))) | unique) as $leaks
  | if ($hs | length) == 0
    then result("gap1"; "fail"; "no harness_selected event for seed bead \($spec.seed_bead)")
    elif ($eh.agent_type != null and ($hs[-1].agent_type != $eh.agent_type))
    then result("gap1"; "fail"; "harness_selected.agent_type=\($hs[-1].agent_type) != expected \($eh.agent_type)")
    elif ($eh.tier != null and ($hs[-1].tier != $eh.tier))
    then result("gap1"; "fail"; "harness_selected.tier=\($hs[-1].tier) != expected \($eh.tier) (harness pin leaked from wrong precedence tier)")
    elif ($ms | length) == 0
    then result("gap1"; "fail"; "no model_selected event for harness \($em.harness // $eh.agent_type)")
    elif ($leaks | length) > 0
    then result("gap1"; "fail"; "node-model pin LEAKED into harness \($em.harness // $eh.agent_type): forbidden model(s) \($leaks | join(",")) (cf pi-model-leak hk-lfrub/hk-pkugu)")
    elif ($em.model != null and ($ms[-1].model != $em.model))
    then result("gap1"; "fail"; "model_selected.model=\($ms[-1].model) != pinned \($em.model)")
    else result("gap1"; "pass"; "harness=\($hs[-1].agent_type) tier=\($hs[-1].tier) model=\(if ($ms[-1].model // "") == "" then "<uncontrolled>" else $ms[-1].model end)")
    end;

# --- gap2..gap5 — declared here, implemented by T4-T8 -----------------------
# Each returns pending (honest: not yet asserted on this branch), NOT pass. The
# per-gap task replaces the pending body with its real assertion over the same stream.
# --- gap2 — remote(tcp://) path == local path (C2) (T7, hk-wf9lv) -----------
# The same seed bead run through the remote (tcp://) runner must yield the SAME event-type
# sequence + terminal outcome as the local cell — no sandbox-wrap misapplied to tcp
# (hk-ybuts). Compares this remote cell's $events against the local reference $ref_events.
# SKIP-LOUD: when no local reference was captured (no reachable tcp:// worker), gap2 is
# `pending` (never a false pass) — the matrix runner surfaces the skip reason.
def norm_seq($evs):
  [ $evs[] | (.type // "") ]
  | map(select(. != "" and . != "agent_heartbeat" and . != "heartbeat"));
def terminal_of($evs):
  [ $evs[] | select(.type == "run_completed" or .type == "run_failed")
    | { t: .type, s: ((.payload.success) // .success // null) } ] | last;
def assert_gap2:
  ($spec.substrate // "") as $sub
  | ($ref_events) as $ref
  | if $sub != "remote"
    then result("gap2"; "pending"; "gap2 applies only to remote cells (substrate=\($sub))")
    elif $ref == null
    then result("gap2"; "pending"; "SKIP-LOUD: no local reference stream captured (no reachable tcp:// worker) — cannot prove remote==local")
    else norm_seq($events) as $rseq | norm_seq($ref) as $lseq
       | terminal_of($events) as $rterm | terminal_of($ref) as $lterm
       | if $rseq == $lseq and $rterm == $lterm
         then result("gap2"; "pass"; "remote event-type sequence + terminal outcome == local (\($rterm.t))")
         elif $rterm != $lterm
         then result("gap2"; "fail"; "remote terminal \($rterm) != local terminal \($lterm)")
         else result("gap2"; "fail"; "remote path diverges from local: event-type sequence mismatch (remote=\($rseq | length) local=\($lseq | length) events)")
         end
    end;
# --- gap3 — provider comms through the sandbox (C3/C6) (T6, hk-i21pt) --------
# Proves the provider round-trip actually reached the sandbox and mutated the tree, AND
# that a degenerate provider reply (content:null / no edit) surfaces LOUDLY rather than
# closing green with no change (the hk-4ir08/hk-u69my silent-no-commit regression).
# Integrity holds iff EITHER a real change landed (implementer_phase_complete.commit_landed)
# OR the run failed explicitly (run_failed). The ONLY violation is a silent no-commit:
# a successful terminal with no commit landed. Enabled by spec.expect.provider.
def assert_gap3:
  ($spec.expect | has("provider")) as $on
  | (of_type("implementer_phase_complete") | map(pl) | map(.commit_landed == true) | any) as $committed
  | (of_type("run_completed") | map(pl) | map(select((.bead_id // null) == $spec.seed_bead and .success == true)) | length > 0) as $succeeded
  | (of_type("run_failed")    | map(pl) | map(select((.bead_id // null) == $spec.seed_bead)) | length > 0) as $failed
  | if ($on | not)
    then result("gap3"; "pending"; "no expect.provider in spec — add it to assert gap3")
    elif $committed
    then result("gap3"; "pass"; "provider produced a real HEAD change (commit_landed)")
    elif $failed
    then result("gap3"; "pass"; "no commit, but an explicit run_failed surfaced (content-null handled loudly, not silent)")
    elif $succeeded
    then result("gap3"; "fail"; "SILENT no-commit: run_completed success with no commit_landed — provider reply produced no change yet no failure surfaced (cf hk-4ir08/hk-u69my)")
    else result("gap3"; "fail"; "no commit, no terminal for seed bead \($spec.seed_bead) — provider round-trip incomplete")
    end;
# --- gap4 — queue-submit → dispatch field fidelity (C7) (T5, hk-bkn5a) -------
# A fully-specified queue item (workflow_ref, workflow_mode, model, harness) must reach
# the dispatched run with every field intact — in particular workflow_mode must NOT be
# silently forced to review-loop (the hk-u6zp/hk-y3o51 hardcoded-override regression).
# Cross-event: model/harness fidelity is gap1's job; gap4 owns the run_started dispatch
# fields. spec.expect.dispatch.workflow_mode = the submitted mode; .workflow_id_present
# = require workflow_ref to have resolved to a real (non-zero) workflow_id.
def assert_gap4:
  ($spec.expect.dispatch // {}) as $ed
  | (of_type("run_started") | map(pl) | map(select((.bead_id // null) == $spec.seed_bead))) as $rs
  | (($rs[-1].workflow_id // "") | tostring) as $wid
  | if ($ed == {})
    then result("gap4"; "pending"; "no expect.dispatch in spec — add {workflow_mode, workflow_id_present} to assert gap4")
    elif ($rs | length) == 0
    then result("gap4"; "fail"; "no run_started event for seed bead \($spec.seed_bead)")
    elif ($ed.workflow_mode != null and ($rs[-1].workflow_mode != $ed.workflow_mode))
    then result("gap4"; "fail"; "run_started.workflow_mode=\($rs[-1].workflow_mode) != submitted \($ed.workflow_mode) (hardcoded review-loop override?)")
    elif ($ed.workflow_id_present == true and ($wid == "" or ($wid | test("^0+(-0+)*$"))))
    then result("gap4"; "fail"; "run_started.workflow_id is absent/zero (\($wid)) — workflow_ref did not resolve at dispatch")
    else result("gap4"; "pass"; "workflow_mode=\($rs[-1].workflow_mode) workflow_id=\($wid)")
    end;
# --- gap5 — claude worktree startup → agent_ready (C8/PR-19) (T8, hk-4vwlx) --
# A real git-worktree claude launch must reach agent_ready past the folder-trust /
# permissions / onboarding modals, with NO agent_ready_timeout, agent_ready_stall_detected,
# post_agent_ready_hang, or launch_stall_detected. Flag-gated at the runner (cap-thrift);
# the assertion is enabled by spec.expect.agent_ready.
def assert_gap5:
  ($spec.expect | has("agent_ready")) as $on
  | (of_type("agent_ready")                 | length > 0) as $ready
  | (of_type("agent_ready_timeout")         | length > 0) as $timeout
  | (of_type("agent_ready_stall_detected")  | length > 0) as $stall
  | (of_type("post_agent_ready_hang")       | length > 0) as $hang
  | (of_type("launch_stall_detected")       | length > 0) as $lstall
  | if ($on | not)
    then result("gap5"; "pending"; "no expect.agent_ready in spec — add it to assert gap5")
    elif $timeout then result("gap5"; "fail"; "agent_ready_timeout — startup never reached AgentReady")
    elif $stall   then result("gap5"; "fail"; "agent_ready_stall_detected during startup")
    elif $lstall  then result("gap5"; "fail"; "launch_stall_detected — launch wedged before AgentReady")
    elif $hang    then result("gap5"; "fail"; "post_agent_ready_hang after AgentReady")
    elif ($ready | not) then result("gap5"; "fail"; "no agent_ready reached (gated by folder-trust/permissions/onboarding modal?)")
    else result("gap5"; "pass"; "agent_ready reached; no timeout/stall/hang")
    end;

# --- t10 — branch-targeting acceptance (KNOWN-RED today, hk-lgykq) -----------
# A bead directed at integration branch X must LAND on X, not main. Asserts the merged
# workspace_merge_status.target_branch == spec.expect.lands_on. This REDs today because
# per-bead/DOT integration-branch targeting is DEAD CODE (LandsOn/landTaskBranch not wired
# into the live workloop merge — internal/daemon/workloop.go:3153). The RED is the
# recorded evidence for hk-lgykq; when that lands, this flips to pass and the self-test's
# expected-fail row breaks loudly (prompting removal of the known-RED marker). t10 is
# deliberately NOT in the default cells' `gaps` — T9's green gate excludes it (mission:
# "known-RED cell, recorded, not a false-green pass").
def assert_t10:
  ($spec.expect.lands_on // null) as $want
  | (of_type("workspace_merge_status") | map(pl) | map(select(.status == "merged"))) as $m
  | if $want == null
    then result("t10"; "pending"; "no expect.lands_on in spec — set it to the intended integration branch")
    elif ($m | length) == 0
    then result("t10"; "fail"; "no merged workspace_merge_status event — nothing landed")
    elif ($m[-1].target_branch != $want)
    then result("t10"; "fail"; "KNOWN-RED (hk-lgykq): landed on '\($m[-1].target_branch)' != intended '\($want)' — per-bead integration targeting is dead code")
    else result("t10"; "pass"; "landed on intended branch '\($want)'")
    end;

# --- dispatcher ------------------------------------------------------------
# Run only the gaps the spec lists, in gap-number order, de-duplicated.
def run_gap($g):
  if   $g == "gap1" then assert_gap1
  elif $g == "gap2" then assert_gap2
  elif $g == "gap3" then assert_gap3
  elif $g == "gap4" then assert_gap4
  elif $g == "gap5" then assert_gap5
  elif $g == "t10"  then assert_t10
  else result($g; "fail"; "unknown gap id in spec: \($g)")
  end;

[ ($spec.gaps // []) | unique[] | run_gap(.) ]
