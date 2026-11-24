import { chromium } from "playwright"
const [base, out, path] = process.argv.slice(2)
const macros = {
  macros: [
    { name: "Plan", keys: "Plan it.\n", source: "builtin", builtin: true, applies_to: "task" },
    { name: "Implement", keys: "Implement.\n", source: "builtin", builtin: true, applies_to: "task" },
    { name: "Review", keys: "Review.\n", source: "builtin", builtin: true, applies_to: "pr" },
    { name: "Address feedback", keys: "Address.\n", source: "builtin", builtin: true, applies_to: "pr" },
    { name: "Fix CI", keys: "Fix CI.\n", source: "database", applies_to: "task", favorite: true },
    { name: "Rebase and repush", keys: "Rebase.\n", source: "database", applies_to: "task", favorite: true },
    { name: "Ticket to PR", keys: "Ticket.\n", source: "database" },
  ],
  suggested: { task: "Plan", my_pr: "Address feedback", others_pr: "Review" }, tiers: {},
}
const item = {
  id: "att-1", producer: "workflows", source_key: "wr-1:merged", source_revision: "merge:1",
  task_id: "LEG-612", title: "Review and merge PR #318", reason: "PR #318 conflicts with main · CI failing",
  category: "workflow", urgency: "normal", signal_kind: "workflow_merge_waiting", status: "open",
  navigation: { kind: "workflow_run", target: "wr-1" },
  actions: [
    { id: "open", label: "Open run", kind: "navigate", intent: "primary" },
    { id: "open_pr", label: "Open PR", kind: "navigate", href: "https://github.com/cpave3/legato/pull/318" },
    { id: "resume_agent", label: "Resume agent", kind: "execute", picks: "macro" },
  ],
  created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
}
const browser = await chromium.launch({ executablePath: process.env.CHROME })
const page = await browser.newPage({ viewport: { width: 1280, height: 760 }, colorScheme: "dark" })
const seen = new Set()
await page.route(url => new URL(url).pathname.startsWith("/api/") || new URL(url).pathname === "/health", route => {
  const u = new URL(route.request().url())
  seen.add(u.pathname)
  const json = body => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) })
  if (u.pathname === "/api/macros") return json(macros)
  if (u.pathname === "/api/attention") return json({ items: [item], hidden_groups: 0 })
  return route.fulfill({ status: 404, contentType: "application/json", body: '{"error":"not mocked"}' })
})
await page.goto(base + path)
await page.getByRole("button", { name: "Resume agent…" }).first().click()
await page.getByRole("menuitem", { name: "Rebase and repush" }).waitFor()
await page.waitForTimeout(300)
await page.screenshot({ path: out + "-favourites.png" })
await page.getByRole("menuitem", { name: /Show all/ }).click()
await page.waitForTimeout(200)
await page.screenshot({ path: out + "-all.png" })
console.log([...seen].join(" "))
await browser.close()
