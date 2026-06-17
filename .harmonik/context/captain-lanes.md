# Tier-2 context: captain lane registry (days cadence)
# Captain reads this on every boot (STARTUP.md Step 0b) BEFORE re-deriving lanes.
# Update: at end of each captain session or whenever a crew gets a new epic assignment.
# Stable across /clear cycles; ephemeral queue/run state belongs in HANDOFF.md (tier1).

## active_lanes

| crew | epic_id | epic_title (plain English) | queue | model |
|---|---|---|---|---|
| (fill at first session) | | | | |

## operator_initiatives

- (list active operator-directed projects with priority order, e.g. "leanfleet: fleet token-efficiency, P1")

## parked

- (lanes or epics on hold; include reason and unblock condition)

## next_lane_pipeline

- (upcoming epics not yet assigned, in priority order; move to active_lanes when a crew slot opens)
