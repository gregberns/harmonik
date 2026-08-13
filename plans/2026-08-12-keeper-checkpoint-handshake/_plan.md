# Plan: keeper checkpoint and restart handshake

## Purpose

Define how the keeper asks an agent to reduce context without destroying active work.

This plan records the history, the current behavior, and the available signals. It does not yet
choose the final hard-stop behavior. The handshake must be clear before the state machine changes.

## Operator direction

The keeper should use three configurable context bands.

1. The first band gives the agent a choice. The message shows the current context and explains how
   to restart. The agent can continue its current work or restart at a good stopping point.
2. The second band gives a stronger request. The agent should settle active work and restart soon.
3. The final band is a safety backstop. The keeper must prevent context exhaustion, but it must not
   treat normal work delay as agent failure.

The example values of 170k and 180k are not decisions. Configuration must own all band values.

The first live trial uses these absolute values:

| Band | Tokens | Intent |
|---|---:|---|
| NOTICE | 170,000 | Give the agent room to shape the work for session continuity. |
| WARN | 200,000 | Ask the agent to favor completing the handoff soon. |
| HARD | 220,000 | Report that the normal transition did not complete and apply the separate hard policy. |

These values are configurable policy. They are trial values, not permanent constants.

The agent owns the decision that work reached a good stopping point. A good stopping point often
requires more than one completed turn. The agent can have active subagents, tests, tools, an
operator exchange, or several related edits to settle.

A fixed short timeout must not classify useful work as failure. A repeated timeout must not lead to
a forced restart.

Message text must have compiled defaults and simple configuration overrides. Operators must be able
to adjust the text as they learn from live use.

The first NOTICE default is:

> KEEPER NOTICE — We use periodic session transitions to keep our work token-efficient. This
> session is at 170k tokens. We generally aim to continue in a fresh session before 200k. As you
> continue, shape the work toward a state that a fresh session can resume without losing decisions
> or repeating work.
>
> When ready, run `/session-handoff HANDOFF-alpha.md` and include
> `<!-- KEEPER:cyc-123 -->`. Then run
> `harmonik keeper restart-now --agent alpha --nonce cyc-123` so we can continue in the fresh
> session.

The rendered message uses live values for the agent, context, next band, handoff path, marker, and
command. The example values above only show the intended prose.

Keep these candidate continuity lines in the plan for later live trials:

- “As you continue, shape the work toward a state that a fresh session can resume without losing
  decisions or repeating work.”
- “As you continue, shape the work so a fresh session can resume it without losing decisions or
  repeating work.”
- “Keep the current work moving. Shape its state so a fresh session can continue without losing
  decisions or repeating work.”
- “Preserve useful momentum, and favor a session transition when the work can continue from its
  durable state.”
- “Continue the current line of work while preparing a clear continuation for the fresh session.”

The first line is the selected default. The others are trial candidates, not fallback behavior.

## Historical record

### June: warn, then restart near a clean boundary

The first keeper work used two main bands. WARN asked the agent to wind down its work. ACT started
the handoff and restart near an idle boundary.

The design used the Stop hook as the best available boundary signal. A Stop can mean completed
work, a question, or a wait for a subagent. Stop is useful evidence, but Stop alone is not permission
to clear the session.

Sources:

- `.kerf/works/session-keeper/01-problem-space.md`
- `.kerf/works/session-keeper/04-research/findings.md`
- `.kerf/works/session-keeper/12-experiments-findings.md`
- `.kerf/works/session-keeper/05-specs/session-keeper-spec.md`

### July 13: the fixed handoff deadline became normative

`specs/session-keeper.md` added an `AwaitingHandoff` state with a 300-second timer. A missing fresh
handoff at the deadline moves the cycle to `Aborted`. Repeated handoff timeouts can cause a forced
restart.

This rule is the main contract defect. It treats the time required to finish work as an agent
failure. It also starts the deadline when the keeper submits a tmux message, not when the agent
receives that message.

### July 17: the design returned to agent-owned checkpoints

The restart timing work recorded the intended sequence:

- Finish the operator exchange.
- Finish the active unit of work.
- Save all state needed for the next session.
- Write the handoff.
- Run a self-restart command.

The research also described the exact late-handoff failure. The watcher can expire before an agent
finishes a long turn or receives the queued message.

Sources:

