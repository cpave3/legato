# Workflow delivery and review documentation audit

Audited 2026-11-26 against the fresh job clone, rebased onto main `60b19e5a` before final validation. Scope: operator site, workflow schemas/catalog, built-in and bundled definitions, CLI/runtime ownership, review engine, steward and review-tour/packet boundaries. No product behavior, CI, live configuration or workflow files were changed.

## Coverage matrix

**Existing** means correct depth retained; **Fixed** means corrected or connected here; **Deferred** means evidence not obtained, not a claimed feature gap. Site paths below are relative to `site/src/content/docs/`.

| Operator concern | Before → disposition | Authority | Operator documentation |
| --- | --- | --- | --- |
| Ship / workflow / review loop / steward / tour / packet distinctions | Missing connected route and ambiguous review wording → **Fixed** | `internal/engine/workflow/builtin_review_loop.go:25`, `internal/service/review_pass.go:819`, `internal/service/review_steward.go:89`; tour and packet contracts in `internal/service/review.go`, `internal/service/handoff_types.go` | `using/delivery.md:14`; `using/reviews.md:8`; `using/review-handoff.md:14`; source glossary `GLOSSARY.md:100` |
| Definitions, discovery precedence, v1/v2, schema/editor validation, typed inputs | **Existing**, preserved; secret/choices declaration and unknown-with exception **Fixed** | `internal/engine/workflow/definition.go:62`, `schema.go:159`, `diagnostics.go:149`; published `site/public/schemas/workflow-v{1,2}.schema.json` | `using/workflows.md:14`, `:250`, `:842`, `:1020` |
| Triggers, conditions, paused copies, confirmations and loop stops | **Existing**; confirm applies to all kinds and template opt-ins **Fixed** | `internal/engine/workflow/trigger.go:41`, `internal/service/workflow_trigger_event.go:109`; six files under `internal/engine/workflow/examples/` | `using/workflows.md:29`, `:423`, `:730`; `using/delivery.md:25` |
| Composition, expression/if branches, loops and gates | **Existing**; working end-to-end example and invalid note key **Fixed** | `internal/service/workflow_call.go:132`, `workflow_control.go:111`; `internal/engine/workflow/loop.go`, `catalog.go:520`; `internal/service/workflow_task_note.go:43` | `using/workflows.md:1116`–`:1285`; `using/delivery.md:85` |
| Ownership, named slots, outputs, completion and cancellation | **Existing**; connected ownership/cleanup warnings **Fixed** | `internal/service/workflow_agent_place.go:49`, `agent_slot.go`; `internal/engine/workflow/agent_wait.go`; `internal/service/workflow_run_ended.go:99` | `using/workflows.md:91`–`:127`, `:1029`, `:1390`; `using/delivery.md:127` |
| Running, monitoring, retry, resume, cancel, suspended and stopped | **Existing**; state legend and recovery route **Fixed** | `web/src/lib/workflowView.ts:16`; `internal/service/workflow_suspend.go`, `workflow_call.go:132` | `using/workflows.md:265`, `:971`, `:1007`; `using/delivery.md:127` |
| Started-run and input snapshots; later macro/child resolution | Scattered/implicit → **Fixed** | `internal/service/workflow.go:208`–`:277`, `workflow_call.go:132`–`:174`, `workflow_loop.go:125`; `review_pass_run.go:262`–`:275` | `using/workflows.md:999`; `using/delivery.md:39` |
| Prerequisites, host-only reviewer vs author permissions, engine ownership | **Existing**; setup checklist and custom App identity repair **Fixed** | `config/review_passes.go:46`, `:65`; `internal/service/credential_profiles/github-reviewer.yaml`, `credential_profiles_app.go:10`; `cmd/legato/review_pass_runtime.go:95`, `review_engine_lock.go` | `using/review-passes.md:64`–`:97`, `:148`; `using/delivery.md:41`; linked credential setup |
| Agent-author vs person-author, outside-author withheld approval, partial approvals | **Existing**; obsolete output shape and universal-note claim → **Fixed** | `internal/service/review_loop_steps.go:707`–`:763`; `internal/engine/workflow/builtin_review_loop.go:80`–`:169`; `internal/service/review_pass_submit.go:771`–`:824` | `using/review-passes.md`, “How it ends”; `using/delivery.md:54`, `:123`; `using/workflows.md:1287` |
| One PR vs stack, each layer's base, pinned and changed heads, incremental passes/rulings | **Existing**, retained and linked into journey; per-PR-base explanation **Fixed** | `internal/service/review_pass_stack.go:56`; `review_pass_submit.go:268`–`:339`, `:406`–`:451`, `:645`; `review_loop_ops.go:227` | `using/review-passes.md`, “How a review runs”, “Tuning one loop”; `using/delivery.md:54`–`:57` |
| Blocking/nits, counted comments/checks, author replies, retry/stuck limits | **Existing**; per-approval pass windows, third-pass non-convergence and invisible skipped-comment count wording **Fixed** | `internal/service/review_pass.go:819`; `internal/engine/store/review_loops.go:1060`; `internal/service/review_pass_submit.go:621`, `:732`, `:827`; `review_publisher_checks.go:124` | `using/review-passes.md`, “What counts as blocking”, “Comments and checks”, “When a review is stuck”; `reference/configuration.md:764` |
| Optional ready/steward, merged lower layers, new top PRs, CI/conflicts/re-entry, holds/end/idle | **Existing**, retained; default table and practical outcome/recovery routes **Fixed** | `internal/engine/workflow/builtin_review_loop.go:67`–`:165`; `internal/service/review_steward.go:198`, `:344`, `:433`; `review_steward_hold.go:123`, `:147`; `review_watch.go:16` | `using/review-passes.md`, “After approval: the steward”; `using/delivery.md:25`, `:58`, `:127` |
| Ticket-to-merge opt-ins, no automatic merge, conditional ticket close/agent cleanup | **Existing** template detail, missing connected example → **Fixed** | `internal/engine/workflow/examples/ticket-to-merged-pr.workflow.yaml:32`–`:98`; `builtin_review_loop.go:67` | `using/delivery.md:61`–`:83`; `using/workflows.md:38` |
| Release/build availability and real external GitHub behavior | **Deferred verification**, not silently claimed tested | Capability/setting checks in `cmd/legato/review_pass_runtime.go`; source behavior is not release provenance or a live GitHub experiment | `using/delivery.md:10` preserves review page's release caveat and tells operators to inspect their installed binary |

