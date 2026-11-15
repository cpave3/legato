import "@testing-library/jest-dom/vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { expect, it, vi } from "vitest"
import { NavRail } from "../NavRail"
import { navSections } from "../../lib/navSections"

vi.mock("../../hooks/useServerTint", () => ({ useServerTint: () => "", useServerVersion: () => "" }))

it("exposes server disclosure and current choice, dismisses by keyboard and restores focus", async () => {
  const user = userEvent.setup()
  const onSwitch = vi.fn()
  render(<MemoryRouter><NavRail sections={navSections()} connected servers={[{ name: "Worker", url: "https://worker.test" }]} activeServerName="Local" baseUrl="" onSwitchServer={onSwitch} /></MemoryRouter>)
  const trigger = screen.getByRole("button", { name: "Server: Local" })
  expect(trigger).toHaveAttribute("aria-expanded", "false")
  trigger.focus()
  await user.keyboard("{Enter}")
  expect(trigger).toHaveAttribute("aria-expanded", "true")
  const current = screen.getByRole("button", { name: "Local" })
  expect(current).toHaveAttribute("aria-current", "true")
  expect(current).toHaveFocus()
  await user.keyboard("{Escape}")
  expect(screen.queryByRole("button", { name: "Worker" })).not.toBeInTheDocument()
  expect(trigger).toHaveFocus()
  await user.keyboard("{Enter}{Tab}{Enter}")
  expect(onSwitch).toHaveBeenCalledWith("https://worker.test")
  expect(trigger).toHaveFocus()
  await user.keyboard("{Enter}{Shift>}{Tab}{/Shift}{Shift>}{Tab}{/Shift}")
  expect(trigger).toHaveAttribute("aria-expanded", "false")
})

it("announces connection changes separately from Attention", () => {
  const view = (connected: boolean) => <MemoryRouter><NavRail sections={navSections({ attention: 4 })} connected={connected} servers={[]} activeServerName="Local" baseUrl="" onSwitchServer={() => {}} /></MemoryRouter>
  const { rerender } = render(view(false))
  expect(screen.getByRole("status", { name: "Connection" })).toHaveTextContent("offline")
  expect(screen.getByRole("link", { name: "Attention: 4 items" })).toBeInTheDocument()
  rerender(view(true))
  expect(screen.getByRole("status", { name: "Connection" })).toHaveTextContent("sync")
})
