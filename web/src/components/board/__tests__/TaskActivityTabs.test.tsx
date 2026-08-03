import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react"
import {MemoryRouter} from "react-router-dom"
import {beforeEach,describe,expect,it,vi} from "vitest"
import {TaskActivityTabs} from "../TaskActivityTabs"

const {apiFetch}=vi.hoisted(()=>({apiFetch:vi.fn()}))
vi.mock("../../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../../lib/api",()=>({apiFetch}))
vi.mock("../../../lib/task-activity",()=>({fetchTaskActivity:vi.fn().mockResolvedValue({reviews:[{id:"rt-1",name:"Implementation",status:"ready"}],plans:[{id:"pl-1",title:"Build it",status:"approved"}],workflow_runs:[{run:{id:"wr-1",workflow_title:"Triage",status:"succeeded"},artifacts:[{id:"wfa-1",output_key:"triage",original_path:"reports/task-triage.md"},{id:"wfa-2",output_key:"legacy-report"}]}]})}))

describe("TaskActivityTabs",()=>{
  beforeEach(()=>{cleanup();apiFetch.mockReset();apiFetch.mockResolvedValue(new Response(new Blob(["report"])));vi.stubGlobal("URL",{...URL,createObjectURL:vi.fn(()=>"blob:test"),revokeObjectURL:vi.fn()})})

  it("shows related reviews plans workflows and artifact filenames",async()=>{render(<MemoryRouter><TaskActivityTabs taskId="T-1" notes={[]}/></MemoryRouter>);await screen.findByRole("tab",{name:"Reviews (1)"});fireEvent.click(screen.getByRole("tab",{name:"Reviews (1)"}));expect(screen.getByRole("link",{name:/Implementation/}).getAttribute("href")).toBe("/review/rt-1");fireEvent.click(screen.getByRole("tab",{name:"Plans (1)"}));expect(screen.getByRole("link",{name:/Build it/}).getAttribute("href")).toBe("/plans/pl-1");fireEvent.click(screen.getByRole("tab",{name:"Workflows (1)"}));expect(screen.getByRole("link",{name:"Triage"}).getAttribute("href")).toBe("/workflows/runs/wr-1");expect(screen.getByRole("button",{name:"task-triage.md"})).toBeTruthy();expect(screen.getByRole("button",{name:"legacy-report"})).toBeTruthy()})

  it("downloads an artifact with its original basename",async()=>{
    let downloadedName=""
    vi.spyOn(HTMLAnchorElement.prototype,"click").mockImplementation(function(this:HTMLAnchorElement){downloadedName=this.download})
    render(<MemoryRouter><TaskActivityTabs taskId="T-1" notes={[]}/></MemoryRouter>)
    fireEvent.click(await screen.findByRole("tab",{name:"Workflows (1)"}))
    fireEvent.click(screen.getByRole("button",{name:"task-triage.md"}))
    await waitFor(()=>expect(downloadedName).toBe("task-triage.md"))
  })
})
