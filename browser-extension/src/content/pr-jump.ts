// Shows a file and line of a pull request on its Files changed page: the
// place an agent cited in a review or a discussion. Ported from the PR Review
// Briefings extension (review-tool), which does the same for its briefings.
//
// GitHub renders diffs lazily, so a #diff- anchor alone often finds nothing.
// The file is revealed through GitHub's own file tree, expanded or loaded when
// it is collapsed or large, and the line is scrolled to and flashed.
export type JumpTarget = { path: string; line?: number; side?: "L" | "R" }
export type JumpResult = { ok: boolean; partial?: boolean; superseded?: boolean; message?: string }
type Timing = { patience?: number }

type JumpWindow = Window & { __legatoJump?: number }

const sleep = (win: Window, ms: number): Promise<void> => new Promise((resolve) => win.setTimeout(resolve, ms))
const cleanPath = (text: string): string => text.replace(/[‎‏⁦-⁩]/g, "").trim()

async function poll<T>(win: Window, read: () => T | null | undefined | false, ms: number): Promise<T | null> {
  const deadline = Date.now() + ms
  do {
    const value = read()
    if (value) return value
    await sleep(win, 50)
  } while (Date.now() < deadline)
  return null
}

function labelled(doc: Document, root: ParentNode, label: string): HTMLButtonElement | undefined {
  return [...root.querySelectorAll<HTMLButtonElement>("button")].find((el) => {
    const labelledBy = el.getAttribute("aria-labelledby")?.split(/\s+/)
      .map((id) => doc.getElementById(id)?.textContent ?? "").join(" ").trim()
    return (labelledBy || el.getAttribute("aria-label")) === label && el.getClientRects().length
  })
}

// The paths the page knows the pull request changes: the file tree's items
// and the headings of rendered diffs.
function knownPaths(doc: Document): string[] {
  const paths = new Set<string>()
  for (const item of doc.querySelectorAll('[role="treeitem"]')) if (item.id) paths.add(item.id)
  for (const heading of doc.querySelectorAll('h3 a[href^="#diff-"]')) paths.add(cleanPath(heading.textContent ?? ""))
  return [...paths]
}

// An agent cites a file by its path in the repository, by its name alone, or
// by its full path in the agent's worktree. Finds which file of the pull
// request that is, or says why it cannot.
function resolvePath(doc: Document, cited: string): { path: string } | { message: string } {
  const known = knownPaths(doc)
  if (known.includes(cited)) return { path: cited }
  const inside = known.filter((path) => cited.endsWith("/" + path)).sort((a, b) => b.length - a.length)
  if (inside.length) return { path: inside[0] }
  const named = known.filter((path) => path.endsWith("/" + cited))
  if (named.length === 1) return { path: named[0] }
  if (named.length > 1) {
    const listed = named.slice(0, 3).join(", ") + (named.length > 3 ? ", …" : "")
    return { message: `More than one file of this pull request is called ${cited}: ${listed}.` }
  }
  return { path: cited.replace(/^\/+/, "") }
}

// The id of a file's diff: GitHub uses "diff-" + sha256(path). The page is
// checked first, so an id GitHub actually rendered wins.
async function resolveId(doc: Document, path: string): Promise<string | null> {
  for (const heading of doc.querySelectorAll('h3 a[href^="#diff-"]')) {
    if (cleanPath(heading.textContent ?? "") === path) return heading.getAttribute("href")!.slice(1)
  }
  const item = [...doc.querySelectorAll('[role="treeitem"]')].find((el) => el.id === path)
  const link = item?.querySelector('a[href^="#diff-"]')
  if (link) return link.getAttribute("href")!.slice(1)
  if (!globalThis.crypto?.subtle) return null
  const digest = await globalThis.crypto.subtle.digest("SHA-256", new TextEncoder().encode(path))
  return "diff-" + [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, "0")).join("")
}

function treeLink(doc: Document, id: string): HTMLElement | null {
  return doc.querySelector(`[role="treeitem"] a[href="#${id}"]`) ?? doc.querySelector(`a[href="#${id}"]`)
}

// Opens collapsed folders in the file tree on the way to a path.
function expandTowards(doc: Document, path: string): boolean {
  let opened = false
  for (const item of doc.querySelectorAll<HTMLElement>('[role="treeitem"][aria-expanded="false"]')) {
    if (item.id && path.startsWith(item.id + "/")) {
      ;(item.querySelector("button") ?? item).click()
      opened = true
    }
  }
  return opened
}

