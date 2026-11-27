import { MemoryRouter } from "react-router-dom"
import { QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { UsageStrip } from "../UsageStrip"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

// Reset times are shown in the browser's zone; these tests pin it.
const zone = process.env.TZ
beforeAll(() => { process.env.TZ = "UTC" })
afterAll(() => { process.env.TZ = zone })

// A Thursday, 08:00 UTC.
const NOW = new Date("2026-11-26T08:00:00Z")

const catalog = [
  { provider: "claude", name: "Claude", meters: [
    { window: "five_hour", qualified: "claude.five_hour", short: "5h", long: "5-hour" },
    { window: "weekly", qualified: "claude.weekly", short: "wk", long: "weekly" },
    { window: "weekly_opus", qualified: "claude.weekly_opus", short: "Opus", long: "weekly Opus" },
    { window: "weekly_sonnet", qualified: "claude.weekly_sonnet", short: "Sonnet", long: "weekly Sonnet" },
  ] },
  { provider: "codex", name: "Codex", meters: [
    { window: "five_hour", qualified: "codex.five_hour", short: "5h", long: "5-hour" },
    { window: "weekly", qualified: "codex.weekly", short: "wk", long: "weekly" },
  ] },
  { provider: "opencode", name: "OpenCode Go", meters: [
    { window: "five_hour", qualified: "opencode.five_hour", short: "5h", long: "5-hour" },
    { window: "weekly", qualified: "opencode.weekly", short: "wk", long: "weekly" },
    { window: "monthly", qualified: "opencode.monthly", short: "mo", long: "monthly" },
  ] },
]

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

let usage: unknown
let posted: string[]

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] })
  vi.setSystemTime(NOW)
  usage = {
    poll_interval_seconds: 300,
    catalog,
    providers: [
      {
        provider: "claude", state: "ok", stale: false, plan: "max", source: "endpoint",
        as_of: "2026-11-26T07:58:00Z",
        windows: [
          { meter: "five_hour", used_percent: 42, resets_at: "2026-11-26T10:00:00Z" },
          { meter: "weekly", used_percent: 91.4, resets_at: "2026-12-01T09:00:00Z" },
        ],
      },
      {
        provider: "codex", state: "stale", stale: true, reason: "unreachable", source: "logs",
        as_of: "2026-11-26T05:00:00Z",
        windows: [{ meter: "weekly", used_percent: 5, resets_at: "2026-11-29T00:00:00Z", window_minutes: 10080 }],
      },
    ],
  }
  posted = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "POST") {
      posted.push(url)
      return json({ status: "refreshing" }, 202)
    }
    if (url === "/api/usage") return json(usage)
    return json({}, 404)
  }))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function renderIt(path = "/agents", props: { sheet?: boolean; compact?: boolean } = {}) {
  render(<QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={[path]}><UsageStrip {...props} /></MemoryRouter></QueryClientProvider>)
}

const card = () => screen.getByRole("dialog", { name: "Subscription usage" })
const row = (strip: HTMLElement, provider: string) => strip.querySelector<HTMLElement>(`[data-provider="${provider}"]`)!

