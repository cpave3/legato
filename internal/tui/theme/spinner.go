package theme

// spinnerFrames is the mark of something at work, turned one step a frame.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerFrames is how many frames one turn of the spinner has.
var SpinnerFrames = len(spinnerFrames)

// Spinner returns the spinner at a frame. Frames count up without end; the
// spinner starts again after its last.
func Spinner(frame int) string {
	if frame < 0 {
		frame = -frame
	}
	return spinnerFrames[frame%len(spinnerFrames)]
}
