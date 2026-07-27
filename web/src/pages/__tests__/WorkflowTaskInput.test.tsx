import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

vi.mock("../../hooks/useWorkflow",()=>({useWorkflows:()=>({data:{definitions:[{schema_version:1,name:"triage-task",title:"Triage task",source:"global",inputs:{task:{type:"task",required:true}},steps:[{id:"triage",title:"Triage",type:"agent.run",with:{}}]}],errors:[]},refresh:vi.fn()}),useWorkflowRuns:()=>({data:[],refresh:vi.fn()})}))
vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../hooks/useBoard",()=>({useBoard:()=>({columns:[{name:"Backlog",cards:[{id:"LEG-42",title:"Investigate timeout",provider:"jira"}]}]})}))

describe("workflow task input",()=>{it("selects an existing board task",()=>{render(<MemoryRouter><WorkflowsPage/></MemoryRouter>);fireEvent.click(screen.getByRole("button",{name:"Run"}));const select=screen.getByRole("combobox",{name:/task/i}) as HTMLSelectElement;expect(screen.getByRole("option",{name:/LEG-42.*Investigate timeout/})).toBeTruthy();fireEvent.change(select,{target:{value:"LEG-42"}});expect(select.value).toBe("LEG-42")})})
