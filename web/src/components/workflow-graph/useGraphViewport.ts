import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, type RefObject } from "react"
import type { ReactFlowInstance, useReactFlow } from "@xyflow/react"
import { centerOn as centerView, focusZoom, inView, noInsets, openingView, type Insets, type Rect } from "../../lib/graphViewport"
import type { FlowNode } from "./graphLayout"
import { rectOf, topRects } from "./graphView"

type Flow = ReturnType<typeof useReactFlow>
type Canvas = { insets: Insets; focusId?: string | null } | undefined
type Size = { width: number; height: number }
type RectOf = (id: string) => Rect | null

// useCentreOn moves the view to a node each time the page asks (n
// changes): at once, not animated, so there is no motion to sit through,
// and React Flow 13's animated move never settled under the tests' jsdom.
function useCentreOn(flow: Flow, centerOn: { id: string; n: number } | null | undefined, rect: RectOf, canvas: boolean, frameSize: () => Size, insets: () => Insets) {
  useEffect(() => {
    if (!centerOn) return
    const at = rect(centerOn.id)
    if (!at) return
    if (canvas) { void flow.setViewport(centerView(at, frameSize(), insets(), Math.max(flow.getZoom(), focusZoom))); return }
    void flow.setCenter(at.x + at.width / 2, at.y + at.height / 2, { zoom: Math.max(flow.getZoom(), 0.9) })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [centerOn?.n])
}

// useKeepInView: on a canvas, a node selected where a panel covers it (the
// side panel opening over it) is brought into the free area, at the same
// zoom.
function useKeepInView(flow: Flow, selected: string | null, rect: RectOf, canvas: boolean, frameSize: () => Size, insets: Insets) {
  useEffect(() => {
    if (!canvas || !selected) return
    const at = rect(selected)
    const size = frameSize()
    if (!at || size.width === 0) return
    const view = flow.getViewport()
    if (!inView(at, view, size, insets)) void flow.setViewport(centerView(at, size, insets, view.zoom))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, insets.left, insets.right])
}

// useStartView fits the graph when it fits at a readable size, else shows
// it from where the run is (a canvas's focusId) or from its start (the
// triggers) at that size, for the operator to pan. Top to bottom (a phone)
// the width is what must fit, so it may shrink further; a long chain still
// scrolls down.
function useStartView(tops: Rect[], direction: "LR" | "TB", focusId: string | null | undefined, rect: RectOf, frameSize: () => Size, insets: () => Insets) {
  const floor = direction === "TB" ? 0.6 : 0.75
  return useCallback((view: Pick<ReactFlowInstance, "setViewport">) => {
    if (tops.length === 0) return
    const focus = focusId ? rect(focusId) : null
    void view.setViewport(openingView(tops, frameSize(), { insets: insets(), floor, focus }))
    // frameSize reads the frame's ref, which is current whenever this runs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tops, floor, rect, focusId])
}

type Options = {
  flow: Flow
  placed: Omit<FlowNode, "ariaLabel">[]
  frame: RefObject<HTMLDivElement | null>
  canvas: Canvas
  centerOn?: { id: string; n: number } | null
  selected: string | null
  direction: "LR" | "TB"
}

/** useGraphViewport is where the view of the graph goes: where it opens
 * (startView), a node centred each time the page asks, and on a canvas the
 * node chosen kept out from under the page's panels. rect is a node's
 * place, frameSize the graph's, tops the places of the nodes in no group. */
export function useGraphViewport({ flow, placed, frame, canvas, centerOn, selected, direction }: Options) {
  const insets = canvas?.insets ?? noInsets
  const insetsRef = useRef(insets)
  // The latest insets, for the effects and the opening view to read.
  useLayoutEffect(() => { insetsRef.current = insets })
  const rect = useCallback((id: string): Rect | null => rectOf(placed, id), [placed])
  const frameSize = () => ({ width: frame.current?.clientWidth ?? 0, height: frame.current?.clientHeight ?? 0 })
  const tops = useMemo(() => topRects(placed), [placed])
  const currentInsets = () => insetsRef.current
  useCentreOn(flow, centerOn, rect, Boolean(canvas), frameSize, currentInsets)
  useKeepInView(flow, selected, rect, Boolean(canvas), frameSize, insets)
  const startView = useStartView(tops, direction, canvas?.focusId, rect, frameSize, currentInsets)
  return { insets, rect, frameSize, tops, startView }
}
