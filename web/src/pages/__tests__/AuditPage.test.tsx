import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AuditPage } from "../AuditPage"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const read = (seq: number) => ({
  seq, at: "2026-10-29T22:39:5" + (seq % 10) + ".000Z", event: "use", credential: "github", operation: "api.read",
  resource: "github:autopilot-dev/autopilot", task_id: "AG-10102", task_title: "Comparable sales", execution_id: "ex-a", detail: "auto · TOKEN autopilot-dev/autopilot",
})
// A full page (100 rows) has older rows behind it.
const firstPage = [
  read(200), read(199), read(198),
  { seq: 197, at: "2026-10-29T14:22:26.000Z", event: "grant", credential: "proxmox_vms", operation: "ssh.login", resource: "ssh://cameron@psi.pavey.network", task_id: "yjpvdfmy", approval_id: "ap-e1e2d310", decided_by: "web" },
  ...Array.from({ length: 96 }, (_, i) => ({ seq: 196 - i, at: "2026-10-29T12:00:00.000Z", event: "request", credential: "github", operation: "git.push", task_id: "rehzkrch" })),
]
const olderPage = [{ seq: 4, at: "2026-10-29T09:54:21.000Z", event: "refuse", credential: "github", operation: "read", task_id: "AG-10102", detail: "no rule allows read" }]

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))
}

function calls(path: string) {
  return apiFetch.mock.calls.map(([, called]) => called as string).filter((called) => called.startsWith(path))
}

describe("AuditPage", () => {
  beforeEach(() => {
    apiFetch.mockReset()
    apiFetch.mockImplementation((_: string, path: string) => {
      if (path.startsWith("/api/credentials/audit/verify")) return json({ intact: true })
      if (path.startsWith("/api/credentials/audit")) return json({ entries: new URLSearchParams(path.split("?")[1]).get("before") ? olderPage : firstPage })
      if (path.startsWith("/api/credentials")) return json({ credentials: [{ name: "github" }, { name: "proxmox_vms" }] })
      return json({})
    })
  })
  afterEach(cleanup)

  it("shows which agent used which credential for what, folding a burst of the same use", async () => {
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    const burst = await screen.findByRole("row", { name: /AG-10102.*api\.read/ })
    expect(within(burst).getByText("Comparable sales")).toBeTruthy()
    expect(within(burst).getByText("×3")).toBeTruthy()
    expect(within(burst).getByText("github:autopilot-dev/autopilot")).toBeTruthy()
    const ssh = screen.getByRole("row", { name: /yjpvdfmy.*ssh\.login/ })
    expect(within(ssh).getByText("grant")).toBeTruthy()
    expect(within(ssh).getByText(/ap-e1e2d310/)).toBeTruthy()
    expect(await screen.findByText(/Chain intact/)).toBeTruthy()
  })

  it("asks again for one credential's rows when it is chosen", async () => {
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    await screen.findByRole("row", { name: /AG-10102/ })
    fireEvent.change(screen.getByRole("combobox", { name: "Credential" }), { target: { value: "proxmox_vms" } })
    await vi.waitFor(() => expect(calls("/api/credentials/audit?").some((c) => c.includes("credential=proxmox_vms"))).toBe(true))
  })

  it("loads older rows from where the page ends", async () => {
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    await screen.findByRole("row", { name: /AG-10102/ })
    fireEvent.click(screen.getByRole("button", { name: "Load older" }))
    expect(await screen.findByRole("row", { name: /refuse/ })).toBeTruthy()
    expect(calls("/api/credentials/audit?").some((c) => c.includes("before=101"))).toBe(true)
  })
})
