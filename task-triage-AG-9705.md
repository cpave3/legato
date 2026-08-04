# Triage: Settlement confirmation upload fails when marking a contract as settled

**Task ID:** `AG-9705`

**Priority:** High

**Disposition:** Ready for implementation

**Estimated complexity:** Medium — the write-path fix is targeted, but production data remediation and end-to-end failure handling span storage, API, and UI behavior.

## User outcome

A user can upload one or more settlement confirmations and complete **Mark as Sold** for PEXA or In Person settlements, including on contracts created before `ContractDocuments` was persisted. Successful submission marks the contract settled and runs the standard settlement tasks and automations. Upload or submission failures remain recoverable and are shown to the user instead of leaving the modal apparently unresponsive.

## Current behavior

- The settlement modal uploads a confirmation through `AddOrUpdateDocument` before the contract can complete settlement.
- A PEXA confirmation is assigned at the Cosmos path:

  ```text
  /ContractDetails/{contractId}/ContractDocuments/PexaSettlementConfirmation
  ```

- Some older campaign documents do not contain the persisted `ContractDocuments` parent object.
- `ContractDetail.ContractDocuments` is default-initialized in C#. After deserialization, application code therefore sees a non-null empty object even when the property is absent from the raw Cosmos JSON.
- `TryAssignContractDocument` uses that in-memory non-null value as evidence that the persisted parent exists and emits a nested patch.
- Cosmos patch operations cannot create a missing intermediate parent. The patch is rejected with `400 BadRequest`, which propagates through `CampaignsService.ApplyCampaignPatch` and surfaces from `AddOrUpdateDocument` as a 500 response.
- The failed upload prevents **Mark as Sold** from completing. The contract remains pending settlement confirmation, and the modal does not present an actionable error.

## Root cause

The document-assignment path conflates two different states:

1. `ContractDocuments` exists in the persisted campaign JSON; and
2. `ContractDocuments` exists only because the C# model initializes it during deserialization.

Only the first state permits a nested Cosmos patch. For older records in the second state, the code must create/set the complete `ContractDocuments` object at its parent path rather than patching `PexaSettlementConfirmation` beneath a parent that Cosmos does not have.

The existing `ContractDocuments_20260519` migration did not normalize every older contract. It skipped contracts that had neither a legacy `SalesAdvice` nor a legacy `ExchangeLetter`, so valid contracts can still lack the persisted parent.

## Recent-change relationship

- Commit `00dca26369` (`feat(settlement): support PEXA settlement confirmations`), authored 14 July 2026 and committed 17 July 2026, routed `PexaSettlementConfirmation` through the existing nested document-patch path and exposed the failure in the settlement workflow.
- The underlying persistence mismatch dates to commit `36104eae71` from 20 May 2026, which introduced the model/migration behavior that allowed persisted records without `ContractDocuments` to deserialize as though the parent existed.

This is a regression in the PEXA settlement workflow, with a pre-existing data-shape vulnerability shared by document assignment.

## Implementation scope

### 1. Make contract-document patching aware of persisted parent existence

Update the contract document assignment/write path so it does not infer Cosmos path existence from the default-initialized C# property.

- Detect whether `ContractDocuments` exists in the stored campaign document before choosing a patch shape.
- When the parent exists, retain the targeted nested patch for the requested document field.
- When the parent is absent, set the complete `ContractDocuments` object at:

  ```text
  /ContractDetails/{contractId}/ContractDocuments
  ```

  Include the newly uploaded confirmation and preserve any document values represented by the source contract model.
- Apply this rule centrally in the shared assignment path rather than adding a PEXA-only special case. Any supported contract document must be safe when used against an older record.
- Do not use `ContractDetail.ContractDocuments != null` as the persisted-path check while the property is default-initialized.

The implementation should preserve the current atomicity expectations of the campaign patch and must not overwrite unrelated contract details.

### 2. Add regression coverage for the legacy persisted shape

Create a fixture that represents the actual failing shape: raw campaign JSON containing the target contract but no `ContractDocuments` property.

Exercise the same public application path used by `AddOrUpdateDocument` and assert that:

- assigning `PexaSettlementConfirmation` succeeds;
- the generated/applied patch creates the parent `ContractDocuments` object rather than targeting a child below an absent parent;
- the confirmation is retained after the updated campaign is read back;
- unrelated contract details and documents are unchanged;
- a campaign where `ContractDocuments` already exists continues to use safe update behavior;
- other supported contract-document types remain functional against both persisted shapes.

A test that constructs only the default-initialized C# model is insufficient: it must begin from serialized JSON with the parent property absent, otherwise it cannot reproduce this defect.

### 3. Surface upload and settlement failures in the modal

The settlement flow must not silently stop when confirmation upload or final submission fails.

