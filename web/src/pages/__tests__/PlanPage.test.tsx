import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { PlanPage } from "../PlanPage"
import type { PlanComment } from "../../lib/plan"

const { refresh, addPlanComment, updatePlanComment, askPlanQuestion, respondToPlanQuestion, planAction } = vi.hoisted(() => ({
  refresh: vi.fn(async () => undefined),
  addPlanComment: vi.fn(),
  updatePlanComment: vi.fn(),
  askPlanQuestion: vi.fn(),
  respondToPlanQuestion: vi.fn(),
  planAction: vi.fn(),
}))

const markdown = "# Block comments\n\nRepeat this paragraph.\n\nRepeat this paragraph.\n\n- First nested item\n- Second nested item\n"
const secondStart = markdown.lastIndexOf("Repeat this paragraph.")
const planData = {
  plan: { id:"pl-1", task_id:"T-1", name:"", title:"Block comments", summary:"", status:"proposed", latest_revision:1, created_at:"", updated_at:"" },
  revision: { id:"rev-1", plan_id:"pl-1", revision:1, markdown, manifest_json:"{}", created_at:"" },
  questions: [], responses: [], comments: [] as PlanComment[], messages: [],
}

vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../hooks/usePlan",()=>({usePlan:()=>({data:planData,loading:false,error:null,refresh})}))
vi.mock("../../lib/plan", async importOriginal => {
  const actual = await importOriginal<typeof import("../../lib/plan")>()
  return {...actual,addPlanComment,updatePlanComment,askPlanQuestion,respondToPlanQuestion,planAction}
})

afterEach(cleanup)
beforeEach(()=>{
  refresh.mockClear(); addPlanComment.mockReset(); updatePlanComment.mockReset(); askPlanQuestion.mockReset(); respondToPlanQuestion.mockReset(); planAction.mockReset()
  addPlanComment.mockResolvedValue({id:"comment-1",revision_id:"rev-1",body:"Clarify this",selection_start:secondStart,selection_end:secondStart+22,selected_text:"Repeat this paragraph.",prefix:"",suffix:"",created_at:""})
  askPlanQuestion.mockResolvedValue({thread_id:"thread-1"})
})

function renderPage(){return render(<MemoryRouter initialEntries={["/plans/pl-1"]}><Routes><Route path="/plans/:planId" element={<PlanPage/>}/><Route path="/plans" element={<div>queue</div>}/></Routes></MemoryRouter>)}

