import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { HelpModal } from "../HelpModal"

it("keeps help focus inside its labeled dialog and restores the opener", () => {
  const close = vi.fn()
  const opener = document.createElement("button")
  document.body.append(opener)
  opener.focus()
  const view = render(<HelpModal open onClose={close} />)
  const dialog = screen.getByRole("dialog", { name: "Keyboard Reference" })
  const button = within(dialog).getByRole("button", { name: /close/i })
  expect(button).toHaveFocus()
  fireEvent.keyDown(button, { key: "Tab", shiftKey: true })
  expect(button).toHaveFocus()
  fireEvent.keyDown(button, { key: "Escape" })
  expect(close).toHaveBeenCalledTimes(1)
  view.unmount()
  expect(opener).toHaveFocus()
  opener.remove()
})

import { MoveTaskModal } from "../MoveTaskModal"
import { ArchiveDoneModal } from "../ArchiveDoneModal"
import { OpenURLPicker } from "../OpenURLPicker"

it.each([
  ["move", (close: () => void) => <MoveTaskModal open taskId="T1" taskTitle="Task" columns={["Todo", "Done"]} currentColumn="Todo" workspaces={[]} currentWorkspaceId={null} onMoveColumn={vi.fn()} onMoveWorkspace={vi.fn()} onClose={close} />],
  ["archive", (close: () => void) => <ArchiveDoneModal open count={3} onConfirm={vi.fn()} onClose={close} />],
  ["links", (close: () => void) => <OpenURLPicker open providerURL="https://example.com" prURL="" onSelect={vi.fn()} onClose={close} />],
] as const)("%s traps Tab and handles Escape", (_, component) => {
  const close = vi.fn()
  render(component(close))
  const dialog = screen.getByRole("dialog")
  expect(dialog).toHaveAccessibleName()
  const buttons = within(dialog).getAllByRole("button").filter(button => !button.hasAttribute("disabled"))
  buttons.at(-1)!.focus()
  fireEvent.keyDown(document.activeElement!, { key: "Tab" })
  expect(buttons[0]).toHaveFocus()
  fireEvent.keyDown(document.activeElement!, { key: "Escape" })
  expect(close).toHaveBeenCalledTimes(1)
})

import { SearchModal } from "../SearchModal"
import { ImportRemoteModal } from "../ImportRemoteModal"
import { ArchivedTasksModal } from "../ArchivedTasksModal"
import { CancelSwarmModal } from "../CancelSwarmModal"
import { LinkPRModal } from "../LinkPRModal"
import { MemoryRouter } from "react-router-dom"
vi.mock("../../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../../lib/board", () => ({
  searchTasks: vi.fn().mockResolvedValue([]), remoteSearch: vi.fn().mockResolvedValue([]), remoteImport: vi.fn(),
  fetchTicketProviders: vi.fn().mockResolvedValue([]), fetchArchivedTasks: vi.fn().mockResolvedValue([]),
  restoreTask: vi.fn(), detectRepo: vi.fn().mockResolvedValue({ owner: "me", repo: "repo" }), prPreview: vi.fn(), linkPR: vi.fn(),
}))
it.each([
  ["search", (close: () => void) => <SearchModal open onSelect={vi.fn()} onClose={close} />],
  ["import", (close: () => void) => <ImportRemoteModal open onImported={vi.fn()} onClose={close} />],
  ["archived", (close: () => void) => <ArchivedTasksModal open onRestored={vi.fn()} onClose={close} />],
  ["cancel swarm", (close: () => void) => <CancelSwarmModal isOpen taskId="T1" taskTitle="Task" onConfirm={vi.fn()} onClose={close} />],
  ["link PR", (close: () => void) => <LinkPRModal open taskId="T1" onLinked={vi.fn()} onClose={close} />],
] as const)("%s is named, focuses inside and closes on Escape", async (_, component) => {
  const close = vi.fn()
  render(<MemoryRouter>{component(close)}</MemoryRouter>)
  const dialog = screen.getByRole("dialog")
  expect(dialog).toHaveAccessibleName()
  expect(dialog.contains(document.activeElement)).toBe(true)
  fireEvent.keyDown(document.activeElement!, { key: "Escape" })
  expect(close).toHaveBeenCalledTimes(1)
})

import { fetchArchivedTasks, restoreTask } from "../../../lib/board"
it("keeps a failed archive restore visible inside the active dialog", async () => {
  vi.mocked(fetchArchivedTasks).mockResolvedValueOnce([{id:"T1",title:"Archived task",status:"Done"}] as Awaited<ReturnType<typeof fetchArchivedTasks>>)
  vi.mocked(restoreTask).mockRejectedValueOnce(new Error("Restore refused"))
  render(<ArchivedTasksModal open onClose={vi.fn()} onRestored={vi.fn()} />)
  fireEvent.click(await screen.findByRole("button", {name:"Restore"}))
  const dialog = screen.getByRole("dialog", {name:"Archived tasks"})
  expect((await within(dialog).findByRole("alert")).textContent).toContain("Restore refused")
  expect(within(dialog).getByRole("button", {name:"Restore"})).toBeEnabled()
})
