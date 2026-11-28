import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ComponentProps } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MacroForm } from "../MacroForm"
import type { Macro } from "../../lib/macros"

const { save, rename } = vi.hoisted(() => ({ save: vi.fn(), rename: vi.fn() }))
vi.mock("../../lib/macros", async (actual) => ({ ...await actual<typeof import("../../lib/macros")>(), saveMacro: save, renameMacro: rename }))

const adapters = { adapters: ["claude-code", "codex"], default: "codex", accounts: [{ name: "work", label: "Work subscription", provider: "claude" }] }
const tiers = {
  codex: [{ name: "fast", description: "quick answers" }, { name: "smart" }],
  "claude-code": [{ name: "smart" }],
}
const macros: Macro[] = [{ name: "Plan", keys: "plan" }, { name: "Review", keys: "review" }]

function open(props: Partial<ComponentProps<typeof MacroForm>> = {}) {
  const onSaved = vi.fn()
  const onClose = vi.fn()
  render(<MacroForm baseUrl="" macro={null} macros={macros} adapters={adapters} tiers={tiers} onClose={onClose} onSaved={onSaved} {...props} />)
  return { onSaved, onClose }
}

const nameField = () => screen.getByRole("textbox", { name: /^Name/ }) as HTMLInputElement
const messageField = () => screen.getByRole("textbox", { name: /^Message/ }) as HTMLTextAreaElement
const saveButton = () => screen.getByRole("button", { name: "Save" }) as HTMLButtonElement

