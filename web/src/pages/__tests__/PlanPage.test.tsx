import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { PlanPage } from "../PlanPage"
import type { PlanComment } from "../../lib/plan"

const { refresh, addPlanComment, updatePlanComment, askPlanQuestion, respondToPlanQuestion, planAction, initializeMermaid, renderMermaid, renderWiremark } = vi.hoisted(() => ({
  refresh: vi.fn(async () => undefined),
  addPlanComment: vi.fn(),
  updatePlanComment: vi.fn(),
  askPlanQuestion: vi.fn(),
  respondToPlanQuestion: vi.fn(),
  planAction: vi.fn(),
  initializeMermaid: vi.fn(),
  renderMermaid: vi.fn(),
  renderWiremark: vi.fn(),
}))

const markdown = "# Block comments\n\nRepeat this paragraph.\n\nRepeat this paragraph.\n\n- First nested item\n- Second nested item\n\n```go\nfmt.Println(\"hello\")\n```\n"
const secondStart = markdown.lastIndexOf("Repeat this paragraph.")
const planData = {
  plan: { id:"pl-1", task_id:"T-1", name:"", title:"Block comments", summary:"", status:"proposed", latest_revision:1, created_at:"", updated_at:"" },
  revision: { id:"rev-1", plan_id:"pl-1", revision:1, markdown, manifest_json:"{}", created_at:"" },
  questions: [], responses: [], comments: [] as PlanComment[], messages: [],
}

vi.mock("mermaid",()=>({default:{initialize:initializeMermaid,render:renderMermaid}}))
vi.mock("@wiremark/core",()=>({render:renderWiremark}))
vi.mock("../../hooks/useServer",()=>({useServer:()=>({baseUrl:""})}))
vi.mock("../../hooks/useRunningAgent",()=>({useRunningAgent:()=>({agent:null,loading:false})}))
vi.mock("../../hooks/usePlan",()=>({usePlan:()=>({data:planData,loading:false,error:null,refresh})}))
vi.mock("../../lib/plan", async importOriginal => {
  const actual = await importOriginal<typeof import("../../lib/plan")>()
  return {...actual,addPlanComment,updatePlanComment,askPlanQuestion,respondToPlanQuestion,planAction}
})