- `plans/2026-07-17-keeper-restart-timing/DIRECTION.md`
- `plans/2026-07-17-keeper-restart-timing/_plan.md`
- `plans/2026-07-17-keeper-restart-timing/C6-findings.md`

### July 18: self-service messages were designed and partly built

The restart-delivery work designed a leader message with four parts:

1. Finish the operator exchange.
2. Finish the active unit.
3. Apply a good-stopping-point test.
4. Run `/session-handoff`, then run `harmonik keeper restart-now`.

The work also added configurable message text and comms delivery for leaders. However, its spec
changes remained drafts. It kept the 300-second automatic timeout and used `restart-now` as a later
escape path after the automatic cycle aborted.

Sources:

- `.kerf/works/2026-07-18-keeper-restart-delivery/01-problem-space.md`
- `.kerf/works/2026-07-18-keeper-restart-delivery/05-spec-drafts/session-keeper-amendment.md`
- `.kerf/works/2026-07-18-keeper-restart-delivery/07-tasks.md`

### August: reliability and architecture improved without changing this policy

The attach-guard work stopped the keeper from missing a handoff that was already on disk. The
policy and ports refactor made the cycle easier to test. Both works kept the existing timeout
policy outside their scope.

Sources:

- `plans/2026-08-09-keeper-attach-guard/_plan.md`
- `.kerf/works/keeper-port-boundaries/01-problem-space.md`

## Current behavior

### Current bands

The repository configuration currently uses these absolute bands:

| Band | Tokens | Current effect |
|---|---:|---|
| WARN | 200,000 | Emit a warning and deliver one of several warning messages. |
| ACT | 215,000 | Start the automatic handoff, clear, and resume cycle. |
| FORCE-ACT | 240,000 | Bypass several normal gates and start the same automatic cycle. |
| Hard ceiling | 280,000 | The configured mode is `alarm`, so it reports but does not restart. |

Percentage caps can make WARN or ACT happen earlier on smaller context windows.

### Current WARN messages

The watcher selects more than one WARN message.

The light message is:

> [KEEPER WARN] Context threshold crossed. At a clean stop: commit + write
> HANDOFF-&lt;name&gt;.md (KEEPER nonce). Keep working.

The actionable message is:

> [KEEPER WARN] Context at Nk tokens (warn Nk / act Nk). Self-restart now: (a) run
> /session-handoff, then (b) run `harmonik keeper restart-now --agent &lt;name&gt;`. Only at a
> clean stop; if mid-task, finish first — the keeper auto-restarts at Nk.

A reachable captain or admiral can instead receive the longer leader defer message through comms.
That message says to finish the operator exchange and active unit. It includes the four-part
good-stopping-point test and a nonce-bearing `restart-now` command.

The light and actionable text have live-reloadable configuration keys. The leader defer text also
has a configuration key. The watcher validates required phrases before it accepts some overrides.

### Current ACT message

ACT does not use the WARN message or its self-service choice. ACT directly submits this slash
command through tmux:

> /session-handoff &lt;path&gt; — IMPORTANT: include exactly this line verbatim in the handoff file:
> &lt;KEEPER marker&gt; — The handoff file already EXISTS: Read it first, then Write it.

The keeper opens `AwaitingHandoff` and starts `handoff_timeout` at submission time. The repository
sets that timer to three minutes. The package default is five minutes.

This is why agents appear to react to WARN. WARN is the only path that lets the agent choose when
to act. ACT is a direct slash command and can wait behind active tmux work before the agent sees it.

## Signals and actions available today

| Signal or action | What it proves | What it does not prove | Current confidence |
|---|---|---|---|
| Context gauge | The measured context crossed a band. | The agent can stop now. | High when fresh and bound to the correct session. |
| Stop hook marker | The agent ended a turn. | The task and subagents are settled. | High as a turn boundary. Low as restart permission. |
| Assistant transcript turn | The transcript has a recent assistant result. | The result completed the task. | Useful as a secondary observation. |
| User transcript turn | A recent real operator message exists. | The operator exchange is complete. | Useful for deferral. Automation must stay excluded. |
| Handoff file mtime | The handoff file changed recently. | The write is complete or belongs to this request. | Medium without a marker. |
| Handoff marker | The handoff contains the cycle nonce. | The agent has stopped all other work. | High proof that the requested handoff was written. |
| `.dispatching` marker | Harmonik queue work is active. | Independent subagents or local tools are settled. | High for dispatched work only. |
| `keeper restart-now` | The agent explicitly requests the restart. | Nothing, if the command validates the handoff first. | Strongest available readiness signal. |
| Session ID change | `/clear` created a new primary session. | The new session received its brief. | High clear confirmation. |
| Tmux submission success | Tmux accepted the input operation. | The agent received or processed the message. | Low as a receipt signal. |
| Comms delivery | The bus accepted a keeper message. | The agent read or acted on the message. | Needs a live receipt test. |
| PreCompact trigger | The runtime reports immediate context pressure. | A safe checkpoint exists. | High urgency, not restart permission. |

