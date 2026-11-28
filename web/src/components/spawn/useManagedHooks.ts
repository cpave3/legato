import { useEffect, useState } from "react"
import { listManagedHooks, managedHooksFor, type ManagedHookList } from "../../lib/managedHooks"

const none: ManagedHookList = { dir: "", hooks: [], supportedTools: [] }

// useManagedHooks is the managed hooks an agent of tool would be given,
// listed each time the dialog opens, and those unticked for it. Every box
// starts ticked; off is only what is unticked among the hooks shown for the
// tool now, so one unticked under another tool is not sent.
export function useManagedHooks(openKey: string, baseUrl: string, tool: string) {
  const [list, setList] = useState<ManagedHookList>(none)
  const [unticked, setUnticked] = useState<string[]>([])

  useEffect(() => {
    if (!openKey) return
    let current = true
    setUnticked([])
    // A server without managed hooks (503) or older than them offers none.
    listManagedHooks(baseUrl)
      .then((listed) => { if (current) setList(listed) })
      .catch(() => { if (current) setList(none) })
    return () => { current = false }
  }, [openKey, baseUrl])

  const hooks = managedHooksFor(list, tool)
  const toggle = (name: string) => setUnticked((now) => now.includes(name) ? now.filter((other) => other !== name) : [...now, name])
  return { hooks, isOn: (name: string) => !unticked.includes(name), toggle, off: hooks.map((hook) => hook.name).filter((name) => unticked.includes(name)) }
}

export type ManagedHooksChoice = ReturnType<typeof useManagedHooks>
