package jira

import (
	"encoding/json"
	"fmt"
	"strings"
)

// adfNode represents a node in an Atlassian Document Format tree.
type adfNode struct {
	Type    string            `json:"type"`
	Text    string            `json:"text,omitempty"`
	Marks   []adfMark         `json:"marks,omitempty"`
	Attrs   map[string]any    `json:"attrs,omitempty"`
	Content []adfNode         `json:"content,omitempty"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// ADFToMarkdown converts an ADF JSON document to Markdown.
// Returns empty string for nil/empty/invalid input.
func ADFToMarkdown(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	var doc adfNode
	if err := json.Unmarshal(data, &doc); err != nil {
		return ""
	}

	var sb strings.Builder
	renderBlocks(&sb, doc.Content, 0)
	return strings.TrimRight(sb.String(), "\n")
}

func renderBlocks(sb *strings.Builder, nodes []adfNode, depth int) {
	for i, node := range nodes {
		switch node.Type {
		case "paragraph":
			renderInline(sb, node.Content)
			sb.WriteString("\n")
			if depth == 0 && i < len(nodes)-1 {
				sb.WriteString("\n")
			}

		case "heading":
			level := intAttr(node.Attrs, "level", 1)
			sb.WriteString(strings.Repeat("#", level))
			sb.WriteString(" ")
			renderInline(sb, node.Content)
			sb.WriteString("\n")
			if i < len(nodes)-1 {
				sb.WriteString("\n")
			}

		case "codeBlock":
			lang, _ := node.Attrs["language"].(string)
			sb.WriteString("```")
			sb.WriteString(lang)
			sb.WriteString("\n")
			renderInline(sb, node.Content)
			sb.WriteString("\n```\n")
			if i < len(nodes)-1 {
				sb.WriteString("\n")
			}

		case "blockquote":
			var inner strings.Builder
			renderBlocks(&inner, node.Content, depth+1)
			for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
				sb.WriteString("> ")
				sb.WriteString(line)
				sb.WriteString("\n")
			}
			if i < len(nodes)-1 {
				sb.WriteString("\n")
			}

		case "bulletList":
			renderList(sb, node.Content, depth, false)
			if depth == 0 && i < len(nodes)-1 {
				sb.WriteString("\n")
			}

		case "orderedList":
			renderList(sb, node.Content, depth, true)
			if depth == 0 && i < len(nodes)-1 {
				sb.WriteString("\n")
			}

		case "table":
			renderTable(sb, node.Content)
			if i < len(nodes)-1 {
				sb.WriteString("\n")
			}

		case "panel":
			renderPanel(sb, node)
			if i < len(nodes)-1 {
				sb.WriteString("\n")
			}

		case "mediaSingle":
			renderBlocks(sb, node.Content, depth)

		case "media":
			renderMedia(sb, node)

		case "rule":
			sb.WriteString("---\n")
			if i < len(nodes)-1 {
				sb.WriteString("\n")
			}

		default:
			// Unknown block: extract text content
			renderInline(sb, node.Content)
			if text := extractText(node); text != "" && len(node.Content) == 0 {
				sb.WriteString(text)
			}
			if sb.Len() > 0 {
				last := sb.String()
				if len(last) > 0 && last[len(last)-1] != '\n' {
					sb.WriteString("\n")
				}
			}
		}
	}
}

func renderInline(sb *strings.Builder, nodes []adfNode) {
	for _, node := range nodes {
		switch node.Type {
		case "text":
			text := applyMarks(node.Text, node.Marks)
			sb.WriteString(text)

		case "mention":
			name, _ := node.Attrs["text"].(string)
			if name == "" {
				name, _ = node.Attrs["displayName"].(string)
			}
			if name != "" {
				if !strings.HasPrefix(name, "@") {
					sb.WriteString("@")
				}
				sb.WriteString(name)
			}

		case "inlineCard":
			url, _ := node.Attrs["url"].(string)
			if url != "" {
				sb.WriteString("[")
				sb.WriteString(url)
				sb.WriteString("](")
				sb.WriteString(url)
				sb.WriteString(")")
			}

		case "emoji":
			shortName, _ := node.Attrs["shortName"].(string)
			if unicode, ok := emojiMap[shortName]; ok {
				sb.WriteString(unicode)
			} else if shortName != "" {
				sb.WriteString(shortName)
			}

		case "status":
			text, _ := node.Attrs["text"].(string)
			sb.WriteString("[")
			sb.WriteString(text)
			sb.WriteString("]")

		case "hardBreak":
			sb.WriteString("\n")

		default:
			// Unknown inline: extract text
			if node.Text != "" {
				sb.WriteString(node.Text)
			}
			renderInline(sb, node.Content)
		}
	}
}

func applyMarks(text string, marks []adfMark) string {
	for _, mark := range marks {
		switch mark.Type {
		case "strong":
			text = "**" + text + "**"
		case "em":
			text = "*" + text + "*"
		case "code":
			text = "`" + text + "`"
		case "strike":
			text = "~~" + text + "~~"
		case "underline":
			// No markdown equivalent, keep as-is
		case "link":
			href, _ := mark.Attrs["href"].(string)
			if href != "" {
				text = "[" + text + "](" + href + ")"
			}
		}
	}
	return text
}

