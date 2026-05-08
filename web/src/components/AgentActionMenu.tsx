import { useState, useRef, useEffect } from "react"
import { useServer } from "../hooks/useServer"
import { useToast } from "../hooks/useToast"
import { messageWorker, closeWorker, finishSwarm } from "../lib/swarm"
import { cn } from "../lib/utils"
import { MoreHorizontal, MessageSquare, X, Send } from "lucide-react"

export function AgentActionMenu({
  agentTaskId,
  parentTaskId,
  subtaskId,
  role,
}: {
  agentTaskId: string
  parentTaskId: string
  subtaskId: string
  role: string
}) {
  const { baseUrl } = useServer()
  const { addToast } = useToast()
  const [menuOpen, setMenuOpen] = useState(false)
  const [sendOpen, setSendOpen] = useState(false)
  const [finishOpen, setFinishOpen] = useState(false)
  const [text, setText] = useState("")
  const [summary, setSummary] = useState("")
  const [sending, setSending] = useState(false)
  const [error, setError] = useState("")
  const menuRef = useRef<HTMLDivElement>(null)
  const isWorker = role !== "conductor"

  useEffect(() => {
    if (!menuOpen) return
    const onClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false)
      }
    }
    document.addEventListener("click", onClick, true)
    return () => document.removeEventListener("click", onClick, true)
  }, [menuOpen])

  const handleSendMessage = async () => {
    if (!text.trim()) return
    setSending(true)
    setError("")
    try {
      // messageWorker accepts task_id (subtaskID or parentID); server routes it.
      await messageWorker(baseUrl, agentTaskId, text.trim())
      addToast("Message sent", "success")
      setSendOpen(false)
      setText("")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to send message")
    } finally {
      setSending(false)
    }
  }

  const handleCloseWorker = async () => {
    if (!confirm(`Close worker ${subtaskId}?`)) return
    setError("")
    try {
      await closeWorker(baseUrl, subtaskId)
      addToast("Worker closed", "success")
    } catch (err) {
      addToast(err instanceof Error ? err.message : "Failed to close worker", "error")
    }
  }

  const handleFinishSwarm = async () => {
    if (!summary.trim()) return
    setSending(true)
    setError("")
    try {
      await finishSwarm(baseUrl, parentTaskId, summary.trim())
      addToast("Swarm finished", "success")
      setFinishOpen(false)
      setSummary("")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to finish swarm")
    } finally {
      setSending(false)
    }
  }

  return (
    <>
      <div className="relative" ref={menuRef}>
        <button
          onClick={() => setMenuOpen((v) => !v)}
          className={cn(
            "rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200",
            menuOpen && "bg-zinc-800 text-zinc-200"
          )}
          title="Actions"
        >
          <MoreHorizontal size={14} />
        </button>
        {menuOpen && (
          <div className="absolute right-0 top-full mt-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[180px] z-50">
            <button
              onClick={() => { setMenuOpen(false); setSendOpen(true); setError("") }}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
            >
              <MessageSquare size={12} />
              Send message
            </button>
            {isWorker ? (
              <button
                onClick={() => { setMenuOpen(false); handleCloseWorker() }}
                className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-red-400 hover:bg-red-950 transition-colors"
              >
                <X size={12} />
                Close worker
              </button>
            ) : (
              <button
                onClick={() => { setMenuOpen(false); setFinishOpen(true); setError("") }}
                className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-indigo-300 hover:bg-zinc-800 transition-colors"
              >
                <Send size={12} />
                Finish swarm
              </button>
            )}
          </div>
        )}
      </div>

      {/* Send message modal */}
      {sendOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="w-full max-w-md rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
            <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
              <h2 className="text-sm font-semibold text-zinc-200">Send Message</h2>
              <button
                onClick={() => { setSendOpen(false); setText(""); setError("") }}
                className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
              >
                <X size={16} />
              </button>
            </div>
            <div className="px-5 py-4 space-y-3">
              {error && (
                <div className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">
                  {error}
                </div>
              )}
              <textarea
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder="Type your message..."
                rows={4}
                className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500 resize-none"
                autoFocus
              />
              <div className="flex justify-end">
                <button
                  onClick={handleSendMessage}
                  disabled={sending || !text.trim()}
                  className="rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
                >
                  {sending ? "Sending..." : "Send"}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Finish swarm modal */}
      {finishOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="w-full max-w-md rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
            <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
              <h2 className="text-sm font-semibold text-zinc-200">Finish Swarm</h2>
              <button
                onClick={() => { setFinishOpen(false); setSummary(""); setError("") }}
                className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
              >
                <X size={16} />
              </button>
            </div>
            <div className="px-5 py-4 space-y-3">
              {error && (
                <div className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">
                  {error}
                </div>
              )}
              <label className="text-xs text-zinc-500 block">Summary</label>
              <textarea
                value={summary}
                onChange={(e) => setSummary(e.target.value)}
                placeholder="Describe the outcome of the swarm..."
                rows={5}
                className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500 resize-none"
                autoFocus
              />
              <div className="flex justify-end">
                <button
                  onClick={handleFinishSwarm}
                  disabled={sending || !summary.trim()}
                  className="rounded bg-emerald-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-emerald-500 disabled:opacity-40"
                >
                  {sending ? "Finishing..." : "Finish Swarm"}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
