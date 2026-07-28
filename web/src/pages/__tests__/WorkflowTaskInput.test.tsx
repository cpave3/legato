import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

const fetchArchivedTasks=vi.fn().mockResolvedValue([{id:"LEG-44",title:"Archived task",status:"Done",workspace_name:"Platform"}])
vi.mock("../../hooks/useWorkflow",()=>({useWorkflows:()=>({data:{definitions:[{schema_version:1,name:"triage-task",title:"Triage task",source:"global",inputs:{task:{type:"task",required:true}},steps:[{id:"triage",title:"Triage",type:"agent.run",with:{}}]}],errors:[]},refresh:vi.fn()}),useWorkflowRuns:()=>({data:[],refresh:vi.fn()})}))
vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../hooks/useBoard",()=>({useBoard:()=>({columns:[{name:"Backlog",cards:[{id:"LEG-42",title:"Investigate timeout",status:"Backlog",workspace_name:"Platform"},{id:"LEG-45",title:"Unassigned task",status:"Backlog",workspace_name:""}]},{name:"Done",cards:[{id:"LEG-43",title:"Already fixed",status:"Done",workspace_name:"Platform"}]}]})}))
vi.mock("../../lib/board",()=>({fetchArchivedTasks:(...args:unknown[])=>fetchArchivedTasks(...args)}))

function openPicker(){cleanup();render(<MemoryRouter><WorkflowsPage/></MemoryRouter>);fireEvent.click(screen.getByRole("button",{name:"Run"}));fireEvent.click(screen.getByRole("combobox",{name:/select a task/i}))}

describe("workflow task input",()=>{
  it("groups active tasks by workspace and hides done and archived tasks by default",()=>{fetchArchivedTasks.mockClear();openPicker();const list=screen.getByRole("listbox",{name:/task/i});const platform=within(list).getByRole("group",{name:"Platform"});const unassigned=within(list).getByRole("group",{name:"Unassigned"});expect(within(platform).getByRole("option",{name:/LEG-42.*Investigate timeout/})).toBeTruthy();expect(within(unassigned).getByRole("option",{name:/LEG-45.*Unassigned task/})).toBeTruthy();expect(within(list).queryByRole("option",{name:/LEG-43|LEG-44/})).toBeNull();expect(fetchArchivedTasks).not.toHaveBeenCalled()})
  it("shows done and archived tasks on request",async()=>{fetchArchivedTasks.mockClear();openPicker();fireEvent.click(screen.getByRole("checkbox",{name:/show done and archived tasks/i}));const list=screen.getByRole("listbox",{name:/task/i});expect(within(within(list).getByRole("group",{name:"Platform"})).getByRole("option",{name:/LEG-43.*Already fixed/})).toBeTruthy();await waitFor(()=>expect(within(within(list).getByRole("group",{name:"Platform"})).getByRole("option",{name:/LEG-44.*Archived task/})).toBeTruthy());expect(fetchArchivedTasks).toHaveBeenCalledWith("")})
})
