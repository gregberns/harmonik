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
#          gap5 claude worktree->agent_ready (T8) · gap6 dot round-trip same-model (D4) ·
#          gap7 codex-first DOT routing (hk-ity2u).

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

# --- t10 — branch-targeting acceptance (GIT-VERIFIED, D2) --------------------
# A bead directed at integration branch X must LAND on X, and main must NOT advance.
# This assertion is NOT event-driven: the workspace_merge_status event the daemon once
# aspired to emit is NEVER emitted (dead/aspirational — the merge writes git but no event),
# so an event-based check is structurally always RED. Instead the matrix runner verifies
# the landing directly from GIT (baseline vs. post-run tips of main + the target branch)
# and injects the branch that actually advanced as $spec._observed_lands_on. t10 simply
# compares intent (expect.lands_on) against that git-observed reality. Per-bead integration
# targeting is LIVE (hk-lgykq landed; proven by daemon E2E
# TestMergeToMain_PerBeadIntegrationTargetLandsOnBranch), so this is no longer known-RED.
def assert_t10:
  ($spec.expect.lands_on // null) as $want
  | ($spec._observed_lands_on // null) as $obs
  | if $want == null
    then result("t10"; "pending"; "no expect.lands_on in spec — set it to the intended integration branch")
    elif ($obs == null or $obs == "")
    then result("t10"; "pending"; "no ._observed_lands_on injected — runner did not git-verify the landing (need --assert + a git scratch)")
    elif ($obs == "main" and $want != "main")
    then result("t10"; "fail"; "main advanced — the change landed on 'main', not the intended '\($want)' (main must not move)")
    elif ($obs == "none")
    then result("t10"; "fail"; "nothing landed — neither 'main' nor '\($want)' advanced (merge did not run / bead did not close)")
    elif ($obs != $want)
    then result("t10"; "fail"; "landed on '\($obs)' != intended '\($want)' (git-verified)")
    else result("t10"; "pass"; "landed on '\($want)' (git-verified; main unchanged)")
    end;

# --- gap6 — dot review->implement round-trip, same model (D4) ----------------
# The dot cell must show a REAL model round-trip driven by the seed rubric, with BOTH the
# implementer and reviewer nodes on the same model (no claude leak into a pi dot run).
# PASS iff the captured stream shows, IN ORDER:
#   (a) a reviewer_verdict with verdict == REQUEST_CHANGES,
#   (b) an implementer RE-DISPATCH after that verdict — a node_dispatch_requested for the
#       implementer node (node_id matches /implement/) OR a second implementer_phase_complete,
#   (c) a reviewer_verdict APPROVE after the re-dispatch, then a terminal pass/close, AND
#   (d) SAME-MODEL: every model_selected.model in the run == the pinned model (spec
#       expect.model_selected.model, default ornith) — any other model is a leak → fail.
# Positional ordering is taken from the append-ordered capture stream (event index).
def assert_gap6:
  ($spec.expect.model_selected.model // "ornith") as $wantModel
  | ([ events[] | {type: .type, p: pl} ] | to_entries
       | map({i: .key, type: .value.type, p: .value.p})) as $seq
  | ([ $seq[] | select(.type == "reviewer_verdict" and (.p.verdict == "REQUEST_CHANGES")) ]
       | (.[0].i // -1)) as $reqIdx
  | ([ $seq[] | select(.type == "implementer_phase_complete"
        or (.type == "node_dispatch_requested" and ((.p.node_id // "") | tostring | test("implement"; "i")))) ]) as $impl
  | ([ $seq[] | select(.type == "reviewer_verdict" and (.p.verdict == "APPROVE")) ]) as $appr
  | (of_type("model_selected") | map(pl.model) | map(select(. != null))) as $models
  | ($models | map(select(. != $wantModel)) | unique) as $badModels
  | (of_type("run_completed") | map(pl)
       | map(select((.bead_id // null) == $spec.seed_bead and .success == true)) | length > 0) as $closed
  | ([ $impl[] | select(.i > $reqIdx) ] | (.[0].i // -1)) as $reImplIdx
  | ([ $appr[] | select(.i > $reImplIdx) ] | (.[0].i // -1)) as $apprIdx
  | if $reqIdx < 0
    then result("gap6"; "fail"; "no REQUEST_CHANGES reviewer_verdict — the seed rubric did not force a round-trip (reviewer approved on the first pass?)")
    elif $reImplIdx < 0
    then result("gap6"; "fail"; "REQUEST_CHANGES@\($reqIdx) but no implementer re-dispatch after it — back-edge did not fire")
    elif $apprIdx < 0
    then result("gap6"; "fail"; "re-dispatch@\($reImplIdx) but no APPROVE reviewer_verdict after it — round-trip did not converge")
    elif ($badModels | length) > 0
    then result("gap6"; "fail"; "same-model VIOLATED: model_selected carried \($badModels | join(",")) != pinned \($wantModel) (claude leaked into the dot run?)")
    elif ($closed | not)
    then result("gap6"; "fail"; "round-trip verdicts present but no terminal run_completed(success) for \($spec.seed_bead) — run did not close green")
    else result("gap6"; "pass"; "REQUEST_CHANGES@\($reqIdx) -> impl re-dispatch@\($reImplIdx) -> APPROVE@\($apprIdx) -> close; all \($models | length) model_selected == \($wantModel)")
    end;

# --- gap7 — codex-first DOT routing (hk-ity2u) -------------------------------
# The codex-first safety boundary in one assertion: a bead carrying the tier-1
# `harness:codex` label runs its IMPLEMENT node on codex while BOTH reviewer nodes
# stay on claude-code via the DOT node pin — and each reviewer actually returns a
# verdict. This is the property that makes per-bead codex labelling safe; when it
# breaks, the reviewer silently goes to codex, emits no verdict, and the run reds in
# a way that reads as "codex cannot do the work" rather than "the routing was wrong"
# (hk-ofm89, hk-3eso9). A green outcome with the wrong routing is FALSE EVIDENCE, so
# outcome alone is never enough.
#
# ATTRIBUTION — why positional, and why that is sound.
# `harness_selected` carries only {bead_id, agent_type, tier}; there is NO node_id on
# it (harnessresolve.go emitHarnessSelected), so per-node routing CANNOT be read off
# the event itself. It is recovered from position: dot_cascade.go emits
# node_dispatch_requested at the TOP of every node visit (the `for visits` loop, before
# the node is handled), and harness_selected is emitted inside the launch-spec builder
# closure at each node's launch — so every harness_selected belongs to the NEAREST
# PRECEDING node_dispatch_requested. reviewer_verdict is attributed the same way (it
# also carries no node_id). Events before the first node_dispatch_requested attribute
# to "<pre-cascade>" and are reported, never silently dropped.
#
# The tiers are the load-bearing half of the assertion, not decoration:
#   implement → tier 1 proves the harness came from the BEAD LABEL (harnessresolve.go
#               tier-1 path). A codex implement at any other tier means the label is
#               not what routed it, and the cell proves nothing about labelling.
#   review/qa → tier 3 proves the harness came from the DOT NODE PIN
#               (pinnedHarnessLaunchSpecBuilder emits tier 3 unconditionally). A
#               claude-code reviewer at tier 4 would be the global default happening
#               to agree — the pin would be untested and would not hold once the
#               default moves.
#
# PASS requires ALL of:
#   (a) ≥1 harness_selected attributed to the implement node, ALL of them
#       agent_type == expect.dot_routing.implement_harness at tier implement_tier;
#   (b) for EVERY node in expect.dot_routing.reviewer_nodes: ≥1 harness_selected
#       attributed to it, ALL of them reviewer_harness at reviewer_tier;
#   (c) NO reviewer-attributed harness_selected carries the implementer's harness —
#       called out separately from (b) because this is THE failure the bead exists to
#       catch, and it deserves its own message;
#   (d) for EVERY reviewer node: ≥1 reviewer_verdict attributed to it — a reviewer that
#       launches on claude and then says nothing is the same false red by another route;
#   (e) no EPERM / "operation not permitted" text anywhere in the stream (the codex
#       shell-step sandbox denial, which surfaces as a tool failure rather than a typed
#       event, so it is caught by scanning the serialized stream).
def _dot_attributed:
  # [{i, type, p, node}] — every event tagged with the node whose dispatch preceded it.
  [ events[] | {type: .type, p: pl} ]
  | to_entries
  | map({i: .key, type: .value.type, p: .value.p})
  | reduce .[] as $e ([[], "<pre-cascade>"];
      if $e.type == "node_dispatch_requested"
      then [ .[0], (($e.p.node_id // "<unnamed>") | tostring) ]
      else [ (.[0] + [$e + {node: .[1]}]), .[1] ]
      end)
  | .[0];
def assert_gap7:
  ($spec.expect.dot_routing // {}) as $dr
  | ($dr.implement_harness // "codex")   as $wantImpl
  | ($dr.implement_tier // 1)            as $wantImplTier
  | ($dr.reviewer_harness // "claude-code") as $wantRev
  | ($dr.reviewer_tier // 3)             as $wantRevTier
  | ($dr.reviewer_nodes // ["review","qa"]) as $revNodes
  | _dot_attributed as $att
  | ($att | map(select(.type == "harness_selected"))) as $hs
  | ($att | map(select(.type == "reviewer_verdict"))) as $rv
  | ($hs | map(select(.node | test("implement"; "i")))) as $implHs
  | ([ $revNodes[] as $n | { node: $n,
        hs: ($hs | map(select(.node == $n))),
        rv: ($rv | map(select(.node == $n))) } ]) as $revs
  | ($implHs | map(select(.p.agent_type != $wantImpl or .p.tier != $wantImplTier))) as $implBad
  | ([ $revs[] | select((.hs | length) == 0) | .node ]) as $revNoLaunch
  | ([ $revs[] | .hs[] | select(.p.agent_type == $wantImpl) ]) as $revOnImplHarness
  | ([ $revs[] | .hs[] | select(.p.agent_type != $wantRev or .p.tier != $wantRevTier) ]) as $revBad
  | ([ $revs[] | select((.rv | length) == 0) | .node ]) as $revNoVerdict
  | ([ events[] | tostring | select(test("EPERM|operation not permitted"; "i")) ] | length) as $eperm
  | if ($dr == {})
    then result("gap7"; "pending"; "no expect.dot_routing in spec — add {implement_harness, reviewer_nodes, reviewer_harness} to assert gap7")
    elif ($att | map(select(.node != "<pre-cascade>")) | length) == 0
    then result("gap7"; "fail"; "no node_dispatch_requested in the stream — this is not a DOT-cascade run, so per-node routing cannot be attributed (did the bead resolve to single/review-loop?)")
    elif ($implHs | length) == 0
    then result("gap7"; "fail"; "no harness_selected attributed to an implement node — the implementer never launched")
    elif ($implBad | length) > 0
    then result("gap7"; "fail"; "implement node routed to \($implBad[0].p.agent_type)/tier\($implBad[0].p.tier) != \($wantImpl)/tier\($wantImplTier) — the harness:\($wantImpl) bead label did not route the implementer")
    elif ($revNoLaunch | length) > 0
    then result("gap7"; "fail"; "reviewer node(s) \($revNoLaunch | join(",")) never launched — the cascade did not reach review/qa")
    elif ($revOnImplHarness | length) > 0
    then result("gap7"; "fail"; "REVIEWER ROUTED TO \($wantImpl) on node '\($revOnImplHarness[0].node)' — the bead label overrode the DOT node pin; this reviewer emits no verdict and the run reds as a false codex-capability failure (hk-ofm89)")
    elif ($revBad | length) > 0
    then result("gap7"; "fail"; "reviewer node '\($revBad[0].node)' routed to \($revBad[0].p.agent_type)/tier\($revBad[0].p.tier) != \($wantRev)/tier\($wantRevTier) — tier\($wantRevTier) is the DOT node pin; another tier means the pin is not what held")
    elif ($revNoVerdict | length) > 0
    then result("gap7"; "fail"; "reviewer node(s) \($revNoVerdict | join(",")) launched on \($wantRev) but emitted NO reviewer_verdict — silent reviewer, same false red by another route")
    elif $eperm > 0
    then result("gap7"; "fail"; "EPERM / 'operation not permitted' in the stream (\($eperm) event(s)) — the codex shell step hit a sandbox denial")
    else result("gap7"; "pass"; "implement=\($wantImpl)/tier\($wantImplTier); \($revs | map("\(.node)=\(.hs[0].p.agent_type)/tier\(.hs[0].p.tier) verdict=\(.rv[0].p.verdict)") | join(" ")); no EPERM")
    end;

# --- dispatcher ------------------------------------------------------------
# Run only the gaps the spec lists, in gap-number order, de-duplicated.
def run_gap($g):
  if   $g == "gap1" then assert_gap1
  elif $g == "gap2" then assert_gap2
  elif $g == "gap3" then assert_gap3
  elif $g == "gap4" then assert_gap4
  elif $g == "gap5" then assert_gap5
  elif $g == "gap6" then assert_gap6
  elif $g == "gap7" then assert_gap7
  elif $g == "t10"  then assert_t10
  else result($g; "fail"; "unknown gap id in spec: \($g)")
  end;

[ ($spec.gaps // []) | unique[] | run_gap(.) ]