// A line in a rendered diff. GitHub's /changes page (2026) marks each cell
// with data-line-number and data-diff-side and has no per-line id; the forms
// GitHub used before (an id, an attribute, a link) are kept as fallbacks.
export function findLine(container: Element, anchor: string, line: number, side: "L" | "R"): Element | null {
  const wantedSide = side === "L" ? "left" : "right"
  const marked = container.querySelector(`[data-line-number="${line}"][data-diff-side="${wantedSide}"]`)
  if (marked) return marked
  const byId = container.querySelector(`[id="${anchor}"]`)
  if (byId) return byId
  for (const el of container.querySelectorAll("*")) {
    for (const attr of el.attributes) {
      if (attr.value === anchor || attr.value === "#" + anchor) return el
    }
  }
  const cells = [...container.querySelectorAll(`[data-line-number="${line}"]`)]
  if (!cells.length) return null
  const sideOf = (el: Element): string => (el.getAttribute("data-diff-side") || el.getAttribute("data-side") || el.className || "").toString().toLowerCase()
  const wanted = side === "L" ? /left|^l$|deletion|old/ : /right|^r$|addition|new/
  const sided = cells.find((el) => wanted.test(sideOf(el)))
  if (sided) return sided
  // Split rows hold the old number first and the new one last.
  return side === "L" ? cells[0] : cells[cells.length - 1]
}

// Makes a file's lines render: expands a collapsed file (viewed files are
// collapsed; the Viewed mark stays) and presses "Load Diff" on a large or
// generated file. True when it pressed something.
async function openDiff(win: Window, doc: Document, id: string, wait: (ms: number) => number): Promise<boolean> {
  let pressed = false
  const box = doc.getElementById(id)
  const expand = box && labelled(doc, box, "Expand file")
  if (expand) {
    expand.click()
    pressed = true
    await poll(win, () => !labelled(doc, doc.getElementById(id) ?? doc.createElement("div"), "Expand file"), wait(2000))
  }
  const load = await poll(win, () => [...(doc.getElementById(id)?.querySelectorAll("button") ?? [])]
    .find((button) => /^load diff$/i.test(button.textContent?.trim() ?? "")), pressed ? wait(800) : 0)
  if (load) {
    load.click()
    pressed = true
  }
  return pressed
}

// Waits until an element stops moving (GitHub scrolls after a tree click).
async function settle(win: Window, read: () => Element | null, ms: number): Promise<void> {
  const deadline = Date.now() + ms
  let last: number | null = null
  let still = 0
  while (Date.now() < deadline) {
    const top = read()?.getBoundingClientRect().top
    if (top !== undefined && last !== null && Math.abs(top - last) < 1) {
      if (++still >= 3) return
    } else still = 0
    last = top ?? null
    await sleep(win, 100)
  }
}

// Centres the row, and centres it again while files above it finish
// rendering and push it down. Returns once it has stayed put.
async function hold(win: Window, doc: Document, findRow: () => Element | null, current: () => boolean, ms: number): Promise<Element | null> {
  const view = doc.defaultView ?? win
  const deadline = Date.now() + ms
  let last: number | null = null
  let still = 0
  let row: Element | null = null
  while (Date.now() < deadline && current()) {
    row = findRow()
    if (!row) return null
    const top = row.getBoundingClientRect().top
    const height = view.innerHeight || doc.documentElement.clientHeight
    if (top < height * 0.15 || top > height * 0.6) {
      row.scrollIntoView({ block: "center" })
      still = 0
      last = null
    } else {
      if (last !== null && Math.abs(top - last) < 2) {
        if (++still >= 3) return row
      } else still = 0
      last = top
    }
    await sleep(win, 100)
  }
  return row
}

// Tints the row's cells for a moment. Cell shadows paint over GitHub's diff
// colours and move with the row.
function flash(win: Window, row: Element): void {
  const cells = (row.matches("tr") ? [...row.children] : [row]) as HTMLElement[]
  row.setAttribute("data-legato-flash", "")
  for (const cell of cells) {
    cell.style.transition = "box-shadow 0.6s ease"
    cell.style.boxShadow = "inset 0 0 0 999px rgba(183, 156, 247, 0.28), inset 0 2px 0 #b79cf7, inset 0 -2px 0 #b79cf7"
  }
  win.setTimeout(() => {
    for (const cell of cells) cell.style.boxShadow = ""
    row.removeAttribute("data-legato-flash")
  }, 2400)
}

