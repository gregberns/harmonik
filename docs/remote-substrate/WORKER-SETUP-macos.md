# Harmonik Remote Worker — macOS Setup Guide (Phase 1)

> Hand this file to a Claude Code session running **on the spare Mac** to provision it as a
> harmonik remote worker. It installs the toolchain, joins the tailnet, clones the repo, and
> self-verifies. One step (the Claude login + Tailscale join) is human-only and must happen
> FIRST — see Part 1.
>
> Target: an Apple-Silicon Mac, admin access, on the same Tailscale account as the harmonik
> box ("box A"). Result: box A can `ssh <worker>` and spawn bead-work there, billed to the
> **subscription**.

---

## Part 0 — What this sets up

A worker Mac that runs harmonik bead-work (a headless Claude Code session + a git worktree +
the build/test gate) on behalf of box A's daemon. Box A drives it over SSH; results return as
a run branch in the worker repo that box A fetches directly over SSH and merges (hk-7bwx — no
worker→GitHub push). Nothing here changes box A.

**Toolchain to be present:** `git`, `tmux`, `gh`, Go, Node, Claude Code CLI, Tailscale.

---

## Part 1 — HUMAN, one-time (do this before handing the rest to a Claude agent)

These need a browser / interactive prompts; a headless agent can't do them.

1. **Install Tailscale and join the tailnet** (same account as box A):
   - GUI app from tailscale.com, or `brew install tailscale && sudo tailscale up --ssh`
   - `--ssh` enables Tailscale SSH so box A authenticates by tailnet identity (no key mgmt).
   - Note the worker's tailnet name (e.g. `worker-mac-1`) — box A's config will use it.
   - **CRITICAL — the SSH ACL must be `accept`-mode, NOT `check`-mode.** In the Tailscale
     admin **Access Controls**, the SSH rule granting box A's user → this worker must use
     `"action": "accept"`. In `check` mode every connection prints
     `# Tailscale SSH requires an additional check / visit: https://login.tailscale.com/...`
     and HANGS waiting for browser re-approval (then fails: `failed to fetch next SSH action`,
     rc 255). A daemon cannot click an approval URL, so `check`-mode breaks every bead spawn.
   - Verify from box A: `tailscale ssh <worker> -- hostname` returns INSTANTLY (no prompt).
2. **Log Claude Code into the subscription** (this is the billing-critical step, satisfies D2):
   - Install Claude Code first if absent (see Part 2 step 2), then run `claude` → `/login`.
   - Choose the **Pro/Max subscription** account. Do **NOT** use an API key.
   - Verify: the account shown is the subscription, and `echo $ANTHROPIC_API_KEY` is EMPTY.
     If `ANTHROPIC_API_KEY` is set anywhere (shell profile, env), REMOVE it — it silently
     overrides the subscription and bills API credits.
3. Now start a Claude Code session in your home dir and paste Parts 2–4 for it to execute.

---

## Part 2 — AGENT-executable (toolchain + repo)

Run these and verify each. Stop and report if any step fails.

1. **Homebrew** (if missing):
   `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`
2. **Core tools:** `brew install git tmux gh go node`
3. **Claude Code CLI** (if not already installed for the Part-1 login):
   `npm install -g @anthropic-ai/claude-code` (or the native installer per the official docs).
   Verify: `claude --version`.
4. **GitHub auth (READ-ONLY — no write/push scope needed):** the worker only *pulls* base
   commits; it does NOT push result branches. As of hk-7bwx, box A fetches each run branch
   DIRECTLY from the worker repo over SSH (`git fetch ssh://<worker>/<repo> run/<id>`), so the
   worker needs no valid GitHub *push* credential. Read access (public clone or `gh auth login`
   for a private repo) is sufficient. Verify clone access: `git -C ~/harmonik-worker/repo fetch origin`.
5. **Clone the repo** to the canonical path box A expects:
   `git clone https://github.com/<owner>/harmonik.git ~/harmonik-worker/repo`
   Verify: `git -C ~/harmonik-worker/repo rev-parse HEAD`.
6. **Build the toolchain once** (so bead build/test gates work):
   `cd ~/harmonik-worker/repo && go build ./...` (or `go install ./cmd/harmonik` if the worker
   should have the binary). Report success/failure.

---

## Part 3 — Box-A side config (the operator/daemon does this, NOT the worker)

1. **Worker registry** — create/extend `.harmonik/workers.yaml` on box A:
   ```yaml
   version: 1
   workers:
     - name: worker-mac-1        # tailnet name, used as the ssh target
       transport: ssh
       host: worker-mac-1
       os: darwin                # darwin | linux  — crews use this to know how to remediate
       repo_path: ~/harmonik-worker/repo
       max_slots: 4              # concurrent beads this worker accepts
       enabled: true             # flip to false to live-disable without deleting the entry
   ```
   Two optional top-level telemetry knobs (`report_interval_seconds`, `disk_floor_mb`) and
   the periodic `worker_report` event they drive are documented in
   [worker-reporting.md](worker-reporting.md).
