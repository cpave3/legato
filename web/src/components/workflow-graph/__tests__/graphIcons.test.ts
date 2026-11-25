import { describe, expect, it } from "vitest"
import { AtSign, MessageCircle, MessageSquarePlus, MessageSquareQuote, MessageSquareShare, MousePointerClick, Smile, SmilePlus, TicketPlus } from "lucide-react"
import { stepIcon, triggerIcon } from "../graphIcons"
import { nodeName, stepTypeLabel, triggerName } from "../../../lib/workflowGraph"
import { triggerKinds } from "../../workflow-editor/triggerForms"

describe("the graph's chat nodes", () => {
  it("names and draws every trigger kind in the schema", () => {
    for (const { kind } of triggerKinds) {
      expect(triggerName(kind), kind).not.toBe(kind)
      expect(triggerIcon(kind), kind).toBeTruthy()
    }
    expect(triggerName("chat.message")).toBe("Chat message")
    expect(triggerName("chat.mention")).toBe("Bot mentioned")
    expect(triggerIcon("chat.message")).toBe(MessageCircle)
    expect(triggerIcon("chat.mention")).toBe(AtSign)
    expect(triggerName("chat.button")).toBe("Chat button")
    expect(triggerIcon("chat.button")).toBe(MousePointerClick)
  })

  it("labels and draws the chat message step", () => {
    expect(stepTypeLabel("chat.message.send")).toBe("Chat message")
    expect(stepIcon("chat.message.send")).toBe(MessageSquareShare)
  })

  it("labels and draws the chat reaction steps", () => {
    expect(stepTypeLabel("chat.reaction.add")).toBe("Chat reaction")
    expect(stepTypeLabel("chat.reaction.remove")).toBe("Remove reaction")
    expect(stepIcon("chat.reaction.add")).toBe(SmilePlus)
    expect(stepIcon("chat.reaction.remove")).toBe(Smile)
  })

  it("labels and draws the chat thread step", () => {
    expect(stepTypeLabel("chat.thread.read")).toBe("Chat thread")
    expect(stepIcon("chat.thread.read")).toBe(MessageSquareQuote)
  })

  it("labels and draws the ticket steps", () => {
    expect(stepTypeLabel("ticket.create")).toBe("Create ticket")
    expect(stepTypeLabel("ticket.comment")).toBe("Ticket comment")
    expect(stepIcon("ticket.create")).toBe(TicketPlus)
    expect(stepIcon("ticket.comment")).toBe(MessageSquarePlus)
  })

  it("names a chat trigger node by the server's summary", () => {
    expect(nodeName({ id: "t", kind: "trigger", trigger: { kind: "chat.message", summary: "Message in C123 on slack", enabled: false } })).toBe("Trigger: Message in C123 on slack, paused here")
    expect(nodeName({ id: "t", kind: "trigger", trigger: { kind: "chat.mention", summary: "", enabled: true } })).toBe("Trigger: Bot mentioned, on here")
  })
})
