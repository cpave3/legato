import type { ClearedStore, DraftStore } from "../scope-drafts"
import { MAX_PINS_PER_BATCH, type Box, type PinTarget, type ScopeBatchResult, type ScopeDraft, type ScopePin, type SentPin, type SentPinState } from "../scope-types"
import { LEGATO_MONO, LEGATO_SANS, loadLegatoFonts } from "./fonts"

// screenshotError says why a pin has no screenshot when Chrome refused one;
// the pin is kept without it.
export type CapturedPin = { pin: Omit<ScopePin, "comment" | "screenshot">; screenshot?: string; screenshotError?: string }

export type ScopeOverlayDeps = {
  store: DraftStore
  // The pinned element's context, screenshot included. The overlay is hidden
  // while it runs.
  capture: (element: Element) => Promise<CapturedPin>
  // The element a saved target names on this page, or null.
  locate: (target: PinTarget) => Element | null
  newId: () => string
  now: () => string
  // Sending drafts and acting on sent pins, through the service worker.
  sent?: {
    send: (pins: ScopePin[]) => Promise<ScopeBatchResult>
    reopen: (id: string, comment: string) => Promise<void>
    withdraw: (id: string) => Promise<void>
    cleared: ClearedStore
  }
}

export type ScopeOverlay = {
  host: HTMLElement
  root: ShadowRoot
  // Turns pin mode on or off, or toggles it; answers whether it is on.
  setPinMode: (on?: boolean) => boolean
  pinMode: () => boolean
  // Reads the drafts again and redraws (after a change in another tab, or a
  // navigation within the site).
  refresh: () => Promise<void>
  // Finds lost markers' elements again after the page changed.
  relocate: () => void
  // The site's sent pins, as Legato last reported them.
  setSent: (pins: SentPin[]) => void
  setSentError: (message: string) => void
  destroy: () => void
}

// What the person interacts with: a marker on the page, and the element it
// is on (null when it cannot be found any more).
type Marker = { key: string; label: string; anchor: Element | null; box: Box; button: HTMLButtonElement; target?: PinTarget }

const BLOCKED = ["pointerdown", "mousedown", "pointerup", "mouseup", "click", "dblclick", "auxclick"]

