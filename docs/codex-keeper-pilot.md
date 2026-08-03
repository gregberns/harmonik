# Codex keeper pilot

This pilot keeps one Codex crew moving after a completed turn. It does not use
a completed turn as proof that the crew is done. A separate work-state file is
the authority.

The current bridge reads one explicit Codex rollout file. This is for an
already running tmux session. It is a temporary bridge because rollout JSONL is
not a public Codex contract. A later launch should use the documented
`agent-turn-complete` notification path.

## Safety model

The operator creates the work-state file. Its only values are `remaining`,
`blocked`, `awaiting_assignment`, and `terminal`.

Only `remaining` permits a prompt. The watcher also does not prompt when the
last assistant message asks the operator for a decision. It records one action
per completed turn in its journal. It stops after the configured prompt cap.

The source file and tmux pane are required arguments. The watcher never finds
the newest Codex session by CWD. That can select another agent.

## Run the current Bravo session

First check the watcher without sending a prompt.

```sh
pilot=/Users/gb/github/harmonik-wt/codex-keeper-pilot
state=/Users/gb/github/harmonik-wt/bravo/.harmonik/keeper/bravo-codex-work-state
journal=/Users/gb/github/harmonik-wt/bravo/.harmonik/keeper/bravo-codex-journal
rollout=/Users/gb/.codex/sessions/2026/08/01/rollout-2026-08-01T17-42-44-019fbfec-547b-7540-a485-24e4a50038cf.jsonl
mkdir -p "$(dirname "$state")"
printf 'remaining\n' > "$state"
"$pilot/scripts/codex-keeper-watch.sh" --agent bravo --tmux bravo:1.1 \
  --work-state "$state" --journal "$journal" --rollout "$rollout" \
  --idle-secs 120 --poll-secs 20 --max-nudges 3 --dry-run --once
```

After the dry run is correct, start the live watcher in its own tmux window.

```sh
tmux new-window -t bravo -n keeper-codex \
  "$pilot/scripts/codex-keeper-watch.sh --agent bravo --tmux bravo:1.1 --work-state $state --journal $journal --rollout $rollout --idle-secs 120 --poll-secs 20 --max-nudges 3"
```

Set the work state to `terminal` before the final hand-back. Set it to
`blocked` or `awaiting_assignment` when the crew needs an operator input. Stop
the watcher with `tmux kill-window -t bravo:keeper-codex`.

## Use the documented signal on a later launch

Codex sends `agent-turn-complete` notifications to the configured `notify`
program. Call `scripts/codex-keeper-notify.sh --event <file>` from a wrapper
that also calls the existing notify program. Do not replace an existing notify
program without preserving it.

Then use `--event <file>` instead of `--rollout <file>`. The watcher sees the
same normalized fields in both cases. This isolates the temporary rollout
codec from the work-state policy and the tmux delivery step.
