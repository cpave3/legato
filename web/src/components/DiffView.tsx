import { cn } from "../lib/utils"
import type { FileDiff } from "../lib/review"

export function DiffView({ files }: { files: FileDiff[] }) {
  if (files.length === 0) {
    return <div className="rounded border border-zinc-800 p-6 text-center text-sm text-zinc-500">No file changes in this step.</div>
  }

  return (
    <div className="space-y-4 font-mono text-xs">
      {files.map((file, fileIndex) => {
        const path = file.old_path !== file.new_path
          ? `${file.old_path} → ${file.new_path}`
          : file.new_path || file.old_path
        return (
          <section key={`${path}-${fileIndex}`} className="overflow-hidden rounded border border-zinc-800 bg-zinc-950">
            <header className="flex items-center justify-between border-b border-zinc-800 bg-zinc-900 px-3 py-2">
              <span className="truncate text-zinc-200">{path}</span>
              <span className="ml-3 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase text-zinc-400">{file.status}</span>
            </header>
            {file.hunks.map((hunk, hunkIndex) => (
              <div key={`${hunk.header}-${hunkIndex}`}>
                <div className="border-y border-zinc-800 bg-indigo-950/40 px-3 py-1 text-indigo-300">{hunk.header}</div>
                {hunk.lines.map((line, lineIndex) => (
                  <div
                    key={lineIndex}
                    className={cn(
                      "grid grid-cols-[3rem_3rem_1fr] leading-5",
                      line.kind === "add" && "bg-emerald-950/40 text-emerald-200",
                      line.kind === "del" && "bg-red-950/40 text-red-200",
                      line.kind === "ctx" && "text-zinc-400",
                    )}
                  >
                    <span aria-label={line.old_no ? `old line ${line.old_no}` : undefined} className="select-none border-r border-zinc-800 px-2 text-right text-zinc-600">
                      {line.old_no || ""}
                    </span>
                    <span aria-label={line.new_no ? `new line ${line.new_no}` : undefined} className="select-none border-r border-zinc-800 px-2 text-right text-zinc-600">
                      {line.new_no || ""}
                    </span>
                    <span className="whitespace-pre px-2"><span aria-hidden>{line.kind === "add" ? "+" : line.kind === "del" ? "-" : " "}</span>{line.text}</span>
                  </div>
                ))}
              </div>
            ))}
          </section>
        )
      })}
    </div>
  )
}
