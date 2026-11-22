// Dev builds only (main.ts imports this behind import.meta.env.DEV, so a
// production build leaves it out): the Even app's WebView on Android has no
// DevTools, so what the app logs, and each call it makes to the Legato app,
// is also sent to the dev server's POST /__log, which prints it
// (dev/legato-dev.ts). Never the pairing code: a call is logged by method,
// address and status.
const LOG_PATH = "/__log"

function relay(level: string, args: unknown[]): void {
  const text = args
    .map((a) => (typeof a === "string" ? a : a instanceof Error ? `${a.name}: ${a.message}` : safeJSON(a)))
    .join(" ")
  // The relay's own failures are not logged, or they would loop.
  void fetch(LOG_PATH, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ level, text }),
  }).catch(() => undefined)
}

function safeJSON(value: unknown): string {
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

// Sends console.log, info, warn and error to the dev server too.
export function relayConsole(): void {
  for (const level of ["log", "info", "warn", "error"] as const) {
    const original = console[level].bind(console)
    console[level] = (...args: unknown[]) => {
      original(...args)
      relay(level, args)
    }
  }
}

// A fetch that logs each call: method, address, status and time taken.
export function loggedFetch(fetchFn: typeof fetch = fetch): typeof fetch {
  return async (input, init) => {
    const method = init?.method ?? "GET"
    const url = String(input instanceof Request ? input.url : input)
    const started = Date.now()
    try {
      const response = await fetchFn(input, init)
      console.info(`[phone] ${method} ${url} → ${response.status} (${Date.now() - started} ms)`)
      return response
    } catch (err) {
      console.warn(`[phone] ${method} ${url} → ${err instanceof Error ? err.message : String(err)} (${Date.now() - started} ms)`)
      throw err
    }
  }
}
