// bippy 0.7.3's core chunk, which has no types of its own. Its exports are
// minified; bippy.ts names the ones Scope uses.
declare module "bippy/dist/core" {
  export const a: (node: unknown) => unknown
  export const i: (type: unknown) => string | null
  export const m: (fiber: unknown) => boolean
  export const g: (fiber: unknown) => boolean
  export const h: (value: unknown) => boolean
}