func renderList(sb *strings.Builder, items []adfNode, depth int, ordered bool) {
	indent := strings.Repeat("  ", depth)
	if ordered {
		indent = strings.Repeat("   ", depth)
	}
	for i, item := range items {
		if item.Type != "listItem" {
			continue
		}
		for j, child := range item.Content {
			switch child.Type {
			case "paragraph":
				if j == 0 {
					sb.WriteString(indent)
					if ordered {
						sb.WriteString(fmt.Sprintf("%d. ", i+1))
					} else {
						sb.WriteString("- ")
					}
					renderInline(sb, child.Content)
					sb.WriteString("\n")
				} else {
					sb.WriteString(indent)
					sb.WriteString("  ")
					renderInline(sb, child.Content)
					sb.WriteString("\n")
				}
			case "bulletList":
				renderList(sb, child.Content, depth+1, false)
			case "orderedList":
				renderList(sb, child.Content, depth+1, true)
			}
		}
	}
}

func renderTable(sb *strings.Builder, rows []adfNode) {
	if len(rows) == 0 {
		return
	}

	var table [][]string
	for _, row := range rows {
		if row.Type != "tableRow" {
			continue
		}
		var cells []string
		for _, cell := range row.Content {
			var cellSB strings.Builder
			renderInline(&cellSB, flattenCellContent(cell.Content))
			cells = append(cells, strings.TrimSpace(cellSB.String()))
		}
		table = append(table, cells)
	}

	if len(table) == 0 {
		return
	}

	// Header row
	sb.WriteString("| ")
	sb.WriteString(strings.Join(table[0], " | "))
	sb.WriteString(" |\n")

	// Separator
	sb.WriteString("|")
	for range table[0] {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")

	// Data rows
	for _, row := range table[1:] {
		sb.WriteString("| ")
		sb.WriteString(strings.Join(row, " | "))
		sb.WriteString(" |\n")
	}
}

func flattenCellContent(nodes []adfNode) []adfNode {
	var result []adfNode
	for _, node := range nodes {
		if node.Type == "paragraph" {
			result = append(result, node.Content...)
		} else {
			result = append(result, node)
		}
	}
	return result
}

func renderPanel(sb *strings.Builder, node adfNode) {
	panelType, _ := node.Attrs["panelType"].(string)
	title := strings.ToUpper(panelType[:1]) + panelType[1:]

	var inner strings.Builder
	renderBlocks(&inner, node.Content, 1)
	content := strings.TrimRight(inner.String(), "\n")

	sb.WriteString("> **")
	sb.WriteString(title)
	sb.WriteString(":** ")
	sb.WriteString(content)
	sb.WriteString("\n")
}

func renderMedia(sb *strings.Builder, node adfNode) {
	mediaType, _ := node.Attrs["type"].(string)
	url, _ := node.Attrs["url"].(string)

	if url != "" {
		sb.WriteString("![](")
		sb.WriteString(url)
		sb.WriteString(")\n")
		return
	}

	if mediaType == "file" {
		filename, _ := node.Attrs["alt"].(string)
		if filename == "" {
			filename, _ = node.Attrs["__fileName"].(string)
		}
		if filename == "" {
			filename = "file"
		}
		sb.WriteString("[Attachment: ")
		sb.WriteString(filename)
		sb.WriteString("]\n")
		return
	}

	// External media with URL in collection/id
	id, _ := node.Attrs["id"].(string)
	if id != "" {
		sb.WriteString("[Attachment: ")
		sb.WriteString(id)
		sb.WriteString("]\n")
	}
}

func extractText(node adfNode) string {
	if node.Text != "" {
		return node.Text
	}
	var parts []string
	for _, child := range node.Content {
		if t := extractText(child); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "")
}

func intAttr(attrs map[string]any, key string, defaultVal int) int {
	v, ok := attrs[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return defaultVal
	}
}

// Common emoji shortName to Unicode mappings.
var emojiMap = map[string]string{
	":thumbsup:":   "\U0001F44D",
	":thumbsdown:": "\U0001F44E",
	":smile:":      "\U0001F604",
	":heart:":      "\u2764\uFE0F",
	":star:":       "\u2B50",
	":check:":      "\u2705",
	":cross:":      "\u274C",
	":warning:":    "\u26A0\uFE0F",
	":fire:":       "\U0001F525",
	":rocket:":     "\U0001F680",
	":tada:":       "\U0001F389",
	":bug:":        "\U0001F41B",
	":bulb:":       "\U0001F4A1",
	":memo:":       "\U0001F4DD",
	":wrench:":     "\U0001F527",
	":lock:":       "\U0001F512",
	":eyes:":       "\U0001F440",
	":question:":   "\u2753",
	":exclamation:": "\u2757",
	":+1:":         "\U0001F44D",
	":-1:":         "\U0001F44E",
}
