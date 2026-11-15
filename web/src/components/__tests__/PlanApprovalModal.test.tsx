import { render, screen, fireEvent, within } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { PlanApprovalModal } from "../PlanApprovalModal"
const { subscribe, send } = vi.hoisted(() => ({ subscribe: vi.fn(() => () => {}), send: vi.fn() }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({subscribe,send,connected:true}) }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({baseUrl:"http://legato"}) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({addToast:vi.fn()}) }))
vi.mock("../../lib/swarm", () => ({ getAllPendingPlans: vi.fn().mockResolvedValue([{parent_task_id:"T1",plan_path:"/plan.yaml",reply_socket:"/reply",plan:{header:{working_dir:"/repo",summary:"Build it"},subtasks:[]}}]) }))
it("opens a named plan decision, traps focus and dismisses without a verdict", async () => {
  render(<PlanApprovalModal />)
  const dialog = await screen.findByRole("dialog", {name:"Plan Proposed"})
  within(dialog).getByRole("button", {name:"Approve"}).focus()
  fireEvent.keyDown(document.activeElement!, {key:"Tab"})
  expect(document.activeElement).toBe(within(dialog).getByRole("button", {name:"Close"}))
  fireEvent.keyDown(document.activeElement!, {key:"Escape"})
  expect(screen.queryByRole("dialog")).toBeNull()
  expect(send).not.toHaveBeenCalled()
})
