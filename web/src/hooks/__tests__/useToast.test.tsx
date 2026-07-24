import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ToastProvider, useToast } from "../useToast"

function ToastControls() {
  const { addToast } = useToast()
  return (
    <>
      <button onClick={() => addToast("Saved", "success")}>Success</button>
      <button onClick={() => addToast("Failed", "error")}>Error</button>
    </>
  )
}

describe("ToastProvider", () => {
  it("announces success and error feedback with appropriate live roles", () => {
    render(
      <ToastProvider>
        <ToastControls />
      </ToastProvider>
    )

    fireEvent.click(screen.getByRole("button", { name: "Success" }))
    fireEvent.click(screen.getByRole("button", { name: "Error" }))

    expect(screen.getByRole("status").textContent).toBe("Saved")
    expect(screen.getByRole("alert").textContent).toBe("Failed")
  })
})
