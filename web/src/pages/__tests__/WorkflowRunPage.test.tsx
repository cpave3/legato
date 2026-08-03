import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { WorkflowRunPage } from "../WorkflowRunPage"

const { fetchArtifact, runData } = vi.hoisted(() => ({
  fetchArtifact: vi.fn(),
  runData: {
    run: {id:"wr-1",workflow_title:"Review PR",status:"running",current_step_id:"review",inputs_json:'{"repo":"acme/app"}'},
    steps: [
      {run_id:"wr-1",step_id:"prepare",step_order:0,title:"Prepare PR",executor_type:"github.pr.prepare",resolved_inputs_json:"{}",outputs_json:'{"head_sha":"abc"}',status:"succeeded",latest_attempt:1},
      {run_id:"wr-1",step_id:"review",step_order:1,title:"Review",executor_type:"agent.run",resolved_inputs_json:'{"prompt":"review"}',outputs_json:"{}",status:"waiting_agent",latest_attempt:1},
    ],
    attempts: [{id:"wa-1",run_id:"wr-1",step_id:"review",attempt_number:1,executor_type:"agent.run",agent_session_id:7,task_id:"task-1",agent_kind:"chimera",working_dir:"/tmp/wt",status:"waiting_agent",needs_attention:true,attention_reason:"agent idle before step completion",started_at:"now"}],
    artifacts: [{id:"wfa-1",run_id:"wr-1",step_id:"review",attempt_id:"wa-1",output_key:"triage",original_path:"reports/task-triage.md",media_type:"text/markdown",size_bytes:24,digest:"sha256:x",created_at:"now"}],
  },
}))

vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../hooks/useWorkflow",()=>({useWorkflowRun:()=>({loading:false,error:null,refresh:vi.fn(),data:runData})}))
vi.mock("../../lib/workflow", async importOriginal => ({...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowArtifact:fetchArtifact}))

function renderPage(){render(<MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage/>}/></Routes></MemoryRouter>)}

describe("WorkflowRunPage",()=>{
  beforeEach(()=>{cleanup();fetchArtifact.mockReset();fetchArtifact.mockResolvedValue(new Blob(["# Triage\n\nReady."]));vi.stubGlobal("URL",{...URL,createObjectURL:vi.fn(()=>"blob:test"),revokeObjectURL:vi.fn()})})

  it("shows ordered executor progress and links the live agent",()=>{renderPage();expect(screen.getByRole("heading",{name:"Prepare PR"})).toBeTruthy();expect(screen.getByText("github.pr.prepare")).toBeTruthy();expect(screen.getByRole("link",{name:"View live agent"}).getAttribute("href")).toBe("/agents?task=task-1&workflow=wr-1");expect(screen.getByRole("button",{name:"Prompt to continue"})).toBeTruthy()})

  it("previews Markdown and downloads it with the original basename",async()=>{
    let downloadedName=""
    vi.spyOn(HTMLAnchorElement.prototype,"click").mockImplementation(function(this:HTMLAnchorElement){downloadedName=this.download})
    renderPage()
    expect(screen.getByText("task-triage.md")).toBeTruthy()
    fireEvent.click(screen.getByText("Preview Markdown"))
    expect(await screen.findByRole("heading",{name:"Triage"})).toBeTruthy()
    fireEvent.click(screen.getByRole("button",{name:"Download"}))
    await waitFor(()=>expect(downloadedName).toBe("task-triage.md"))
  })
})
