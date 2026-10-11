declare global {
  interface Window {
    // Present only inside the Legato mobile app's WebView.
    ReactNativeWebView?: { postMessage: (message: string) => void }
  }
}

// The mobile app reopens this interface when the system reclaimed it while the
// app was in the background. What a view keeps only in memory is gone by then,
// so the view names the address that brings it back.
export function tellShellWhereToReturn(path: string): void {
  window.ReactNativeWebView?.postMessage(JSON.stringify({
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
