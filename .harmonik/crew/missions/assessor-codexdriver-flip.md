---
schema_version: 2
assessor_name: assessor-codexdriver-flip
epic_id: hk-tckw3
branch: phase1-session-restart-substrate
gate: deploy
commit: 4d308f3b0aaee61d60f3bd4bed4684d9d9426e3f
found_by_sources: [assessor, admiral, fast-follow]
report_path: .harmonik/reports/codexdriver-flip-gate.md
spawned_by: admiral
---

# Gate: deploy — phase1-session-restart-substrate

You are the **assessor** for epic **hk-tckw3** (codex-first) on branch
**phase1-session-restart-substrate**, commit **4d308f3b**. Run the **deploy** gate on an
isolated scratch clone you own, file findings as scoped `found-by:assessor` beads, post a
reasoned **PASS|BLOCK** to **admiral** over `--topic gate`, and self-terminate.

## Current State

**Read this whole section before you start. A prior proof already exists and you are NOT
being asked to repeat it.** Your job is to close the one gap that proof left open.

### What is being released

Not a code change. A **production CONFIG FLIP** to `HARMONIK_SUBSTRATE=codexdriver`, which
routes bead *implementation* onto local Codex while *review* stays on Claude. The binary
under test is **4d308f3b**, which is what production already runs — unmodified. There is no
new code in this release. That is precisely why it is attractive and precisely why it needs
you: a config flip has a very wide blast radius (every bead in the queue) for a very small
diff (none).

**Operator context, verbatim (2026-07-22T05:40Z):** *"The sooner we can get beads processed
by codex the longer we'll be able to run this system."* This is a **runway constraint** —
Claude tokens are the resource that runs out. There is real pressure to ship this. **Do not
let that pressure move your verdict.** Speed comes from cutting scope, never from cutting
this gate. A BLOCK that is correct is worth more to the operator than a PASS that is fast.

### What has ALREADY been proven — do not re-prove it

Crew `india` ran a go/no-go on its own isolated daemon (bead `igt-6ig`, run
`019f884d-50d1-74d6-8c81-7cd9364601cc`), DOT cascade mode, on **unmodified 4d308f3b**. The
admiral **accepted** it. All three admiral criteria were met:

1. **Implement really ran on Codex**, proven two independent ways — event
   `harness_selected {agent_type: codex, bead_id: igt-6ig, tier: 4}` **and** the live process
   argv `codex exec --json -c sandbox_mode="danger-full-access" -C <worktree>`. Genuine codex
   on LocalRunner, native sandbox OFF, no ssh, **no silent claude fallback**.
2. **Real multi-file self-commit** — `f5a2206` "Add greeting package", body carrying
   `Refs: igt-6ig`, 4 files / 50 insertions / 2 deletions. `implementer_phase_complete`
   `commit_landed=true`, `exit_code=0`, 105.7s. The daemon logged `ensureCodexRefsTrailer
   already_present` — Codex committed correctly on its own; the fallback committer was never
   needed.
3. **Real verdicts, terminal through the daemon** — BOTH reviewer-class nodes resolved to
   claude via `harness_selected {agent_type: claude-code, tier: 3}` (the `workflow.dot` node
   pin) and **both APPROVED substantively**: the reviewer independently ran `go build`,
   `go vet`, `go test -count=1`, and *executed the binary*. Then `bead_closed` and
   `run_completed success=true summary='dot: reached terminal node "close"'`.

**The structural finding that makes this release possible:** `codex-implements/claude-reviews`
is **already correctly configured in production's own graph**. `workflow.dot` pins
`harness="claude-code"` on both reviewer-class nodes (`review`, `qa`); the `implement` node is
unpinned and takes the tier-4 global default. Under `dot_cascade.go:1394-1417` (`hk-2jxqg`) a
node pin wins **unconditionally**. Hence tier 4 for implement, tier 3 for both reviewers. **No
code change is required for the split.**

Also settled: `hk-pkxju` (codex reviewer never emits `agent_ready`) is a **review-loop-mode-only**
defect and is **NOT** on this critical path. An earlier gate hit it only because it forced
`--workflow-mode review-loop`, which is not the production path. Do not re-litigate this.

### THE GAP YOU EXIST TO CLOSE

The admiral **withheld** the release on india's proof, on india's own honestly-volunteered
limits. **What was proven is the PLUMBING. What is NOT proven is CAPABILITY:**

1. **The test project was a small purpose-built Go module, not harmonik.** A greeting package
   is not a hard bead.
2. **The commit gate was REDUCED.** india had to patch the graph's `commit_gate` tool_command
   from `go build && go vet && go test -run=^$ && scenario-gate.sh` down to `go build && go vet`,
   because `scripts/scenario-gate.sh` does not exist outside this repo. **Production's real,
   heavier commit gate has therefore never once run under Codex.**

