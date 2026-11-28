import { useRef, forwardRef } from "react"
import type { PromptState } from "../lib/agentWire"
import { useServer } from "../hooks/useServer"
import { usePastedImages } from "../hooks/usePastedImages"
import { insertImagePath } from "../lib/agentImage"
import { PastedImageStrip } from "./PastedImageStrip"
import { cn } from "../lib/utils"
import type { SandboxSwitch } from "../lib/agentReload"
import { useVoiceRecorder } from "../hooks/useVoiceRecorder"
import { DictationBar, WorkingStrip } from "./Composer"
import { dictating } from "../lib/composer"
import type { SidePanelsProps } from "./AgentChatHeader"
import { useTerminalMenu } from "../hooks/useTerminalMenu"
import { askingPrompt, isBashCommand } from "../lib/promptBar"
import { ActionList, BarHeader, ComposerFooter, ComposerTools, MessageBox } from "./PromptBarParts"
import { useMacroMenu, useMessageKeys, usePromptBarHandle, usePromptDraft, type PromptBarHandle } from "./usePromptBar"

interface PromptBarProps {
  promptState: PromptState | null
  onSendKeys: (keys: string) => void
  // Returns false when the message did not go out, so it stays in the box.
  onSubmitMessage: (content: string) => boolean | void
  onDismissPrompt: () => void
  onDetectPrompt: () => void
  onDisconnect: () => void
  onKill: () => void
  // Stops the agent and starts it again, resuming its conversation.
  onReload?: () => void
  // Reloads it on the other side of the sandbox, where there is one.
  switchSandbox?: SandboxSwitch
  // Reloads it with or without the host network for its containers.
  switchHostNetwork?: SandboxSwitch
  // Opens the dialog that shows what Legato knows about the agent.
  onShowInfo?: () => void
  onRefresh: () => void
  onTogglePromptDetection: () => void
  promptDetectionEnabled: boolean
  agentId: string
  agentTitle?: string
  agentActivity?: string
  agentCommand?: string
  agentKind?: string
  // The engram the agent runs as, shown beside its tool.
  engram?: string
  connected?: boolean
  ntfyConfigured?: boolean
  notifyEnabled?: boolean
  onToggleNotify?: () => void
  voiceEnabled?: boolean
  onUpgradeChimera?: () => Promise<void>
  // What the column beside the session can show, when the bar is its only
  // toolbar.
  panels?: SidePanelsProps
  // The page's header above the terminal already names the agent and holds
  // its side panels and More actions menu (useTerminalMenu); the bar keeps
  // only the message box and the controls under it.
  menuInHeader?: boolean
}

export type { PromptBarHandle } from "./usePromptBar"

export const PromptBar = forwardRef<PromptBarHandle, PromptBarProps>(function PromptBar({ promptState, onSendKeys, onSubmitMessage, onDismissPrompt, onDetectPrompt, onDisconnect, onKill, onReload, switchSandbox, switchHostNetwork, onShowInfo, onRefresh, onTogglePromptDetection, promptDetectionEnabled, agentId, agentTitle, agentActivity, agentCommand, agentKind, engram, connected, ntfyConfigured, notifyEnabled, onToggleNotify, voiceEnabled, onUpgradeChimera, panels, menuInHeader = false }, ref) {
  const { baseUrl } = useServer()
  const terminalMenu = useTerminalMenu({ agentKind, onUpgradeChimera, onShowInfo, onRefresh, onDetectPrompt, onTogglePromptDetection, promptDetectionEnabled, ntfyConfigured, notifyEnabled, onToggleNotify, onReload, switchSandbox, switchHostNetwork, onDisconnect, onKill })
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const { input, inputRef, change, clear } = usePromptDraft(agentId, textareaRef)
  const { ref: macroRef, ...macroMenu } = useMacroMenu(baseUrl, onSubmitMessage)
  // Dictation: the server sends what was said to the agent, or types it into
  // the pane, as the voice settings say.
  const voice = useVoiceRecorder({ agentId, agentKind, baseUrl })

  usePromptBarHandle(ref, textareaRef, voice)

  // An image pasted into the message box is stored by Legato, and its path
  // goes into the message where the cursor is.
  const pasted = usePastedImages(agentId, input, (path) => {
    change(insertImagePath(inputRef.current, path, textareaRef.current?.selectionStart))
  })

  const { submit, onKeyDown } = useMessageKeys({ input, onSubmitMessage, clear, onSendKeys })

  const prompt = askingPrompt(agentActivity, promptState)
  const asking = prompt !== null
  // Claude-specific: "! " prefix runs a bash command.
  const bash = isBashCommand(agentKind || agentCommand, input)

  return (
    <div className="bg-lw-ground px-3 pb-3 font-lw text-[13px] text-lw-text md:px-6 md:pb-[18px]">
      {!menuInHeader && <BarHeader agentKind={agentKind} engram={engram} agentTitle={agentTitle} panels={panels} terminalMenu={terminalMenu} />}

      {/* An image dropped anywhere on the composer joins the message. */}
      <div {...pasted.dropTarget} className={cn("mx-auto flex w-full max-w-[820px] flex-col rounded-[10px] border bg-lw-surface max-sm:rounded-xl", composerBorder(dictating(voice), pasted.dragging), menuInHeader && "mt-3")}>
        {agentActivity === "working" && !asking && <WorkingStrip onInterrupt={() => onSendKeys("Escape")} />}
        {prompt ? (
          <div className="p-2.5">
            <ActionList
              actions={prompt.actions}
              type={prompt.type}
              onSelect={(keys) => { onSendKeys(keys); onDismissPrompt() }}
              onDismiss={onDismissPrompt}
            />
          </div>
        ) : (
          <>
            <PastedImageStrip pasted={pasted} />
            <MessageBox textareaRef={textareaRef} input={input} bash={bash} connected={connected} onChange={change} onKeyDown={onKeyDown} onPaste={pasted.onPaste} />
          </>
        )}

        {dictating(voice) ? <DictationBar recorder={voice} /> : (
          <ComposerFooter asking={asking} connected={connected} canSend={input.trim() !== ""} voiceEnabled={voiceEnabled} voice={voice} onSubmit={submit}>
            <ComposerTools asking={asking} connected={connected} onAttach={pasted.attach} onSendKeys={onSendKeys} macroRef={macroRef} macroMenu={macroMenu} />
          </ComposerFooter>
        )}
      </div>
    </div>
  )
})

// The composer's border: red while dictating, the accent while an image is
// dragged over it.
function composerBorder(dictatingNow: boolean, dragging: boolean): string {
  if (dictatingNow) return "border-lw-danger-line"
  return dragging ? "border-lw-accent" : "border-lw-line-strong"
}
