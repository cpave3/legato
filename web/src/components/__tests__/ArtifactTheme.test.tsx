import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"
import { SandboxedHtml } from "../SandboxedHtml"
import { setAppTheme } from "../../lib/appTheme"
import { buildStandaloneDocument } from "../../lib/agentUi"

afterEach(async () => { cleanup(); await setAppTheme("legato") })

it("rebuilds an open artifact in the selected theme and saves that same palette", async () => {
  await setAppTheme("legato")
  render(<SandboxedHtml title="Artifact" html="<p>Hello</p>" resourceDomains={[]} />)
  const frame = screen.getByTitle("Artifact")
  expect(frame.getAttribute("srcdoc")).toContain("--color-background-primary: #18181b;")
  await act(() => setAppTheme("ocular-light"))
  const themed = frame.getAttribute("srcdoc")!
  expect(themed).toContain("color-scheme: light;")
  const style = document.documentElement.style
  expect(themed).toContain(`--color-background-primary: ${style.getPropertyValue("--color-lw-surface")};`)
  expect(themed).toContain(`--color-background-tertiary: ${style.getPropertyValue("--color-lw-raised")};`)
  expect(themed).toContain(`--color-border-secondary: ${style.getPropertyValue("--color-lw-line")};`)
  expect(themed).toContain(`--shadow-sm: ${style.getPropertyValue("--shadow-lw-control")};`)
  expect(themed).toContain("--color-text-primary: #252B33;")
  expect(themed).toContain('"darkMode":false')
  expect(themed).toContain('"primaryTextColor":"#252B33"')
  const saved = buildStandaloneDocument({ html: "<p>Hello</p>", title: "Saved", resourceDomains: [] })
  expect(saved).toContain("--color-text-primary: #252B33;")
  expect(saved).toContain(`--color-background-tertiary: ${style.getPropertyValue("--color-lw-raised")};`)
  await act(() => setAppTheme("legato"))
  expect(frame.getAttribute("srcdoc")).toContain("--color-background-primary: #18181b;")
})
