# CQ-MIG-01 implementation review

## Verdict

**APPROVE**

Reviewed strictly as the claimed implementation range:

```text
8478de9fa104a69af4df569d5804c5730b56ad22..8785db9779428ef792181e0938267d3df3c11c5b
```

This verdict does not compare, merge, or approve the ancient branch outside
that range.

## Scope and contract

The range changes exactly the expected four files:

- `internal/queue/persistence.go`
- `internal/queue/migration_fault_test.go`
- `internal/lifecycle/startup_pl005_qm002.go`
- `internal/lifecycle/startup_pl005_qm002_test.go`

The production change is confined to `MigrateFromLegacy`, its private local
fault-seam implementation, and `LoadQueueAtStartup`'s migration-error handling.
No general transaction API, queue schema, unrelated persistence operation, or
excluded production symbol changed.

The implementation satisfies CQ-MIG-01 and the approved CQ-00/CQ-00B
migration evidence:

- legacy bytes are read and parsed before destructive action;
- an existing destination is parsed and required to be deeply equivalent to
  the intended legacy queue;
- empty, corrupt, unsupported-schema, same-ID-but-different, and
  different-ID destinations fail closed while preserving the legacy source;
- an absent destination is written through exclusive temp creation, complete
  write, file sync, close, rename, and queues-directory sync before legacy
  removal;
- an equivalent existing destination is still queues-directory-synced before
  legacy removal, completing the retry after rename success plus directory-sync
  failure;
- legacy removal precedes `.harmonik` directory sync, and an absent-legacy
  retry repeats that parent durability obligation;
- every injected cut retains at least one parseable intended copy, and a
  fault-free retry converges to one canonical copy;
- startup returns the migration error before queue enumeration, ledger access,
  installation, or dispatch, so ambiguity cannot select a destination.

The syscall seam is private to legacy migration and covers read, stat, mkdir,
temp create/write/sync/close, rename, both directory open/sync/close sequences,
and removal. Cleanup errors remain joined to the primary error.

## Verification

All CQ-MIG-01-scoped checks passed:

```text
go test ./internal/queue -run '^TestMigrateFromLegacy_' -count=10
  PASS

go test ./internal/lifecycle \
  -run '^TestLoadQueueAtStartup_MigrationErrorFailsClosed$' -count=10
  PASS

go test -race ./internal/queue -run '^TestMigrateFromLegacy_' -count=1
  PASS

go test -race ./internal/lifecycle \
  -run '^TestLoadQueueAtStartup_MigrationErrorFailsClosed$' -count=1
  PASS

go vet ./internal/queue ./internal/lifecycle
  PASS

gofmt -d <four changed files>
  PASS (no output)

git diff --check 8478de9fa104a69af4df569d5804c5730b56ad22..8785db977
  PASS
```

`make check-fast` passed formatting, whole-repository vet/build, tagged vet,
and `golangci-lint --new-from-rev=HEAD~1` with zero issues. It then failed the
unrelated `readywait-freeze-gate` because that gate names retired
`internal/daemon` files. Every missing file reported by the gate is already
absent at the exact CQ-MIG-01 claim base, and CQ-MIG-01 changes no daemon file,
so this is a non-blocking baseline gate failure for this range.

UBS required the installed modern Bash and completed its four-file scan. Its
nonzero findings were investigated:

- the reported secret-comparison “critical” is an ordinary Bead-ID equality in
  pre-existing lifecycle test code outside the claimed hunks;
- the reported unclosed file is the pre-existing `Persist` open-file region,
  not the changed migration implementation;
- module warnings are artifacts of UBS's four-file shadow workspace.

No UBS finding is introduced by CQ-MIG-01.

## Conclusion

The bounded migration fix preserves the only valid legacy queue, refuses
ambiguous ownership, completes directory durability in the required order,
converges on retry, and prevents startup from proceeding after migration
failure. It is ready for integration under the normal coordinator gate.
