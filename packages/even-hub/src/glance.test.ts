import { describe, expect, it } from "vitest"
import { getTextWidth } from "@evenrealities/pretext"
import { glance, ITEM_TEXT_WIDTH, LIST_ITEM_BYTES, LIST_ITEMS } from "./glance"
import type { Agent } from "./legato"

const at = (minute: number) => `2026-11-13T07:${String(minute).padStart(2, "0")}:00Z`

function agent(fields: Partial<Agent> & Pick<Agent, "task_id">): Agent {
  return {
    title: "",
    status: "running",
    activity: "working",
    sandboxed: false,
    started_at: at(0),
    ...fields,
  }
}

const mixed = [
  agent({ task_id: "IDLE-1", title: "Idle one", activity: "", started_at: at(1) }),
  agent({ task_id: "RUN-1", title: "Running one", activity: "working", started_at: at(2) }),
  agent({ task_id: "BG-1", title: "Waiting on a build", activity: "background", started_at: at(3) }),
  agent({ task_id: "ASK-1", title: "Asks for approval", activity: "waiting", started_at: at(4) }),
  agent({ task_id: "ERR-1", title: "Hit a rate limit", activity: "error", started_at: at(5) }),
  agent({ task_id: "IDLE-2", title: "Idle two", activity: "", started_at: at(0) }),
]

const texts = (items: { text: string }[]) => items.map((i) => i.text)
const utf8 = (s: string) => new TextEncoder().encode(s).length

describe("glance", () => {
  it("lists the agents that are not idle on the active tab, those that need the operator first, then a way to the idle ones", () => {
    const screen = glance(mixed, { tab: "active" })

    expect(texts(screen.items)).toEqual([
      "◆ Reeve  asleep",
      "● needs you  ASK-1 Asks for approval",
      "▲ error  ERR-1 Hit a rate limit",
      "▶ running  RUN-1 Running one",
      "▷ background  BG-1 Waiting on a build",
      "Idle agents (2) →",
    ])
    expect(screen.items.map((i) => i.open ?? i.go)).toEqual([undefined, "ASK-1", "ERR-1", "RUN-1", "BG-1", "idle"])
  })

  it("lists the idle agents on the idle tab, oldest first, then the way back", () => {
    const screen = glance(mixed, { tab: "idle" })

    expect(texts(screen.items)).toEqual(["○ IDLE-2 Idle two", "○ IDLE-1 Idle one", "← Active agents (4)"])
    expect(screen.items.map((i) => i.open ?? i.go)).toEqual(["IDLE-2", "IDLE-1", "active"])
  })

  it("names the tabs with how many agents each holds, and opens on the active one", () => {
    expect(glance(mixed)).toMatchObject({ tab: "active", tabs: { active: "Active 4", idle: "Idle 2" } })
  })

  it("says so when no agent is running", () => {
    expect(texts(glance([]).items)).toEqual(["◆ Reeve  asleep", "No agents running."])
  })

  it("says nothing is running when every agent is idle, with the way to them", () => {
    const screen = glance([agent({ task_id: "A", activity: "" })])

    expect(texts(screen.items)).toEqual(["◆ Reeve  asleep", "Nothing running.", "Idle agents (1) →"])
  })

  it("leaves out agents whose session has ended", () => {
    const screen = glance([
      agent({ task_id: "LIVE", activity: "working" }),
      agent({ task_id: "GONE", status: "dead", activity: "" }),
      agent({ task_id: "LOST", status: "interrupted", activity: "" }),
    ])

    expect(texts(screen.items)).toEqual(["◆ Reeve  asleep", "▶ running  LIVE"])
    expect(screen.tabs).toEqual({ active: "Active 1", idle: "Idle 0" })
  })

  it("cuts an item to the list's width and to the bytes the firmware takes", () => {
    const title = "Rework the whole credential broker so that every grant expires on its own schedule"
    const [, item] = glance([agent({ task_id: "LEG-163", title, activity: "waiting" })]).items

    expect(item.text.startsWith("● needs you  LEG-163 Rework")).toBe(true)
    expect(item.text.endsWith("…")).toBe(true)
    expect(getTextWidth(item.text)).toBeLessThanOrEqual(ITEM_TEXT_WIDTH)
    expect(utf8(item.text)).toBeLessThanOrEqual(LIST_ITEM_BYTES)
  })

  it("keeps to the firmware's twenty items, counting what does not fit", () => {
    const many = Array.from({ length: 30 }, (_, i) => agent({ task_id: `LEG-${100 + i}` }))
    const items = glance([...many, agent({ task_id: "Z", activity: "" })]).items

    expect(items).toHaveLength(LIST_ITEMS)
    expect(items.at(-2)).toEqual({ text: "+13 more" })
    expect(items.at(-1)).toMatchObject({ go: "idle" })
  })

  it("says when the list was read, and shows a problem while keeping the last list", () => {
    const screen = glance([agent({ task_id: "A", title: "Still here" })], {
      updatedAt: new Date(2026, 10, 13, 9, 5),
      now: new Date(2026, 10, 13, 9, 5),
      problem: "Can't reach Legato",
    })

    expect(screen.clock).toBe("09:05")
    expect(screen.status).toBe("Can't reach Legato")
    expect(texts(screen.items)).toEqual(["◆ Reeve  asleep", "▶ running  A Still here"])
    expect(glance([]).status).toBe("")
  })
})

