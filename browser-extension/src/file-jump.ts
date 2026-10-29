import type { FileReference } from "./file-references"

export type JumpReply = { ok: boolean; status?: "navigating"; partial?: boolean; superseded?: boolean; message?: string }
export type JumpMessage = { type: "legato:jump" } & FileReference

type Effects = {
  sendToTab: (tabId: number, message: JumpMessage) => Promise<JumpReply | undefined>
  inject: (tabId: number) => Promise<void>
}

export const browserJumpEffects: Effects = {
  sendToTab: (tabId, message) => chrome.tabs.sendMessage(tabId, message) as Promise<JumpReply | undefined>,
  inject: async (tabId) => { await chrome.scripting.executeScript({ target: { tabId }, files: ["github.js"] }) },
}

// Asks the content script on the pull request's tab to show a file the agent
// cited. A tab opened before the extension was installed or reloaded has no
// live script, so one is put in and asked again.
export async function showInPullRequest(tabId: number, reference: FileReference, effects: Effects = browserJumpEffects): Promise<JumpReply> {
  const message: JumpMessage = { type: "legato:jump", ...reference }
  try {
    return (await effects.sendToTab(tabId, message)) ?? { ok: false, message: "The pull request page did not answer." }
  } catch (error) {
    const text = error instanceof Error ? error.message : String(error)
    // The page left while working (it went to Files changed): the new page
    // finishes the jump and reports back.
    if (/message (channel|port) closed|port closed/i.test(text)) return { ok: true, status: "navigating", message: "Opening Files changed…" }
    if (!/Receiving end does not exist|Could not establish connection/i.test(text)) throw error
    await effects.inject(tabId)
    return (await effects.sendToTab(tabId, message)) ?? { ok: false, message: "The pull request page did not answer." }
  }
}
