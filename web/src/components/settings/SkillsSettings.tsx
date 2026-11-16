import { useCallback, useEffect, useState } from "react"
import { Wand2 } from "lucide-react"
import { SettingsCard, Toggle } from "./SettingsUI"
import { listSkills, setSkillEnabled, type SkillList } from "../../lib/skills"

// How often the list is read again, so a skill folder added, changed or
// removed on disk shows without reloading the page.
const refreshEvery = 10_000

// SkillsSettings lists the skills in Legato's skills folder and turns each
// on or off for agents started from now on. Skills are added, changed and
// removed in the folder itself.
export function SkillsSettings({ baseUrl }: { baseUrl: string }) {
  const [list, setList] = useState<SkillList | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setList(await listSkills(baseUrl))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])

  useEffect(() => {
    void load()
    const timer = setInterval(() => { void load() }, refreshEvery)
    const onFocus = () => { void load() }
    window.addEventListener("focus", onFocus)
    return () => {
      clearInterval(timer)
      window.removeEventListener("focus", onFocus)
    }
  }, [load])

  const toggle = async (name: string, enabled: boolean) => {
    setBusy(name)
    setError(null)
    try {
      const saved = await setSkillEnabled(baseUrl, name, enabled)
      setList((current) => current && { ...current, skills: current.skills.map((s) => (s.name === name ? saved : s)) })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      await load()
    } finally {
      setBusy(null)
    }
  }

  const folder = list?.dir ? <code className="font-lw-mono text-lw-text">{list.dir}</code> : "the skills folder"

  return (
    <SettingsCard id="skills" icon={Wand2} title="Skills"
      description={<>Skills from {folder}, one folder each with a SKILL.md. Agents started from now on get the skills that are on.</>}>
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className="text-sm text-lw-error">{error}</p>}
        {list && list.skills.length === 0 && (
          <p className="text-sm text-lw-text-2">No skills yet. Add a folder with a SKILL.md to {folder}.</p>
        )}
        {list && list.skills.length > 0 && (
          <ul className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
            {list.skills.map((skill) => (
              <li key={skill.name} aria-label={skill.name} className="flex items-start gap-3 px-3 py-2.5">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-lw-mono text-sm text-lw-text">{skill.name}</span>
                    {skill.error && <span className="rounded border border-lw-line px-1.5 py-px font-lw-mono text-[10px] uppercase tracking-wide text-lw-error">error</span>}
                  </div>
                  {skill.error
                    ? <p className="mt-0.5 text-xs text-lw-error">{skill.error}</p>
                    : <p className="mt-0.5 text-xs text-lw-text-2">{skill.description}</p>}
                  <p className="mt-0.5 truncate font-lw-mono text-[11px] text-lw-faint" title={skill.path}>{skill.path}</p>
                </div>
                {!skill.error && (
                  <Toggle label={`Give agents ${skill.name}`} on={skill.enabled} disabled={busy === skill.name}
                    onChange={() => void toggle(skill.name, !skill.enabled)} />
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </SettingsCard>
  )
}
