import { useCallback, type RefObject } from "react"
import { modelSettingsResponse } from "../lib/modelSettings"
import { postAgentJson } from "../lib/nativeAgentApi"
import type { NativeSyncState } from "../lib/nativeAgentSync"
import { effortChangeResult, effortChoices, modelChangeResult, modelChangeStands } from "../lib/nativeSessionSettings"
import { reasonText } from "../lib/nativeAgentView"

export interface SessionSettingsOptions {
  agentId: string
  baseUrl: string
  syncState: NativeSyncState
  syncStateRef: RefObject<NativeSyncState>
  replaceSyncState: (next: NativeSyncState) => void
  // Counts model changes, so an answer to an older one is dropped.
  modelRequestRef: RefObject<number>
  setMode: (mode: string) => void
  closeModes: () => void
}

// useSessionSettings changes how a native session runs from its composer:
// its mode, its model and the model's effort.
export function useSessionSettings({ agentId, baseUrl, syncState, syncStateRef, replaceSyncState, modelRequestRef, setMode, closeModes }: SessionSettingsOptions) {
  const { model } = syncState

  const handleModeChange = useCallback((mode: string) => {
    postAgentJson(baseUrl, agentId, "modes", { mode }).then(() => setMode(mode)).catch(() => {})
    closeModes()
  }, [agentId, baseUrl, setMode, closeModes])

  const handleModelChange = useCallback((ref: string) => {
    if (ref === model) return
    const request = ++modelRequestRef.current
    postAgentJson(baseUrl, agentId, "model", { model: ref, expectedModel: model }).then(modelSettingsResponse).then((body) => {
      if (request !== modelRequestRef.current) return
      if (!modelChangeStands(syncStateRef.current.model, model, ref)) return
      replaceSyncState({ ...syncStateRef.current, ...modelChangeResult(body, ref) })
    }).catch((reason) => {
      if (request !== modelRequestRef.current) return
      replaceSyncState({ ...syncStateRef.current, error: reasonText(reason) })
    })
  }, [agentId, baseUrl, model, replaceSyncState, syncStateRef, modelRequestRef])

  const handleEffortChange = useCallback((value: string) => {
    if (!model) return
    postAgentJson(baseUrl, agentId, "model", { expectedModel: model, settings: { reasoningEffort: value } }).then(modelSettingsResponse).then((body) => {
      const settingsState = effortChangeResult(body)
      if (settingsState) replaceSyncState({ ...syncStateRef.current, model: settingsState.model, settingsState })
    }).catch((reason) => {
      replaceSyncState({ ...syncStateRef.current, error: reasonText(reason) })
    })
  }, [agentId, baseUrl, model, replaceSyncState, syncStateRef])

  return {
    handleModeChange, handleModelChange, handleEffortChange,
    efforts: effortChoices(syncState.settingsState),
    // The effort the model runs at, when the session reports one.
    effort: syncState.settingsState?.effective.reasoningEffort,
  }
}
