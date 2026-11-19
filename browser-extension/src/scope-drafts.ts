import type { ScopeDraft } from "./scope-types"

// The part of chrome.storage.local the drafts use.
export type StorageAreaLike = {
  get: (keys: string | string[]) => Promise<Record<string, unknown>>
  set: (items: Record<string, unknown>) => Promise<void>
  remove: (keys: string | string[]) => Promise<void>
}

export const draftsKey = (origin: string): string => `scopeDrafts:${origin}`
const screenshotKey = (origin: string, id: string): string => `scopeShot:${origin}:${id}`

function isDraft(value: unknown): value is ScopeDraft {
  if (!value || typeof value !== "object") return false
  const candidate = value as Partial<ScopeDraft>
  return typeof candidate.id === "string" && typeof candidate.pin === "object" && candidate.pin !== null
    && typeof candidate.pin.comment === "string" && typeof candidate.pin.page?.url === "string"
}

// An origin's pins not sent yet. They live in chrome.storage.local, so they
// survive reloads and navigation within the site, and every tab of the site
// sees the same ones. Screenshots are kept under keys of their own, so the
// list stays small to read.
export function createDraftStore(area: StorageAreaLike, origin: string) {
  const key = draftsKey(origin)
  const list = async (): Promise<ScopeDraft[]> => {
    const value = (await area.get(key))[key]
    return Array.isArray(value) ? value.filter(isDraft) : []
  }
  const save = (drafts: ScopeDraft[]): Promise<void> => area.set({ [key]: drafts })
  const removeMany = async (ids: string[]): Promise<void> => {
    const drafts = await list()
    await save(drafts.filter((draft) => !ids.includes(draft.id)))
    const shots = drafts.filter((draft) => ids.includes(draft.id) && draft.hasScreenshot).map((draft) => screenshotKey(origin, draft.id))
    if (shots.length) await area.remove(shots)
  }
  return {
    key,
    list,
    add: async (draft: ScopeDraft, screenshot?: string): Promise<void> => {
      if (screenshot) await area.set({ [screenshotKey(origin, draft.id)]: screenshot })
      await save([...(await list()), { ...draft, hasScreenshot: Boolean(screenshot) }])
    },
    update: async (id: string, comment: string): Promise<void> => {
      await save((await list()).map((draft) => draft.id === id ? { ...draft, pin: { ...draft.pin, comment } } : draft))
    },
    remove: (id: string): Promise<void> => removeMany([id]),
    removeMany,
    screenshot: async (id: string): Promise<string | undefined> => {
      const value = (await area.get(screenshotKey(origin, id)))[screenshotKey(origin, id)]
      return typeof value === "string" ? value : undefined
    },
  }
}

export type DraftStore = ReturnType<typeof createDraftStore>

// A storage area in memory, for tests and for pages without chrome.storage.
export function memoryArea(initial: Record<string, unknown> = {}): StorageAreaLike & { data: Record<string, unknown> } {
  const data: Record<string, unknown> = structuredClone(initial)
  return {
    data,
    get: async (keys) => {
      const out: Record<string, unknown> = {}
      for (const k of Array.isArray(keys) ? keys : [keys]) if (k in data) out[k] = structuredClone(data[k])
      return out
    },
    set: async (items) => { for (const [k, v] of Object.entries(items)) data[k] = structuredClone(v) },
    remove: async (keys) => { for (const k of Array.isArray(keys) ? keys : [keys]) delete data[k] },
  }
}
