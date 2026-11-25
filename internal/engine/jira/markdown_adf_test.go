package jira

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// adfDoc wraps block JSON as the document MarkdownToADF gives.
func adfDoc(blocks string) string {
	return `{"type":"doc","version":1,"content":[` + blocks + `]}`
}

func sameJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got is not JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %v\n%s", err, want)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestMarkdownToADF(t *testing.T) {
	cases := []struct {
		name, md, want string
	}{
		{"empty", "", ""},
		{"paragraphs", "One line\nwraps here.\n\nTwo.",
			`{"type":"paragraph","content":[{"type":"text","text":"One line wraps here."}]},` +
				`{"type":"paragraph","content":[{"type":"text","text":"Two."}]}`},
		{"heading", "## Steps to reproduce",
			`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Steps to reproduce"}]}`},
		{"marks", "a **bold** *em* `code` ~~gone~~ b",
			`{"type":"paragraph","content":[` +
				`{"type":"text","text":"a "},` +
				`{"type":"text","text":"bold","marks":[{"type":"strong"}]},` +
				`{"type":"text","text":" "},` +
				`{"type":"text","text":"em","marks":[{"type":"em"}]},` +
				`{"type":"text","text":" "},` +
				`{"type":"text","text":"code","marks":[{"type":"code"}]},` +
				`{"type":"text","text":" "},` +
				`{"type":"text","text":"gone","marks":[{"type":"strike"}]},` +
				`{"type":"text","text":" b"}]}`},
		{"nested marks", "***both***",
			`{"type":"paragraph","content":[{"type":"text","text":"both","marks":[{"type":"em"},{"type":"strong"}]}]}`},
		{"links", "See [the log](https://example.com/log) and <https://example.com/x>.",
			`{"type":"paragraph","content":[` +
				`{"type":"text","text":"See "},` +
				`{"type":"text","text":"the log","marks":[{"type":"link","attrs":{"href":"https://example.com/log"}}]},` +
				`{"type":"text","text":" and "},` +
				`{"type":"text","text":"https://example.com/x","marks":[{"type":"link","attrs":{"href":"https://example.com/x"}}]},` +
				`{"type":"text","text":"."}]}`},
		{"bare URL", "at https://example.com/y now",
			`{"type":"paragraph","content":[` +
				`{"type":"text","text":"at "},` +
				`{"type":"text","text":"https://example.com/y","marks":[{"type":"link","attrs":{"href":"https://example.com/y"}}]},` +
				`{"type":"text","text":" now"}]}`},
		{"image", "![screen](https://example.com/s.png)",
			`{"type":"paragraph","content":[{"type":"text","text":"screen","marks":[{"type":"link","attrs":{"href":"https://example.com/s.png"}}]}]}`},
		{"hard break", "one  \ntwo",
			`{"type":"paragraph","content":[{"type":"text","text":"one"},{"type":"hardBreak"},{"type":"text","text":"two"}]}`},
		{"fenced code keeps its text", "```go\nif a < b {\n\treturn \"<x>\"\n}\n```",
			`{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"if a < b {\n\treturn \"<x>\"\n}"}]}`},
		{"indented code", "    plain()\n",
			`{"type":"codeBlock","content":[{"type":"text","text":"plain()"}]}`},
		{"empty code block", "```\n```",
			`{"type":"codeBlock"}`},
		{"bullet list", "- one\n- two",
			`{"type":"bulletList","content":[` +
				`{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]},` +
				`{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]}`},
		{"ordered list from 3 with a nested list", "3. three\n   - inner\n4. four",
			`{"type":"orderedList","attrs":{"order":3},"content":[` +
				`{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"three"}]},` +
				`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"inner"}]}]}]}]},` +
				`{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"four"}]}]}]}`},
		{"ordered list from 1", "1. one",
			`{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]}]}`},
		{"blockquote and rule", "> quoted\n\n---",
			`{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},{"type":"rule"}`},
		{"raw HTML is text", "<b>hi</b>",
			`{"type":"paragraph","content":[{"type":"text","text":"<b>hi</b>"}]}`},
		{"table", "| A | B |\n|---|---|\n| 1 | **2** |",
			`{"type":"table","content":[` +
				`{"type":"tableRow","content":[` +
				`{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"A"}]}]},` +
				`{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"B"}]}]}]},` +
				`{"type":"tableRow","content":[` +
				`{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]},` +
				`{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"2","marks":[{"type":"strong"}]}]}]}]}]}`},
		{"empty table cell", "| A | B |\n|---|---|\n| 1 |  |",
			`{"type":"table","content":[` +
				`{"type":"tableRow","content":[` +
				`{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"A"}]}]},` +
				`{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"B"}]}]}]},` +
				`{"type":"tableRow","content":[` +
				`{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]},` +
				`{"type":"tableCell","content":[{"type":"paragraph"}]}]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sameJSON(t, MarkdownToADF(tc.md), adfDoc(tc.want))
		})
	}
}

// Jira refuses a text node with no text.
func TestMarkdownToADFHasNoEmptyTextNodes(t *testing.T) {
	md := "# \n\n**​**\n\n- \n\n> \n\n`` ` ``\n\n[](https://example.com)\n\n| |\n|-|\n| |"
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		if n["type"] == "text" && n["text"] == "" {
			t.Fatalf("an empty text node in %s", MarkdownToADF(md))
		}
		children, _ := n["content"].([]any)
		for _, c := range children {
			walk(c.(map[string]any))
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(MarkdownToADF(md), &doc); err != nil {
		t.Fatal(err)
	}
	walk(doc)
}

// What Legato writes to Jira reads back as the same Markdown.
func TestMarkdownToADFReadsBack(t *testing.T) {
	md := "## Seen in prod\n\nThe **login** page fails, see [the log](https://example.com/log).\n\n" +
		"- step one\n- step `two`\n\n```sh\ncurl -s https://example.com | jq '.a < 2'\n```"
	got := ADFToMarkdown(MarkdownToADF(md))
	for _, want := range []string{"## Seen in prod", "**login**", "[the log](https://example.com/log)", "- step one", "`two`",
		"```sh\ncurl -s https://example.com | jq '.a < 2'\n```"} {
		if !strings.Contains(got, want) {
			t.Errorf("read back %q, missing %q", got, want)
		}
	}
}
