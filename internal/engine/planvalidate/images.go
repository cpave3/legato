package planvalidate

import (
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Image is one image a Markdown document shows.
type Image struct {
	// Destination is where the Markdown says the image is: a path or an
	// address, as the document names it.
	Destination string `json:"destination"`
	Alt         string `json:"alt"`
	// Line is the one-based source line the image is on.
	Line int `json:"line"`
}

// Images lists the images of a Markdown document in the order it shows
// them. What is written inside a code block is not an image.
func Images(source []byte) []Image {
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	var images []Image
	_ = gast.Walk(document, func(node gast.Node, entering bool) (gast.WalkStatus, error) {
		image, ok := node.(*gast.Image)
		if !ok || !entering {
			return gast.WalkContinue, nil
		}
		images = append(images, Image{
			Destination: string(image.Destination),
			Alt:         string(image.Text(source)),
			Line:        imageLine(source, image),
		})
		return gast.WalkSkipChildren, nil
	})
	return images
}

// imageLine finds the line an image is on. An inline node records no
// position of its own: its alt text does, and an image with none is placed
// by the text before it or, failing that, by the block it is in.
func imageLine(source []byte, image *gast.Image) int {
	if offset, ok := firstTextOffset(image); ok {
		return lineAtOffset(source, offset)
	}
	for previous := image.PreviousSibling(); previous != nil; previous = previous.PreviousSibling() {
		if segment, ok := previous.(*gast.Text); ok {
			return lineAtOffset(source, segment.Segment.Stop)
		}
	}
	for parent := image.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Type() == gast.TypeBlock && parent.Lines() != nil && parent.Lines().Len() > 0 {
			return lineAtOffset(source, parent.Lines().At(0).Start)
		}
	}
	return 0
}

func firstTextOffset(node gast.Node) (int, bool) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if segment, ok := child.(*gast.Text); ok {
			return segment.Segment.Start, true
		}
		if offset, ok := firstTextOffset(child); ok {
			return offset, true
		}
	}
	return 0, false
}
