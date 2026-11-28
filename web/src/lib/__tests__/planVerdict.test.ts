import { describe, expect, it } from "vitest"
import { newPendingPlan, otherSurfaceVerdict, planVerdictMessage, verdictToast } from "../planVerdict"

const plan = (id: string) => ({ parent_task_id: id, plan_path: `/${id}.yaml`, reply_socket: "/r" })

describe("planVerdict", () => {
  it("answers the conductor with the plan's path and socket", () => {
    expect(planVerdictMessage(plan("T1"), "approved")).toEqual({ type: "plan_verdict", parent_task_id: "T1", plan_path: "/T1.yaml", reply_socket: "/r", status: "approved", notes: "" })
    expect(planVerdictMessage(plan("T1"), "rejected", "no").notes).toBe("no")
    expect(verdictToast("approved")).toEqual(["Plan approved", "success"])
    expect(verdictToast("rejected")).toEqual(["Plan rejected with notes", "error"])
  })

  it("shows the oldest pending plan unless it is already shown", () => {
    expect(newPendingPlan([], null)).toBeNull()
    expect(newPendingPlan([plan("T1"), plan("T2")], null)?.parent_task_id).toBe("T1")
    expect(newPendingPlan([plan("T1"), plan("T2")], "T1")).toBeNull()
    expect(newPendingPlan([plan("T1")], "T2")?.parent_task_id).toBe("T1")
  })

  it("reads another surface's verdict on the plan shown", () => {
    expect(otherSurfaceVerdict({ type: "swarm_changed", parent_task_id: "T1", status: "plan_applied" }, "T1")).toBe("approved")
    expect(otherSurfaceVerdict({ type: "swarm_changed", parent_task_id: "T1", status: "rejected" }, "T1")).toBe("rejected")
    expect(otherSurfaceVerdict({ type: "swarm_changed", parent_task_id: "T1", status: "running" }, "T1")).toBeNull()
    expect(otherSurfaceVerdict({ type: "swarm_changed", parent_task_id: "T2", status: "rejected" }, "T1")).toBeNull()
    expect(otherSurfaceVerdict({ type: "swarm_changed", status: "rejected" }, undefined)).toBeNull()
    expect(otherSurfaceVerdict({ type: "plan_proposed", parent_task_id: "T1", status: "rejected" }, "T1")).toBeNull()
  })
})
