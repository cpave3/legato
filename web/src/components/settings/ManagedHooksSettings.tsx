import { useCallback } from "react"
import { ShieldCheck } from "lucide-react"
import { SettingsCard } from "./SettingsUI"
import { FolderItemRow } from "./FolderItemRow"
import { listManagedHooks, setManagedHookEnabled, type ManagedHook } from "../../lib/managedHooks"
import { useFolderToggles } from "./useFolderToggles"

// ManagedHooksSettings lists the hooks in Legato's managed-hooks folder and
// turns each on or off for every agent started from now on. Hooks are added,
// changed and removed in the folder itself.
export function ManagedHooksSettings({ baseUrl }: { baseUrl: string }) {
  const read = useCallback(async () => {
    const { dir, hooks } = await listManagedHooks(baseUrl)
    return { dir, items: hooks }
  }, [baseUrl])
  const save = useCallback((name: string, enabled: boolean) => setManagedHookEnabled(baseUrl, name, enabled), [baseUrl])
  const { list, busy, error, toggle } = useFolderToggles<ManagedHook>(read, save)

  const folder = list?.dir ? <code className="font-lw-mono text-lw-text">{list.dir}</code> : "the managed-hooks folder"

  return (
    <SettingsCard id="managed-hooks" icon={ShieldCheck} title="Managed hooks"
      description={<>Hooks from {folder}, one folder each with a HOOK.yaml. Agents started from now on get the hooks that are on.</>}>
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className="text-sm text-lw-error">{error}</p>}
        {list && list.items.length === 0 && (
          <p className="text-sm text-lw-text-2">No managed hooks yet. Add a folder with a HOOK.yaml to {folder}.</p>
        )}
        {list && list.items.length > 0 && (
          <ul className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
            {list.items.map((hook) => (
              <FolderItemRow key={hook.name} name={hook.name} error={hook.error} path={hook.path} enabled={hook.enabled}
                busy={busy === hook.name} onToggle={() => void toggle(hook.name, !hook.enabled)}>
                <p className="mt-0.5 text-xs text-lw-text-2">{hook.description}</p>
                <p className="mt-0.5 text-xs text-lw-faint">
                  Runs at {hook.events.join(", ")} · for {hook.agent_tools.length > 0 ? hook.agent_tools.join(", ") : "every agent tool"}
                  {hook.read_only && " · read-only agents too"}
                </p>
              </FolderItemRow>
            ))}
          </ul>
        )}
      </div>
    </SettingsCard>
  )
}
