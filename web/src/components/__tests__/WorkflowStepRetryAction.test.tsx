import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { WorkflowStepRetryAction } from "../WorkflowStepRetryAction"

const step={run_id:"wr",step_id:"cleanup",step_order:2,title:"Cleanup",executor_type:"worktree.remove",resolved_inputs_json:'{"expected_path":"/tmp/review-worktree"}',outputs_json:"{}",status:"failed",latest_attempt:1}

describe("WorkflowStepRetryAction",()=>{it("can force a failed worktree removal retry",async()=>{const retry=vi.fn().mockResolvedValue(undefined);render(<WorkflowStepRetryAction step={step} onRetry={retry}/>);fireEvent.click(screen.getByRole("button",{name:"Retry"}));const dialog=screen.getByRole("dialog");expect(dialog.textContent).toContain("/tmp/review-worktree");fireEvent.click(screen.getByRole("checkbox",{name:/force removal/i}));fireEvent.click(screen.getByRole("button",{name:"Retry removal"}));await vi.waitFor(()=>expect(retry).toHaveBeenCalledWith(true))})})
