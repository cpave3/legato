package setup

import (
	"context"
	"fmt"
	"strings"
)

// StarterTaskTitle is the task setup leaves on the board to start a first
// agent on.
const StarterTaskTitle = "Try Legato: explain this repository"

const starterTaskDescription = `A first task to try Legato with. Start an agent on it (press a on the board), and ask it to explain this repository: what it is for, how it is laid out, and where a newcomer should start reading.`

// StarterTasks finds and makes the starter task.
type StarterTasks interface {
	// FindTask finds an active task by its title.
	FindTask(ctx context.Context, title string) (id string, found bool, err error)
	// CreateTask makes a local task in the board's first column, working
	// in workingDir when it is not "".
	CreateTask(ctx context.Context, title, description, workingDir string) (id string, err error)
}

// stepStarterTask leaves one starter task on the board, in the repository
// setup saved first (else the working directory), for the board to open on.
func (f *Flow) stepStarterTask(ctx context.Context) error {
	if id, found, err := f.Tasks.FindTask(ctx, StarterTaskTitle); err != nil {
		return fmt.Errorf("looking for the starter task: %w", err)
	} else if found {
		f.StarterTask = id
		fmt.Fprintf(f.Out, "  %q is on the board already.\n", StarterTaskTitle)
		return nil
	}
	dir := f.chosenRepository
	if dir == "" {
		dir = f.WorkDir
	}
	answer := strings.ToLower(f.ask(fmt.Sprintf("  Add the task %q, working in %s? [Y/n]: ", StarterTaskTitle, dir)))
	if answer != "" && answer != "y" && answer != "yes" {
		return nil
	}
	id, err := f.Tasks.CreateTask(ctx, StarterTaskTitle, starterTaskDescription, dir)
	if err != nil {
		fmt.Fprintf(f.Out, "  ✗ %v\n", err)
		return nil
	}
	f.StarterTask = id
	fmt.Fprintln(f.Out, "  ✓ The board opens on it: press a to start an agent on it.")
	return nil
}
