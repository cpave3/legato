import { hostPermissionFor } from "./config"

export async function hasOriginPermission(baseUrl: string): Promise<boolean> {
  return chrome.permissions.contains({ origins: [hostPermissionFor(baseUrl)] })
}

export async function requestOriginPermission(baseUrl: string): Promise<boolean> {
  return chrome.permissions.request({ origins: [hostPermissionFor(baseUrl)] })
}

export async function replaceOriginPermission(previousBaseUrl: string | undefined, nextBaseUrl: string): Promise<boolean> {
  const next = hostPermissionFor(nextBaseUrl)
  if (!(await chrome.permissions.request({ origins: [next] }))) return false
  if (previousBaseUrl) {
    const previous = hostPermissionFor(previousBaseUrl)
    if (previous !== next) await chrome.permissions.remove({ origins: [previous] })
  }
  return true
}
