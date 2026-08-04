# Domain implementation review

Status: revise before persistence work

The first isolated `internal/continuity` slice passed its local tests but did
not yet meet the approved contract. Do not build a controller on it.

The next implementation pass must add:

1. Typed instance, generation, lease, crew, capability, work-scope, source,
   and external-block references.
2. A full claim record with identity, attempt, effect, transport variant,
   source watermark, and delivery uncertainty preservation.
3. A normal continuation claim allocation path. It must persist the claim
   before an action can be dispatched.
4. Exact decision-fence behavior. An open decision cancels only undispatched
   work. It cannot erase an in-flight `May` state.
5. Exact source-pair and declaration correlation rules.
6. Fixed reason enums. The core must not use free-form reason strings.
7. Source freshness and known-active-turn state.

The package is isolated and contains no prose inspection. Its first test suite
passes. It is a scaffold only until these findings are resolved and reviewed.
