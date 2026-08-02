# Change Design Review — 2026-08-01

Reviewer result:

```json
{"schema_version":1,"verdict":"APPROVE","notes":"The design preserves the Alpha-owned implementation boundary. It separates handler transport records from durable event payloads, states both run_started choices, and explicitly prohibits a synthetic workflow_id on the single-mode path. Source checks confirm the competing payload shapes and missing workflow identity. The draft is specific enough for Alpha to make the final selection."}
```

The review approves the design. It does not select a final core payload option.
