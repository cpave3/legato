package jira

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// MarkdownToADF converts Markdown (CommonMark with GitHub's tables,
// strikethrough and bare links) to an Atlassian Document Format document,
// for an issue's description or a comment. Code blocks keep their text
// exactly; an image becomes a link to it, raw HTML plain text. No text node
// is empty, which Jira refuses.
func MarkdownToADF(md string) json.RawMessage {
	source := []byte(md)
	doc := markdown.Parser().Parse(text.NewReader(source))
	c := adfConverter{source: source}
	content := c.blocks(doc)
	if content == nil {
		content = []adfNode{}
	}
	out, _ := json.Marshal(struct {
		Type    string    `json:"type"`
		Version int       `json:"version"`
		Content []adfNode `json:"content"`
	}{"doc", 1, content})
	return out
}

type adfConverter struct {
	source []byte
}

func (c adfConverter) blocks(parent gast.Node) []adfNode {
	var out []adfNode
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		if node, ok := c.block(n); ok {
			out = append(out, node)
		}
	}
	return out
}

func (c adfConverter) block(n gast.Node) (adfNode, bool) {
	switch n := n.(type) {
	case *gast.Paragraph, *gast.TextBlock:
		return adfNode{Type: "paragraph", Content: c.inlines(n, nil)}, true
	case *gast.Heading:
		return adfNode{Type: "heading", Attrs: map[string]any{"level": n.Level}, Content: c.inlines(n, nil)}, true
	case *gast.FencedCodeBlock:
		node := codeBlock(c.lines(n))
		if lang := string(n.Language(c.source)); lang != "" {
			node.Attrs = map[string]any{"language": lang}
		}
		return node, true
	case *gast.CodeBlock:
		return codeBlock(c.lines(n)), true
	case *gast.HTMLBlock:
		body := c.lines(n)
		if n.HasClosure() {
			body += string(n.ClosureLine.Value(c.source))
		}
		return adfNode{Type: "paragraph", Content: textNodes(strings.TrimRight(body, "\n"), nil)}, true
	case *gast.List:
		node := adfNode{Type: "bulletList"}
		if n.IsOrdered() {
			node.Type = "orderedList"
			if n.Start != 1 {
				node.Attrs = map[string]any{"order": n.Start}
			}
		}
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			node.Content = append(node.Content, adfNode{Type: "listItem", Content: c.nonEmptyBlocks(item)})
		}
		return node, true
	case *gast.Blockquote:
		return adfNode{Type: "blockquote", Content: c.nonEmptyBlocks(n)}, true
	case *gast.ThematicBreak:
		return adfNode{Type: "rule"}, true
	case *east.Table:
		node := adfNode{Type: "table"}
		for row := n.FirstChild(); row != nil; row = row.NextSibling() {
			cellType := "tableCell"
			if _, header := row.(*east.TableHeader); header {
				cellType = "tableHeader"
			}
			r := adfNode{Type: "tableRow"}
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				r.Content = append(r.Content, adfNode{Type: cellType, Content: []adfNode{{Type: "paragraph", Content: c.inlines(cell, nil)}}})
			}
			node.Content = append(node.Content, r)
		}
		return node, true
	}
	return adfNode{}, false
}

// nonEmptyBlocks is a container's blocks, or an empty paragraph: a list
// item or a quote must hold a block.
func (c adfConverter) nonEmptyBlocks(parent gast.Node) []adfNode {
	if blocks := c.blocks(parent); len(blocks) > 0 {
		return blocks
	}
	return []adfNode{{Type: "paragraph"}}
}

func (c adfConverter) lines(n gast.Node) string {
	var sb strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(c.source))
	}
	return sb.String()
}

func codeBlock(body string) adfNode {
	return adfNode{Type: "codeBlock", Content: textNodes(strings.TrimSuffix(body, "\n"), nil)}
}