## Focused validation

No full suite or browser/live-instance testing. Go invocations use `GOMAXPROCS=4`, `-p 4`, `-count=1`, a job-specific writable `/tmp` GOCACHE and `CGO_ENABLED=0` for service/harness checks. Docs build uses a 1536 MiB Node heap bound and disables Astro telemetry rather than writing to the operator's home.

### Parser, call contract and published schemas — passed

`go test ./internal/engine/workflow -run '^(TestOperatorGuideWorkflowExamplesValidate|TestEveryEmbeddedExampleValidates|TestTheBundledTemplatesArePausedAndNeedNoExtraTools|TestTheGeneratedSchemaIsCurrent|TestTheV2SchemaIsPublishedBesideV1|TestAWorkflowRunStepsOutputsAreTheChildsAndItsStatus|TestTheBuiltInReviewLoopIsAV2WorkflowOfTheReviewSteps|TestUpgradeRewritesTheOldCopyToCallReviewLoop)$' -count=1`

The new test reads the actual operator Markdown, checks the loop snippet and complete delivery recipe with `workflow.Check`, then validates child inputs/output references with `CallDiagnostics` against the real built-in. It first failed on the old `task.note.with.note` key; changing that to `body` made it pass. This is parser/call-contract evidence, not proof of agent or GitHub execution. Existing bundled-template checks do not cover these site examples, so the new table protects a separate boundary. No duplicate constant-only tests were added.

### Service behavior — passed

Five selected tests in `./internal/service`: `TestTaskNoteExecutorPersistsJiraBackedNoteLocally`, `TestATaskNoteTakesATaskIDAsText`, `TestWorkflowTaskIntrinsicReloadsForManualAndRetryExecution`, `TestRetryOfAFailedCallResumesTheSameChild`, `TestWorkflowRetryStartsSecondAttemptAndKeepsHistory`.

