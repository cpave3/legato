import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { PlanPage } from "../PlanPage"

const { refresh, addPlanComment, askPlanQuestion, respondToPlanQuestion, planAction } = vi.hoisted(() => ({
  refresh: vi.fn(async () => undefined),
  addPlanComment: vi.fn(),
  askPlanQuestion: vi.fn(),
  respondToPlanQuestion: vi.fn(),
  planAction: vi.fn(),
}))

const markdown = "# Block comments\n\nRepeat this paragraph.\n\nRepeat this paragraph.\n"
const secondStart = markdown.lastIndexOf("Repeat this paragraph.")
const planData = {
  plan: { id:"pl-1", task_id:"T-1", name:"", title:"Block comments", summary:"", status:"proposed", latest_revision:1, created_at:"", updated_at:"" },
  revision: { id:"rev-1", plan_id:"pl-1", revision:1, markdown, manifest_json:"{}", created_at:"" },
  questions: [], responses: [], comments: [], messages: [],
}

vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../hooks/usePlan",()=>({usePlan:()=>({data:planData,loading:false,error:null,refresh})}))
vi.mock("../../lib/plan", async importOriginal => {
  const actual = await importOriginal<typeof import("../../lib/plan")>()
  return {...actual,addPlanComment,askPlanQuestion,respondToPlanQuestion,planAction}
})

afterEach(cleanup)
beforeEach(()=>{
  refresh.mockClear(); addPlanComment.mockReset(); askPlanQuestion.mockReset(); respondToPlanQuestion.mockReset(); planAction.mockReset()
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
