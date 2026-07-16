import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { ReviewMessageBody } from "../ReviewMessageBody"

afterEach(cleanup)

describe("ReviewMessageBody", () => {
  it("renders Markdown structure and fenced code", () => {
    const { container } = render(<ReviewMessageBody body={"### Why\n\n- Uses `singleflight`\n\n```diff\n-old\n+new\n```"} />)

    expect(screen.getByRole("heading", { name: "Why" })).toBeTruthy()
    expect(screen.getByRole("list")).toBeTruthy()
    expect(screen.getByText("singleflight").tagName).toBe("CODE")
    expect(screen.getByText(/-old/).closest("pre")).toBeTruthy()
    expect(container.querySelector("pre code.language-diff")).toBeTruthy()
  })

  it("renders plain text without requiring Markdown syntax", () => {
    render(<ReviewMessageBody body="The refresh is single-flight." />)
    expect(screen.getByText("The refresh is single-flight.").tagName).toBe("P")
  })

  it("does not interpret raw HTML", () => {
    const { container } = render(<ReviewMessageBody body={'<script>alert("x")</script>'} />)
    expect(container.querySelector("script")).toBeNull()
    expect(screen.getByText(/<script>/)).toBeTruthy()
  })
})
