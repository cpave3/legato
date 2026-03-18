package jira

import (
	"strings"
	"testing"
)

func TestADFPlainParagraph(t *testing.T) {
	adf := `{
		"version": 1,
		"type": "doc",
		"content": [
			{
				"type": "paragraph",
				"content": [
					{"type": "text", "text": "Hello world"}
				]
			}
		]
	}`
	got := ADFToMarkdown([]byte(adf))
	want := "Hello world"
	if strings.TrimSpace(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestADFBoldMark(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "text", "text": "bold text", "marks": [{"type": "strong"}]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "**bold text**") {
		t.Errorf("got %q, want **bold text**", got)
	}
}

func TestADFItalicMark(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "text", "text": "italic", "marks": [{"type": "em"}]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "*italic*") {
		t.Errorf("got %q, want *italic*", got)
	}
}

func TestADFCodeMark(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "text", "text": "code", "marks": [{"type": "code"}]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "`code`") {
		t.Errorf("got %q, want `code`", got)
	}
}

func TestADFStrikethroughMark(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "text", "text": "deleted", "marks": [{"type": "strike"}]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "~~deleted~~") {
		t.Errorf("got %q, want ~~deleted~~", got)
	}
}

func TestADFUnderlineMark(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "text", "text": "underlined", "marks": [{"type": "underline"}]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	// Markdown doesn't have underline; render as-is or with HTML
	if !strings.Contains(got, "underlined") {
		t.Errorf("got %q, should contain 'underlined'", got)
	}
}

func TestADFLinkMark(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "text", "text": "click here", "marks": [{"type": "link", "attrs": {"href": "https://example.com"}}]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "[click here](https://example.com)") {
		t.Errorf("got %q, want link markdown", got)
	}
}

func TestADFMultipleMarks(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "text", "text": "bold italic", "marks": [{"type": "strong"}, {"type": "em"}]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "***bold italic***") && !strings.Contains(got, "**_bold italic_**") && !strings.Contains(got, "*__bold italic__*") {
		// Accept any valid bold+italic combo
		if !strings.Contains(got, "**") || !strings.Contains(got, "*") {
			t.Errorf("got %q, want bold+italic marks", got)
		}
	}
}

func TestADFNilInput(t *testing.T) {
	got := ADFToMarkdown(nil)
	if got != "" {
		t.Errorf("got %q for nil input, want empty", got)
	}
}

func TestADFEmptyDoc(t *testing.T) {
	adf := `{"version": 1, "type": "doc", "content": []}`
	got := ADFToMarkdown([]byte(adf))
	if strings.TrimSpace(got) != "" {
		t.Errorf("got %q for empty doc, want empty", got)
	}
}

// Task 2.2: Block nodes
func TestADFHeadings(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [
			{"type": "heading", "attrs": {"level": 1}, "content": [{"type": "text", "text": "H1"}]},
			{"type": "heading", "attrs": {"level": 2}, "content": [{"type": "text", "text": "H2"}]},
			{"type": "heading", "attrs": {"level": 3}, "content": [{"type": "text", "text": "H3"}]}
		]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "# H1") {
		t.Errorf("missing # H1 in %q", got)
	}
	if !strings.Contains(got, "## H2") {
		t.Errorf("missing ## H2 in %q", got)
	}
	if !strings.Contains(got, "### H3") {
		t.Errorf("missing ### H3 in %q", got)
	}
}

func TestADFHeadingWithMarks(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "heading", "attrs": {"level": 2},
			"content": [{"type": "text", "text": "Important", "marks": [{"type": "strong"}]}]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "## **Important**") {
		t.Errorf("got %q, want heading with bold", got)
	}
}

func TestADFCodeBlock(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "codeBlock", "attrs": {"language": "go"},
			"content": [{"type": "text", "text": "func main() {}"}]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "```go\nfunc main() {}\n```") {
		t.Errorf("got %q, want fenced code block with language", got)
	}
}

func TestADFCodeBlockNoLanguage(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "codeBlock",
			"content": [{"type": "text", "text": "some code"}]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "```\nsome code\n```") {
		t.Errorf("got %q, want plain fenced code block", got)
	}
}

func TestADFBlockquote(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "blockquote",
			"content": [{
				"type": "paragraph",
				"content": [{"type": "text", "text": "quoted text"}]
			}]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "> quoted text") {
		t.Errorf("got %q, want blockquote", got)
	}
}

// Task 2.3: Lists
func TestADFBulletList(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "bulletList",
			"content": [
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Alpha"}]}]},
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Beta"}]}]},
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Gamma"}]}]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "- Alpha\n") {
		t.Errorf("missing '- Alpha' in %q", got)
	}
	if !strings.Contains(got, "- Beta\n") {
		t.Errorf("missing '- Beta' in %q", got)
	}
	if !strings.Contains(got, "- Gamma") {
		t.Errorf("missing '- Gamma' in %q", got)
	}
}

