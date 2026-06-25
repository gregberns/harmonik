# Scratch-daemon harness — a standing on-demand isolated test loop

**What it is:** `scripts/scratch-daemon.sh` runs a SECOND, fully-isolated harmonik
daemon on a separate git clone and lets you rebuild + re-exercise it in seconds —
WITHOUT ever touching the live ("fleet") production daemon. It is a **general,
always-available capability**, not a remote-substrate-specific tool: reach for it
any time you want to drive the real daemon over a batch of beads on code you just
edited, without disturbing the fleet or polluting your main checkout's history.

**The win:** the real-daemon reproducer round-trip is otherwise a ~30-minute trip
(edit → wait for a fleet slot → watch one run → repeat). A scratch clone with its
own socket, tmux session, binary, and beads DB lets you iterate in **seconds**:
`cycle` (down → build → up) plus `batch` (submit + structured pass/fail) plus
`feedback` (turn failures into fleet beads). The fleet daemon keeps running
untouched the whole time.

> **Authoritative source:** every command, flag, env var, and guard described here
> is implemented in `scripts/scratch-daemon.sh`. If this doc and the script ever
> disagree, the script wins — re-read it (`./scripts/scratch-daemon.sh --help`
> prints the usage banner verbatim) and fix this doc.

---

## When to reach for it

- You changed daemon-core code (dispatch, queue, workflow-mode, merge, reviewer,
  remote-substrate routing) and want to see the REAL daemon process a real bead
  end-to-end — not just a unit/scenario test.
- You want to exercise the daemon over a **batch** of beads and get a machine-
  readable pass/fail verdict, without occupying fleet slots or risking the fleet's
  git history / beads ledger.
- You want a clean-room: no auto-revive supervisor, so a `down` stays down for a
  deterministic rebuild (the fleet's `supervise` path would revive the binary out
  from under you).

It pairs naturally with the fast remote reproducer, which exercises the
remote-substrate path against a localhost worker (no second machine needed):

```bash
go test -tags=scenario -run TestScenario_RemoteSubstrate_Localhost_E2E ./internal/daemon/
```

— but the harness itself is change-agnostic: any batch of beads, any daemon change.

---

## The full loop

Drive every command from your **fleet checkout** (the live repo). You pass the
scratch path explicitly on every call — you edit and build the scratch clone, but
you never need to `cd` into it, and you never run these from inside the scratch
clone.

```
init  →  build  →  up  →  batch  →  feedback  →  down
                    └────── cycle (down→build→up) ──────┘   ← the fast inner loop
```

