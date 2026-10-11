package theme

import "testing"

func TestSpinnerTurnsThroughItsFramesAndStartsAgain(t *testing.T) {
	seen := map[string]bool{}
	for frame := 0; frame < SpinnerFrames; frame++ {
		seen[Spinner(frame)] = true
	}
	if len(seen) != SpinnerFrames {
		t.Fatalf("the spinner repeats a frame within one turn: %v", seen)
	}
	if Spinner(SpinnerFrames) != Spinner(0) || Spinner(2*SpinnerFrames+3) != Spinner(3) {
		t.Fatal("the spinner should start again after its last frame")
	}
}