describe("the collapsed card", () => {
  it("shows each provider's tightest window, with a bar for every window", async () => {
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(strip.getAttribute("aria-expanded")).toBe("false")
    expect(strip.getAttribute("aria-haspopup")).toBe("dialog")
    const claude = row(strip, "claude")
    expect(within(claude).getByText("Claude")).toBeTruthy()
    expect(within(claude).getByText("91%")).toBeTruthy()
    expect(within(claude).getByText("wk")).toBeTruthy()
    expect(claude.querySelectorAll("[data-meter]")).toHaveLength(2)
    const codex = row(strip, "codex")
    expect(within(codex).getByText("5%")).toBeTruthy()
    expect(codex.querySelectorAll("[data-meter]")).toHaveLength(1)
  })

  it("says a reading is stale in words, not only in grey", async () => {
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(within(strip).getByText("stale 3h")).toBeTruthy()
    expect(strip.getAttribute("aria-label")).toMatch(/Codex weekly 5%, stale/)
  })

  it("names a window at its limit in its label", async () => {
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(strip.getAttribute("aria-label")).toMatch(/Claude weekly 91%, at limit/)
  })

  it("shows how fresh the newest reading is when none is stale", async () => {
    usage = { ...(usage as Record<string, unknown>), providers: [(usage as { providers: unknown[] }).providers[0]] }
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(within(strip).getByText("2m")).toBeTruthy()
  })

  it("keeps four accounts and names the rest", async () => {
    const account = (provider: string, percent: number) => ({ provider, state: "ok", stale: false, as_of: "2026-11-26T07:58:00Z", windows: [{ meter: "weekly", used_percent: percent }] })
    usage = { poll_interval_seconds: 300, providers: [account("a", 10), account("b", 90), account("c", 50), account("d", 5), account("e", 75), account("f", 61)] }
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(strip.querySelectorAll("[data-provider]")).toHaveLength(4)
    expect(within(strip).getByText("+2 more: a, d")).toBeTruthy()
  })

  it("says Auto, or Pinned, beside the active profile", async () => {
    usage = { ...(usage as Record<string, unknown>), policy: {
      active: { profile: { name: "Economy", agent: "codex" }, reason: { text: "Fallback" }, since: NOW.toISOString() },
      profiles: [{ name: "Economy", agent: "codex" }], switches: [],
    } }
    renderIt()
    let strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(within(strip).getByText("Economy")).toBeTruthy()
    expect(within(strip).getByText("Auto")).toBeTruthy()
    cleanup()
    usage = { ...(usage as Record<string, unknown>), policy: {
      active: { profile: { name: "Economy", agent: "codex" }, reason: { text: "Pinned" }, since: NOW.toISOString(), pin: { profile: "Economy" } },
      profiles: [{ name: "Economy", agent: "codex" }], switches: [],
    } }
    renderIt()
    strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(within(strip).getByText("Pinned")).toBeTruthy()
  })

  it("says a failed profile switch on the card", async () => {
    usage = { ...(usage as Record<string, unknown>), policy: {
      active: { profile: { name: "Economy", agent: "codex" }, reason: { text: "Fallback" }, since: NOW.toISOString(), error: "codex is not installed" },
      profiles: [{ name: "Economy", agent: "codex" }], switches: [],
    } }
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(within(strip).getByText("Switch failed")).toBeTruthy()
  })

  it("shows nothing when no provider is read", async () => {
    usage = { poll_interval_seconds: 0, providers: [] }
    renderIt()
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
    expect(screen.queryByRole("button", { name: /subscription usage/i })).toBeNull()
  })

  it("shows nothing on a server without usage", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ error: "not found" }, 404)))
    renderIt()
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
    expect(screen.queryByRole("button", { name: /subscription usage/i })).toBeNull()
  })

  it("surfaces usage read errors and can retry", async () => {
    let failed = true
    vi.stubGlobal("fetch", vi.fn(async () => failed ? json({ error: "Policy cache unavailable" }, 500) : json(usage)))
    renderIt()
    expect((await screen.findByRole("alert")).textContent).toContain("Policy cache unavailable")
    failed = false
    fireEvent.click(screen.getByRole("button", { name: "Retry usage" }))
    expect(await screen.findByRole("button", { name: /subscription usage/i })).toBeTruthy()
  })
})

