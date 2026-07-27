import { render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowRunPage } from "../WorkflowRunPage"

vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../hooks/useWorkflow",()=>({useWorkflowRun:()=>({loading:false,error:null,refresh:vi.fn(),data:{run:{id:"wr-1",workflow_title:"Empty run",status:"running",current_step_id:"prepare",inputs_json:"{}"},steps:[{run_id:"wr-1",step_id:"prepare",step_order:0,title:"Prepare",executor_type:"github.pr.prepare",resolved_inputs_json:"{}",outputs_json:"{}",status:"pending",latest_attempt:0}],attempts:null,artifacts:null}})}))

describe("WorkflowRunPage empty collections",()=>{it("renders before attempts or artifacts exist",()=>{render(<MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage/>}/></Routes></MemoryRouter>);expect(screen.getByRole("heading",{name:"Empty run"})).toBeTruthy();expect(screen.getByRole("heading",{name:"Prepare"})).toBeTruthy()})})