- Handle a rejected `AddOrUpdateDocument` request in the Mark as Settled modal and show the user an actionable error.
- Keep the modal open with the entered settlement data recoverable so the user can retry.
- Prevent the final settlement action from presenting success or continuing with missing required confirmations after upload failure.
- Handle final **Mark as Sold** submission failure with equivalent visible feedback.
- Preserve existing loading/disabled-state behavior and prevent duplicate submissions while a request is in flight.

Use the application's standard error presentation and logging conventions; do not expose raw Cosmos or server exception details to the user.

### 4. Remediate affected persisted records

Treat the write-path fix as required even if a backfill is performed, because unmigrated or newly imported legacy data may still reach this path.

- Query for contracts whose raw persisted JSON lacks `ContractDocuments`, including contracts with neither legacy `SalesAdvice` nor `ExchangeLetter`.
- Backfill an empty `ContractDocuments` object, preserving all existing campaign and contract data.
- Make the remediation idempotent and record counts for scanned, updated, skipped, and failed records.
- Validate the production campaign and contract linked in `AG-9705` before and after remediation.

Coordinate the backfill through the normal production migration/release process; do not make request success depend solely on the backfill having run.

## Acceptance criteria

1. On a contract whose raw Cosmos JSON has no `ContractDocuments` property, uploading a PEXA settlement confirmation succeeds without a Cosmos `BadRequest` or API 500.
2. The fallback write creates the complete `ContractDocuments` parent and persists `PexaSettlementConfirmation` without changing unrelated contract data.
3. Uploading or replacing a confirmation on a contract where `ContractDocuments` already exists continues to work.
4. The shared document-assignment path safely handles every supported contract-document type when the parent is absent; the fix is not limited to PEXA.
5. After successful confirmation upload, **Mark as Sold** closes the modal, updates the contract to Settled, and triggers the existing standard settlement tasks and automations exactly once.
6. If confirmation upload fails, the modal remains open, does not mark the contract settled, retains recoverable user input, and displays an actionable error.
7. If final settlement submission fails, the modal displays an actionable error and does not falsely indicate completion.
8. Duplicate clicks while upload or settlement submission is in flight do not create duplicate documents, settlement transitions, tasks, or automations.
9. The remediation identifies and safely backfills records lacking the persisted parent, including records with neither legacy `SalesAdvice` nor `ExchangeLetter`, and is safe to rerun.
10. Existing settlement flows and contract document uploads for current records remain unchanged.

## Test plan

### Backend and persistence

- Add a serialization-based regression test using legacy campaign JSON without `ContractDocuments`.
- Exercise the public `AddOrUpdateDocument` handler/service path and the real patch-building behavior, not only a helper in isolation.
- Assert the absent-parent and existing-parent patch cases and read-back persistence.
- Cover PEXA confirmation plus representative existing document types to establish that the shared path is fixed.
- Assert unrelated contract/campaign fields survive the update.
- Assert storage errors continue to propagate as a controlled API error rather than being swallowed.

### Settlement UI

- Exercise the Mark as Settled modal with PEXA and In Person settlement types.
- Verify successful confirmation upload followed by **Mark as Sold** reaches the settled state.
- Mock/reproduce an upload rejection and assert visible error text, retained modal/input state, and no final settlement request.
- Mock/reproduce final submission rejection and assert visible error text and no false success state.
- Verify in-flight duplicate-submit protection.

### Workflow integration

- Run an integration test through the settlement entry point and assert the contract transition plus standard settlement tasks/automations.
- Verify those side effects occur once and only after required confirmation upload succeeds.
- Run the backfill against representative pre-migration, already-migrated, and partially populated fixtures; assert idempotency and preservation.

### Production verification

Before release, inspect the raw JSON for campaign `ad51bd12-cff8-447e-ac11-b6d68cb1d345`, contract `d553f728-d6c9-4639-94fd-536659fbfaf5`, and confirm whether `ContractDocuments` is absent. After deployment/remediation, retry the reported flow and verify upload, settled status, tasks, automations, and user-visible failure behavior.

## Risks and boundaries

- **Data-loss risk:** setting the whole parent object can overwrite existing values if the source model is stale. Use the fallback only when the persisted parent is absent, and preserve all represented values.
- **Concurrency risk:** a read-then-patch decision can race with another document update. Retain existing optimistic concurrency/ETag behavior or make parent creation atomic under the repository's established Cosmos update pattern.
- **Backfill is not the fix:** remediation lowers exposure but cannot replace a write path that safely handles legacy/imported records.
- **Side-effect ordering matters:** settlement tasks and automations must not run before required uploads succeed and must not run twice on retry.
- **User feedback is required but secondary to correctness:** translating the backend failure into a visible modal error must accompany, not mask, the persistence fix.
- **Evidence boundary:** the assigned checkout is the Legato orchestration repository and does not contain the application symbols, migrations, or commits cited above. The causal chain and commit relationship come from the supplied diagnosis and should be confirmed in the owning application repository; production raw JSON inspection was not available from this checkout.
