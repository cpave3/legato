import type { ReviewPanelModel } from "./review-model"

type Effects = {
  load: (tabId: number, selectedRunId?: string) => Promise<ReviewPanelModel>
  render: (model: ReviewPanelModel) => void
  showError: (error: unknown) => void
  schedule: (callback: () => void) => void
  cancel: () => void
}

export function createReviewPanelController(effects: Effects) {
  let tabId: number | undefined
  let selectedRunId: string | undefined
  let generation = 0

  const refresh = async (): Promise<void> => {
    if (tabId === undefined) return
    const requestGeneration = ++generation
    const requestTabId = tabId
    const requestRunId = selectedRunId
    effects.cancel()
    try {
      const model = await effects.load(requestTabId, requestRunId)
      if (requestGeneration !== generation || requestTabId !== tabId || requestRunId !== selectedRunId) return
      selectedRunId = model.selectedRunId
      effects.render(model)
      if (model.poll) effects.schedule(() => { void refresh() })
    } catch (error) {
      if (requestGeneration === generation && requestTabId === tabId && requestRunId === selectedRunId) effects.showError(error)
    }
  }

  const followTab = async (nextTabId: number | undefined): Promise<void> => {
    generation++
    tabId = nextTabId
    selectedRunId = undefined
    effects.cancel()
    await refresh()
  }

  return {
    refresh,
    followTab,
    selectRun: async (runId: string) => { selectedRunId = runId; await refresh() },
    contextUpdated: async (updatedTabId: number) => { if (updatedTabId === tabId) await followTab(tabId) },
    currentTab: () => tabId,
  }
}
