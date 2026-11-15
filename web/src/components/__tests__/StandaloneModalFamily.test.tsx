import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { ManualWorkflowStepAction } from "../ManualWorkflowStepAction"
import { WorkflowStepRetryAction } from "../WorkflowStepRetryAction"
import { RunWorkflowDialog } from "../RunWorkflowDialog"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
const step = {run_id:"wr",step_id:"cleanup",step_order:2,title:"Cleanup",executor_type:"worktree.remove",resolved_inputs_json:'{"expected_path":"/tmp/worktree"}',outputs_json:"{}",status:"waiting_manual",latest_attempt:0}

it.each([false, true])("worktree confirmation protects focus and restores its trigger (retry=%s)", retry => {
  render(retry ? <WorkflowStepRetryAction step={step} onRetry={vi.fn()} /> : <ManualWorkflowStepAction step={step} onTrigger={vi.fn()} />)
  const trigger = screen.getByRole("button")
  trigger.focus()
  fireEvent.click(trigger)
  const dialog = screen.getByRole("dialog", {name: retry ? "Retry worktree removal?" : "Remove worktree?"})
  expect(dialog.contains(document.activeElement)).toBe(true)
  const buttons = within(dialog).getAllByRole("button")
  buttons.at(-1)!.focus()
  fireEvent.keyDown(document.activeElement!, {key:"Tab"})
  expect(within(dialog).getByRole("checkbox")).toHaveFocus()
  fireEvent.keyDown(document.activeElement!, {key:"Escape"})
  expect(screen.queryByRole("dialog")).toBeNull()
  expect(trigger).toHaveFocus()
})

it("workflow launch traps focus and dismisses with Escape", () => {
  const close = vi.fn()
  render(<MemoryRouter><RunWorkflowDialog definition={{schema_version:1,name:"triage",title:"Triage",source:"triage.yaml",inputs:{},steps:[]}} repositories={[]} onClose={close} /></MemoryRouter>)
  const dialog = screen.getByRole("dialog", {name:"Run Triage"})
  within(dialog).getByRole("button", {name:"Start"}).focus()
  fireEvent.keyDown(document.activeElement!, {key:"Tab"})
  expect(within(dialog).getByRole("button", {name:"Cancel"})).toHaveFocus()
  fireEvent.keyDown(document.activeElement!, {key:"Escape"})
  expect(close).toHaveBeenCalledTimes(1)
})

import { WorkflowTriggerInputModal } from "../WorkflowTriggerInputModal"
it("names the trigger request and lets Escape leave it pending", () => {
  const close = vi.fn()
  render(<WorkflowTriggerInputModal baseUrl="" request={{id:"wtr-1",workflow_name:"triage",status:"waiting_input",missing_inputs_json:"[]"}} repositories={[]} onClose={close} onStarted={vi.fn()} />)
  const dialog = screen.getByRole("dialog", {name:"Repository required"})
  expect(dialog.contains(document.activeElement)).toBe(true)
  fireEvent.keyDown(document.activeElement!, {key:"Escape"})
  expect(close).toHaveBeenCalledTimes(1)
})
