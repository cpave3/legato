// The classes of an approval card's choices.

// A choice on a phone is a full-width button a thumb hits without aiming;
// from the md breakpoint it is the compact button of a desktop tray.
export const choiceSize = "flex min-h-12 w-full items-center justify-center rounded-xl px-4 text-base md:min-h-0 md:w-auto md:rounded md:px-2.5 md:py-1 md:text-xs"
export const primaryChoice = "bg-lw-accent font-semibold text-[var(--theme-canvas,#fff)] hover:brightness-110 disabled:opacity-50"
const secondaryChoice = "border border-lw-line-strong font-medium text-lw-text hover:bg-lw-selected disabled:opacity-50"

// The first option is the one a thumb is meant for.
export function choiceTone(index: number): string {
  return index === 0 ? primaryChoice : secondaryChoice
}
