# Isolated test-daemon methodology — validating risky substrates under concurrency

**What this is:** the *methodology + lessons* layer on top of the
[scratch-daemon runbook](scratch-daemon-runbook.md). The runbook is the
command reference (every subcommand, flag, env var, and safety guard). This doc
is the *why* and the *how-to-reason* — the field manual for an engineer who is
about to stand up an isolated test daemon to validate something genuinely new
and risky: containerization, a Linux port, a remote SSH worker, or a
distributed / multi-worker topology.

> **Read the runbook first** for the literal commands. This doc assumes you know
> what `init` / `build` / `up` / `cycle` / `batch` / `feedback` / `down` do; it
> tells you how to wield them to flush out concurrency bugs in a new substrate
> before they reach the production fleet.

> **Source of truth:** `scripts/scratch-daemon.sh`. The localhost-worker
> counterpart test is
> `internal/daemon/scenario_remote_substrate_localhost_test.go`. Remote-worker
> provisioning is `docs/remote-substrate/WORKER-SETUP-macos.md`. If this doc and
> any of those disagree, they win — re-read and fix this doc.

---

## 1. What & why — the isolated test-daemon pattern

The **isolated test daemon** is a SECOND, fully self-contained harmonik daemon
running on a SEPARATE git clone, with its own socket, its own tmux session, its
own binary built from that clone, and its own beads DB — that you can pin to a
real (or brand-new) substrate and hammer with throwaway work, **WITHOUT ever
touching the live "fleet" production daemon**.

Why it matters:

- **The fleet reproduce loop is ~30 minutes.** Edit code, wait for a free fleet
  slot, watch a single run go end-to-end, repeat. That round-trip is fatal to
  the kind of iteration a new substrate needs (you will cycle dozens of times).
- **The scratch loop is seconds-to-minutes.** `cycle` (down → build → up) reloads
  the exact code you just edited in seconds; `batch` submits a wave and returns a
  machine-readable verdict. You stay in a tight edit → validate → edit loop.
- **It is a disposable sandbox for risky substrates.** A remote SSH worker, a
  container, a Linux box, or N distributed nodes can be exercised under
  *realistic concurrency* without risking the fleet's git history, its beads
  ledger, or its uptime. When the sandbox gets wedged, you tear it down and
  re-`init` — no cleanup debt on anything that matters.

The mental model: **the fleet daemon is production; the scratch daemon is a
crash-test dummy you can pin to any new substrate and drive into the wall on
purpose.**

---

## 2. The isolation contract — what makes it safe and non-polluting

