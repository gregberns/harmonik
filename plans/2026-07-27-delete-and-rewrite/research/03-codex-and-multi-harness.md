# Codex and multi-harness keeper design

## Recommendation

Use Codex's documented Stop hook as the first Codex crew vertical. Do not make
rollout-file polling and tmux paste the production design.

The Stop hook receives `session_id`, `turn_id`, `stop_hook_active`, and the
last assistant message. It can return `{"decision":"block","reason":"..."}`.
For Stop, that response creates a new continuation prompt. The hook protocol is
documented in the [Codex Hooks guide](https://learn.chatgpt.com/docs/hooks.md).
The hook must write JSON only to stdout.

This is a better signal than a file mtime. It is still not a completion
authority. A completed turn means only that Codex stopped its current turn.

## The authority for stop

Add a durable crew-work state with these closed values:

- `remaining` — assigned work remains. A bounded continuation is allowed.
- `blocked` — a decision or missing authority is needed. Alert and stop.
- `awaiting_assignment` — the crew completed known work but has no next unit.
  Stop and notify the captain. Do not manufacture more work.
- `terminal` — the stated crew assignment is complete. Stop.

The model's words and a special string are evidence, not authority. A nonce can
join a prompt to its answer. It is not a secret or a security boundary. The
durable work state and the crew's queue or mission rule decide whether the
keeper may continue.

The current mission front matter does not supply this state. Its `goal` and
`epic_id` are human-readable inputs. The post-core lane must choose a single
writer for machine-readable work state before enabling Codex continuation.

## Stop-hook decision policy

1. Reject a foreign or duplicate `(agent, session_id, turn_id)` event.
2. Persist the decision before returning hook output.
3. Stop and alert when work state is `blocked`, `awaiting_assignment`, or
   `terminal`.
4. Stop and alert when the final message asks for an operator decision.
5. Continue only when state is `remaining`, no veto is present, and the
   per-session cooldown and continuation cap permit it.
6. Use `stop_hook_active` to prevent an immediate hook loop. Escalate when the
   cap is reached.
7. Treat malformed hook input or unknown work state as an alert and no action.

This solves the two different cases in the request:

| Situation | Evidence | Result |
| --- | --- | --- |
| Codex finished a turn but assigned work remains | stop hook plus `remaining` | one bounded continuation |
| Codex completed its assigned work | stop hook plus `terminal` or `awaiting_assignment` | stop and report |

## Other options

| Option | Use | Limits |
| --- | --- | --- |
| Codex Stop hook | First production vertical | Requires trusted hook install and durable work state. |
| `scripts/codex-nudge.sh` | Supervised fallback only | Uses undocumented rollout records, CWD-based discovery, an untested injection path, and a shared tmux buffer. Pin identity and add fixtures before any use. |
| Codex app server | Later adapter | It offers structured `turn/completed`, thread status, token updates, and goals. It needs a crew integration spike and must not block the Stop-hook vertical. See the [App Server guide](https://learn.chatgpt.com/docs/app-server.md). |
| Special completion string | Correlation aid only | It cannot prove work completion or prevent a stale or copied response. |

The July rollout measurement remains useful for the fallback: 170 of 174
records ended with `task_complete`; among 36 observed stops, a decision-keyword
gate found seven cases that needed a human decision. It does not make the
rollout format a stable interface.

## Generic contract

The common keeper vocabulary is small.

~~~text
Signal: identity, activity, stop, context use, health, operator request
State: work state, hold, cooldown, continuation count, last processed turn
Decision: continue, reset-context, await-operator, await-assignment, terminal, alert
Effect: harness-specific delivery and durable outcome record
~~~

Claude maps its status line, hooks, transcript, handoff, and tmux controls to
this contract. Codex maps its Stop-hook JSON input and response to it.
App-server events can later map to the same signals. A new harness should add a
codec and delivery adapter. It must not add a case to a giant keeper watcher.

Keep the pure types in `internal/keeper`. Put codec wiring in the command
composition layer. Preserve the import fence: harness vertical packages do not
import keeper packages.

## Delivery sequence

1. Define the durable work-state owner and write its contract.
2. Extract a small pure continuation policy from `Watcher.Run`.
3. Add a Codex Stop-hook executable and hook installation path.
4. Prove the policy with fixtures, simulator, and live canary.
5. Make `harmonik start crew --harness codex` use the proven path. It currently
   fails closed in `internal/crewrun/launchspec.go`.
6. Investigate app-server support as a separately gated adapter. Do not tie it
   to the first Codex crew launch.

The long-lived Codex session belongs in tmux for operator inspection. The
keeper's primary continuation does not need tmux injection. Tmux remains a
fallback delivery channel and a health probe.
