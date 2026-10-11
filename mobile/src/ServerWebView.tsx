import { useCallback, useEffect, useImperativeHandle, useRef, useState, type Ref } from "react"
import { AppState, Linking, StyleSheet } from "react-native"
import { WebView } from "react-native-webview"
import type { WebViewNavigation } from "react-native-webview"
import { allowNavigation, bootstrapScript } from "./compat"

// How long a page gets to answer after the app returns to the foreground.
const ANSWER_TIMEOUT_MS = 4000
// A page lost again this soon after being reopened, with the app in front the
// whole time, would only keep being lost.
const LOST_AGAIN_MS = 30000

// Runs in the page when the app returns to the foreground. The page answers
// through the WebView's message channel; a loaded page whose root element is
// empty has lost its interface. The event tells the web app the person is
// back, so it can catch up on what it missed.
const returnScript = (id: string) =>
  `(function(){try{var root=document.getElementById("root");window.ReactNativeWebView.postMessage(JSON.stringify({legatoShell:"answer",id:${JSON.stringify(id)},rendered:!root||root.childElementCount>0||document.readyState!=="complete"}));window.dispatchEvent(new Event("legato:shell-return"))}catch(e){}})();true;`

// Anything in the page can post a message, so nothing about one is assumed.
function readMessage(data: string): { legatoShell?: unknown; id?: unknown; rendered?: unknown; url?: unknown } {
  try {
    const message: unknown = JSON.parse(data)
    return typeof message === "object" && message !== null ? message : {}
  } catch { return {} }
}

function samePage(one: string, other: string): boolean {
  try {
    const a = new URL(one), b = new URL(other)
    return a.origin === b.origin && a.pathname === b.pathname
  } catch { return false }
}

export type ServerWebViewHandle = { reload: () => void; goBack: () => void }

type Props = {
  ref?: Ref<ServerWebViewHandle>
  baseUrl: string
  token: string
  onCanGoBackChange: (canGoBack: boolean) => void
  onBlockedLink: (url: string) => void
  onUnavailable: (reason: string) => void
}

// The open server's page. Android reclaims a backgrounded WebView's renderer
// while the app itself lives on, which leaves a blank view; this notices, on
// the system's report or by asking the page when the app returns, and reopens
// the page where the person was.
export function ServerWebView({ ref, baseUrl, token, onCanGoBackChange, onBlockedLink, onUnavailable }: Props) {
  const webRef = useRef<WebView>(null)
  // A WebView whose renderer is gone cannot be reused, so each recovery mounts
  // a new one in its place.
  const [page, setPage] = useState({ generation: 0, uri: baseUrl })
  const lastUrl = useRef(baseUrl)
  // Where the page asked to be brought back to: what it shows can depend on
  // more than its address, and it is the one that knows.
  const returnTo = useRef<string | undefined>(undefined)
  const reopenedAt = useRef(0)
  const awaitingAnswer = useRef<{ id: string; timeout: ReturnType<typeof setTimeout> } | undefined>(undefined)
  const backgrounded = useRef(false)
  const lostInBackground = useRef<string | undefined>(undefined)

  const stopWaiting = () => {
    clearTimeout(awaitingAnswer.current?.timeout)
    awaitingAnswer.current = undefined
  }
  const reopen = useCallback((url: string) => {
    stopWaiting()
    reopenedAt.current = Date.now()
    const target = returnTo.current && samePage(returnTo.current, url) ? returnTo.current : url
    setPage((current) => ({ generation: current.generation + 1, uri: allowNavigation(baseUrl, target) ? target : baseUrl }))
  }, [baseUrl])

  const rendererLost = (url?: string) => {
    const lost = url || lastUrl.current
    // A page opened while nobody is looking would only be reclaimed again, so
    // one lost in the background waits for the person to return.
    if (backgrounded.current) lostInBackground.current = lost
    else if (Date.now() - reopenedAt.current < LOST_AGAIN_MS) onUnavailable("This server's page keeps stopping on this device. Open it again to retry.")
    else reopen(lost)
  }

  useEffect(() => {
    const subscription = AppState.addEventListener("change", (state) => {
      // A page cannot be expected to answer in the background; it is asked
      // again on the next return.
      if (state === "background") { backgrounded.current = true; reopenedAt.current = 0; stopWaiting(); return }
      if (state !== "active" || !backgrounded.current) return
      backgrounded.current = false
      const lost = lostInBackground.current
      lostInBackground.current = undefined
      if (lost !== undefined) { reopen(lost); return }
      // Not every lost page is reported, so ask this one whether it is still there.
      const id = `${Date.now()}-${Math.random()}`
      awaitingAnswer.current = { id, timeout: setTimeout(() => reopen(lastUrl.current), ANSWER_TIMEOUT_MS) }
      webRef.current?.injectJavaScript(returnScript(id))
    })
    return () => { subscription.remove(); stopWaiting() }
  }, [reopen])

  useImperativeHandle(ref, () => ({ reload: () => webRef.current?.reload(), goBack: () => webRef.current?.goBack() }), [])

  return <WebView ref={webRef} key={page.generation} style={styles.webView} source={{ uri: page.uri }} injectedJavaScriptBeforeContentLoaded={bootstrapScript(baseUrl, token)}
    mixedContentMode="never" overScrollMode="never" onNavigationStateChange={(state: WebViewNavigation) => { lastUrl.current = state.url; onCanGoBackChange(state.canGoBack) }}
    onShouldStartLoadWithRequest={({ url }) => { if (allowNavigation(baseUrl, url)) return true; Linking.openURL(url).catch(() => onBlockedLink(url)); return false }}
    // Android's event names the lost page, though its type does not say so.
    onRenderProcessGone={({ nativeEvent }) => rendererLost((nativeEvent as { url?: string }).url)}
    onContentProcessDidTerminate={({ nativeEvent }) => rendererLost(nativeEvent.url)}
    onMessage={({ nativeEvent }) => {
      const message = readMessage(nativeEvent.data)
      if (message.legatoShell === "restore" && typeof message.url === "string") returnTo.current = message.url
      if (message.legatoShell !== "answer" || message.id !== awaitingAnswer.current?.id) return
      stopWaiting()
      if (message.rendered === false) reopen(lastUrl.current)
    }}
    onError={({ nativeEvent }) => onUnavailable(nativeEvent.description)} />
}

const styles = StyleSheet.create({
  webView: { flex: 1, backgroundColor: "#0a0a0f" },
})