describe("the compact button", () => {
  const button = () => screen.findByRole("button", { name: /^Subscription usage/ })

  it("is one button with the whole summary as its name, and no account lines", async () => {
    renderIt("/agents", { compact: true })
    const trigger = await button()
    expect(trigger.getAttribute("aria-label")).toBe(
      "Subscription usage. Claude 5-hour 42%; Claude weekly 91%, at limit; Codex weekly 5%, stale",
    )
    expect(trigger.getAttribute("title")).toBe("Usage")
    expect(document.querySelector("[data-provider]")).toBeNull()
  })

  it("marks the worst fresh window's tone", async () => {
    renderIt("/agents", { compact: true })
    expect((await button()).querySelector("[data-tone='over']")).toBeTruthy()
    cleanup()
    const providers = (usage as { providers: { windows: { used_percent: number }[] }[] }).providers
    providers[0].windows[1].used_percent = 80
    renderIt("/agents", { compact: true })
    expect((await button()).querySelector("[data-tone='warn']")).toBeTruthy()
    cleanup()
    providers[0].windows[1].used_percent = 50
    renderIt("/agents", { compact: true })
    expect((await button()).querySelector("[data-tone]")).toBeNull()
  })

  it("opens the details as a modal sheet", async () => {
    renderIt("/agents", { compact: true })
    fireEvent.click(await button())
    expect(card().getAttribute("aria-modal")).toBe("true")
    expect((await button()).getAttribute("aria-expanded")).toBe("true")
  })

  it("retries a failed read from the button, without a block of text", async () => {
    let fail = true
    vi.stubGlobal("fetch", vi.fn(async () => fail ? json({ error: "usage poller unavailable" }, 500) : json(usage)))
    renderIt("/agents", { compact: true })
    const retry = await screen.findByRole("button", { name: "Subscription usage could not be read: usage poller unavailable. Retry" })
    expect(screen.queryByRole("alert")).toBeNull()
    fail = false
    fireEvent.click(retry)
    expect(await screen.findByRole("button", { name: /^Subscription usage\. Claude/ })).toBeTruthy()
  })

  it("shows nothing when no provider is read", async () => {
    usage = { poll_interval_seconds: 0, providers: [] }
    renderIt("/agents", { compact: true })
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(screen.queryByRole("button")).toBeNull()
  })
})