**Your central question:** *On your own isolated daemon, does a **real harmonik bead** run
end-to-end under `codexdriver` with production's **UNMODIFIED** commit gate — `go build`,
`go vet`, `go test`, **and** `scenario-gate.sh` — implementing on Codex and reviewing on
Claude?*

Run it on a **clone of the harmonik codebase**, not a toy module. Keep **every harness pin
byte-identical to production's `workflow.dot`** — those pins are the thing under test and must
not be edited to make a run succeed. If you must change anything to get a green, that change
*is* a finding.

### Also evaluate: the lower-blast-radius alternative

A wholesale substrate flip may be the wrong shape. A **per-bead `harness:codex` label**
(tier 1) would let the fleet ramp gradually — a few beads at a time — instead of betting the
whole queue at once. The admiral would rather ship a gradual ramp today than a wholesale flip
tomorrow.

Assess it, and specifically confirm **`deps.harnessRegistry != nil` on the production dispatch
path**. This is load-bearing: per `dot_cascade.go:1411`, the tier-3 reviewer pin's protection
is *conditional* on a non-nil registry. If it is nil, a tier-1 `harness:codex` **bead label**
overrides the reviewer pin, the reviewer goes to Codex, no `agent_ready` ever arrives, and the
run reads **RED for a configuration reason while looking exactly like a product defect**.
india's run emitted tier 3, so the registry was non-nil *in india's setup* — that does **not**
establish it for production. Crew `juliet` is validating this by code reading in parallel;
reach your own conclusion independently rather than waiting on or deferring to it.

### Second change folding into this same release

**One gate, one binary swap covers both.** Crew `lima`'s `hk-qx065` fix — the shared
`~/.claude.json` folder-trust lost-update race that made concurrent workers park on a trust
dialog — is live-proven and admiral-accepted: 1-of-3 became 3-of-3, same harness, same
settings, only the binary changed, pane sweeps at five timepoints showing zero trust modals.
The fix does post-write verification, bounded retry with a fresh read per attempt, per-attempt
flock released across backoff, and a structural error when the key will not stick.

**lima's honest residual, which the admiral accepted at face value and so should you:** this
**narrows** the race, it does not **close** it. A clobber landing after final verification but
before Claude's own startup read is unpreventable from our side of the process boundary.
**3-of-3 once is one trial, not a rate.** lima has been asked for repeat trials and a run at
concurrency 6 (capacity target is ~9; 6 is where we are known to fail today).

Codex running at real concurrency **is** the target production state, so validate the two
together — a serial validation would certify a configuration we will never actually run. This
fix lands through review shortly; fold it in when it does. If it has not landed by the time you
are otherwise ready to render a verdict, **say so and scope your verdict to what you actually
tested** rather than waiting indefinitely or assuming it landed.

### Standing rules you may not trade away

- **Isolated daemon you own. NEVER a production canary.** We change production because we are
  already confident — never to find out whether something works. If anyone frames a live prod
  bead as a canary, that is a finding, not a plan.
- **Containment before dispatch.** Point `origin` at a throwaway bare repo and set
  `branching.yaml` `start_from`/`lands_on` to a scratch branch. Verify with
  `git -C <scratch> remote -v` **before** any bead is dispatched. Production HEAD is
  `20fdcb70` and must be untouched when you finish — re-verify at the end.
- **Never `cd` into a worktree**; operate via `git -C` with absolute paths.
- **Private build cache** — `GOCACHE=$(mktemp -d)`.
- You **do not** flip production, swap a binary, or merge anything. You execute and you
  recommend. **The admiral holds the release decision.**

### Known trap, so you do not lose a run to it

`harmonik init` scaffolds a `harnesses.pi` block but **no `codex` block**, so a freshly
initialized project dies in under a second at spec-build on a missing
`codex.stale_wal_max_bytes` (there is no compiled default). Filed as `hk-yhvrh`. **Production
is NOT affected** — `.harmonik/config.yaml:146-147` already carries it. Add the key to your
scratch project's config and move on; adding it does not taint the proof. **Do not "fix" it in
production config.**

### What the admiral wants back

A **reasoned PASS or BLOCK** over `--topic gate`, weighed against
`.harmonik/agents/assessor/good-enough-principles.md`, plus your concerns and the report at
`report_path`. A PASS is not an automatic release and a BLOCK is not automatically fatal — the
admiral reads your concerns and decides, and may probe you over `--topic gate` before doing so.

Be explicit about **which** configuration you are blessing: wholesale substrate flip, per-bead
label ramp, both, or neither. If Codex passes the plumbing but fails hard beads under the real
commit gate, **say that plainly** — "ramp gradually on easy beads, do not flip wholesale" is a
genuinely useful verdict and is far better learned on your daemon than in production. Do not
tune a bead to make it pass. If a run fails, **name the node that wedged** (implement vs
review) so a Codex failure is never confused with a Claude-side one.