// Reveals a file (and line, on side "R" or "L") on a Files changed page.
export async function jumpToFile(win: JumpWindow, target: JumpTarget, timing: Timing = {}): Promise<JumpResult> {
  const doc = win.document
  const wait = (ms: number): number => Math.round(ms * (timing.patience ?? 1))
  const { line, side = "R" } = target
  // A newer jump takes over: this one stops moving the page.
  const ticket = (win.__legatoJump = (win.__legatoJump ?? 0) + 1)
  const current = (): boolean => win.__legatoJump === ticket
  const resolved = resolvePath(doc, target.path)
  if ("message" in resolved) return { ok: false, message: resolved.message }
  const { path } = resolved
  const id = await resolveId(doc, path)
  if (!id) return { ok: false, message: `Could not work out GitHub's id for ${path}.` }
  // A file already on the page is not clicked in the tree: GitHub's own
  // scroll after that click would land after ours and move the page.
  if (!doc.getElementById(id)) {
    labelled(doc, doc, "Expand file tree")?.click()
    let link = treeLink(doc, id)
    if (!link && expandTowards(doc, path)) link = await poll(win, () => treeLink(doc, id), wait(1500))
    link?.click()
    if (!(await poll(win, () => doc.getElementById(id), wait(5000)))) {
      return {
        ok: false,
        message: `${path} is not in this pull request's changes. A filter may hide it, or the pull request does not change it. If the pull request changed after this page loaded, reload the page.`,
      }
    }
    await settle(win, () => doc.getElementById(id), wait(2000))
  }
  const container = (): HTMLElement | null => doc.getElementById(id)
  if (!line) {
    container()?.scrollIntoView({ block: "start" })
    return { ok: true }
  }
  const anchor = `${id}${side}${line}`
  const findRow = (): Element | null => {
    const box = container()
    const cell = box && findLine(box, anchor, line, side)
    return cell ? cell.closest("tr") ?? cell : null
  }
  // Lines can be missing for three reasons: the file is collapsed, it waits
  // behind "Load Diff", or GitHub has not rendered it yet because it is far
  // from the screen. The last one shows up once the file is scrolled to, and
  // can then turn out to need "Load Diff" too.
  let row = findRow()
  for (let attempt = 0; !row && attempt < 2; attempt++) {
    if (await openDiff(win, doc, id, wait)) row = await poll(win, findRow, wait(8000))
    else if (attempt === 0) {
      container()?.scrollIntoView({ block: "start" })
      row = await poll(win, findRow, wait(2500))
    }
  }
  if (!current()) return { ok: true, superseded: true }
  if (!row) {
    container()?.scrollIntoView({ block: "start" })
    return {
      ok: true,
      partial: true,
      message: `Line ${line} of ${path} is not in this diff: GitHub shows only the changed lines and a little around them. Showing the file.`,
    }
  }
  row = (await hold(win, doc, findRow, current, wait(3000))) ?? row
  if (!current()) return { ok: true, superseded: true }
  flash(win, row)
  return { ok: true }
}

// ---- Getting to the Files changed page --------------------------------------

const FILES_PATH = /^\/[^/]+\/[^/]+\/pull\/\d+\/(changes|files)\/?$/
const PENDING = "legato:pendingJump"
const prOf = (href: string): RegExpExecArray | null => /^\/([^/]+)\/([^/]+)\/pull\/(\d+)/.exec(new URL(href).pathname)

export function onFilesPage(win: Window): boolean {
  const doc = win.document
  return FILES_PATH.test(win.location.pathname)
    && !!(doc.getElementById("diff-comparison-viewer-container") ?? doc.querySelector('[id^="diff-"], #files'))
}

// Gets to the pull request's Files changed page: through GitHub's own tab
// when it is there (no reload), otherwise by loading it, after which the
// jump resumes from sessionStorage. False when the page is loading.
export async function ensureFilesPage(win: Window, target: JumpTarget): Promise<boolean> {
  if (onFilesPage(win)) return true
  const pr = prOf(win.location.href)
  if (!pr) return false
  // Saved first: if the tab click loads a new page, that page resumes it.
  win.sessionStorage.setItem(PENDING, JSON.stringify(target))
  const tab = win.document.querySelector<HTMLAnchorElement>("#prs-files-anchor-tab, a[href$='/changes'], a[href$='/files']")
  if (tab && prOf(new URL(tab.href, win.location.href).href)?.[3] === pr[3]) {
    tab.click()
    if (await poll(win, () => onFilesPage(win), 8000)) {
      win.sessionStorage.removeItem(PENDING)
      return true
    }
  }
  win.location.assign(`/${pr[1]}/${pr[2]}/pull/${pr[3]}/changes`)
  return false
}

// A jump that had to load the Files changed page first.
export async function resumePendingJump(win: Window): Promise<JumpResult | null> {
  const raw = win.sessionStorage.getItem(PENDING)
  if (!raw) return null
  win.sessionStorage.removeItem(PENDING)
  if (!(await poll(win, () => onFilesPage(win), 15000))) return null
  return jumpToFile(win, JSON.parse(raw) as JumpTarget)
}
