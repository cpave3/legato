import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { useConfirm } from "../../../hooks/useConfirm"

function Harness({ onAnswer }: { onAnswer: (answer: boolean) => void }) {
  const { confirm, dialog } = useConfirm()
  return (
    <>
      <button onClick={() => { void confirm({ title: "Kill agent", message: "Kill this agent session?", confirmLabel: "Kill", destructive: true }).then(onAnswer) }}>ask</button>
      {dialog}
    </>
  )
}

afterEach(cleanup)

describe("useConfirm", () => {
  it("resolves true when confirmed", async () => {
    const answers: boolean[] = []
    render(<Harness onAnswer={(a) => answers.push(a)} />)
    fireEvent.click(screen.getByText("ask"))
    expect(screen.getByRole("dialog", { name: "Kill agent" })).toBeTruthy()
    expect(screen.getByText("Kill this agent session?")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Kill" }))
    await waitFor(() => expect(answers).toEqual([true]))
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("resolves false on Cancel and on Escape", async () => {
    const answers: boolean[] = []
    render(<Harness onAnswer={(a) => answers.push(a)} />)
    fireEvent.click(screen.getByText("ask"))
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(answers).toEqual([false]))

    fireEvent.click(screen.getByText("ask"))
    fireEvent.keyDown(document, { key: "Escape" })
    await waitFor(() => expect(answers).toEqual([false, false]))
    expect(screen.queryByRole("dialog")).toBeNull()
  })
})
