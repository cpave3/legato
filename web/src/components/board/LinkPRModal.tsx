import { useState, useEffect, useRef } from "react"
import { useServer } from "../../hooks/useServer"
import { detectRepo, prPreview, linkPR } from "../../lib/board"
import type { PRPreviewResponse } from "../../lib/board-types"

import { X } from "lucide-react"

interface LinkPRModalProps {
  open: boolean
  taskId: string
  onClose: () => void
  onLinked: () => void
}

export function LinkPRModal({ open, taskId, onClose, onLinked }: LinkPRModalProps) {
  const { baseUrl } = useServer()
  const [phase, setPhase] = useState<"input" | "loading" | "confirm">("input")
  const [repo, setRepo] = useState("")
  const [prNumber, setPrNumber] = useState("")
  const [preview, setPreview] = useState<PRPreviewResponse | null>(null)
  const [error, setError] = useState("")
  const [linking, setLinking] = useState(false)
  const repoRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setPhase("input")
    setPrNumber("")
    setPreview(null)
    setError("")
    setLinking(false)
    detectRepo(baseUrl)
      .then((d) => setRepo(`${d.owner}/${d.repo}`))
      .catch(() => setRepo(""))
    setTimeout(() => repoRef.current?.focus(), 50)
  }, [open, baseUrl])

  const handleFetch = async () => {
    setError("")
    const parts = repo.trim().split("/")
    if (parts.length !== 2 || !parts[0] || !parts[1]) {
      setError("Use owner/repo format")
      return
    }
    const num = Number(prNumber.trim())
    if (!num || num <= 0) {
      setError("Enter a valid PR number")
      return
    }
    setPhase("loading")
    try {
      const p = await prPreview(baseUrl, taskId, parts[0], parts[1], num)
      setPreview(p)
      setPhase("confirm")
    } catch (e) {
      setPhase("input")
      setError(e instanceof Error ? e.message : "Failed to fetch PR")
    }
  }

  const handleConfirm = async () => {
    if (!preview || linking) return
    const parts = repo.trim().split("/")
    setLinking(true)
    setError("")
    try {
      await linkPR(baseUrl, taskId, { owner: parts[0], repo: parts[1], pr_number: preview.number })
      onLinked()
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : "Link failed")
    } finally {
      setLinking(false)
    }
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="w-full max-w-sm rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Link PR</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        <div className="px-5 py-4 space-y-3">
          {error && (
            <div role="alert" className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">{error}</div>
          )}
          {(phase === "input" || phase === "loading") && (
            <>
              <div className="space-y-1.5">
                <label htmlFor="link-repo" className="text-xs font-medium text-zinc-400">Repo (owner/repo)</label>
                <input
                  id="link-repo"
                  ref={repoRef}
                  type="text"
                  value={repo}
                  onChange={(e) => setRepo(e.target.value)}
                  className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
                />
              </div>
              <div className="space-y-1.5">
                <label htmlFor="link-pr" className="text-xs font-medium text-zinc-400">PR #</label>
                <input
                  id="link-pr"
                  type="text"
                  value={prNumber}
                  onChange={(e) => setPrNumber(e.target.value.replace(/\D/g, ""))}
                  className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
                />
              </div>
              {phase === "loading" ? (
                <div className="text-xs text-zinc-500">Fetching PR details...</div>
              ) : (
                <div className="text-[10px] text-zinc-600">tab: switch · enter: fetch · esc: cancel</div>
              )}
            </>
          )}
          {phase === "confirm" && preview && (
            <>
              <div className="text-sm font-semibold text-zinc-200">
                #{preview.number}
              </div>
              <div className="text-xs text-zinc-400">
                State: <span className="text-zinc-300">{preview.state}</span>
                {preview.is_draft && " (draft)"}
              </div>
              {preview.comment_count > 0 && (
                <div className="text-xs text-zinc-500">{preview.comment_count} comments</div>
              )}
              <div className="text-[10px] text-zinc-600">Link to task {taskId}?</div>
              <div className="text-[10px] text-zinc-600">y: confirm · n: cancel</div>
            </>
          )}
        </div>
        <div className="flex items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3">
          {phase === "input" && (
            <>
              <button onClick={onClose} className="rounded border border-zinc-700 px-3 py-1.5 text-xs text-zinc-400 hover:bg-zinc-800">
                Cancel
              </button>
              <button
                onClick={handleFetch}
                className="rounded bg-indigo-600 px-3 py-1.5 text-xs text-white hover:bg-indigo-500"
              >
                Fetch
              </button>
            </>
          )}
          {phase === "confirm" && (
            <>
              <button onClick={() => setPhase("input")} className="rounded border border-zinc-700 px-3 py-1.5 text-xs text-zinc-400 hover:bg-zinc-800">
                Back
              </button>
              <button disabled={linking} onClick={handleConfirm} className="rounded bg-indigo-600 px-3 py-1.5 text-xs text-white hover:bg-indigo-500 disabled:cursor-wait disabled:opacity-50">
                {linking ? "Linking…" : "Link"}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