afterEach(cleanup)
beforeEach(()=>{
  planData.revision.markdown=markdown
  planData.comments=[]
  refresh.mockClear(); addPlanComment.mockReset(); updatePlanComment.mockReset(); askPlanQuestion.mockReset(); respondToPlanQuestion.mockReset(); planAction.mockReset(); renderMermaid.mockReset(); renderWiremark.mockReset()
  renderMermaid.mockResolvedValue({svg:"<svg><text>Rendered diagram</text></svg>"})
  renderWiremark.mockReturnValue({svg:"<svg><text>Rendered mockup</text></svg>",diagnostics:[]})
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

  it("anchors comments directly to an outer fenced code block",async()=>{
    renderPage()
    const selector=screen.getByRole("button",{name:/Comment on code block beginning/})
    fireEvent.click(selector)
    const composer=screen.getByLabelText("Comment on selected block")
    fireEvent.change(composer,{target:{value:"Rename this call"}})
    fireEvent.keyDown(composer,{key:"Enter",metaKey:true})
    const fenced="```go\nfmt.Println(\"hello\")\n```"
    const start=markdown.indexOf(fenced)
    await waitFor(()=>expect(addPlanComment).toHaveBeenCalledWith("","pl-1",expect.objectContaining({selection_start:start,selection_end:start+fenced.length,selected_text:fenced})))
  })

  it("renders Mermaid fences as diagrams while preserving exact whole-fence byte anchors",async()=>{
    const originalMarkdown=planData.revision.markdown
    const mermaidFence="```mermaid\ngraph TD\n  A --> B\n```"
    const goFence="```go\nfmt.Println(\"still code\")\n```"
    planData.revision.markdown=`# Dïagrams\n\n${mermaidFence}\n\n${goFence}\n`
    const characterStart=planData.revision.markdown.indexOf(mermaidFence)
    const byteStart=new TextEncoder().encode(planData.revision.markdown.slice(0,characterStart)).length
    const byteEnd=byteStart+new TextEncoder().encode(mermaidFence).length
    addPlanComment.mockResolvedValue({id:"diagram-comment",revision_id:"rev-1",body:"Clarify this flow",selection_start:byteStart,selection_end:byteEnd,selected_text:mermaidFence,prefix:"",suffix:"",created_at:""})

    renderPage()

    expect(await screen.findByText("Rendered diagram")).toBeTruthy()
    expect(screen.getByText('fmt.Println("still code")')).toBeTruthy()
    expect(renderMermaid).toHaveBeenCalledWith(expect.stringMatching(/^mermaid-diagram-/),"graph TD\n  A --> B\n")
    expect(renderMermaid.mock.calls.every(([,source])=>source==="graph TD\n  A --> B\n")).toBe(true)

    const diagramControl=screen.getAllByRole("button",{name:/Comment on code block beginning/})[0]
    fireEvent.click(diagramControl)
    const composer=screen.getByLabelText("Comment on selected block")
    fireEvent.change(composer,{target:{value:"Clarify this flow"}})
    fireEvent.keyDown(composer,{key:"Enter",metaKey:true})

    await waitFor(()=>expect(addPlanComment).toHaveBeenCalledWith("","pl-1",{
      body:"Clarify this flow",selection_start:byteStart,selection_end:byteEnd,selected_text:mermaidFence,
    }))
    expect(await screen.findByText("Clarify this flow")).toBeTruthy()
    expect(screen.getAllByRole("button",{name:/Comment on code block beginning/})[0].getAttribute("data-commented")).toBe("draft")

    fireEvent.click(screen.getByRole("button",{name:"Request changes"}))
    await waitFor(()=>expect(planAction).toHaveBeenCalledWith("","pl-1","request-changes",undefined))
    planData.revision.markdown=originalMarkdown
  })

  it("renders Wiremark fences while preserving exact anchors and stable mockups",async()=>{
    const wireframeFence="```wireframe\nWireframe mobile\n  Button \"Approve\" contained\n```"
    planData.revision.markdown=`# Møckup\n\n${wireframeFence}\n`
    const characterStart=planData.revision.markdown.indexOf(wireframeFence)
    const byteStart=new TextEncoder().encode(planData.revision.markdown.slice(0,characterStart)).length
    const byteEnd=byteStart+new TextEncoder().encode(wireframeFence).length
    addPlanComment.mockResolvedValue({id:"mockup-comment",revision_id:"rev-1",body:"Adjust this screen",selection_start:byteStart,selection_end:byteEnd,selected_text:wireframeFence,prefix:"",suffix:"",created_at:""})

    renderPage()

    expect(await screen.findByText("Rendered mockup")).toBeTruthy()
    expect(renderWiremark).toHaveBeenCalledWith("Wireframe mobile\n  Button \"Approve\" contained\n",{theme:"dark",interactive:true})
    const mockup=screen.getByRole("img",{name:"Wiremark mockup"})
    const settledRenderCount=renderWiremark.mock.calls.length
    fireEvent.change(screen.getByLabelText("Question for plan agent"),{target:{value:"Why?"}})
    await new Promise(resolve=>setTimeout(resolve,0))
    expect(renderWiremark).toHaveBeenCalledTimes(settledRenderCount)
    expect(screen.getByRole("img",{name:"Wiremark mockup"})).toBe(mockup)

    fireEvent.click(screen.getByRole("button",{name:/Comment on code block beginning/}))
    fireEvent.change(screen.getByLabelText("Comment on selected block"),{target:{value:"Adjust this screen"}})
    fireEvent.keyDown(screen.getByLabelText("Comment on selected block"),{key:"Enter",metaKey:true})
    await waitFor(()=>expect(addPlanComment).toHaveBeenCalledWith("","pl-1",{
      body:"Adjust this screen",selection_start:byteStart,selection_end:byteEnd,selected_text:wireframeFence,
    }))
  })

  it("keeps mockup comments and plan actions usable when Wiremark rejects malformed source",async()=>{
    planData.revision.markdown="```wireframe\nWireframe mobile\n  FancyPanel\n```\n"
    renderWiremark.mockImplementation(()=>{throw new Error("Unknown component FancyPanel at line 2")})

    renderPage()

    expect((await screen.findByRole("alert")).textContent).toContain("Unknown component FancyPanel at line 2")
    fireEvent.click(screen.getByRole("button",{name:/Comment on code block beginning/}))
    expect(screen.getByLabelText("Comment on selected block")).toBeTruthy()
    expect(screen.getByRole("button",{name:"Approve"})).toBeTruthy()
  })

  it("keeps rendered Mermaid diagrams mounted while typing unrelated feedback drafts",async()=>{
    planData.revision.markdown="```mermaid\ngraph TD\nA-->B\n```\n"

    renderPage()

    expect(await screen.findByText("Rendered diagram")).toBeTruthy()
    const diagram=screen.getByRole("img",{name:"Mermaid diagram"})
    const settledRenderCount=renderMermaid.mock.calls.length
    const question=screen.getByLabelText("Question for plan agent") as HTMLTextAreaElement
    const comment=screen.getByLabelText("General plan comment") as HTMLTextAreaElement

    fireEvent.change(question,{target:{value:"W"}})
    fireEvent.change(question,{target:{value:"Wh"}})
    fireEvent.change(question,{target:{value:"Why?"}})
    fireEvent.change(comment,{target:{value:"N"}})
    fireEvent.change(comment,{target:{value:"Note"}})
    await new Promise(resolve=>setTimeout(resolve,0))

    expect(question.value).toBe("Why?")
    expect(comment.value).toBe("Note")
    expect(renderMermaid).toHaveBeenCalledTimes(settledRenderCount)
    expect(screen.getByRole("img",{name:"Mermaid diagram"})).toBe(diagram)
  })

  it("renders duplicate and case-insensitive Mermaid fences independently without treating inline or unknown code as diagrams",async()=>{
    const originalMarkdown=planData.revision.markdown
    planData.revision.markdown="`mermaid`\n\n```MERMAID\ngraph TD\nA-->B\n```\n\n```mermaid\ngraph TD\nA-->B\n```\n\n```unknown\nmermaid\n```\n\n```\nmermaid\n```\n"

    renderPage()

    await waitFor(()=>expect(screen.getAllByRole("img",{name:"Mermaid diagram"})).toHaveLength(2))
    const renderIDs=renderMermaid.mock.calls.map(([id])=>id)
    expect(new Set(renderIDs).size).toBe(renderIDs.length)
    expect(renderMermaid.mock.calls.every(([,source])=>source==="graph TD\nA-->B\n")).toBe(true)
    expect(screen.getAllByRole("button",{name:/Comment on code block beginning/})).toHaveLength(4)
    expect(screen.getAllByText("mermaid").length).toBeGreaterThanOrEqual(3)
    planData.revision.markdown=originalMarkdown
  })

  it("keeps diagram comments and plan actions usable when Mermaid rejects malformed source",async()=>{
    const originalMarkdown=planData.revision.markdown
    planData.revision.markdown="```mermaid\nnot valid mermaid\n```\n"
    renderMermaid.mockRejectedValue(new Error("Parse error"))

    renderPage()

    expect(await screen.findByText("Diagram could not be rendered. Mermaid source:")).toBeTruthy()
    expect(screen.getByText("not valid mermaid")).toBeTruthy()
    const control=screen.getByRole("button",{name:/Comment on code block beginning/})
    fireEvent.click(control)
    expect(screen.getByLabelText("Comment on selected block")).toBeTruthy()
    expect(screen.getByRole("button",{name:"Approve"})).toBeTruthy()
    planData.revision.markdown=originalMarkdown
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

  it("lets mobile header actions wrap without forcing page overflow",()=>{
    const { container }=renderPage()
    expect(container.querySelector("[data-plan-actions]")?.className).toContain("flex-wrap")
    expect(container.querySelector("[data-plan-actions]")?.className).toContain("w-full")
  })

  it("approves with cleanup unchecked by default or checked by the reviewer",async()=>{
    renderPage()
    const checkbox=screen.getByRole("checkbox",{name:/Clean up plan files/})
    expect((checkbox as HTMLInputElement).checked).toBe(false)
    fireEvent.click(checkbox)
    fireEvent.click(screen.getByRole("button",{name:"Approve"}))
    await waitFor(()=>expect(planAction).toHaveBeenCalledWith("","pl-1","approve",{cleanup_after_implementation:true}))
  })

  it("submits Q&A with Cmd+Enter and Ctrl+Enter but not plain Enter",async()=>{
    renderPage()
    const input=screen.getByLabelText("Question for plan agent")
    fireEvent.change(input,{target:{value:"First question"}})
    fireEvent.keyDown(input,{key:"Enter"})
    expect(askPlanQuestion).not.toHaveBeenCalled()
    fireEvent.keyDown(input,{key:"Enter",metaKey:true})
    await waitFor(()=>expect(askPlanQuestion).toHaveBeenCalledWith("","pl-1","First question"))
    const success = await screen.findByRole("status")
    expect(success.textContent).toContain("Question sent")
    expect(success.className).toContain("emerald")

    askPlanQuestion.mockResolvedValueOnce({thread_id:"thread-2",warning:"Agent delivery is delayed"})
    fireEvent.change(input,{target:{value:"Second question"}})
    fireEvent.keyDown(input,{key:"Enter",ctrlKey:true})
    await waitFor(()=>expect(askPlanQuestion).toHaveBeenCalledWith("","pl-1","Second question"))
    const warning = await screen.findByRole("status")
    expect(warning.textContent).toContain("Agent delivery is delayed")
    expect(warning.className).toContain("amber")
    expect(askPlanQuestion).toHaveBeenCalledTimes(2)
  })
})
