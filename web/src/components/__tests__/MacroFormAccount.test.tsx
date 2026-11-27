import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { MacroForm } from "../MacroForm"
const save = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))
vi.mock("../../lib/macros", async (actual) => ({ ...await actual<typeof import("../../lib/macros")>(), saveMacro: save }))
afterEach(cleanup)
it("keeps a macro's configured account when edited", async () => {
  render(<MacroForm baseUrl="" macro={{ name: "Build", keys: "Build this", agent: "claude-code", account: "work" }} macros={[]} adapters={{ adapters: ["claude-code"], default: "claude-code", accounts: [{ name: "work", label: "Work subscription", provider: "claude" }] }} tiers={{}} onClose={() => {}} onSaved={() => {}} />)
  expect((screen.getByRole("combobox", { name: "Account" }) as HTMLSelectElement).value).toBe("work")
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(save).toHaveBeenCalledWith("", "Build", expect.objectContaining({ account: "work" })))
})
