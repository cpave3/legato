import { HighlightStyle, type TagStyle } from "@codemirror/language"
import { tags } from "@lezer/highlight"

// The YAML's colours come from the selected theme's syntax roles
// (--theme-keyword, --theme-string, …, which appTheme sets from the
// palette), and fall back to Legato's own roles in the Legato theme, which
// has none. CodeMirror's defaultHighlightStyle is a light-theme palette:
// dark blue keys on a dark editor could not be read.
const role = (theme: string, fallback: string) => `var(--theme-${theme}, var(--color-lw-${fallback}))`

export const yamlHighlightSpecs: TagStyle[] = [
  { tag: [tags.definition(tags.propertyName), tags.propertyName], color: role("keyword", "link") },
  { tag: [tags.string, tags.special(tags.string), tags.attributeValue], color: role("string", "live") },
  { tag: [tags.number, tags.bool, tags.null, tags.atom], color: role("number", "needs") },
  { tag: [tags.lineComment, tags.comment, tags.meta], color: role("comment", "faint"), fontStyle: "italic" },
  { tag: [tags.keyword], color: role("keyword", "accent-text") },
  { tag: [tags.labelName, tags.typeName], color: role("type", "done") },
  { tag: [tags.separator, tags.punctuation, tags.squareBracket, tags.brace], color: "var(--color-lw-text-2)" },
  { tag: [tags.invalid], color: "var(--color-lw-error)" },
]

export const yamlHighlightStyle = HighlightStyle.define(yamlHighlightSpecs)
