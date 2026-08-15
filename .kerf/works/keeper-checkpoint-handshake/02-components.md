# Components

## 1. Context request policy

Own the configurable NOTICE, WARN, and HARD thresholds. A context sample sets urgency. It does not
grant restart authority.

## 2. Stop policy

Accept Stop as a typed event. Combine it with the pending request, operator guard, handoff state,
and cooldown. Return a decision and effects without I/O.

## 3. Handoff authority

Track the request marker separately from file freshness. A marked handoff plus a later Stop grants
automatic restart authority. `restart-now` grants explicit authority after it validates the
handoff.

## 4. Message renderer

Render typed NOTICE, WARN, reminder, and HARD messages. Configuration owns prose. Code owns the
agent, counts, request ID, handoff path, marker, and command.

## 5. Observation shell

Convert gauge, Stop, transcript, handoff, tmux, and session observations into typed events. Keep
safe reads active while operator interaction suppresses pane effects.

## 6. Restart effector

Use one clear, session-change, and brief sequence for automatic and explicit restart entry events.

## 7. Event record and replay tests

Record state changes and decision reasons. Drive the pure reactor with fake events and use narrow
recording ports for shell tests.

## Dependency direction

~~~text
observations -> typed events -> pure policy/reactor -> typed actions -> narrow ports
~~~

The reactor depends on values only. The shell depends on consumer-owned ports. Message delivery and
restart effects remain separate ports.

