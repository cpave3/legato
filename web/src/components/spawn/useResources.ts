import { useEffect, useState } from "react"
import { listResources, type Resource } from "../../lib/resources"

// useResources is the files and folders the person keeps for agents, listed
// each time the dialog opens, and the ones picked for this agent.
export function useResources(openKey: string, baseUrl: string) {
  const [resources, setResources] = useState<Resource[]>([])
  const [picked, setPicked] = useState<string[]>([])

  useEffect(() => {
    if (!openKey) return
    let current = true
    setPicked([])
    listResources(baseUrl)
      .then((listed) => { if (current) setResources(listed) })
      .catch(() => { if (current) setResources([]) })
    return () => { current = false }
  }, [openKey, baseUrl])

  const toggle = (name: string) => setPicked((now) => now.includes(name) ? now.filter((other) => other !== name) : [...now, name])
  return { resources, picked, toggle }
}

export type ResourceChoice = ReturnType<typeof useResources>
