import { useCallback, useMemo, useRef, useState } from "react"
import { useElementWidth } from "../hooks/useElementWidth"
import { subagentTree, type SubagentNode } from "../lib/agentGlance"
import type { BackgroundJob } from "../lib/agentWire"
import { fromStored, mergeShells, type NativeBackgroundShell, type StoredBackgroundShell } from "../lib/backgroundShells"
import type { NativeConversationItem } from "../lib/nativeAgentSync"
import type { DeckTile } from "./tiles/TileDeck"
import { shellTiles } from "./tiles/shellTiles"
import { saveSubagentView, savedSubagentView, subagentTiles, type SubagentView } from "./tiles/subagentTiles"

// The least width at which subagent tiles sit beside the conversation; below
// it they are a strip above it.
const tilesBesideWidth = 1000

// What the conversation feed needs to draw the tiles of subagents and
// shells beside their calls.
export interface FeedTiles {
  show: boolean
  all: DeckTile[]
  shells: NativeBackgroundShell[]
  nodes: SubagentNode[]
  highlighted: string | null
  onHighlight: (id: string | null) => void
  onFocus: (id: string | null) => void
}

// useSubagentTiles is the deck of a native agent's subagents and background
// shells: whether it is shown (a choice kept in the browser), beside the
// conversation or above it by the panel's width, and the tile in focus.
export function useSubagentTiles(items: NativeConversationItem[], nativeShells: NativeBackgroundShell[], storedShells: StoredBackgroundShell[] | undefined, backgroundJobs: BackgroundJob[] | undefined) {
  const shells = useMemo(() => mergeShells(nativeShells, fromStored(storedShells)), [nativeShells, storedShells])
  const tiles = useMemo(() => [...subagentTiles(items, backgroundJobs), ...shellTiles(shells)], [items, shells, backgroundJobs])
  const nodes = useMemo(() => subagentTree(backgroundJobs), [backgroundJobs])
  const [view, setView] = useState<SubagentView>(savedSubagentView)
  const [focused, setFocused] = useState<string | null>(null)
  const [highlighted, setHighlighted] = useState<string | null>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const panelWidth = useElementWidth(panelRef)
  const show = view === "tiles" && tiles.length > 0

  const chooseView = useCallback((next: SubagentView) => {
    setView(next)
    saveSubagentView(next)
    setFocused(null)
  }, [])

  const feed: FeedTiles = { show, all: tiles, shells, nodes, highlighted, onHighlight: setHighlighted, onFocus: setFocused }
  return {
    panelRef, tiles, feed, view, chooseView, focused, setFocused, highlighted, setHighlighted, show,
    deckTitle: shells.length > 0 ? "Sub-agents and shells" : "Sub-agents",
    beside: panelWidth >= tilesBesideWidth,
  }
}
