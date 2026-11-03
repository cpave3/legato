package clonesetup_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/clonesetup"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsLegatosOwnFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".legato.toml", `
[setup]
run = ["bun install", "task gen"]
copy = [".env.local"]
link = ["local.settings.json"]
`)

	setup, err := clonesetup.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := clonesetup.Setup{File: ".legato.toml", Run: []string{"bun install", "task gen"}, Copy: []string{".env.local"}, Link: []string{"local.settings.json"}}
	if !reflect.DeepEqual(setup, want) {
		t.Errorf("setup = %+v, want %+v", setup, want)
	}
}

func TestLoadReadsAYggdrasilFileWhenThereIsNoLegatoOne(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".yggdrasil.toml", `
[general]
trunk = "main"
path_template = "../{repo}.{branch}"

[provision]
copy = [".env.local", "apps/web/.env.local"]
link = ["local.settings.json"]

[hooks]
pre_create = ["echo never"]
post_create = ['cd apps && bun install', 'command -v st >/dev/null || true']
pre_remove = ["echo never"]
`)

	setup, err := clonesetup.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := clonesetup.Setup{File: ".yggdrasil.toml", Run: []string{"cd apps && bun install", "command -v st >/dev/null || true"},
		Copy: []string{".env.local", "apps/web/.env.local"}, Link: []string{"local.settings.json"}}
	if !reflect.DeepEqual(setup, want) {
		t.Errorf("setup = %+v, want %+v", setup, want)
	}
}

func TestLegatosFileWinsOverYggdrasils(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".legato.toml", "[setup]\nrun = [\"make deps\"]\n")
	write(t, dir, ".yggdrasil.toml", "[hooks]\npost_create = [\"bun install\"]\n")

	setup, err := clonesetup.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if setup.File != ".legato.toml" || !reflect.DeepEqual(setup.Run, []string{"make deps"}) {
		t.Errorf("setup = %+v", setup)
	}
}

func TestLoadWithNoFileHasNothingToDo(t *testing.T) {
	setup, err := clonesetup.Load(t.TempDir())
	if !errors.Is(err, clonesetup.ErrNoSetup) {
		t.Errorf("err = %v, want ErrNoSetup (setup %+v)", err, setup)
	}
}

func TestLoadSaysWhatIsWrongWithAFile(t *testing.T) {
	for name, content := range map[string]string{
		"not TOML":     "[setup\nrun = 1",
		"unknown key":  "[setup]\nrnu = [\"x\"]\n",
		"wrong type":   "[setup]\nrun = \"bun install\"\n",
		"absolute":     "[setup]\ncopy = [\"/etc/passwd\"]\n",
		"outside":      "[setup]\nlink = [\"../secrets\"]\n",
		"outside deep": "[setup]\ncopy = [\"a/../../b\"]\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, ".legato.toml", content)
			_, err := clonesetup.Load(dir)
			if err == nil || !strings.Contains(err.Error(), ".legato.toml") {
				t.Errorf("err = %v, want one naming .legato.toml", err)
			}
		})
	}
}
