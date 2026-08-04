# Read-only stale-ledger triage

Date: 2026-08-02

The current machine ledger does not contain `hk-v4wer`, `hk-o4sgg`,
`hk-fmere`, or `hk-b4xf2`. Each `br show` request returned `ISSUE_NOT_FOUND`.
No closure or other mutation was made.

Current source retains named regression tests for all four findings in
`internal/daemon`. The Step 7 plan still describes their old failures. This is
evidence that the source changed after the historical finding records, but it
is not authority to close a record that is absent from this machine-local
ledger.

Any future closure must run in the ledger that contains the original records,
then verify the named test and current production path again. No new defect was
filed because this pass found no current source failure.