// inlines converts a block's inline children under marks, merging
// neighbouring text with the same marks.
func (c adfConverter) inlines(parent gast.Node, marks []adfMark) []adfNode {
	var out []adfNode
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		out = appendInline(out, c.inline(n, marks)...)
	}
	return out
}

func (c adfConverter) inline(n gast.Node, marks []adfMark) []adfNode {
	switch n := n.(type) {
	case *gast.Text:
		value := string(n.Segment.Value(c.source))
		nodes := textNodes(value, marks)
		switch {
		case n.HardLineBreak():
			nodes = append(textNodes(strings.TrimRight(value, " "), marks), adfNode{Type: "hardBreak"})
		case n.SoftLineBreak():
			nodes = append(nodes, textNodes(" ", marks)...)
		}
		return nodes
	case *gast.String:
		return textNodes(string(n.Value), marks)
	case *gast.Emphasis:
		mark := "em"
		if n.Level == 2 {
			mark = "strong"
		}
		return c.inlines(n, withMark(marks, adfMark{Type: mark}))
	case *east.Strikethrough:
		return c.inlines(n, withMark(marks, adfMark{Type: "strike"}))
	case *gast.CodeSpan:
		var sb strings.Builder
		for t := n.FirstChild(); t != nil; t = t.NextSibling() {
			if s, ok := t.(*gast.Text); ok {
				sb.Write(s.Segment.Value(c.source))
			} else if s, ok := t.(*gast.String); ok {
				sb.Write(s.Value)
			}
		}
		// Jira allows the code mark beside a link mark only.
		var kept []adfMark
		for _, m := range marks {
			if m.Type == "link" {
				kept = append(kept, m)
			}
		}
		return textNodes(sb.String(), append(kept, adfMark{Type: "code"}))
	case *gast.Link:
		return c.linked(n, string(n.Destination), marks)
	case *gast.Image:
		return c.linked(n, string(n.Destination), marks)
	case *gast.AutoLink:
		href := string(n.URL(c.source))
		if n.AutoLinkType == gast.AutoLinkEmail && !strings.HasPrefix(href, "mailto:") {
			href = "mailto:" + href
		}
		return textNodes(string(n.Label(c.source)), withMark(marks, linkMark(href)))
	case *gast.RawHTML:
		var sb strings.Builder
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			sb.Write(seg.Value(c.source))
		}
		return textNodes(sb.String(), marks)
	}
	return c.inlines(n, marks)
}

// linked is a link's (or an image's) text with a link mark: the address
// itself when it has none.
func (c adfConverter) linked(n gast.Node, href string, marks []adfMark) []adfNode {
	marks = withMark(marks, linkMark(href))
	if nodes := c.inlines(n, marks); len(nodes) > 0 {
		return nodes
	}
	return textNodes(href, marks)
}

func linkMark(href string) adfMark {
	return adfMark{Type: "link", Attrs: map[string]any{"href": href}}
}

func withMark(marks []adfMark, m adfMark) []adfMark {
	out := make([]adfMark, 0, len(marks)+1)
	return append(append(out, marks...), m)
}

// textNodes is text as a text node, or none for no text.
func textNodes(s string, marks []adfMark) []adfNode {
	if s == "" {
		return nil
	}
	return []adfNode{{Type: "text", Text: s, Marks: marks}}
}

// appendInline appends nodes, joining text to the text before it when
// their marks match.
func appendInline(out []adfNode, nodes ...adfNode) []adfNode {
	for _, n := range nodes {
		if last := len(out) - 1; last >= 0 && n.Type == "text" && out[last].Type == "text" && sameMarks(out[last].Marks, n.Marks) {
			out[last].Text += n.Text
			continue
		}
		out = append(out, n)
	}
	return out
}

func sameMarks(a, b []adfMark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || !reflect.DeepEqual(a[i].Attrs, b[i].Attrs) {
			return false
		}
	}
	return true
}
