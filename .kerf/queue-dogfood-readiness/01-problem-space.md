# Queue dogfood readiness — Problem Space

**Status:** first planning pass. This record defines the safety work that must
finish before the project uses the normal queue for real tasks.

## Summary

The daemon is intentionally down. The next goal is not to resume every part of
the delete-and-rewrite program. It is to run the normal queue safely enough for
small, real work and to prove that claim in an isolated scratch daemon.

The normal queue cannot send work to an assessor. An assessor is started as a
separate gate role. It audits a named branch in its own scratch daemon, files
findings, and returns a reasoned PASS or BLOCK. It does not consume normal work
from the queue.

Before any queue dogfood run, the team must repair known queue safety defects,
make the proof suite trustworthy, inspect stale operational state, and obtain
operator authority for any registry reset or new mission. The first queue run
is a controlled assessment input. It is not a general fleet restart.

## Goals

1. Make shutdown safe for DOT runs. A committed but unmerged run must drain or
   report a durable, reviewable failure before daemon stop completes.
2. Make queue failure recovery real. An operator command must re-arm failed
   items when it claims to do so, or the command and its comments must not make
   that claim.
3. Make reservation changes durable and quarantine state reliable. No raw write
   may erase a failed reservation quarantine or leave disk and memory with
   different reservation state.
4. Give the durability changes tests that fail when their persistence path is
   removed. A green test name or ratchet must not be treated as proof of a
   transaction write that it does not observe.
5. Establish a trustworthy daemon test procedure. It must distinguish machine
   contention from a product defect and record the load conditions with the
   result.
6. Run the Step 9 and core-loop live readiness proof in a scratch daemon. Keep
   its result artifact for the assessor.
7. Reset stale registry and mission state before launch. Use a new mission that
   limits scope, has known acceptance behavior, and can be audited. Read current
   state first. Change it only with operator authority.
8. Clean the bead and event inputs used for the first run. Close only verified
   stale work. Preserve unresolved safety findings as scoped, actionable work.

## Scope

### Safety blockers

The implementation plan must address these confirmed blockers before a real
queue task is submitted:

- `hk-dk0sf`: DOT shutdown does not drain committed but unmerged work.
- `hk-xhmf1`: queue resume does not re-arm failed items.
- `hk-7t615`, `hk-e91d8`, and `hk-mk4cl`: reservation and queue-write durability
  defects, including non-sticky quarantine and non-durable undo.
- `hk-jvrsv` and `hk-c8mvo`: tests and ratchets that claim durability proof but
  do not observe the durability boundary.
- `hk-hqttl`: daemon-suite reliability. The available evidence supports load
  contention as the immediate cause. The readiness procedure must run the suite
  under controlled load and state the rule or code change that keeps results
  useful.
- Step 9 completion evidence and the core-loop live test. The required run uses
  `scripts/scratch-daemon.sh`, keeps its artifacts, and is available to the
  assessor.
- Registry and mission hygiene for the controlled run. Inspect existing records
  first. The operator authorizes any reset, retirement, or new mission.

### First-canary limits

The first controlled queue batch is fixed at these limits. Later design may make
it stricter. It must not make it broader without a new operator decision.

- One local stream group only.
- Maximum concurrency is one.
- The item is repeat-safe. A repeat after daemon restart cannot cause a
  user-facing or cross-repository change.
- No remote harnesses.
- No Pi harnesses.
- No cross-repository work.
- No wave groups.

### Verified stale closure candidates

The old graph findings `hk-v4wer`, `hk-o4sgg`, `hk-fmere`, and `hk-b4xf2` are
not new code work in this plan. The Step 13 DOT rewrite removed the former
single-mode tail and changed the paths these records described. A triage pass
must verify each current source path and close it with evidence if it is stale.
If any condition remains, it becomes a newly scoped finding with current
evidence. Do not reopen the old description by assumption.

### Ownership boundaries

