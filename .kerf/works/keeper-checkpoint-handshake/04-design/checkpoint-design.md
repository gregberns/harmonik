# Checkpoint handshake design

## Three independent axes

- Urgency comes from the context band.
- Opportunity comes from Stop and operator state.
- Authority comes from a marked handoff plus completion, or from `restart-now`.

No single input stands in for all three.

## Normal transition

~~~text
context crosses NOTICE
  -> record request and submit NOTICE

context crosses WARN
  -> strengthen the same request and submit WARN

handoff marker appears
  -> record confirmed handoff

Stop occurs after marker
  -> enter shared clear and resume tail

or restart-now occurs after marker
  -> enter shared clear and resume tail
~~~

## Deadline migration

The first code slice keeps a finite shell wake so the synchronous watcher does not wedge. When the
wake fires without a marked handoff, the cycle parks as pending rather than aborting. It does not
increment escalation or clear the managed session.

The next watcher tick must resume the same request identity. It must not scrub a late marker as a
stale marker from a new request. This requires durable pending-request state in the cycle journal.

The long-term shell should stop using a synchronous drive loop for the pending-request phase. The
watcher can then observe all inputs while the request remains pending.

## Operator interaction

The five-minute transcript lookback remains configurable. It suppresses autonomous message and
restart effects. It does not suppress Stop, transcript, gauge, or handoff reads. The request stays
pending and re-evaluates later.

## Message policy

NOTICE gives room and explains session continuity. WARN asks the agent to favor completing the
transition. HARD reports that the normal path did not complete. It does not blame the agent.

The selected NOTICE continuity line is:

> As you continue, shape the work toward a state that a fresh session can resume without losing
> decisions or repeating work.

The plan file keeps alternate lines for later trials.