describe("PlanPage block comments",()=>{
  it("anchors a comment to the selected occurrence and shows its draft thread",async()=>{
    renderPage()
    const repeated=screen.getAllByRole("button",{name:/Comment on paragraph beginning Repeat this paragraph/})
    fireEvent.click(repeated[1])
    expect(screen.getAllByRole("button",{name:/Comment on paragraph beginning Repeat this paragraph/})[1].getAttribute("aria-pressed")).toBe("true")

    const composer=screen.getByLabelText("Comment on selected block")
    fireEvent.change(composer,{target:{value:"Clarify this"}})
    fireEvent.keyDown(composer,{key:"Enter",metaKey:true})

    await waitFor(()=>expect(addPlanComment).toHaveBeenCalledWith("","pl-1",expect.objectContaining({
      body:"Clarify this",selection_start:secondStart,selection_end:secondStart+22,selected_text:"Repeat this paragraph.",
    })))
    expect(await screen.findByText("Clarify this")).toBeTruthy()
    expect(screen.getByText("Draft")).toBeTruthy()
    expect(screen.getAllByRole("button",{name:/Comment on paragraph beginning Repeat this paragraph/})[1].getAttribute("data-commented")).toBe("draft")
  })

  it("renders one control for an outer list rather than controls for nested items",()=>{
    renderPage()
    expect(screen.getAllByRole("button",{name:/Comment on list beginning/})).toHaveLength(1)
    expect(screen.queryByRole("button",{name:/Comment on list item beginning/})).toBeNull()
  })

  it("drag-selects a contiguous range of outer blocks",async()=>{
    renderPage()
    const first=screen.getByRole("button",{name:/Comment on heading beginning Block comments/})
    fireEvent.mouseDown(first,{button:0})
    const second=screen.getAllByRole("button",{name:/Comment on paragraph beginning Repeat this paragraph/})[0]
    fireEvent.mouseEnter(second,{buttons:1})
    fireEvent.mouseUp(second)

    expect(screen.getByRole("button",{name:/Comment on heading beginning Block comments/}).getAttribute("aria-pressed")).toBe("true")
    expect(screen.getAllByRole("button",{name:/Comment on paragraph beginning Repeat this paragraph/})[0].getAttribute("aria-pressed")).toBe("true")
    const composer=screen.getByLabelText("Comment on selected blocks")
    fireEvent.change(composer,{target:{value:"Treat these together"}})
    fireEvent.keyDown(composer,{key:"Enter",metaKey:true})
    const end=markdown.indexOf("Repeat this paragraph.")+22
    await waitFor(()=>expect(addPlanComment).toHaveBeenCalledWith("","pl-1",expect.objectContaining({
      selection_start:0,selection_end:end,selected_text:markdown.slice(0,end),
    })))
  })

  it("edits an anchored comment without changing its lifecycle",async()=>{
    planData.comments=[{id:"existing",revision_id:"rev-1",body:"Typo here",selection_start:secondStart,selection_end:secondStart+22,selected_text:"Repeat this paragraph.",prefix:"",suffix:"",submitted_at:"2026-01-01",created_at:""}]
    updatePlanComment.mockResolvedValue({...planData.comments[0],body:"Fixed wording"})
    renderPage()
    const thread=screen.getByLabelText("Block comment thread")
    fireEvent.click(within(thread).getByRole("button",{name:"Edit comment"}))
    const editor=within(thread).getByLabelText("Edit comment")
    fireEvent.change(editor,{target:{value:"Fixed wording"}})
    fireEvent.keyDown(editor,{key:"Enter",metaKey:true})
    await waitFor(()=>expect(updatePlanComment).toHaveBeenCalledWith("","pl-1","existing","Fixed wording"))
    expect(await screen.findByText("Submitted")).toBeTruthy()
    expect(screen.getByText("Fixed wording")).toBeTruthy()
    planData.comments=[]
  })

  it("shows saved general feedback at the bottom of the document",async()=>{
    renderPage()
    const composer=screen.getByLabelText("General plan comment")
    addPlanComment.mockResolvedValue({id:"general",revision_id:"rev-1",body:"Overall note",selected_text:"",prefix:"",suffix:"",created_at:""})
    fireEvent.change(composer,{target:{value:"Overall note"}})
    fireEvent.keyDown(composer,{key:"Enter",ctrlKey:true})
    const section=await screen.findByRole("region",{name:"General feedback"})
    expect(within(section).getByText("Overall note")).toBeTruthy()
    expect(screen.queryByLabelText("General comments")).toBeNull()
  })

  it("does not show prior-revision general feedback in the current document",()=>{
    planData.comments=[{id:"old-general",revision_id:"rev-0",body:"Old overall note",selected_text:"",prefix:"",suffix:"",created_at:""}]
    renderPage()
    expect(screen.queryByRole("region",{name:"General feedback"})).toBeNull()
    planData.comments=[]
  })

  it("submits Q&A with Cmd+Enter and Ctrl+Enter but not plain Enter",async()=>{
    renderPage()
    const input=screen.getByLabelText("Question for plan agent")
    fireEvent.change(input,{target:{value:"First question"}})
    fireEvent.keyDown(input,{key:"Enter"})
    expect(askPlanQuestion).not.toHaveBeenCalled()
    fireEvent.keyDown(input,{key:"Enter",metaKey:true})
    await waitFor(()=>expect(askPlanQuestion).toHaveBeenCalledWith("","pl-1","First question"))

    fireEvent.change(input,{target:{value:"Second question"}})
    fireEvent.keyDown(input,{key:"Enter",ctrlKey:true})
    await waitFor(()=>expect(askPlanQuestion).toHaveBeenCalledWith("","pl-1","Second question"))
    expect(askPlanQuestion).toHaveBeenCalledTimes(2)
  })
})
