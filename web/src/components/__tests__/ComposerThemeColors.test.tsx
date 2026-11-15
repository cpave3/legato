import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { SendButton } from "../Composer"

it("renders the send action with theme canvas ink, including its keyboard hint", () => {
  const onClick = vi.fn()
  render(<SendButton label="Send message" queue={false} disabled={false} onClick={onClick} />)

  const send = screen.getByRole("button", { name: "Send message" })
  expect(send.className).toContain("bg-lw-accent")
  expect(send.className).toContain("text-[var(--theme-canvas,#fff)]")
  expect(screen.getByText("↵").className).toContain("--theme-canvas")
  fireEvent.click(send)
  expect(onClick).toHaveBeenCalledOnce()
})
