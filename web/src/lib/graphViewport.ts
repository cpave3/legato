// Where a graph's view goes: fitted, centred on a node, or how it opens,
// all within the free area of its frame, the part the panels over it
// leave uncovered (insets). Pure, so the graph component only applies it.

export type Rect = { x: number; y: number; width: number; height: number }
export type Insets = { top: number; right: number; bottom: number; left: number }
export type Viewport = { x: number; y: number; zoom: number }
export type Frame = { width: number; height: number }

export const noInsets: Insets = { top: 0, right: 0, bottom: 0, left: 0 }

// The space kept between the free area's edge and what is fitted in it.
const pad = 24
// The zoom a run opens at on the step it is at.
export const focusZoom = 0.8
export const minZoom = 0.2

function area(frame: Frame, insets: Insets) {
  return {
    left: insets.left + pad,
    top: insets.top + pad,
    width: Math.max(1, frame.width - insets.left - insets.right - 2 * pad),
    height: Math.max(1, frame.height - insets.top - insets.bottom - 2 * pad),
  }
}

function bounds(rects: Rect[]): Rect {
  const minX = Math.min(...rects.map(rect => rect.x)), maxX = Math.max(...rects.map(rect => rect.x + rect.width))
  const minY = Math.min(...rects.map(rect => rect.y)), maxY = Math.max(...rects.map(rect => rect.y + rect.height))
  return { x: minX, y: minY, width: maxX - minX, height: maxY - minY }
}

// fitView shows every rect, centred in the free area, at full size at most.
export function fitView(rects: Rect[], frame: Frame, insets: Insets): Viewport {
  const all = bounds(rects), free = area(frame, insets)
  const zoom = Math.max(minZoom, Math.min(1, free.width / all.width, free.height / all.height))
  return { x: free.left + (free.width - all.width * zoom) / 2 - all.x * zoom, y: free.top + (free.height - all.height * zoom) / 2 - all.y * zoom, zoom }
}

// centerOn puts a rect's centre at the free area's centre.
export function centerOn(rect: Rect, frame: Frame, insets: Insets, zoom: number): Viewport {
  const free = area(frame, insets)
  return { x: free.left + free.width / 2 - (rect.x + rect.width / 2) * zoom, y: free.top + free.height / 2 - (rect.y + rect.height / 2) * zoom, zoom }
}

// openingView is how a graph opens: fitted when that keeps it readable (at
// floor or more), else on focus (the step a run is at) at focusZoom, else
// at its start at floor, for the operator to pan.
export function openingView(rects: Rect[], frame: Frame, options: { insets?: Insets; floor: number; focus?: Rect | null }): Viewport {
  const insets = options.insets ?? noInsets
  const fit = fitView(rects, frame, insets)
  if (fit.zoom >= options.floor) return fit
  if (options.focus) return centerOn(options.focus, frame, insets, focusZoom)
  const all = bounds(rects), free = area(frame, insets), zoom = options.floor
  const y = all.height * zoom <= free.height ? free.top + (free.height - all.height * zoom) / 2 - all.y * zoom : free.top - all.y * zoom
  return { x: free.left - all.x * zoom, y, zoom }
}

// inView is whether a rect is wholly inside the free area as the view
// draws it.
export function inView(rect: Rect, view: Viewport, frame: Frame, insets: Insets): boolean {
  const left = rect.x * view.zoom + view.x, top = rect.y * view.zoom + view.y
  return left >= insets.left && top >= insets.top && left + rect.width * view.zoom <= frame.width - insets.right && top + rect.height * view.zoom <= frame.height - insets.bottom
}