func TestADFNestedBulletList(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "bulletList",
			"content": [{
				"type": "listItem",
				"content": [
					{"type": "paragraph", "content": [{"type": "text", "text": "Parent"}]},
					{"type": "bulletList", "content": [
						{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Child"}]}]}
					]}
				]
			}]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "- Parent\n") {
		t.Errorf("missing parent in %q", got)
	}
	if !strings.Contains(got, "  - Child") {
		t.Errorf("missing indented child in %q", got)
	}
}

func TestADFOrderedList(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "orderedList",
			"content": [
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "First"}]}]},
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Second"}]}]},
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Third"}]}]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "1. First\n") {
		t.Errorf("missing '1. First' in %q", got)
	}
	if !strings.Contains(got, "2. Second\n") {
		t.Errorf("missing '2. Second' in %q", got)
	}
	if !strings.Contains(got, "3. Third") {
		t.Errorf("missing '3. Third' in %q", got)
	}
}

// Task 2.4: Tables
func TestADFTable(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "table",
			"content": [
				{"type": "tableRow", "content": [
					{"type": "tableHeader", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Name"}]}]},
					{"type": "tableHeader", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Value"}]}]}
				]},
				{"type": "tableRow", "content": [
					{"type": "tableCell", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "foo"}]}]},
					{"type": "tableCell", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "bar", "marks": [{"type": "strong"}]}]}]}
				]}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "| Name | Value |") {
		t.Errorf("missing header row in %q", got)
	}
	if !strings.Contains(got, "| --- | --- |") {
		t.Errorf("missing separator in %q", got)
	}
	if !strings.Contains(got, "| foo | **bar** |") {
		t.Errorf("missing data row with bold in %q", got)
	}
}

// Task 2.5: Inline nodes
func TestADFMention(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "mention", "attrs": {"text": "@Cameron", "id": "123"}}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "@Cameron") {
		t.Errorf("got %q, want @Cameron", got)
	}
}

func TestADFInlineCard(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "inlineCard", "attrs": {"url": "https://jira.example.com/browse/PROJ-1"}}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	want := "[https://jira.example.com/browse/PROJ-1](https://jira.example.com/browse/PROJ-1)"
	if !strings.Contains(got, want) {
		t.Errorf("got %q, want inline card link", got)
	}
}

func TestADFEmojiKnown(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "emoji", "attrs": {"shortName": ":thumbsup:"}}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "\U0001F44D") {
		t.Errorf("got %q, want thumbsup unicode", got)
	}
}

func TestADFEmojiUnknown(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "emoji", "attrs": {"shortName": ":custom_thing:"}}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, ":custom_thing:") {
		t.Errorf("got %q, want :custom_thing:", got)
	}
}

func TestADFStatus(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "status", "attrs": {"text": "IN PROGRESS", "color": "blue"}}
			]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "[IN PROGRESS]") {
		t.Errorf("got %q, want [IN PROGRESS]", got)
	}
}

// Task 2.6: Panels
func TestADFInfoPanel(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "panel", "attrs": {"panelType": "info"},
			"content": [{"type": "paragraph", "content": [{"type": "text", "text": "Note this."}]}]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "> **Info:** Note this.") {
		t.Errorf("got %q, want info panel blockquote", got)
	}
}

func TestADFWarningPanel(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "panel", "attrs": {"panelType": "warning"},
			"content": [{"type": "paragraph", "content": [{"type": "text", "text": "Be careful!"}]}]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "> **Warning:** Be careful!") {
		t.Errorf("got %q, want warning panel", got)
	}
}

// Task 2.7: Media
func TestADFMediaWithURL(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "mediaSingle", "attrs": {"layout": "center"},
			"content": [{
				"type": "media", "attrs": {"type": "external", "url": "https://example.com/img.png"}
			}]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "![](https://example.com/img.png)") {
		t.Errorf("got %q, want image markdown", got)
	}
}

func TestADFMediaAttachment(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "mediaSingle",
			"content": [{
				"type": "media", "attrs": {"type": "file", "id": "abc-123", "__fileName": "screenshot.png"}
			}]
		}]
	}`
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "[Attachment: screenshot.png]") {
		t.Errorf("got %q, want attachment placeholder", got)
	}
}

// Task 2.8: Unknown node
func TestADFUnknownNode(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "totallyFakeNode",
			"content": [{"type": "text", "text": "extracted text"}]
		}]
	}`
	// Should not panic
	got := ADFToMarkdown([]byte(adf))
	if !strings.Contains(got, "extracted text") {
		t.Errorf("got %q, want extracted text from unknown node", got)
	}
}

func TestADFUnknownNodeEmpty(t *testing.T) {
	adf := `{
		"version": 1, "type": "doc",
		"content": [{
			"type": "totallyFakeNode"
		}]
	}`
	// Should not panic
	_ = ADFToMarkdown([]byte(adf))
}
