import type { KeyboardEvent } from "react"

export function themePickerKeys(event: KeyboardEvent<HTMLDivElement>): void {
  if (!["ArrowLeft", "ArrowUp", "ArrowRight", "ArrowDown", "Home", "End"].includes(event.key)) return
  const choices = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('button[role="radio"]:not(:disabled)')]
  const current = choices.indexOf(event.target as HTMLButtonElement)
  if (current < 0) return
  event.preventDefault()
  const index = event.key === "Home" ? 0 : event.key === "End" ? choices.length - 1
    : (current + (event.key === "ArrowLeft" || event.key === "ArrowUp" ? -1 : 1) + choices.length) % choices.length
  choices[index].focus()
  choices[index].click()
}
