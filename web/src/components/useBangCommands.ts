import { useCallback, useState, type RefObject } from "react"
import type { QueuedEntry } from "./Composer"
import { postAgentJson } from "../lib/nativeAgentApi"

export interface BangOptions {
  agentId: string
  baseUrl: string
  busy: boolean
  resync: () => Promise<void>
  failWith: (reason: unknown) => void
  scrollToBottomAfterRender: () => void
  setDraft: (draft: string) => void
  textareaRef: RefObject<HTMLTextAreaElement | null>
}

// useBangCommands runs the shell commands the operator writes as "!command":
// at once while the agent is idle, else queued until its turn ends.
export function useBangCommands({ agentId, baseUrl, busy, resync, failWith, scrollToBottomAfterRender, setDraft, textareaRef }: BangOptions) {
  const [bangQueue, setBangQueue] = useState<string[]>([])
  const [bangRunning, setBangRunning] = useState(false)

  const executeBang = useCallback(async (command: string) => {
    setBangRunning(true)
    try {
      const response = await postAgentJson(baseUrl, agentId, "bang", { command })
      if (!response.ok) throw new Error(`bang: ${response.status}`)
      await resync()
      scrollToBottomAfterRender()
    } catch (reason) {
      failWith(reason)
    } finally {
      setBangRunning(false)
    }
  }, [agentId, baseUrl, resync, failWith, scrollToBottomAfterRender])

  const runOrQueue = useCallback((command: string) => {
    if (busy) {
      setBangQueue((prev) => [...prev, command])
    } else {
      void executeBang(command)
    }
  }, [busy, executeBang])

  // A turn ended: the first queued command runs.
  const runNext = useCallback(() => {
    setBangQueue((prev) => {
      if (prev.length > 0) void executeBang(prev[0])
      return prev.slice(1)
    })
  }, [executeBang])

  // The queued commands as the composer lists them, each one editable or
  // removable before it runs.
  const entries: QueuedEntry[] = bangQueue.map((command, index) => ({
    key: `bang-${index}-${command}`,
    text: `!${command}`,
    onEdit: () => {
      setBangQueue((current) => current.filter((_, other) => other !== index))
      setDraft(`!${command}`)
      textareaRef.current?.focus()
    },
    onRemove: () => setBangQueue((current) => current.filter((_, other) => other !== index)),
  }))

  return { running: bangRunning, runOrQueue, runNext, entries }
}
