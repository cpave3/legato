import { describe, expect, it } from "vitest"
import { AtSign, MessageCircle, MessageSquareShare, Smile, SmilePlus } from "lucide-react"
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

  it("names a chat trigger node by the server's summary", () => {
    expect(nodeName({ id: "t", kind: "trigger", trigger: { kind: "chat.message", summary: "Message in C123 on slack", enabled: false } })).toBe("Trigger: Message in C123 on slack, paused here")
    expect(nodeName({ id: "t", kind: "trigger", trigger: { kind: "chat.mention", summary: "", enabled: true } })).toBe("Trigger: Bot mentioned, on here")
  })
})
