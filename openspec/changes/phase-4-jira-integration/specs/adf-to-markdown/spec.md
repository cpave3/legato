## ADDED Requirements

### Requirement: Heading Conversion

The ADF converter SHALL convert `heading` nodes (levels 1 through 6) to Markdown headings using the corresponding number of `#` characters.

#### Scenario: Convert heading levels

- **WHEN** an ADF document contains heading nodes at levels 1, 2, and 3
- **THEN** the output contains `#`, `##`, and `###` prefixed lines respectively

#### Scenario: Heading with inline marks

- **WHEN** a heading node contains text with bold or italic marks
- **THEN** the heading text includes the appropriate Markdown formatting (`**bold**`, `*italic*`)

### Requirement: Paragraph Conversion

The ADF converter SHALL convert `paragraph` nodes to plain text followed by a blank line. Inline text marks (bold, italic, code, strikethrough, underline) MUST be converted to their Markdown equivalents.

#### Scenario: Plain paragraph

- **WHEN** an ADF document contains a paragraph with plain text
- **THEN** the output contains the text followed by a blank line

#### Scenario: Paragraph with inline marks

- **WHEN** a paragraph contains text with `strong`, `em`, `code`, and `strike` marks
- **THEN** the output contains `**bold**`, `*italic*`, `` `code` ``, and `~~strikethrough~~` respectively

#### Scenario: Paragraph with link mark

- **WHEN** a paragraph contains text with a `link` mark
- **THEN** the output contains `[text](url)` Markdown link syntax

### Requirement: Bullet List Conversion

The ADF converter SHALL convert `bulletList` nodes to Markdown unordered lists using `- ` prefix. Nested lists MUST be indented with two spaces per level.

#### Scenario: Simple bullet list

- **WHEN** an ADF document contains a bulletList with three items
- **THEN** the output contains three lines each prefixed with `- `

#### Scenario: Nested bullet list

- **WHEN** a bulletList item contains a child bulletList
- **THEN** the nested items are indented with two additional spaces

### Requirement: Ordered List Conversion

The ADF converter SHALL convert `orderedList` nodes to Markdown ordered lists using `1. `, `2. `, etc. prefix. Nested lists MUST be indented with three spaces per level.

#### Scenario: Simple ordered list

- **WHEN** an ADF document contains an orderedList with three items
- **THEN** the output contains lines prefixed with `1. `, `2. `, `3. `

#### Scenario: Nested ordered list

- **WHEN** an orderedList item contains a child orderedList
- **THEN** the nested items are indented with three additional spaces and numbered starting from 1

### Requirement: Code Block Conversion

The ADF converter SHALL convert `codeBlock` nodes to fenced Markdown code blocks using triple backticks. The language attribute, when present, MUST be included after the opening fence.

#### Scenario: Code block with language

- **WHEN** an ADF codeBlock has a `language` attribute of "go"
- **THEN** the output is wrapped in `` ```go `` and `` ``` `` fences

#### Scenario: Code block without language

- **WHEN** an ADF codeBlock has no `language` attribute
- **THEN** the output is wrapped in plain `` ``` `` fences

### Requirement: Blockquote Conversion

The ADF converter SHALL convert `blockquote` nodes to Markdown blockquotes using `> ` prefix on each line.

#### Scenario: Simple blockquote

- **WHEN** an ADF document contains a blockquote with a paragraph
- **THEN** each line of the paragraph is prefixed with `> `

#### Scenario: Multi-paragraph blockquote

- **WHEN** a blockquote contains multiple paragraphs
- **THEN** all paragraphs are prefixed with `> ` and separated by `>` on a blank line

### Requirement: Table Conversion

The ADF converter SHALL convert `table` nodes to Markdown tables. The first row MUST be treated as the header row with a separator line of dashes.

#### Scenario: Simple table

- **WHEN** an ADF document contains a table with a header row and two data rows
- **THEN** the output is a Markdown table with pipe-delimited columns, a header row, a `|---|---|` separator, and data rows

#### Scenario: Table with formatted cells

- **WHEN** table cells contain inline marks (bold, code)
- **THEN** the cell content includes the appropriate Markdown formatting

### Requirement: Mention Conversion

The ADF converter SHALL convert `mention` nodes to `@display_name` format in the output.

#### Scenario: User mention

- **WHEN** an ADF document contains a mention node with displayName "Cameron"
- **THEN** the output contains `@Cameron`

### Requirement: Inline Card Conversion

The ADF converter SHALL convert `inlineCard` nodes (Jira issue links) to Markdown links in the format `[title](url)`.

#### Scenario: Inline Jira link

- **WHEN** an ADF document contains an inlineCard with a URL to a Jira issue
- **THEN** the output contains a Markdown link `[url](url)`

### Requirement: Emoji Conversion

The ADF converter SHALL convert `emoji` nodes to their Unicode emoji equivalent using the `shortName` attribute. Unknown emoji shortcodes MUST be rendered as the shortcode text (e.g., `:unknown_emoji:`).

#### Scenario: Known emoji

- **WHEN** an ADF document contains an emoji node with shortName `:thumbsup:`
- **THEN** the output contains the Unicode thumbs-up character

#### Scenario: Unknown emoji

- **WHEN** an ADF document contains an emoji node with an unrecognized shortName
- **THEN** the output contains the shortName text as-is (e.g., `:custom_emoji:`)

### Requirement: Panel Conversion

The ADF converter SHALL convert `panel` nodes to Markdown blockquotes prefixed with the panel type in bold. Supported panel types are `info`, `note`, `warning`, `error`, and `success`.

#### Scenario: Info panel

- **WHEN** an ADF document contains a panel node with panelType "info"
- **THEN** the output is a blockquote prefixed with `> **Info:** ` followed by the panel content

#### Scenario: Warning panel

- **WHEN** an ADF document contains a panel node with panelType "warning"
- **THEN** the output is a blockquote prefixed with `> **Warning:** ` followed by the panel content

### Requirement: Media Conversion

The ADF converter SHALL convert `mediaSingle` and `media` nodes to Markdown image syntax when a URL is available. When the media references an attachment by ID without a direct URL, the converter MUST render a placeholder indicating an attachment.

#### Scenario: Media with URL

- **WHEN** an ADF media node has a direct URL
- **THEN** the output contains `![](url)`

#### Scenario: Media as attachment reference

- **WHEN** an ADF media node references an attachment by ID without a URL
- **THEN** the output contains `[Attachment: <filename>]` as a placeholder

### Requirement: Status Conversion

The ADF converter SHALL convert `status` nodes to inline text in the format `[STATUS_TEXT]`.

#### Scenario: Status badge

- **WHEN** an ADF document contains a status node with text "IN PROGRESS"
- **THEN** the output contains `[IN PROGRESS]`

### Requirement: Unknown Node Handling

The ADF converter SHALL render any unrecognized node type by extracting its text content. The converter MUST NOT fail or panic on unknown nodes.

#### Scenario: Unrecognized node type

- **WHEN** an ADF document contains a node type not handled by the converter
- **THEN** the converter extracts any text content from the node and renders it as plain text
- **THEN** the converter does not return an error or panic
