import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Bot, CheckCircle, Eye, BellRing, CircleHelp, GitMerge, GitPullRequest, Hand, ListChecks, MessageSquare, Settings2, ShieldCheck, Workflow, type LucideIcon } from "lucide-react"
import { setConfigSetting, type ConfigSetting } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"
import { SettingsRow, SettingsRows, Toggle } from "./SettingsUI"
import { RegistrySettingRow, SourceChip } from "./RegistrySettings"
import { useAutoOpen, writeAutoOpen, type AutoOpen } from "../../lib/railAutoOpen"

type RailSwitch = { group: string; title: string; description: string; icon: LucideIcon }

// What agents post for the operator with for_operator, by kind.
const fromAgents: RailSwitch[] = [
  { group: "actions", title: "Actions", description: "Something only you can do: a command, a test in a browser, a merge.", icon: Hand },
  { group: "questions", title: "Questions", description: "A question an agent asked you in plain words.", icon: CircleHelp },
  { group: "done", title: "Finished work", description: "Work an agent finished for you to look at.", icon: CheckCircle },
  { group: "other", title: "Anything else", description: "Anything else an agent wants you to see.", icon: MessageSquare },
]

const fromLegato: RailSwitch[] = [
  { group: "approvals", title: "Approvals", description: "Requests that hold an agent mid-turn, and paired instances' change sets.", icon: ShieldCheck },
  { group: "agents", title: "Agents", description: "Agents waiting for you or stopped on an error.", icon: Bot },
  { group: "workflows", title: "Workflows", description: "Gates, failures, stuck review loops and triggers.", icon: Workflow },
  { group: "plans", title: "Plans", description: "Plans waiting for your decision.", icon: ListChecks },
  { group: "review", title: "Ready for review", description: "Review tours ready and handoffs done.", icon: GitPullRequest },
  { group: "merge", title: "Merge blockers", description: "Pull requests that cannot merge, and Argus's cases.", icon: GitMerge },
  { group: "legato", title: "Legato", description: "Updates, restarts, settings, devices, handoff, integrations, engrams and interrupted agents.", icon: Settings2 },
]

// RailSettings switches what the needs-you rail shows, one switch per kind
// of item (the web.rail.* settings, kept by Legato so every client agrees).
export function RailSettings({ baseUrl, settings }: { baseUrl: string; settings: ConfigSetting[] }) {
  const queryClient = useQueryClient()
  const [error, setError] = useState("")
  const [busy, setBusy] = useState("")
  // When the phone's sheet opens by itself: kept in this browser.
  const autoOpen = useAutoOpen(baseUrl)
  const flipDevice = (key: keyof AutoOpen) => writeAutoOpen(baseUrl, { ...autoOpen, [key]: !autoOpen[key] })
  const setting = (group: string) => settings.find((s) => s.key === `web.rail.${group}`)
  const flip = async (key: string, on: boolean) => {
    setBusy(key)
    setError("")
    try {
      await setConfigSetting(baseUrl, key, !on)
      await queryClient.invalidateQueries({ queryKey: queryKeys.configSettings(baseUrl) })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy("")
    }
  }
  const switches = (label: string, list: RailSwitch[]) => (
    <section role="group" aria-label={label} className="flex flex-col gap-2">
      <h4 className="font-lw-mono text-[10.5px] uppercase tracking-wider text-lw-text-2">{label}</h4>
      <SettingsRows>
        {list.map((item) => {
          const current = setting(item.group)
          if (!current) return null
          const on = current.value === true
          return (
            <div key={item.group} id={`setting-${current.key}`} tabIndex={-1} className="scroll-mt-40">
              <SettingsRow icon={item.icon} title={item.title} description={item.description}
                chip={<SourceChip setting={current} />}
                control={<Toggle label={item.title} on={on} disabled={current.pinned || busy === current.key} onChange={() => void flip(current.key, on)} />} />
            </div>
          )
        })}
      </SettingsRows>
    </section>
  )
  if (!settings.some((s) => s.key.startsWith("web.rail."))) return null
  // How long the phone's sheet, which opens by itself, stays closed.
  const snooze = settings.find((s) => s.key === "web.rail.snooze")
  return (
    <div id="needs-you-rail" className="flex flex-col gap-3">
      <p className="text-[13px] text-lw-text-2">What the rail on the right of every page shows. Approvals come first, then the rest, newest first. The Attention page always lists everything.</p>
      <div className="grid gap-4 lg:grid-cols-2">
        {switches("From agents", fromAgents)}
        {switches("From Legato", fromLegato)}
      </div>
      <section role="group" aria-label="On a phone" className="flex flex-col gap-2">
        <h4 className="font-lw-mono text-[10.5px] uppercase tracking-wider text-lw-text-2">On a phone</h4>
        <p className="text-[13px] text-lw-text-2">The rail is a sheet that opens by itself when something waits. It never opens while you type, in focus mode or on the Attention page.</p>
        {snooze && <SettingsRows><RegistrySettingRow setting={snooze} baseUrl={baseUrl} /></SettingsRows>}
        <p className="text-[13px] text-lw-text-2">These two apply to this device only, and each phone or browser keeps its own:</p>
        <SettingsRows>
          <SettingsRow icon={Eye} title="For the agent in view" browser
            description="An approval, question, plan or gate from the agent whose page you have open opens the sheet too."
            control={<Toggle label="For the agent in view" on={autoOpen.agentInView} onChange={() => flipDevice("agentInView")} />} />
          <SettingsRow icon={BellRing} title="For items that don't hold work" browser
            description="A waiting agent, finished work and the like open the sheet. Off, only approvals, questions, plans and gates do."
            control={<Toggle label="For items that don't hold work" on={autoOpen.everything} onChange={() => flipDevice("everything")} />} />
        </SettingsRows>
      </section>
      {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
    </div>
  )
}
