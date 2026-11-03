import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react"
import {beforeEach,describe,expect,it,vi} from "vitest"
import {TaskStacks} from "../TaskStacks"
import type {TaskStack} from "../../../lib/task-stacks"

const {apiFetch}=vi.hoisted(()=>({apiFetch:vi.fn()}))
vi.mock("../../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../../lib/api",()=>({apiFetch}))

const pushed:TaskStack={clone_id:3,repo:"acme/widget",root:"main",read_at:"2026-11-03T09:00:00Z",live:true,importable:true,document_name:"t1-widget-stack.json",
  branches:[{name:"legato/t1-a",parent:"main",base_sha:"a",head_sha:"b",state:"pushed"},{name:"legato/t1-b",parent:"legato/t1-a",base_sha:"b",head_sha:"c",state:"pushed"}]}

describe("TaskStacks",()=>{
  beforeEach(()=>{cleanup();apiFetch.mockReset();apiFetch.mockResolvedValue(new Response(new Blob(['{"format":1}'])));vi.stubGlobal("URL",{...URL,createObjectURL:vi.fn(()=>"blob:test"),revokeObjectURL:vi.fn()})})

  it("shows a pushed stack as a tree with its download and import command",async()=>{
    let downloaded=""
    vi.spyOn(HTMLAnchorElement.prototype,"click").mockImplementation(function(this:HTMLAnchorElement){downloaded=this.download})
    render(<TaskStacks taskId="t1" stacks={[pushed]}/>)

    expect(screen.getByText("acme/widget")).toBeTruthy()
    const items=screen.getAllByRole("listitem")
    expect(items.map(item=>item.textContent)).toEqual([expect.stringContaining("legato/t1-a"),expect.stringContaining("legato/t1-b")])
    expect(items[1].getAttribute("aria-level")).toBe("2")
    expect(screen.getAllByText("pushed")).toHaveLength(2)
    expect(screen.getByText("st graph import ~/Downloads/t1-widget-stack.json")).toBeTruthy()

    fireEvent.click(screen.getByRole("button",{name:/Download/}))
    await waitFor(()=>expect(downloaded).toBe("t1-widget-stack.json"))
    expect(apiFetch).toHaveBeenCalledWith("","/api/tasks/t1/stacks/3/document")
  })

  it("says why a stack cannot be taken home yet",()=>{
    const unpushed:TaskStack={...pushed,importable:false,document_name:undefined,reason:"not pushed: legato/t1-b",
      branches:[pushed.branches[0],{...pushed.branches[1],state:"unpushed"}]}
    render(<TaskStacks taskId="t1" stacks={[unpushed]}/>)

    expect(screen.getByText("unpushed")).toBeTruthy()
    expect(screen.getByText(/not pushed: legato\/t1-b/)).toBeTruthy()
    expect(screen.queryByRole("button",{name:/Download/})).toBeNull()
    expect(screen.queryByText(/st graph import/)).toBeNull()
  })

  it("marks a stack whose clone is gone",()=>{
    render(<TaskStacks taskId="t1" stacks={[{...pushed,live:false}]}/>)
    expect(screen.getByText(/clone removed/i)).toBeTruthy()
    expect(screen.getByRole("button",{name:/Download/})).toBeTruthy()
  })
})
