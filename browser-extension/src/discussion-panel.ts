// Decides which discussion the side panel follows: the one running for the
// pull request in the panel's tab. While there is none (it may still be
// starting) it looks again every few seconds.
export type DiscussionRef = { runId: string; taskId: string; agentUrl: string }
export type DiscussionPanelView =
  | { kind: "looking" }
  | { kind: "none" }
  | { kind: "error"; message: string }
  | { kind: "discussion"; ref: DiscussionRef }

type Session = { stop: () => void }

type Effects = {
  find: (tabId: number) => Promise<DiscussionRef | null>
  startSession: (ref: DiscussionRef) => Session
  show: (view: DiscussionPanelView) => void
  schedule: (callback: () => void) => number
  cancel: (id: number) => void
}

const lookAgainMs = 3_000

export function createDiscussionPanel(effects: Effects) {
  let tabId: number | undefined
  let current: { ref: DiscussionRef; session: Session } | undefined
  let timer: number | undefined
  let generation = 0

  const stopSession = (): void => {
    current?.session.stop()
    current = undefined
  }
  const stopLooking = (): void => {
    if (timer !== undefined) effects.cancel(timer)
    timer = undefined
  }

  const look = async (): Promise<void> => {
    if (tabId === undefined) return
    const mine = ++generation
    const lookingFor = tabId
    let ref: DiscussionRef | null
    try {
      ref = await effects.find(lookingFor)
    } catch (error) {
      if (mine !== generation) return
      stopSession()
      effects.show({ kind: "error", message: error instanceof Error ? error.message : String(error) })
      return
    }
    if (mine !== generation) return
    if (!ref) {
      stopSession()
      effects.show({ kind: "none" })
      timer = effects.schedule(() => { timer = undefined; void look() })
      return
    }
    if (current?.ref.taskId !== ref.taskId) {
      stopSession()
      current = { ref, session: effects.startSession(ref) }
    }
    effects.show({ kind: "discussion", ref })
  }

  return {
    follow: async (nextTabId: number | undefined): Promise<void> => {
      stopLooking()
      if (nextTabId !== tabId) effects.show({ kind: "looking" })
      tabId = nextTabId
      await look()
    },
    hide: (): void => {
      generation++
      stopLooking()
      stopSession()
    },
  }
}

export { lookAgainMs }