describe("the popout", () => {
  it("opens to a bar per window, with when each resets and how fresh the reading is", async () => {
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    fireEvent.click(strip)
    expect(strip.getAttribute("aria-expanded")).toBe("true")
    const claude = within(card()).getByRole("group", { name: "Claude" })
    const weekly = within(claude).getByRole("progressbar", { name: "Claude weekly" })
    expect(weekly.getAttribute("aria-valuenow")).toBe("91")
    expect(within(claude).getByText("in 2h")).toBeTruthy()
    expect(within(claude).getByText("Tue 09:00")).toBeTruthy()
    expect(within(claude).getByText("2m ago")).toBeTruthy()
    expect(within(claude).getByText(/max/)).toBeTruthy()

    const codex = within(card()).getByRole("group", { name: "Codex" })
    expect(within(codex).getByText(/stale · 3h ago · could not reach it/)).toBeTruthy()
    expect(within(codex).getByText(/from Codex's session logs/)).toBeTruthy()
  })

  it("closes on Escape and gives focus back to the card", async () => {
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    strip.focus()
    fireEvent.click(strip)
    expect(card().contains(document.activeElement)).toBe(true)
    fireEvent.keyDown(document.activeElement!, { key: "Escape" })
    expect(screen.queryByRole("dialog", { name: "Subscription usage" })).toBeNull()
    expect(document.activeElement).toBe(strip)
  })

  it("closes from its close button", async () => {
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    fireEvent.click(within(card()).getByRole("button", { name: "Close" }))
    expect(screen.queryByRole("dialog", { name: "Subscription usage" })).toBeNull()
  })

  it("opens as a sheet over the page on a phone", async () => {
    renderIt("/agents", { sheet: true })
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    expect(card().getAttribute("aria-modal")).toBe("true")
  })

  it("rounds Codex's credit balance", async () => {
    usage = { poll_interval_seconds: 300, catalog, providers: [{ provider: "codex", state: "ok", stale: false, as_of: "2026-11-26T07:58:00Z", credits: { has_credits: true, unlimited: false, balance: "1240.50382910" }, windows: [{ meter: "weekly", used_percent: 74 }] }] }
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    expect(within(card()).getByText("1,240.50")).toBeTruthy()
  })

  it("says why a provider is unavailable", async () => {
    usage = {
      poll_interval_seconds: 300,
      providers: [
        { provider: "claude", state: "unavailable", stale: false, reason: "not_signed_in", windows: [] },
        { provider: "codex", state: "unavailable", stale: false, reason: "codex_missing", windows: [] },
      ],
    }
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(within(strip).getAllByText("unavailable")).toHaveLength(2)
    fireEvent.click(strip)
    expect(within(card()).getByText("Sign in to Claude Code on this machine.")).toBeTruthy()
    expect(within(card()).getByText("The codex command is not installed on this machine.")).toBeTruthy()
    expect(within(card()).queryByRole("progressbar")).toBeNull()
  })

  it("names the configured account in sign-in guidance", async () => {
    usage = { poll_interval_seconds: 300, catalog: [...catalog, { provider: "claude:review", name: "Review subscription", meters: [] }], providers: [{ provider: "claude:review", kind: "claude", account: "review", state: "unavailable", stale: false, reason: "not_signed_in", windows: [] }] }
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    expect(screen.getByText("Sign in to Review subscription on this machine.")).toBeTruthy()
  })

  it("shows OpenCode Go beside the others, with its monthly meter", async () => {
    usage = {
      ...(usage as Record<string, unknown>),
      providers: [
        ...(usage as { providers: unknown[] }).providers,
        {
          provider: "opencode", state: "ok", stale: false, source: "endpoint",
          as_of: "2026-11-26T07:58:00Z",
          windows: [
            { meter: "five_hour", used_percent: 30, resets_at: "2026-11-26T13:00:00Z" },
            { meter: "weekly", used_percent: 66, resets_at: "2026-11-30T00:00:00Z" },
            { meter: "monthly", used_percent: 50, resets_at: "2026-12-01T00:00:00Z" },
          ],
        },
      ],
    }
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    const opencode = row(strip, "opencode")
    expect(within(opencode).getByText("OpenCode Go")).toBeTruthy()
    expect(within(opencode).getByText("66%")).toBeTruthy()
    expect(opencode.querySelectorAll("[data-meter]")).toHaveLength(3)
    fireEvent.click(strip)
    const group = within(card()).getByRole("group", { name: "OpenCode Go" })
    expect(within(group).getByRole("progressbar", { name: "OpenCode Go monthly" }).getAttribute("aria-valuenow")).toBe("50")
    expect(within(group).getByText("Tue 00:00")).toBeTruthy()
  })

  it("says what to do when OpenCode Go has no key", async () => {
    usage = {
      ...(usage as Record<string, unknown>),
      providers: [{ provider: "opencode", state: "unavailable", stale: false, reason: "missing_key", windows: [] }],
    }
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    expect(within(card()).getByText("Name its API key in Settings › Usage.")).toBeTruthy()
  })

  it("says what to do when OpenCode Go's key is refused", async () => {
    usage = {
      ...(usage as Record<string, unknown>),
      providers: [{ provider: "opencode", state: "unavailable", stale: false, reason: "invalid_key", windows: [] }],
    }
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    expect(within(card()).getByText("OpenCode Go's API key was refused; replace the key's secret and restart.")).toBeTruthy()
  })

  it("asks for a reading now", async () => {
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    fireEvent.click(within(card()).getByRole("button", { name: "Read now" }))
    await waitFor(() => expect(posted).toEqual(["/api/usage/refresh"]))
  })
})

describe("the profile in the popout", () => {
  it("offers every catalog meter to pin until, named and with its reset", async () => {
    usage = {
      ...(usage as Record<string, unknown>),
      policy: {
        active: { profile: { name: "Economy", agent: "codex" }, reason: { text: "Claude weekly reached 90%" }, since: "2026-11-26T07:00:00Z" },
        profiles: [{ name: "Economy", agent: "codex" }],
        switches: [],
      },
    }
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    fireEvent.click(screen.getByRole("button", { name: "Pin a profile" }))
    fireEvent.click(screen.getByRole("radio", { name: "A reset" }))
    const meters = screen.getByRole("combobox", { name: "Until this limit resets" })
    expect((within(meters).getByRole("option", { name: "OpenCode Go · monthly" }) as HTMLOptionElement).value).toBe("opencode.monthly")
    expect((within(meters).getByRole("option", { name: "Claude · weekly (Tue 09:00)" }) as HTMLOptionElement).value).toBe("claude.weekly")
    expect(within(meters).getByRole("option", { name: "Claude · 5-hour (in 2h)" })).toBeTruthy()
  })

  it("shows a policy with readers off, its agent, reason and time, and bounded recent switches", async () => {
    usage = { providers: [], poll_interval_seconds: 0, policy: {
      active: { profile: { name: "Economy", agent: "codex", tier: "fast", reviewer: { agent: "claude-code", tier: "high" } }, reason: { text: "Claude weekly reached 90%" }, since: "2026-11-26T07:00:00Z", error: "Restart to apply the new tier" },
      profiles: [{ name: "Economy", agent: "codex", tier: "fast" }],
      switches: Array.from({ length: 12 }, (_, i) => ({ id: i, from_profile: `Old ${i}`, to_profile: "Economy", since: "2026-11-26T07:00:00Z", reason: "Threshold reached" })),
    } }
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    expect(within(card()).getAllByText("Economy").length).toBeGreaterThan(0)
    expect(within(card()).getByText("codex · fast")).toBeTruthy()
    expect(within(card()).getByText("claude-code · high")).toBeTruthy()
    expect(within(card()).getByText("Claude weekly reached 90%")).toBeTruthy()
    expect(within(card()).getByText("07:00 · 1h ago")).toBeTruthy()
    expect(within(card()).getByText("Usage readers are off.")).toBeTruthy()
    expect(within(card()).getByRole("alert").textContent).toBe("Restart to apply the new tier")
    const toggle = within(card()).getByRole("button", { name: /Recent switches/ })
    expect(toggle.getAttribute("aria-expanded")).toBe("false")
    fireEvent.click(toggle)
    expect(within(card()).getAllByRole("listitem")).toHaveLength(5)
  })

  it("offers profile setup when no profiles are configured", async () => {
    usage = { providers: [], poll_interval_seconds: 0, policy: {
      active: { profile: { name: "", agent: "" }, reason: { text: "Usual defaults" }, since: "0001-01-01T00:00:00Z" },
      profiles: null, switches: [],
    } }
    renderIt("/agents?usage=open&pin=1")
    expect((await screen.findByRole("link", { name: "Configure profiles" })).getAttribute("href")).toBe("/settings#usage")
    expect(screen.queryByRole("button", { name: "Pin a profile" })).toBeNull()
  })
})

it("opens pin controls from the address, sends the expiry intent, and keeps a rejected draft", async () => {
  usage = { catalog, providers: [], poll_interval_seconds: 0, policy: {
    active: { profile: { name: "Economy", agent: "codex" }, reason: { text: "Fallback" }, since: NOW.toISOString() },
    profiles: [{ name: "Economy", agent: "codex" }, { name: "Quality", agent: "claude-code" }], switches: [],
  } }
  const requests: unknown[] = []
  vi.stubGlobal("fetch", vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === "PUT") { requests.push(JSON.parse(String(init.body))); return json({ error: "Meter has no known reset" }, 400) }
    return json(usage)
  }))
  renderIt("/agents?usage=open&pin=Quality")
  const profile = await screen.findByRole("combobox", { name: "Pin profile" })
  expect((profile as HTMLSelectElement).value).toBe("Quality")
  fireEvent.click(screen.getByRole("radio", { name: "A reset" }))
  fireEvent.change(screen.getByRole("combobox", { name: "Until this limit resets" }), { target: { value: "claude.weekly" } })
  fireEvent.click(screen.getByRole("button", { name: "Pin profile" }))
  expect((await screen.findByRole("alert")).textContent).toBe("Meter has no known reset")
  expect(requests).toEqual([{ profile: "Quality", until_reset: "claude.weekly" }])
  expect((profile as HTMLSelectElement).value).toBe("Quality")
})

