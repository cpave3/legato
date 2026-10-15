import { act, fireEvent, render, screen } from "@testing-library/react-native"
import { AppState, Linking, type AppStateStatus } from "react-native"
import { ServerWebView } from "./ServerWebView"

// The page a native WebView would be showing. Scripts the shell injects run
// against it, so a test describes the page's condition, not the shell's messages.
const mockPage = {
  opened: [] as string[],
  localStorage: {} as Record<string, string>,
  // false: the renderer is gone, so nothing the shell injects runs.
  alive: true,
  // false: the page is running but has nothing on screen.
  rendered: true,
  // Events the shell dispatched in the page, for the web app to hear.
  events: [] as string[],
  // What the shell told the page about dictation, in order.
  voice: [] as Record<string, unknown>[],
  // What the shell set up in the page before it loaded.
  shell: undefined as { voice?: number; key?: string } | undefined,
}

const mockRecorder = {
  prepareToRecordAsync: jest.fn(async () => {}),
  record: jest.fn(),
  stop: jest.fn(async () => {}),
  uri: "file:///cache/recording.m4a" as string | null,
  getStatus: () => ({ isRecording: true, metering: -20 }),
}
const mockMicrophone = { granted: true }

jest.mock("expo-audio", () => ({
  useAudioRecorder: () => mockRecorder,
  requestRecordingPermissionsAsync: jest.fn(async () => ({ granted: mockMicrophone.granted })),
  setAudioModeAsync: jest.fn(async () => {}),
}))

jest.mock("expo-file-system/legacy", () => ({
  readAsStringAsync: jest.fn(async () => "QUJD"),
  deleteAsync: jest.fn(async () => {}),
  EncodingType: { Base64: "base64" },
}))

jest.mock("expo-crypto", () => ({ randomUUID: () => "page-key" }))

jest.mock("react-native-webview", () => {
  const React = jest.requireActual("react")
  const { View } = jest.requireActual("react-native")
  function WebView({ ref, ...props }: any) {
    const latest = React.useRef(props)
    latest.current = props
    const run = (script: string) => {
      if (!mockPage.alive) return
      const { onMessage } = latest.current
      const window: Record<string, unknown> = {
        ...(onMessage ? { ReactNativeWebView: { postMessage: (data: string) => onMessage({ nativeEvent: { data } }) } } : {}),
        dispatchEvent: (event: { type: string; detail?: Record<string, unknown> }) => {
          mockPage.events.push(event.type)
          if (event.type === "legato:shell-voice" && event.detail) mockPage.voice.push(event.detail)
        },
      }
      new Function("localStorage", "window", "document", script)(
        { setItem: (key: string, value: string) => { mockPage.localStorage[key] = value } },
        window,
        { readyState: "complete", getElementById: (id: string) => id === "root" ? { childElementCount: mockPage.rendered ? 1 : 0 } : null },
      )
      if (window.legatoShell) mockPage.shell = window.legatoShell as typeof mockPage.shell
    }
    React.useImperativeHandle(ref, () => ({ injectJavaScript: run }), [])
    React.useEffect(() => {
      mockPage.opened.push(props.source.uri)
      run(props.injectedJavaScriptBeforeContentLoaded)
    }, [])
    return <View testID="webview" {...props} />
  }
  return { WebView }
})

const server = "http://desk.local:3080"
const callbacks = { onCanGoBackChange: jest.fn(), onBlockedLink: jest.fn(), onUnavailable: jest.fn() }
const webview = () => screen.getByTestId("webview")
const rendererLost = (url: string) => fireEvent(webview(), "renderProcessGone", { nativeEvent: { url, didCrash: false } })
const navigatedTo = (url: string) => fireEvent(webview(), "navigationStateChange", { url, canGoBack: true })
// What the Legato web app posts to say where a reload should bring it back to.
const pageAsksToReturnTo = (url: string) => fireEvent(webview(), "message", { nativeEvent: { data: JSON.stringify({ legatoShell: "restore", url }) } })
// Long enough for anything the shell is waiting on to run out.
const timePasses = () => act(async () => { jest.runOnlyPendingTimers() })

