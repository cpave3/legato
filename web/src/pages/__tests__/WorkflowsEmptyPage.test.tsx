import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

vi.mock("../../hooks/useWorkflow",()=>({useWorkflows:()=>({data:{definitions:null,errors:null},refresh:vi.fn()}),useWorkflowRuns:()=>({data:null,refresh:vi.fn()})}))
vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))

describe("WorkflowsPage empty state",()=>{it("renders an empty discovery response without crashing",()=>{render(<MemoryRouter><WorkflowsPage/></MemoryRouter>);expect(screen.getByRole("heading",{name:"Workflows"})).toBeTruthy();expect(screen.getByText("No workflow definitions found")).toBeTruthy()})})