- **Alpha** owns every `internal/daemon` change and test in this work. This
  includes DOT shutdown drain, `evaluateGroupAdvanceWithOutcome`, release-path
  durability, and daemon-side proof integration.
- **Bravo** owns `internal/queue`, `internal/queuewiring`, queue CLI behavior,
  test and ratchet repairs outside `internal/daemon`, and the backlog triage
  record. `queue-status-writer` does not wire failed-item resume. This work
  needs an explicit recovery-wiring task instead of assuming that transition
  work provides it.
- **Alpha and Bravo** define release and shutdown-path contracts together before
  either side changes a shared boundary. The daemon package still has one writer.
- **Assessor** is independent. It receives an assessor mission and audits an
  isolated scratch daemon. It does not run normal queue work.

## Non-goals

- Do not turn on the fleet daemon or begin broad queue dispatch in this work.
- Do not use an assessor as a queue worker or make it consume a normal queue.
- Do not resume the full delete-and-rewrite program as a condition of the first
  queue run.
- Do not change code, bead status, registry state, or missions in this planning
  pass.
- Do not treat a green loaded-machine daemon suite as release evidence.
- Do not delete or close a bead only because its description is old.

## Constraints

- The normal daemon remains down until the readiness gate is complete and an
  operator chooses a controlled launch.
- Scratch runs use a separate clone, socket, tmux session, binary, and beads
  ledger. The fleet daemon and fleet worktree stay untouched.
- The first live run must use the fixed first-canary limits. Its item must be
  safe to repeat or have a clear recovery plan.
- Registry reset, stale-mission retirement, and creation of a new mission are
  read-first operations. An agent must show the affected records and obtain
  operator authority before it changes them.
- The assessor mission must follow `specs/assessor-handoff-schema.md`. The
  assessor reports PASS or BLOCK to its gate owner and files scoped findings.
- Beads are evidence and work records. They do not replace the assessor verdict.
- The queue ledger is machine-local. A tracked planning record must state the
  decisions and exact initial mission criteria that must survive this machine.

## Success criteria

The later design and task passes must define a gate that is satisfied only when:

1. The listed safety blockers have a reviewed fix and focused regression proof.
2. The daemon suite has a reproducible controlled-load result and an explicit
   operating rule for concurrent daemon tests.
3. Step 9 and core-loop live readiness have passed in a scratch daemon with
   retained artifacts.
4. Stale graph beads have a source-backed closure or are replaced by scoped,
   current findings.
5. Registry state is reset, stale missions are retired, and one fresh mission
   is defined for the first controlled batch, after the operator authorizes
   each state change.
6. An assessor handoff names the branch, scope, report path, gate owner, and
   proof artifacts. The assessor runs separately from the normal queue.
7. An operator can make a narrow go or no-go decision from the assessor report.
   A PASS authorizes only the stated controlled queue batch, not fleet-wide
   dispatch.
8. The first batch is one local repeat-safe stream at concurrency one. It has no
   remote, Pi, cross-repository, or wave work.

## Preliminary affected areas

- `internal/daemon` and DOT shutdown and release paths.
- `internal/queue`, `internal/queuewiring`, and queue CLI recovery behavior.
- Queue durability scenario tests and status writer ratchets.
- `scripts/scratch-daemon.sh`, `scripts/core-loop-matrix.sh`, and the Step 9
  live-proof record.
- `.harmonik` registry, assessor mission files, and the assessor report path.
- The beads ledger and event artifacts used by the initial controlled batch.
- `plans/2026-07-27-delete-and-rewrite/LANES.md` and both live handoffs.

## Open planning questions

1. Which narrow batch can prove execution without risking a user-facing change
   if it is repeated after restart?
2. Does controlled daemon testing require a code-level timeout change, or is a
   one-daemon-suite-at-a-time operating rule enough after an idle-box trial?
3. What evidence must a stale graph-bead closure include so that it does not
   hide a live defect behind the Step 13 rewrite?