const appListeners = new Set<(state: AppStateStatus) => void>()
const appBecomes = (state: AppStateStatus) => act(async () => { appListeners.forEach((listener) => listener(state)) })
const personReturns = async () => { await appBecomes("background"); await appBecomes("active") }

beforeEach(() => {
  jest.clearAllMocks()
  jest.useFakeTimers()
  mockPage.opened = []
  mockPage.localStorage = {}
  mockPage.alive = true
  mockPage.rendered = true
  mockPage.events = []
  mockPage.voice = []
  mockPage.shell = undefined
  mockRecorder.uri = "file:///cache/recording.m4a"
  mockMicrophone.granted = true
  appListeners.clear()
  jest.spyOn(AppState, "addEventListener").mockImplementation((_type, listener) => {
    appListeners.add(listener)
    return { remove: () => { appListeners.delete(listener) } }
  })
})

test("opens the server signed in with the saved token", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  expect(mockPage.opened).toEqual([server])
  expect(mockPage.localStorage[`legato:token:${server}`]).toBe("s3cret")
})

test("reopens the page the person was on when the system reclaims it", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await rendererLost(`${server}/board`)

  expect(mockPage.opened).toEqual([server, `${server}/board`])
})

test("reopens the server itself when the lost page was not one of its own", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await rendererLost("https://elsewhere.example/board")

  expect(mockPage.opened).toEqual([server, server])
})

test("waits for the person to return before reopening a page lost in the background", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await appBecomes("background")
  await rendererLost(`${server}/board`)
  expect(mockPage.opened).toEqual([server])

  await appBecomes("active")
  expect(mockPage.opened).toEqual([server, `${server}/board`])
})

test("reopens a page that no longer answers when the person returns", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)
  await navigatedTo(`${server}/review`)
  mockPage.alive = false

  await personReturns()
  await timePasses()

  expect(mockPage.opened).toEqual([server, `${server}/review`])
})

test("leaves a page that answers alone when the person returns", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await personReturns()
  await timePasses()

  expect(mockPage.opened).toEqual([server])
})

test("reopens where the person last went when the lost page is not named", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)
  await navigatedTo(`${server}/plans`)

  await fireEvent(webview(), "renderProcessGone", { nativeEvent: { didCrash: true } })

  expect(mockPage.opened).toEqual([server, `${server}/plans`])
})

test("reopens the page when iOS ends its process", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await fireEvent(webview(), "contentProcessDidTerminate", { nativeEvent: { url: `${server}/board` } })

  expect(mockPage.opened).toEqual([server, `${server}/board`])
})

test("reopens a page that came back with nothing on screen", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)
  await navigatedTo(`${server}/board`)
  mockPage.rendered = false

  await personReturns()

  expect(mockPage.opened).toEqual([server, `${server}/board`])
})

test("reopens the view the page asked to come back to", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)
  await navigatedTo(`${server}/agents`)
  await pageAsksToReturnTo(`${server}/agents?task=abc123`)

  await rendererLost(`${server}/agents`)

  expect(mockPage.opened).toEqual([server, `${server}/agents?task=abc123`])
})

test("forgets what the page asked for once the person has gone elsewhere", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)
  await navigatedTo(`${server}/agents`)
  await pageAsksToReturnTo(`${server}/agents?task=abc123`)
  await navigatedTo(`${server}/board`)

  await rendererLost(`${server}/board`)

  expect(mockPage.opened).toEqual([server, `${server}/board`])
})

test("gives up on a page that is lost again as soon as it is reopened", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await rendererLost(`${server}/board`)
  await rendererLost(`${server}/board`)

  expect(mockPage.opened).toEqual([server, `${server}/board`])
  expect(callbacks.onUnavailable).toHaveBeenCalledTimes(1)
})

test("reopens a page lost again after it had been running for a while", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await rendererLost(`${server}/board`)
  await act(async () => { jest.advanceTimersByTime(5 * 60 * 1000) })
  await rendererLost(`${server}/board`)

  expect(mockPage.opened).toEqual([server, `${server}/board`, `${server}/board`])
  expect(callbacks.onUnavailable).not.toHaveBeenCalled()
})