No current signal directly reports independent subagent completion. The parent agent must settle
those subagents before it requests a restart.

## Stop as a first-class decision point

The keeper should watch every Stop event. A Stop is significant because the agent can receive a
new instruction at that boundary. It also shows that the current model turn is no longer active.

Stop must not mean `clear`. Stop should cause the keeper to re-evaluate durable facts and choose a
non-destructive action.

The minimum Stop decision table is:

| State at Stop | Keeper decision |
|---|---|
| Below the advisory band | Record the Stop. Take no context action. |
| In the advisory band | Deliver or repeat the optional self-restart message, subject to cooldown. |
| In the settle band | Deliver the stronger settle message, subject to operator and delivery guards. |
| In the hard band, no confirmed handoff | Deliver the emergency handoff request. Do not clear. |
| Confirmed handoff, no explicit restart | Record that the handoff is ready. Apply the agreed restart policy. |
| Explicit `restart-now` | Validate the handoff, then clear and resume. |
| Recent operator interaction | Suppress autonomous prompts that can interrupt the exchange. Record why. |
| Active `.dispatching` | Defer restart. Record why. |

Repeated Stop events are useful. They let the keeper retry a deferred message without polling the
pane or using a short deadline. A cooldown prevents a message on every short turn.

The keeper should keep the last processed Stop identity. A duplicate Stop must not repeat an
effect. The best identity depends on the harness:

- Claude can use the session ID plus a hook event identity or marker timestamp.
- Codex can use the session ID and turn ID.
- A transcript observation can remain a backstop when a hook is unavailable.

## Operator interaction at Stop

The current five-minute `operator_turn_lookback` is a reasonable safety default. It detects a real
operator transcript turn and excludes Harmonik automation messages.

The current `post_answer_grace` adds a second guard after the agent answers. The repository sets it
to 30 seconds. Together, these guards reduce the chance that the keeper inserts a message into an
active exchange.

The new design should keep these guards but narrow what they control:

- Recent operator interaction suppresses keeper message delivery and automatic restart decisions.
- It does not suppress safe observation of Stop, transcript, gauge, or handoff state.
- It does not discard a pending context request.
- The keeper re-evaluates the request after the lookback expires or after a later Stop.
- An explicit agent `restart-now` remains authoritative because the agent chose that moment.

Five minutes is a policy value, not a proof that the conversation ended. Configuration should keep
the value adjustable. Tests should cover an exchange that lasts longer than the lookback and an
operator who returns after the keeper message.

An improved rule can later combine recent operator input with conversation shape. For example, the
keeper can treat a recent operator question followed by an agent answer as an active exchange until
the post-answer grace expires. The first implementation should not infer intent from transcript
text.

## Codex continuation history and reuse

The Codex continuation design exists in
`plans/2026-07-27-delete-and-rewrite/research/03-codex-and-multi-harness.md`.

It proposed a Codex Stop hook that can return a continuation prompt. It also required durable work
state with four values:

- `remaining`
- `blocked`
- `awaiting_assignment`
- `terminal`

The planned rule was `Stop + remaining -> one bounded continuation`. Other durable states stop and
report. The design correctly states that a completed turn is evidence, not completion authority.

The production continuation vertical is not present today. The codebase has a structured Codex
driver and `turn/completed` events, but it does not have the proposed durable work-state owner or
the Codex Stop-hook decision policy.

The shared concept is still useful:

~~~text
Stop signal + durable work state + operator guard + context state -> decision
~~~

The decisions differ by purpose:

| Harness and condition | Decision |
|---|---|
| Claude, context pressure, work remains | Ask the agent to settle and self-restart later. |
| Claude, confirmed handoff and explicit restart | Clear and resume. |
| Codex, assigned work remains | Send one bounded continuation. |
| Codex, blocked or awaiting operator | Stop and report. |
| Either harness, recent operator exchange | Do not inject an autonomous continuation or restart message. |

This suggests a shared pure Stop policy with harness-specific effects. It does not suggest one
large watcher with Claude and Codex branches.

## Current `keeper restart-now` behavior

`keeper restart-now` is synchronous. It does not wait for the automatic cycle.

It performs these main checks and actions:

1. Resolve the target pane.
2. Read and validate the primary session ID.
3. Require a recent handoff file.
4. Refuse while `.dispatching` exists, unless the caller uses the explicit force option.
5. Inject an acknowledgement line.
6. Inject `/clear`.
7. Inject the restart brief.
8. Emit a restart audit event on success.

This path is close to the desired agent-owned handshake. It needs focused live and scenario tests.
Its handoff check currently uses freshness, not a required request marker.

## Handshake design space

### Option A: agent-owned two-command restart

The keeper sends an advisory message. At a good stopping point, the agent runs:

1. `/session-handoff` with the required path and marker.
2. `harmonik keeper restart-now --agent <name> --nonce <id>`.

The second command validates the handoff and drives clear and resume. The keeper sends no new
message between readiness and handoff. This avoids the extra round trip described by the operator.

This option uses the strongest current readiness event. The agent invokes `restart-now` only after
it finishes the handoff.

### Option B: handoff marker starts the restart

The keeper watches for its marker in the handoff file. The marker starts clear and resume without
a separate `restart-now` command.

This removes one agent command. It also makes a file write an implicit destructive request. An
agent can save a draft handoff before it settles subagents, so this option needs a separate final
marker or atomic completion rule.

### Option C: explicit receipt before later self-restart

The first message asks the agent to run a small acknowledgement command. The acknowledgement says
only that the agent received the request. The agent later uses Option A.

This gives useful delivery data without granting permission to clear. The restart must not depend
on the receipt. A missing receipt can mean delayed delivery, not agent failure.

### Option D: keeper message subscription

The session subscribes to a keeper message source early in its life. The source delivers messages
without a tmux user-message paste. The agent can acknowledge through the same control surface.

This can improve delivery and sender identity. It does not solve checkpoint ownership by itself.
The design must prove that messages enter the active agent input stream. Bus acceptance alone is
not enough.

## Initial direction

Lean toward Option A for the normal path. It already exists, it needs no extra readiness exchange,
and it keeps destructive authority with the agent.

Use the context bands as a message progression:

| Band | Proposed behavior |
|---|---|
| Advisory | Show context and procedure. Give the agent the option to continue or restart. |
| Settle | Ask the agent to stop new work, settle tools and subagents, write the handoff, and run `restart-now`. |
| Hard stop | Use an explicit emergency policy. Do not reuse normal handoff timeout escalation. |

The hard-stop contract remains open. A forced `/clear` without a confirmed handoff is not safe.
Submitting `/session-handoff` to a busy pane also does not prove delivery. The next design pass must
define what the keeper can safely force at this band.

The likely safe minimum is to force the handoff request, not the clear. The keeper can keep the
request active until it sees a marked handoff or an explicit `restart-now`. It can report the delay
as an urgent state without calling useful work a failure.

### Refined normal handshake

The normal handshake needs no acknowledgement round trip.

1. A context band crossing records a pending request.
2. A Stop event gives the keeper a safe chance to deliver the correct message.
3. The agent can continue work after the advisory message.
4. At a good stopping point, the agent runs `/session-handoff` with the request marker.
5. After the handoff is complete, the agent runs the supplied `keeper restart-now` command.
6. `restart-now` validates the handoff, clears the session, and sends the restart brief.

The keeper can observe handoff edits during this sequence. It must not infer completion from an
mtime change. The explicit `restart-now` command is the normal restart permission.

The optional receipt command remains telemetry only. It can help distinguish queued tmux input
from agent receipt, but it must not become a required handshake step.

### Separate policy axes

The design must not overload WARN and ACT names. Three independent questions exist:

1. **Urgency:** advisory, settle, or emergency.
2. **Opportunity:** active turn, Stop boundary, recent operator exchange, or quiet session.
3. **Authority:** no handoff, marked handoff, or explicit restart request.

