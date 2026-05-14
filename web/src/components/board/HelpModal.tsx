
import { X } from "lucide-react"

interface HelpModalProps {
  open: boolean
  onClose: () => void
}

const sections = [
  {
    title: "Navigation",
    items: [
      { key: "h/l", desc: "Move between columns" },
      { key: "j/k", desc: "Move up/down within column" },
      { key: "g/G", desc: "Jump to first/last card" },
      { key: "1-5", desc: "Jump to column by number" },
    ],
  },
  {
    title: "Actions",
    items: [
      { key: "enter", desc: "Open task detail" },
      { key: "m", desc: "Move task" },
      { key: "n", desc: "Create new task" },
      { key: "d", desc: "Delete task" },
      { key: "i", desc: "Import remote ticket" },
      { key: "p", desc: "Link PR" },
      { key: "t", desc: "Edit title (local)" },
      { key: "e", desc: "Edit description (local)" },
      { key: "y", desc: "Copy description" },
      { key: "Y", desc: "Copy full context" },
      { key: "o", desc: "Open URL" },
      { key: "X", desc: "Archive done cards" },
      { key: "/", desc: "Search/filter" },
      { key: "w", desc: "Workspace filter" },
      { key: "esc", desc: "Back / close" },
    ],
  },
  {
    title: "Detail View",
    items: [
      { key: "t", desc: "Edit title" },
      { key: "e", desc: "Edit description" },
      { key: "m", desc: "Move" },
      { key: "D", desc: "Delete" },
      { key: "y", desc: "Copy description" },
      { key: "Y", desc: "Copy full context" },
      { key: "o", desc: "Open URL" },
      { key: "esc", desc: "Close detail" },
    ],
  },
]

export function HelpModal({ open, onClose }: HelpModalProps) {
  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="w-full max-w-xl rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Keyboard Reference</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        <div className="grid grid-cols-2 gap-4 px-5 py-4">
          {sections.map((sec) => (
            <div key={sec.title}>
              <h3 className="mb-2 text-xs font-bold uppercase tracking-wider text-[#AFA9EC]">{sec.title}</h3>
              <div className="space-y-1">
                {sec.items.map((item) => (
                  <div key={item.key} className="flex items-center gap-2 text-xs">
                    <kbd className="min-w-[3rem] rounded bg-zinc-800 px-1.5 py-0.5 text-center text-[10px] font-bold text-[#AFA9EC]">
                      {item.key}
                    </kbd>
                    <span className="text-zinc-400">{item.desc}</span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
        <div className="border-t border-zinc-800 px-5 py-2 text-[10px] text-zinc-600">Press ? or esc to close</div>
      </div>
    </div>
  )
}
