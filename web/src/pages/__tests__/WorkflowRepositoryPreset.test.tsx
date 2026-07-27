import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

vi.mock("../../hooks/useWorkflow",()=>({useWorkflows:()=>({data:{definitions:[{schema_version:1,name:"review",title:"Review",source:"test",inputs:{repo:{type:"string"},primary_dir:{type:"string"}},steps:[{id:"run",title:"Run",type:"agent.run",with:{}}]}],errors:[],repositories:[{name:"Legato",repo:"cpave3/legato",primary_dir:"/projects/legato"}]},refresh:vi.fn()}),useWorkflowRuns:()=>({data:[],refresh:vi.fn()})}))
vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))

describe("workflow repository presets",()=>{it("prefills editable workflow inputs",()=>{render(<MemoryRouter><WorkflowsPage/></MemoryRouter>);fireEvent.click(screen.getByRole("button",{name:"Run"}));fireEvent.change(screen.getByRole("combobox",{name:/Repository preset/}),{target:{value:"Legato"}});const repo=screen.getByLabelText("repo") as HTMLInputElement;expect(repo.value).toBe("cpave3/legato");fireEvent.change(repo,{target:{value:"other/repo"}});expect(repo.value).toBe("other/repo")})})
