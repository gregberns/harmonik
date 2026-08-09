# Driving a real daemon with a fake agent

This is the proven way to run work end to end through a real daemon with no
model and no money. It was worked out twice, over hours, and lost both times
because it lived only in a session handoff. Verified on 2026-08-08 against
`work/bravo-reachability`. Three beads reached `complete-success`, one per
harness.

A *twin* is a fake agent binary that speaks a real harness's protocol.

## The one thing that surprises everybody

**The daemon never reads the twin's stdout.**

`agent_ready` has exactly one source: a `harmonik hook-relay SessionStart`
message on the daemon socket. `internal/daemon/agentlaunch.go` `OnLaunched`
registers the callback, and `internal/hook/sessionstore.go` `notifyAgentReady`
fires it from the hook-relay socket acceptor. Real Claude Code reaches it
because `internal/workspace/claudesettings_wm040a.go` `bridgeMatcherGroupFor`
writes `.claude/settings.json` hooks that call `harmonik hook-relay`.

The twin writes NDJSON to stdout. On the tmux substrate that stdout goes to a
pane nobody parses. So a twin that runs perfectly still ends in
`agent_ready_timeout`, and no scenario name fixes it.

Two runs, one variable, proved it:

- Twin invoked cleanly, all 11 NDJSON lines emitted, no hook relay:
  `agent_ready_timeout`, then `run_failed`.
- Shim that fires `hook-relay SessionStart` and does not invoke the twin for
  the handshake at all: `agent_ready` arrives with
  `provenance: claude_session_start`.

Codex and pi skip the handshake completely. Their `Completion()` returns
`CompletionProcessExit`, and `agentlaunch.go` derives `SkipReadyHandshake` from
it, because a harness that self-terminates on turn completion never emits
`agent_ready`. Their `agent_ready` events carry a nil payload and are synthetic.

## The shim is a translator, not a passthrough

No twin is a drop-in. Each one rejects the argv its own daemon builds:

| Harness | Why it dies |
|---|---|
| claude | `internal/harness/claude/launchspec.go` `BuildLaunchSpec` emits `--session-id`/`--resume`, `--model`, `--effort` and `--dangerously-skip-permissions`. The twin's FlagSet declares none of them and uses `flag.ContinueOnError`. |
| codex | `internal/harness/codex/launchspec.go` emits lowercase `-c sandbox_mode=...`. The twin declares `-C` and `--cd`. Go flag parsing is case sensitive. |
| pi | `cmd/harmonik-twin-pi/main.go` requires `--scenario`. Real pi never sends it. |

This matters beyond the rig. A twin that dies at flag parse exits 1 with no
output, and that takes the same branch a real handler failure takes — so a test
can go green without the twin ever running.

**Keep an argv sensor in every shim.** Make it refuse loudly when the argv is
not production shaped. A shim that silently discards the daemon's argv gives you
a green run that proves nothing about the real harness, which is how this stayed
invisible.

## Two environment variables must reach the daemon process

`tmux new-session` does not carry them. `update-environment` covers neither, and
the pane shell resets `PATH`. Passing `-e PATH=...` is silently lost. Put them
inside the pane command with `env`.

- `PATH` — with the shim directory first. The daemon's default `HandlerBinary`
  is the bare name `claude`, so it resolves through `PATH`.
- `CLAUDE_CONFIG_HOME` — pointed at a scratch directory. Without it a "fully
  isolated" scratch daemon takes a write lock on the operator's **real**
  `~/.claude.json`. While anyone is using Claude Code on the box that lock is
  contended and every run dies with
  `EnsureWorktreeTrust: ... write-lock acquire timed out`.
  `claudeGlobalConfigPath` in `internal/workspace/claudetrust_wm040b.go` reads
  the variable first.

Always verify both landed before trusting a result:

```
ps eww -p $(head -1 <scratch>/.harmonik/daemon.pid) | tr ' ' '\n' | grep -E '^(PATH|CLAUDE_CONFIG_HOME)='
```

## What each shim has to do

**claude** — five steps, in order:

1. Refuse with a distinct exit code unless the argv carries
   `--dangerously-skip-permissions`. This is the sensor.
2. Call `harmonik hook-relay SessionStart` with hook JSON on stdin. The
   `session_id` MUST equal `$HARMONIK_CLAUDE_SESSION_ID` and `hook_event_name`
   MUST equal the argv event kind, or the relay exits 1 with
   `bridge_session_id_mismatch` or `bridge_event_kind_mismatch`. This is the
   handshake.
3. Run the twin with a translated argv.
4. Wait a few seconds, then make the commit with a `Refs: <bead_id>` trailer.
   The wait is load-bearing: without it the pane child has already exited when
   the daemon pastes the brief, and you get
   `pasteinject_failed ... can't find pane`. The bead id is in
   `$HARMONIK_WORKSPACE_PATH/.harmonik/agent-task.md`.
5. Call `harmonik hook-relay Stop` with a `message` field. The relay maps it to
   `outcome_emitted{kind: WORK_COMPLETE}`.

**pi** — prepend `--scenario happy-path` and make an edit. The pi twin changes
nothing itself, so against a clean worktree the run trips
`implementer_no_work_suspected` and fails. With an edit present the daemon's own
post-exit fallback commits it. `HARMONIK_WORKSPACE_PATH` is unset on this path,
so use `pwd`.

**codex** — discard the daemon argv and re-derive the worktree from `-C` and the
bead id from `.harmonik/agent-task.md`. Keep the sensor.

## Use a workflow the twin can pass

The stock `workflow.dot` runs `make full` at the commit gate. No twin will ever
pass that. Use a minimal graph: start, implement, close.

## Reading the result

`events.jsonl` accumulates across every run in that project. Filter on the bead
id or run id you just submitted, or you will read an old failure as a new one.
Capture the line count before you submit and read only past it.

A healthy claude run emits 26 events ending
`outcome_emitted` → `bead_closed` → `run_completed` →
`queue_group_completed{final_status: complete-success}`.

Counting event types after a run is worth as much as reading errors — a missing
type is as informative as a failure line:

```
python3 -c "
import json,collections,sys
c=collections.Counter(json.loads(l)['type'] for l in open(sys.argv[1]) if l.strip())
[print(f'{v:4}  {k}') for k,v in c.most_common()]" <scratch>/.harmonik/events/events.jsonl
```

## Known noise, so you do not chase it

- `working_tree_refresh_failed` fires on every run that adds a new file. The
  scoped refresh asks the main checkout for paths that exist only in the branch
  it just merged. The run survives. Tracked.
- `run orphaned by daemon restart: no terminal event before shutdown` just means
  a run was in flight when you restarted the daemon.
- The first queue submitted after a daemon boot can wait minutes to dispatch.
  Later submits against the same warm daemon take seconds. Tracked.
