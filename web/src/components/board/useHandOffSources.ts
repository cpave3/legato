import { useEffect, useState } from "react"

import { apiFetch } from "../../lib/api"
import { workerReviewPasses, type ReviewOverride, type ReviewPassesAdvert } from "../../lib/away"
import { localToolOrder } from "../../lib/handoffForm"
import { listMCPConnections } from "../../lib/mcpConnections"
import { listSkills } from "../../lib/skills"
import { handoffBlock, listWorkers, type Worker } from "../../lib/workers"

// The tools this Legato drives, its default first, or null when they can't
// be read.
async function readLocalTools(baseUrl: string): Promise<string[] | null> {
  try {
    const response = await apiFetch(baseUrl, "/api/adapters")
    if (!response.ok) return null
    return localToolOrder(await response.json())
  } catch {
    return null
  }
}

// The skills and MCP connections this Legato's agents get. What a worker
// lacks is a warning; failing to read it here is none.
async function readHere(baseUrl: string): Promise<{ skills: string[]; mcpConnections: string[] }> {
  const [skills, mcpConnections] = await Promise.all([
    listSkills(baseUrl).then((list) => list.skills.filter((s) => s.enabled && !s.error).map((s) => s.name)).catch(() => []),
    listMCPConnections(baseUrl).then((list) => list.filter((c) => c.enabled).map((c) => c.name)).catch(() => []),
  ])
  return { skills, mcpConnections }
}

// The first worker that can take a handoff, or none.
function firstAvailableWorker(workers: Worker[]): string {
  return workers.find((w) => handoffBlock(w) === null)?.instance_id ?? ""
}

export function errorText(reason: unknown): string {
  return reason instanceof Error ? reason.message : String(reason)
}

// What the Hand off form reads each time it opens: the paired workers, this
// Legato's own tools, and the skills and MCP connections its agents get, to
// compare with the chosen worker's advert. The first worker that can take
// the handoff is chosen unless one already is.
export function useHandOffSources(open: boolean, baseUrl: string, chooseWorker: (pick: (chosen: string) => string) => void, setError: (error: string | null) => void) {
  const [workers, setWorkers] = useState<Worker[] | null>(null)
  const [localTools, setLocalTools] = useState<string[]>([])
  const [here, setHere] = useState<{ skills: string[]; mcpConnections: string[] }>({ skills: [], mcpConnections: [] })

  useEffect(() => {
    if (!open) return
    let current = true
    setError(null)
    listWorkers(baseUrl)
      .then((list) => {
        if (!current) return
        setWorkers(list.workers)
        chooseWorker((chosen) => chosen || firstAvailableWorker(list.workers))
      })
      .catch((reason) => { if (current) setError(errorText(reason)) })
    readLocalTools(baseUrl).then((ordered) => { if (current && ordered) setLocalTools(ordered) })
    readHere(baseUrl).then((found) => { if (current) setHere(found) })
    return () => { current = false }
    // chooseWorker and setError are state setters, stable for the form's life.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, baseUrl])

  return { workers, localTools, here }
}

// The chosen worker's review passes, and this handoff's changes to them,
// which start empty again whenever another worker is chosen.
export function useWorkerReviewPasses(open: boolean, baseUrl: string, worker: string) {
  const [advert, setAdvert] = useState<ReviewPassesAdvert | null>(null)
  const [review, setReview] = useState<ReviewOverride>({})

  useEffect(() => {
    if (!open || worker === "") {
      setAdvert(null)
      return
    }
    let current = true
    setReview({})
    workerReviewPasses(baseUrl, worker).then((found) => { if (current) setAdvert(found) })
    return () => { current = false }
  }, [open, baseUrl, worker])

  return { advert, review, setReview }
}

// The agent tool the form sends: the first of the tools offered until the
// person chooses one, and theirs while it is still offered. The choice is
// forgotten when the form closes.
export function useFollowedTool(open: boolean, tools: string[]) {
  const [tool, setTool] = useState("")
  const [toolChosen, setToolChosen] = useState(false)
  useEffect(() => {
    if (!open) setToolChosen(false)
  }, [open])
  // tools is rebuilt each render; its names say when it changed.
  const toolsKey = tools.join(" ")
  useEffect(() => {
    setTool((chosen) => (toolChosen && tools.includes(chosen) ? chosen : tools[0] ?? ""))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toolsKey, toolChosen])
  const choose = (kind: string) => {
    setTool(kind)
    setToolChosen(true)
  }
  return { tool, choose }
}