// The web app's lw- colours (web/src/index.css) and Geist, as in legato-fab.ts.
const STYLE = `
:host { all: initial; }
* { box-sizing: border-box; }
.layer { position: fixed; inset: 0; pointer-events: none; z-index: 2147483647;
  --lw-ground: #111114; --lw-surface: #17171b; --lw-raised: #1d1d22; --lw-line: #26262d; --lw-line-strong: #31313a;
  --lw-text: #e6e5eb; --lw-text-2: #9a99a5; --lw-faint: #66656f; --lw-accent: #6d5ef6; --lw-accent-ring: #4f46b8;
  --lw-accent-text: #c8c0ff; --lw-live: #3ecf8e; --lw-warn: #f0b860; --lw-error: #f0646b;
  font-family: ${LEGATO_SANS}, "Geist Sans", Geist, Inter, ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  color: var(--lw-text); font-size: 13px; line-height: 1.4; -webkit-font-smoothing: antialiased; }
.mono { font-family: ${LEGATO_MONO}, "Geist Mono", ui-monospace, SFMono-Regular, Menlo, monospace; }
.outline { position: fixed; display: none; border: 2px solid var(--lw-accent); background: rgba(109, 94, 246, .12);
  border-radius: 3px; pointer-events: none; }
.marker { position: fixed; pointer-events: auto; width: 22px; height: 22px; padding: 0; border-radius: 50% 50% 50% 4px;
  border: 2px solid #fff; background: var(--lw-accent); color: #fff; font: 600 11px/18px inherit; font-family: inherit;
  text-align: center; cursor: pointer; box-shadow: 0 2px 8px rgba(0,0,0,.45); }
.marker[data-lost="true"] { background: var(--lw-faint); border-style: dashed; }
.marker[data-pending="true"] { background: var(--lw-accent-ring); }
.panel { pointer-events: auto; background: var(--lw-surface); border: 1px solid var(--lw-line); border-radius: 10px;
  box-shadow: 0 12px 32px rgba(0,0,0,.5); color: var(--lw-text); }
.popover { position: fixed; width: 280px; padding: 10px; display: flex; flex-direction: column; gap: 8px; }
.popover .meta { color: var(--lw-text-2); font-size: 11px; overflow-wrap: anywhere; }
textarea { width: 100%; min-height: 64px; resize: vertical; padding: 6px 8px; border-radius: 6px; border: 1px solid var(--lw-line-strong);
  background: var(--lw-ground); color: var(--lw-text); font: inherit; }
textarea:focus { outline: 2px solid var(--lw-accent-ring); outline-offset: 0; }
.row { display: flex; gap: 6px; align-items: center; justify-content: flex-end; flex-wrap: wrap; }
button.act { min-height: 28px; padding: 4px 10px; border-radius: 6px; border: 1px solid var(--lw-line-strong); background: transparent;
  color: var(--lw-text); font: 500 12px/1.2 inherit; font-family: inherit; cursor: pointer; }
button.act:hover { background: var(--lw-raised); }
button.act.accent { background: var(--lw-accent); border-color: transparent; color: #fff; }
button.act.accent:hover { filter: brightness(1.1); }
button.act[aria-pressed="true"] { border-color: var(--lw-accent); color: var(--lw-accent-text); }
button.act:disabled { opacity: .5; cursor: default; }
.tray { position: fixed; right: 16px; bottom: 16px; width: min(360px, calc(100vw - 32px)); max-height: min(70vh, 560px);
  display: none; flex-direction: column; overflow: hidden; }
.tray[data-visible="true"] { display: flex; }
.tray header { display: flex; align-items: center; gap: 8px; padding: 8px 10px; border-bottom: 1px solid var(--lw-line); }
.tray[data-collapsed="true"] header { border-bottom: 0; }
.tray[data-collapsed="true"] .body { display: none; }
.mark { display: grid; place-items: center; width: 20px; height: 20px; border-radius: 5px; background: var(--lw-accent); color: #fff; font-weight: 600; }
.title { flex: 1; font-weight: 600; }
.title small { color: var(--lw-text-2); font-weight: 400; }
.body { overflow: auto; padding: 8px 10px; display: flex; flex-direction: column; gap: 8px; }
.empty { color: var(--lw-text-2); }
.draft { display: grid; grid-template-columns: 22px 1fr; gap: 4px 8px; padding-bottom: 8px; border-bottom: 1px solid var(--lw-line); }
.draft .num { width: 20px; height: 20px; border-radius: 50%; background: var(--lw-accent); color: #fff; font-size: 11px; font-weight: 600; text-align: center; line-height: 20px; }
.draft .page { color: var(--lw-text-2); font-size: 11px; overflow-wrap: anywhere; }
.draft textarea, .draft .row { grid-column: 2; }
.draft textarea { min-height: 44px; }
.status { white-space: pre-line; overflow-wrap: anywhere; color: var(--lw-text-2); font-size: 12px; }
.status[data-kind="error"] { color: var(--lw-error); }
.status[data-kind="success"] { color: var(--lw-live); }
.status:empty { display: none; }
.marker[data-state] { border-radius: 50%; font-size: 12px; }
.marker[data-state="sent"] { background: var(--lw-accent-ring); }
.marker[data-state="delivered"] { background: var(--lw-accent); }
.marker[data-state="fixed"] { background: var(--lw-live); color: #0b2a1c; }
.marker[data-state="wont_fix"] { background: var(--lw-faint); }
.section { display: flex; align-items: center; gap: 8px; margin-top: 4px; color: var(--lw-text-2); font-size: 11px;
  text-transform: uppercase; letter-spacing: .04em; }
.section span { flex: 1; }
.sent { display: flex; flex-direction: column; gap: 4px; padding-bottom: 8px; border-bottom: 1px solid var(--lw-line); }
.sent .head { display: flex; gap: 8px; align-items: center; }
.chip { padding: 1px 6px; border-radius: 999px; font-size: 11px; border: 1px solid var(--lw-line-strong); color: var(--lw-text-2); }
.chip[data-state="delivered"] { color: var(--lw-accent-text); border-color: var(--lw-accent-ring); }
.chip[data-state="fixed"] { color: var(--lw-live); border-color: var(--lw-live); }
.sent .page { flex: 1; color: var(--lw-text-2); font-size: 11px; overflow-wrap: anywhere; }
.sent .comment, .popover .comment { overflow-wrap: anywhere; }
.note { padding: 4px 8px; border-left: 2px solid var(--lw-accent-ring); color: var(--lw-text-2); overflow-wrap: anywhere; }
.send { display: flex; justify-content: flex-end; }
`

