# CQ-02 digest amendment — independent review

- Date: 2026-07-27
- Reviewer: independent Sol/xhigh architecture review
- Baseline: `7524073e38450da5a4444cf4fd02ace040988395`
- Verdict: **APPROVE**

## Scope

This review covers the uncommitted digest amendment in:

- `specs/queue-model.md` QM-007
- `.kerf/works/queue-transaction-contract/04-design/queue-model-design.md`
  §§4 and 14
- `.kerf/works/queue-transaction-contract/05-spec-drafts/queue-model.md`
  QM-007
- `plans/2026-07-24-code-health-audit/tasks/evidence/CQ-02.yaml`
  `replace_intent.cancellation_handoff`

## Problem

The prior schema required the predecessor replace intent to bind the exact
successor archive-intent bytes and their SHA-256 digest, while the successor
contained `predecessor_intent_sha256`. Interpreting that field as the digest of
the final canonical predecessor produced the equations:

```text
P = encode(predecessor facts, S, SHA256(S))
S = encode(successor facts, SHA256(P))
```

There is no deterministic serialization order for those records. Producing
both requires a coupled SHA-256 fixed point; iterative remarshal/re-hash is not
a recovery or construction protocol.

## Decision and consistency

The amendment completely implements the simpler resolution:

- successor v1 no longer contains `predecessor_intent_sha256`;
- its exact field set is now `schema_version`, `archive_intent_id`,
  `predecessor_transaction_id`, `archive_origin`, `archive_kind`,
  `normalized_name`, `source_identity`, `source_sha256`, and
  `destination_basename`;
- the predecessor remains authoritative for the exact successor ID, canonical
  bytes, and SHA-256 digest;
- both record IDs are preallocated before canonical serialization; and
- no final-predecessor digest participates in the successor digest domain.

The normative spec and Kerf spec draft are byte-identical. The design and
`CQ-02.yaml` use the same nine-field successor schema and the same allocation,
pair-validation, and successor-only recovery rules. No reviewed artifact
retains the removed field or implies a predecessor-record digest.

The exact field enumeration makes the v1 schema unambiguous. No projection
record, placeholder hash, null substitute, iterative hash, or additional
canonicalization scheme is introduced.

## Invariant proof

The one-way exact binding preserves the required crash/restart states:

1. **Predecessor only.** The predecessor contains the one permitted successor
   ID, exact canonical bytes, and digest. Recovery can validate the embedded
   complete successor, both IDs, and all duplicated handoff facts before
   installing only those bytes.
2. **Predecessor plus successor.** Continuation requires byte equality with the
   predecessor-bound successor, SHA-256 equality, successor-ID equality,
   `predecessor_transaction_id` equality, and equality of every duplicated
   origin, kind, name, source, and destination fact. An unrelated, stale,
   altered, or third successor cannot satisfy the classifier.
3. **Successor only.** A predecessor digest would be unverifiable after the
   predecessor's durable deletion and therefore would distinguish no
   additional non-adversarial restart state. The amended contract instead
   requires the complete successor schema and exact normalized path, source
   identity/digest, and selected destination namespace facts. Schema version
   alone is not sufficient.
4. **Mismatch or originless state.** Missing bindings, changed or inconsistent
   predecessor facts, mismatched pairs, third destinations, and originless
   cancelled canonicals remain preserve-and-refuse states.

Predecessor deletion remains ordered after exact successor installation and
queue-parent durability. Archive and applicable legacy durability still
precede successor removal. Thus neither a crash cut nor a stale record can
cause recovery to invent an origin, destination, or replacement-successor
association.

## API consequences

The implementation must preallocate the predecessor transaction ID and
successor archive-intent ID before serializing either record. A transaction
preparation API therefore cannot require callers to submit fully serialized
successor bytes before the predecessor transaction ID exists.

The generic transaction substrate should either accept a stable preallocated
transaction ID or, preferably, accept semantic archive-handoff inputs and have
one preparation routine allocate both IDs, construct the exact successor
bytes, hash them, and construct the final predecessor. Exact successor
installation must consume the predecessor-bound bytes rather than a
caller-remarshaled mutable value.

The archive-intent type and classifier must remove
`predecessor_intent_sha256`. Exact-pair classification must perform the full
byte/digest/ID/fact checks above, while successor-only recovery must receive
enough namespace context to validate the exact path, source, and destination
facts. No predecessor-link projection or general canonical JSON subsystem is
needed.

## Commands run

```text
git status --short
git diff --check -- specs/queue-model.md \
  .kerf/works/queue-transaction-contract/04-design/queue-model-design.md \
  .kerf/works/queue-transaction-contract/05-spec-drafts/queue-model.md \
  plans/2026-07-24-code-health-audit/tasks/evidence/CQ-02.yaml
git diff --stat -- specs/queue-model.md \
  .kerf/works/queue-transaction-contract/04-design/queue-model-design.md \
  .kerf/works/queue-transaction-contract/05-spec-drafts/queue-model.md \
  plans/2026-07-24-code-health-audit/tasks/evidence/CQ-02.yaml
git diff -- specs/queue-model.md \
  .kerf/works/queue-transaction-contract/04-design/queue-model-design.md \
  .kerf/works/queue-transaction-contract/05-spec-drafts/queue-model.md \
  plans/2026-07-24-code-health-audit/tasks/evidence/CQ-02.yaml
rg -n \
  'predecessor_intent_sha256|predecessor.*digest|digest of the final predecessor|successor_v1_fields|successor digest domain|successor_digest_domain|linked_pair_validation|preallocated|preallocate' \
  specs/queue-model.md \
  .kerf/works/queue-transaction-contract/04-design/queue-model-design.md \
  .kerf/works/queue-transaction-contract/05-spec-drafts/queue-model.md \
  plans/2026-07-24-code-health-audit/tasks/evidence/CQ-02.yaml
cmp -s specs/queue-model.md \
  .kerf/works/queue-transaction-contract/05-spec-drafts/queue-model.md
ruby -e 'require "yaml"; doc = YAML.load_file(ARGV.fetch(0)); handoff = doc.fetch("replace_intent").fetch("cancellation_handoff"); expected = %w[schema_version archive_intent_id predecessor_transaction_id archive_origin archive_kind normalized_name source_identity source_sha256 destination_basename]; abort unless handoff.fetch("successor_v1_fields") == expected' \
  plans/2026-07-24-code-health-audit/tasks/evidence/CQ-02.yaml
rg -n 'predecessor_intent_sha256' \
  specs/queue-model.md \
  .kerf/works/queue-transaction-contract/04-design/queue-model-design.md \
  .kerf/works/queue-transaction-contract/05-spec-drafts/queue-model.md \
  plans/2026-07-24-code-health-audit/tasks/evidence/CQ-02.yaml
git rev-parse HEAD
```

Results:

- YAML parsed successfully.
- `successor_v1_fields` exactly matched the nine-field amended schema.
- Normative spec and Kerf draft were byte-identical.
- Removed-field scan found no occurrence.
- `git diff --check` passed.
- The only pre-review worktree modifications were the four scoped amendment
  artifacts.
