package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/cpave3/legato/internal/service"
)

// headlessWelcome is what a first `legato serve` prints instead of asking
// anything: the checks, and a phrase to pair the Legato app with, so setup
// can be finished from a phone or a browser.
func headlessWelcome(out io.Writer, report service.DoctorReport, open func() (service.PairingStatus, error), webURL string) {
	fmt.Fprintln(out, "\nFirst start. Legato asks nothing here: finish setting it up in the web UI.")
	fmt.Fprintln(out)
	fmt.Fprint(out, formatDoctorReport(report))
	fmt.Fprintln(out)
	if status, err := open(); err == nil && len(status.Phrase) > 0 {
		fmt.Fprintf(out, "Pair the Legato app: add this server there and type\n\n    %s\n\n", strings.Join(status.Phrase, " "))
		fmt.Fprintf(out, "Valid until %s. For another: legato pair.\n", status.ExpiresAt.Local().Format("15:04"))
	} else {
		fmt.Fprintln(out, "Pair the Legato app with: legato pair.")
	}
	fmt.Fprintf(out, "Then finish setup at %s/settings\n\n", strings.TrimRight(webURL, "/"))
}
