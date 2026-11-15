import "@testing-library/jest-dom/vitest"
import { useState } from "react"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { expect, it, vi } from "vitest"
import { MobileTabBar, MoreSheet } from "../MobileNav"
import { navSections } from "../../lib/navSections"

const props = {
  sections: navSections({ plans: 123, learning: 2, workflow_attention: 3 }),
  connected: true, servers: [{ name: "Worker", url: "https://worker.test" }],
  activeServerName: "Local", baseUrl: "", onSwitchServer: vi.fn(), onClose: vi.fn(),
}

it("names More destinations with their full counts and marks the current page", () => {
  render(<MemoryRouter initialEntries={["/plans/one"]}><MoreSheet {...props} /></MemoryRouter>)
  expect(screen.getByRole("link", { name: "Plans: 123 to decide" })).toHaveAttribute("aria-current", "page")
  expect(screen.getByRole("link", { name: "Learning: 2 to decide" })).not.toHaveAttribute("aria-current")
  expect(screen.getByRole("link", { name: "Workflows: 3 need attention" })).toHaveAttribute("href", "/workflows")
  expect(screen.getByRole("link", { name: "Audit log" })).toHaveAttribute("href", "/audit")
})

it("keeps connection status separate from the Attention count", () => {
  const sections = navSections({ attention: 4 })
  const view = (connected: boolean) => <MemoryRouter><MobileTabBar sections={sections} connected={connected} moreOpen={false} onMore={() => {}} /></MemoryRouter>
  const { rerender } = render(view(false))
  const attention = screen.getByRole("link", { name: "Attention: 4 items" })
  const status = screen.getByRole("status", { name: "Connection" })
  expect(status).toHaveTextContent("offline")
  expect(attention).not.toContainElement(status)
  expect(screen.queryByLabelText("Disconnected")).not.toBeInTheDocument()
  rerender(view(true))
  expect(screen.getByRole("status", { name: "Connection" })).toHaveTextContent("sync")
  expect(screen.getByRole("link", { name: "Attention: 4 items" })).toBeInTheDocument()
})

it("marks the mobile server choice and returns focus when dismissing or choosing", async () => {
  const user = userEvent.setup()
  const onClose = vi.fn()
  const onSwitchServer = vi.fn()
  render(<MemoryRouter><MoreSheet {...props} onClose={onClose} onSwitchServer={onSwitchServer} /></MemoryRouter>)
  const trigger = screen.getByRole("button", { name: "Switch server" })
  await user.click(trigger)
  expect(trigger).toHaveAttribute("aria-expanded", "true")
  const local = screen.getByRole("button", { name: "Local" })
  expect(local).toHaveAttribute("aria-current", "true")
  expect(local).toHaveFocus()
  await user.keyboard("{Escape}")
  expect(trigger).toHaveAttribute("aria-expanded", "false")
  expect(trigger).toHaveFocus()
  expect(onClose).not.toHaveBeenCalled()
  await user.keyboard("{Enter}{Tab}{Enter}")
  expect(onSwitchServer).toHaveBeenCalledWith("https://worker.test")
  expect(trigger).toHaveFocus()
})

it("contains keyboard focus in More and restores its opener after Escape", async () => {
  const user = userEvent.setup()
  function Shell() {
    const [open, setOpen] = useState(false)
    return <><MobileTabBar sections={props.sections} connected moreOpen={open} onMore={() => setOpen(!open)} />{open && <MoreSheet {...props} onClose={() => setOpen(false)} />}</>
  }
  render(<MemoryRouter><Shell /></MemoryRouter>)
  const trigger = screen.getByRole("button", { name: "More: 128 waiting" })
  await user.click(trigger)
  const dialog = screen.getByRole("dialog", { name: "More" })
  expect(dialog).toHaveAttribute("aria-modal", "true")
  expect(screen.getByRole("button", { name: "Close" })).toHaveFocus()
  await user.tab({ shift: true })
  expect(screen.getByRole("link", { name: "All settings" })).toHaveFocus()
  await user.tab()
  expect(screen.getByRole("button", { name: "Close" })).toHaveFocus()
  await user.keyboard("{Escape}")
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  expect(trigger).toHaveFocus()
})