describe("the Reeve on the list", () => {
  const reeve = (activity: string) => agent({ task_id: "rv1", title: "The Reeve", activity, reeve: true })

  it("is pinned first on the active tab, with its state, and counted on neither tab", () => {
    const screen = glance([...mixed, reeve("working")], { tab: "active" })

    expect(screen.items[0]).toEqual({ text: "◆ Reeve  working", reeve: true })
    expect(texts(screen.items.slice(1))).toEqual(texts(glance(mixed, { tab: "active" }).items.slice(1)))
    expect(screen.tabs).toEqual({ active: "Active 4", idle: "Idle 2" })
  })

  it("is ready when idle, and asleep when it is not running", () => {
    expect(glance([reeve("")]).items[0].text).toBe("◆ Reeve  ready")
    expect(glance([reeve("waiting")]).items[0].text).toBe("◆ Reeve  needs you")
    expect(glance([]).items).toEqual([{ text: "◆ Reeve  asleep", reeve: true }, { text: "No agents running." }])
  })

  it("is not on the idle tab", () => {
    expect(texts(glance([...mixed, reeve("")], { tab: "idle" }).items)).toEqual(texts(glance(mixed, { tab: "idle" }).items))
  })

  it("leaves room for itself within the list's items", () => {
    const many = Array.from({ length: 30 }, (_, i) => agent({ task_id: `RUN-${i}`, started_at: at(i) }))
    const items = glance([...many, reeve("")]).items

    expect(items).toHaveLength(LIST_ITEMS)
    expect(items[0].reeve).toBe(true)
    expect(items[LIST_ITEMS - 1].text).toBe("+12 more")
  })
})

describe("approvals on the list", () => {
  it("adds the way to the approvals under the Reeve while any wait, within the list's items", () => {
    const many = Array.from({ length: 30 }, (_, i) => agent({ task_id: `RUN-${i}`, started_at: at(i) }))
    const items = glance(many, { approvals: 2 }).items

    expect(items[1]).toEqual({ text: "● To approve (2) →", approvals: true })
    expect(items).toHaveLength(LIST_ITEMS)
    expect(items[LIST_ITEMS - 1].text).toBe("+13 more")
    expect(glance(many, { approvals: 0 }).items[1].text).toBe("▶ running  RUN-0")
  })

  it("adds the way to the questions above the approvals while any wait", () => {
    const many = Array.from({ length: 30 }, (_, i) => agent({ task_id: `RUN-${i}`, started_at: at(i) }))
    const items = glance(many, { approvals: 2, questions: 4 }).items

    expect(items[1]).toEqual({ text: "● Questions (4) →", questions: true })
    expect(items[2]).toEqual({ text: "● To approve (2) →", approvals: true })
    expect(items).toHaveLength(LIST_ITEMS)
    expect(items[LIST_ITEMS - 1].text).toBe("+14 more")
    expect(glance(many, { questions: 1 }).items.slice(1, 3).map((i) => i.text)).toEqual(["● Questions (1) →", "▶ running  RUN-0"])
  })
})

describe("how old the list is", () => {
  const read = new Date(2026, 10, 13, 9, 5, 0)
  const later = (seconds: number) => new Date(read.getTime() + seconds * 1000)

  it("goes beside the time it was read once it is a minute old, in minutes, then an hour or more", () => {
    const clock = (seconds: number) => glance([], { updatedAt: read, now: later(seconds) }).clock

    expect(clock(4)).toBe("09:05")
    expect(clock(59)).toBe("09:05")
    expect(clock(60)).toBe("1m · 09:05")
    expect(clock(185)).toBe("3m · 09:05")
    expect(clock(2 * 3600)).toBe("1h+ · 09:05")
    expect(glance([], { now: later(5) }).clock).toBe("")
  })
})
