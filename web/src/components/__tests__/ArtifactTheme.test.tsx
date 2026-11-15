import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"
import { SandboxedHtml } from "../SandboxedHtml"
import { setAppTheme } from "../../lib/appTheme"
import { buildStandaloneDocument } from "../../lib/agentUi"

afterEach(async () => { cleanup(); await setAppTheme("legato") })

it.each([
  { id: "ocular-light", polarity: "light", text: "#252B33" },
  { id: "ocular-light-vivid", polarity: "light", text: "#252B33" },
  { id: "ocular-dark-vivid", polarity: "dark", text: "#E2E5E9" },
] as const)("rebuilds an open artifact in $id and saves that same palette", async ({ id, polarity, text }) => {
  await setAppTheme("legato")
  render(<SandboxedHtml title="Artifact" html="<p>Hello</p>" resourceDomains={[]} />)
  const frame = screen.getByTitle("Artifact")
  expect(frame.getAttribute("srcdoc")).toContain("--color-background-primary: #18181b;")
  await act(() => setAppTheme(id))
  const themed = frame.getAttribute("srcdoc")!
  expect(themed).toContain(`color-scheme: ${polarity};`)
  const style = document.documentElement.style
  expect(themed).toContain(`--color-background-primary: ${style.getPropertyValue("--color-lw-surface")};`)
  expect(themed).toContain(`--color-background-tertiary: ${style.getPropertyValue("--color-lw-raised")};`)
  expect(themed).toContain(`--color-border-secondary: ${style.getPropertyValue("--color-lw-line")};`)
  expect(themed).toContain(`--shadow-sm: ${style.getPropertyValue("--shadow-lw-control")};`)
  expect(themed).toContain(`--color-text-primary: ${text};`)
  expect(themed).toContain(`"darkMode":${polarity === "dark"}`)
  expect(themed).toContain(`"primaryTextColor":"${text}"`)
  const saved = buildStandaloneDocument({ html: "<p>Hello</p>", title: "Saved", resourceDomains: [] })
  expect(saved).toContain(`--color-ring-primary: ${style.getPropertyValue("--color-lw-accent")};`)
  expect(saved).toContain(`--color-text-primary: ${text};`)
  expect(saved).toContain(`--color-background-tertiary: ${style.getPropertyValue("--color-lw-raised")};`)
  await act(() => setAppTheme("legato"))
  expect(frame.getAttribute("srcdoc")).toContain("--color-background-primary: #18181b;")
})
