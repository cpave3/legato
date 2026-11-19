import type { Box } from "./scope-types"
import { MAX_SCREENSHOT_BYTES } from "./scope-types"

// A pin's screenshot: the service worker captures the visible tab, crops it
// around the pinned element with a margin, outlines the element, scales it
// to at most 1024 pixels wide and encodes it as a JPEG within 500 KB.

export const MAX_SCREENSHOT_WIDTH = 1024
export const SCREENSHOT_MARGIN = 24

type Size = { width: number; height: number }

export type CropPlan = {
  // In the captured picture's pixels.
  source: Box
  // The picture made.
  output: Size
  // The element in the picture made, cut at its edges.
  outline: Box
}

// Where to crop: box is the element in viewport CSS pixels. The ratio
// between picture and viewport is read from the picture, so the device's
// pixel ratio and the page's zoom are both counted. Null when no part of the
// element is visible.
export function planCrop(input: { box: Box; viewport: Size; image: Size; margin?: number; maxWidth?: number }): CropPlan | null {
  const { box, viewport, image } = input
  const margin = input.margin ?? SCREENSHOT_MARGIN
  const maxWidth = input.maxWidth ?? MAX_SCREENSHOT_WIDTH
  if (box.width <= 0 || box.height <= 0 || viewport.width <= 0 || viewport.height <= 0) return null
  const clampX = (value: number) => Math.min(Math.max(value, 0), viewport.width)
  const clampY = (value: number) => Math.min(Math.max(value, 0), viewport.height)
  const left = clampX(box.x - margin)
  const right = clampX(box.x + box.width + margin)
  const top = clampY(box.y - margin)
  const bottom = clampY(box.y + box.height + margin)
  const elementLeft = clampX(box.x)
  const elementRight = clampX(box.x + box.width)
  const elementTop = clampY(box.y)
  const elementBottom = clampY(box.y + box.height)
  if (elementRight <= elementLeft || elementBottom <= elementTop) return null

  const scale = image.width / viewport.width
  const px = (value: number, limit: number) => Math.min(Math.max(Math.round(value * scale), 0), limit)
  const sx = px(left, image.width)
  const sy = px(top, image.height)
  const source = { x: sx, y: sy, width: px(right, image.width) - sx, height: px(bottom, image.height) - sy }
  if (source.width <= 0 || source.height <= 0) return null

  const shrink = Math.min(1, maxWidth / source.width)
  const output = { width: Math.max(1, Math.round(source.width * shrink)), height: Math.max(1, Math.round(source.height * shrink)) }
  const out = (value: number) => Math.round(value * scale * shrink)
  const outline = {
    x: out(elementLeft - left),
    y: out(elementTop - top),
    width: out(elementRight - elementLeft),
    height: out(elementBottom - elementTop),
  }
  return { source, output, outline }
}

const QUALITIES = [0.85, 0.7, 0.55, 0.4]
const SCALES = [1, 0.75, 0.5]

// Encodes at lower quality, then at a smaller size, until the picture fits
// the limit; null when nothing does.
export async function encodeWithinLimit(
  encode: (quality: number, scale: number) => Promise<Blob>,
  limit: number = MAX_SCREENSHOT_BYTES,
): Promise<Blob | null> {
  for (const scale of SCALES) {
    for (const quality of QUALITIES) {
      const blob = await encode(quality, scale)
      if (blob.size <= limit) return blob
    }
  }
  return null
}

export async function blobToDataUrl(blob: Blob): Promise<string> {
  const bytes = new Uint8Array(await blob.arrayBuffer())
  let binary = ""
  for (let index = 0; index < bytes.length; index += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(index, index + 0x8000))
  }
  return `data:${blob.type || "application/octet-stream"};base64,${btoa(binary)}`
}

// The service worker's part: crops the captured tab (a PNG data URL) with
// OffscreenCanvas, outlines the element and encodes the result.
export async function cropScreenshot(capturedDataUrl: string, box: Box, viewport: Size): Promise<string | null> {
  const bitmap = await createImageBitmap(await (await fetch(capturedDataUrl)).blob())
  try {
    const plan = planCrop({ box, viewport, image: { width: bitmap.width, height: bitmap.height } })
    if (!plan) return null
    const blob = await encodeWithinLimit(async (quality, scale) => {
      const width = Math.max(1, Math.round(plan.output.width * scale))
      const height = Math.max(1, Math.round(plan.output.height * scale))
      const canvas = new OffscreenCanvas(width, height)
      const context = canvas.getContext("2d")
      if (!context) throw new Error("No 2D canvas in the service worker")
      context.drawImage(bitmap, plan.source.x, plan.source.y, plan.source.width, plan.source.height, 0, 0, width, height)
      const ratio = width / plan.output.width
      context.strokeStyle = "#6d5ef6"
      context.lineWidth = Math.max(2, Math.round(3 * ratio))
      const inset = context.lineWidth / 2
      context.strokeRect(
        plan.outline.x * ratio - inset, plan.outline.y * ratio - inset,
        plan.outline.width * ratio + context.lineWidth, plan.outline.height * ratio + context.lineWidth,
      )
      return canvas.convertToBlob({ type: "image/jpeg", quality })
    })
    return blob ? blobToDataUrl(blob) : null
  } finally {
    bitmap.close()
  }
}
