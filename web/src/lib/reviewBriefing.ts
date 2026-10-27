import { apiFetch } from "./api"
import { contentPolicy } from "./agentUi"

// A review briefing is the set of HTML pages a branch carries under
// .review/<branch-slug>/ for the pull request it will become, as the PR
// Review Briefings extension shows them on GitHub. Legato reads them from the
// agent's checkout, so they can be read before the pull request exists.

export interface ReviewBriefingPage {
  // From the repository root: ".review/feat-login/index.html".
  path: string
  folder: string
  label: string
  // Changes when the page is rebuilt.
  version: string
}

export interface ReviewBriefing {
  branch?: string
  base?: string
  pages: ReviewBriefingPage[]
}

export interface ReviewBriefingPageContent {
  path: string
  html: string
  resource_domains: string[]
}

async function readJSON<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null
    throw new Error(body?.error ?? `request failed (${response.status})`)
  }
  return response.json() as Promise<T>
}

export async function fetchReviewBriefing(baseUrl: string, taskId: string): Promise<ReviewBriefing> {
  const data = await readJSON<ReviewBriefing>(await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/review-briefing`))
  return { ...data, pages: data.pages ?? [] }
}

export async function fetchReviewBriefingPage(baseUrl: string, taskId: string, path: string): Promise<ReviewBriefingPageContent> {
  const query = new URLSearchParams({ path })
  return readJSON(await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/review-briefing/page?${query}`))
}

// A briefing page takes a place among the artifact panel's tabs under an ID
// that no item of an agent can have.
const ID_PREFIX = "review:"

export function briefingTabId(path: string): string {
  return ID_PREFIX + path
}

export function briefingPathOf(tabId: string | null | undefined): string | null {
  return tabId?.startsWith(ID_PREFIX) ? tabId.slice(ID_PREFIX.length) : null
}

// What a tab says: "Review" for the overview, "Review · Risks" for another
// page, with its folder when the branch carries more than one.
export function briefingTabLabel(page: ReviewBriefingPage, pages: ReviewBriefingPage[]): string {
  const several = new Set(pages.map((entry) => entry.folder)).size > 1
  const name = page.label === "Overview" ? "" : ` · ${page.label}`
  return several && page.folder ? `Review ${page.folder}${name}` : `Review${name}`
}

// briefingPageOf reads where a link in a page leads, from the page it is on:
// the repository path of another file of the briefing, or null for a link
// that leaves the repository or names no file.
export function briefingPageOf(href: string, fromPage: string): string | null {
  if (!href || /^([a-z][a-z0-9+.-]*:|\/\/|#|\/)/i.test(href)) return null
  const url = new URL(href, `https://repo.invalid/${fromPage}`)
  if (url.host !== "repo.invalid") return null
  return decodeURIComponent(url.pathname.slice(1))
}

// The script a briefing page starts with. A link to another page of the
// briefing asks the panel to open it; a web link asks the host to open it,
// as agent HTML does. A briefing written for the extension finds no
// reviewHost here, so its code links show their path, as the briefing
// itself provides for.
const BRIEFING_BOOTSTRAP = `(function () {
  function post(method, params) { parent.postMessage({ jsonrpc: "2.0", method: method, params: params || {} }, "*") }
  document.addEventListener("click", function (event) {
    if (event.defaultPrevented || event.button !== 0) return
    var link = event.target && event.target.closest ? event.target.closest("a[href]") : null
    if (!link) return
    var href = link.getAttribute("href") || ""
    if (href.charAt(0) === "#") return
    event.preventDefault()
    if (/^https?:/i.test(href)) post("ui/open-link", { url: href })
    else post("legato/open-page", { href: href })
  })
})()`

export interface BriefingDocumentInput {
  html: string
  resourceDomains: string[]
}

// buildBriefingDocument is a briefing page as the sandboxed frame draws it.
// The page is a whole document with its own theme, light and dark, so it is
// drawn as it is, with only the sandbox's content security policy and the
// link script put first in its head: the policy then governs everything the
// page loads, as it does for HTML an agent shows.
export function buildBriefingDocument({ html, resourceDomains }: BriefingDocumentInput): string {
  const lead = `<meta http-equiv="Content-Security-Policy" content="${contentPolicy(resourceDomains)}"><meta name="referrer" content="no-referrer"><script>${BRIEFING_BOOTSTRAP}</script>`
  const head = /<head(\s[^>]*)?>/i.exec(html)
  if (head) return html.slice(0, head.index + head[0].length) + lead + html.slice(head.index + head[0].length)
  const root = /<html(\s[^>]*)?>/i.exec(html)
  if (root) return html.slice(0, root.index + root[0].length) + `<head>${lead}</head>` + html.slice(root.index + root[0].length)
  return `<head>${lead}</head>${html}`
}
