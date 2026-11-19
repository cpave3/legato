package setup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeTasks struct {
	tasks map[string]string // title -> working dir
	made  int
}

func (f *fakeTasks) FindTask(_ context.Context, title string) (string, bool, error) {
	if _, ok := f.tasks[title]; ok {
		return "starter", true, nil
	}
	return "", false, nil
}

func (f *fakeTasks) CreateTask(_ context.Context, title, _ string, workingDir string) (string, error) {
	if f.tasks == nil {
		f.tasks = map[string]string{}
	}
	f.tasks[title] = workingDir
	f.made++
	return "starter", nil
}

func TestSetupEndsWithOneStarterTaskInTheChosenRepository(t *testing.T) {
	home := repoTree(t)
	tasks := &fakeTasks{}
	setupWith := func(input string) *setupRun {
		return runSetup(t, input, nil, []service.DoctorReport{machine(nil)}, func(r *setupRun) {
			r.repositories = &fakePresets{}
			r.repositoryRoots = []string{filepath.Join(home, "Projects")}
			r.tasks = tasks
		})
	}
	// Tool skipped; the recent repository (legato) saved; tracker skipped;
	// the starter task taken.
	r := setupWith("s\n\ns\n\n")
	if r.err != nil {
		t.Fatalf("setup = %v\n%s", r.err, r.out.String())
	}
	if tasks.made != 1 || r.flow.StarterTask != "starter" {
		t.Fatalf("made %d, starter %q\n%s", tasks.made, r.flow.StarterTask, r.out.String())
	}
	if dir := tasks.tasks[StarterTaskTitle]; dir != filepath.Join(home, "Projects", "legato") {
		t.Errorf("starter task works in %q", dir)
	}

	again := setupWith("s\ns\ns\n")
	if again.err != nil {
		t.Fatal(again.err)
	}
	if tasks.made != 1 {
		t.Fatalf("a rerun made another starter task")
	}
	if again.flow.StarterTask != "starter" {
		t.Errorf("a rerun does not select the starter task")
	}
}

func TestTheStarterTaskCanBeSkipped(t *testing.T) {
	tasks := &fakeTasks{}
	r := runSetup(t, "s\ns\ns\n", nil, []service.DoctorReport{machine(nil)}, func(r *setupRun) { r.tasks = tasks })
	if r.err != nil || tasks.made != 0 || r.flow.StarterTask != "" {
		t.Fatalf("err %v, made %d, starter %q", r.err, tasks.made, r.flow.StarterTask)
	}
}
