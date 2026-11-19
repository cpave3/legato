// A pin's component source: which component drew the pinned element, and
// the line of the source file that made it. It is found in two halves.
//
// In the page (the MAIN world, which the page controls), a framework
// adapter reads what the framework keeps on the element: the component's
// name, its owners, and raw frames (a script URL and a position). In the
// service worker, a source resolver turns a frame into a project file and a
// line, fetching the module and its source map from the page's origin. The
// service worker checks what the page returned before it uses any of it.
//
// React is the one adapter and Vite the one resolver; another framework
// (Svelte, Vue) is another adapter in main-world.ts, and another dev server
// (Next) another resolver in lookup.ts.

// The attribute the content script puts on the pinned element for the
// MAIN-world reader to find, and takes off again.
export const PROBE_ATTRIBUTE = "data-legato-scope-probe"

// A position in a script the page loaded. `original` is true when the
// position is already in the source file (React 18's _debugSource), false
// when it is in the code the browser ran and needs the source map.
export type RawFrame = { url: string; line: number; column: number; original: boolean }

// What an adapter read from the pinned element.
export type ComponentReading = {
  framework: string
  // The component that rendered the element; "Anonymous" when it has no name.
  name?: string
  // The components that rendered that one, nearest first.
  owners: string[]
  // Stack frames, innermost first; the first in the project is the JSX.
  frames: RawFrame[]
}

// Runs in the page: reads one element, or answers null when its framework
// did not render it.
export type FrameworkAdapter = {
  framework: string
  read: (element: Element) => ComponentReading | null
}

// A frame's place in the project.
export type ResolvedSource = { file: string; line?: number; column?: number }

// Fetches a URL of the page's origin as text; throws when it cannot.
export type FetchText = (url: string) => Promise<string>

export type ResolveContext = { origin: string; fetchText: FetchText }

// Runs in the service worker: a frame's file and line, or null when the
// frame is not a project file this resolver knows (a dependency, the dev
// server's own client, another server's URL).
export type SourceResolver = {
  name: string
  resolve: (frame: RawFrame, context: ResolveContext) => Promise<ResolvedSource | null>
}

// Bounds on what the page may return.
export const MAX_FRAMES = 20
export const MAX_OWNERS = 6
export const MAX_NAME_LENGTH = 120
export const MAX_URL_LENGTH = 2048
