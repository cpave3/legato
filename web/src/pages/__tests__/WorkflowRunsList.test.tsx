import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

vi.mock("../../hooks/useWorkflow",()=>({useWorkflows:()=>({data:{definitions:[],errors:[]},refresh:vi.fn()}),useWorkflowRuns:()=>({data:[{id:"wr-1",workflow_name:"review-pr",workflow_title:"Review PR",definition_source:"test",definition_digest:"digest",inputs_json:"{}",status:"running",current_step_id:"review",needs_attention:true,attention_reason:"agent waiting for input",created_at:"2026-07-28T10:00:00Z"},{id:"wr-2",workflow_name:"cleanup",workflow_title:"Cleanup",definition_source:"test",definition_digest:"digest",inputs_json:"{}",status:"succeeded",current_step_id:"",created_at:"2026-07-28T09:00:00Z"}],refresh:vi.fn()})}))
vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))

describe("workflow runs list",()=>{it("renders compact navigable rows with run details",()=>{render(<MemoryRouter><WorkflowsPage/></MemoryRouter>);fireEvent.click(screen.getByRole("button",{name:"Runs"}));const list=screen.getByRole("list",{name:"Workflow runs"});const rows=within(list).getAllByRole("listitem");expect(rows).toHaveLength(2);expect(rows[0].textContent).toContain("Review PR");expect(rows[0].textContent).toContain("review");expect(rows[0].textContent).toContain("agent waiting for input");expect(within(rows[0]).getByRole("link").getAttribute("href")).toBe("/workflows/runs/wr-1");expect(rows[1].textContent).toContain("Complete")})})
