//go:build !darwin

package sandbox

import "os"

// terminalPath is only needed by the Seatbelt backend, on macOS.
func terminalPath(*os.File) string { return "" }
