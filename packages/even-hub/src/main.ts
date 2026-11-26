import { waitForEvenAppBridge } from "@evenrealities/even_hub_sdk"
import { startGlance, type LegatoClient } from "./app"
import { answerQuestions, decideApproval, fetchAgents, fetchApprovals, fetchConversation, fetchInstances, PHONE_URL, messageReeve, sendMessage, transcribe, type Phone } from "./legato"
import { loadCode, loadScreenOff, saveCode, saveScreenOff, screenOffMs } from "./settings"

// The app reads Legato through the Legato Android app's glasses link on this
// phone. In the dev server it reads the fake phone instead (dev/legato-dev.ts),
// unless the build was started with VITE_LEGATO_PHONE=real, to sideload it
// onto the glasses.
const fake = import.meta.env.DEV && import.meta.env.VITE_LEGATO_PHONE !== "real"

let fetchFn: typeof fetch = (input, init) => fetch(input, init)
if (import.meta.env.DEV) {
  const { loggedFetch, relayConsole } = await import("./dev-log")
  relayConsole()
  fetchFn = loggedFetch(fetchFn)
}

const bridge = await waitForEvenAppBridge()
const phone: Phone = fake ? { url: "/phone", code: false } : { url: PHONE_URL, code: await loadCode(bridge) }

// The phone-side page: the pairing code from the Legato app.
const form = document.querySelector<HTMLFormElement>("#settings")!
const codeInput = form.querySelector<HTMLInputElement>("[name=code]")!
const status = document.querySelector<HTMLElement>("#status")!

function showPairing() {
  if (fake) status.textContent = "Reading the dev server's fake phone."
  else if (phone.code) status.textContent = "Paired with the Legato app."
  else status.textContent = "Not paired. In the Legato app, open Glasses, switch on the link and copy its code here."
  codeInput.value = ""
  codeInput.placeholder = phone.code ? "Saved" : ""
}

form.addEventListener("submit", (event) => {
  event.preventDefault()
  void saveCode(bridge, codeInput.value).then(
    (saved) => {
      if (!fake) phone.code = saved
      showPairing()
    },
    (err: unknown) => {
      status.textContent = err instanceof Error ? err.message : String(err)
    },
  )
})
showPairing()

// How often to read while the glasses' screen is off, taken at once.
let screenOff = await loadScreenOff(bridge)
const screenOffInput = document.querySelector<HTMLSelectElement>("#screen-off")!
screenOffInput.value = screenOff
screenOffInput.addEventListener("change", () => {
  void saveScreenOff(bridge, screenOffInput.value).then(
    (saved) => (screenOff = saved),
    (err: unknown) => (status.textContent = err instanceof Error ? err.message : String(err)),
  )
})

const legato: LegatoClient = {
  instances: () => fetchInstances(phone, fetchFn),
  agents: (instance) => fetchAgents(phone, instance, fetchFn),
  conversation: (instance, taskId, limit) => fetchConversation(phone, instance, taskId, fetchFn, limit),
  transcribe: (instance, pcm) => transcribe(phone, instance, pcm, fetchFn),
  send: (instance, taskId, text) => sendMessage(phone, instance, taskId, text, fetchFn),
  reeve: (instance, text) => messageReeve(phone, instance, text, fetchFn),
  approvals: (instance) => fetchApprovals(phone, instance, fetchFn),
  decide: (instance, id, decision, option) => decideApproval(phone, instance, id, decision, option, fetchFn),
  answer: (instance, id, answers) => answerQuestions(phone, instance, id, answers, fetchFn),
}

await startGlance(bridge, legato, { screenOffReadMs: () => screenOffMs(screenOff) })
console.log("[legato-even-hub] ready")
