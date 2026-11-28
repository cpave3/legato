import { useCallback } from "react"
import { Wand2 } from "lucide-react"
import { SettingsCard } from "./SettingsUI"
import { FolderItemRow } from "./FolderItemRow"
import { listSkills, setSkillEnabled, type Skill } from "../../lib/skills"
import { useFolderToggles } from "./useFolderToggles"

// SkillsSettings lists the skills in Legato's skills folder and turns each
// on or off for agents started from now on. Skills are added, changed and
// removed in the folder itself.
export function SkillsSettings({ baseUrl }: { baseUrl: string }) {
  const read = useCallback(async () => {
    const { dir, skills } = await listSkills(baseUrl)
    return { dir, items: skills }
  }, [baseUrl])
  const save = useCallback((name: string, enabled: boolean) => setSkillEnabled(baseUrl, name, enabled), [baseUrl])
  const { list, busy, error, toggle } = useFolderToggles<Skill>(read, save)

  const folder = list?.dir ? <code className="font-lw-mono text-lw-text">{list.dir}</code> : "the skills folder"

  return (
    <SettingsCard id="skills" icon={Wand2} title="Skills"
      description={<>Skills from {folder}, one folder each with a SKILL.md. Agents started from now on get the skills that are on.</>}>
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className="text-sm text-lw-error">{error}</p>}
        {list && list.items.length === 0 && (
          <p className="text-sm text-lw-text-2">No skills yet. Add a folder with a SKILL.md to {folder}.</p>
        )}
        {list && list.items.length > 0 && (
          <ul className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
            {list.items.map((skill) => (
              <FolderItemRow key={skill.name} name={skill.name} error={skill.error} path={skill.path} enabled={skill.enabled}
                busy={busy === skill.name} onToggle={() => void toggle(skill.name, !skill.enabled)}>
                <p className="mt-0.5 text-xs text-lw-text-2">{skill.description}</p>
              </FolderItemRow>
            ))}
          </ul>
        )}
      </div>
    </SettingsCard>
  )
}
