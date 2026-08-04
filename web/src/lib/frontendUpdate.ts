const attemptedBuildKey = 'legato:frontend-update-attempt'

export function clearFrontendUpdateAttempt(storage: StorageLike): void {
  storage.removeItem(attemptedBuildKey)
}

interface StorageLike {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

export interface FrontendUpdateDependencies {
  fetchBuildID: () => Promise<string | null>
  storage: StorageLike
  updateServiceWorker: () => Promise<void>
  reload: () => void
  onUpdateRequired: (serverBuildID: string) => void
}

export async function coordinateFrontendUpdate(
  browserBuildID: string,
  dependencies: FrontendUpdateDependencies,
): Promise<void> {
  let serverBuildID: string | null
  try {
    serverBuildID = await dependencies.fetchBuildID()
  } catch {
    return
  }
  if (!serverBuildID || serverBuildID === 'frontend-unavailable' || serverBuildID === browserBuildID) {
    if (serverBuildID === browserBuildID) dependencies.storage.removeItem(attemptedBuildKey)
    return
  }
  if (dependencies.storage.getItem(attemptedBuildKey) === serverBuildID) {
    dependencies.onUpdateRequired(serverBuildID)
    return
  }
  dependencies.storage.setItem(attemptedBuildKey, serverBuildID)
  await dependencies.updateServiceWorker()
  dependencies.reload()
}

export async function updateRegisteredServiceWorkers(): Promise<void> {
  if (!('serviceWorker' in navigator)) return
  const registrations = await navigator.serviceWorker.getRegistrations()
  await Promise.all(registrations.map((registration) => registration.update()))
  const installing = registrations.map((registration) => registration.installing ?? registration.waiting).find(Boolean)
  if (!installing) return
  await new Promise<void>((resolve) => {
    const timeout = window.setTimeout(resolve, 3000)
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      window.clearTimeout(timeout)
      resolve()
    }, { once: true })
  })
}
