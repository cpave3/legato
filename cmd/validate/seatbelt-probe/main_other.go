//go:build !darwin

// Command seatbelt-probe answers, on macOS, the questions that Legato's
// Seatbelt sandbox depends on. See main_darwin.go.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "seatbelt-probe runs on macOS only")
	os.Exit(2)
}
