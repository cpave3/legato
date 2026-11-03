import { useState } from "react"
import { applyUpdate, type UpdateStatus } from "../lib/updates"

const laterKey = "legato:update-later"

function putOff(): string {
  try {
    return localStorage.getItem(laterKey) ?? ""
  } catch {
    return ""
  }
}

// UpdateNotice is the bar along the top that says a newer Legato is out
// (in ask mode) and installs it when asked, and says how an update goes.
// "Later" hides it in this browser until the next release.
export function UpdateNotice({ status, baseUrl }: { status: UpdateStatus | null | undefined; baseUrl: string }) {
  const [later, setLater] = useState(putOff)
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState("")
  if (!status?.release || !status.latest) return null

  const update = async () => {
    setStarting(true)
    setError("")
    try {
      await applyUpdate(baseUrl)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setStarting(false)
    }
  }
  const putOffThis = () => {
    try {
      localStorage.setItem(laterKey, status.latest!)
    } catch {
      // Hidden for this page only.
    }
    setLater(status.latest!)
  }

  let text: string
  let actions = false
  switch (status.state) {
    case "installing":
    case "restarting":
      text = `Updating Legato to ${status.latest}. This page reconnects when it is back.`
      break
    case "installed":
      if (later === status.latest) return null
      text = `Legato ${status.latest} is installed. Restart Legato to use it.`
      break
    case "failed":
      if (later === status.latest) return null
      text = `Updating Legato to ${status.latest} failed: ${status.error ?? "unknown error"}`
      actions = true
      break
    case "available":
      if (status.mode !== "ask" || !status.available || later === status.latest) return null
      text = `Legato ${status.latest} is available. This server runs ${status.version}.`
      actions = true
      break
    default:
      return null
  }

  const button = "shrink-0 rounded px-3 py-1.5 text-sm font-medium disabled:opacity-50"
  return (
    <div role="status" className="fixed inset-x-4 top-4 z-[65] mx-auto flex max-w-xl flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border border-lw-accent/50 bg-zinc-950 p-4 text-sm text-zinc-200 shadow-xl">
      <span className="min-w-0 flex-1">
        {text}
        {status.latest_url && (
          <> <a href={status.latest_url} target="_blank" rel="noreferrer" className="text-lw-link underline">What's new</a></>
        )}
        {error && <span role="alert" className="mt-1 block text-lw-error">{error}</span>}
      </span>
      {actions && (
        <span className="flex gap-2">
          <button className={button + " text-zinc-300 hover:bg-zinc-800"} onClick={putOffThis}>Later</button>
          <button className={button + " bg-lw-accent text-white"} disabled={starting} onClick={() => void update()}>
            {status.state === "failed" ? "Try again" : "Update and restart"}
          </button>
        </span>
      )}
      {status.state === "installed" && (
        <button className={button + " text-zinc-300 hover:bg-zinc-800"} onClick={putOffThis}>Dismiss</button>
      )}
    </div>
  )
}
