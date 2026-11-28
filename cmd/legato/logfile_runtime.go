package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/logfile"
	"github.com/cpave3/legato/internal/engine/version"
)

// startLogFile sends this process's log to the data folder's log file, and
// also to also when it is not nil (`legato serve` keeps its stderr; the TUI
// owns its terminal and passes nil). stop puts the log back on also, or
// discards it, and closes the file.
func startLogFile(cfg *config.Config, also io.Writer) (stop func()) {
	// Several processes share the file: each line names its process and
	// binary, so the order of a restart can be read from it (LEG-689).
	prefix := log.Prefix()
	log.SetPrefix(fmt.Sprintf("[%d %s] ", os.Getpid(), version.String()))
	restore := also
	if restore == nil {
		restore = io.Discard
	}
	w, err := logfile.Open(logfile.Path(config.ResolveDataDir(cfg)), logfile.Options{})
	if err != nil {
		if also != nil {
			fmt.Fprintf(also, "log file: %v\n", err)
		}
		log.SetOutput(restore)
		return func() { log.SetPrefix(prefix) }
	}
	if also != nil {
		log.SetOutput(io.MultiWriter(also, w))
	} else {
		log.SetOutput(w)
	}
	return func() {
		log.SetOutput(restore)
		log.SetPrefix(prefix)
		_ = w.Close()
	}
}

// stderrLog is `legato serve`'s second log destination.
var stderrLog io.Writer = os.Stderr