These exercise real temporary stores, note persistence, execution-task reloading and attempt/child retry behavior rather than restating the YAML schema.

### Review/steward lifecycle harness — passed after rebase

17 selected top-level tests in `./internal/handofftest` (8 seconds, plus subcases):

- Parent output/outcome branching: `TestReviewLoopsOutcomeApprovedReachesTheParentsIfWithTheContractsOutputs`, `TestReviewLoopsOutcomeClosedReachesTheParentsIf`, `TestReviewLoopsOutcomeStuckReachesTheParentsIfOnlyOnceTheOperatorEndsIt`.
- Retry/limits: `TestRetryOfAFailedReviewLoopChildResumesTheSameChildAndLoop`, `TestAnotherPassRaisesOnlyTheCurrentWindowByOne`.
- Ready/partial failure: `TestReadyMarksAnApprovedStacksDraftsReadyBottomUp`, `TestALayerThatWillNotBeMarkedReadyFailsTheStepIntoItsOnFailure`.
- Stack lifecycle: `TestARestackAcrossThreeLayersIsOneReEntry`, `TestAMergedLowerLayerRoutesRestackTheRestAndAllMergedIsMerged`, `TestADraftLayerAddedWhileStewardingFlipsAfterItsReEntrysConfirm`.
- Budget/hold/idle recovery: `TestTheSixthReEntryMakesTheStewardStuckAndWatchAgainAllowsExactlyOneMore`, `TestStopWatchingSendsNothingAndWatchAgainReEntersOnceForPushesWhileHeld`, `TestSevenQuietDaysEndTheStewardIdle`.
- Complete bundled caller: `TestTicketToMergedPRCallsReviewLoopWithTheRepoPRAndScopeStack`, `TestTicketToMergedPRClosesTheTicketAndStopsTheAgentOnceMerged`, `TestTicketToMergedPRNotesTheCardOnEveryOtherOutcome`, `TestARefusedReviewLoopPausesTicketToMergedPR`.

These execute workflow services/engine with isolated stores and fake GitHub, agents and ticket close; they do not prove live GitHub permissions or make a real merge. Initial compilation on the job's old base failed on two stale `ListWorkflowRunsByRelatedTask` callers; fetching/rebasing onto main `60b19e5a` picked up upstream #425. No local product workaround was added.

A second focused harness run passed seven output/safety tests (2.7 seconds): `TestTheOpenStepOpensALoopAndTheCloseStepClosesItWithTheContractsOutputs`, `TestCloseOutputsCountAKeptApprovalAndReadTheDraftState`, `TestAStackPassIsNotPostedWhenAnyLayersHeadMovesAfterSubmit`, `TestAfterARebaseANewBlockingFindingOnAnUpstreamLineIsRefusedUnlessCritical`, `TestAnOutsidersCleanRunSaysItsApprovalWasWithheld`, `TestARunIsNotApprovedWhileItsSettledChecksApproveIsNotPosted`, `TestAPersonDismissingTheApprovalWithoutAPushNeedsYouAndGetsNoReApproval`. These verify actual close-output objects, partial approval, changed heads, incremental rebase boundaries, outsider withholding, late approvals and operator dismissal, beyond parser validation.

### Site and hygiene — passed

`ASTRO_TELEMETRY_DISABLED=1 NODE_OPTIONS=--max-old-space-size=1536 pnpm -C site build`: 63 pages, all internal links/anchors valid. The first build caught the new composition link's incomplete anchor; corrected to `#calling-a-workflow-workflowrun`. `git diff --check` passes. No route, hotkey or schema generation was needed; schema drift checks confirm the existing published files are current.

## Remaining limits

- Full verification belongs to CI. No full Go/web/package suites, CI edits, server restarts, live workflow/config changes or private operator data were used.
- Release-history provenance and actual App installations/permissions were not tested. The guide keeps the existing release caution instead of assigning a guessed minimum version.
- Only public bundled templates define universal defaults here. Operator-local workflows (including a locally steward-enabled one-off-task) were not inspected or copied into universal guidance.
- No separate Ship product surface, auto-merge, sign-off implementation or automatic tour-to-review-loop connection is claimed. Historical analysis docs are not promoted to operator contracts.
