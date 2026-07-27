import { render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowRunPage } from "../WorkflowRunPage"

vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../hooks/useWorkflow",()=>({useWorkflowRun:()=>({loading:false,error:null,refresh:vi.fn(),data:{run:{id:"wr-1",workflow_title:"Review PR",status:"running",current_step_id:"review",inputs_json:'{"repo":"acme/app"}'},steps:[{run_id:"wr-1",step_id:"prepare",step_order:0,title:"Prepare PR",executor_type:"github.pr.prepare",resolved_inputs_json:"{}",outputs_json:'{"head_sha":"abc"}',status:"succeeded",latest_attempt:1},{run_id:"wr-1",step_id:"review",step_order:1,title:"Review",executor_type:"agent.run",resolved_inputs_json:'{"prompt":"review"}',outputs_json:"{}",status:"waiting_agent",latest_attempt:1}],attempts:[{id:"wa-1",run_id:"wr-1",step_id:"review",attempt_number:1,executor_type:"agent.run",agent_session_id:7,task_id:"task-1",agent_kind:"chimera",working_dir:"/tmp/wt",status:"waiting_agent",started_at:"now"}],artifacts:[]}})}))

describe("WorkflowRunPage",()=>{it("shows ordered executor progress and links the live agent",()=>{render(<MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage/>}/></Routes></MemoryRouter>);expect(screen.getByRole("heading",{name:"Prepare PR"})).toBeTruthy();expect(screen.getByText("github.pr.prepare")).toBeTruthy();expect(screen.getByRole("link",{name:"View live agent"}).getAttribute("href")).toBe("/agents?task=task-1&workflow=wr-1")})})