test("reopens once when the loss is reported while the page is being asked", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)
  mockPage.alive = false

  await personReturns()
  await rendererLost(`${server}/board`)
  await timePasses()

  expect(mockPage.opened).toEqual([server, `${server}/board`])
})

test("reopens a page lost again while the person was away, however soon", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)
  await rendererLost(`${server}/board`)

  await personReturns()
  await rendererLost(`${server}/board`)

  expect(mockPage.opened).toEqual([server, `${server}/board`, `${server}/board`])
  expect(callbacks.onUnavailable).not.toHaveBeenCalled()
})

test("keeps the page on its server and hands other links to the system", async () => {
  jest.spyOn(Linking, "openURL").mockResolvedValue(true)
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  expect(await fireEvent(webview(), "shouldStartLoadWithRequest", { url: `${server}/board` })).toBe(true)
  expect(await fireEvent(webview(), "shouldStartLoadWithRequest", { url: "https://example.com/docs" })).toBe(false)
  expect(Linking.openURL).toHaveBeenCalledWith("https://example.com/docs")
})

test("says why a page that failed to load is unavailable", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await fireEvent(webview(), "error", { nativeEvent: { description: "net::ERR_CONNECTION_REFUSED" } })

  expect(callbacks.onUnavailable).toHaveBeenCalledWith("net::ERR_CONNECTION_REFUSED")
  expect(mockPage.opened).toEqual([server])
})

test("does not judge a page while the app is in the background", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)
  mockPage.alive = false

  await personReturns()
  await appBecomes("background")
  await timePasses()
  expect(mockPage.opened).toEqual([server])

  await appBecomes("active")
  await timePasses()
  expect(mockPage.opened).toEqual([server, server])
})

test("tells a page that is still there that the person has returned", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)
  expect(mockPage.events).toEqual([])

  await personReturns()

  expect(mockPage.events).toEqual(["legato:shell-return"])
})

test("says nothing to a page it is about to reopen", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await appBecomes("background")
  await rendererLost(`${server}/agents`)
  await appBecomes("active")

  expect(mockPage.opened).toEqual([server, `${server}/agents`])
  expect(mockPage.events).toEqual([])
})

// What the Legato web app posts to the shell about dictation.
const pageAsksToRecord = (action: "start" | "stop" | "cancel", key = "page-key") =>
  act(async () => { fireEvent(webview(), "message", { nativeEvent: { data: JSON.stringify({ legatoShell: "voice", key, action }) } }) })

test("tells the page it can record, with a key only the page has", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  expect(mockPage.shell).toEqual({ voice: 1, key: "page-key" })
})

test("records when the page asks, and hands it the recording", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await pageAsksToRecord("start")
  expect(mockRecorder.record).toHaveBeenCalled()
  expect(mockPage.voice).toContainEqual({ type: "started" })

  await pageAsksToRecord("stop")
  expect(mockRecorder.stop).toHaveBeenCalled()
  expect(mockPage.voice).toContainEqual({ type: "audio", data: "QUJD", mime: "audio/mp4" })
})

test("tells the page how loud the microphone is while it records", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await pageAsksToRecord("start")
  await act(async () => { jest.advanceTimersByTime(300) })

  expect(mockPage.voice.some((event) => event.type === "level" && typeof event.level === "number" && event.level > 0)).toBe(true)
})

test("throws a recording away when the page cancels it", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await pageAsksToRecord("start")
  await pageAsksToRecord("cancel")

  expect(mockRecorder.stop).toHaveBeenCalled()
  expect(mockPage.voice.some((event) => event.type === "audio")).toBe(false)
})

test("does not record for a frame that lacks the page's key", async () => {
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await pageAsksToRecord("start", "guessed")

  expect(mockRecorder.record).not.toHaveBeenCalled()
  expect(mockPage.voice).toEqual([])
})

test("says so when the microphone is refused", async () => {
  mockMicrophone.granted = false
  await render(<ServerWebView baseUrl={server} token="s3cret" {...callbacks} />)

  await pageAsksToRecord("start")

  expect(mockRecorder.record).not.toHaveBeenCalled()
  expect(mockPage.voice).toContainEqual({ type: "error", message: "Legato may not use the microphone. Allow it in the app's settings." })
})
