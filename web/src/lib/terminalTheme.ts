import type { ITheme } from "@xterm/xterm"

// The colours of agents' terminals, chosen in Settings and kept in this
// browser. The frame around a terminal stays Legato's whatever the theme.

export type TerminalThemeId = "legato" | "night" | "solarized" | "classic"

export interface TerminalThemeOption {
  id: TerminalThemeId
  name: string
  theme: ITheme
}

export const TERMINAL_THEME_KEY = "legato:terminal-theme"

// Legato's own: its state colours (running, needs you, done, error) as the
// ANSI ones, its accent for the cursor and selection, on a ground a little
// darker and cooler than the page's.
const legatoAnsi = {
  black: "#1d1d24",
  red: "#f0646b",
  green: "#3ecf8e",
  yellow: "#f0b44c",
  blue: "#6aa5ff",
  magenta: "#b49cff",
  cyan: "#4fd1c5",
  white: "#c9c8d2",
  brightBlack: "#5d5c68",
  brightRed: "#ff8a90",
  brightGreen: "#6be3ab",
  brightYellow: "#ffcb73",
  brightBlue: "#93bdff",
  brightMagenta: "#cdbcff",
  brightCyan: "#7fe3da",
  brightWhite: "#f4f3f8",
}

export const terminalThemes: TerminalThemeOption[] = [
  {
    id: "legato",
    name: "Legato",
    theme: { background: "#0b0b10", foreground: "#e3e2ea", cursor: "#a99cff", cursorAccent: "#0b0b10", selectionBackground: "#2e2a55", ...legatoAnsi },
  },
  {
    id: "night",
    name: "Night",
    theme: { background: "#000000", foreground: "#e3e2ea", cursor: "#a99cff", cursorAccent: "#000000", selectionBackground: "#2e2a55", ...legatoAnsi },
  },
  {
    id: "solarized",
    name: "Solarized Dark",
    theme: {
      background: "#002b36", foreground: "#93a1a1", cursor: "#93a1a1", cursorAccent: "#002b36", selectionBackground: "#073642",
      black: "#073642", red: "#dc322f", green: "#859900", yellow: "#b58900", blue: "#268bd2", magenta: "#d33682", cyan: "#2aa198", white: "#eee8d5",
      brightBlack: "#586e75", brightRed: "#cb4b16", brightGreen: "#859900", brightYellow: "#b58900", brightBlue: "#839496", brightMagenta: "#6c71c4", brightCyan: "#93a1a1", brightWhite: "#fdf6e3",
    },
  },
  {
    id: "classic",
    name: "Classic",
    theme: {
      background: "#000000", foreground: "#ffffff", cursor: "#ffffff", cursorAccent: "#000000", selectionBackground: "#4d4d4d",
      black: "#2e3436", red: "#cc0000", green: "#4e9a06", yellow: "#c4a000", blue: "#3465a4", magenta: "#75507b", cyan: "#06989a", white: "#d3d7cf",
      brightBlack: "#555753", brightRed: "#ef2929", brightGreen: "#8ae234", brightYellow: "#fce94f", brightBlue: "#729fcf", brightMagenta: "#ad7fa8", brightCyan: "#34e2e2", brightWhite: "#eeeeec",
    },
  },
]

// The theme this browser chose; Legato's when none is saved, the saved one
// is no longer offered, or storage cannot be read.
export function terminalTheme(): TerminalThemeOption {
  let id: string | null = null
  try {
    id = localStorage.getItem(TERMINAL_THEME_KEY)
  } catch {
    // A private window, or storage turned off.
  }
  return terminalThemes.find((option) => option.id === id) ?? terminalThemes[0]
}

export function setTerminalTheme(id: TerminalThemeId): void {
  try {
    localStorage.setItem(TERMINAL_THEME_KEY, id)
  } catch {
    // Not kept: the terminal stays as it was.
  }
}
