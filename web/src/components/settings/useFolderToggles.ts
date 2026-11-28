import { useCallback, useEffect, useState } from "react"

// How often the list is read again, so a folder added, changed or removed
// on disk shows without reloading the page.
const refreshEvery = 10_000

export interface FolderList<T> {
  dir: string
  items: T[]
}

const message = (reason: unknown) => (reason instanceof Error ? reason.message : String(reason))

// useFolderToggles keeps a list of things Legato finds in a folder on disk
// (skills, managed hooks), read again every 10 s and on window focus, and
// turns one on or off. A refused change is shown and the list read again.
export function useFolderToggles<T extends { name: string }>(
  read: () => Promise<FolderList<T>>,
  save: (name: string, enabled: boolean) => Promise<T>,
) {
  const [list, setList] = useState<FolderList<T> | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setList(await read())
    } catch (reason) {
      setError(message(reason))
    }
  }, [read])

  useEffect(() => {
    void load()
    const timer = setInterval(() => { void load() }, refreshEvery)
    const onFocus = () => { void load() }
    window.addEventListener("focus", onFocus)
    return () => {
      clearInterval(timer)
      window.removeEventListener("focus", onFocus)
    }
  }, [load])

  const toggle = async (name: string, enabled: boolean) => {
    setBusy(name)
    setError(null)
    try {
      const saved = await save(name, enabled)
      setList((current) => current && { ...current, items: current.items.map((item) => (item.name === name ? saved : item)) })
    } catch (reason) {
      setError(message(reason))
      await load()
    } finally {
      setBusy(null)
    }
  }

  return { list, busy, error, toggle }
}