describe("MacroForm", () => {
  beforeEach(() => {
    save.mockReset().mockResolvedValue(undefined)
    rename.mockReset().mockResolvedValue(undefined)
  })
  afterEach(cleanup)

  it("makes a new macro with what was chosen", async () => {
    const { onSaved } = open()
    expect(screen.getByRole("heading", { name: "New macro" })).toBeTruthy()
    expect(saveButton().disabled).toBe(true)
    fireEvent.change(nameField(), { target: { value: "  Ship  " } })
    expect(saveButton().disabled).toBe(true)
    fireEvent.change(messageField(), { target: { value: "Ship it." } })
    fireEvent.click(screen.getByRole("radio", { name: "Pull requests" }))
    fireEvent.click(screen.getByRole("radio", { name: "Codex" }))
    fireEvent.change(screen.getByRole("combobox", { name: /Model tier/ }), { target: { value: "fast" } })
    fireEvent.click(saveButton())
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(save).toHaveBeenCalledWith("", "Ship", { message: "Ship it.", applies_to: "pr", agent: "codex", tier: "fast" })
    expect(rename).not.toHaveBeenCalled()
  })

  it("names the default agent, then each tool", () => {
    open()
    expect(screen.getAllByRole("radio").slice(3).map((radio) => radio.parentElement?.textContent)).toEqual(["Your default (Codex)", "Claude Code", "Codex"])
    expect((screen.getByRole("radio", { name: "Tasks and pull requests" }) as HTMLInputElement).checked).toBe(true)
  })

  it("offers the default tool's tiers, and says whose they are", () => {
    open()
    const tier = screen.getByRole("combobox", { name: /Model tier/ }) as HTMLSelectElement
    expect(Array.from(tier.options).map((option) => option.textContent)).toEqual(["None", "fast · quick answers", "smart"])
    expect(screen.getByText("Codex’s tiers from config.yaml. Used when this macro starts the agent.")).toBeTruthy()
  })

  it("asks no tier of a tool that has none", () => {
    open({ adapters: { adapters: ["chimera"], default: "chimera" } })
    expect(screen.queryByRole("combobox", { name: /Model tier/ })).toBeNull()
    expect(screen.getByText("Your default (Chimera)")).toBeTruthy()
  })

  it("keeps a tier the new tool has, and drops one it lacks", () => {
    open()
    const tier = () => screen.getByRole("combobox", { name: /Model tier/ }) as HTMLSelectElement
    fireEvent.change(tier(), { target: { value: "smart" } })
    fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
    expect(tier().value).toBe("smart")
    fireEvent.click(screen.getByRole("radio", { name: "Codex" }))
    fireEvent.change(tier(), { target: { value: "fast" } })
    fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
    expect(tier().value).toBe("")
  })

  it("warns of a tier the tool does not have", () => {
    open({ macro: { name: "Deep", keys: "Go deep.", agent: "codex", tier: "huge" } })
    expect(screen.getByRole("alert").textContent).toBe("Codex has no tier “huge”. It starts without one.")
    expect((screen.getByRole("combobox", { name: /Model tier/ }) as HTMLSelectElement).value).toBe("huge")
  })

  it("offers Claude Code's accounts only for Claude Code, and forgets the account on another tool", () => {
    open()
    expect(screen.queryByRole("combobox", { name: "Account" })).toBeNull()
    fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
    const account = screen.getByRole("combobox", { name: "Account" }) as HTMLSelectElement
    expect(Array.from(account.options).map((option) => option.textContent)).toEqual(["Default account", "Work subscription"])
    fireEvent.change(account, { target: { value: "work" } })
    fireEvent.click(screen.getByRole("radio", { name: "Codex" }))
    expect(screen.queryByRole("combobox", { name: "Account" })).toBeNull()
  })

  it("follows the usage profile for an account on the default Claude Code", () => {
    open({ adapters: { ...adapters, default: "claude-code" } })
    const account = screen.getByRole("combobox", { name: "Account" }) as HTMLSelectElement
    expect(account.options[0].textContent).toBe("Follow usage profile")
  })

  it("keeps an account that is not configured, and says so", () => {
    open({ macro: { name: "Build", keys: "Build", account: "old" }, adapters: { adapters: ["codex"], default: "codex" } })
    const account = screen.getByRole("combobox", { name: "Account" }) as HTMLSelectElement
    expect(account.value).toBe("old")
    expect(Array.from(account.options).map((option) => option.textContent)).toEqual(["Follow usage profile", "old (not configured)"])
  })

  it("lists a tool the macro names that is not configured", () => {
    open({ macro: { name: "Old", keys: "x", agent: "aider" } })
    expect((screen.getByRole("radio", { name: "aider" }) as HTMLInputElement).checked).toBe(true)
  })

  it("refuses a name another macro has, whatever its case", () => {
    open()
    fireEvent.change(nameField(), { target: { value: " review " } })
    fireEvent.change(messageField(), { target: { value: "x" } })
    expect(screen.getByRole("alert").textContent).toBe("Another macro has that name.")
    expect(saveButton().disabled).toBe(true)
  })

  it("edits a macro, warning of a rename and renaming after the save", async () => {
    const { onSaved } = open({ macro: { name: "Plan", keys: "plan\n", applies_to: "task" } })
    expect(screen.getByRole("heading", { name: "Edit Plan" })).toBeTruthy()
    expect(messageField().value).toBe("plan")
    fireEvent.change(nameField(), { target: { value: "PLAN" } })
    expect(screen.queryByText(/will fail/)).toBeNull()
    fireEvent.change(nameField(), { target: { value: "Plan it" } })
    expect(screen.getByText("Workflows that use the old name, Plan, will fail.")).toBeTruthy()
    fireEvent.click(saveButton())
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(save).toHaveBeenCalledWith("", "Plan", { message: "plan", applies_to: "task", agent: "", tier: "" })
    expect(rename).toHaveBeenCalledWith("", "Plan", "Plan it")
  })

  it("does not let a built-in macro be renamed", async () => {
    const { onSaved } = open({ macro: { name: "Review", keys: "review", builtin: true } })
    expect(screen.getByText("Built-in names can’t change.")).toBeTruthy()
    expect(screen.getByText(/Built-in macro\./)).toBeTruthy()
    expect(nameField().disabled).toBe(true)
    fireEvent.click(saveButton())
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(rename).not.toHaveBeenCalled()
  })

  it("says why it could not save, and lets it be tried again", async () => {
    save.mockRejectedValueOnce(new Error("disk full"))
    const { onSaved } = open({ macro: { name: "Plan", keys: "plan" } })
    fireEvent.click(saveButton())
    expect((await screen.findByRole("alert")).textContent).toBe("disk full")
    expect(saveButton().disabled).toBe(false)
    expect(onSaved).not.toHaveBeenCalled()
  })

  it("says Saving… while it saves", async () => {
    let finish = () => {}
    save.mockReturnValueOnce(new Promise<void>((resolve) => { finish = resolve }))
    open({ macro: { name: "Plan", keys: "plan" } })
    fireEvent.click(saveButton())
    const saving = await screen.findByRole("button", { name: "Saving…" }) as HTMLButtonElement
    expect(saving.disabled).toBe(true)
    finish()
  })

  it("closes on Cancel", () => {
    const { onClose } = open()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(onClose).toHaveBeenCalled()
  })
})
