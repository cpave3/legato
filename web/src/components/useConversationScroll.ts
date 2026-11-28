import { useCallback, useEffect, useRef, useState } from "react"

// How near its end the conversation counts as there: near enough to follow
// new content, and near enough not to offer the way back down.
const FOLLOW_PX = 60
const AT_BOTTOM_PX = 40

function distanceFromBottom(el: HTMLElement): number {
  return el.scrollHeight - el.scrollTop - el.clientHeight
}

// Keeps a viewport that is near its end at the end as content grows.
export function followIfNearBottom(el: HTMLElement | null): void {
  if (el && distanceFromBottom(el) < FOLLOW_PX) el.scrollTop = el.scrollHeight
}

// Where the reader was before a refresh replaced the conversation.
export interface ScrollAnchor {
  atBottom: boolean
  top: number
}

export function scrollAnchorOf(el: HTMLElement | null): ScrollAnchor | null {
  return el ? { atBottom: distanceFromBottom(el) < AT_BOTTOM_PX, top: el.scrollTop } : null
}

function scrollTo(el: HTMLElement | null, top: number): void {
  if (el) el.scrollTop = top
}

// useConversationScroll keeps the conversation at its latest message unless
// the reader scrolled up, and says when they have.
export function useConversationScroll(loading: boolean, items: unknown) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [isScrolledUp, setIsScrolledUp] = useState(false)
  const initialScrollDone = useRef(false)

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      setIsScrolledUp(false)
    }
  }, [])

  const scrollToBottomAfterRender = useCallback(() => {
    requestAnimationFrame(scrollToBottom)
  }, [scrollToBottom])

  // Scroll to bottom once the feed has content. The initial load always
  // scrolls to the latest message regardless of isScrolledUp, because the
  // browser fires a spurious onScroll at scrollTop=0 when content first
  // overflows. After that, scroll to bottom only when the user is near the
  // bottom (within a threshold), respecting deliberate scroll-up.
  useEffect(() => {
    if (loading || !scrollRef.current) return
    if (initialScrollDone.current) {
      followIfNearBottom(scrollRef.current)
      return
    }
    scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    setIsScrolledUp(false)
    initialScrollDone.current = true
  }, [loading, items])

  const onScroll = useCallback((e: React.UIEvent<HTMLDivElement>) => {
    setIsScrolledUp(!(distanceFromBottom(e.currentTarget) < AT_BOTTOM_PX))
  }, [])

  // After a refresh: back at the end when asked or when the reader was
  // there, else where they were.
  const restoreAfterSync = useCallback((anchor: ScrollAnchor | null, toEnd: boolean) => {
    if (toEnd || anchor?.atBottom) {
      setIsScrolledUp(false)
      return
    }
    if (anchor) requestAnimationFrame(() => scrollTo(scrollRef.current, anchor.top))
  }, [])

  // Follows content that just arrived, once it is drawn.
  const followLatest = useCallback(() => {
    requestAnimationFrame(() => followIfNearBottom(scrollRef.current))
  }, [])

  // A new agent opens at its latest message again.
  const restartInitialScroll = useCallback(() => {
    initialScrollDone.current = false
  }, [])

  return { scrollRef, isScrolledUp, scrollToBottom, scrollToBottomAfterRender, onScroll, restoreAfterSync, followLatest, restartInitialScroll }
}
