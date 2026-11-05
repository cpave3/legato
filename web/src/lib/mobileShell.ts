declare global {
  interface Window {
    // Present only inside the Legato mobile app's WebView. Every version of the
    // app exposes the object; only those that listen for messages put
    // postMessage on it.
    ReactNativeWebView?: { postMessage?: (message: string) => void }
  }
}

// The mobile app reopens this interface when the system reclaimed it while the
// app was in the background. What a view keeps only in memory is gone by then,
// so the view names the address that brings it back.
export function tellShellWhereToReturn(path: string): void {
  window.ReactNativeWebView?.postMessage?.(JSON.stringify({
    legatoShell: "restore",
    url: new URL(path, window.location.origin).toString(),
  }))
}

// The mobile app dispatches this in the page each time it comes back to the
// foreground and finds the page still there.
export function onShellReturn(handler: () => void): () => void {
  window.addEventListener("legato:shell-return", handler)
  return () => window.removeEventListener("legato:shell-return", handler)
}

// The mobile app dispatches this in the page when it goes to the background,
// just before it pauses the page.
export function onShellLeave(handler: () => void): () => void {
  window.addEventListener("legato:shell-leave", handler)
  return () => window.removeEventListener("legato:shell-leave", handler)
}

type ShellObject = { approval?: number; enroll?: number; workers?: number; key?: string }

function shell(): { object: ShellObject; post: (message: string) => void } | null {
  const object = (window as { legatoShell?: ShellObject }).legatoShell
  const post = window.ReactNativeWebView?.postMessage
  if (!object?.key || !post) return null
  return { object, post: (message) => post.call(window.ReactNativeWebView, message) }
}

/**
 * The Legato app, when it can sign approval decisions with this phone's
 * key: open its native sheet on an approval, which signs with the
 * fingerprint or PIN. The app reads only the ID; it fetches the request
 * itself. null outside the app, or in an app too old to sign.
 */
export function shellApprovals(): { open: (approvalId: string) => void; enroll: () => void } | null {
  const s = shell()
  if (!s?.object.approval) return null
  const key = s.object.key
  return {
    open: (id) => s.post(JSON.stringify({ legatoShell: "approval", key, id })),
    enroll: () => s.post(JSON.stringify({ legatoShell: "enroll", key })),
  }
}

/** What the app's approval sheet came to. */
export type ShellApprovalResult = { id: string; status: "decided" | "cancelled" | "error"; option?: string; error?: string }

/** The app dispatches legato:shell-approval when its sheet closes. */
export function onShellApproval(handler: (result: ShellApprovalResult) => void): () => void {
  const listener = (event: Event) => handler((event as CustomEvent<ShellApprovalResult>).detail)
  window.addEventListener("legato:shell-approval", listener)
  return () => window.removeEventListener("legato:shell-approval", listener)
}

/** The app dispatches legato:shell-enroll when it has enrolled (or not). */
export function onShellEnroll(handler: (result: { status: "enrolled" | "pending" | "cancelled" | "error"; error?: string }) => void): () => void {
  const listener = (event: Event) => handler((event as CustomEvent).detail)
  window.addEventListener("legato:shell-enroll", listener)
  return () => window.removeEventListener("legato:shell-enroll", listener)
}

/** A server the Legato app knows, which the page may offer as a worker. key is the app's own ID for it. */
export type ShellServer = { key: string; name: string; url: string }

/** What the app answers a worker message with: legato:shell-worker, its detail carrying the message's id. */
type ShellWorkerAnswer = {
  id: string
  servers?: ShellServer[]
  address?: string
  phrase?: string | string[]
  expires_at?: string
  cancelled?: boolean
  error?: string
}

// How long the app has to list its servers. Opening a request waits for the
// person, so it has no limit.
const CANDIDATES_WAIT_MS = 10_000

let workerMessages = 0

/**
 * The Legato app, when it can pair one of its other servers as a worker of
 * the server in front (window.legatoShell.workers === 1). Only the app holds
 * those servers' tokens: it lists them, and opens a main-purpose pairing
 * request on one after its own confirm, outside the page. null outside the
 * app, or in an app too old to.
 *
 * Each message carries the type both as legatoShell, which is how the app
 * reads every message, and as type, as the bridge contract names it.
 */
export function shellWorkers(): {
  candidates: () => Promise<ShellServer[]>
  openPairing: (server: string, main: string) => Promise<{ address: string; phrase: string; expires_at?: string } | null>
} | null {
  const s = shell()
  if (s?.object.workers !== 1) return null
  const key = s.object.key
  const ask = (type: string, rest: Record<string, string>, waitMs?: number) => new Promise<ShellWorkerAnswer>((resolve, reject) => {
    const id = `w${Date.now().toString(36)}-${++workerMessages}`
    let timer: ReturnType<typeof setTimeout> | undefined
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<ShellWorkerAnswer>).detail
      if (!detail || detail.id !== id) return
      window.removeEventListener("legato:shell-worker", listener)
      if (timer) clearTimeout(timer)
      resolve(detail)
    }
    window.addEventListener("legato:shell-worker", listener)
    if (waitMs) {
      timer = setTimeout(() => {
        window.removeEventListener("legato:shell-worker", listener)
        reject(new Error("The app did not answer."))
      }, waitMs)
    }
    s.post(JSON.stringify({ legatoShell: type, type, key, id, ...rest }))
  })
  return {
    candidates: async () => {
      const answer = await ask("workerCandidates", {}, CANDIDATES_WAIT_MS)
      if (answer.error) throw new Error(answer.error)
      return (answer.servers ?? []).filter((server) => server && typeof server.key === "string" && typeof server.url === "string")
    },
    openPairing: async (server, main) => {
      const answer = await ask("openWorkerPairing", { server, main })
      if (answer.cancelled) return null
      if (answer.error) throw new Error(answer.error)
      if (!answer.address || !answer.phrase) throw new Error("The app opened no pairing request.")
      const phrase = Array.isArray(answer.phrase) ? answer.phrase.join(" ") : answer.phrase
      return { address: answer.address, phrase, expires_at: answer.expires_at }
    },
  }
}