The harness is safe because *every handle is keyed off the scratch clone's
path*, so a second daemon can never collide with — or be mistaken for — the
fleet. The script enforces four safety layers (see the runbook §"Safety
guarantee" for the literal mechanics):

1. **Argv-gated PID kill** — `down` kills only the PID in
   `<scratch>/.harmonik/daemon.pid`, and only after confirming that live
   process's argv actually contains the scratch path. No blanket `pkill`.
2. **Ownership-gated tmux teardown** — the `harmonik-<hash>-default` session is
   killed only when the argv ownership proof above succeeded.
3. **`guard_path` (scratch ≠ fleet)** — every command symlink-resolves the path
   and hard-refuses `/`, an empty path, or the script's own repo root.
4. **`assert_not_supervised`** — `up`/`down`/`batch` refuse any project with a
   live `hk-<hash>-supervise` session (that's a supervised fleet deployment).

On top of those four script-enforced layers, these **session-proven additions**
are what make the harness safe to *hammer* — to run no-merge throwaway waves at
high concurrency without leaking anything to the fleet or to GitHub:

- **(a) Own `.harmonik` → own everything.** The scratch clone gets its own
  `daemon.sock`, its own `workers.yaml`, and its own `config.yaml`. None of these
  are shared with the fleet. The socket path, the pidfile, and the tmux session
  name are all derived from `realpath(scratch)` (PL-006a: `projecthash` = first
  12 hex of SHA-256 of the resolved path), so isolation is automatic.

- **(b) Repoint the scratch clone's `origin` to a THROWAWAY bare repo.** This is
  the single most important addition for no-merge hammering. By default `init`
  clones from the fleet's `origin` URL, which means the scratch daemon's
  merge/push step could in principle reach GitHub or the fleet repo. Before you
  hammer, sever that:

  ```bash
  # Make a throwaway bare origin from the LOCAL repo (no network, instant).
  git clone --bare /Users/gb/github/harmonik /tmp/hk-scratch-origin.git
  # Repoint the scratch clone at it — now the daemon's `push origin main` can
  # NEVER reach GitHub or the fleet checkout.
  git -C /tmp/hk-scratch remote set-url origin /tmp/hk-scratch-origin.git
  ```

  After this, every merge/push the scratch daemon performs lands in
  `/tmp/hk-scratch-origin.git` and nowhere else. You can hammer merge-racing
  waves all day and the worst case is a corrupt throwaway bare repo you `rm -rf`.

- **(c) Build the binary FROM the clone.** `build` runs `go build -C <scratch>`,
  so the daemon runs *exactly* the code in the checkout under test — not the
  fleet binary, not `$GOBIN/harmonik`. Edit the scratch clone, `cycle`, and the
  new code is live. This is what makes the loop trustworthy: there is no skew
  between "the code I edited" and "the code the daemon is running."

- **(d) Config boot landmines — set them or the daemon refuses to boot.** A fresh
  `harmonik init` writes a `config.yaml` with two keys that the daemon
  fail-louds on if they are missing or unset:
  - `liveness_no_progress_n` — the no-progress liveness threshold. If it is
    absent (or commented out) the daemon refuses to start (this is the
    fail-loud-on-missing-key behavior; `0` is a *valid explicit* "off", but the
    key must be present).
  - `max_concurrent` (a.k.a. `daemon.max_concurrent`) — must be set, or the
    daemon won't honor the concurrency you think you configured.

  After `init`, open `<scratch>/.harmonik/config.yaml` and SET both before the
  first `up`. A daemon that "won't boot" on a fresh scratch clone is almost
  always one of these two, not a code defect — check the log
  (`<scratch>/.harmonik/scratch-daemon.log`) for the named missing key.

---

## 3. Pinning the test daemon to a substrate

The whole point is to point the test daemon at the thing you're validating. The
substrate is selected by **the worker registry + the substrate config** — swap
those and the same harness validates a different substrate.

### Remote SSH worker (the worked case)

Write `<scratch>/.harmonik/workers.yaml` with the worker you want to hammer:

```yaml
version: 1
workers:
  - name: gb-mbp
    transport: ssh
    host: 100.87.151.114      # tailnet IP, or a LAN IP for a faster direct path
    os: darwin                # darwin | linux
    repo_path: /Users/gb/harmonik-worker/repo
    max_slots: 3              # ← test at the TARGET concurrency, NOT 1 (see §7)
    enabled: true
```

Then bring the daemon up at matching concurrency:

```bash
SCRATCH_MAX_CONCURRENT=3 ./scripts/scratch-daemon.sh cycle /tmp/hk-scratch
```

The worker box itself is provisioned per
`docs/remote-substrate/WORKER-SETUP-macos.md` (toolchain, tailnet join in
`accept`-mode not `check`-mode, subscription login with NO `ANTHROPIC_API_KEY`,
read-only GitHub access — box A fetches run branches directly over SSH, no
worker→GitHub push).

### Generalizing the idea

The registry-swap is the whole abstraction. The same harness pins to:

- **a container** — a worker entry whose transport/host resolves to a container
  (or a `docker exec`-style runner); `repo_path` is the path inside the
  container; `max_slots` is how many concurrent beads the container accepts.
- **a Linux box** — same `ssh` transport, `os: linux`, the box's repo path.
- **N distributed workers** — multiple entries in `workers.yaml`, each with its
  own `max_slots`, under the daemon's global `--max-concurrent` cap.

You change *what the worker registry points at* and *the substrate config*; the
daemon's dispatch / worktree / sync / merge machinery is unchanged. That is
exactly why the harness is reusable across substrate types.

---

## 4. The hammer pattern — throwaway no-merge beads

The throwaway work you submit should be a **self-proving probe**: a bead whose
task is trivial but whose *execution leaves ground-truth evidence on the
substrate*. The canonical probe:

> "Write `hostname` to line 1 and a timestamp to line 2 of a new file, then
> commit."

You do **NOT** care whether these beads merge or land. You care that they
**PROVABLY EXECUTE on the target substrate**. The proof is two-pronged:

1. **Daemon-side:** the scratch `events.jsonl` shows a `run_started` event
   carrying `worker_name=<target>` for that run (the run was *routed* to the
   substrate, not silently run LOCAL).
2. **Ground truth on the substrate:** the actual commit exists in the
   substrate's repo path, with **line 1 == the substrate's own hostname**. A
   probe run on `gb-mbp` produces a file whose first line is the worker's
   hostname — proof the work physically ran *there*, not on box A.

The real proof artifacts from this program look exactly like this — e.g.
`docs/remote-substrate/e2e-proof/proof-*.md`, whose first line is the worker
hostname (`gb-mac-mini.local`, `gb-mbp`) and second line is the proof
description. That hostname-on-line-1 is the substrate fingerprint.

Submit the probes as a wave at the target concurrency:

```bash
# Create N trivial probe beads in the SCRATCH beads DB (contained subshell — §7).
( cd /tmp/hk-scratch && \
  for n in a b c; do
    br create --title "probe-$n: hostname+ts to a new file, commit" \
              --type task --priority 2
  done )

# Hammer them as one wave at slots=3.
SCRATCH_MAX_CONCURRENT=3 \
  ./scripts/scratch-daemon.sh batch /tmp/hk-scratch hammer --beads hk-probe-a,hk-probe-b,hk-probe-c
```

A `wave` group dispatches its items in parallel up to `--max-concurrent`, so a
3-item wave at slots=3 puts all three on the substrate at once — which is the
condition that flushes out concurrency bugs (§7).

---

## 5. Observing correctly — the event-correlation lessons (LOAD-BEARING)

This section is the difference between a real diagnosis and a confident false
conclusion. Internalize all four:

- **Correlate by `run_id`, NOT `bead_id`.** Several key events —
  `agent_ready`, `launch_initiated`, `agent_ready_timeout` — carry only `run_id`
  in their payload, **not** `bead_id`. A watcher that filters on `bead_id` gets
  **false negatives**: it concludes "the run never reached `agent_ready`" when it
  actually did, because those events were invisible to a bead-keyed filter. The
  correct procedure: build the `bead → run_id` map from `run_started` (which
  carries both), then track *everything else* by `run_id`.

- **Always time-filter with a CUT timestamp.** `events.jsonl` is append-only and
  accumulates across runs. Pick a cut timestamp (the moment you submitted the
  wave) and correlate only to the current wave's `run_id` values. Otherwise a
  stale historical event produces a false signal — e.g. an old `worker_unhealthy`
  from last week reads as "the worker is down now."

- **events.jsonl is authoritative for routing — daemon stderr is NOT.** Worker
  routing is logged as TYPED events in `events.jsonl` (`run_started.worker_name`,
  `worker_unhealthy`), **never** to daemon stderr. Grepping the daemon log for
  the selector (`SelectWorker`, the worker name, `workers.Load`) **always returns
  0 regardless of whether routing happened** — that zero is meaningless, not a
  "routing isn't firing" signal. Confirm routing only via the `run_started`
  event's `worker_name` field. (This exact false signal burned a prior
  investigation that concluded "SelectWorker isn't engaging" when it was.)

- **Verify GROUND TRUTH on the substrate, not just the daemon's verdict.** A run
  can report a clean terminal (`run_completed success=true`) while the artifact
  on the substrate tells a different story — and vice-versa. Always check the
  *thing itself*: does the commit exist in the substrate's repo, is line 1 the
  substrate's hostname, did the file actually get written there? The daemon's
  pass/fail is a claim; the substrate artifact is the fact.

The localhost-worker scenario test
(`scenario_remote_substrate_localhost_test.go`) encodes the same discipline as
executable assertions: it asserts the worker's commit lands on box A's main
*and* that `run_started.worker_name == "localhost"` (with a NoWorker negative
guard proving that assertion is load-bearing). Read it as the canonical example
of "verify routing AND ground truth, not just terminal state."

---

## 6. The loop — reproduce → root-cause → fix-in-scratch → cycle → re-validate

The core workflow once a substrate misbehaves:

1. **Reproduce until it RECURS reliably.** Hammer the wave (§4) at the target
   concurrency until the failure reproduces *every time*, not once. A bug you
   can't reproduce on demand can't be confirmed fixed. If it only fails 2/6
   times, that intermittency is itself data — it usually means a contended
   shared resource (§7), so raise concurrency until it's reliable.
2. **Root-cause it.** Read the code along the failing path; find the *shared or
   contended resource* (a per-host connection, a fixed file path, a singleton).
   Use the event-correlation rules from §5 to locate exactly where the run
   stalls (e.g. `launch_initiated` present but `agent_ready` absent → the stall
   is in the launch gap, keyed by `run_id`).
3. **Apply the fix to the SCRATCH clone.** Edit the code in `/tmp/hk-scratch`
   directly — that's the checkout the binary is built from.
4. **`cycle` to load it.** `./scripts/scratch-daemon.sh cycle /tmp/hk-scratch`
   rebuilds and restarts in seconds.
5. **Re-run the same wave.** Confirm clean — every probe routed to the substrate
   *and* every ground-truth artifact present with the right hostname.
6. **THEN port the validated fix to the fleet** via the normal path: a
   worktree, independent reviewers, the review gate, and a fast-forward land
   onto main. The scratch loop is for *fast validation*; landing is a separate,
   reviewed step. Do not conflate "it works in scratch" with "it's shipped."

`feedback` (runbook §"feedback") bridges the two: it turns scratch-batch
FAILURES into deduped OPEN beads on the fleet ledger, so a reproduced failure
becomes tracked work the fleet daemon can pick up. That's the one deliberate
fleet write; everything else stays in the sandbox.

---

## 7. Lessons learned — concurrency is the discriminator

This is the biggest theme of the whole program. Most substrate bugs are
**invisible at one slot and reliable at three.**

### Single-slot validation is INSUFFICIENT — you MUST test at target concurrency

Multiple real bugs **passed at `max_slots:1` and failed reliably at
`max_slots:3`.** Two concrete examples from this program:

- **Shared per-host SSH ControlMaster.** A multiplexed SSH connection
  (`ControlMaster auto` / a shared `ControlPath`) means every per-run reverse
  tunnel and SSH read collapses onto ONE master connection. Single-slot: fine.
  Three concurrent runs: the shared master truncates/drops reads under churn and
  runs fail nondeterministically. Fix: pin `-o ControlMaster=no -o
  ControlPath=none` *per connection* so each run gets its own SSH channel.
- **Verdict read through the worker runner.** A code path `ssh`-`cat`'d a
  box-A-LOCAL file *through the worker runner*. Single-slot: fine. Three
  concurrent runs: the read truncated 3/3 under concurrency. Fix: read the local
  file locally, not through the per-host worker transport.

**The pattern:** *a resource that is shared/fixed PER-HOST rather than PER-RUN is
invisible at slots:1 and breaks at slots>1.* When you hammer a new substrate,
always ask: what here is one-per-host (a connection, a control socket, a fixed
path, a lock, a port) that N concurrent runs will contend for? Test at the
concurrency you'll actually deploy, or you have validated nothing about
concurrency.

### Merge-race noise is EXPECTED and out-of-scope

N throwaway beads racing to merge the same `main` will produce
`non_ff_merge` / `merge_build_failed` — that is **NOT a substrate-path failure.**
For concurrent no-merge hammering, define success as **"reached the substrate +
did the work"**: `agent_ready` + `implementer_phase_complete` + the ground-truth
commit on the substrate, *independent of merge outcome*. Don't chase merge
collisions — they're the expected cost of racing many beads at one branch, and
they tell you nothing about whether the substrate path works. (This is also why
§2(b)'s throwaway origin matters: merge failures are harmless when they land in a
disposable bare repo.)

### Rapid-boot backoff

Cycling the scratch daemon many times in quick succession trips a **restart
backoff** that delays startup by ~60s. The script's `up` bind-wait (45s) can
then time out and *look like a boot failure* when it isn't. If `up` times out
after a flurry of `cycle`s: **wait out the backoff and re-check the socket**
(`status`), don't assume the binary is broken. Confirm with the log — a
backoff-delayed boot logs the delay; a real crash logs a stack/fail-loud.

### CWD drift

Running `br` / `harmonik` against the *scratch* beads DB is dangerous from the
main shell, because CWD silently drifts (a guard can reset it mid-loop) and your
`br` command then hits the *fleet* beads DB by accident — producing false
`ISSUE_NOT_FOUND` or, worse, writing to the wrong ledger. Always run scratch-DB
commands in a **contained subshell**:

```bash
( cd /tmp/hk-scratch && br ready --limit 0 )      # operates on the SCRATCH DB only
```

The parentheses confine the `cd` to the subshell; the main shell's CWD never
moves. (The script itself uses this pattern internally — `feedback` runs `br` in
a `( cd "$fleet" && ... )` subshell precisely so the target DB is unambiguous.)

---

## 8. Forward applicability

The pattern generalizes to every "risky new substrate" on the roadmap. In each
case the recipe is identical — pin the worker registry / substrate config at the
new thing, hammer with the hostname-proof probe at target concurrency, verify
routing (`run_started.worker_name`) AND ground truth (the commit on the
substrate) — and the concurrency lessons of §7 carry over verbatim.

- **Containerization.** Point the worker registry / substrate config at a
  container. Hammer to validate the container launch + exec + commit path *under
  concurrency* before any fleet rollout. The hostname-proof probe confirms work
  ran *inside the container* (line 1 = the container's hostname, not the host's).
  Watch for per-host shared resources: a single shared container, a shared bind
  mount, or one control socket shared across concurrent `exec`s is the
  ControlMaster lesson in a new costume.

- **Linux support.** Run the scratch daemon targeting (or hosted on) a Linux
  box. The same hostname-proof probe catches OS-specific issues the macOS path
  hides — path differences, the `sockaddr_un.sun_path` length limit (the
  localhost test already roots box A under a SHORT `/tmp` path for exactly this
  reason on macOS; Linux has its own limit), and tooling/version skew
  (`git`/`tmux`/`claude`). Hammer at concurrency to surface Linux-specific
  contention before the fleet depends on it.

- **Distributed / multi-instance.** Put multiple workers in the registry and
  hammer a wave large enough to spread across them. This is where cross-instance
  **shared-resource collisions** surface: the ControlMaster lesson generalizes to
  *any per-host shared handle* — a shared SSH master, a shared lock file, a
  shared port, a shared cache directory. A bug that's invisible with one worker
  at slots:1 becomes reliable with three workers at slots:3. Verify each run
  routed to the *intended* worker (`run_started.worker_name`) and that each
  worker's ground-truth artifacts carry *its own* hostname — a routing bug that
  sends everything to one node will otherwise pass the merge assertions and slip
  through.

The constant across all of these: **the fleet stays untouched while you drive a
disposable second daemon into the new substrate at production concurrency, and
you only port a fix to the fleet after the scratch loop proves it clean.**

---

**Refs:** `scripts/scratch-daemon.sh` (command source of truth);
[scratch-daemon-runbook.md](scratch-daemon-runbook.md) (command reference);
`internal/daemon/scenario_remote_substrate_localhost_test.go` (executable
counterpart — routing + ground-truth assertions);
`docs/remote-substrate/WORKER-SETUP-macos.md` (worker provisioning);
`docs/remote-substrate/e2e-proof/` (real hostname-proof artifacts).
</content>
</invoke>
