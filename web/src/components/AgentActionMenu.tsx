import { createPortal } from "react-dom"
import { useMenuLayer } from "../hooks/useMenuLayer"
import { Dialog, DialogPanel, DialogTitle } from "./ui/Dialog"
import { useState, useRef } from "react"
import { useServer } from "../hooks/useServer"
import { useToast } from "../hooks/useToast"
import { useConfirm } from "../hooks/useConfirm"
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
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [menuOpen, setMenuOpen] = useState(false)
  const [sendOpen, setSendOpen] = useState(false)
  const [finishOpen, setFinishOpen] = useState(false)
  const [text, setText] = useState("")
  const [summary, setSummary] = useState("")
  const [sending, setSending] = useState(false)
  const [error, setError] = useState("")
  const menuRef = useRef<HTMLDivElement>(null)
  const isWorker = role !== "conductor"

  const trigger = useRef<HTMLButtonElement>(null)
  useMenuLayer({ open: menuOpen, onOpen: () => setMenuOpen(true), onClose: () => setMenuOpen(false), containerRef: menuRef, triggerRef: trigger, align: "right" })
  const closeSend = () => { setSendOpen(false); setText(""); setError(""); trigger.current?.focus() }
  const closeFinish = () => { setFinishOpen(false); setSummary(""); setError(""); trigger.current?.focus() }
  const handleSendMessage = async () => {
    if (!text.trim()) return
    setSending(true)
    setError("")
    try {
      // messageWorker accepts task_id (subtaskID or parentID); server routes it.
      await messageWorker(baseUrl, agentTaskId, text.trim())
      addToast("Message sent", "success")
      closeSend()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to send message")
    } finally {
      setSending(false)
    }
  }

  const handleCloseWorker = async () => {
    if (!await confirm({ title: "Close worker", message: `Close worker ${subtaskId}?`, confirmLabel: "Close worker", destructive: true })) return
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
      closeFinish()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to finish swarm")
    } finally {
      setSending(false)
    }
  }

  return (
    <>
      <div className="relative">
        <button
          ref={trigger}
          type="button"
          aria-label="Actions"
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onClick={() => setMenuOpen((v) => !v)}
          className={cn(
            "rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200",
            menuOpen && "bg-zinc-800 text-zinc-200"
          )}
          title="Actions"
        >
          <MoreHorizontal size={14} />
        </button>
        {menuOpen && createPortal(
          <div ref={menuRef} role="menu" aria-label="Agent actions" className="absolute right-0 top-full mt-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[180px] z-50">
            <button type="button" role="menuitem"
              onClick={() => { setMenuOpen(false); trigger.current?.focus(); setSendOpen(true); setError("") }}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
            >
              <MessageSquare size={12} />
              Send message
            </button>
            {isWorker ? (
              <button type="button" role="menuitem"
                onClick={() => { setMenuOpen(false); trigger.current?.focus(); handleCloseWorker() }}
                className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-red-400 hover:bg-red-950 transition-colors"
              >
                <X size={12} />
                Close worker
              </button>
            ) : (
              <button type="button" role="menuitem"
                onClick={() => { setMenuOpen(false); trigger.current?.focus(); setFinishOpen(true); setError("") }}
                className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-indigo-300 hover:bg-zinc-800 transition-colors"
              >
                <Send size={12} />
                Finish swarm
              </button>
            )}
          </div>, document.body
        )}
      </div>

      {confirmDialog}

      {/* Send message modal */}
      {sendOpen && (
        <Dialog open={sendOpen} onClose={closeSend} dismissible={!sending}>
          <DialogPanel>
            <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
              <DialogTitle>Send Message</DialogTitle>
              <button
                aria-label="Close message" disabled={sending} onClick={closeSend}
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
                aria-label="Message"
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
          </DialogPanel>
        </Dialog>
      )}

      {/* Finish swarm modal */}
      {finishOpen && (
        <Dialog open={finishOpen} onClose={closeFinish} dismissible={!sending}>
          <DialogPanel>
            <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
              <DialogTitle>Finish Swarm</DialogTitle>
              <button
                aria-label="Close finish swarm" disabled={sending} onClick={closeFinish}
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
              <label htmlFor="swarm-summary" className="text-xs text-zinc-500 block">Summary</label>
              <textarea
                id="swarm-summary"
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
          </DialogPanel>
        </Dialog>
      )}
    </>
  )
}
