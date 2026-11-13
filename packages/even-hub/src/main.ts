import { waitForEvenAppBridge } from "@evenrealities/even_hub_sdk"
import { startGlance } from "./app"
import { fetchAgents, fetchConversation, sendMessage, transcribe } from "./legato"
import { DEV_SOURCE, loadSource, saveSource } from "./settings"

const bridge = await waitForEvenAppBridge()
let source = await loadSource(bridge)

// The phone-side page: which Legato to read, and its web token.
const form = document.querySelector<HTMLFormElement>("#settings")!
const urlInput = form.querySelector<HTMLInputElement>("[name=url]")!
const tokenInput = form.querySelector<HTMLInputElement>("[name=token]")!
const status = document.querySelector<HTMLElement>("#source")!

function showSource() {
  status.textContent =
    source.url === DEV_SOURCE.url ? "Reading Legato through the dev server." : `Reading ${source.url}.`
  urlInput.value = source.url === DEV_SOURCE.url ? "" : source.url
  tokenInput.value = ""
  tokenInput.placeholder = source.token ? "Saved" : ""
}

form.addEventListener("submit", (event) => {
  event.preventDefault()
  const token = tokenInput.value || source.token || ""
  void saveSource(bridge, { url: urlInput.value, token })
    .then(() => loadSource(bridge))
    .then((saved) => {
      source = saved
      showSource()
    })
})
showSource()

await startGlance(bridge, {
  agents: () => fetchAgents(source),
  conversation: (taskId) => fetchConversation(source, taskId),
  transcribe: (pcm) => transcribe(source, pcm),
  send: (taskId, text) => sendMessage(source, taskId, text),
})
console.log("[legato-even-hub] ready")