const RESOLVED = new Set<SentPinState>(["fixed", "wont_fix"])
const STATE_LABELS: Record<SentPinState, string> = { sent: "Sent", delivered: "Delivered", fixed: "Fixed", wont_fix: "Won't fix" }
const STATE_GLYPHS: Record<SentPinState, string> = { sent: "↑", delivered: "→", fixed: "✓", wont_fix: "✕" }

function pathOf(url: string): string {
  try {
    const parsed = new URL(url)
    return `${parsed.pathname}${parsed.search}`
  } catch {
    return url
  }
}

export function createScopeOverlay(doc: Document, win: Window, deps: ScopeOverlayDeps): ScopeOverlay {
  loadLegatoFonts(doc)
  const host = doc.createElement("legato-scope")
  host.style.cssText = "all: initial; position: fixed; inset: 0; pointer-events: none; z-index: 2147483647;"
  const root = host.attachShadow({ mode: "closed" })
  const style = doc.createElement("style")
  style.textContent = STYLE
  const layer = doc.createElement("div")
  layer.className = "layer"
  const outline = doc.createElement("div")
  outline.className = "outline"
  const markers = doc.createElement("div")
  root.append(style, layer)
  layer.append(outline, markers)

  const el = <K extends keyof HTMLElementTagNameMap>(tag: K, className = "", text = ""): HTMLElementTagNameMap[K] => {
    const node = doc.createElement(tag)
    if (className) node.className = className
    if (text) node.textContent = text
    return node
  }
  const button = (label: string, action: string, accent = false): HTMLButtonElement => {
    const node = el("button", accent ? "act accent" : "act", label)
    node.type = "button"
    node.dataset.action = action
    return node
  }

  // The tray: every draft of the site, with pin mode's switch.
  const tray = el("section", "tray panel")
  tray.setAttribute("aria-label", "Legato Scope")
  tray.dataset.collapsed = "false"
  const header = el("header")
  const mark = el("span", "mark", "♩")
  mark.setAttribute("aria-hidden", "true")
  const title = el("span", "title")
  const pinToggle = button("Pin", "pin-mode")
  pinToggle.title = "Pin mode: click an element to pin it (Esc to stop)"
  const collapse = button("▾", "collapse")
  collapse.setAttribute("aria-label", "Collapse")
  header.append(mark, title, pinToggle, collapse)
  const body = el("div", "body")
  const list = el("div", "drafts")
  const status = el("div", "status")
  status.setAttribute("role", "status")
  const sendRow = el("div", "send")
  const sendButton = button("Send", "send", true)
  sendRow.append(sendButton)
  const sentHeader = el("div", "section")
  const clearResolved = button("Clear resolved", "clear-resolved")
  sentHeader.append(el("span", "", "Sent"), clearResolved)
  const sentList = el("div", "sent-list")
  body.append(list, sendRow, sentHeader, sentList, status)
  tray.append(header, body)
  layer.append(tray)

  let pinning = false
  let drafts: ScopeDraft[] = []
  // Changes to drafts made in the panel and not stored yet: Send waits for
  // them, so it never sends a comment older than the one shown.
  const edits = new Set<Promise<unknown>>()
  const track = (change: Promise<unknown>): void => {
    edits.add(change)
    void change.catch(() => undefined).finally(() => edits.delete(change))
  }
  let markerList: Marker[] = []
  let popover: HTMLElement | null = null
  let pending: { captured: CapturedPin; element: Element; marker: Marker } | null = null
  let destroyed = false
  let sentPins: SentPin[] = []
  let cleared = new Set<string>()
  let sending = false

  const ours = (event: Event): boolean => event.composedPath().includes(host)
  const targetOf = (event: Event): Element | null => {
    const isElement = (node: unknown): node is Element => (node as Node | null)?.nodeType === 1
    const first = event.composedPath()[0]
    return isElement(first) ? first : isElement(event.target) ? event.target : null
  }

  // Where a marker goes: the top left corner of its element, or its saved
  // place in the page when the element is gone.
  const place = (marker: Marker): void => {
    let left: number
    let top: number
    if (marker.anchor?.isConnected) {
      const rect = marker.anchor.getBoundingClientRect()
      left = rect.left
      top = rect.top
      marker.button.dataset.lost = "false"
    } else {
      left = marker.box.x - win.scrollX
      top = marker.box.y - win.scrollY
      marker.button.dataset.lost = "true"
    }
    marker.button.style.left = `${Math.round(left - 10)}px`
    marker.button.style.top = `${Math.round(top - 10)}px`
  }
  const reposition = (): void => { for (const marker of markerList) place(marker) }
  let frame = false
  const schedule = (): void => {
    if (frame || destroyed) return
    frame = true
    const run = () => { frame = false; reposition() }
    if (typeof win.requestAnimationFrame === "function") win.requestAnimationFrame(run)
    else win.setTimeout(run, 16)
  }

  // Closing drops a pin being placed, unless it is the one being opened.
  const closePopover = (keep?: Marker): void => {
    popover?.remove()
    popover = null
    if (pending && pending.marker !== keep) {
      pending.marker.button.remove()
      markerList = markerList.filter((marker) => marker !== pending!.marker)
      pending = null
    }
  }

  const openPopover = (marker: Marker, options: { comment: string; meta: string; onSave: (comment: string) => Promise<void>; onDelete?: () => Promise<void> }): void => {
    closePopover(marker)
    const panel = el("div", "popover panel")
    panel.setAttribute("role", "dialog")
    panel.setAttribute("aria-label", `Pin ${marker.label}`)
    const meta = el("div", "meta mono", options.meta)
    const text = el("textarea")
    text.placeholder = "What should change here?"
    text.value = options.comment
    const row = el("div", "row")
    const save = button("Save", "save", true)
    const cancel = button("Cancel", "cancel")
    save.disabled = !text.value.trim()
    text.addEventListener("input", () => { save.disabled = !text.value.trim() })
    text.addEventListener("keydown", (event) => {
      if (event.key === "Enter" && (event.metaKey || event.ctrlKey) && !save.disabled) save.click()
      if (event.key === "Escape") { event.stopPropagation(); cancel.click() }
    })
    save.addEventListener("click", () => {
      const comment = text.value.trim()
      if (!comment) return
      save.disabled = true
      void options.onSave(comment).then(() => {
        if (pending?.marker === marker) pending = null
        closePopover()
        return refresh()
      })
    })
    cancel.addEventListener("click", () => closePopover())
    if (options.onDelete) {
      const remove = button("Delete", "delete")
      remove.addEventListener("click", () => { void options.onDelete!().then(() => { closePopover(); return refresh() }) })
      row.append(remove)
    }
    row.append(cancel, save)
    panel.append(meta, text, row)
    layer.append(panel)
    popover = panel
    const rect = marker.button.getBoundingClientRect()
    const left = Math.max(8, Math.min(rect.left + 28, win.innerWidth - 288))
    const top = Math.max(8, Math.min(rect.top, win.innerHeight - 180))
    panel.style.left = `${left}px`
    panel.style.top = `${top}px`
    text.focus()
  }

  const makeMarker = (key: string, label: string, anchor: Element | null, box: Box, target?: PinTarget): Marker => {
    const node = el("button", "marker", label)
    node.type = "button"
    const marker: Marker = { key, label, anchor, box, button: node, target }
    markers.append(node)
    markerList.push(marker)
    place(marker)
    return marker
  }

  const capturePin = async (element: Element): Promise<void> => {
    outline.style.display = "none"
    host.style.visibility = "hidden"
    let captured: CapturedPin
    try {
      captured = await deps.capture(element)
    } catch (error) {
      say(error instanceof Error ? error.message : String(error), "error")
      return
    } finally {
      host.style.visibility = ""
    }
    if (destroyed) return
    if (captured.screenshotError) say(`No screenshot: ${captured.screenshotError}`, "error")
    const marker = makeMarker("pending", String(drafts.length + 1), element, captured.pin.target.box)
    marker.button.dataset.pending = "true"
    pending = { captured, element, marker }
    openPopover(marker, {
      comment: "",
      meta: describe(element),
      onSave: async (comment) => {
        const id = deps.newId()
        await deps.store.add({
          id,
          createdAt: deps.now(),
          hasScreenshot: false,
          pin: { ...captured.pin, comment },
        }, captured.screenshot || undefined)
      },
    })
  }

  const describe = (element: Element): string => {
    const tag = element.tagName.toLowerCase()
    const id = element.id ? `#${element.id}` : ""
    const classes = [...element.classList].slice(0, 2).map((name) => `.${name}`).join("")
    return `${tag}${id}${classes}`
  }

  const block = (event: Event): void => {
    if (!pinning || ours(event)) return
    event.preventDefault()
    event.stopImmediatePropagation()
    if (event.type !== "click" || popover) return
    const target = targetOf(event)
    if (target) void capturePin(target)
  }
  const hover = (event: Event): void => {
    if (!pinning || popover) return
    if (ours(event)) { outline.style.display = "none"; return }
    const target = targetOf(event)
    if (!target) return
    const rect = target.getBoundingClientRect()
    outline.style.display = "block"
    outline.style.left = `${rect.left}px`
    outline.style.top = `${rect.top}px`
    outline.style.width = `${rect.width}px`
    outline.style.height = `${rect.height}px`
  }
  const keydown = (event: KeyboardEvent): void => {
    if (event.key !== "Escape") return
    if (popover) { closePopover(); return }
    if (pinning) setPinMode(false)
  }

  for (const type of BLOCKED) win.addEventListener(type, block, { capture: true })
  win.addEventListener("pointermove", hover, { capture: true, passive: true })
  win.addEventListener("keydown", keydown, { capture: true })
  win.addEventListener("scroll", schedule, { capture: true, passive: true })
  win.addEventListener("resize", schedule, { passive: true })

  // A quiet message is shown when the tray is, but does not open it: Legato
  // being unreachable is news on a page only when there is something to send.
  const say = (message: string, kind: "info" | "error" | "success" = "info", quiet = false): void => {
    status.dataset.quiet = String(quiet)
    status.textContent = message
    status.dataset.kind = kind
    renderTray()
  }

  const visibleSent = (): SentPin[] => sentPins.filter((pin) => !(RESOLVED.has(pin.state) && cleared.has(pin.id)))

  const renderTray = (): void => {
    const count = drafts.length
    const shown = visibleSent()
    const batch = Math.min(count, MAX_PINS_PER_BATCH)
    sendRow.style.display = deps.sent && count > 0 ? "flex" : "none"
    sendButton.textContent = sending ? "Sending…" : batch === 1 ? "Send 1 pin" : `Send ${batch} pins`
    sendButton.disabled = sending || count === 0
    sentHeader.style.display = shown.length > 0 ? "flex" : "none"
    clearResolved.style.display = shown.some((pin) => RESOLVED.has(pin.state)) ? "inline-block" : "none"
    sentList.replaceChildren(...shown.map((pin) => sentRow(pin)))
    title.replaceChildren("Scope ", el("small", "", count === 1 ? "· 1 draft" : `· ${count} drafts`))
    pinToggle.setAttribute("aria-pressed", String(pinning))
    pinToggle.textContent = pinning ? "Pinning" : "Pin"
    tray.dataset.visible = String(pinning || count > 0 || shown.length > 0 || (Boolean(status.textContent) && status.dataset.quiet !== "true"))
    const rows = new Map([...list.querySelectorAll<HTMLElement>(".draft")].map((row) => [row.dataset.id!, row]))
    const next: HTMLElement[] = []
    drafts.forEach((draft, index) => {
      let row = rows.get(draft.id)
      if (!row) {
        row = el("div", "draft")
        row.dataset.id = draft.id
        const num = el("span", "num")
        const page = el("span", "page mono")
        const text = el("textarea")
        text.setAttribute("aria-label", "Comment")
        text.addEventListener("change", () => {
          const comment = text.value.trim()
          if (comment) track(deps.store.update(draft.id, comment).then(refresh))
        })
        const actions = el("div", "row")
        const remove = button("Delete", "delete")
        remove.addEventListener("click", () => { track(deps.store.remove(draft.id).then(refresh)) })
        actions.append(remove)
        row.append(num, page, text, actions)
      }
      row.querySelector(".num")!.textContent = String(index + 1)
      row.querySelector(".page")!.textContent = pathOf(draft.pin.page.url)
      const text = row.querySelector("textarea")!
      if (root.activeElement !== text) text.value = draft.pin.comment
      next.push(row)
    })
    if (next.length === 0) next.push(el("p", "empty", pinning ? "Click an element on the page to pin it." : "No drafts."))
    list.replaceChildren(...next)
  }

  const renderMarkers = (): void => {
    const kept = pending ? [pending.marker] : []
    for (const marker of markerList) if (!kept.includes(marker)) marker.button.remove()
    markerList = kept
    const here = pathOf(win.location.href)
    drafts.forEach((draft, index) => {
      if (pathOf(draft.pin.page.url) !== here) return
      const marker = makeMarker(draft.id, String(index + 1), deps.locate(draft.pin.target), draft.pin.target.box, draft.pin.target)
      marker.button.setAttribute("aria-label", `Pin ${index + 1}: ${draft.pin.comment}`)
      marker.button.title = draft.pin.comment
      marker.button.addEventListener("click", () => openPopover(marker, {
        comment: draft.pin.comment,
        meta: draft.pin.target.selectors[0] ?? "",
        onSave: (comment) => deps.store.update(draft.id, comment),
        onDelete: () => deps.store.remove(draft.id),
      }))
    })
    renderSentMarkers()
  }

  // A sent pin's popover: its comment and the agent's note, and what can be
  // done with it in its state.
  const openSentPopover = (pin: SentPin, anchor: Element): void => {
    closePopover()
    const panel = el("div", "popover panel")
    panel.setAttribute("role", "dialog")
    panel.setAttribute("aria-label", `Sent pin: ${STATE_LABELS[pin.state]}`)
    const meta = el("div", "meta mono", `${STATE_LABELS[pin.state]} · ${pathOf(pin.url)}`)
    panel.append(meta, el("div", "comment", pin.comment))
    if (pin.note) panel.append(el("div", "note", pin.note))
    const row = el("div", "row")
    const close = button("Close", "cancel")
    close.addEventListener("click", () => closePopover())
    if (RESOLVED.has(pin.state) && deps.sent) {
      const text = el("textarea")
      text.placeholder = "What is still wrong?"
      const reopen = button("Reopen", "reopen", true)
      reopen.disabled = true
      text.addEventListener("input", () => { reopen.disabled = !text.value.trim() })
      reopen.addEventListener("click", () => {
        const comment = text.value.trim()
        if (!comment) return
        reopen.disabled = true
        void act(() => deps.sent!.reopen(pin.id, comment), "Reopened. The pin is back with Legato.")
      })
      panel.append(text)
      row.append(close, reopen)
    } else if (pin.state === "sent" && deps.sent) {
      const withdraw = button("Withdraw", "withdraw")
      withdraw.addEventListener("click", () => { void act(() => deps.sent!.withdraw(pin.id), "Withdrawn.") })
      row.append(withdraw, close)
    } else {
      row.append(close)
    }
    panel.append(row)
    layer.append(panel)
    popover = panel
    const rect = anchor.getBoundingClientRect()
    panel.style.left = `${Math.max(8, Math.min(rect.left + 28, win.innerWidth - 288))}px`
    panel.style.top = `${Math.max(8, Math.min(rect.top, win.innerHeight - 220))}px`
  }

  const act = async (work: () => Promise<void>, done: string): Promise<void> => {
    try {
      await work()
      closePopover()
      say(done, "success")
    } catch (error) {
      say(error instanceof Error ? error.message : String(error), "error")
    }
  }

  const sentRow = (pin: SentPin): HTMLElement => {
    const row = el("div", "sent")
    row.dataset.id = pin.id
    const head = el("div", "head")
    const chip = el("span", "chip", STATE_LABELS[pin.state])
    chip.dataset.state = pin.state
    head.append(chip, el("span", "page mono", pathOf(pin.url)))
    if (RESOLVED.has(pin.state) && deps.sent) {
      const reopen = button("Reopen…", "reopen")
      reopen.addEventListener("click", () => openSentPopover(pin, row))
      head.append(reopen)
    } else if (pin.state === "sent" && deps.sent) {
      const withdraw = button("Withdraw", "withdraw")
      withdraw.addEventListener("click", () => { void act(() => deps.sent!.withdraw(pin.id), "Withdrawn.") })
      head.append(withdraw)
    }
    row.append(head, el("div", "comment", pin.comment))
    if (pin.note) row.append(el("div", "note", pin.note))
    return row
  }

  const renderSentMarkers = (): void => {
    const here = pathOf(win.location.href)
    for (const pin of visibleSent()) {
      if (pathOf(pin.url) !== here) continue
      const target: PinTarget = { selectors: pin.selectors ?? (pin.selector ? [pin.selector] : []), text: pin.text ?? "", box: pin.box }
      const marker = makeMarker(`sent:${pin.id}`, STATE_GLYPHS[pin.state], deps.locate(target), pin.box, target)
      marker.button.dataset.state = pin.state
      marker.button.setAttribute("aria-label", `${STATE_LABELS[pin.state]}: ${pin.comment}`)
      marker.button.title = pin.note ? `${pin.comment}\n— ${pin.note}` : pin.comment
      marker.button.addEventListener("click", () => openSentPopover(pin, marker.button))
    }
  }

  // A comment typed in the panel and not saved yet (its textarea still has
  // focus) is saved first.
  const flushEdits = async (): Promise<void> => {
    for (const row of list.querySelectorAll<HTMLElement>(".draft")) {
      const draft = drafts.find((candidate) => candidate.id === row.dataset.id)
      const comment = row.querySelector("textarea")?.value.trim()
      if (draft && comment && comment !== draft.pin.comment) track(deps.store.update(draft.id, comment))
    }
    await Promise.allSettled([...edits])
    drafts = await deps.store.list()
  }

  const send = async (): Promise<void> => {
    if (!deps.sent || sending || drafts.length === 0) return
    sending = true
    try {
      await flushEdits()
    } catch (error) {
      sending = false
      say(error instanceof Error ? error.message : String(error), "error")
      return
    }
    const batch = drafts.slice(0, MAX_PINS_PER_BATCH)
    if (batch.length === 0) {
      sending = false
      await refresh()
      return
    }
    say("Sending…")
    try {
      const pins = await Promise.all(batch.map(async (draft): Promise<ScopePin> => {
        const screenshot = draft.hasScreenshot ? await deps.store.screenshot(draft.id) : undefined
        return screenshot ? { ...draft.pin, screenshot } : { ...draft.pin }
      }))
      const result = await deps.sent.send(pins)
      await deps.store.removeMany(batch.map((draft) => draft.id))
      const left = drafts.length - batch.length
      const lines = [batch.length === 1 ? "Sent 1 pin to Legato." : `Sent ${batch.length} pins to Legato.`]
      if (!result.subscribed) lines.push(`They wait in Legato's pool until an agent subscribes to ${win.location.origin}.`)
      else if (result.delivered) lines.push("The agent subscribed to this site has them.")
      else lines.push("An agent is subscribed to this site; it gets them next.")
      if (left > 0) lines.push(left === 1 ? "1 draft left: send again." : `${left} drafts left: send again.`)
      sending = false
      say(lines.join("\n"), "success")
    } catch (error) {
      sending = false
      say(error instanceof Error ? error.message : String(error), "error")
    }
    await refresh()
  }
  sendButton.addEventListener("click", () => { void send() })
  clearResolved.addEventListener("click", () => {
    const ids = visibleSent().filter((pin) => RESOLVED.has(pin.state)).map((pin) => pin.id)
    for (const id of ids) cleared.add(id)
    void deps.sent?.cleared.add(ids)
    renderMarkers()
    renderTray()
  })

  const refresh = async (): Promise<void> => {
    drafts = await deps.store.list()
    if (deps.sent) cleared = new Set(await deps.sent.cleared.list())
    if (destroyed) return
    renderMarkers()
    renderTray()
  }

  const setPinMode = (on?: boolean): boolean => {
    pinning = on ?? !pinning
    if (!pinning) outline.style.display = "none"
    renderTray()
    return pinning
  }

  pinToggle.addEventListener("click", () => setPinMode())
  collapse.addEventListener("click", () => {
    const collapsed = tray.dataset.collapsed !== "true"
    tray.dataset.collapsed = String(collapsed)
    collapse.textContent = collapsed ? "▴" : "▾"
    collapse.setAttribute("aria-label", collapsed ? "Expand" : "Collapse")
  })
  // The overlay's own presses stay its own: a page's modal would hear them
  // as a press outside it.
  for (const type of ["pointerdown", "mousedown", "touchstart", "focusin", "keydown"]) {
    layer.addEventListener(type, (event) => event.stopPropagation())
  }

  doc.documentElement.append(host)
  renderTray()

  return {
    host,
    root,
    setPinMode,
    pinMode: () => pinning,
    refresh,
    relocate: () => {
      for (const marker of markerList) {
        if (marker.target && !marker.anchor?.isConnected) marker.anchor = deps.locate(marker.target)
      }
      reposition()
    },
    setSent: (pins) => {
      sentPins = pins
      void (async () => {
        if (deps.sent) cleared = new Set(await deps.sent.cleared.list())
        if (destroyed) return
        renderMarkers()
        renderTray()
      })()
    },
    setSentError: (message) => say(message, "error", true),
    destroy: () => {
      destroyed = true
      for (const type of BLOCKED) win.removeEventListener(type, block, { capture: true })
      win.removeEventListener("pointermove", hover, { capture: true })
      win.removeEventListener("keydown", keydown, { capture: true })
      win.removeEventListener("scroll", schedule, { capture: true })
      win.removeEventListener("resize", schedule)
      host.remove()
    },
  }
}
