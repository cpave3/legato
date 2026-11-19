// bippy's fibre helpers (https://github.com/aidenybai/bippy, MIT, Aiden Bai).
//
// bippy's main entry installs the React DevTools hook as it loads, and
// imports React; Scope's page script must do neither. So the helpers come
// from bippy's core chunk, which does neither, under the minified names that
// bippy 0.7.3's dist/index.js maps to these (the version is pinned in
// package.json; main-world.test.ts fails if an upgrade moves them).
import { a, g, h, i, l, m } from "bippy/dist/core"

export type Fiber = {
  tag: number
  type: unknown
  return: Fiber | null
  _debugOwner?: unknown
  _debugStack?: unknown
  _debugSource?: unknown
}

// The fibre React keeps on a DOM node (its __reactFiber$… key), or null.
export const getFiberFromHostInstance = a as (node: unknown) => Fiber | null
// The current fibre of a fibre's pair. A DOM node keeps the fibre it was
// made with, which after an update may be the older of the two.
export const getLatestFiber = l as (fiber: Fiber) => Fiber
// A component type's name: displayName, the function's name, or what memo
// and forwardRef wrap; null when it has none.
export const getDisplayName = i
// A function, class, forwardRef or memo component's fibre.
export const isCompositeFiber = m as (fiber: Fiber) => boolean
// A DOM element's or text's fibre.
export const isHostFiber = g as (fiber: Fiber) => boolean
export const isFiber = h as (value: unknown) => value is Fiber
