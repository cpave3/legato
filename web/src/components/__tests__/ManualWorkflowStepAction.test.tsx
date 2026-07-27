import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { ManualWorkflowStepAction } from "../ManualWorkflowStepAction"

const step={run_id:"wr",step_id:"cleanup",step_order:2,title:"Cleanup",executor_type:"worktree.remove",resolved_inputs_json:'{"expected_path":"/tmp/review-worktree"}',outputs_json:"{}",status:"waiting_manual",latest_attempt:0}

describe("ManualWorkflowStepAction",()=>{it("confirms cleanup and explains artifact preservation",async()=>{const trigger=vi.fn().mockResolvedValue(undefined);render(<ManualWorkflowStepAction step={step} onTrigger={trigger}/>);fireEvent.click(screen.getByRole("button",{name:"Remove worktree"}));expect(screen.getByRole("dialog")).toBeTruthy();const dialog=screen.getByRole("dialog");expect(dialog.textContent).toContain("/tmp/review-worktree");expect(dialog.textContent).toMatch(/artifacts.*remain available/i);fireEvent.click(screen.getAllByRole("button",{name:"Remove worktree"})[1]);await vi.waitFor(()=>expect(trigger).toHaveBeenCalledOnce())})})
