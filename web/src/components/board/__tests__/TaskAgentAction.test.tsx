import {fireEvent,render,screen} from "@testing-library/react"
import {MemoryRouter} from "react-router-dom"
import {describe,expect,it,vi} from "vitest"
import {TaskAgentAction} from "../TaskAgentAction"
const {apiFetch,select}=vi.hoisted(()=>({apiFetch:vi.fn(),select:vi.fn()}))
vi.mock("../../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../../hooks/useAgentSelection",()=>({useAgentSelection:()=>({setSelectedAgentId:select})}))
vi.mock("../../../lib/api",()=>({apiFetch}))
describe("TaskAgentAction",()=>{it("creates or resumes the task agent and selects it",async()=>{apiFetch.mockImplementation((_:string,path:string)=>Promise.resolve(path==="/api/adapters"?new Response(JSON.stringify({adapters:["chimera"]}),{status:200}):path==="/api/settings"?new Response(JSON.stringify({working_dir:"/repo"}),{status:200}):new Response(JSON.stringify({task_id:"T-1",created:true}),{status:201})));render(<MemoryRouter><TaskAgentAction taskId="T-1"/></MemoryRouter>);fireEvent.click(screen.getByRole("button",{name:"Open agent"}));await screen.findByRole("option",{name:"chimera"});fireEvent.change(screen.getByLabelText("Agent"),{target:{value:"chimera"}});fireEvent.click(screen.getAllByRole("button",{name:"Open agent"})[1]);await vi.waitFor(()=>expect(select).toHaveBeenCalledWith("T-1"));expect(apiFetch).toHaveBeenCalledWith("","/api/tasks/T-1/agent",expect.objectContaining({method:"POST",body:JSON.stringify({agent_kind:"chimera",working_dir:"/repo"})}))})})
