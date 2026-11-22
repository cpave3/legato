import { waitForEvenAppBridge } from "@evenrealities/even_hub_sdk"
import { startGlance, type LegatoClient } from "./app"
import { fetchAgents, fetchConversation, fetchInstances, PHONE_URL, sendMessage, transcribe, type Phone } from "./legato"
import { loadCode, saveCode } from "./settings"

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

// Until the glasses have a page to pick one (LEG-478), they read the first
// instance saved in the Legato app.
let instance: string | undefined
async function firstInstance(): Promise<{ ok: true; id: string } | { ok: false; problem: string }> {
  if (instance) return { ok: true, id: instance }
  const listed = await fetchInstances(phone, fetchFn)
  if (!listed.ok) return listed
  if (!listed.instances.length) return { ok: false, problem: "Add a Legato in the Legato app" }
  instance = listed.instances[0].id
  return { ok: true, id: instance }
}

const legato: LegatoClient = {
  agents: async () => {
    const at = await firstInstance()
    if (!at.ok) return at
    const answer = await fetchAgents(phone, at.id, fetchFn)
    if (!answer.ok && answer.gone) instance = undefined
    return answer
  },
  conversation: (taskId) => fetchConversation(phone, instance ?? "", taskId, fetchFn),
  transcribe: (pcm) => transcribe(phone, instance ?? "", pcm, fetchFn),
  send: (taskId, text) => sendMessage(phone, instance ?? "", taskId, text, fetchFn),
}

await startGlance(bridge, legato)
console.log("[legato-even-hub] ready")