it.each(["indefinite", "time"])("pins a profile with %s expiry and can unpin it", async (expiry) => {
  const policy = {
    active: { profile: { name: "Economy", agent: "codex" }, reason: { text: "Fallback" }, since: NOW.toISOString(), pin: { profile: "Economy" } },
    profiles: [{ name: "Economy", agent: "codex" }], switches: [],
  }
  usage = { providers: [], poll_interval_seconds: 0, policy }
  const requests: { method: string; body?: unknown }[] = []
  vi.stubGlobal("fetch", vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === "PUT" || init?.method === "DELETE") {
      requests.push({ method: init.method, ...(init.body ? { body: JSON.parse(String(init.body)) } : {}) })
      return json({ status: "ok" })
    }
    return json(usage)
  }))
  renderIt("/agents?usage=open&pin=Economy")
  await screen.findByRole("combobox", { name: "Pin profile" })
  if (expiry === "time") {
    fireEvent.click(screen.getByRole("radio", { name: "A time" }))
    fireEvent.change(screen.getByLabelText("Until (local time)"), { target: { value: "2026-11-27T12:00" } })
  }
  fireEvent.click(screen.getByRole("button", { name: "Pin profile" }))
  expect((await screen.findByRole("status")).textContent).toBe("Profile pinned.")
  expect(requests[0]).toEqual({ method: "PUT", body: { profile: "Economy", ...(expiry === "time" ? { until: new Date("2026-11-27T12:00").toISOString() } : {}) } })
  fireEvent.click(screen.getByRole("button", { name: "Unpin" }))
  await waitFor(() => expect(requests[1]).toEqual({ method: "DELETE" }))
})

