package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrPairOnStart is a pairing that waits for the web server Legato starts
// once setup is done: the first run's.
var ErrPairOnStart = errors.New("pairing waits for the web server to start")

// WebSetup turns on the web UI and pairs a phone with it. web.enabled is a
// file-only setting: it is written to config.yaml, never the database.
type WebSetup interface {
	// State says whether the web UI is on, and whether config.yaml sets
	// web.enabled (then setup leaves it as it is).
	State() (on, inFile bool)
	// Enable writes web.enabled: true into config.yaml, keeping the rest
	// of the file, and says where.
	Enable(ctx context.Context) (string, error)
	// Address is where the web UI is reached.
	Address() string
	// Pair runs the pairing-phrase flow with the web server, until a phone
	// pairs or the operator gives up; ErrPairOnStart when it can only run
	// once Legato has started its web server.
	Pair(ctx context.Context) error
}

// stepWeb turns on the web UI and offers to pair a phone.
func (f *Flow) stepWeb(ctx context.Context) error {
	on, inFile := f.Web.State()
	switch {
	case inFile && !on:
		fmt.Fprintln(f.Out, "  The web UI is off (pinned in config.yaml).")
		return nil
	case !on:
		answer := strings.ToLower(f.ask("  Turn on the web UI, to use Legato from a browser and your phone? [Y/n]: "))
		if answer != "" && answer != "y" && answer != "yes" {
			fmt.Fprintln(f.Out, "  Later: set web.enabled: true in config.yaml.")
			return nil
		}
		said, err := f.Web.Enable(ctx)
		if err != nil {
			fmt.Fprintf(f.Out, "  ✗ %v. Set web.enabled: true in config.yaml yourself.\n", err)
			return nil
		}
		fmt.Fprintf(f.Out, "  ✓ %s\n", said)
	case inFile:
		fmt.Fprintln(f.Out, "  The web UI is on (pinned in config.yaml).")
	}
	fmt.Fprintf(f.Out, "  The web UI: %s\n", f.Web.Address())
	answer := strings.ToLower(f.ask("  Pair a phone now? [y/N]: "))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(f.Out, "  Later: legato pair, or Settings › Security › Approval devices.")
		return nil
	}
	switch err := f.Web.Pair(ctx); {
	case errors.Is(err, ErrPairOnStart):
		f.PairOnStart = true
		fmt.Fprintln(f.Out, "  Legato shows the pairing phrase as soon as its web server starts.")
	case err != nil:
		fmt.Fprintf(f.Out, "  ✗ %v. Later: legato pair.\n", err)
	}
	return nil
}
