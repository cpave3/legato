package planvalidate

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type Kind string

const (
	Mermaid   Kind = "mermaid"
	Wireframe Kind = "wireframe"
)

type Block struct {
	Kind   Kind   `json:"kind"`
	Index  int    `json:"index"`
	Line   int    `json:"line"`
	Source string `json:"source"`
}

type Diagnostic struct {
	Kind    Kind   `json:"kind,omitempty"`
	Index   int    `json:"index,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
}

func Extract(source []byte) ([]Block, []Diagnostic) {
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	indexes := map[Kind]int{}
	var blocks []Block
	var diagnostics []Diagnostic

	_ = gast.Walk(document, func(node gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		fence, ok := node.(*gast.FencedCodeBlock)
		if !ok {
			return gast.WalkContinue, nil
		}
		kind := visualKind(string(fence.Language(source)))
		if kind == "" {
			return gast.WalkContinue, nil
		}
		indexes[kind]++
		openingLine := lineAtOffset(source, fence.Info.Segment.Start)
		if !hasClosingFence(source, openingLine) {
			diagnostics = append(diagnostics, Diagnostic{Kind: kind, Index: indexes[kind], Line: openingLine, Message: "unclosed visual fence"})
			return gast.WalkContinue, nil
		}
		blocks = append(blocks, Block{
			Kind: kind, Index: indexes[kind], Line: openingLine + 1,
			Source: string(fence.Lines().Value(source)),
		})
		return gast.WalkContinue, nil
	})
	return blocks, diagnostics
}

func visualKind(language string) Kind {
	switch strings.ToLower(language) {
	case string(Mermaid):
		return Mermaid
	case string(Wireframe):
		return Wireframe
	default:
		return ""
	}
}

func lineAtOffset(source []byte, offset int) int {
	return bytes.Count(source[:offset], []byte("\n")) + 1
}

func hasClosingFence(source []byte, openingLine int) bool {
	lines := bytes.Split(source, []byte("\n"))
	if openingLine < 1 || openingLine > len(lines) {
		return false
	}
	opener := bytes.TrimLeft(lines[openingLine-1], " \t")
	if len(opener) < 3 || (opener[0] != '`' && opener[0] != '~') {
		return false
	}
	marker := opener[0]
	length := 0
	for length < len(opener) && opener[length] == marker {
		length++
	}
	for _, line := range lines[openingLine:] {
		candidate := bytes.TrimSpace(line)
		count := 0
		for count < len(candidate) && candidate[count] == marker {
			count++
		}
		if count >= length && len(bytes.TrimSpace(candidate[count:])) == 0 {
			return true
		}
	}
	return false
}

func FormatDiagnostic(path string, diagnostic Diagnostic) string {
	location := path
	if diagnostic.Line > 0 {
		location += fmt.Sprintf(":%d", diagnostic.Line)
		if diagnostic.Column > 0 {
			location += fmt.Sprintf(":%d", diagnostic.Column)
		}
	}
	label := string(diagnostic.Kind)
	if diagnostic.Index > 0 {
		label += fmt.Sprintf(" block %d", diagnostic.Index)
	}
	if label != "" {
		return fmt.Sprintf("%s: %s: %s", location, label, diagnostic.Message)
	}
	return fmt.Sprintf("%s: %s", location, diagnostic.Message)
}
