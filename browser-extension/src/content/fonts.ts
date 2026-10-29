// The web app sets its text in Geist. The widgets carry their own copy of it
// (public/fonts) and add it to the page under names of their own, so they look
// like Legato on any site and never change the site's own type. Where a page
// refuses the fonts, the widgets fall back to the system's.
export const LEGATO_SANS = '"Legato Geist"'
export const LEGATO_MONO = '"Legato Geist Mono"'

const faces: Array<[family: string, weight: string, file: string]> = [
  ["Legato Geist", "400", "geist-sans-latin-400-normal.woff2"],
  ["Legato Geist", "500", "geist-sans-latin-500-normal.woff2"],
  ["Legato Geist", "600", "geist-sans-latin-600-normal.woff2"],
  ["Legato Geist Mono", "400", "geist-mono-latin-400-normal.woff2"],
  ["Legato Geist Mono", "500", "geist-mono-latin-500-normal.woff2"],
]

const loaded = new WeakSet<object>()

export function loadLegatoFonts(doc: Document): void {
  const fonts = (doc as Document & { fonts?: { add: (face: FontFace) => void } }).fonts
  const getURL = typeof chrome === "undefined" ? undefined : chrome.runtime?.getURL
  if (!fonts || !getURL || typeof FontFace === "undefined" || loaded.has(fonts)) return
  loaded.add(fonts)
  for (const [family, weight, file] of faces) {
    const face = new FontFace(family, `url("${getURL(`fonts/${file}`)}")`, { weight, style: "normal", display: "swap" })
    void face.load().then((ready) => fonts.add(ready)).catch(() => undefined)
  }
}
