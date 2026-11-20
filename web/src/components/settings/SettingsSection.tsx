import type { ReactNode } from "react"
import type { ConfigList, ConfigSetting } from "../../lib/configSettings"
import { fileOnlyPlace, fileOnlyTitle, groupApplies, groupElementId, hasOwnCard, leadGroup, listPlace, listsWithCards, sectionGroups, settingPlace, settingsCards, type SettingsSection } from "../../lib/settingsCatalog"
import { FileOnlyRow, RegistrySettings } from "./RegistrySettings"
import { SettingsRows } from "./SettingsUI"
import { modeLabel } from "../../lib/settingValues"
import { partnerKeys, settingPickers } from "../../lib/settingPickers"
import { ListCard } from "./ListSettings"

// What a section draws: the registry's settings by the API's place, the
// cards and lists by the catalog's, and the file-only keys read-only.
export interface SectionContent {
  settings: ConfigSetting[]
  fileOnly: string[]
  fileOnlyValues?: Record<string, string>
  lists: ConfigList[]
  baseUrl: string
  // The cards this page can draw, by ID; a card left out (Development
  // without hot reload) is not shown.
  cards: Record<string, ReactNode>
}

export function SettingsSectionView({ section, content }: { section: SettingsSection; content: SectionContent }) {
  const { settings, fileOnly, fileOnlyValues = {}, lists, baseUrl, cards } = content
  const groups = sectionGroups(section, settings)
  return <section id={`section-${section.id}`} aria-labelledby={`section-${section.id}-title`} className="flex flex-col gap-6">
    <div>
      <h2 id={`section-${section.id}-title`} className="text-xl font-semibold text-lw-text">{section.title}</h2>
      {section.intro && <p className="mt-1 max-w-2xl text-sm text-lw-text-2">{section.intro}</p>}
    </div>
    {groups.map((group) => {
      const at = (place: { section: string; group: string }) => place.section === section.id && place.group === group
      // A model or tier is drawn in its agent's row, when that is here.
      const drawnByAgent = (key: string) => partnerKeys.includes(key) && settings.some((s) => { const p = settingPickers[s.key]; return p?.kind === "agent" && [p.modelOrTier, p.tier, p.model].includes(key) })
      const rows = settings.filter((s) => at(settingPlace(s)) && !hasOwnCard(s.key) && !drawnByAgent(s.key))
      const groupCards = settingsCards.filter((card) => at(card) && cards[card.id] !== undefined)
      const groupLists = lists.filter((list) => at(listPlace(list.key)) && !listsWithCards.includes(list.key))
      const files = fileOnly.filter((key) => at(fileOnlyPlace(key)))
      if (rows.length + groupCards.length + groupLists.length + files.length === 0) return null
      // A group whose settings share when they apply says it once; else
      // each row says its own.
      const modes = new Set(settings.filter((s) => at(settingPlace(s))).map((s) => s.mode))
      const shared = groupApplies[`${section.id}/${group}`] ?? (modes.size === 1 ? modeLabel([...modes][0]) : "")
      const body = <div className="flex flex-col gap-4">
        {groupCards.filter((card) => !card.after).map((card) => <div key={card.id}>{cards[card.id]}</div>)}
        <RegistrySettings settings={rows} baseUrl={baseUrl} prefixes={rows.map((s) => s.key)} showMode={modes.size > 1 && !groupApplies[`${section.id}/${group}`]} />
        {groupCards.filter((card) => card.after).map((card) => <div key={card.id}>{cards[card.id]}</div>)}
        {groupLists.map((list) => <ListCard key={`${baseUrl}:${list.key}`} list={list} baseUrl={baseUrl} />)}
        {files.length > 0 && <SettingsRows>{files.map((key) => <FileOnlyRow key={key} keyName={key} title={fileOnlyTitle(key)} value={fileOnlyValues[key]} />)}</SettingsRows>}
      </div>
      if (group === leadGroup) return <div key={group} id={groupElementId(section.id, group)}>{body}</div>
      const id = groupElementId(section.id, group)
      return <section key={group} id={groupElementId(section.id, group)} aria-labelledby={`${groupElementId(section.id, group)}-title`} className="scroll-mt-6">
        <div className="mb-2 flex items-baseline gap-3">
          <h3 id={`${id}-title`} className="text-sm font-semibold text-lw-text">{group}</h3>
          {shared && <span className="ml-auto text-xs text-lw-text-2" data-testid={`${id}-applies`}>{shared}</span>}
        </div>
        {body}
      </section>
    })}
  </section>
}
