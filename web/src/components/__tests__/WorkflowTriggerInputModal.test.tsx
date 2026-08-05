import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { WorkflowTriggerInputModal } from "../WorkflowTriggerInputModal"

afterEach(()=>{cleanup();vi.restoreAllMocks()})
describe("WorkflowTriggerInputModal",()=>{
  it("submits an existing repository preset and remembers it",async()=>{
    const fetchMock=vi.spyOn(globalThis,"fetch").mockResolvedValue(new Response(JSON.stringify({status:"started"}),{status:200}))
    const onStarted=vi.fn()
    render(<WorkflowTriggerInputModal baseUrl="http://legato" request={{id:"wtr-1",workflow_name:"triage",status:"waiting_input",missing_inputs_json:'["repository"]'}} repositories={[{name:"legato",repo:"cpave3/legato",primary_dir:"/projects/legato",base_branch:"main"}]} onClose={()=>{}} onStarted={onStarted}/>)
    fireEvent.click(screen.getByRole("button",{name:"Start workflow"}))
    await waitFor(()=>expect(onStarted).toHaveBeenCalled())
    const [,init]=fetchMock.mock.calls[0]
    expect(JSON.parse(String(init?.body))).toEqual({inputs:{repository:"legato"},remember:true})
  })
  it("confirms a workflow without asking for repository input",async()=>{
    const fetchMock=vi.spyOn(globalThis,"fetch").mockResolvedValue(new Response(JSON.stringify({status:"started"}),{status:200}))
    const onStarted=vi.fn()
    render(<WorkflowTriggerInputModal baseUrl="http://legato" request={{id:"wtr-2",workflow_name:"close-active-agent",status:"waiting_confirmation",missing_inputs_json:"[]",definition_json:'{"title":"Close active agent","description":"Close the open agent session."}'}} repositories={[]} onClose={()=>{}} onStarted={onStarted}/>)
    expect(screen.getByRole("heading",{name:"Close active agent"})).toBeTruthy()
    expect(screen.queryByLabelText("Repository")).toBeNull()
    fireEvent.click(screen.getByRole("button",{name:"Start workflow"}))
    await waitFor(()=>expect(onStarted).toHaveBeenCalled())
    const [,init]=fetchMock.mock.calls[0]
    expect(JSON.parse(String(init?.body))).toEqual({confirm:true})
  })
  it("keeps the request pending when closed",()=>{const onClose=vi.fn();render(<WorkflowTriggerInputModal baseUrl="" request={{id:"wtr-1",workflow_name:"triage",status:"waiting_input",missing_inputs_json:"[]"}} repositories={[]} onClose={onClose} onStarted={()=>{}}/>);fireEvent.click(screen.getByRole("button",{name:"Not now"}));expect(onClose).toHaveBeenCalledTimes(1)})
})
