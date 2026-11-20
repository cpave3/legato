import type { ReactNode } from "react"
import type { ConfigList, ConfigSetting } from "../../lib/configSettings"
import { fileOnlyPlace, groupElementId, hasOwnCard, leadGroup, listPlace, listsWithCards, sectionGroups, settingPlace, settingsCards, type SettingsSection } from "../../lib/settingsCatalog"
import { RegistrySettings } from "./RegistrySettings"
import { ListCard } from "./ListSettings"

// What a section draws: the registry's settings by the API's place, the
// cards and lists by the catalog's, and the file-only keys read-only.
export interface SectionContent {
  settings: ConfigSetting[]
  fileOnly: string[]
  lists: ConfigList[]
  baseUrl: string
  // The cards this page can draw, by ID; a card left out (Development
  // without hot reload) is not shown.
  cards: Record<string, ReactNode>
}

function FileOnlyRow({ keyName }: { keyName: string }) {
  return <section id={`setting-${keyName}`} tabIndex={-1} className="scroll-mt-40 rounded-lg border border-lw-line p-4">
    <h4 className="break-all text-sm font-medium">{keyName}</h4>
    <p className="mt-1 text-sm text-lw-text-2">config.yaml only · Read-only here. Edit <code className="break-all">{keyName}</code> in this server’s <code>config.yaml</code>, then restart Legato to apply the change.</p>
  </section>
}

export function SettingsSectionView({ section, content }: { section: SettingsSection; content: SectionContent }) {
  const { settings, fileOnly, lists, baseUrl, cards } = content
  const groups = sectionGroups(section, settings)
  return <section id={`section-${section.id}`} aria-labelledby={`section-${section.id}-title`} className="flex flex-col gap-6">
    <div>
      <h2 id={`section-${section.id}-title`} className="text-xl font-semibold text-lw-text">{section.title}</h2>
      {section.intro && <p className="mt-1 max-w-2xl text-sm text-lw-text-2">{section.intro}</p>}
    </div>
    {groups.map((group) => {
      const at = (place: { section: string; group: string }) => place.section === section.id && place.group === group
      const rows = settings.filter((s) => at(settingPlace(s)) && !hasOwnCard(s.key))
      const groupCards = settingsCards.filter((card) => at(card) && cards[card.id] !== undefined)
      const groupLists = lists.filter((list) => at(listPlace(list.key)) && !listsWithCards.includes(list.key))
      const files = fileOnly.filter((key) => at(fileOnlyPlace(key)))
      if (rows.length + groupCards.length + groupLists.length + files.length === 0) return null
      const body = <div className="flex flex-col gap-4">
        {groupCards.map((card) => <div key={card.id}>{cards[card.id]}</div>)}
        <RegistrySettings settings={rows} baseUrl={baseUrl} prefixes={rows.map((s) => s.key)} />
        {groupLists.map((list) => <ListCard key={`${baseUrl}:${list.key}`} list={list} baseUrl={baseUrl} />)}
        {files.map((key) => <FileOnlyRow key={key} keyName={key} />)}
      </div>
      if (group === leadGroup) return <div key={group} id={groupElementId(section.id, group)}>{body}</div>
      return <section key={group} id={groupElementId(section.id, group)} aria-labelledby={`${groupElementId(section.id, group)}-title`} className="scroll-mt-6">
        <h3 id={`${groupElementId(section.id, group)}-title`} className="mb-2 text-sm font-semibold text-lw-text">{group}</h3>
        {body}
      </section>
    })}
  </section>
}
