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

type ShellObject = { approval?: number; enroll?: number; key?: string }

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
