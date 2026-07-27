import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

const {startWorkflow}=vi.hoisted(()=>({startWorkflow:vi.fn().mockResolvedValue({run:{id:"wr-1"}})}))
vi.mock("../../lib/workflow",async importOriginal=>({...await importOriginal<typeof import("../../lib/workflow")>(),startWorkflow}))
vi.mock("../../hooks/useWorkflow",()=>({useWorkflows:()=>({data:{definitions:[{schema_version:1,name:"triage-task",title:"Triage task",source:"test",inputs:{task:{type:"task",required:true}},steps:[{id:"triage",title:"Triage",type:"agent.run",with:{}}]}],errors:[],repositories:[{name:"Legato",repo:"cpave3/legato",primary_dir:"/projects/legato",base_branch:"main"}]},refresh:vi.fn()}),useWorkflowRuns:()=>({data:[],refresh:vi.fn()})}))
vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../hooks/useBoard",()=>({useBoard:()=>({columns:[{name:"Backlog",cards:[{id:"LEG-42",title:"Investigate timeout"}]}]})}))

describe("workflow repository presets",()=>{it("submits only inputs declared by the workflow",async()=>{render(<MemoryRouter><WorkflowsPage/></MemoryRouter>);fireEvent.click(screen.getByRole("button",{name:"Run"}));expect(screen.queryByRole("combobox",{name:/Repository preset/})).toBeNull();fireEvent.change(screen.getByRole("combobox",{name:/task/i}),{target:{value:"LEG-42"}});fireEvent.click(screen.getByRole("button",{name:"Start"}));await vi.waitFor(()=>expect(startWorkflow).toHaveBeenCalledWith("","triage-task",{task:"LEG-42"}))})})
