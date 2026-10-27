import { describe, expect, it } from "vitest"
import { briefingPageOf, briefingTabLabel, buildBriefingDocument, type ReviewBriefingPage } from "../reviewBriefing"

const policyOf = (document: string) => /http-equiv="Content-Security-Policy" content="([^"]*)"/.exec(document)?.[1] ?? ""

describe("buildBriefingDocument", () => {
  it("puts the sandbox policy first in the page's own head and keeps the page's own look", () => {
    const page = `<!doctype html><html lang="en"><head><meta charset="utf-8"><style>:root { --bg: white }</style></head><body><h1>Login</h1></body></html>`
    const built = buildBriefingDocument({ html: page, resourceDomains: ["cdn.jsdelivr.net", "not a host"] })

    // The policy comes before anything the page loads, so it governs all of it.
    expect(built.indexOf("Content-Security-Policy")).toBeLessThan(built.indexOf("<style>:root { --bg: white }"))
    expect(built.startsWith(`<!doctype html><html lang="en"><head><meta http-equiv="Content-Security-Policy"`)).toBe(true)
    const policy = policyOf(built)
    expect(policy).toContain("connect-src 'none'")
    expect(policy).toContain("script-src 'unsafe-inline' https://cdn.jsdelivr.net")
    expect(policy).not.toContain("not a host")
    // A briefing has its own theme, light and dark; Legato's is not added.
    expect(built).not.toContain("--color-background-primary")
    expect(built).toContain("<h1>Login</h1>")
  })

  it("gives a page with no head one", () => {
    const built = buildBriefingDocument({ html: "<p>bare</p>", resourceDomains: [] })
    expect(built.startsWith("<head><meta http-equiv=\"Content-Security-Policy\"")).toBe(true)
    expect(built.endsWith("<p>bare</p>")).toBe(true)
  })
})

describe("briefingPageOf", () => {
  it("reads a link to another page from the page it is on", () => {
    expect(briefingPageOf("risks.html", ".review/feat-login/index.html")).toBe(".review/feat-login/risks.html")
    expect(briefingPageOf("../other/index.html#top", ".review/feat-login/index.html")).toBe(".review/other/index.html")
    expect(briefingPageOf("deep/data%20flow.html", ".review/feat-login/index.html")).toBe(".review/feat-login/deep/data flow.html")
  })

  it("names no page for a link that is absolute or an anchor", () => {
    for (const href of ["https://github.com", "//cdn.example.com/x", "/etc/passwd", "#top", "mailto:me@example.com", ""]) {
      expect(briefingPageOf(href, ".review/feat-login/index.html")).toBeNull()
    }
  })
})

describe("briefingTabLabel", () => {
  const page = (folder: string, label: string): ReviewBriefingPage => ({ path: `.review/${folder}/${label}.html`, folder, label, version: "v" })

  it("calls the overview Review and names the other pages", () => {
    const pages = [page("feat-login", "Overview"), page("feat-login", "Risk notes")]
    expect(pages.map((entry) => briefingTabLabel(entry, pages))).toEqual(["Review", "Review · Risk notes"])
  })

  it("names the folder when the branch carries more than one", () => {
    const pages = [page("feat-login", "Overview"), page("another", "Overview")]
    expect(pages.map((entry) => briefingTabLabel(entry, pages))).toEqual(["Review feat-login", "Review another"])
  })
})
