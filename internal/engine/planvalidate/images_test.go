package planvalidate_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/planvalidate"
)

func TestImagesListsEachImageWithItsDestinationAndLine(t *testing.T) {
	markdown := strings.Join([]string{
		"# Plan", // 1
		"",
		"Before: ![The board](assets/board.png) and after.", // 3
		"",
		"![](<assets/my file.png>)", // 5
		"",
		"```",
		"![not an image](code.png)", // 8: inside a code block
		"```",
		"",
		"- a list item",
		"  with ![by reference][shot] in it", // 12
		"",
		"![remote](https://example.com/x.png \"A title\")", // 14
		"",
		"[shot]: assets/shot.png",
	}, "\n")

	images := planvalidate.Images([]byte(markdown))

	want := []planvalidate.Image{
		{Destination: "assets/board.png", Alt: "The board", Line: 3},
		{Destination: "assets/my file.png", Alt: "", Line: 5},
		{Destination: "assets/shot.png", Alt: "by reference", Line: 12},
		{Destination: "https://example.com/x.png", Alt: "remote", Line: 14},
	}
	if len(images) != len(want) {
		t.Fatalf("images = %+v", images)
	}
	for i := range want {
		if images[i] != want[i] {
			t.Errorf("images[%d] = %+v, want %+v", i, images[i], want[i])
		}
	}
}

func TestImagesOfADocumentWithNoneIsEmpty(t *testing.T) {
	if images := planvalidate.Images([]byte("# Plan\n\nText and a [link](a.png).\n")); len(images) != 0 {
		t.Fatalf("images = %+v", images)
	}
}