A context band sets urgency. A Stop provides an opportunity. A handoff and `restart-now` provide
authority. No one signal should stand in for all three.

## Required spec changes

The next normative change should amend `specs/session-keeper.md` before code changes.

1. Split a restart request from an active restart cycle.
2. Remove `handoff_timeout -> Aborted` as the normal response to a missing handoff.
3. Remove handoff-delay escalation to forced restart.
4. Start bounded restart timing only after the agent writes a confirmed handoff or invokes
   `restart-now`.
5. Define the three message bands and their configurable thresholds.
6. Define configurable templates for advisory, settle, and hard-stop messages.
7. Define which template fields are required for safety and which prose is free-form.
8. Define the role of Stop, transcript turns, handoff freshness, handoff markers, and
   `.dispatching`.
9. Define the hard-stop contract separately from the normal path.
10. Define receipt acknowledgement as optional telemetry, not restart permission.
11. Define Stop as a first-class event that re-evaluates pending requests.
12. Define duplicate Stop handling and per-message cooldowns.
13. Define operator interaction as a delivery guard, not an observation guard.
14. Define a harness-neutral Stop decision vocabulary with separate Claude and Codex effects.

The July 18 SK-022 through SK-037 draft must be reconciled into this change. It must not enter the
normative spec unchanged because it preserves the fixed timeout workaround.

## Test obligations

The implementation must use a fake clock and recording ports. Each negative assertion must include
positive proof that the relevant path ran.

Required scenarios include:

- Advisory message received while the agent continues useful work for longer than five minutes.
- Settle message queued behind a long tool command.
- Stop while subagents remain active, with no handoff and no clear.
- Several Stop events before the agent reaches a checkpoint.
- Duplicate delivery of the same Stop event.
- Stop during a recent operator exchange, followed by retry after the guard expires.
- Stop after an operator answer but inside `post_answer_grace`.
- Stop after the operator guard expires while the context request remains pending.
- Handoff written in several edits, with the marker added last.
- Fresh handoff without a marker.
- Marked handoff without `restart-now`.
- `restart-now` after a marked handoff.
- `restart-now` with a missing, stale, or partial handoff.
- Active `.dispatching` refusal and later retry.
- Operator conversation during each band.
- Tmux submission without agent receipt.
- Comms delivery without agent receipt.
- PreCompact while tools or subagents remain active.
- Hard-stop behavior at the final configured band.
- Restart confirmation through a new session ID and a delivered brief.
- Codex Stop with durable work state `remaining` and one bounded continuation.
- Codex Stop with `blocked`, `awaiting_assignment`, or `terminal` and no continuation.

Existing tests that require `handoff_timeout -> Aborted` defend the old contract. Change them only
after the normative spec changes.

## Questions for the contract discussion

1. Should the advisory and settle bands both use the same two-command self-restart procedure?
2. Should a marked handoff alone start a restart, or must `restart-now` remain the explicit request?
3. What may the keeper force at the hard band: a message, `/session-handoff`, or `/clear` after a
   confirmed handoff?
4. How should PreCompact differ from the configured hard band?
5. Should the optional receipt command report only `received`, or also an agent-selected state?
6. Can the current comms follow path prove agent receipt, or does it prove only bus delivery?
7. Should independent subagent state become an observable signal, or should the parent agent remain
   its sole owner?
8. Should a confirmed marked handoff without `restart-now` remain passive, or start a delayed
   automatic restart?
9. Which durable store should own the Codex work-state value?

## Work sequence

1. Agree on the conceptual handshake and hard-stop contract.
2. Create a kerf work for the cross-cutting spec and state-machine change.
3. Amend and finalize the normative session-keeper spec.
4. Add characterization tests for the current WARN, ACT, and `restart-now` paths.
5. Add failing scenario tests for the new three-band contract.
6. Change the pure cycle reactor and its events.
7. Change the shell and message delivery paths.
8. Verify `restart-now` with fake boundaries and a controlled tmux integration test.
9. Deploy one keeper lane and observe it before wider deployment.

## Out of scope for this plan

- Selecting final threshold values.
- Changing the live keeper before the contract is agreed.
- Treating transcript text as a command channel.
- Using a timer to infer that the agent failed.
- Clearing a session without a confirmed handoff.
