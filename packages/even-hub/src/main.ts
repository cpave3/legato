import { waitForEvenAppBridge } from "@evenrealities/even_hub_sdk"
import { startGlance } from "./app"
import { fetchAgents, fetchConversation, sendMessage, transcribe } from "./legato"
import { DEV_SOURCE, loadSource, saveSource } from "./settings"

// A packed build knows the Legato it was made for (its origin only, never a
// token); in the dev server it reads through /legato.
const packed = import.meta.env.VITE_LEGATO_URL
const fallback = packed ? { url: packed } : DEV_SOURCE

const bridge = await waitForEvenAppBridge()
let source = await loadSource(bridge, fallback)

// The phone-side page: which Legato to read, and its web token.
const form = document.querySelector<HTMLFormElement>("#settings")!
const urlInput = form.querySelector<HTMLInputElement>("[name=url]")!
const tokenInput = form.querySelector<HTMLInputElement>("[name=token]")!
const status = document.querySelector<HTMLElement>("#source")!

function showSource() {
  status.textContent =
    source.url === DEV_SOURCE.url ? "Reading Legato through the dev server." : `Reading ${source.url}.`
  urlInput.value = source.url === fallback.url ? "" : source.url
  tokenInput.value = ""
  tokenInput.placeholder = source.token ? "Saved" : ""
}

form.addEventListener("submit", (event) => {
  event.preventDefault()
  // An empty token field keeps the saved token, for the same server only.
  void saveSource(bridge, { url: urlInput.value, token: tokenInput.value || undefined })
    .then(() => loadSource(bridge, fallback))
    .then(
      (saved) => {
        source = saved
        showSource()
      },
      (err: unknown) => {
        status.textContent = err instanceof Error ? err.message : String(err)
      },
    )
})
showSource()

await startGlance(bridge, {
  agents: () => fetchAgents(source),
  conversation: (taskId) => fetchConversation(source, taskId),
  transcribe: (pcm) => transcribe(source, pcm),
  send: (taskId, text) => sendMessage(source, taskId, text),
})
console.log("[legato-even-hub] ready")
