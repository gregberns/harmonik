schema_version: 1
crew_name: hawat
queue: hawat-q
epic_id: hk-rs-phase1-qfn1
captain_name: captain
goal: |
  LANE #2 (operator priority order, admiral event 019f4ed0) — REMOTE SUBSTRATE.
  Epic hk-rs-phase1-qfn1 (tracking epic; tasks carry label codename:remote-substrate).
  The operator's framing: remote is BUGGY and needs HEAVY TESTING. Your job is to make
  the remote path provably reliable via local test-first work, NOT to chase a live e2e
  that the substrate can't yet support.

  DISPATCH (your OWN queue hawat-q; never main), file-disjoint, ranked:
  - Testing/validation + reverse-tunnel reliability beads under codename:remote-substrate.
    Candidates in the ready feed: hk-5z1f0 (remote reviewer agent_ready_timeout under
    concurrent slots), hk-tagp (rs gap7 e2e text-file+commit), hk-rs-validate-remote-898a
    (first remote dispatch proof), hk-rs-tunnel-verify-dvx8 (live e2e verify), hk-icdz
    (concurrent worktree-create proof), hk-tyyy (daemon auto-provisions binary to worker).
  - Pull the ranked codename:remote-substrate ready set; work file-disjoint, test-first
    (L0-L5 pyramid / isolated test-daemon — the substrate reliability history shows the
    real bugs are in the daemon reverse-tunnel + remote worktree-create path).

  HARD CONSTRAINTS (load-bearing):
  - gb-mbp live re-enable is HELD for the OPERATOR — do NOT `harmonik worker enable gb-mbp`,
    do NOT flip workers.yaml enabled:true. If a bead genuinely needs a LIVE remote run, STOP
    and surface to captain (captain coordinates the operator hold). Everything up to that is
    local isolated-daemon testing.
  - COLLISION GUARD: remote reverse-tunnel/worktree-create fixes touch internal/daemon/
    (workloop.go). stilgar's quality lane (hk-ih5k6 daemon regression tests) also touches
    internal/daemon. Before landing any internal/daemon edit, check for overlap + coordinate
    with captain — do NOT land a colliding workloop.go change blind.

  Model posture: Opus (this lane is buggy + needs triage/testing judgment — classify failures
  yourself, but escalate a genuine daemon-core defect or a needs-live-gb-mbp blocker to captain).

  Progress feed per crew contract: comms --topic status AND br comments — on bead-close +
  <=10min while dispatching / <=15min idle-drain, + boot/drain bookends. Arm comms recv --follow.
  Escalate genuine blockers (needs-live-remote, daemon-core defect, collision) to captain.
