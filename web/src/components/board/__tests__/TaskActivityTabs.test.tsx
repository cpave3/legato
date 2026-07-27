import {fireEvent,render,screen} from "@testing-library/react"
import {MemoryRouter} from "react-router-dom"
import {describe,expect,it,vi} from "vitest"
import {TaskActivityTabs} from "../TaskActivityTabs"
vi.mock("../../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../../lib/task-activity",()=>({fetchTaskActivity:vi.fn().mockResolvedValue({reviews:[{id:"rt-1",name:"Implementation",status:"ready"}],plans:[{id:"pl-1",title:"Build it",status:"approved"}],workflow_runs:[{run:{id:"wr-1",workflow_title:"Triage",status:"succeeded"},artifacts:[{id:"wfa-1",output_key:"report"}]}]})}))
describe("TaskActivityTabs",()=>{it("shows related reviews plans workflows and artifacts",async()=>{render(<MemoryRouter><TaskActivityTabs taskId="T-1" notes={[]}/></MemoryRouter>);await screen.findByRole("tab",{name:"Reviews (1)"});fireEvent.click(screen.getByRole("tab",{name:"Reviews (1)"}));expect(screen.getByRole("link",{name:/Implementation/}).getAttribute("href")).toBe("/review/rt-1");fireEvent.click(screen.getByRole("tab",{name:"Plans (1)"}));expect(screen.getByRole("link",{name:/Build it/}).getAttribute("href")).toBe("/plans/pl-1");fireEvent.click(screen.getByRole("tab",{name:"Workflows (1)"}));expect(screen.getByRole("link",{name:"Triage"}).getAttribute("href")).toBe("/workflows/runs/wr-1");expect(screen.getByRole("button",{name:"report"})).toBeTruthy()})})
