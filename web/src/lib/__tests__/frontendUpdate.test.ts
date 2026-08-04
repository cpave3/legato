import { describe, expect, it, vi } from 'vitest'
import { coordinateFrontendUpdate } from '../frontendUpdate'

function dependencies(serverBuildID: string) {
  const values = new Map<string, string>()
  return {
    fetchBuildID: vi.fn(async () => serverBuildID),
    storage: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key),
    },
    updateServiceWorker: vi.fn(async () => {}),
    reload: vi.fn(),
    onUpdateRequired: vi.fn(),
  }
}

describe('coordinateFrontendUpdate', () => {
  it('does nothing when the server and browser bundle identities match', async () => {
    const deps = dependencies('same-build')
    await coordinateFrontendUpdate('same-build', deps)
    expect(deps.updateServiceWorker).not.toHaveBeenCalled()
    expect(deps.reload).not.toHaveBeenCalled()
  })

  it('updates the service worker and reloads once for a changed build', async () => {
    const deps = dependencies('new-build')
    await coordinateFrontendUpdate('old-build', deps)
    expect(deps.updateServiceWorker).toHaveBeenCalledOnce()
    expect(deps.reload).toHaveBeenCalledOnce()

    await coordinateFrontendUpdate('old-build', deps)
    expect(deps.reload).toHaveBeenCalledOnce()
    expect(deps.onUpdateRequired).toHaveBeenCalledWith('new-build')
  })

  it('ignores an unavailable frontend identity', async () => {
    const deps = dependencies('frontend-unavailable')
    await coordinateFrontendUpdate('old-build', deps)
    expect(deps.reload).not.toHaveBeenCalled()
    expect(deps.onUpdateRequired).not.toHaveBeenCalled()
  })
})
