import { engramMarkLabel, takesEngram, type EngramChoice, type EngramPicker } from "./engrams"

// The spawn dialog's Engram field, the parts that are worked out from the
// picker the server answers with.

// The engrams of this place first (the task's last one leads them), then
// the rest.
export function engramGroups(picker: EngramPicker | null): { place: EngramChoice[]; other: EngramChoice[] } {
  const engrams = picker?.engrams ?? []
  const place = engrams.filter((choice) => choice.bound || choice.mark === "last" || choice.mark === "default")
  return { place, other: engrams.filter((choice) => !place.includes(choice)) }
}

// The words for the place: its group's label and its noun.
export function engramPlaceWords(picker: EngramPicker | null): { word: string; noun: string } {
  return picker?.place?.kind === "folder" ? { word: "This folder", noun: "folder" } : { word: "This repo", noun: "repo" }
}

// Whether the field shows: engrams are offered, and the tool takes one.
export function engramOffered(picker: EngramPicker | null, adapter: string): boolean {
  return Boolean(picker?.enabled) && takesEngram(adapter)
}

// Remember is offered only when ticking it would change something: binding
// an engram that is not the place's default, or clearing a default.
export function canRememberEngram(shown: boolean, picker: EngramPicker | null, engram: string): boolean {
  if (!shown || !picker?.place) return false
  const placeDefault = picker.place.default ?? ""
  return engram ? engram !== placeDefault : placeDefault !== ""
}

// An engram's line in the list, with its mark.
export function engramOptionLabel(choice: EngramChoice): string {
  const mark = engramMarkLabel(choice.mark)
  return mark ? `${choice.name} · ${mark}` : choice.name
}

// The pick an answer brings: its pre-selection, when engrams are offered.
export function engramPreselect(picker: EngramPicker): string {
  return picker.enabled ? picker.preselect ?? "" : ""
}
