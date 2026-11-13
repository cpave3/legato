import { getTextWidth } from "@evenrealities/pretext"

// One spacing scale for every page on the glasses, so pages line up with
// each other: an 8 px margin round the canvas, 4 px between boxes, 4 px of
// padding inside a box, and a 1 px border on the boxes that frame something.
export const CANVAS_WIDTH = 576
export const CANVAS_HEIGHT = 288
export const MARGIN = 8
export const GAP = 4
export const PADDING = 4
export const BORDER = 1

// A line of text is 27 px (pretext's line height). A box must hold its line
// inside its padding and border, or the firmware gives it a scrollbar.
export const LINE_HEIGHT = 27
export const boxHeight = (padding: number, border = 0) => LINE_HEIGHT + 2 * (padding + border) + 1

// The header row: one line in a bordered box.
export const HEADER_Y = 4
export const HEADER_HEIGHT = boxHeight(PADDING, BORDER)
// The frame under the header that holds a page's list or text.
export const FRAME_X = MARGIN
export const FRAME_Y = HEADER_Y + HEADER_HEIGHT + GAP
export const FRAME_WIDTH = CANVAS_WIDTH - 2 * MARGIN
// The line at the foot of the agent pages that says what the touchpad does:
// centred, so it needs no padding.
export const HINT_HEIGHT = boxHeight(0)
export const HINT_Y = CANVAS_HEIGHT - HINT_HEIGHT
// A frame's height with the hint line under it, and without.
export const FRAME_HEIGHT_WITH_HINT = HINT_Y - GAP - FRAME_Y
export const FRAME_HEIGHT = CANVAS_HEIGHT - MARGIN - FRAME_Y

// What text may take inside a box: its width less padding and border on
// both sides. A list item also leaves room for the firmware's highlight of
// the selected item.
export const inner = (width: number, border = 0) => width - 2 * (PADDING + border)
const HIGHLIGHT = 24
export const ITEM_TEXT_WIDTH = inner(FRAME_WIDTH, BORDER) - HIGHLIGHT

// The state badge at the top left of an agent's pages: wide enough for the
// longest state, so the title beside it does not move as the state changes.
export const BADGE_WIDTH = Math.ceil(getTextWidth("▷ background") + 2 * (PADDING + BORDER) + 8)
export const TITLE_X = MARGIN + BADGE_WIDTH + GAP * 2
export const TITLE_WIDTH = CANVAS_WIDTH - MARGIN - TITLE_X

// Text centred in a box of `width` px of text, by leading spaces: the
// firmware aligns text only to the left.
export function center(text: string, width: number): string {
  const free = width - getTextWidth(text)
  let spaces = Math.max(0, Math.round(free / 2 / getTextWidth(" ")))
  while (spaces > 0 && getTextWidth(`${" ".repeat(spaces)}${text}`) > width) spaces--
  return `${" ".repeat(spaces)}${text}`
}