it("can unpin with an incomplete expiry draft and surfaces a refusal", async () => {
  usage = { providers: [], poll_interval_seconds: 0, policy: {
    active: { profile: { name: "Economy", agent: "codex" }, reason: { text: "Pinned" }, since: NOW.toISOString(), pin: { profile: "Economy" } },
    profiles: [{ name: "Economy", agent: "codex" }], switches: [],
  } }
  const deleted = vi.fn()
  vi.stubGlobal("fetch", vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === "DELETE") { deleted(); return json({ error: "Policy owner unavailable" }, 400) }
    return json(usage)
  }))
  renderIt("/agents?usage=open&pin=Economy")
  fireEvent.click(await screen.findByRole("radio", { name: "A time" }))
  fireEvent.click(screen.getByRole("button", { name: "Unpin" }))
  expect((await screen.findByRole("alert")).textContent).toBe("Policy owner unavailable")
  expect(deleted).toHaveBeenCalledOnce()
})

it("keeps an existing reset expiry when changing its pinned profile", async () => {
  usage = { providers: [], poll_interval_seconds: 0, policy: {
    active: { profile: { name: "Economy", agent: "codex" }, reason: { text: "Pinned" }, since: NOW.toISOString(), pin: { profile: "Economy", until_reset: "codex.weekly" } },
    profiles: [{ name: "Economy", agent: "codex" }, { name: "Quality", agent: "claude-code" }], switches: [],
  } }
  let body: unknown
  vi.stubGlobal("fetch", vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === "PUT") { body = JSON.parse(String(init.body)); return json({ status: "ok" }) }
    return json(usage)
  }))
  renderIt("/agents?usage=open&pin=Quality")
  await screen.findByRole("button", { name: "Pin profile" })
  fireEvent.click(screen.getByRole("button", { name: "Pin profile" }))
  await waitFor(() => expect(body).toEqual({ profile: "Quality", until_reset: "codex.weekly" }))
})
