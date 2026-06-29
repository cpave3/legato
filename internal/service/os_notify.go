package service

import (
	"fmt"
	"os/exec"
	"runtime"
)

// osNotifier sends desktop notifications via the OS-native mechanism.
// Linux: notify-send (KDE/GNOME/any Freedesktop-compliant DE)
// macOS: osascript display notification
// Other platforms: no-op
type osNotifier struct{}

// NewOSNotifier creates an OS notifier. Returns a no-op notifier on unsupported platforms.
func NewOSNotifier() Notifier {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		return &osNotifier{}
	}
	return &noopNotifier{}
}

func (o *osNotifier) Configured() bool { return true }

func (o *osNotifier) CanNotify(_ string) bool { return true }

func (o *osNotifier) Notify(title, message string) error {
	switch runtime.GOOS {
	case "linux":
		cmd := exec.Command("notify-send", "--app-name=Legato", title, message)
		return cmd.Run()
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title %q`, message, title)
		cmd := exec.Command("osascript", "-e", script)
		return cmd.Run()
	}
	return nil
}
