# Claude-lane finding schema (shared with Codex lane)

Each RU agent writes ONE JSON file: `raw/<RU-ID>.json`, shape:

```json
{
  "chunk": "RU-01",
  "reviewer": "claude",
  "scope_reviewed": ["file1.go", "file2.go"],
  "status": "reviewed | partial | failed",
  "status_note": "why partial/failed, if applicable",
  "findings": [
    {
      "file": "internal/daemon/workloop.go",
      "line": 4210,
      "severity": "critical | high | medium | low | nit",
      "category": "concurrency | resource-leak | error-handling | nil-deref | spec-drift | over-abstraction | maintainability | dead-code | test-theater | coverage-gap | correctness",
      "title": "one-line",
      "detail": "what's wrong + why it matters + failure scenario",
      "suggested_fix": "direction only; we are NOT fixing in this pass"
    }
  ]
}
```

Rules (load-bearing):
- **Read the files yourself.** Do not trust summaries.
- **Failed != clean.** If you cannot read the scope, set status="failed" — never emit empty findings as if reviewed.
- **Cite `file:line`** for every finding.
- **severity=nit** is for pure style/naming/typo/preference. Real problems are low+.
- Focus: major correctness, concurrency/resource hazards, architecture, maintainability, sloppy/unmaintainable code. Include bad code even if unchanged vs main.
- **findings-as-data only.** NEVER edit the tree.
</content>
</invoke>
