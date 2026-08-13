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
# LAST-WINS APPLIES WITHIN ONE ROLE, NOT ACROSS ROLES. A dot run resolves a harness for the
# implementer node AND for the reviewer node. The two events are the same type and carry the
# same bead id, so last-wins over the mixed set answers a question about the implementer with
# the reviewer's event whenever the reviewer launches last — which is what a converged
# round-trip does. gap1 therefore attributes each harness_selected to the node that was
# dispatched for it (harness_selected_by_node below), keeps the implementer's, and takes
# last-wins inside that set. The retry the rule exists for — a re-dispatch of the SAME node
# at a new tier — still wins. Bead: hk-vf4ju.
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

# is_implementer_node — does this workflow node id name the implementer?
#
# The stream names no ROLE. harness_selected carries {bead_id, agent_type, tier} and
# model_selected carries {run_id, harness, model}, so neither says which node resolved it.
# The one role signal in the stream is the node id on node_dispatch_requested. Every graph
# this gate runs spells its implementer node "implement" (no-review-bead, standard-bead) or
# "implementer" (review-loop-example), and spells its reviewer node "review" or "reviewer".
# gap6 already read the node id this way for its re-dispatch clause, so the test lives here
# once and both gaps use the one spelling.
def is_implementer_node($id): (($id // "") | tostring | test("implement"; "i"));

# harness_selected_by_node — every harness_selected event, tagged with the node it belongs to.
#
# The daemon dispatches a node and then resolves that node's harness, so the most recent
# node_dispatch_requested before a harness_selected names the node that event belongs to.
# node_dispatch_requested carries a run_id and no bead id, so the scan first drops the
# dispatches of any OTHER run — the same join gap4 makes, for the same reason: a sibling
# run's nodes must not answer for this cell. Pass "" for $rid to keep every dispatch, which
# is what a stream with no run_started for the seed bead gets.
#
# .node is null on a harness_selected that no dispatch precedes. A stream that carries no
# node_dispatch_requested at all gives every event a null node, and the caller then falls
# back to the un-attributed set and behaves as it did before.
def harness_selected_by_node($rid):
  [ foreach (events[] | {t: (.type // ""), p: pl}) as $e
      (null;
       if $e.t == "node_dispatch_requested"
          and ($rid == "" or ((($e.p.run_id // "") | tostring) == $rid))
       then (($e.p.node_id // "") | tostring)
       else . end;
       {node: ., t: $e.t, p: $e.p})
  ]
  | map(select(.t == "harness_selected"));

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
#
# WHOSE HARNESS (a) IS ABOUT: the IMPLEMENTER's. A dot run resolves a harness for the review
# node too, under the same bead id, so the events must be told apart before last-wins picks
# one. They are told apart by the node that was dispatched for each (harness_selected_by_node
# + is_implementer_node above), which is the only role signal the stream carries.
#
# A stream that names no implementer node — no node_dispatch_requested at all, or a graph
# that spells its implementer node something is_implementer_node does not match — falls back
# to the un-attributed set and behaves as this gap did before hk-vf4ju. The fallback is quiet
# on purpose, because every fixture that predates node_dispatch_requested relies on it, so
# a NEW graph with an unusual implementer node id loses the scoping without a word. Teach
# is_implementer_node the new spelling when that happens.
def assert_gap1:
  ($spec.expect.harness_selected // {}) as $eh
  | ($spec.expect.model_selected  // {}) as $em
  | ($em.no_leak_models // []) as $forbidden
  | ((of_type("run_started") | map(pl)
        | map(select((.bead_id // null) == $spec.seed_bead))
        | (.[-1].run_id // "")) | tostring) as $rid
  | (harness_selected_by_node($rid)
        | map(select(.p.bead_id == $spec.seed_bead))) as $hsAll
  | ($hsAll | map(select(.node != null and is_implementer_node(.node)))) as $hsImpl
  | ((if ($hsImpl | length) > 0 then $hsImpl else $hsAll end) | map(.p)) as $hs
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
# the dispatched run with every field intact.
# Cross-event: model/harness fidelity is gap1's job; gap4 owns the run_started dispatch
# fields. spec.expect.dispatch.workflow_mode = the resolved mode, which a version-2
# run_started record always reports as "dot"; .workflow_id = the exact graph the cell
# resolves; .workflow_id_present = require a real (non-zero) workflow_id at all.
#
# workflow_mode alone no longer distinguishes a no-review run from a reviewed one —
# the daemon runs both as DOT graphs, so the mode names the engine. review_policy and
# workflow_selection_source carry that distinction instead, and gap4 asserts them when
# the cell declares them. Both are REQUIRED on a version-2 run_started record
# (event-model.md §8.1.1).
#
# spec.expect.dispatch.nodes is the CONTAINMENT check on the dispatched node set:
# .required lists node ids the run must dispatch, .forbidden lists node ids it must
# never dispatch. node_dispatch_requested carries run_id and NOT bead_id
# (event-model.md §8.1.11), so the set is scoped by joining on the seed bead's own
# run_started.run_id — a sibling run's nodes must not answer for this cell. The check
# is containment and not equality on purpose: revisit counts and the terminal node the
# run ends on are routing outcomes, so an equality set would go red on a legal path.
#
# Refs: hk-oeqn9, hk-gap4-workflow-mode-drift-7xwat.
def assert_gap4:
  ($spec.expect.dispatch // {}) as $ed
  | (of_type("run_started") | map(pl) | map(select((.bead_id // null) == $spec.seed_bead))) as $rs
  | (($rs[-1].workflow_id // "") | tostring) as $wid
  | (($rs[-1].run_id // "") | tostring) as $rid
  | ([ of_type("node_dispatch_requested")[] | pl
       | select((($rid == "") | not) and (((.run_id // "") | tostring) == $rid))
       | ((.node_id // "") | tostring) | select(. != "") ] | unique) as $nodes
  | ($ed.nodes.required  // []) as $needNodes
  | ($ed.nodes.forbidden // []) as $banNodes
  | ($needNodes | map(select(. as $n | ($nodes | index($n)) == null))) as $missingNodes
  | ($banNodes  | map(select(. as $n | ($nodes | index($n)) != null))) as $bannedNodes
  | if ($ed == {})
    then result("gap4"; "pending"; "no expect.dispatch in spec — add {workflow_mode, workflow_id, review_policy, workflow_selection_source, workflow_id_present, nodes} to assert gap4")
    elif ($rs | length) == 0
    then result("gap4"; "fail"; "no run_started event for seed bead \($spec.seed_bead)")
    elif ($ed.workflow_mode != null and ($rs[-1].workflow_mode != $ed.workflow_mode))
    then result("gap4"; "fail"; "run_started.workflow_mode=\($rs[-1].workflow_mode) != expected \($ed.workflow_mode)")
    elif ($ed.review_policy != null and ($rs[-1].review_policy != $ed.review_policy))
    then result("gap4"; "fail"; "run_started.review_policy=\($rs[-1].review_policy) != expected \($ed.review_policy) — the run took the wrong review path")
    elif ($ed.workflow_selection_source != null and ($rs[-1].workflow_selection_source != $ed.workflow_selection_source))
    then result("gap4"; "fail"; "run_started.workflow_selection_source=\($rs[-1].workflow_selection_source) != expected \($ed.workflow_selection_source) — the resolver chose the graph for the wrong reason")
    elif ($ed.workflow_id != null and ($wid != ($ed.workflow_id | tostring)))
    then result("gap4"; "fail"; "run_started.workflow_id=\($wid) != expected \($ed.workflow_id) — the resolver selected a different graph")
    elif ($ed.workflow_id_present == true and ($wid == "" or ($wid | test("^0+(-0+)*$"))))
    then result("gap4"; "fail"; "run_started.workflow_id is absent/zero (\($wid)) — workflow_ref did not resolve at dispatch")
    elif ((($needNodes | length) + ($banNodes | length)) > 0 and ($rid == ""))
    then result("gap4"; "fail"; "run_started for \($spec.seed_bead) carries no run_id — the dispatched node set cannot be scoped to this run")
    elif (($needNodes | length) > 0 and ($nodes | length) == 0)
    then result("gap4"; "fail"; "no node_dispatch_requested event for run \($rid) — the run dispatched no node, so required node(s) \($needNodes | join(",")) are absent")
    elif ($missingNodes | length) > 0
    then result("gap4"; "fail"; "run \($rid) never dispatched required node(s) \($missingNodes | join(",")) — dispatched set was [\($nodes | join(","))] (wrong graph resolved?)")
    elif ($bannedNodes | length) > 0
    then result("gap4"; "fail"; "run \($rid) dispatched forbidden node(s) \($bannedNodes | join(",")) — the selected graph declares none (a reviewer ran on a no_review run?)")
    else result("gap4"; "pass"; "workflow_mode=\($rs[-1].workflow_mode) review_policy=\($rs[-1].review_policy) source=\($rs[-1].workflow_selection_source) workflow_id=\($wid) nodes=[\($nodes | join(","))]")
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
# A bead directed at integration branch X must LAND on X, and the TRUNK must NOT advance.
# This assertion is NOT event-driven: the workspace_merge_status event the daemon once
# aspired to emit is NEVER emitted (dead/aspirational — the merge writes git but no event),
# so an event-based check is structurally always RED. Instead the matrix runner verifies
# the landing directly from GIT (baseline vs. post-run tips of the trunk + the target branch)
# and injects the branch that actually advanced as $spec._observed_lands_on. t10 simply
# compares intent (expect.lands_on) against that git-observed reality. Per-bead integration
# targeting is LIVE (hk-lgykq landed; proven by daemon E2E
# TestMergeToMain_PerBeadIntegrationTargetLandsOnBranch), so this is no longer known-RED.
#
# THIS ASSERTION NAMES NO BRANCH. It used to say "main" in one comparison and three
# messages, and the daemon has never landed on `main` in a scratch — scratch-daemon.sh
# isolate_push_target points defaults.lands_on at scratch/main. Both branch names now
# arrive from the runner, which reads them from the components that own them:
# expect.lands_on from the seed (the "@resolved" sentinel, substituted before the fold) and
# ._trunk_branch from the daemon's .harmonik/branching.yaml. A missing injection is PENDING,
# never a quiet pass — an un-nameable trunk cannot be checked for not moving.
#
# A cell whose seed declares no target_branch lands ON the trunk. For that cell $want equals
# $trunk, the trunk-advanced arm is skipped by its own second condition, and the landing
# reads as the clean pass it is.
def assert_t10:
  ($spec.expect.lands_on // null) as $want
  | ($spec._observed_lands_on // null) as $obs
  | ($spec._trunk_branch // null) as $trunk
  | if $want == null
    then result("t10"; "pending"; "no expect.lands_on in spec — set it to \"@resolved\" and let the runner read the branch off the seed")
    elif ($want | type) == "string" and ($want | startswith("@"))
    then result("t10"; "pending"; "expect.lands_on is the unsubstituted sentinel '\($want)' — the runner did not resolve the cell's landing branch from its seed")
    elif ($obs == null or $obs == "")
    then result("t10"; "pending"; "no ._observed_lands_on injected — runner did not git-verify the landing (need --assert + a git scratch)")
    elif ($trunk == null or $trunk == "")
    then result("t10"; "pending"; "no ._trunk_branch injected — runner did not read defaults.lands_on out of the daemon's branching.yaml, so 'the trunk must not move' cannot be checked")
    elif ($obs == $trunk and $want != $trunk)
    then result("t10"; "fail"; "the trunk '\($trunk)' advanced — the change landed there, not on the intended '\($want)' (the trunk must not move)")
    elif ($obs == "none")
    then result("t10"; "fail"; "nothing landed — neither the trunk '\($trunk)' nor '\($want)' advanced (merge did not run / bead did not close)")
    elif ($obs != $want)
    then result("t10"; "fail"; "landed on '\($obs)' != intended '\($want)' (git-verified)")
    elif $want == $trunk
    then result("t10"; "pass"; "landed on the trunk '\($want)', which is what this cell's seed asked for (it declares no target_branch) (git-verified)")
    else result("t10"; "pass"; "landed on '\($want)' (git-verified; trunk '\($trunk)' unchanged)")
    end;

# --- gap6 — dot review->implement round-trip, one model (D4) -----------------
# The dot cell must show a REAL model round-trip driven by the seed rubric, with no foreign
# model reaching the run. It must NOT be read as "the implementer and the reviewer are on the
# same model": they are not, and cannot be. The reviewer of a pi dot run is forced onto
# claude-code and resolves no model — see clause (d).
# PASS iff the captured stream shows, IN ORDER:
#   (a) a reviewer_verdict with verdict == REQUEST_CHANGES,
#   (b) an implementer RE-DISPATCH after that verdict — a node_dispatch_requested for the
#       implementer node (is_implementer_node) OR a second implementer_phase_complete,
#   (c) a reviewer_verdict APPROVE after the re-dispatch, then a terminal pass/close, AND
#   (d) SAME-MODEL: every node that RESOLVED a model resolved the same one — the pinned model
#       (spec expect.model_selected.model) when the cell pins one, else the run's OWN first
#       resolved model, so the check stays what its name says without this file naming a
#       model. A literal default here was a copy of a fact that lives in config, and it went
#       stale the day the model changed.
#
#       "RESOLVED a model" is the scope, and an EMPTY model string is outside it. A node that
#       pins no model= emits model_selected with model:"". The reviewer of a pi dot run always
#       does: the daemon refuses to let a reviewer inherit a SessionIDCaptured harness
#       (internal/runloop ReviewerDefaultHarness), so the review runs on claude-code, the
#       review node pins nothing, and ResolveModelPreference returns "". An unscoped clause
#       read that "" as a foreign model and reported a claude leak, which named the wrong
#       cause for a cell that could not pass in any ordering. Bead: hk-vf4ju.
#
#       THE SCOPE IS EMPTINESS AND NOT THE HARNESS FAMILY, and the difference is load-bearing.
#       gap1's no_leak_models keeps only events on the cell's OWN harness, because a node-pin
#       leak arrives on that harness. A model leak into a dot run arrives on the FOREIGN
#       harness with a NON-EMPTY model (testdata/pi-dot-modelleak-fail.ndjson), so gap1's
#       filter applied here would hide the one event this clause exists to find.
#
#       THE COST, stated rather than hidden: ANY node that resolved NO model is invisible to
#       this clause. An unpinned node on a harness other than pi resolves an empty model, so
#       an escape to that harness leaves no trace here. Two shapes, and they are not covered
#       equally. An IMPLEMENTER that escapes is caught by gap1's harness check. A THIRD
#       agentic node that escapes is caught by NEITHER gap, because gap1 speaks only for the
#       implementer. No graph in the tree has a third agentic node today (review-loop.dot is
#       start, implementer, reviewer, close), so this is a future hole, not a live one. Give
#       gap1 a per-node harness check before you add one.
# Positional ordering is taken from the append-ordered capture stream (event index).
def assert_gap6:
  (of_type("model_selected") | map(pl.model) | map(select(. != null and . != ""))) as $models
  | (of_type("model_selected") | length) as $modelEvents
  | (($spec.expect.model_selected.model // "") | tostring) as $pinnedModel
  | (if $pinnedModel != "" then $pinnedModel else ($models | first) end) as $wantModel
  | ([ events[] | {type: .type, p: pl} ] | to_entries
       | map({i: .key, type: .value.type, p: .value.p})) as $seq
  | ([ $seq[] | select(.type == "reviewer_verdict" and (.p.verdict == "REQUEST_CHANGES")) ]
       | (.[0].i // -1)) as $reqIdx
  | ([ $seq[] | select(.type == "implementer_phase_complete"
        or (.type == "node_dispatch_requested" and is_implementer_node(.p.node_id))) ]) as $impl
  | ([ $seq[] | select(.type == "reviewer_verdict" and (.p.verdict == "APPROVE")) ]) as $appr
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
    then result("gap6"; "fail"; "one-model VIOLATED: a node resolved model(s) \($badModels | join(",")) but this run's model is \($wantModel) — report is what was observed; the cause may be a node model= pin or an escape to another harness")
    elif ($closed | not)
    then result("gap6"; "fail"; "round-trip verdicts present but no terminal run_completed(success) for \($spec.seed_bead) — run did not close green")
    else result("gap6"; "pass"; "REQUEST_CHANGES@\($reqIdx) -> impl re-dispatch@\($reImplIdx) -> APPROVE@\($apprIdx) -> close; all \($models | length) of \($modelEvents) model_selected resolved \($wantModel) (\($modelEvents - ($models | length)) resolved no model)")
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
  elif $g == "t10"  then assert_t10
  else result($g; "fail"; "unknown gap id in spec: \($g)")
  end;

[ ($spec.gaps // []) | unique[] | run_gap(.) ]
