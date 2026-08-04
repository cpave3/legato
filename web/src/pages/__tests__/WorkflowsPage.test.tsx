import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"
const { useWorkflowRuns } = vi.hoisted(() => ({ useWorkflowRuns: vi.fn(() => ({ data: [], refresh: vi.fn() })) }))
vi.mock("../../hooks/useWorkflow",()=>({useWorkflows:()=>({data:{definitions:[{schema_version:1,name:"review-pr",title:"Review PR",description:"Review code",source:"/project/.legato/workflows/review.yaml",inputs:{repo:{type:"string",required:true}},steps:[{id:"prepare",title:"Prepare",type:"github.pr.prepare",with:{}},{id:"review",title:"Review",type:"agent.run",with:{}}]}],errors:[]},refresh:vi.fn()}),useWorkflowRuns}))
vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
describe("WorkflowsPage",()=>{it("shows reusable definitions and their executor types",()=>{render(<MemoryRouter><WorkflowsPage/></MemoryRouter>);expect(screen.getByRole("heading",{name:"Review PR"})).toBeTruthy();expect(screen.getByText("github.pr.prepare")).toBeTruthy();expect(screen.getByText("agent.run")).toBeTruthy();expect(screen.getByRole("button",{name:"Run"})).toBeTruthy();expect(useWorkflowRuns).not.toHaveBeenCalled()})})