| Subcommand | Arg surface (exactly as the script accepts) | What it does |
|---|---|---|
| `init`     | `init <scratch-path> [<source-repo>]` | Clone harmonik into `<scratch-path>` (default source = this repo's `origin`), build, and `harmonik init --project <scratch> --force --no-supervise` if no `.harmonik/config.yaml` yet. |
| `build`    | `build <scratch-path>` | Build the scratch binary FROM the clone → `<scratch>/.harmonik/bin/harmonik`. |
| `up`       | `up <scratch-path>` | Start the bare `harmonik --project <scratch>` binary in its own tmux session; wait (≤45s) for the socket. NO supervisor. |
| `status`   | `status <scratch-path>` | Print project path, tmux session liveness, socket presence, daemon PID state, last 10 log lines. |
| `down`     | `down <scratch-path>` | Stop ONLY the scratch daemon (argv-verified PID kill), tear down its confirmed tmux session, remove the stale socket. |
| `cycle`    | `cycle <scratch-path>` | `down` → `build` → `up`. The fast inner loop after each edit. |
| `batch`    | `batch <scratch-path> <name> --beads id1,id2,…`  **or**  `batch <scratch-path> <name> --file <queue.json>` | Submit a named batch to the SCRATCH queue, await every item's terminal event, emit a structured pass/fail summary (JSON artifact + grep-able stdout). |
| `feedback` | `feedback <results-json> [--batch <name>] [--priority N] [--dry-run]` | For every FAIL in a batch results artifact, create-or-update a deduped, OPEN bead on the **fleet** beads DB. The one deliberate fleet write. |

### Tunables (environment variables)

| Env var | Applies to | Default | Effect |
|---|---|---|---|
| `SCRATCH_MAX_CONCURRENT` | `up` | `1` | daemon `--max-concurrent`. |
| `SCRATCH_WORKFLOW_MODE`  | `up` | `review-loop` | daemon `--workflow-mode`. |
| `SCRATCH_DAEMON_FLAGS`   | `up` | (empty) | extra flags appended verbatim to the daemon start. |
| `SCRATCH_BATCH_TIMEOUT`  | `batch` | `1800` | max seconds `batch` waits for terminal events before marking the rest `incomplete`. |

(`up`-stage env vars take effect at the next `up`/`cycle`, since they are read when
the daemon process starts.)

### Prerequisites

`batch` and `feedback` need `jq` (the script hard-fails with a clear message if it
is missing). `feedback` needs `br` available and a `.beads` dir under the fleet
root.

---

## `batch` — output contract

`batch` is stable + parseable so a later step (and `feedback`) can consume it
deterministically:

- **JSON artifact** at `<scratch>/.harmonik/batch-<name>-<queue_id>.json` — an array
  of `{ "bead", "run_id"|null, "verdict": "pass"|"fail"|"incomplete", "fail_signature"|null }`.
  `fail_signature` is a one-line (≤200 char) excerpt of the run's failure summary.
  This file is the authoritative input to `feedback`.
- **Stdout lines** (grep-able; `BATCH_ITEM` rows are tab-separated):

  ```
  BATCH_SUBMIT  name=<name> queue_id=<id> items=<n>
  BATCH_ITEM<TAB><bead><TAB><verdict><TAB><run_id|-><TAB><fail_signature|->
  BATCH_SUMMARY name=<name> total=<n> pass=<p> fail=<f> incomplete=<i> results=<path>
  ```

- `incomplete` = no terminal event arrived before `SCRATCH_BATCH_TIMEOUT`.
- **Exit code:** `0` only if every item passed; `1` if anything failed or stayed
  incomplete — so you can branch on it in a script.

Notes that matter for accuracy:
- `<name>` is BOTH the named queue and the summary label. It must match
  `^[A-Za-z0-9._-]+$` (the script rejects `/`, `..`, etc.) and must come BEFORE the
  flags. Exactly one of `--beads` / `--file` is required.
- The in-file `queue` field is ignored by `queue submit`, so `batch` always passes
  `--queue <name>` explicitly (the named-queue route). `--file` mode reads the bead
  set from `.groups[].items[].bead_id`.
- `batch` brings the daemon up itself if the socket is absent, so you can skip a
  separate `up` — but it will not rebuild; run `cycle` (or `build`) after an edit.

---

## Worked example 1 — a trivial 1-bead batch

The smallest possible loop: stand up a scratch daemon and run one bead through it.

```bash
# From your fleet checkout. One-time setup (clone + init + build).
./scripts/scratch-daemon.sh init  /tmp/hk-scratch
./scripts/scratch-daemon.sh up    /tmp/hk-scratch        # build happened in init; just start it

# Run a single bead as a named batch. 'smoke' is the queue + summary label.
./scripts/scratch-daemon.sh batch /tmp/hk-scratch smoke --beads hk-test001
```

Expected stdout (shape):

```
BATCH_SUBMIT  name=smoke queue_id=019ee... items=1
BATCH_ITEM	hk-test001	pass	019ee...-run	-
BATCH_SUMMARY name=smoke total=1 pass=1 fail=0 incomplete=0 results=/tmp/hk-scratch/.harmonik/batch-smoke-019ee....json
```

After editing daemon code in `/tmp/hk-scratch`, re-run the inner loop and the batch:

```bash
./scripts/scratch-daemon.sh cycle /tmp/hk-scratch          # down → build → up, in seconds
./scripts/scratch-daemon.sh batch /tmp/hk-scratch smoke --beads hk-test001
```

When done:

```bash
./scripts/scratch-daemon.sh down  /tmp/hk-scratch
```

---

## Worked example 2 — the remote-substrate scenario batch

Use `--file` mode to drive a multi-bead batch that exercises the remote-substrate
routing path. The daemon-side change you want to test lives in the scratch clone;
the localhost-worker reproducer test above is the unit-level counterpart, and this
is the full-daemon counterpart.

First, a minimal queue file. The `queue submit <file>` format is just a
`schema_version` + `groups[].items[].bead_id` document (a `wave` group runs a
closed batch; `--max-concurrent > 1` lets a wave's items dispatch in parallel):

```json
{
  "schema_version": 1,
  "groups": [
    {
      "kind": "wave",
      "items": [
        { "bead_id": "hk-remote-a" },
        { "bead_id": "hk-remote-b" },
        { "bead_id": "hk-remote-c" }
      ]
    }
  ]
}
```

Save it as `/tmp/remote-batch.json`, then:

```bash
# Edit the remote-substrate code in the scratch clone, then rebuild + restart.
# SCRATCH_MAX_CONCURRENT lets the wave's items run concurrently; SCRATCH_DAEMON_FLAGS
# appends any extra daemon flags your scratch config needs verbatim.
SCRATCH_MAX_CONCURRENT=3 ./scripts/scratch-daemon.sh cycle /tmp/hk-scratch

# Submit the remote-substrate beads as a named batch and collect the verdict.
# SCRATCH_BATCH_TIMEOUT widens the wait if remote runs are slow (default 1800s).
SCRATCH_BATCH_TIMEOUT=3600 \
  ./scripts/scratch-daemon.sh batch /tmp/hk-scratch remote-substrate --file /tmp/remote-batch.json
```

Expected stdout (shape — one `BATCH_ITEM` per bead, here with a failure):

```
BATCH_SUBMIT  name=remote-substrate queue_id=019ee... items=3
BATCH_ITEM	hk-remote-a	pass	019ee...-r1	-
BATCH_ITEM	hk-remote-b	fail	019ee...-r2	worker_unhealthy: ssh dial timeout after 30s
BATCH_ITEM	hk-remote-c	pass	019ee...-r3	-
BATCH_SUMMARY name=remote-substrate total=3 pass=2 fail=1 incomplete=0 results=/tmp/hk-scratch/.harmonik/batch-remote-substrate-019ee....json
```

The batch exits `1` (one fail), and the results artifact is ready for `feedback`.

---

## `feedback` — turning scratch failures into fleet beads

```bash
# Dry-run first: print the create/update plan, touch no DB.
./scripts/scratch-daemon.sh feedback \
  /tmp/hk-scratch/.harmonik/batch-remote-substrate-019ee....json --dry-run

# For real: file/refresh OPEN beads on the FLEET ledger (default priority 2).
./scripts/scratch-daemon.sh feedback \
  /tmp/hk-scratch/.harmonik/batch-remote-substrate-019ee....json --priority 1
```

What it does, precisely:
- Reads the batch results artifact and acts on **FAIL items only** (`pass` and
  `incomplete` are ignored).
- Each fail maps to a stable provenance key
  `prov:<hash>` where `hash = sha256(<batch-name> 0x1f <fail_signature>)[:12]`. The
  queue_id is deliberately excluded so a re-run of the SAME failure dedupes.
- Before creating, it looks up an existing OPEN bead by `prov:<hash>`. A hit →
  it **updates** that bead in place (refreshes `--notes`, appends a recurrence
  comment) instead of filing a duplicate. A miss → it **creates** a fresh OPEN bead
  (type `bug`, labels `codename:test-daemon-harness,scratch-feedback,prov:<hash>`).
  A previously-CLOSED bead is intentionally NOT reused: a failure that recurs after
  being marked resolved files a fresh, actionable bead.
- `--batch <name>` overrides the batch name in the provenance key + title/body
  (default is parsed from the `batch-<name>-<qid>.json` filename).

Stdout (grep-able; `FEEDBACK_ITEM` rows tab-separated):

```
FEEDBACK_ITEM	create	<scratch-bead>	prov:<hash>	<fleet-bead-id>
FEEDBACK_SUMMARY batch=<name> fail_items=<n> created=<c> updated=<u> db=<fleet>/.beads
```

> **This is the one deliberate fleet write.** Beads are created **OPEN and never
> assigned** (the daemon owns claiming) and never `in_progress` — so they surface in
> `br ready` / `kerf next` like any other work. Nothing is auto-dispatched; you (or
> the fleet daemon) pick them up normally. No surprise: `feedback` writing to the
> fleet ledger is its entire purpose.

---

## Safety guarantee — it NEVER touches the fleet daemon

Every subcommand except `feedback` operates ONLY on the scratch clone you name.
The script prints a one-line safety banner on every invocation and enforces four
layers of protection (all in `scripts/scratch-daemon.sh`):

1. **Argv-gated PID kill (`down`).** `down` kills ONLY the PID named in
   `<scratch>/.harmonik/daemon.pid`, and only after confirming that live process's
   command line (`ps -o command=`) actually contains the scratch path. A non-numeric
   pidfile line, a stale/dead PID, or an argv that does not match the scratch path
   all refuse the kill. A blanket `pkill harmonik` (or even `pkill -f "harmonik
   --project"`) — which would take down the fleet daemon — is never run.
2. **Ownership-gated tmux teardown.** The scratch tmux session (`harmonik-<hash>-
   default`) is killed ONLY when the live-argv ownership proof above succeeded. An
   unconfirmed session is left untouched (with a NOTE), because harmonik freezes a
   `-default` session as the single spawn target — killing an unconfirmed one could,
   in a stale-pidfile + symlinked-to-fleet tail case, take down the FLEET's spawn
   target.
3. **`guard_path` — scratch ≠ fleet refusal.** Every command resolves the scratch
   path with symlink resolution (`pwd -P`, matching harmonik's own
   `filepath.EvalSymlinks` / PL-006a) and hard-refuses an empty path, `/`, or this
   script's own repo root (the fleet checkout). Because both sides are
   symlink-resolved, a scratch path that is a symlink pointing at the fleet repo is
   also caught.
4. **`assert_not_supervised`.** `up` / `down` / `batch` refuse a project that has a
   live `hk-<hash>-supervise` session — that is a supervised fleet deployment, never
   a throwaway scratch clone.

The ONE deliberate exception is `feedback`, which writes to the fleet beads ledger
on purpose (see above). It runs `br` with the fleet repo root (located via the same
`fleet_root` used by `guard_path`) as CWD, so it targets the fleet's `.beads/*.db`
and never the scratch clone's — and it only ever creates/updates OPEN beads, never
claims or transitions them.

If you ever stop a daemon by hand while a scratch daemon is also running, kill by
the exact PID from the relevant `.harmonik/daemon.pid`, never by name.

---

**Refs:** `scripts/scratch-daemon.sh` (source of truth); hk-4tdlw (init/build/up/
cycle loop), hk-1gkc8 (batch + feedback). Cross-linked from
[docs/known-workarounds.md](known-workarounds.md).