2. **SSH multiplexing** — add to box A's `~/.ssh/config` so per-bead chatter reuses one
   connection:
   ```
   Host worker-mac-1
     ControlMaster auto
     ControlPath ~/.ssh/cm-%r@%h:%p
     ControlPersist 10m
   ```

---

## Part 4 — Verification checklist (run FROM box A)

All must pass before the worker is marked healthy:

```bash
ssh worker-mac-1 -- tmux -V                                  # tmux present
ssh worker-mac-1 -- claude --version                        # claude present
ssh worker-mac-1 -- git -C ~/harmonik-worker/repo rev-parse HEAD   # repo cloned
ssh worker-mac-1 -- 'test -z "$ANTHROPIC_API_KEY" && echo OK-no-apikey'  # no API key
ssh worker-mac-1 -- git -C ~/harmonik-worker/repo fetch origin   # read access (no push needed; hk-7bwx)
# Smoke: a trivial headless claude run confirms subscription auth works end-to-end
ssh worker-mac-1 -- 'cd ~/harmonik-worker/repo && claude -p "print the word READY and exit"'
```

The last line proves the worker can run Claude Code headless on the subscription with no
interactive login — which is exactly what a dispatched bead will do.

---

## Part 4b — Run the T4 (M4-C8) end-to-end Claude proof against this worker

Once Part 4 is green, prove the **Claude slice** end to end with the scenario test
`TestScenario_RemoteSubstrate_ClaudeSlice_RemoteWorker_E2E`
(`internal/daemon/scenario_remote_substrate_t4_claude_test.go`). It dispatches ONE bead whose
**real Claude process runs on this worker** over the production `ssh -- tmux/git` path — reverse
tunnel, `agent_ready` + `agent_input_acked` over the tunnel, commit, and merge back to box A's
`main` — with NO manual step. It SKIPS unless armed, so it is safe to leave in the normal test set.

**Preconditions (all of Part 4, plus):**
- Box A can `ssh <worker> true`, `ssh <worker> claude --version`, and `ssh <worker> tmux -V`
  (the test probes all three and skips with the failing one named).
- The worker's `claude` is logged into the **subscription** and `ANTHROPIC_API_KEY` is **empty**
  on the worker (Part 1 step 2). The daemon's D2 guard fail-closes the spawn if the key is set.
- A worker repo the run can materialize into. Two ways to supply it:
  - **Same-box proof** (`<worker>` shares box A's filesystem, e.g. `localhost`): leave
    `HARMONIK_T4_WORKER_REPO` unset — the test clones a throwaway repo under a temp dir.
  - **Genuine two-box proof** (`gb-mbp`): point `HARMONIK_T4_WORKER_REPO` at the worker's clone
    from Part 2 step 5 (e.g. `~/harmonik-worker/repo`), and ensure the worker can fetch box A's
    base commit (the same reachability Part 4 verifies).

**Exact command (run FROM box A, at the repo root):**

```bash
HARMONIK_T4_WORKER=gb-mbp \
HARMONIK_T4_WORKER_REPO=~/harmonik-worker/repo \
TMPDIR=/tmp/h \
go test -tags scenario -run TestScenario_RemoteSubstrate_ClaudeSlice_RemoteWorker_E2E ./internal/daemon/ -v
```

A same-box smoke (no separate worker) is just:

```bash
HARMONIK_T4_WORKER=localhost TMPDIR=/tmp/h \
go test -tags scenario -run TestScenario_RemoteSubstrate_ClaudeSlice_RemoteWorker_E2E ./internal/daemon/ -v
```

**PASS** means: a real Claude ran on the worker; `agent_ready` (provenance `claude_session_start`)
and `agent_input_acked` were observed over the reverse tunnel; `run_started.worker_name` matched
the host; and the worker's commit landed on box A's `main`. Leaving `HARMONIK_T4_WORKER` unset
skips the test (armed-only) — that is the expected state in CI and on the build box.

---

## Part 5 — Ongoing maintenance (dependency drift)

When a project adds a dependency the worker lacks, a bead's build/test gate will fail on the
worker. Recovery:
1. Run metadata records the worker name + OS, so the crew knows which box and how to reach it.
2. SSH in (`ssh worker-mac-1`) and install the dep (`brew install …`, `go install …`), or
   dispatch a small "provision worker-mac-1: <dep>" task.
3. Long-term, the worker's environment should be captured (a Brewfile, or an `adze` manifest)
   so it can be re-provisioned reproducibly — but that's a later refinement, not a V1 blocker.

---

## Notes / gotchas

- **Never** set `ANTHROPIC_API_KEY` on the worker — it overrides the subscription login and
  bills API credits (the credit-burn class we explicitly guard against).
- Keep box A's CWD discipline: the daemon addresses the worker by absolute paths over SSH; it
  never `cd`s into a worktree.
- If the worker is offline, box A marks it unhealthy and skips it (or falls back to local) —
  it does NOT remove the config entry. Re-run Part 4 to re-enable once it's back.
