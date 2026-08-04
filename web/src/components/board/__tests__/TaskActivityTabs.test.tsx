import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react"
import {MemoryRouter} from "react-router-dom"
import {beforeEach,describe,expect,it,vi} from "vitest"
import {TaskActivityTabs} from "../TaskActivityTabs"

const {apiFetch,initializeMermaid,renderDiagram}=vi.hoisted(()=>({apiFetch:vi.fn(),initializeMermaid:vi.fn(),renderDiagram:vi.fn()}))
vi.mock("mermaid",()=>({default:{initialize:initializeMermaid,render:renderDiagram}}))
vi.mock("../../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../../lib/api",()=>({apiFetch}))
vi.mock("../../../lib/task-activity",()=>({fetchTaskActivity:vi.fn().mockResolvedValue({reviews:[{id:"rt-1",name:"Implementation",status:"ready"}],plans:[{id:"pl-1",title:"Build it",status:"approved"}],workflow_runs:[{run:{id:"wr-1",workflow_title:"Triage",status:"succeeded"},artifacts:[{id:"wfa-1",output_key:"triage",original_path:"reports/task-triage.md"},{id:"wfa-2",output_key:"legacy-report"}]}]})}))

describe("TaskActivityTabs",()=>{
  beforeEach(()=>{cleanup();apiFetch.mockReset();apiFetch.mockResolvedValue(new Response(new Blob(["report"])));renderDiagram.mockReset();vi.stubGlobal("URL",{...URL,createObjectURL:vi.fn(()=>"blob:test"),revokeObjectURL:vi.fn()})})

  it("renders task notes as semantic Markdown",()=>{
    const body="# Triage\n\n- Inspect `TaskActivityTabs`\n\n| Status | Value |\n| --- | --- |\n| Result | Confirmed |\n\n[Open docs](https://example.com)\n\n```diff\n-raw\n+rendered\n```\n\n<script>alert('x')</script>"
    const {container}=render(<MemoryRouter><TaskActivityTabs taskId="T-1" notes={[{id:"note-1",body,source:"triage",created_at:"2026-08-04T23:31:45Z"}]}/></MemoryRouter>)

    expect(screen.getByRole("heading",{name:"Triage"})).toBeTruthy()
    expect(screen.getByRole("list")).toBeTruthy()
    expect(screen.getByText("TaskActivityTabs").tagName).toBe("CODE")
    expect(screen.getByRole("table")).toBeTruthy()
    expect(screen.getByRole("link",{name:"Open docs"}).getAttribute("href")).toBe("https://example.com")
    expect(container.querySelector("pre code.language-diff")).toBeTruthy()
    expect(container.querySelector("script")).toBeNull()
  })

  it("renders Mermaid diagrams in task notes",async()=>{
    const source="graph TD\n  Raw --> Rendered"
    renderDiagram.mockResolvedValue({svg:"<svg><text>Rendered note diagram</text></svg>"})
    render(<MemoryRouter><TaskActivityTabs taskId="T-1" notes={[{id:"note-1",body:`\`\`\`mermaid\n${source}\n\`\`\``,source:"triage",created_at:"2026-08-04T23:31:45Z"}]}/></MemoryRouter>)

    expect(await screen.findByText("Rendered note diagram")).toBeTruthy()
    expect(renderDiagram).toHaveBeenCalledWith(expect.stringMatching(/^mermaid-diagram-\d+$/),`${source}\n`)
    expect(screen.getByRole("button",{name:"Expand Mermaid diagram"})).toBeTruthy()
  })

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
